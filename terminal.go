package computer

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// The terminal's keep-alive, gorilla/websocket's own pattern: a ping every pingPeriod, a client that answers none
// within pongWait is gone, and a write that takes longer than writeWait is a client that stopped reading. Variables
// so that a test can make them short.
var (
	pongWait   = 60 * time.Second
	pingPeriod = pongWait * 9 / 10
	writeWait  = 10 * time.Second
)

// A TerminalOption changes how TerminalHandler serves.
type TerminalOption func(*terminalOptions)

type terminalOptions struct{ checkOrigin func(*http.Request) bool }

// CheckOrigin is the rule for which pages may open the terminal, for a host whose pages are on another origin than
// its own, such as a development server's, or behind a proxy that changes the Host header. The rule is the host's:
// the terminal is the person a cookie names, which a page of any site may send. Without this option a page of the
// request's own origin alone may.
func CheckOrigin(check func(*http.Request) bool) TerminalOption {
	return func(o *terminalOptions) { o.checkOrigin = check }
}

// TerminalHandler serves a computer's Prompt to a terminal in a browser, over a WebSocket. get is the host's: it
// returns the requester's computer and the working directory to start in, after the host's own checks of who is
// asking; an error that is fs.ErrPermission refuses the request with 403, and another with 500, which is logged and
// not told to the client. The computer stays the host's, to close. A request that is not a WebSocket upgrade, or
// whose page the origin rule refuses (the request's own origin alone, unless CheckOrigin says otherwise), is
// refused before get is called.
//
// Binary messages carry what is typed in, and the terminal's output out. A text message {"cols": 80, "rows": 24}
// is the terminal's size. Closing the socket ends the prompt and the command that is running, as does a client that
// answers no ping for a minute or takes more than ten seconds to take a message; the prompt's end, at exit or
// Ctrl-D, closes the socket.
func TerminalHandler(get func(*http.Request) (*Computer, string, error), opts ...TerminalOption) http.Handler {
	o := terminalOptions{checkOrigin: sameOrigin}
	pongWait, pingPeriod, writeWait := pongWait, pingPeriod, writeWait // as they are when the handler is made
	for _, opt := range opts {
		opt(&o)
	}
	upgrader := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: o.checkOrigin}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case !websocket.IsWebSocketUpgrade(r):
			http.Error(w, "the terminal is a WebSocket", http.StatusBadRequest)
			return
		case !o.checkOrigin(r):
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		c, workDir, err := get(r)
		switch {
		case errors.Is(err, fs.ErrPermission):
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		case err != nil:
			log.Printf("computer: the terminal of %s: %v", r.URL.Path, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil) // which answers a request it refuses
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(1 << 20)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })

		keys, typed := io.Pipe()
		size := make(chan [2]int, 1)
		go func() { // the socket's messages, until it closes or its client is gone
			defer typed.Close()
			for {
				kind, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if kind == websocket.BinaryMessage {
					if _, err := typed.Write(msg); err != nil {
						return
					}
					continue
				}
				var s struct{ Cols, Rows int }
				if json.Unmarshal(msg, &s) == nil && s.Cols > 0 && s.Rows > 0 {
					select { // only the latest size matters
					case <-size:
					default:
					}
					size <- [2]int{s.Cols, s.Rows}
				}
			}
		}()
		out := &socketWriter{conn: conn, wait: writeWait}
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(pingPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
						_ = conn.Close() // which ends the read above, and so the prompt
						return
					}
				case <-done:
					return
				}
			}
		}()
		_ = c.Prompt(r.Context(), struct {
			io.Reader
			io.Writer
		}{keys, out}, workDir, nil, size)
		_ = keys.Close() // ends a write of what is typed that the prompt will not read
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	})
}

// sameOrigin is gorilla/websocket's own rule: a request with no Origin, which no browser's page sends, or whose
// Origin is the request's host.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

// socketWriter is the terminal's output, a binary message for each write. A connection has one writer at a time,
// and a write that its client does not take in wait closes the connection, which ends the prompt.
type socketWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
	wait time.Duration
}

func (s *socketWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(s.wait))
	if err := s.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		_ = s.conn.Close()
		return 0, err
	}
	return len(p), nil
}
