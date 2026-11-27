package computer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Lakeshore-Labs/gbash"
	"golang.org/x/term"
)

// Prompt is the computer's shell for a person at a terminal: a line editor with history and Tab completion, which
// gbash's own interactive mode does not have. Each line is run in the computer; the working directory and exported
// variables carry from one to the next, and are this prompt's own, as a shell's are: two prompts on one computer
// do not see each other's, and Exec does not see theirs. It returns at the end of term's input, at Ctrl-D on an
// empty line, at exit, or when ctx ends, and ends the command that is running.
//
// term is a terminal in raw mode, or what stands for one, such as a WebSocket to a terminal in a browser: Prompt
// reads keystrokes from it and writes output for a terminal to it, a newline as "\r\n". While a command runs, what
// is typed is echoed and given to the command a line at a time, as a terminal's line discipline does, and Ctrl-C
// ends the command and its read of the terminal. Ctrl-C at the prompt drops the line. A value on interrupt ends
// the running command as Ctrl-C does, and a value on size, columns and rows, is the terminal's new size; either
// may be nil. Prompt reads term from one goroutine until it returns, and that read may outlast it: the caller
// closes term, or stops giving it input, to end the read.
//
// After a command ends, Prompt writes "\x1b]133;D;code\x07", the mark of shell integration in terminals (OSC 133)
// that a command has ended with its exit code: 130 for one interrupted.
func (c *Computer) Prompt(ctx context.Context, term io.ReadWriter, workDir string, interrupt <-chan struct{}, size <-chan [2]int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := &lockedWriter{w: term}
	keys := make(chan []byte)
	go func() { // what is typed, read ahead by one goroutine for as long as the prompt runs
		defer close(keys)
		for {
			buf := make([]byte, 4096)
			n, err := term.Read(buf)
			if n > 0 {
				select {
				case keys <- buf[:n]:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	in := &keyReader{keys: keys, done: ctx.Done()}
	p := &prompt{c: c, ctx: ctx, cwd: workDir, env: map[string]string{}, in: in, out: out, width: 80, height: 24}
	p.newEditor(nil)

	var mu sync.Mutex
	var stop func() // ends the command that is running, if one is
	go func() {
		for {
			select {
			case <-interrupt:
				mu.Lock()
				if stop != nil {
					stop()
				}
				mu.Unlock()
			case s, ok := <-size:
				if !ok {
					size = nil
					continue
				}
				p.resize(s[0], s[1])
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		p.mu.Lock()
		t := p.t
		p.mu.Unlock()
		t.SetPrompt("\x1b[36m" + p.cwd + "\x1b[0m $ ")
		in.interrupted = false
		line, err := t.ReadLine()
		if err != nil && in.interrupted { // Ctrl-C at the prompt
			io.WriteString(out, "^C\r\n")
			p.newEditor(t.History)
			continue
		}
		if err != nil || ctx.Err() != nil { // the end of input
			return nil
		}
		if strings.TrimSpace(line) == "" {
			continue
		}

		running, cancelRun := context.WithCancel(ctx)
		stdin := newTyped(out)
		mu.Lock()
		stop = func() { cancelRun(); stdin.interrupt() }
		mu.Unlock()
		done := make(chan struct{})
		var res *gbash.ExecutionResult
		go func() {
			defer close(done)
			lines := &newlines{w: out}
			// An Env makes Exec carry nothing of its own: the prompt carries its variables itself.
			res, err = c.Exec(running, &gbash.ExecutionRequest{Script: line, WorkDir: p.cwd, Env: p.env, Stdin: stdin, Stdout: lines, Stderr: lines})
		}()
		typed, ended := in.pending, false
		in.pending = nil
		for !ended {
			if len(typed) > 0 && stdin.keys(typed) { // Ctrl-C
				io.WriteString(out, "^C\r\n")
				mu.Lock()
				stop()
				mu.Unlock()
			}
			select {
			case <-done:
				ended = true
			case b, ok := <-keys:
				if !ok { // the end of input ends what is running
					keys = nil
					cancel()
				}
				typed = b
			}
		}
		mu.Lock()
		stop = nil
		mu.Unlock()
		in.pending = append(stdin.close(), in.pending...) // what the command did not read is the prompt's, as a terminal keeps it
		interrupted := running.Err() != nil
		cancelRun()
		if ctx.Err() != nil {
			return nil
		}
		code := 1
		switch {
		case interrupted:
			code = 130
		case res != nil:
			code = res.ExitCode
		}
		fmt.Fprintf(out, "\x1b]133;D;%d\x07", code)
		if res != nil && res.FinalEnv != nil {
			p.env = res.FinalEnv
		}
		switch {
		case err != nil:
			if !interrupted {
				fmt.Fprintf(t, "computer: %v\n", err)
			}
		case res.ShellExited && !interrupted:
			return nil
		case res.FinalEnv["PWD"] != "":
			p.cwd = res.FinalEnv["PWD"]
		}
	}
}

// prompt is one Prompt's line editor and what it completes from.
type prompt struct {
	c   *Computer
	ctx context.Context
	cwd string
	env map[string]string // the exported variables, as the last line left them
	in  *keyReader
	out io.Writer

	mu            sync.Mutex // guards t, width and height, which a resize changes
	t             *term.Terminal
	width, height int
}

// newEditor starts the line editor again, with the history it had, after Ctrl-C at the prompt: x/term ends
// ReadLine at Ctrl-C with the line left in it.
func (p *prompt) newEditor(history term.History) {
	t := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{p.in, p.out}, "")
	if history != nil {
		t.History = history
	}
	t.AutoCompleteCallback = p.complete(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = t.SetSize(p.width, p.height)
	p.t = t
}

func (p *prompt) resize(width, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if width > 0 && height > 0 {
		p.width, p.height = width, height
		_ = p.t.SetSize(width, height)
	}
}

func (p *prompt) complete(t *term.Terminal) func(string, int, rune) (string, int, bool) {
	return func(line string, pos int, key rune) (string, int, bool) {
		if key != '\t' {
			return "", 0, false
		}
		before := line[:pos]
		word := before[strings.LastIndexAny(before, " \t")+1:]
		found := p.c.Complete(p.ctx, p.cwd, before)
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
}

// keyReader is what is typed, as the line editor reads it. A read ends at Enter, so that what is typed after a line
// goes to its command, and at Ctrl-C, which it notes, so that the editor's end at Ctrl-C is told from the end of
// input.
type keyReader struct {
	keys        <-chan []byte
	done        <-chan struct{}
	pending     []byte
	interrupted bool
}

func (k *keyReader) Read(p []byte) (int, error) {
	for len(k.pending) == 0 {
		select {
		case b, ok := <-k.keys:
			if !ok {
				return 0, io.EOF
			}
			k.pending = b
		case <-k.done:
			return 0, io.EOF
		}
	}
	until := k.pending
	if i := bytes.IndexAny(until, "\x03\r\n"); i >= 0 {
		if until[i] == '\r' && i+1 < len(until) && until[i+1] == '\n' {
			i++ // the editor takes CRLF as one Enter
		}
		until = until[:i+1]
	}
	n := copy(p, until)
	k.interrupted = k.interrupted || n > 0 && p[n-1] == 0x03
	k.pending = k.pending[n:]
	return n, nil
}

// typed is a command's standard input from the terminal: what is typed, a line at a time, echoed as it is typed,
// with a backspace taking back a character and Ctrl-D ending the input, as a terminal's line discipline does in
// its canonical mode. It is gbash's StdinReader, so a read ends at a deadline, as the read builtin expects.
type typed struct {
	echo io.Writer

	mu       sync.Mutex
	line     []byte // being typed
	ready    []byte // typed, and not yet read
	eof      bool   // Ctrl-D: the next read that finds nothing ready is the end of input
	closed   bool
	deadline time.Time
	changed  chan struct{} // closed, and made again, when any of the above changes
}

func newTyped(echo io.Writer) *typed { return &typed{echo: echo, changed: make(chan struct{})} }

// notify wakes a read waiting for a change; s.mu is held.
func (s *typed) notify() {
	close(s.changed)
	s.changed = make(chan struct{})
}

// keys takes what was typed, and reports whether it held Ctrl-C, which drops what has not been read, and the rest.
func (s *typed) keys(b []byte) (interrupted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var echo []byte
	defer func() { _, _ = s.echo.Write(echo) }()
	for _, k := range b {
		switch k {
		case 0x03:
			s.line, s.ready = nil, nil
			return true
		case '\r', '\n':
			s.ready = append(append(s.ready, s.line...), '\n')
			s.line = nil
			echo = append(echo, '\r', '\n')
		case 0x04:
			s.ready = append(s.ready, s.line...)
			s.eof = len(s.line) == 0
			s.line = nil
		case 0x7f, 0x08:
			if len(s.line) > 0 {
				_, n := utf8.DecodeLastRune(s.line)
				s.line = s.line[:len(s.line)-n]
				echo = append(echo, '\b', ' ', '\b')
			}
			continue
		default:
			s.line = append(s.line, k)
			if k >= 0x20 || k == '\t' {
				echo = append(echo, k)
			}
			continue
		}
		s.notify()
	}
	return false
}

func (s *typed) Read(p []byte) (int, error) {
	for {
		s.mu.Lock()
		switch {
		case len(s.ready) > 0:
			n := copy(p, s.ready)
			s.ready = s.ready[n:]
			s.mu.Unlock()
			return n, nil
		case s.eof:
			s.eof = false
			s.mu.Unlock()
			return 0, io.EOF
		case s.closed:
			s.mu.Unlock()
			return 0, os.ErrClosed
		}
		deadline, changed := s.deadline, s.changed
		s.mu.Unlock()
		if deadline.IsZero() {
			<-changed
			continue
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			return 0, os.ErrDeadlineExceeded
		}
		timer := time.NewTimer(wait)
		select {
		case <-changed:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (s *typed) SetReadDeadline(d time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deadline = d
	s.notify()
	return nil
}

// interrupt ends a read under way, as Ctrl-C does, without taking input.
func (s *typed) interrupt() { _ = s.SetReadDeadline(time.Now()) }

// Close does nothing: gbash closes a command's standard input when it is done with it, and the prompt closes
// this one itself, once the command has ended, which ends a read a program left behind.
func (s *typed) Close() error { return nil }

// close ends the input, and any read under way, and returns what was typed and not read, as it was typed.
func (s *typed) close() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.notify()
	return bytes.ReplaceAll(append(s.ready, s.line...), []byte("\n"), []byte("\r"))
}

// lockedWriter is a terminal written from more than one goroutine: the line editor, a command's output and the echo
// of what is typed.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// newlines is a command's output to a terminal in raw mode, with each newline a carriage return and a newline, as a
// terminal's own output processing makes it.
type newlines struct{ w io.Writer }

func (n *newlines) Write(p []byte) (int, error) {
	if _, err := n.w.Write(bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Complete is what could complete the last word of before, a line typed in the shell at cwd: a command when it is
// the first word, a tool or one of its arguments after a command that calls tools, and otherwise a file. Only such a
// command is run, to list its tools; completing a line runs nothing else of it.
func (c *Computer) Complete(ctx context.Context, cwd, before string) []string {
	word := before[strings.LastIndexAny(before, " \t")+1:]
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

// quote is word as one word of a shell command, whatever it holds.
func quote(word string) string { return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'" }

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
