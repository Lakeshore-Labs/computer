package computers

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Lakeshore-Labs/computer"
)

type sessionKey struct{}

func inSession(id string) context.Context {
	return context.WithValue(context.Background(), sessionKey{}, id)
}

func sessionOf(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(sessionKey{}).(string)
	return id, ok
}

// pool is a Computers whose sessions each mount a directory of their own at /home/user, and the sessions it made.
func pool(t *testing.T, idle time.Duration) (*Computers, *[]string) {
	t.Helper()
	var made []string
	cs := New(sessionOf, func(_ context.Context, session string) (computer.Config, error) {
		made = append(made, session)
		return computer.Config{Mounts: []computer.Mount{{Guest: "/home/user", Host: t.TempDir()}}, WorkDir: "/home/user"}, nil
	}, idle)
	t.Cleanup(func() { _ = cs.Close() })
	return cs, &made
}

func run(t *testing.T, cs *Computers, session, script string) string {
	t.Helper()
	var out bytes.Buffer
	code, err := cs.Run(inSession(session), script, "/home/user", nil, false, &out, &out)
	if err != nil || code != 0 {
		t.Fatalf("%q: %d, %v, %q", script, code, err, out.String())
	}
	return strings.TrimSpace(out.String())
}

func TestEachSessionHasItsOwnComputer(t *testing.T) {
	cs, made := pool(t, 0)
	run(t, cs, "a", "echo one > mine")
	run(t, cs, "b", "echo two > mine; echo x > onlyb")
	if got := run(t, cs, "a", "cat mine; ls"); got != "one\nmine" {
		t.Errorf("a sees %q", got)
	}
	if got := run(t, cs, "b", "cat mine; ls"); got != "two\nmine\nonlyb" {
		t.Errorf("b sees %q", got)
	}
	if len(*made) != 2 {
		t.Errorf("made computers for %v, want one each for a and b", *made)
	}
}

func TestACallNamingNoSessionFails(t *testing.T) {
	cs, made := pool(t, 0)
	if _, err := cs.Read(context.Background(), "/home/user/x", 0, -1); err != errNoSession {
		t.Errorf("got %v, want %v", err, errNoSession)
	}
	if len(*made) != 0 {
		t.Errorf("made computers for %v", *made)
	}
}

func TestAnIdleComputerIsClosedAndTheNextCallMakesANewOne(t *testing.T) {
	cs, made := pool(t, 100*time.Millisecond)
	run(t, cs, "a", "echo kept > /tmp/scratch")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cs.mu.Lock()
		n := len(cs.open)
		cs.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := run(t, cs, "a", "ls /tmp"); got != "" || len(*made) != 2 {
		t.Errorf("after the idle time /tmp holds %q and %d computers were made, want it empty and 2", got, len(*made))
	}
}

func TestEndClosesOneSessionsComputer(t *testing.T) {
	cs, made := pool(t, 0)
	run(t, cs, "a", "echo kept > /tmp/scratch")
	run(t, cs, "b", "echo kept > /tmp/scratch")
	if err := cs.End("a"); err != nil {
		t.Fatal(err)
	}
	if err := cs.End("nobody"); err != nil {
		t.Errorf("ending a session with no computer: %v", err)
	}
	if got := run(t, cs, "b", "cat /tmp/scratch"); got != "kept" || len(*made) != 2 {
		t.Errorf("b sees %q after a ended, %d made", got, len(*made))
	}
	if got := run(t, cs, "a", "ls /tmp"); got != "" || len(*made) != 3 {
		t.Errorf("a sees %q after it ended, %d made, want it empty and 3", got, len(*made))
	}
}

func TestCloseClosesEveryComputer(t *testing.T) {
	cs, made := pool(t, 0)
	run(t, cs, "a", "echo kept > /tmp/scratch")
	run(t, cs, "b", "echo kept > /tmp/scratch")
	if err := cs.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cs.Close(); err != nil {
		t.Errorf("a second Close: %v", err)
	}
	// The computers it held are closed: a call after Close is not given one of them, with its /tmp.
	if got := run(t, cs, "a", "ls /tmp"); got != "" || len(*made) != 3 {
		t.Errorf("a sees %q after Close, %d made, want it empty and 3", got, len(*made))
	}
}
