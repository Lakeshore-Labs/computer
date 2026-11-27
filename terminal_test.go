package computer

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// The host's error refuses the terminal: 403 for fs.ErrPermission, and 500 for another, which does not tell the
// client what the host's error was.
func TestTheHostsErrorRefusesTheTerminal(t *testing.T) {
	for err, status := range map[error]int{fs.ErrPermission: http.StatusForbidden, errors.New("the host's secret"): http.StatusInternalServerError} {
		srv := httptest.NewServer(TerminalHandler(func(*http.Request) (*Computer, string, error) { return nil, "", err }))
		_, res, dialErr := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		srv.Close()
		if dialErr == nil || res == nil || res.StatusCode != status {
			t.Fatalf("%v: %v, %+v", err, dialErr, res)
		}
		if body, _ := io.ReadAll(res.Body); strings.Contains(string(body), "secret") {
			t.Errorf("%v: the response tells the host's error: %q", err, body)
		}
	}
}

// A request that is not a WebSocket upgrade, or comes from another site's page, is refused before the host is asked
// for a computer, which it might make for the person the request's cookie names.
func TestTheTerminalRefusesARequestBeforeAskingTheHost(t *testing.T) {
	c := newComputerAt(t, t.TempDir())
	var asked atomic.Int32
	srv := httptest.NewServer(TerminalHandler(func(*http.Request) (*Computer, string, error) { asked.Add(1); return c, "/home/user", nil }))
	defer srv.Close()
	_, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": {"https://elsewhere.example"}})
	if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("another site's page was given the terminal: %v, %+v", err, res)
	}
	plain, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	plain.Body.Close()
	if plain.StatusCode != http.StatusBadRequest {
		t.Errorf("a plain GET: %d", plain.StatusCode)
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("the host was asked %d times", n)
	}
}

// A host whose pages are on another origin, such as a development server's, says which it accepts.
func TestAHostMayAcceptAnotherOrigin(t *testing.T) {
	c := newComputerAt(t, t.TempDir())
	srv := httptest.NewServer(TerminalHandler(func(*http.Request) (*Computer, string, error) { return c, "/home/user", nil },
		CheckOrigin(func(r *http.Request) bool { return r.Header.Get("Origin") == "http://localhost:5173" })))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": {"http://localhost:5173"}})
	if err != nil {
		t.Fatalf("the origin the host accepts: %v", err)
	}
	conn.Close()
	if _, res, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": {"https://elsewhere.example"}}); err == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("another origin: %v", err)
	}
}

// quickKeepAlive makes the handler's keep-alive and write deadline short, for the test.
func quickKeepAlive(t *testing.T) {
	saved := [3]time.Duration{pongWait, pingPeriod, writeWait}
	pongWait, pingPeriod, writeWait = 600*time.Millisecond, 200*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { pongWait, pingPeriod, writeWait = saved[0], saved[1], saved[2] })
}

// servedUntil is a TerminalHandler serving c at /home/user, and a channel closed when its request ends.
func servedUntil(t *testing.T, c *Computer) (url string, ended chan struct{}) {
	ended = make(chan struct{})
	h := TerminalHandler(func(*http.Request) (*Computer, string, error) { return c, "/home/user", nil })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(ended)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), ended
}

// A client that is gone without closing, and so answers no ping, ends the prompt and the command it was running.
func TestAClientThatStopsAnsweringEndsThePrompt(t *testing.T) {
	quickKeepAlive(t)
	dir := t.TempDir()
	url, ended := servedUntil(t, newComputerAt(t, dir))
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetPingHandler(func(string) error { return nil }) // it reads, and answers nothing
	s := &screen{}
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			s.Write(msg)
		}
	}()
	s.expect(t, "$ ")
	send(t, conn, "echo sleeping; sleep 3; echo late > late.txt\r")
	s.expect(t, "\r\nsleeping\r\n")
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the prompt went on for a client that answers no ping")
	}
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(filepath.Join(dir, "late.txt")); err == nil {
		t.Error("the command went on after its client was gone")
	}
}

// A client that stops reading, while a command writes more than the connection holds, ends the prompt and the
// command: a write waits only so long, well before a ping would find the client gone.
func TestAClientThatStopsReadingEndsThePrompt(t *testing.T) {
	quickKeepAlive(t)
	pongWait, pingPeriod = 30*time.Second, 20*time.Second
	url, ended := servedUntil(t, newComputerAt(t, t.TempDir()))
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	send(t, conn, "for i in $(seq 1 200000); do echo xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; done\r")
	select { // it reads nothing at all
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt went on writing to a client that reads nothing")
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
