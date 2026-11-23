package main

import (
	"bufio"
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

	"github.com/ewhauser/gbash"
	"golang.org/x/term"

	"github.com/Lakeshore-Labs/computer"
)

// prompt is the computer's shell for a person at a terminal: a line editor with history and Tab completion, which
// gbash's own interactive mode does not have. Each line is run in the computer; the working directory and exported
// variables carry from one to the next. A program that serves a terminal to a browser, such as gotty or ttyd, makes
// this the computer's web terminal.
func prompt(ctx context.Context, c *computer.Computer, workDir string) error {
	fd := int(os.Stdin.Fd())
	cooked, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer func() { _ = term.Restore(fd, cooked) }()
	input := &terminalInput{terminalDescriptor: os.Stdin, reader: bufio.NewReader(os.Stdin)}
	t := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{commandInput{input, ctx}, os.Stdout}, "")
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
		running, stop := signal.NotifyContext(ctx, os.Interrupt)
		_ = term.Restore(fd, cooked)
		res, err := c.Exec(running, &gbash.ExecutionRequest{Script: line, Stdin: commandInput{input, running}, Stdout: os.Stdout, Stderr: os.Stderr})
		interrupted := running.Err() != nil
		_, rawErr := term.MakeRaw(fd)
		stop()
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

// An interrupted terminal read can still receive the next line. Both the command and
// the prompt use the same pending read so those bytes reach whoever reads next.
type terminalInput struct {
	terminalDescriptor
	mu     sync.Mutex
	reader *bufio.Reader
	ready  chan error
}

// Forward terminal metadata without exposing os.File.WriteTo, which bypasses Read during io.Copy.
type terminalDescriptor interface {
	Fd() uintptr
	Stat() (os.FileInfo, error)
	SetReadDeadline(time.Time) error
}

type commandInput struct {
	*terminalInput
	ctx context.Context
}

func (in commandInput) Close() error { return nil } // the prompt owns the terminal

func (in commandInput) Read(p []byte) (int, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if err := in.ctx.Err(); err != nil || len(p) == 0 {
		return 0, err
	}
	if in.ready == nil {
		if in.ctx.Done() == nil || in.reader.Buffered() > 0 {
			return in.reader.Read(p)
		}
		in.ready = make(chan error, 1)
		go func(ready chan<- error) {
			_, err := in.reader.Peek(1)
			ready <- err
		}(in.ready)
	}
	select {
	case <-in.ctx.Done():
		return 0, in.ctx.Err()
	case err := <-in.ready:
		in.ready = nil
		if err != nil {
			return 0, err
		}
		if err := in.ctx.Err(); err != nil {
			return 0, err
		}
		return in.reader.Read(p)
	}
}

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
		// Only such a command is run: any other, such as agent, would do its work with --help as its argument.
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
