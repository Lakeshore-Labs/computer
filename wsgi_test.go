package computer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSGIHandler(t *testing.T) {
	const app = `
import sys

def broken_response():
    yield b"partial response"
    raise RuntimeError("broken WSGI response")

def application(env, start_response):
    start_response("201 Created", [
        ("Content-Type", "application/octet-stream"),
        ("Set-Cookie", "a=1"),
        ("Set-Cookie", "b=2"),
    ])
    if env["PATH_INFO"] == "/broken":
        return broken_response()
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
	if rec.Code != http.StatusCreated || rec.Body.String() != "wasi POST /echo x=1 yes https 443\x00\xff" {
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
}
