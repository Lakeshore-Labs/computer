package wasm

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/tetratelabs/wazero/api"

	"github.com/Lakeshore-Labs/computer/files"
)

// A worker is this executable started again, which compiles the program from the cache and runs it, one run at a
// time, at full speed: unstoppable, without wazero's check at every loop. A run is stopped by killing its worker,
// which the system can do to a process and Go cannot do to a goroutine. A worker that ends its run is kept for the
// next, since loading the compiled code is most of starting one (about 0.9 s for CPython); a killed one is replaced
// at once, in the background.
//
// The parent and a worker speak in frames on the worker's standard input and output: a kind, a length and that
// many bytes. Everything a run is given crosses there: its arguments, environment and directories (host paths,
// which the worker opens itself), its standard input, read by the parent only when the program asks, as in this
// process, its output, each write answered with the parent's error, and the host call, answered by the parent's
// Program.Host.

// workerEnv marks a process started as a worker.
const workerEnv = "WASM_WORKER"

// workerExe is the executable a worker is started from; empty when Worker was not called, and runs are in this
// process.
var workerExe string

// workerHeap is how much a worker's Go heap may grow beyond what the compiled program needs before it is collected
// (see serve). wazero allocates a copy of the program's stack at every try_table, which C++ enters at nearly every
// call, so CPython drawing a chart makes gigabytes a second of short-lived garbage; with 256 MiB the worker collects
// all the time and runs it at half speed. 512 MiB is as fast as GOGC=100, with the same peak (about 1.1 GB for
// bench.py's pandas and matplotlib).
const workerHeap = 512 << 20

// idleFor is how long a worker beyond the first is kept when no run uses it.
var idleFor = time.Minute

// Worker lets Compile run programs in worker processes, at full speed and still stopped at their context's end. A
// worker is this executable started again: a program calls Worker first thing in main, before it reads flags or
// starts anything, and in a process started as a worker Worker serves its parent and exits without returning.
// Otherwise it returns at once. Without it a program runs in this process, stoppable or fast as Program.Unstoppable
// says. A worker ends when its parent does.
func Worker() {
	if os.Getenv(workerEnv) != "" {
		os.Exit(serve(os.Stdin, os.Stdout))
	}
	if !canWork {
		return
	}
	if exe, err := os.Executable(); err == nil {
		workerExe = exe
	}
}

// The kinds of frame. From the parent: the program and its binary, a run, a piece of standard input, the reply to a
// host call, the answer to a write. From a worker: ready, failed to compile, output, a request for input, a host
// call, the run's end.
const (
	frameProgram = 'p'
	frameBinary  = 'b'
	frameRun     = 'r'
	frameInput   = 'i'
	frameReply   = 'h'
	frameWritten = 'a'

	frameReady  = 'k'
	frameFailed = 'f'
	frameStdout = 'o'
	frameStderr = 'e'
	frameWant   = 'w'
	frameCall   = 'c'
	frameExit   = 'x'
)

type frame struct {
	kind byte
	data []byte
}

func writeFrame(w io.Writer, kind byte, data []byte) error {
	var head [5]byte
	head[0] = kind
	binary.BigEndian.PutUint32(head[1:], uint32(len(data)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func readFrame(r io.Reader) (frame, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return frame{}, err
	}
	f := frame{kind: head[0], data: make([]byte, binary.BigEndian.Uint32(head[1:]))}
	_, err := io.ReadFull(r, f.data)
	return f, err
}

// workerProgram is a Program as it crosses to a worker, before its binary.
type workerProgram struct {
	Features    api.CoreFeatures
	MemoryPages uint32
	CacheDir    string
	Host        bool
}

// workerRun is a Run as it crosses to a worker.
type workerRun struct {
	Args  []string
	Env   map[string]string
	Dirs  []Dir // host directories only: FS is nil
	Stdin bool
}

type workerExit struct {
	Code int
	Err  string
}

// errorFrame is an error as it crosses: the system's error number, when it is one, so that a program sees the
// error it would in this process (a closed pipe as EPIPE), then the message.
func errorFrame(err error) []byte {
	if err == nil {
		return nil
	}
	var b [4]byte
	var errno syscall.Errno
	if errors.As(err, &errno) {
		binary.BigEndian.PutUint32(b[:], uint32(errno))
	}
	return append(b[:], err.Error()...)
}

func frameError(b []byte) error {
	if len(b) < 4 {
		return nil
	}
	if errno := syscall.Errno(binary.BigEndian.Uint32(b)); errno != 0 {
		return errno
	}
	return errors.New(string(b[4:]))
}

// hostDirs are the directories as a worker opens them, by host path, and whether every one is a host directory.
func hostDirs(dirs []Dir) ([]Dir, bool) {
	out := make([]Dir, len(dirs))
	for i, d := range dirs {
		switch fs := d.FS.(type) {
		case nil:
			host, err := filepath.Abs(d.Host)
			if err != nil {
				return nil, false
			}
			out[i] = Dir{Guest: d.Guest, Host: host, ReadOnly: d.ReadOnly}
		case *files.Root:
			out[i] = Dir{Guest: d.Guest, Host: fs.Path(), ReadOnly: d.ReadOnly}
		default: // in this process's memory, or anything else only this process can reach
			return nil, false
		}
	}
	return out, true
}

// pool is a program's workers.
type pool struct {
	program Program

	mu     sync.Mutex
	idle   []*worker
	all    map[*worker]bool
	closed bool
}

type worker struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	frames chan frame    // from the worker; closed when it has ended
	ended  chan struct{} // closed when the process has been waited for
	err    error         // why it ended, set before ended is closed
	stderr tail
	reap   *time.Timer // ends the worker when it has been idle too long
}

// tail keeps the end of a worker's own standard error, where Go reports a crash.
type tail struct {
	mu sync.Mutex
	b  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = t.b[len(t.b)-4096:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

func newPool(ctx context.Context, p Program) (*pool, error) {
	pl := &pool{program: p, all: map[*worker]bool{}}
	w, err := pl.start()
	if err != nil {
		return nil, err
	}
	pl.put(w)
	return pl, nil
}

// start starts a worker and waits for it to be ready.
func (pl *pool) start() (*worker, error) {
	cmd := exec.Command(workerExe)
	cmd.Env = append(os.Environ(), workerEnv+"=1")
	cmd.SysProcAttr = procAttr()
	w := &worker{cmd: cmd, frames: make(chan frame, 16), ended: make(chan struct{})}
	cmd.Stderr = &w.stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	w.in = in
	pl.mu.Lock()
	if pl.closed {
		pl.mu.Unlock()
		return nil, errors.New("wasm: the module is closed")
	}
	if err := cmd.Start(); err != nil {
		pl.mu.Unlock()
		return nil, err
	}
	pl.all[w] = true
	pl.mu.Unlock()
	go func() {
		r := bufio.NewReaderSize(out, 64<<10)
		for {
			f, err := readFrame(r)
			if err != nil {
				break
			}
			w.frames <- f
		}
		w.err = cmd.Wait()
		pl.mu.Lock()
		delete(pl.all, w)
		pl.mu.Unlock()
		close(w.ended)
		close(w.frames)
	}()
	p := pl.program
	header, _ := json.Marshal(workerProgram{Features: p.Features, MemoryPages: p.MemoryPages, CacheDir: p.CacheDir, Host: p.Host != nil})
	if err := errors.Join(writeFrame(in, frameProgram, header), writeFrame(in, frameBinary, p.Binary)); err != nil {
		return nil, w.fail(err)
	}
	for f := range w.frames {
		switch f.kind {
		case frameReady:
			return w, nil
		case frameFailed:
			w.kill()
			return nil, errors.New(string(f.data))
		}
	}
	return nil, w.fail(errors.New("the worker ended before it was ready"))
}

// fail kills the worker and is err, with what the worker said as it ended.
func (w *worker) fail(err error) error {
	w.kill()
	if w.err != nil {
		err = fmt.Errorf("%w (%v)", err, w.err)
	}
	if s := w.stderr.String(); s != "" {
		err = fmt.Errorf("%w: %s", err, s)
	}
	return fmt.Errorf("wasm worker: %w", err)
}

// kill ends the worker and waits until it has. What it said last is dropped, or its reader would wait to hand it
// over.
func (w *worker) kill() {
	_ = w.cmd.Process.Kill()
	for range w.frames {
	}
	<-w.ended
}

// get is a worker ready for a run: an idle one, or a new one.
func (pl *pool) get(ctx context.Context) (*worker, error) {
	pl.mu.Lock()
	if n := len(pl.idle); n > 0 {
		w := pl.idle[n-1]
		pl.idle = pl.idle[:n-1]
		if w.reap != nil {
			w.reap.Stop()
		}
		pl.mu.Unlock()
		return w, nil
	}
	pl.mu.Unlock()
	type started struct {
		w   *worker
		err error
	}
	ch := make(chan started, 1)
	go func() {
		w, err := pl.start()
		ch <- started{w, err}
	}()
	select {
	case s := <-ch:
		return s.w, s.err
	case <-ctx.Done():
		go func() { // the worker is kept for the next run
			if s := <-ch; s.err == nil {
				pl.put(s.w)
			}
		}()
		return nil, ctx.Err()
	}
}

// put keeps a worker for the next run. One idle worker is kept for as long as the pool is; another ends after
// idleFor.
func (pl *pool) put(w *worker) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if pl.closed {
		go w.kill()
		return
	}
	pl.idle = append(pl.idle, w)
	if len(pl.idle) > 1 {
		w.reap = time.AfterFunc(idleFor, func() {
			pl.mu.Lock()
			defer pl.mu.Unlock()
			for i, idle := range pl.idle {
				if idle == w && len(pl.idle) > 1 {
					pl.idle = append(pl.idle[:i], pl.idle[i+1:]...)
					_ = w.in.Close() // the worker ends when its input does
					return
				}
			}
		})
	}
}

// replace starts a worker in place of a killed one, unless there is one idle.
func (pl *pool) replace() {
	pl.mu.Lock()
	enough := len(pl.idle) > 0 || pl.closed
	pl.mu.Unlock()
	if enough {
		return
	}
	go func() {
		if w, err := pl.start(); err == nil {
			pl.put(w)
		}
	}()
}

func (pl *pool) close() error {
	pl.mu.Lock()
	pl.closed = true
	all := make([]*worker, 0, len(pl.all))
	for w := range pl.all {
		all = append(all, w)
	}
	pl.idle = nil
	pl.mu.Unlock()
	for _, w := range all {
		w.kill()
	}
	return nil
}

// run runs r in a worker; dirs are r's directories as hostDirs gives them.
func (pl *pool) run(ctx context.Context, r Run, dirs []Dir) (int, error) {
	w, err := pl.get(ctx)
	switch {
	case err == nil && ctx.Err() != nil:
		pl.put(w)
		fallthrough
	case ctx.Err() != nil:
		return 124, ctx.Err()
	case err != nil:
		return 70, err
	}
	spec, _ := json.Marshal(workerRun{Args: r.Args, Env: r.Env, Dirs: dirs, Stdin: r.Stdin != nil})
	if err := writeFrame(w.in, frameRun, spec); err != nil {
		return 70, w.fail(err)
	}
	stdout, stderr := r.Stdout, r.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	// What the parent answers with comes from goroutines, and is sent to the worker only by this loop and only
	// during this run: a read of the standard input that ends after the run is not given to the next.
	input := make(chan []byte, 1)
	replies := make(chan []byte, 1)
	for {
		select {
		case f, ok := <-w.frames:
			if !ok {
				if ctx.Err() != nil {
					return 124, ctx.Err()
				}
				return 70, w.fail(errors.New("the worker ended during the run"))
			}
			var err error
			switch f.kind {
			case frameStdout, frameStderr:
				out := stdout
				if f.kind == frameStderr {
					out = stderr
				}
				_, werr := out.Write(f.data)
				err = writeFrame(w.in, frameWritten, errorFrame(werr))
			case frameWant:
				go func(n int) {
					b := make([]byte, n)
					n, err := r.Stdin.Read(b)
					for n == 0 && err == nil {
						n, err = r.Stdin.Read(b)
					}
					input <- inputFrame(b[:n], err)
				}(int(binary.BigEndian.Uint32(f.data)))
			case frameCall:
				go func(request []byte) { replies <- pl.program.Host(ctx, request) }(f.data)
			case frameExit:
				var exit workerExit
				_ = json.Unmarshal(f.data, &exit)
				pl.put(w) // which has ended its run, and is ready for the next
				if ctx.Err() != nil {
					return 124, ctx.Err()
				}
				if exit.Err != "" {
					return exit.Code, errors.New(exit.Err)
				}
				return exit.Code, nil
			}
			if err != nil {
				return 70, w.fail(err)
			}
		case b := <-input:
			if err := writeFrame(w.in, frameInput, b); err != nil {
				return 70, w.fail(err)
			}
		case b := <-replies:
			if err := writeFrame(w.in, frameReply, b); err != nil {
				return 70, w.fail(err)
			}
		case <-ctx.Done():
			w.kill()
			pl.replace()
			return 124, ctx.Err()
		}
	}
}

// serve is a worker's main: it compiles the program its parent sends, then runs what it is asked, one run at a
// time, until its input ends, which is when the parent has ended or let it go.
//
// A program running unstoppable reaches no point where Go could stop it until it calls into Go, and Go's collector
// stops every goroutine at least twice in a collection: one during a long loop would freeze the worker, and with
// it the goroutine that sees the parent end. A collection starts on a timer, every two minutes, or when the heap
// has grown by GOGC. So the worker has no timer and collects only when the heap has grown by workerHeap, which a
// loop that calls nothing does not grow; and a run's memory is mapped from the system, outside Go's heap.
func serve(stdin io.Reader, stdout io.Writer) int {
	debug.SetGCPercent(-1)
	runtime.GOMAXPROCS(max(2, runtime.GOMAXPROCS(0))) // one for the program, at least one for the rest
	in := bufio.NewReaderSize(stdin, 64<<10)
	s := &server{out: stdout}
	fail := func(err error) int {
		s.send(frameFailed, []byte(err.Error()))
		return 1
	}
	head, err := readFrame(in)
	if err != nil || head.kind != frameProgram {
		return 1
	}
	var wp workerProgram
	if err := json.Unmarshal(head.data, &wp); err != nil {
		return fail(err)
	}
	bin, err := readFrame(in)
	if err != nil || bin.kind != frameBinary {
		return 1
	}
	p := Program{Binary: bin.data, Features: wp.Features, MemoryPages: wp.MemoryPages, CacheDir: wp.CacheDir, Unstoppable: true}
	if wp.Host {
		p.Host = s.host
	}
	l, err := compileLocal(context.Background(), p, mappedMemory{})
	if err != nil {
		return fail(err)
	}
	p, bin = Program{}, frame{}
	debug.FreeOSMemory()
	var heap runtime.MemStats
	runtime.ReadMemStats(&heap)
	debug.SetMemoryLimit(int64(heap.HeapAlloc) + workerHeap)
	runs := make(chan *served)
	go func() {
		defer os.Exit(0) // the parent has ended, or let this worker go
		for {
			f, err := readFrame(in)
			if err != nil {
				return
			}
			s.mu.Lock()
			current := s.current
			s.mu.Unlock()
			if f.kind == frameRun {
				var wr workerRun
				if err := json.Unmarshal(f.data, &wr); err != nil {
					return
				}
				run := &served{spec: wr, input: make(chan []byte, 1), reply: make(chan []byte, 1), written: make(chan []byte, 1)}
				s.mu.Lock()
				s.current = run
				s.mu.Unlock()
				runs <- run
				continue
			}
			if current == nil { // an answer to a run that has ended
				continue
			}
			var to chan []byte
			switch f.kind {
			case frameInput:
				to = current.input
			case frameReply:
				to = current.reply
			case frameWritten:
				to = current.written
			}
			select {
			case to <- f.data:
			default:
			}
		}
	}()
	s.send(frameReady, nil)
	for run := range runs {
		ctx, cancel := context.WithCancel(context.Background())
		r := Run{Args: run.spec.Args, Env: run.spec.Env, Dirs: run.spec.Dirs,
			Stdout: output{s, run, frameStdout}, Stderr: output{s, run, frameStderr}}
		if run.spec.Stdin {
			r.Stdin = remoteInput{ctx, s, run}
		}
		code, err := l.run(ctx, r)
		cancel()
		exit := workerExit{Code: code}
		if err != nil {
			exit.Err = err.Error()
		}
		b, _ := json.Marshal(exit)
		s.mu.Lock()
		s.current = nil
		s.mu.Unlock()
		s.send(frameExit, b)
		debug.FreeOSMemory()
	}
	return 0
}

// server is a worker's side of its frames.
type server struct {
	out     io.Writer
	mu      sync.Mutex // for out, and current
	current *served
}

// served is a run in the worker, and where its parent's answers go.
type served struct {
	spec                  workerRun
	input, reply, written chan []byte
}

func (s *server) send(kind byte, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = writeFrame(s.out, kind, data)
}

func (s *server) host(ctx context.Context, request []byte) []byte {
	s.mu.Lock()
	run := s.current
	s.mu.Unlock()
	s.send(frameCall, request)
	return <-run.reply
}

// output is a run's standard output or error: each write is the parent's, and returns its error.
type output struct {
	s    *server
	run  *served
	kind byte
}

func (o output) Write(p []byte) (int, error) {
	o.s.send(o.kind, p)
	if err := frameError(<-o.run.written); err != nil {
		return 0, err
	}
	return len(p), nil
}

// remoteInput is a run's standard input, read by the parent when the program asks.
type remoteInput struct {
	ctx context.Context
	s   *server
	run *served
}

func (r remoteInput) Read(p []byte) (int, error) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(p)))
	r.s.send(frameWant, n[:])
	select {
	case b := <-r.run.input:
		data, err := fromInputFrame(b)
		return copy(p, data), err
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
}

// inputFrame is what one read of the standard input gave: the data's length, the data, and the error, if any, with
// the end of the input as no error number and no message.
func inputFrame(data []byte, err error) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	b = append(b, data...)
	if err == io.EOF {
		return append(b, 0, 0, 0, 0)
	}
	return append(b, errorFrame(err)...)
}

func fromInputFrame(b []byte) ([]byte, error) {
	n := binary.BigEndian.Uint32(b)
	data, rest := b[4:4+n], b[4+n:]
	if len(rest) == 4 && binary.BigEndian.Uint32(rest) == 0 {
		return data, io.EOF
	}
	return data, frameError(rest)
}
