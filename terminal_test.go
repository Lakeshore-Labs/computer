package computer

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialTerminal is a WebSocket to a TerminalHandler serving c at /home/user, and what it is sent, as a screen.
func dialTerminal(t *testing.T, c *Computer) (*websocket.Conn, *screen) {
	t.Helper()
	srv := httptest.NewServer(TerminalHandler(func(*http.Request) (*Computer, string, error) { return c, "/home/user", nil }))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	s := &screen{}
	go func() {
		for {
			kind, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.BinaryMessage {
				s.Write(msg)
			}
		}
	}()
	s.expect(t, "/home/user\x1b[0m $ ")
	return conn, s
}

func send(t *testing.T, conn *websocket.Conn, keys string) {
	t.Helper()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(keys)); err != nil {
		t.Fatal(err)
	}
}

func TestTheTerminalRunsLinesCompletesAndResizesOverAWebSocket(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes-for-tab.txt"), []byte("tabbed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn, s := dialTerminal(t, newComputerAt(t, dir))
	send(t, conn, "mkdir d; cd d; export GREETING=hi\r")
	s.expect(t, "/home/user/d\x1b[0m $ ")
	send(t, conn, "echo $GREETING $(pwd)\r")
	s.expect(t, "\r\nhi /home/user/d\r\n")
	s.expect(t, "$ ")
	send(t, conn, "cat ../note\t")
	s.expect(t, "s-for-tab.txt ")
	send(t, conn, "\r")
	s.expect(t, "\r\ntabbed\r\n")
	s.expect(t, "$ ")

	// The line editor wraps a line at the width it is told: the prompt "/home/user/d $ " is 15 columns of 20.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"cols":20,"rows":10}`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	send(t, conn, "echo 1234567890")
	s.expect(t, "echo \r\n1234567890")
}

func TestClosingTheTerminalEndsTheRunningCommand(t *testing.T) {
	dir := t.TempDir()
	conn, s := dialTerminal(t, newComputerAt(t, dir))
	send(t, conn, "echo sleeping; sleep 2; echo late > late.txt\r")
	s.expect(t, "\r\nsleeping\r\n")
	_ = conn.Close()
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(filepath.Join(dir, "late.txt")); err == nil {
		t.Error("the command went on after the terminal closed")
	}
}

func TestTheHostsErrorRefusesTheTerminal(t *testing.T) {
	for err, status := range map[error]int{fs.ErrPermission: http.StatusForbidden, errors.New("broken"): http.StatusInternalServerError} {
		srv := httptest.NewServer(TerminalHandler(func(*http.Request) (*Computer, string, error) { return nil, "", err }))
		_, res, dialErr := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		srv.Close()
		if dialErr == nil || res == nil || res.StatusCode != status {
			t.Errorf("%v: %v, %+v", err, dialErr, res)
		}
	}
}

func TestTheTerminalRefusesAnotherSitesPage(t *testing.T) {
	c := newComputerAt(t, t.TempDir())
	srv := httptest.NewServer(TerminalHandler(func(*http.Request) (*Computer, string, error) { return c, "/home/user", nil }))
	defer srv.Close()
	_, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": {"https://elsewhere.example"}})
	if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("another site's page was given the terminal: %v, %+v", err, res)
	}
}

func newComputerAt(t *testing.T, dir string) *Computer {
	t.Helper()
	c, err := New(Config{Mounts: []Mount{{Guest: "/home/user", Host: dir}}, WorkDir: "/home/user"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
