package computer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Lakeshore-Labs/gbash"
)

// CGI runs a script in a computer's shell for each request, as net/http/cgi's Handler runs a program: the request
// is the script's CGI variables (RFC 3875) and its standard input, and what the script writes, headers, an empty
// line and a body, is the response. Each request has a shell of its own, so requests run side by side, and a
// request that ends stops its script. A script that fails before it has written its headers is answered with 500.
//
// The host keeps what is its own: who may ask, how large a request may be (http.MaxBytesHandler) and how long it
// may take (a deadline on the request's context).
type CGI struct {
	Computer *Computer
	Script   string            // the shell script, such as `python3 app.py`
	Dir      string            // where it runs; the computer's WorkDir when empty
	Root     string            // the path the script is mounted at, its SCRIPT_NAME, after what http.StripPrefix took; the rest of the path is PATH_INFO
	Env      map[string]string // further variables
	Stderr   io.Writer         // the script's error output; os.Stderr when nil
}

func (h *CGI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A script is told its input's length, so a body of unknown length is read first.
	body, length := io.Reader(r.Body), r.ContentLength
	if length < 0 {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		body, length = bytes.NewReader(b), int64(len(b))
	}
	host, port := r.Host, "80"
	if r.TLS != nil {
		port = "443"
	}
	if name, p, err := net.SplitHostPort(r.Host); err == nil {
		host, port = name, p
	}
	remote, _, _ := net.SplitHostPort(r.RemoteAddr)
	env := map[string]string{
		"GATEWAY_INTERFACE": "CGI/1.1", "SERVER_SOFTWARE": "computer", "SERVER_PROTOCOL": r.Proto,
		"SERVER_NAME": host, "SERVER_PORT": port, "REMOTE_ADDR": remote, "REMOTE_HOST": remote,
		"REQUEST_METHOD": r.Method, "REQUEST_URI": r.RequestURI, "QUERY_STRING": r.URL.RawQuery,
		"SCRIPT_NAME": stripped(r) + h.Root, "PATH_INFO": strings.TrimPrefix(r.URL.Path, h.Root),
		"CONTENT_LENGTH": strconv.FormatInt(length, 10), "CONTENT_TYPE": r.Header.Get("Content-Type"),
		"HTTP_HOST": r.Host,
	}
	if r.TLS != nil {
		env["HTTPS"] = "on"
	}
	for name, values := range r.Header {
		// Proxy would be HTTP_PROXY, which programs read as their proxy; a name with an underscore would pass for
		// another header's.
		if name == "Content-Type" || name == "Content-Length" || name == "Proxy" || strings.Contains(name, "_") {
			continue
		}
		sep := ", "
		if name == "Cookie" {
			sep = "; "
		}
		env["HTTP_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_"))] = strings.Join(values, sep)
	}
	maps.Copy(env, h.Env)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stderr := h.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	out, in := io.Pipe()
	defer out.Close() // a script still writing is not kept waiting for a reader
	go func() {
		code, err := h.Computer.shell(ctx, &gbash.ExecutionRequest{
			Script: h.Script, WorkDir: h.Dir, Env: env, Stdin: body, Stdout: in, Stderr: stderr,
		})
		if err == nil && code != 0 {
			err = fmt.Errorf("exit status %d", code)
		}
		in.CloseWithError(err)
	}()

	page := bufio.NewReader(out)
	headers, err := textproto.NewReader(page).ReadMIMEHeader()
	status := http.StatusOK
	if text, _, _ := strings.Cut(headers.Get("Status"), " "); err == nil && text != "" {
		status, err = strconv.Atoi(text)
	} else if err == nil && headers.Get("Location") != "" {
		status = http.StatusFound
	}
	if err != nil || len(headers) == 0 {
		if ctx.Err() == nil {
			log.Printf("computer: the CGI script of %s: no headers: %v", r.URL.Path, err)
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	headers.Del("Status")
	maps.Copy(w.Header(), http.Header(headers))
	w.WriteHeader(status)
	_, _ = io.Copy(w, page)
}

var wsgiName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*:[A-Za-z_][A-Za-z0-9_.]*$`)

// WSGI is a CGI that serves the Python WSGI application app names as gunicorn, uvicorn and others name one,
// module:callable: "app:app" is the object app of app.py, a Flask application say. The module is imported with
// dir, a folder of the computer, as python3's working directory, WorkDir when empty. Each request is a python3 of
// its own (see CGI), so nothing is kept in memory between requests. What the application prints goes to the
// error output, not into the page. It is for a host that says itself which application is where; WebHandler
// takes the name from the request's path.
func WSGI(c *Computer, app, dir string) (*CGI, error) {
	if !wsgiName.MatchString(app) {
		return nil, fmt.Errorf("computer: a WSGI application is named module:callable, not %q", app)
	}
	module, callable, _ := strings.Cut(app, ":")
	// As wsgiref's CGIHandler serves one request, with the page's output kept from what the application prints.
	return &CGI{Computer: c, Dir: dir, Script: `python3 -c '
import functools, importlib, sys
from wsgiref.handlers import BaseCGIHandler, read_environ
page, sys.stdout = sys.stdout.buffer, sys.stderr
application = functools.reduce(getattr, "` + callable + `".split("."), importlib.import_module("` + module + `"))
handler = BaseCGIHandler(sys.stdin.buffer, page, sys.stderr, read_environ(), multithread=False, multiprocess=True)
handler.wsgi_run_once = True
handler.run(application)
'`}, nil
}

// stripped is the prefix an http.StripPrefix before the handler took from the request's path, which a script is
// told as part of where it is mounted, so that the links it writes lead back to it; "" when none was taken.
func stripped(r *http.Request) string {
	asked, err := url.ParseRequestURI(r.RequestURI)
	if err != nil {
		return ""
	}
	prefix, ok := strings.CutSuffix(asked.Path, r.URL.Path)
	if !ok {
		return ""
	}
	return prefix
}

// A WebOption changes how WebHandler serves.
type WebOption func(*webOptions)

type webOptions struct{ errorLog func(*http.Request) io.Writer }

// ErrorLog is where the error output of the application a request asks goes, such as a file of the computer its
// author reads; os.Stderr for a request it returns nil for. The writer is the host's: it is written to while the
// request runs, by requests side by side, and is not closed.
func ErrorLog(of func(*http.Request) io.Writer) WebOption {
	return func(o *webOptions) { o.errorLog = of }
}

// WebHandler serves a folder of a computer as python3 -m http.server serves one: the request's path is a path
// under the folder, a file is sent as it is, a Python file's source too, and a folder as its index.html or as a
// list of what it holds.
//
// A Python web application is asked for by its name in the path, written as gunicorn and uvicorn write one,
// module:callable, where the module's file is: /shop/app:app/cart is the object app of shop/app.py, asked for
// /cart (see WSGI), and /shop/app.py is that file. So nothing is declared and nothing is looked for: a file is
// reached by its name, and an application by its own. Only a module that is a file of the folder is run.
//
// get is the host's, as TerminalHandler's is: it returns the requester's computer and the folder, after the
// host's own checks of who is asking; a folder that is not absolute is under the computer's WorkDir, and "" is
// WorkDir. An error that is fs.ErrPermission refuses the request with 403, and another with 500, which is logged
// and not told to the client. The computer stays the host's, to close. The handler may be mounted under
// http.StripPrefix.
//
// What an application writes to its error output, a failure's traceback among it, is the server's error log, as
// a web server keeps one, and no part of the answer: the host's own error output, unless ErrorLog says where.
func WebHandler(get func(*http.Request) (*Computer, string, error), opts ...WebOption) http.Handler {
	var o webOptions
	for _, opt := range opts {
		opt(&o)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		c, root, err := get(r)
		var files *ruled
		if err == nil {
			files, err = c.files(ctx)
		}
		switch {
		case errors.Is(err, fs.ErrPermission):
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		case err != nil:
			log.Printf("computer: the web folder of %s: %v", r.URL.Path, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if !path.IsAbs(root) {
			root = path.Join(c.cfg.WorkDir, root)
		}
		clean := path.Clean("/" + r.URL.Path) // rooted, so no .. is left in it
		r2 := r.Clone(ctx)
		if r2.URL.Path = clean; strings.HasSuffix(r.URL.Path, "/") && clean != "/" {
			r2.URL.Path += "/"
		}
		is := func(name string) bool { _, err := files.Stat(ctx, name); return err == nil }
		dir := root // the folder of the part of the path looked at
		for part := range strings.SplitSeq(clean[1:], "/") {
			if module, _, _ := strings.Cut(part, ":"); wsgiName.MatchString(part) && !is(path.Join(dir, part)) {
				file := path.Join(dir, strings.ReplaceAll(module, ".", "/"))
				if is(file+".py") || is(file+"/__init__.py") {
					app, _ := WSGI(c, part, dir)
					app.Root = strings.TrimPrefix(dir, root) + "/" + part
					if o.errorLog != nil {
						app.Stderr = o.errorLog(r)
					}
					app.ServeHTTP(w, r2)
					return
				}
			}
			dir = path.Join(dir, part)
		}
		folder, err := fs.Sub(webFS{ctx, files}, strings.TrimPrefix(root, "/"))
		if err != nil { // the computer's own root, which Sub does not name
			folder = webFS{ctx, files}
		}
		http.FileServerFS(folder).ServeHTTP(w, r2)
	})
}

// FS is the computer's files as an fs.FS, from its root, read under the rules the shell's own commands are held
// to, until ctx ends: for what reads an fs.FS, such as http.FileServerFS, which serves a folder of them as a web
// server serves files. fs.Sub is a folder of it.
func (c *Computer) FS(ctx context.Context) (fs.FS, error) {
	files, err := c.files(ctx)
	if err != nil {
		return nil, err
	}
	return webFS{ctx, files}, nil
}

type webFS struct {
	ctx   context.Context
	files *ruled
}

func (f webFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	full := path.Join("/", name)
	info, err := f.files.Stat(f.ctx, full)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		entries, err := f.files.ReadDir(f.ctx, full)
		return &webDir{info: info, entries: entries}, err
	}
	file, err := f.files.Open(f.ctx, full)
	if err != nil {
		return nil, err
	}
	if _, ok := file.(io.Seeker); ok {
		return file, nil
	}
	// http serves a file it can seek in. ponytail: one that cannot is read whole, which the shell's limit on a
	// file's size bounds.
	defer file.Close()
	content, err := io.ReadAll(file)
	return &webFile{bytes.NewReader(content), info}, err
}

type webFile struct {
	*bytes.Reader
	info fs.FileInfo
}

func (f *webFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *webFile) Close() error               { return nil }

type webDir struct {
	info    fs.FileInfo
	entries []fs.DirEntry
}

func (d *webDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *webDir) Close() error               { return nil }
func (d *webDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.Name(), Err: errors.New("is a directory")}
}

func (d *webDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 || n >= len(d.entries) {
		rest := d.entries
		d.entries = nil
		if n > 0 && len(rest) == 0 {
			return nil, io.EOF
		}
		return rest, nil
	}
	rest := d.entries[:n]
	d.entries = d.entries[n:]
	return rest, nil
}
