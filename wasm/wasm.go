// Package wasm runs a WebAssembly program built for WASI the way a command runs: with arguments, an environment,
// standard streams and directories, to an exit code. The program reaches nothing else, except through the one host
// call its embedder chooses to answer.
package wasm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"slices"
	"sync"
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

	// Unstoppable runs the program at full speed and gives up stopping it: a run then ends only when the program
	// does, whatever its context. Stopping costs a call into Go on every loop iteration, which slows an interpreter
	// such as CPython three to four times. Set it only for a program trusted to end.
	Unstoppable bool

	// Host answers the program's host call, and HostModule is the module name the binary imports it from. The
	// module exports call(ptr, len) -> replyLen, which hands Host a message from the program's memory, and
	// read(ptr), which copies the reply into it. Nil gives the program no host call.
	HostModule string
	Host       func(ctx context.Context, request []byte) []byte
}

// A Module is a compiled Program. It is safe for concurrent runs, each with its own memory.
type Module struct {
	runtime wazero.Runtime
	code    wazero.CompiledModule
	replies sync.Map // a running instance to the reply it has not read yet
	sum     [sha256.Size]byte
}

// Sum identifies the program the module was compiled from, and how: what one module writes for itself to read
// later, such as an engine's bytecode, is good only for a module with the same sum.
func (m *Module) Sum() [sha256.Size]byte { return m.sum }

// Compile compiles p once, for every run of it.
func Compile(ctx context.Context, p Program) (*Module, error) {
	// ponytail: a goroutine cannot be killed, so wazero stops a run by having it check at every loop. The upgrade
	// that keeps both speed and the stop is to run programs in a worker process, which the system can kill.
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
	m := &Module{runtime: wazero.NewRuntimeWithConfig(ctx, cfg), sum: sha256.Sum256(slices.Concat(p.Binary, []byte{byte(p.Features), b2i(p.Unstoppable)}))}
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, m.runtime); err != nil {
		return nil, errors.Join(err, m.Close(ctx))
	}
	if p.Host != nil {
		_, err := m.runtime.NewHostModuleBuilder(p.HostModule).
			NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, ptr, n uint32) uint32 {
			request, ok := mod.Memory().Read(ptr, n)
			if !ok {
				panic("host call: the message is outside the program's memory")
			}
			reply := p.Host(ctx, request)
			m.replies.Store(mod, reply)
			return uint32(len(reply))
		}).Export("call").
			NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, ptr uint32) {
			reply, _ := m.replies.LoadAndDelete(mod)
			if b, _ := reply.([]byte); !mod.Memory().Write(ptr, b) {
				panic("host call: the reply does not fit the program's memory")
			}
		}).Export("read").
			Instantiate(ctx)
		if err != nil {
			return nil, errors.Join(err, m.Close(ctx))
		}
	}
	var err error
	if m.code, err = m.runtime.CompileModule(ctx, p.Binary); err != nil {
		return nil, errors.Join(err, m.Close(ctx))
	}
	return m, nil
}

func b2i(b bool) byte {
	if b {
		return 1
	}
	return 0
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
		fs = fs.(sysfs.FSConfig).WithSysFSMount(&dirFS{fs: dir, readOnly: d.ReadOnly}, d.Guest)
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
	mod, err := m.runtime.InstantiateModule(ctx, m.code, cfg)
	if mod != nil {
		m.replies.Delete(mod)
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
	return 0, nil
}

// stdin is a program's standard input, read for it by a goroutine, so that a program waiting on its input is still
// stopped when its context ends: a Read of the caller's reader cannot be interrupted, and wazero would make the
// program wait in that Read. It is pollable, so a program can wait for input with a timeout, as the agent's engine
// does to keep its timers running.
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
