// Package wasm runs a WebAssembly program built for WASI the way a command runs: with arguments, an environment,
// standard streams and directories, to an exit code. The program reaches nothing else, except through the one host
// call its embedder chooses to answer.
package wasm

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/afero"
	"github.com/tetratelabs/wazero"

	"github.com/Lakeshore-Labs/computer/files"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	"github.com/tetratelabs/wazero/experimental/sysfs"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// A Program is a WASI binary and what it is allowed.
type Program struct {
	Binary      []byte
	Features    api.CoreFeatures // what the binary needs beyond WebAssembly 2.0, such as exception handling
	MemoryPages uint32           // the most memory one run may hold, in 64 KiB pages; 0 is wazero's limit of 4 GiB
	CacheDir    string           // where compiled code is kept between processes; empty keeps none

	// Unstoppable runs the program at full speed and gives up stopping it: a run then ends only when the program
	// does, whatever its context. Stopping costs a call into Go on every loop iteration, which slows an interpreter
	// such as CPython three to four times. Set it only for a program trusted to end. A program run in worker
	// processes (see Worker) is at full speed and stopped, so this matters only for a run in this process.
	Unstoppable bool

	// MaxRuns is how many runs there may be at once; a further one waits for its turn until its context ends. Each
	// run may take MemoryPages of memory, so this is what bounds the memory of all of them. Zero is no limit.
	MaxRuns int

	// Host answers the program's host call: host::call(ptr, len, replyPtrPtr, replyLenPtr) -> status, as
	// wasi-python's door is. It hands Host the request in the program's memory, puts the reply in memory the
	// program's exported host_alloc gives, and stores the reply's address and length. Nil gives the program no
	// host call: the call answers -1.
	Host func(ctx context.Context, request []byte) []byte

	// Prepare, when set, is code run once in a program that has wasi-python's prepare, whose memory every run then
	// starts from: what the code imported is imported already. See Preparation.
	Prepare *Preparation
}

// A Preparation is a starting image for every run of a program: Code is run once, in an instance given Run's
// arguments, environment and directories, and the instance's memory, read whole, is copied into each later instance
// before its run. The program must be one that wasi-python's prepare can make an image of (its plain reactor,
// started with wizer.initialize, or its snapshot), so that the image holds the whole of its state.
//
// The image is kept in CacheDir, named by the binary, Code, Run and Key, so a later process takes it from there,
// and trusts it as it trusts the compiled code kept there. Key names what else Code reads, such as the files of
// Run's directories.
type Preparation struct {
	Code string
	Run  Run
	Key  string
}

// A Module is a compiled Program. It is safe for concurrent runs, each with its own memory.
type Module struct {
	program Program
	turns   chan struct{} // one place for each run allowed at once; nil when there is no limit
	workers *pool         // the worker processes, when Worker was called and the program has a CacheDir

	// local runs the program in this process: always without workers, and with them only for a run given a
	// directory that is not the host's, which a worker cannot open. With workers it is compiled on first use.
	localOnce sync.Once
	local     *local
	localErr  error
}

// Compile compiles p once, for every run of it. When the process called Worker and p has a CacheDir, the program
// runs in worker processes, compiled there from the cache, and Compile waits for the first worker to be ready.
func Compile(ctx context.Context, p Program) (*Module, error) {
	m := &Module{program: p}
	if p.MaxRuns > 0 {
		m.turns = make(chan struct{}, p.MaxRuns)
	}
	if workerExe != "" && p.CacheDir != "" {
		var err error
		m.workers, err = newPool(ctx, p)
		return m, err
	}
	m.localOnce.Do(func() { m.local, m.localErr = compileLocal(ctx, p, nil) })
	return m, m.localErr
}

// Close frees the compiled code, and ends the workers, with any run in them.
func (m *Module) Close(ctx context.Context) error {
	var errs []error
	if m.workers != nil {
		errs = append(errs, m.workers.close())
	}
	m.localOnce.Do(func() {}) // a local compiled from now on would not be closed
	if m.local != nil {
		errs = append(errs, m.local.runtime.Close(ctx))
	}
	return errors.Join(errs...)
}

// A Dir is a directory the program sees at the path Guest: the filesystem FS, or, when FS is nil, the host
// directory Host.
type Dir struct {
	Guest, Host string
	FS          afero.Fs
	ReadOnly    bool
}

// A Run is one execution of a program. Args includes the program's own name.
type Run struct {
	Args           []string
	Env            map[string]string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Dirs           []Dir
}

// Run runs the program to its end and returns its exit code. When ctx ends first the program is stopped and the
// error is ctx's.
func (m *Module) Run(ctx context.Context, r Run) (int, error) {
	if m.turns != nil {
		select {
		case m.turns <- struct{}{}:
			defer func() { <-m.turns }()
		case <-ctx.Done():
			return 124, ctx.Err()
		}
	}
	if m.workers != nil {
		if dirs, ok := hostDirs(r.Dirs); ok {
			return m.workers.run(ctx, r, dirs)
		}
		m.localOnce.Do(func() { m.local, m.localErr = compileLocal(context.WithoutCancel(ctx), m.program, nil) })
		if m.localErr != nil {
			return 70, m.localErr
		}
	}
	return m.local.run(ctx, r)
}

// local is a program compiled in this process.
type local struct {
	runtime wazero.Runtime
	code    wazero.CompiledModule
	reactor bool                         // the binary exports run, called after _initialize, in place of a main
	memory  experimental.MemoryAllocator // where a run's memory is kept; nil is wazero's own, on the Go heap
	image   []byte                       // the memory every run starts from, a whole number of pages; nil without Prepare
}

func compileLocal(ctx context.Context, p Program, memory experimental.MemoryAllocator) (*local, error) {
	// A goroutine cannot be killed, so wazero stops a run by having it check at every loop, unless it is
	// unstoppable. A worker process runs its program unstoppable and is killed instead.
	cfg := wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | p.Features).WithCloseOnContextDone(!p.Unstoppable).
		WithDebugInfoEnabled(false) // or a stopped run spends seconds reading the binary's debug data for a stack trace
	if p.MemoryPages > 0 {
		cfg = cfg.WithMemoryLimitPages(p.MemoryPages)
	}
	if p.CacheDir != "" {
		cache, err := wazero.NewCompilationCacheWithDir(p.CacheDir)
		if err != nil {
			return nil, err
		}
		cfg = cfg.WithCompilationCache(cache)
	}
	l := &local{runtime: wazero.NewRuntimeWithConfig(ctx, cfg), memory: memory}
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, l.runtime); err != nil {
		return nil, errors.Join(err, l.runtime.Close(ctx))
	}
	host := p.Host // and not p, whose Binary the function would keep
	_, err := l.runtime.NewHostModuleBuilder("host").NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr, n, replyPtrPtr, replyLenPtr uint32) int32 {
			if host == nil {
				return -1
			}
			request, ok := mod.Memory().Read(ptr, n)
			alloc := mod.ExportedFunction("host_alloc")
			if !ok || alloc == nil {
				return -2
			}
			reply := host(ctx, slices.Clone(request)) // the program's memory may move while the host answers
			at, err := alloc.Call(ctx, uint64(len(reply)))
			if err != nil || len(at) == 0 || at[0] == 0 && len(reply) > 0 { // 0 is malloc's answer when the heap is full
				return -2
			}
			if !mod.Memory().Write(uint32(at[0]), reply) || !mod.Memory().WriteUint32Le(replyPtrPtr, uint32(at[0])) ||
				!mod.Memory().WriteUint32Le(replyLenPtr, uint32(len(reply))) {
				return -2
			}
			return 0
		}).Export("call").Instantiate(ctx)
	if err != nil {
		return nil, errors.Join(err, l.runtime.Close(ctx))
	}
	// A kept image is looked for while the program compiles: naming it hashes the binary, and both take a while.
	kept := make(chan cachedImage, 1)
	if p.Prepare != nil {
		go func() { kept <- findImage(p) }()
	}
	if l.code, err = l.runtime.CompileModule(ctx, p.Binary); err != nil {
		return nil, errors.Join(err, l.runtime.Close(ctx))
	}
	_, l.reactor = l.code.ExportedFunctions()["run"]
	if p.Prepare != nil {
		if l.image, err = l.prepare(ctx, p.Prepare, <-kept); err != nil {
			return nil, errors.Join(err, l.runtime.Close(ctx))
		}
	}
	return l, nil
}

func (l *local) run(ctx context.Context, r Run) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	code := 0
	mod, closeDirs, err := l.instantiate(ctx, r, l.image == nil)
	defer closeDirs()
	if err == nil && l.image != nil {
		err = restore(mod.Memory(), l.image)
	}
	if err == nil && l.reactor {
		var res []uint64
		if res, err = mod.ExportedFunction("run").Call(ctx); err == nil && len(res) == 1 {
			code = int(int32(res[0]))
		}
	}
	if mod != nil {
		_ = mod.Close(ctx)
	}
	var exit *sys.ExitError
	switch {
	case ctx.Err() != nil:
		return 124, ctx.Err()
	case errors.As(err, &exit):
		return int(exit.ExitCode()), nil
	case err != nil:
		return 70, err
	}
	return code, nil
}

// instantiate makes an instance of the program for r, started by its _initialize when start is set and the program
// is a reactor. closeDirs closes the host directories it opened, and is to be called when the instance is closed.
func (l *local) instantiate(ctx context.Context, r Run, start bool) (_ api.Module, closeDirs func(), _ error) {
	var opened []io.Closer
	closeDirs = func() {
		for _, c := range opened {
			_ = c.Close()
		}
	}
	fs := wazero.NewFSConfig()
	for _, d := range r.Dirs {
		dir := d.FS
		if dir == nil {
			host, err := files.Dir(d.Host)
			if err != nil {
				return nil, closeDirs, err
			}
			opened = append(opened, host)
			dir = host
		}
		fs = fs.(sysfs.FSConfig).WithSysFSMount(newDirFS(dir, d.ReadOnly), d.Guest)
	}
	if r.Stdin != nil {
		r.Stdin = newStdin(ctx, r.Stdin)
	}
	// The empty name lets any number of instances of the one module run at once.
	cfg := wazero.NewModuleConfig().WithName("").WithArgs(r.Args...).WithFSConfig(fs).
		WithStdin(r.Stdin).WithStdout(r.Stdout).WithStderr(r.Stderr).
		WithSysWalltime().WithSysNanotime().WithRandSource(rand.Reader).
		WithNanosleep(func(ns int64) { // a sleep the run's deadline can end, which time.Sleep is not
			select {
			case <-time.After(time.Duration(ns)):
			case <-ctx.Done():
			}
		})
	for k, v := range r.Env {
		cfg = cfg.WithEnv(k, v)
	}
	switch {
	case !start:
		cfg = cfg.WithStartFunctions() // the image is the started program
	case l.reactor: // a reactor is started by _initialize, and its run returns the exit code
		cfg = cfg.WithStartFunctions("_initialize")
	}
	mod, err := l.runtime.InstantiateModule(experimental.WithMemoryAllocator(ctx, l.memory), l.code, cfg)
	return mod, closeDirs, err
}

// restore puts image in mem, which must be no larger. The instance's one mutable global, its stack pointer, is
// where the image's was, since the image was read between calls.
func restore(mem api.Memory, image []byte) error {
	if grow := len(image)/pageSize - int(mem.Size()/pageSize); grow > 0 {
		if _, ok := mem.Grow(uint32(grow)); !ok {
			return errors.New("wasm: the starting image is larger than a run's memory")
		}
	}
	if !mem.Write(0, image) {
		return errors.New("wasm: the starting image does not fit the run's memory")
	}
	return nil
}

const pageSize = 64 << 10

// A cachedImage is a starting image as CacheDir keeps it: the file it is kept in, and the image, nil when there is
// none yet.
type cachedImage struct {
	name  string // empty when there is no CacheDir
	image []byte
}

// findImage is p's starting image in CacheDir, named by everything it is made from.
func findImage(p Program) cachedImage {
	if p.CacheDir == "" {
		return cachedImage{}
	}
	h := sha256.New()
	field := func(s string) { fmt.Fprintf(h, "%d:%s", len(s), s) }
	h.Write(p.Binary)
	for _, s := range append([]string{p.Prepare.Code, p.Prepare.Key}, p.Prepare.Run.Args...) {
		field(s)
	}
	for _, k := range slices.Sorted(maps.Keys(p.Prepare.Run.Env)) {
		field(k)
		field(p.Prepare.Run.Env[k])
	}
	for _, d := range p.Prepare.Run.Dirs { // a directory given as an FS is named by its place alone
		field(d.Guest)
		field(d.Host)
	}
	c := cachedImage{name: filepath.Join(p.CacheDir, "images", hex.EncodeToString(h.Sum(nil)))}
	if image, err := os.ReadFile(c.name); err == nil && len(image)%pageSize == 0 && len(image) > 0 {
		c.image = image
	}
	return c
}

// prepare is the starting image: the one kept, or one made by running prep's code and then kept.
func (l *local) prepare(ctx context.Context, prep *Preparation, kept cachedImage) ([]byte, error) {
	if kept.image != nil {
		return kept.image, nil
	}
	image, err := l.makeImage(ctx, prep)
	if err != nil || kept.name == "" {
		return image, err
	}
	// Written whole and renamed into place, so a reader finds the image whole or not at all. A failure to keep
	// it costs only the next process the time to make it again.
	dir := filepath.Dir(kept.name)
	if err := os.MkdirAll(dir, 0o700); err == nil {
		if f, err := os.CreateTemp(dir, "making"); err == nil {
			_, err = f.Write(image)
			if err = errors.Join(err, f.Close()); err == nil {
				err = os.Rename(f.Name(), kept.name)
			}
			if err != nil {
				_ = os.Remove(f.Name())
			}
		}
		// An image is about 100 MB, and one made from another binary or code is no longer wanted: the others are
		// removed, which costs a process that still wants one the time to make it again.
		others, _ := filepath.Glob(filepath.Join(dir, "*"))
		for _, other := range others {
			if other != kept.name && !strings.HasPrefix(filepath.Base(other), "making") {
				_ = os.Remove(other)
			}
		}
	}
	return image, nil
}

// makeImage runs prep's code in a new instance, as wasi-python's prepare runs it: after wizer.initialize, which
// starts the interpreter as a snapshot's is started, unless the binary is a snapshot already.
func (l *local) makeImage(ctx context.Context, prep *Preparation) ([]byte, error) {
	var out bytes.Buffer
	r := prep.Run
	if r.Stdout == nil {
		r.Stdout = &out
	}
	if r.Stderr == nil {
		r.Stderr = &out
	}
	mod, closeDirs, err := l.instantiate(ctx, r, true)
	defer closeDirs()
	if err != nil {
		return nil, err
	}
	defer mod.Close(ctx)
	prepare, alloc := mod.ExportedFunction("prepare"), mod.ExportedFunction("host_alloc")
	if prepare == nil || alloc == nil {
		return nil, errors.New("wasm: the program has no prepare, so no starting image can be made of it")
	}
	if start := mod.ExportedFunction("wizer.initialize"); start != nil {
		if _, err := start.Call(ctx); err != nil {
			return nil, fmt.Errorf("wasm: starting the program for its image: %w: %s", err, out.Bytes())
		}
	}
	at, err := alloc.Call(ctx, uint64(len(prep.Code)))
	if err != nil || at[0] == 0 || !mod.Memory().Write(uint32(at[0]), []byte(prep.Code)) {
		return nil, errors.Join(errors.New("wasm: no memory for the starting image's code"), err)
	}
	res, err := prepare.Call(ctx, at[0], uint64(len(prep.Code)))
	if err != nil {
		return nil, fmt.Errorf("wasm: making the starting image: %w: %s", err, out.Bytes())
	}
	if status := int32(res[0]); status != 0 {
		return nil, fmt.Errorf("wasm: the starting image's code ended with status %d: %s", status, out.Bytes())
	}
	image, _ := mod.Memory().Read(0, mod.Memory().Size())
	return bytes.Clone(image), nil
}

// stdin is a program's standard input, read for it by a goroutine, so that a program waiting on its input is still
// stopped when its context ends: a Read of the caller's reader cannot be interrupted, and wazero would make the
// program wait in that Read. It is pollable, so a program can wait for input with a timeout, as Python's select
// does.
//
// The goroutine ends when the reader ends, or, once the run is over, when its pending Read returns. A reader whose
// Read never returns keeps the goroutine for as long as that Read lasts; nothing in Go can end it sooner.
type stdin struct {
	ctx     context.Context
	chunks  chan chunk
	wanted  chan struct{} // the program is waiting for input
	reading bool          // a read asked for has not yet been taken
	pending []byte
	err     error // what the reader returned after pending, or the context's error
}

type chunk struct {
	b   []byte
	err error
}

func newStdin(ctx context.Context, r io.Reader) *stdin {
	s := &stdin{ctx: ctx, chunks: make(chan chunk), wanted: make(chan struct{}, 1)}
	go func() {
		for {
			// The reader is read only when the program asks for input. Reading ahead would take, from a terminal
			// or a pipe the program shares, what was meant for whoever reads it after this program has ended.
			select {
			case <-s.wanted:
			case <-ctx.Done():
				return
			}
			b := make([]byte, 32<<10)
			n, err := r.Read(b)
			for n == 0 && err == nil {
				n, err = r.Read(b)
			}
			select {
			case s.chunks <- chunk{b[:n], err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return s
}

// fill waits until there is something to read or timeout fires, and says which. A nil timeout waits for ever.
func (s *stdin) fill(timeout <-chan time.Time) bool {
	if len(s.pending) > 0 || s.err != nil {
		return true
	}
	if !s.reading { // one read at a time: a wait that timed out left its read under way
		s.reading = true
		s.wanted <- struct{}{}
	}
	select {
	case c := <-s.chunks:
		s.pending, s.err, s.reading = c.b, c.err, false
	case <-s.ctx.Done():
		s.err = s.ctx.Err()
	case <-timeout:
		return false
	}
	return true
}

func (s *stdin) Read(p []byte) (int, error) {
	s.fill(nil)
	n := copy(p, s.pending)
	if s.pending = s.pending[n:]; n > 0 {
		return n, nil
	}
	return 0, s.err
}

// Poll implements wazero's experimental sys.Pollable, which it asks of a standard input before a program waits on it.
func (s *stdin) Poll(flag experimentalsys.Pflag, timeoutMillis int32) (bool, experimentalsys.Errno) {
	if flag != experimentalsys.POLLIN {
		return false, experimentalsys.ENOTSUP
	}
	var timeout <-chan time.Time
	if timeoutMillis >= 0 {
		timeout = time.After(time.Duration(timeoutMillis) * time.Millisecond)
	}
	return s.fill(timeout), 0
}
