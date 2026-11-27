package computer

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// TerminalHandler serves a computer's Prompt to a terminal in a browser, over a WebSocket. get is the host's: it
// returns the requester's computer and the working directory to start in, after the host's own checks of who is
// asking; an error that is fs.ErrPermission refuses the request with 403, and another with 500. The computer stays
// the host's, to close.
//
// Binary messages carry what is typed in, and the terminal's output out. A text message {"cols": 80, "rows": 24}
// is the terminal's size. Closing the socket ends the prompt and the command that is running, and the prompt's
// end, at exit or Ctrl-D, closes the socket. A page of another origin is refused, as gorilla/websocket refuses one
// whose Origin is not the request's host.
func TerminalHandler(get func(*http.Request) (*Computer, string, error)) http.Handler {
	upgrader := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, workDir, err := get(r)
		switch {
		case errors.Is(err, fs.ErrPermission):
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil) // which answers a request it refuses
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(1 << 20)

		keys, typed := io.Pipe()
		size := make(chan [2]int, 1)
		go func() { // the socket's messages, until it closes
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
		out := &socketWriter{conn: conn}
		_ = c.Prompt(r.Context(), struct {
			io.Reader
			io.Writer
		}{keys, out}, workDir, nil, size)
		_ = keys.Close() // ends a write of what is typed that the prompt will not read
		out.mu.Lock()
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		out.mu.Unlock()
	})
}

// socketWriter is the terminal's output, a binary message for each write. A connection has one writer at a time.
type socketWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (s *socketWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
