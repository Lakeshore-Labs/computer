package computer

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// screen is what a terminal has been sent, read as it arrives.
type screen struct {
	mu   sync.Mutex
	got  bytes.Buffer
	seen int // how much of got an expect has passed
}

func (s *screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.got.Write(p)
}

// expect waits for marker after what an earlier expect matched, and returns what came before it.
func (s *screen) expect(t *testing.T, marker string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		rest := s.got.String()[s.seen:]
		if i := strings.Index(rest, marker); i >= 0 {
			s.seen += i + len(marker)
			s.mu.Unlock()
			return rest[:i]
		}
		s.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Fatalf("no %q in %q", marker, s.got.String()[s.seen:])
	return ""
}

// promptOverAPipe is a prompt in a computer whose home is dir, typed into with keys, and the channel its return is
// sent on.
func promptOverAPipe(t *testing.T, dir string) (keys *io.PipeWriter, s *screen, interrupt chan struct{}, done chan error) {
	t.Helper()
	return promptOn(t, newComputerAt(t, dir))
}

// promptOn is a prompt in c, as promptOverAPipe's.
func promptOn(t *testing.T, c *Computer) (keys *io.PipeWriter, s *screen, interrupt chan struct{}, done chan error) {
	t.Helper()
	in, keys := io.Pipe()
	s, interrupt, done = &screen{}, make(chan struct{}), make(chan error, 1)
	go func() {
		done <- c.Prompt(context.Background(), struct {
			io.Reader
			io.Writer
		}{in, s}, "/home/user", interrupt, nil)
	}()
	t.Cleanup(func() { _ = keys.Close(); <-done })
	s.expect(t, "/home/user\x1b[0m $ ")
	return keys, s, interrupt, done
}

func TestThePromptRunsALineAndPromptsAgain(t *testing.T) {
	keys, s, _, _ := promptOverAPipe(t, t.TempDir())
	io.WriteString(keys, "echo hello\r")
	s.expect(t, "\r\nhello\r\n")
	s.expect(t, "\x1b]133;D;0\x07") // the command's end, for a terminal that marks it
	s.expect(t, "$ ")
	io.WriteString(keys, "false\r")
	s.expect(t, "\x1b]133;D;1\x07")
}

func TestTheWorkingDirectoryAndExportedVariablesCarryToTheNextLine(t *testing.T) {
	keys, s, _, _ := promptOverAPipe(t, t.TempDir())
	io.WriteString(keys, "mkdir d; cd d; export GREETING=hi\r")
	s.expect(t, "/home/user/d\x1b[0m $ ")
	io.WriteString(keys, "echo $GREETING $(pwd)\r")
	s.expect(t, "\r\nhi /home/user/d\r\n")
}

func TestAnInterruptEndsTheRunningCommandAndThePromptReturns(t *testing.T) {
	keys, s, interrupt, _ := promptOverAPipe(t, t.TempDir())
	for name, stop := range map[string]func(){
		"Ctrl-C":      func() { io.WriteString(keys, "\x03") },
		"the channel": func() { interrupt <- struct{}{} },
	} {
		io.WriteString(keys, "echo sleeping; sleep 30\r")
		s.expect(t, "\r\nsleeping\r\n")
		start := time.Now()
		stop()
		s.expect(t, "\x1b]133;D;130\x07")
		s.expect(t, "$ ")
		if time.Since(start) > 5*time.Second {
			t.Errorf("%s: the prompt returned after %v", name, time.Since(start))
		}
	}
	// Ctrl-C at the prompt drops the line, as bash does, and the prompt goes on.
	io.WriteString(keys, "echo dropped\x03")
	s.expect(t, "$ ")
	io.WriteString(keys, "echo kept\r")
	s.expect(t, "\r\nkept\r\n")
	if s.mu.Lock(); strings.Contains(s.got.String(), "\r\ndropped\r\n") {
		t.Errorf("the dropped line ran: %q", s.got.String())
	}
	s.mu.Unlock()
}

func TestACommandReadsWhatIsTypedAndCtrlCEndsTheRead(t *testing.T) {
	keys, s, _, _ := promptOverAPipe(t, t.TempDir())
	io.WriteString(keys, "read -r line; echo got $line\r")
	io.WriteString(keys, "abcx\x7f\r") // a backspace takes back the x
	s.expect(t, "abcx\b \b\r\n")       // what is typed is echoed, as is the backspace
	s.expect(t, "got abc\r\n")
	io.WriteString(keys, "echo reading; read line; echo after\r")
	s.expect(t, "\r\nreading\r\n")
	io.WriteString(keys, "\x03")
	s.expect(t, "$ ")
	io.WriteString(keys, "echo resumed\r")
	s.expect(t, "\r\nresumed\r\n")
}

func TestTabCompletesAFileName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes-for-tab.txt"), []byte("tabbed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, s, _, _ := promptOverAPipe(t, dir)
	io.WriteString(keys, "cat note\t")
	s.expect(t, "s-for-tab.txt ")
	io.WriteString(keys, "\r")
	s.expect(t, "\r\ntabbed\r\n")
}

func TestTheEndOfInputEndsThePromptAndWhatIsRunning(t *testing.T) {
	keys, s, _, done := promptOverAPipe(t, t.TempDir())
	io.WriteString(keys, "echo sleeping; sleep 30\r")
	s.expect(t, "\r\nsleeping\r\n")
	_ = keys.Close()
	select {
	case err := <-done:
		done <- err // for the cleanup
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt went on after its input ended")
	}
}

// What is typed after a line, before its command reads it, is the command's; what the command leaves unread is the
// prompt's next line, as a terminal keeps it.
func TestTypedAheadGoesToTheCommandAndWhatItLeavesToThePrompt(t *testing.T) {
	keys, s, _, _ := promptOverAPipe(t, t.TempDir())
	io.WriteString(keys, "read -r x; echo got $x\rabc\r")
	s.expect(t, "\r\ngot abc\r\n")
	s.expect(t, "$ ")
	io.WriteString(keys, "sleep 0.5\recho after\r")
	s.expect(t, "\r\nafter\r\n")
}

// Ctrl-D ends a command's input: cat ends, and the prompt returns.
func TestCtrlDEndsACommandsInput(t *testing.T) {
	keys, s, _, _ := promptOverAPipe(t, t.TempDir())
	io.WriteString(keys, "cat\r")
	io.WriteString(keys, "hello\r")
	s.expect(t, "hello\r\nhello\r\n") // the echo, and cat's copy
	io.WriteString(keys, "\x04")
	s.expect(t, "\x1b]133;D;0\x07")
}

// An interrupt ends a command blocked reading the terminal, whichever way it reads: the read builtin, cat, and pr,
// which copies its input as Go's io.Copy does.
func TestAnInterruptEndsACommandReadingTheTerminal(t *testing.T) {
	keys, s, interrupt, _ := promptOverAPipe(t, t.TempDir())
	for _, command := range []string{"read line", "cat", "pr"} {
		for name, stop := range map[string]func(){
			"Ctrl-C":      func() { io.WriteString(keys, "\x03") },
			"the channel": func() { interrupt <- struct{}{} },
		} {
			io.WriteString(keys, "echo reading; "+command+"\r")
			s.expect(t, "\r\nreading\r\n")
			stop()
			s.expect(t, "\x1b]133;D;130\x07")
			s.expect(t, "$ ")
			t.Logf("%s ended by %s", command, name)
		}
	}
}

// Two prompts on one computer are two shells: each has its own working directory and its own exported variables.
func TestTwoPromptsOnOneComputerKeepTheirOwnVariables(t *testing.T) {
	c := newComputerAt(t, t.TempDir())
	one, first, _, _ := promptOn(t, c)
	two, second, _, _ := promptOn(t, c)
	io.WriteString(one, "export ONLY_HERE=one; mkdir -p d; cd d\r")
	first.expect(t, "/home/user/d\x1b[0m $ ")
	io.WriteString(two, "echo [$ONLY_HERE] $(pwd)\r")
	second.expect(t, "\r\n[] /home/user\r\n")
	io.WriteString(one, "echo [$ONLY_HERE] $(pwd)\r")
	first.expect(t, "\r\n[one] /home/user/d\r\n")
	if got := run(t, c, "echo [$ONLY_HERE]"); got != "[]" {
		t.Errorf("the computer's own Exec has the prompt's variable: %q", got)
	}
}
