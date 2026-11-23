package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Lakeshore-Labs/gbash"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/Lakeshore-Labs/computer"
)

// prompt is the computer's shell for a person at a terminal: a line editor with history and Tab completion, which
// gbash's own interactive mode does not have. Each line is run in the computer; the working directory and exported
// variables carry from one to the next. A program that serves a terminal to a browser, such as gotty or ttyd, makes
// this the computer's web terminal.
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
	// Each reader of the terminal below has its own duplicate of the descriptor in non-blocking mode, which Go's
	// poller manages, so gbash's read deadlines work and closing one ends a read under way without taking input.
	// The mode belongs to the terminal, which the program that started this one shares, so the mode it had is put
	// back, as are the terminal's line settings.
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
	t := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{input, os.Stdout}, "")
	resize := func() {
		if w, h, err := term.GetSize(fd); err == nil {
			_ = t.SetSize(w, h)
		}
	}
	resize()
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)
	go func() {
		for range resized {
			resize()
		}
	}()

	cwd := workDir
	t.AutoCompleteCallback = func(line string, pos int, key rune) (string, int, bool) {
		if key != '\t' {
			return "", 0, false
		}
		before := line[:pos]
		word := before[strings.LastIndexAny(before, " \t")+1:]
		found := candidates(ctx, c, cwd, before, word)
		switch shared := sharedPrefix(found); {
		case len(found) == 1:
			shared += " "
			fallthrough
		case len(shared) > len(word):
			return before[:len(before)-len(word)] + shared + line[pos:], pos - len(word) + len(shared), true
		case len(found) > 1:
			fmt.Fprintln(t, strings.Join(found, "  "))
		}
		return "", 0, false
	}
	for {
		t.SetPrompt("\x1b[36m" + cwd + "\x1b[0m $ ")
		line, err := t.ReadLine()
		if err != nil { // the end of input, or Ctrl-C at the prompt
			return nil
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		// The command has the terminal as it was, so its output is laid out as it expects, and Ctrl-C stops it.
		mu.Lock()
		_ = term.Restore(fd, cooked)
		mu.Unlock()
		running, stop := signal.NotifyContext(ctx, os.Interrupt)
		// The command reads the terminal through its own descriptor. Ctrl-C sets a deadline that ends a read under way,
		// as gbash's read builtin expects, and closing the descriptor when the command ends stops one a program left
		// behind, so neither takes what is typed next, which is the prompt's.
		in, err := openTerminal(fd)
		if err != nil {
			stop()
			return err
		}
		stopInterrupt := context.AfterFunc(running, func() { _ = in.SetReadDeadline(time.Now()) })
		res, err := c.Exec(running, &gbash.ExecutionRequest{Script: line, Stdin: terminal{in}, Stdout: os.Stdout, Stderr: os.Stderr})
		interrupted := running.Err() != nil
		stopInterrupt()
		_ = in.Close()
		stop()
		mu.Lock()
		_, rawErr := term.MakeRaw(fd)
		mu.Unlock()
		if rawErr != nil {
			return rawErr
		}
		switch {
		case err != nil:
			if !interrupted {
				fmt.Fprintf(t, "computer: %v\n", err)
			}
		case res.ShellExited && !interrupted:
			return nil
		case res.FinalEnv["PWD"] != "":
			cwd = res.FinalEnv["PWD"]
		}
	}
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

// terminal is a command's view of the terminal: gbash's StdinReader, and nothing else of the *os.File. gbash copies a
// reader without Close or SetReadDeadline through a goroutine that never stops reading. Close does nothing because
// the prompt closes the file itself, once the command has ended. os.File.Fd would put the terminal into blocking mode.
type terminal struct{ file *os.File }

func (t terminal) Read(p []byte) (int, error)        { return t.file.Read(p) }
func (t terminal) Close() error                      { return nil }
func (t terminal) SetReadDeadline(d time.Time) error { return t.file.SetReadDeadline(d) }

// quote is word as one word of a shell command, whatever it holds.
func quote(word string) string { return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'" }

// candidates is what could complete word, the last word of before: a command when it is the first word, a tool or
// one of its arguments after a command that calls tools, and otherwise a file.
func candidates(ctx context.Context, c *computer.Computer, cwd, before, word string) []string {
	// With its own environment, a completion leaves the shell's working directory and variables as they are.
	run := func(script string) []string {
		res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: script, WorkDir: cwd, Env: map[string]string{}})
		if err != nil || res.ExitCode != 0 {
			return nil
		}
		return strings.Split(strings.TrimSpace(res.Stdout), "\n")
	}
	var all []string
	switch words := strings.Fields(before[:len(before)-len(word)]); {
	case len(words) == 0:
		all = run("ls /usr/bin")
	case strings.HasPrefix(word, "-") || len(words) == 1:
		// A command that calls tools lists them, and a tool its arguments, in the first word of its help's
		// indented lines. Any other command's help is not read this way, and its arguments complete as files.
		// Only such a command is run: another might do its work with --help as its argument.
		if !slices.Contains(c.ToolCommands(), words[0]) {
			break
		}
		probe := quote(words[0])
		if len(words) > 1 {
			probe += " " + quote(words[1])
		}
		help := run(probe + " --help 2>/dev/null")
		if len(help) > 0 && strings.HasPrefix(help[0], "usage: "+words[0]+" <tool>") || len(words) > 1 && len(help) > 0 && strings.HasPrefix(help[0], words[0]+" "+words[1]+":") {
			for _, line := range help[1:] {
				if fields := strings.Fields(line); strings.HasPrefix(line, "  ") && len(fields) > 0 {
					all = append(all, fields[0])
				}
			}
		}
	}
	if all == nil {
		all = run("compgen -f -- " + quote(word))
	}
	var found []string
	for _, candidate := range all {
		if candidate != "" && strings.HasPrefix(candidate, word) {
			found = append(found, candidate)
		}
	}
	sort.Strings(found)
	return found
}

// sharedPrefix is the longest start all of words have in common.
func sharedPrefix(words []string) string {
	if len(words) == 0 {
		return ""
	}
	shared := words[0]
	for _, w := range words[1:] {
		for !strings.HasPrefix(w, shared) {
			shared = shared[:len(shared)-1]
		}
	}
	return shared
}
