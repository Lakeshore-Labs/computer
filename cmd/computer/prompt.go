package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/Lakeshore-Labs/computer"
)

// prompt is the computer's Prompt at this process's terminal, which it puts in raw mode for as long as the prompt
// runs. A program that serves a terminal to a browser, such as gotty or ttyd, makes this the computer's web
// terminal, as does computer.TerminalHandler.
func prompt(ctx context.Context, c *computer.Computer, workDir string) error {
	fd := syscall.Stdin // os.Stdin.Fd would put a non-blocking terminal into blocking mode
	// SIGTERM and SIGHUP put the terminal back and then end the process as they would have, without waiting for the
	// command that is running. One the parent had this process ignore, as nohup does, stays ignored. They are caught
	// before the terminal is changed, so there is no moment when one would leave it changed.
	ended := make(chan os.Signal, 1)
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			signal.Notify(ended, sig)
		}
	}
	// mu is held while the terminal's settings change. Putting them back for good takes it and keeps it, so nothing
	// changes them after.
	var mu sync.Mutex
	mu.Lock()
	cooked, err := term.MakeRaw(fd)
	if err != nil {
		signal.Stop(ended)
		return err
	}
	// The prompt reads the terminal through its own duplicate of the descriptor in non-blocking mode, which Go's
	// poller manages, so closing it ends a read under way. The mode belongs to the terminal, which the program that
	// started this one shares, so the mode it had is put back, as are the terminal's line settings.
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		_ = term.Restore(fd, cooked)
		signal.Stop(ended)
		return err
	}
	err = syscall.SetNonblock(fd, true)
	restore := func() {
		mu.Lock()
		_ = syscall.SetNonblock(fd, flags&syscall.O_NONBLOCK != 0)
		_ = term.Restore(fd, cooked)
	}
	mu.Unlock()
	defer func() {
		restore()
		signal.Stop(ended)
	}()
	if err != nil {
		return err
	}
	go func() {
		sig := (<-ended).(syscall.Signal)
		restore()
		signal.Reset(syscall.SIGTERM, syscall.SIGHUP)
		_ = syscall.Kill(os.Getpid(), sig)
	}()
	input, err := openTerminal(fd)
	if err != nil {
		return err
	}
	defer input.Close() // which ends the prompt's read of the terminal

	// In raw mode Ctrl-C is a byte the prompt reads; SIGINT, sent by another process, interrupts as it does.
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	defer signal.Stop(interrupted)
	interrupt := make(chan struct{})
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)
	size := make(chan [2]int, 1)
	if w, h, err := term.GetSize(fd); err == nil {
		size <- [2]int{w, h}
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-interrupted:
				select {
				case interrupt <- struct{}{}:
				case <-done:
					return
				}
			case <-resized:
				if w, h, err := term.GetSize(fd); err == nil {
					select {
					case size <- [2]int{w, h}:
					case <-done:
						return
					}
				}
			case <-done:
				return
			}
		}
	}()
	return c.Prompt(ctx, struct {
		io.Reader
		io.Writer
	}{input, os.Stdout}, workDir, interrupt, size)
}

// openTerminal is a new descriptor for the terminal fd, as a file Go's poller manages, since fd is in non-blocking mode.
func openTerminal(fd int) (*os.File, error) {
	d, err := syscall.Dup(fd)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(d)
	return os.NewFile(uintptr(d), "/dev/stdin"), nil
}

// candidates is what could complete word, the last word of before, at the prompt: the computer's own completion.
func candidates(ctx context.Context, c *computer.Computer, cwd, before, _ string) []string {
	return c.Complete(ctx, cwd, before)
}
