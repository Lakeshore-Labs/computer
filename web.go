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
	"time"

	"github.com/Lakeshore-Labs/gbash"
	"github.com/Lakeshore-Labs/gbash/commands"
	gbfs "github.com/Lakeshore-Labs/gbash/fs"
)

// CGI runs a shell script for each HTTP request. The script receives CGI variables
// (RFC 3875) and the request body on stdin. It writes headers, a blank line and
// the response body to stdout. The request context controls the lifetime of the
// shell. A script that fails after its headers are sent ends the response short.
//
// Policy is the host's: who may ask, how large a request may be (http.MaxBytesHandler),
// how long it may take (a deadline on the request's context), and which of the
// request's headers the script sees. Every header reaches the script, a credential
// or a cookie too, as with net/http/cgi; the host removes what is its own first.
type CGI struct {
	Computer *Computer
	Script   string            // the shell script, such as `python3 app.py`
	Dir      string            // where it runs; the computer's WorkDir when empty
	Root     string            // mount path after http.StripPrefix; the remaining path is PATH_INFO
	Env      map[string]string // further variables
	Stderr   io.Writer         // the script's error output; os.Stderr when nil
}

func (h *CGI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimRight(h.Root, "/")
	p := r.URL.Path
	prefix := stripped(r)
	if strings.HasSuffix(prefix, "/") {
		prefix = strings.TrimSuffix(prefix, "/")
		p = "/" + p
	}
	h.serveHTTP(w, r, prefix+root, strings.TrimPrefix(p, root))
}

func (h *CGI) serveHTTP(w http.ResponseWriter, r *http.Request, scriptName, pathInfo string) {
	ctx, cancel := context.WithCancel(r.Context())
	out, in := io.Pipe()
	conn := http.NewResponseController(w)
	if r.ContentLength > 0 {
		_ = conn.EnableFullDuplex() // a script can write its page before it finishes reading
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(closed)
		out.CloseWithError(ctx.Err())
		if r.Context().Err() != nil {
			_ = conn.SetWriteDeadline(time.Now())
		}
		if r.Body != nil && r.Body != http.NoBody {
			_ = conn.SetReadDeadline(time.Now())
			_ = r.Body.Close() // unblock a script waiting for input
		}
	})
	defer func() {
		if !stop() {
			<-closed
		}
		cancel()
	}()
	// A script is told its input's length, so a body of unknown length is read first.
	body, length := io.Reader(r.Body), r.ContentLength
	if length < 0 {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			status := http.StatusBadRequest
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		body, length = bytes.NewReader(b), int64(len(b))
	}

	stderr := h.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	done := make(chan struct{})
	defer func() {
		select {
		case <-done:
		default:
			cancel()
			<-done
		}
	}()
	go func() {
		code, err := h.Computer.shell(ctx, &gbash.ExecutionRequest{
			Script: h.Script, WorkDir: h.Dir, Env: h.environment(r, scriptName, pathInfo, length),
			Stdin: body, Stdout: in, Stderr: stderr,
		})
		if err == nil && code != 0 {
			err = fmt.Errorf("exit status %d", code)
		}
		close(done)
		in.CloseWithError(err)
	}()

	page := bufio.NewReader(io.LimitReader(out, http.DefaultMaxHeaderBytes))
	headers, err := textproto.NewReader(page).ReadMIMEHeader()
	status := http.StatusOK
	if err == nil {
		switch {
		case headers["Status"] != nil:
			code, _, _ := strings.Cut(headers.Get("Status"), " ")
			status, err = strconv.Atoi(code)
			if len(headers["Status"]) != 1 || len(code) != 3 || status < 200 || status > 599 {
				err = fmt.Errorf("invalid CGI status %q", headers["Status"])
			}
		case headers.Get("Location") != "":
			status = http.StatusFound
		}
	}
	hasBody := status != http.StatusNoContent && status != http.StatusResetContent && status != http.StatusNotModified
	if err == nil && hasBody && headers.Get("Content-Type") == "" && headers.Get("Location") == "" {
		err = errors.New("missing CGI Content-Type")
	}
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("computer: the CGI script of %s: %v", r.URL.Path, err)
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	headers.Del("Status")
	for name, values := range headers {
		w.Header()[name] = append(w.Header()[name], values...)
	}
	w.WriteHeader(status)
	var dst io.Writer = w
	if r.Method == http.MethodHead || !hasBody {
		dst = io.Discard
	}
	if _, err := io.Copy(dst, io.MultiReader(page, out)); err != nil && ctx.Err() == nil {
		// The status is sent already, so the client is told by a response that does not end as one should.
		log.Printf("computer: the CGI response of %s: %v", r.URL.Path, err)
		panic(http.ErrAbortHandler)
	}
}

func (h *CGI) environment(r *http.Request, scriptName, pathInfo string, length int64) map[string]string {
	host, port := r.Host, "80"
	if r.TLS != nil {
		port = "443"
	}
	if p := (&url.URL{Host: host}).Port(); p != "" {
		host, port = strings.TrimSuffix(host, ":"+p), p
	}
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remote = r.RemoteAddr
	}
	env := map[string]string{
		"GATEWAY_INTERFACE": "CGI/1.1", "SERVER_SOFTWARE": "computer", "SERVER_PROTOCOL": r.Proto,
		"SERVER_NAME": host, "SERVER_PORT": port, "REMOTE_ADDR": remote, "REMOTE_HOST": remote,
		"REQUEST_METHOD": r.Method, "REQUEST_URI": r.RequestURI, "QUERY_STRING": r.URL.RawQuery,
		"SCRIPT_NAME": scriptName, "PATH_INFO": pathInfo,
		"HTTP_HOST": r.Host,
	}
	if length > 0 {
		env["CONTENT_LENGTH"] = strconv.FormatInt(length, 10)
	}
	if r.TLS != nil {
		env["HTTPS"] = "on"
	}
	for name, values := range r.Header {
		// Proxy would be HTTP_PROXY, which programs read as their proxy; a name with an underscore would pass for
		// another header's.
		name = http.CanonicalHeaderKey(name)
		if len(values) == 0 {
			continue
		}
		if name == "Content-Type" {
			env["CONTENT_TYPE"] = values[0]
			continue
		}
		if name == "Content-Length" || name == "Proxy" || strings.Contains(name, "_") {
			continue
		}
		sep := ", "
		if name == "Cookie" {
			sep = "; "
		}
		env["HTTP_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_"))] = strings.Join(values, sep)
	}
	maps.Copy(env, h.Env)
	return env
}

const pythonName = `[A-Za-z_][A-Za-z0-9_]*`
const dottedName = pythonName + `(?:\.` + pythonName + `)*`

var wsgiName = regexp.MustCompile(`^(` + dottedName + `):(` + dottedName + `)$`)

// WSGI serves a Python application named "module:callable", such as "app:app",
// through CGI. Each request imports the module in a fresh interpreter with dir
// as its working directory (the computer's WorkDir when empty). Application
// prints and tracebacks go to the CGI handler's Stderr.
func WSGI(c *Computer, app, dir string) (*CGI, error) {
	name := wsgiName.FindStringSubmatch(app)
	if name == nil {
		return nil, fmt.Errorf("computer: a WSGI application is named module:callable, not %q", app)
	}
	return wsgi(c, dir, name, ""), nil
}

// localModule answers 404 unless the module imported is a file of the working directory: a module Python has
// already, its own or the image's, has the name of a local file at times, and is not the folder's to serve.
const localModule = `
import os
if not (getattr(sys.modules[module], "__file__", None) or "").startswith(os.getcwd().rstrip("/") + "/"):
    app = lambda environ, start: start("404 Not Found", [("Content-Type", "text/plain")]) and [] or [b"404 page not found\n"]
`

func wsgi(c *Computer, dir string, name []string, guard string) *CGI {
	script := `import functools, importlib, sys
from wsgiref.handlers import CGIHandler
handler = CGIHandler()
sys.stdout = sys.stderr
module, target = sys.argv[1:]
app = functools.reduce(getattr, target.split("."), importlib.import_module(module))
` + guard + `
handler.run(app)
`
	return &CGI{Computer: c, Dir: dir, Script: "python3 -c " + quote(script) + " " + name[1] + " " + name[2]}
}

// stripped recovers the mount removed by http.StripPrefix from the original request URI.
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

// ErrorLog selects a writer for each application's stderr. A nil writer uses
// os.Stderr. The host owns the writer and may receive output from concurrent requests.
func ErrorLog(of func(*http.Request) io.Writer) WebOption {
	return func(o *webOptions) { o.errorLog = of }
}

// WebHandler serves files, directory indexes and listings using http.FileServerFS.
// A path component of the form "module:callable" mounts a local Python WSGI app
// unless a file or directory has that literal name. For example, /shop/app:app/cart
// sends /cart to the app in shop/app.py. /shop/app.py serves the source file.
//
// get authorizes the request and returns the host's computer and folder. Relative
// folders resolve under the computer's WorkDir. fs.ErrPermission returns 403.
// Other errors are logged and return 500. The host can mount the handler under
// http.StripPrefix and select the application's stderr with ErrorLog.
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
		files.dir = gbfs.Resolve(files.dir, root)
		folder := webFS{ctx, files}
		clean := path.Clean("/" + r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/") && clean != "/" {
			clean += "/"
		}
		regular := func(name string) bool {
			info, err := folder.Stat(name)
			return err == nil && info.Mode().IsRegular()
		}
		end := 0
		for part := range strings.SplitSeq(clean[1:], "/") {
			dir := strings.TrimPrefix(clean[:end], "/")
			end += 1 + len(part)
			name := wsgiName.FindStringSubmatch(part)
			if name == nil {
				continue
			}
			mount := clean[1:end]
			if _, err := folder.Stat(mount); !errors.Is(err, fs.ErrNotExist) {
				continue // a literal file or directory takes precedence
			}
			file := path.Join(dir, strings.ReplaceAll(name[1], ".", "/"))
			if !regular(file+".py") && !regular(file+"/__init__.py") {
				continue
			}
			app := wsgi(c, path.Join(files.dir, dir), name, localModule)
			if o.errorLog != nil {
				app.Stderr = o.errorLog(r)
			}
			if strings.HasPrefix(r.URL.Path, "/") {
				mount = "/" + mount
			}
			app.serveHTTP(w, r, stripped(r)+mount, clean[end:])
			return
		}
		http.FileServerFS(folder).ServeHTTP(w, r)
	})
}

// FS exposes the computer's files from its root, under the shell's policy.
// Open and Stat use ctx. Regular files support seeking for http.FileServerFS.
func (c *Computer) FS(ctx context.Context) (fs.FS, error) {
	files, err := c.files(ctx)
	if err != nil {
		return nil, err
	}
	files.dir = "/"
	return webFS{ctx, files}, nil
}

type webFS struct {
	ctx   context.Context
	files *ruled
}

func (f webFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, told("stat", name, fs.ErrInvalid)
	}
	if err := f.ctx.Err(); err != nil {
		return nil, told("stat", name, err)
	}
	info, err := f.files.Stat(f.ctx, name)
	return info, told("stat", name, err)
}

func (f webFS) Open(name string) (fs.File, error) {
	info, err := f.Stat(name)
	if err != nil {
		return nil, told("open", name, err)
	}
	if info.IsDir() {
		return &webDir{fsys: f, name: name, info: info}, nil
	}
	if !info.Mode().IsRegular() {
		return nil, told("open", name, fs.ErrPermission)
	}
	file, err := f.files.Open(f.ctx, name)
	if err != nil {
		return nil, told("open", name, err)
	}
	reader, ok := file.(io.ReadSeeker)
	if !ok {
		// gbash's own memory files cannot seek. Buffer them under the same limit as a shell command.
		var inv commands.Invocation
		inv.Limits.MaxFileBytes = f.files.maxFileBytes
		content, err := commands.ReadAll(f.ctx, &inv, file)
		if err != nil {
			file.Close()
			return nil, told("open", name, err)
		}
		reader = bytes.NewReader(content)
	}
	return webFile{file, reader}, nil
}

// Expose only reads. A host file also has methods such as Chmod even when opened read-only.
type webFile struct {
	fs.File
	io.ReadSeeker
}

func (f webFile) Read(p []byte) (int, error) { return f.ReadSeeker.Read(p) }

type webDir struct {
	fsys    webFS
	name    string
	info    fs.FileInfo
	entries []fs.DirEntry
}

func (d *webDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *webDir) Close() error               { return nil }
func (d *webDir) Read([]byte) (int, error) {
	return 0, told("read", d.name, fs.ErrInvalid)
}

func (d *webDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.fsys.files != nil {
		entries, err := d.fsys.files.ReadDir(d.fsys.ctx, d.name)
		if err != nil {
			return nil, told("readdir", d.name, err)
		}
		d.entries, d.fsys.files = entries, nil
	}
	if n > 0 && len(d.entries) == 0 {
		return nil, io.EOF
	}
	if n <= 0 || n > len(d.entries) {
		n = len(d.entries)
	}
	rest := d.entries[:n]
	d.entries = d.entries[n:]
	return rest, nil
}
