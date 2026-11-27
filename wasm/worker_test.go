//go:build unix

package wasm

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/Lakeshore-Labs/computer/files"
)

func TestMain(m *testing.M) {
	Worker()
	if dir := os.Getenv("WASM_TEST_ORPHAN"); dir != "" {
		os.Exit(orphan(dir))
	}
	os.Exit(m.Run())
}

// spin is a WebAssembly program whose _start loops for ever and calls nothing.
var spin = []byte{0, 'a', 's', 'm', 1, 0, 0, 0,
	1, 4, 1, 0x60, 0, 0, // a type: no parameters, no results
	3, 2, 1, 0, // one function of it
	7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0, // exported as _start
	10, 9, 1, 7, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b} // loop; br 0; end

func compileIn(t *testing.T, p Program) *Module {
	t.Helper()
	ctx := context.Background()
	m, err := Compile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close(ctx) })
	return m
}

func TestAWorkerRunsTheProgramAndAnswersItsHostCall(t *testing.T) {
	m := compileIn(t, Program{Binary: door, CacheDir: t.TempDir(), Host: func(_ context.Context, req []byte) []byte {
		return append([]byte("pong to "), req...)
	}})
	if m.workers == nil {
		t.Fatal("the program does not run in workers")
	}
	for range 3 { // one worker, kept for each run
		var out bytes.Buffer
		if code, err := m.Run(context.Background(), Run{Args: []string{"door"}, Stdout: &out}); code != 0 || err != nil || out.String() != "pong to ping\n" {
			t.Errorf("exit %d, %v, %q", code, err, out.String())
		}
	}
}

func TestAProgramThatDoesNotEndIsKilledAtItsDeadline(t *testing.T) {
	m := compileIn(t, Program{Binary: spin, CacheDir: t.TempDir(), MaxRuns: 1})
	for range 2 { // and the next run has a worker
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		start := time.Now()
		code, err := m.Run(ctx, Run{Args: []string{"spin"}})
		cancel()
		if late := time.Since(start) - 500*time.Millisecond; late > 300*time.Millisecond {
			t.Errorf("stopped %v after its deadline", late)
		}
		if code != 124 || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("exit %d, %v; want 124 and the deadline", code, err)
		}
	}
}

func TestADirectoryInMemoryRunsInThisProcessAndIsStillStopped(t *testing.T) {
	m := compileIn(t, Program{Binary: spin, CacheDir: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	code, err := m.Run(ctx, Run{Args: []string{"spin"}, Dirs: []Dir{{Guest: "/mem", FS: afero.NewMemMapFs()}}})
	if code != 124 || !errors.Is(err, context.DeadlineExceeded) || m.local == nil {
		t.Errorf("exit %d, %v, in this process %v", code, err, m.local != nil)
	}
}

// orphan is a parent that starts a worker on a program that does not end, with dir as its cache, says the worker's
// process id, and waits to be killed.
func orphan(dir string) int {
	m, err := Compile(context.Background(), Program{Binary: spin, CacheDir: dir})
	if err != nil {
		fmt.Println(err)
		return 1
	}
	go m.Run(context.Background(), Run{Args: []string{"spin"}})
	time.Sleep(200 * time.Millisecond)
	m.workers.mu.Lock()
	for w := range m.workers.all {
		fmt.Println(w.cmd.Process.Pid)
	}
	m.workers.mu.Unlock()
	time.Sleep(time.Minute)
	return 0
}

func TestAWorkerEndsWithItsParent(t *testing.T) {
	parent := exec.Command(os.Args[0], "-test.run=^$")
	parent.Env = append(os.Environ(), "WASM_TEST_ORPHAN="+t.TempDir())
	out, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	pid, perr := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || perr != nil {
		_ = parent.Process.Kill()
		t.Fatalf("the parent said %q (%v)", line, err)
	}
	_ = parent.Process.Kill()
	_ = parent.Wait()
	for start := time.Now(); time.Since(start) < 5*time.Second; time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			t.Logf("the worker ended %v after its parent", time.Since(start).Round(time.Millisecond))
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Error("the worker running a program that does not end outlived its parent")
}

// A worker opens a mounted directory by its path, and refuses one that is no longer the directory this process
// holds open: the path may have been renamed away and made again since.
func TestAWorkerRefusesADirectoryThatWasReplacedAtItsPath(t *testing.T) {
	m := compileIn(t, Program{Binary: door, CacheDir: t.TempDir(), Host: func(context.Context, []byte) []byte { return nil }})
	base := t.TempDir()
	held, err := files.Dir(base + "/mounted")
	if err == nil {
		t.Fatal("opened a directory that is not there")
	}
	if err := os.Mkdir(base+"/mounted", 0o700); err != nil {
		t.Fatal(err)
	}
	if held, err = files.Dir(base + "/mounted"); err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	run := func() (int, error) {
		return m.Run(context.Background(), Run{Args: []string{"door"}, Dirs: []Dir{{Guest: "/d", FS: held}}})
	}
	if code, err := run(); code != 0 || err != nil {
		t.Fatalf("with the directory in place: exit %d, %v", code, err)
	}
	if err := errors.Join(os.Rename(base+"/mounted", base+"/moved"), os.Mkdir(base+"/mounted", 0o700)); err != nil {
		t.Fatal(err)
	}
	if code, err := run(); err == nil || !strings.Contains(err.Error(), "no longer") {
		t.Errorf("with another directory at the path: exit %d, %v", code, err)
	}
}

// Runs that end at their deadline while a worker starts leave no more workers than MaxRuns allows.
func TestThereAreNoMoreWorkersThanRuns(t *testing.T) {
	m := compileIn(t, Program{Binary: spin, CacheDir: t.TempDir(), MaxRuns: 1})
	for range 5 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, _ = m.Run(ctx, Run{Args: []string{"spin"}})
		cancel()
		m.workers.mu.Lock()
		n := len(m.workers.all)
		m.workers.mu.Unlock()
		if n > 1 {
			t.Fatalf("%d workers for a program of one run at a time", n)
		}
	}
}
