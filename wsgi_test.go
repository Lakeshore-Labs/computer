package computer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWSGIHandler(t *testing.T) {
	const app = `
import sys

def broken_response():
    yield b"partial response"
    raise RuntimeError("broken WSGI response")

request_count = 0

def application(env, start_response):
    global request_count
    request_count += 1
    if env["PATH_INFO"] == "/hang":
        while True:
            pass
    start_response("201 Created", [
        ("X-Request-Count", str(request_count)),
        ("Content-Type", "application/octet-stream"),
        ("Set-Cookie", "a=1"),
        ("Set-Cookie", "b=2"),
    ])
    if env["PATH_INFO"] == "/broken":
        return broken_response()
    if env["PATH_INFO"] != "/echo":
        return [b"ok"]
    assert "HTTP_X_HOP" not in env
    metadata = [sys.platform, env["REQUEST_METHOD"], env["PATH_INFO"],
                env["QUERY_STRING"], env["HTTP_X_DEMO"], env["wsgi.url_scheme"],
                env["SERVER_PORT"]]
    return [" ".join(metadata).encode(), env["wsgi.input"].read(int(env["CONTENT_LENGTH"]))]
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte(app), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{
		Mounts:  []Mount{{Host: dir, Guest: "/app", ReadOnly: true}},
		WorkDir: "/app",
		Python:  python(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	handler, err := c.WSGIHandler("app:application", 0)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://localhost/echo?x=1", strings.NewReader("\x00\xff"))
	req.Header.Set("X-Demo", "yes")
	req.Header.Set("Connection", "X-Hop")
	req.Header.Set("X-Hop", "skip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Request-Count") != "1" || rec.Body.String() != "wasi POST /echo x=1 yes https 443\x00\xff" {
		t.Fatalf("response: %d %v %q", rec.Code, rec.Header(), rec.Body.Bytes())
	}
	if cookies := rec.Header().Values("Set-Cookie"); len(cookies) != 2 || cookies[0] != "a=1" || cookies[1] != "b=2" {
		t.Fatalf("response cookies: %v", cookies)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://localhost/broken", nil))
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "partial response") {
		t.Fatalf("partial response: %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://localhost/state", nil))
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Request-Count") != "3" {
		t.Fatalf("worker did not retain state: %d %v", rec.Code, rec.Header())
	}

	// The WSGI worker has a separate session from the computer's shell.
	if got := run(t, c, "echo working"); got != "working" {
		t.Fatalf("shell response: %q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "https://localhost/hang", nil))
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("timed-out response: %d %q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://localhost/state", nil))
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Request-Count") != "1" {
		t.Fatalf("worker did not restart: %d %v", rec.Code, rec.Header())
	}
}
