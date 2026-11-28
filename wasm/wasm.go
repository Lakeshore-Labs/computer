// Package wasm runs a WebAssembly program built for WASI the way a command runs: with arguments, an environment,
// standard streams and directories, to an exit code. The program reaches nothing else, except through the one host
// call its embedder chooses to answer.
package wasm

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/spf13/afero"
	"github.com/tetratelabs/wazero"

	"github.com/Lakeshore-Labs/computer/files"
	"github.com/tetratelabs/wazero/api"
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

	// MaxRuns is how many runs there may be at once; a further one waits for its turn until its context ends. Each
	// run may take MemoryPages of memory, so this is what bounds the memory of all of them. Zero is no limit.
	MaxRuns int

	// Host answers the program's host call: host::call(ptr, len, replyPtrPtr, replyLenPtr) -> status, as
	// wasi-python's door is. It hands Host the request in the program's memory, puts the reply in memory the
	// program's exported host_alloc gives, and stores the reply's address and length. Nil gives the program no
	// host call: the call answers -1.
	Host func(ctx context.Context, request []byte) []byte
}

// A Module is a compiled Program. It is safe for concurrent runs, each with its own memory.
type Module struct {
	runtime wazero.Runtime
	code    wazero.CompiledModule
	reactor bool          // the binary exports run, called after _initialize, in place of a main
	turns   chan struct{} // one place for each run allowed at once; nil when there is no limit
}

// Compile compiles p once, for every run of it.
func Compile(ctx context.Context, p Program) (*Module, error) {
	// ponytail: a goroutine cannot be killed, so wazero stops a run by having it check at every loop. The upgrade
	// that keeps both speed and the stop is to run programs in a worker process, which the system can kill.
	cfg := wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | p.Features).WithCloseOnContextDone(true).
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
	m := &Module{runtime: wazero.NewRuntimeWithConfig(ctx, cfg)}
	if p.MaxRuns > 0 {
		m.turns = make(chan struct{}, p.MaxRuns)
	}
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, m.runtime); err != nil {
		return nil, errors.Join(err, m.Close(ctx))
	}
	_, err := m.runtime.NewHostModuleBuilder("host").NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr, n, replyPtrPtr, replyLenPtr uint32) int32 {
			if p.Host == nil {
				return -1
			}
			request, ok := mod.Memory().Read(ptr, n)
			alloc := mod.ExportedFunction("host_alloc")
			if !ok || alloc == nil {
				return -2
			}
			reply := p.Host(ctx, slices.Clone(request)) // the program's memory may move while the host answers
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
		return nil, errors.Join(err, m.Close(ctx))
	}
	if m.code, err = m.runtime.CompileModule(ctx, p.Binary); err != nil {
		return nil, errors.Join(err, m.Close(ctx))
	}
	_, m.reactor = m.code.ExportedFunctions()["run"]
	return m, nil
}

// Close frees the compiled code.
func (m *Module) Close(ctx context.Context) error { return m.runtime.Close(ctx) }

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
	fs := wazero.NewFSConfig()
	for _, d := range r.Dirs {
		dir := d.FS
		if dir == nil {
			host, err := files.Dir(d.Host)
			if err != nil {
				return 70, err
			}
			defer host.Close()
			dir = host
		}
		fs = fs.(sysfs.FSConfig).WithSysFSMount(newDirFS(dir, d.ReadOnly), d.Guest)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
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
	code := 0
	if m.reactor { // a reactor is started by _initialize, and its run returns the exit code
		cfg = cfg.WithStartFunctions("_initialize")
	}
	mod, err := m.runtime.InstantiateModule(ctx, m.code, cfg)
	if err == nil && m.reactor {
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
