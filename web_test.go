package computer

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCGIRunsAScriptForARequest(t *testing.T) {
	c := newComputer(t, Config{})
	h := &CGI{Computer: c, Root: "/x", Env: map[string]string{"GREETING": "hi"},
		Script: `printf 'Status: 201 Created\r\nContent-Type: text/plain\r\nSet-Cookie: a=1\r\nSet-Cookie: b=2\r\n\r\n'
echo "$GREETING $REQUEST_METHOD $SCRIPT_NAME $PATH_INFO $QUERY_STRING $HTTP_X_DEMO $CONTENT_LENGTH"; cat`}
	req := httptest.NewRequest(http.MethodPost, "/x/echo?q=1", strings.NewReader("body"))
	req.Header.Set("X-Demo", "yes")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Body.String() != "hi POST /x /echo q=1 yes 4\nbody" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) != 2 {
		t.Errorf("cookies: %v", got)
	}

	h.Script = "exit 3" // a script that says nothing is the server's failure, not a page
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("a silent script: %d %q", rec.Code, rec.Body.String())
	}
}

func TestWSGIServesAnApplicationByItsName(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	run(t, c, `mkdir hello && cat > hello/app.py <<'PY'
import sys
from flask import Flask, request

app = Flask(__name__)

@app.get("/")
def index():
    print("a print is not the page")
    return f"{sys.platform} {request.script_root}"

@app.post("/echo")
def echo():
    return request.get_data()
PY
echo '<h1>static</h1>' > hello/index.html`)
	if _, err := WSGI(c, "app.py", "hello"); err == nil {
		t.Error("a file's name passed for module:callable")
	}
	h, err := WSGI(c, "app:app", "/home/user/hello")
	if err != nil {
		t.Fatal(err)
	}
	get := func(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
		return rec
	}
	if rec := get(h, "GET", "/", ""); rec.Code != 200 || rec.Body.String() != "wasi " {
		t.Errorf("the page: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "POST", "/echo", "\x00\xff"); rec.Code != 200 || rec.Body.String() != "\x00\xff" {
		t.Errorf("the echo: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "GET", "/app.py", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a path the application does not have: %d %q", rec.Code, rec.Body.String())
	}
	// Under a prefix the host strips, the application is told where it is mounted.
	if rec := get(http.StripPrefix("/t", h), "GET", "/t/", ""); rec.Code != 200 || rec.Body.String() != "wasi /t" {
		t.Errorf("under a prefix: %d %q", rec.Code, rec.Body.String())
	}
}

func TestWebHandlerServesFilesByNameAndApplicationsByTheirs(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
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
PY`)
	var errorLog bytes.Buffer // one request at a time writes to it here
	h := WebHandler(func(*http.Request) (*Computer, string, error) { return c, "www", nil },
		ErrorLog(func(*http.Request) io.Writer { return &errorLog }))
	get := func(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
		return rec
	}
	// As http.server: a folder's list, its index, a file as it is, a Python file's source too.
	if rec := get(h, "GET", "/", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `href="hello/"`) {
		t.Errorf("a folder's list: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "GET", "/hello/", ""); rec.Code != 200 || rec.Body.String() != "<h1>static</h1>\n" {
		t.Errorf("a folder's index: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "GET", "/hello/app.py", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Flask(") {
		t.Errorf("a Python file: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "GET", "/hello/nothing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("no file: %d", rec.Code)
	}
	// An application by its name, where its module's file is.
	if rec := get(h, "GET", "/hello/app:app/", ""); rec.Code != 200 || rec.Body.String() != "/hello/app:app" {
		t.Errorf("the application: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "POST", "/hello/../hello/app:app/echo", "\x00\xff"); rec.Code != 200 || rec.Body.String() != "\x00\xff" {
		t.Errorf("the echo: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(http.StripPrefix("/t", h), "GET", "/t/hello/app:app/", ""); rec.Code != 200 || rec.Body.String() != "/t/hello/app:app" {
		t.Errorf("under a prefix: %d %q", rec.Code, rec.Body.String())
	}
	// A failure is the error log's, and no part of the answer.
	if rec := get(h, "GET", "/hello/app:app/broken", ""); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "what went wrong") {
		t.Errorf("a failure: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(errorLog.String(), "RuntimeError: what went wrong") {
		t.Errorf("the error log: %q", errorLog.String())
	}
	// Only a module that is a file of the folder is run: not Python's own, and not one a folder up.
	for _, target := range []string{"/hello/os:getcwd/", "/app:app/", "/hello/nothing:app/"} {
		if rec := get(h, "GET", target, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %q", target, rec.Code, rec.Body.String())
		}
	}
}
