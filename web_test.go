package computer

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func webRequest(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

// cgi is a script of c as a handler, mounted at /x.
func cgi(c *Computer, script string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveCGI(w, r, c, script, "", stripped(r)+"/x", strings.TrimPrefix(r.URL.Path, "/x"), io.Discard)
	})
}

func TestCGIRunsAScriptForARequest(t *testing.T) {
	c := newComputer(t, Config{})
	h := cgi(c, `printf 'Status: 201 Created\r\nContent-Type: text/plain\r\nSet-Cookie: a=1\r\nSet-Cookie: b=2\r\n\r\n'
echo "$REQUEST_METHOD $SCRIPT_NAME $PATH_INFO $QUERY_STRING $HTTP_X_DEMO $CONTENT_LENGTH $REMOTE_ADDR ${HTTP_PROXY-unset}"; cat`)
	req := httptest.NewRequest(http.MethodPost, "/t/x/echo?q=1", strings.NewReader("body"))
	req.Header.Set("X-Demo", "yes")
	req.Header.Set("Proxy", "value")
	req.RemoteAddr = "192.0.2.1"
	rec := httptest.NewRecorder()
	rec.Header().Add("Set-Cookie", "host=1")
	http.StripPrefix("/t", h).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Body.String() != "POST /t/x /echo q=1 yes 4 192.0.2.1 unset\nbody" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) != 3 || got[0] != "host=1" {
		t.Errorf("the host's cookie and the script's: %v", got)
	}
	// A script that says nothing, or not what a CGI script says, is the server's failure, not a page.
	for _, script := range []string{"exit 3", `printf 'Status: 99 Bad\n\n'`, `printf 'Status: \n\n'`, `printf 'X-Only: yes\n\n'`} {
		if rec := webRequest(cgi(c, script), "GET", "/x/", ""); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: %d %q", script, rec.Code, rec.Body.String())
		}
	}
}

func TestCGIEndsShortTheResponseOfAScriptThatFailsAfterItsHeaders(t *testing.T) {
	h := cgi(newComputer(t, Config{}), `printf 'Content-Type: text/plain\r\n\r\nhalf'; exit 1`)
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Error("a response cut short was ended as a whole one is")
		}
	}()
	webRequest(h, "GET", "/x/", "")
}

func TestCGIAnswersAStatusWithNoBodyThoughTheScriptWroteOne(t *testing.T) {
	server := httptest.NewServer(cgi(newComputer(t, Config{}), `printf 'Status: 204 No Content\n\nextra'`))
	defer server.Close()
	res, err := http.Get(server.URL + "/x/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("got %d", res.StatusCode)
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
PY
mkdir -p hello/pkg hello/missing.py
echo 'from . import app' > hello/pkg/__init__.py
cp hello/app.py hello/pkg/app.py`)
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
