package computer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Lakeshore-Labs/gbash"
	gbfs "github.com/Lakeshore-Labs/gbash/fs"
	"github.com/Lakeshore-Labs/gbash/policy"
)

func TestCGIRunsAScriptForARequest(t *testing.T) {
	c := newComputer(t, Config{})
	h := &CGI{Computer: c, Root: "/x", Env: map[string]string{"GREETING": "hi"},
		Script: `printf 'Status: 201 Created\r\nContent-Type: text/plain\r\nSet-Cookie: a=1\r\nSet-Cookie: b=2\r\n\r\n'
echo "$GREETING $REQUEST_METHOD $SCRIPT_NAME $PATH_INFO $QUERY_STRING $HTTP_X_DEMO $CONTENT_LENGTH"; cat`}
	req := httptest.NewRequest(http.MethodPost, "/x/echo?q=1", strings.NewReader("body"))
	req.Header.Set("X-Demo", "yes")
	rec := httptest.NewRecorder()
	rec.Header().Add("Set-Cookie", "host=1")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Body.String() != "hi POST /x /echo q=1 yes 4\nbody" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) != 3 || got[0] != "host=1" {
		t.Errorf("cookies: %v", got)
	}

	h.Script = "exit 3" // a script that says nothing is the server's failure, not a page
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("a silent script: %d %q", rec.Code, rec.Body.String())
	}
}

func TestCGIEndsShortTheResponseOfAScriptThatFailsAfterItsHeaders(t *testing.T) {
	c := newComputer(t, Config{})
	h := &CGI{Computer: c, Stderr: io.Discard, Script: `printf 'Content-Type: text/plain\r\n\r\nhalf'; exit 1`}
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Error("a response cut short was ended as a whole one is")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestCGIRejectsAnInvalidResponse(t *testing.T) {
	c := newComputer(t, Config{})
	for _, headers := range []string{"Content-Type: text/plain\nStatus: 99 Bad", "Status: ", "Status: 200 OK", "X-Only: yes"} {
		h := &CGI{Computer: c, Script: "printf '%s\\n\\n' '" + headers + "'"}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%q: %d", headers, rec.Code)
		}
	}
}

func TestCGIMountAndRequestEnvironment(t *testing.T) {
	h := &CGI{Computer: newComputer(t, Config{}), Root: "/x/", Script: `printf 'Content-Type: text/plain\n\n'
echo "$SCRIPT_NAME $PATH_INFO $REMOTE_ADDR ${CONTENT_LENGTH-unset} ${HTTP_PROXY-unset}"`}
	req := httptest.NewRequest(http.MethodGet, "/prefix/x/a", nil)
	req.RemoteAddr = "192.0.2.1"
	req.Header["proxy"] = []string{"value"}
	rec := httptest.NewRecorder()
	http.StripPrefix("/prefix/", h).ServeHTTP(rec, req)
	if got := rec.Body.String(); got != "/prefix/x /a 192.0.2.1 unset unset\n" {
		t.Errorf("environment: %q", got)
	}
}

func TestCGICancellationInterruptsReadingTheRequest(t *testing.T) {
	h := &CGI{Computer: newComputer(t, Config{}), Script: "cat >/dev/null"}
	for _, length := range []int64{2, -1} {
		body, input := io.Pipe()
		ctx, cancel := context.WithCancel(t.Context())
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", body)
		req.ContentLength = length
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.ServeHTTP(httptest.NewRecorder(), req)
		}()
		_, err := io.WriteString(input, "a") // the reader is waiting for the rest
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Errorf("reading a body of length %d outlived the request", length)
		}
		input.Close()
		<-done
		if err != nil {
			t.Fatal(err)
		}
	}
}

func webRequest(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func TestWSGIServesAnApplicationByItsName(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	run(t, c, `mkdir hello && cat > hello/app.py <<'PY'
import sys

def app(env, start):
    print("a print is not the page")
    status = "200 OK" if env["PATH_INFO"] in ("/", "/echo") else "404 Not Found"
    start(status, [("Content-Type", "application/octet-stream")])
    if env["REQUEST_METHOD"] == "POST":
        return [env["wsgi.input"].read(int(env["CONTENT_LENGTH"]))]
    return [(sys.platform + " " + env["SCRIPT_NAME"]).encode()]
PY`)
	for _, name := range []string{"app.py", "app.:app", "app:api..app", "app.2:app"} {
		if _, err := WSGI(c, name, "hello"); err == nil {
			t.Errorf("accepted invalid WSGI name %q", name)
		}
	}
	h, err := WSGI(c, "app:app", "/home/user/hello")
	if err != nil {
		t.Fatal(err)
	}
	if rec := webRequest(h, "GET", "/", ""); rec.Code != 200 || rec.Body.String() != "wasi " {
		t.Errorf("the page: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(h, "POST", "/echo", "\x00\xff"); rec.Code != 200 || rec.Body.String() != "\x00\xff" {
		t.Errorf("the echo: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(h, "GET", "/app.py", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a path the application does not have: %d %q", rec.Code, rec.Body.String())
	}
	// Under a prefix the host strips, the application is told where it is mounted.
	if rec := webRequest(http.StripPrefix("/t", h), "GET", "/t/", ""); rec.Code != 200 || rec.Body.String() != "wasi /t" {
		t.Errorf("under a prefix: %d %q", rec.Code, rec.Body.String())
	}
}

func TestWebHandlerServesFilesByNameAndApplicationsByTheirs(t *testing.T) {
	c := newComputer(t, Config{Python: pythonFrom(t, "")})
	run(t, c, `mkdir -p www/hello && cd www && echo '<h1>static</h1>' > hello/index.html && cat > hello/app.py <<'PY'
from flask import Flask, request

app = Flask(__name__)

@app.get("/")
def index():
    return request.script_root

@app.post("/echo")
def echo():
    return request.get_data()

@app.get("/broken")
def broken():
    raise RuntimeError("what went wrong")
PY
mkdir -p hello/pkg hello/missing.py
echo 'from . import app' > hello/pkg/__init__.py
cp hello/app.py hello/pkg/app.py
echo 'literal' > 'hello/pkg:app'`)
	var errorLog bytes.Buffer // one request at a time writes to it here
	h := WebHandler(func(*http.Request) (*Computer, string, error) { return c, "www", nil },
		ErrorLog(func(*http.Request) io.Writer { return &errorLog }))
	// As http.server: a folder's list, its index, a file as it is, a Python file's source too.
	if rec := webRequest(h, "GET", "/", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `href="hello/"`) {
		t.Errorf("a folder's list: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(h, "GET", "/hello/", ""); rec.Code != 200 || rec.Body.String() != "<h1>static</h1>\n" {
		t.Errorf("a folder's index: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(h, "GET", "/hello/app.py", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Flask(") {
		t.Errorf("a Python file: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(h, "GET", "/hello/pkg:app", ""); rec.Code != 200 || rec.Body.String() != "literal\n" {
		t.Errorf("literal file with a colon: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(h, "GET", "/hello/nothing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("no file: %d", rec.Code)
	}
	// An application by its name, where its module's file is.
	if rec := webRequest(h, "GET", "/hello/app:app/", ""); rec.Code != 200 || rec.Body.String() != "/hello/app:app" {
		t.Errorf("the application: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(h, "POST", "/hello/../hello/app:app/echo", "\x00\xff"); rec.Code != 200 || rec.Body.String() != "\x00\xff" {
		t.Errorf("the echo: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(http.StripPrefix("/t", h), "GET", "/t/hello/app:app/", ""); rec.Code != 200 || rec.Body.String() != "/t/hello/app:app" {
		t.Errorf("under a prefix: %d %q", rec.Code, rec.Body.String())
	}
	if rec := webRequest(http.StripPrefix("/t/", h), "GET", "/t/old/../hello/pkg.app:app/", ""); rec.Code != 200 || rec.Body.String() != "/t/hello/pkg.app:app" {
		t.Errorf("cleaned package URL under a prefix: %d %q", rec.Code, rec.Body.String())
	}
	root := WebHandler(func(*http.Request) (*Computer, string, error) { return c, "/", nil })
	if rec := webRequest(root, "GET", "/home/user/www/hello/app:app/", ""); rec.Code != 200 || rec.Body.String() != "/home/user/www/hello/app:app" {
		t.Errorf("computer root: %d %q", rec.Code, rec.Body.String())
	}
	trailing := WebHandler(func(*http.Request) (*Computer, string, error) { return c, "/home/user/www/", nil })
	if rec := webRequest(trailing, "GET", "/hello/", ""); rec.Code != 200 || rec.Body.String() != "<h1>static</h1>\n" {
		t.Errorf("absolute folder ending with slash: %d %q", rec.Code, rec.Body.String())
	}
	// A failure is the error log's, and no part of the answer.
	if rec := webRequest(h, "GET", "/hello/app:app/broken", ""); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "what went wrong") {
		t.Errorf("a failure: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(errorLog.String(), "RuntimeError: what went wrong") {
		t.Errorf("the error log: %q", errorLog.String())
	}
	// Only a module that is a file of the folder is run: not Python's own, and not one a folder up.
	for _, target := range []string{"/hello/os:getcwd/", "/hello/missing:app/", "/app:app/", "/hello/nothing:app/"} {
		if rec := webRequest(h, "GET", target, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %q", target, rec.Code, rec.Body.String())
		}
	}
}

func TestComputerFS(t *testing.T) {
	c := newComputer(t, Config{Options: []gbash.Option{
		gbash.WithFileSystem(gbash.SeededInMemoryFileSystem(gbfs.InitialFiles{
			"/www/index.txt": {Content: []byte("hello")},
			"/large":         {Content: []byte("longer")},
		})),
		gbash.WithPolicy(policy.NewStatic(&policy.Config{ReadRoots: []string{"/www", "/large"}})),
		gbash.WithLimitOverrides(policy.Limits{MaxFileBytes: 5}),
	}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	files, err := c.FS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := fs.Sub(files, "www")
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(folder, "index.txt"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/index.txt", nil)
	req.Header.Set("Range", "bytes=1-3")
	rec := httptest.NewRecorder()
	http.FileServerFS(folder).ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "ell" {
		t.Errorf("range: %d %q", rec.Code, rec.Body.String())
	}
	if f, err := files.Open("large"); f != nil || err == nil {
		t.Errorf("oversized non-seekable file opened: %v, %v", f, err)
	}
	if _, err := files.Open("private"); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("opening a denied path: %v", err)
	}
	cancel()
	if _, err := files.Open("www/index.txt"); !errors.Is(err, context.Canceled) {
		t.Errorf("opening after cancellation: %v", err)
	}
}
