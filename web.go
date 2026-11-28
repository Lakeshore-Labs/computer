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
	"github.com/Lakeshore-Labs/gbash/commands"
	gbfs "github.com/Lakeshore-Labs/gbash/fs"
)

// An ErrorLog is where the error output of the application a request asks goes, a failure's traceback among it,
// such as a file of the computer its author reads; os.Stderr for a request it returns nil for. The writer is the
// host's: it is written to while the request runs, by requests side by side, and is not closed.
type ErrorLog func(*http.Request) io.Writer

// wsgiName is a WSGI application's name as gunicorn and uvicorn write one, module:callable, each a dotted name.
var wsgiName = regexp.MustCompile(`^([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*):([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)$`)

// wsgiScript serves one request with the application its two arguments name, as wsgiref's CGIHandler does. What
// the application prints goes to the error output, not into the page.
var wsgiScript = "python3 -c " + quote(`import functools, importlib, sys
from wsgiref.handlers import CGIHandler
handler = CGIHandler()
sys.stdout = sys.stderr
handler.run(functools.reduce(getattr, sys.argv[2].split("."), importlib.import_module(sys.argv[1])))
`)

// WebHandler serves a folder of a computer as python3 -m http.server serves one: the request's path is a path
// under the folder, a file is sent as it is, a Python file's source too, and a folder as its index.html or as a
// list of what it holds (http.FileServer).
//
// A Python web application is asked for by its name in the path, written as gunicorn and uvicorn write one,
// module:callable, where the module's file is: /shop/app:app/cart is the object app of shop/app.py, asked for
// /cart, and /shop/app.py is that file. So nothing is declared: a file is reached by its name, and an application
// by its own. The application is a WSGI one (PEP 3333), served as a CGI script is (RFC 3875): each request is a
// python3 of its own, in the module's folder, so requests run side by side and nothing is kept in memory between
// them. A request that ends stops its python3, and an application that fails once its answer has begun ends the
// answer short.
//
// get is the host's, as TerminalHandler's is: it returns the requester's computer and the folder, after the
// host's own checks of who is asking; a folder that is not absolute is under the computer's WorkDir. An error that
// is fs.ErrPermission refuses the request with 403, and another with 500, which is logged and not told to the
// client. The computer stays the host's, to close. The handler may be mounted under http.StripPrefix.
//
// Policy is the host's too: how large a request may be (http.MaxBytesHandler; a request to an application is
// read whole before the application runs), how long it may take (a deadline on the request's context), and which
// of the request's headers an application sees: every one reaches it, a credential or a cookie too, unless the
// host removes it first. An application's error output is the host's own, unless an ErrorLog says where.
func WebHandler(get func(*http.Request) (*Computer, string, error), errorLog ...ErrorLog) http.Handler {
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
		clean := path.Clean("/" + r.URL.Path) // rooted, so no .. is left in it
		if strings.HasSuffix(r.URL.Path, "/") && clean != "/" {
			clean += "/"
		}
		regular := func(name string) bool {
			info, err := folder.Stat(name)
			return err == nil && info.Mode().IsRegular()
		}
		end := 0 // of the part of the path looked at
		for part := range strings.SplitSeq(clean[1:], "/") {
			dir := strings.TrimPrefix(clean[:end], "/")
			end += 1 + len(part)
			name := wsgiName.FindStringSubmatch(part)
			if name == nil {
				continue
			}
			// Only a module that is a file of the folder is run.
			file := path.Join(dir, strings.ReplaceAll(name[1], ".", "/"))
			if !regular(file+".py") && !regular(file+"/__init__.py") {
				continue
			}
			stderr := io.Writer(os.Stderr)
			for _, of := range errorLog {
				if to := of(r); to != nil {
					stderr = to
				}
			}
			// The path a host's http.StripPrefix took is part of where the application is mounted.
			mount := strings.TrimSuffix(stripped(r), "/") + clean[:end]
			serveCGI(w, r, c, wsgiScript+" "+name[1]+" "+name[2], path.Join(files.dir, dir), mount, clean[end:], stderr)
			return
		}
		http.FileServerFS(folder).ServeHTTP(w, r)
	})
}

// stripped is the prefix an http.StripPrefix before the handler took from the request's path; "" when none was.
func stripped(r *http.Request) string {
	asked, err := url.ParseRequestURI(r.RequestURI)
	if err != nil {
		return ""
	}
	prefix, _ := strings.CutSuffix(asked.Path, r.URL.Path)
	if prefix == asked.Path {
		return ""
	}
	return prefix
}

// serveCGI runs script in the computer's shell as a CGI program (RFC 3875), as net/http/cgi's Handler runs one:
// the request is its variables and its standard input, and what it writes, headers, an empty line and a body, is
// the response. It returns when the script is over, which a request that ends stops.
func serveCGI(w http.ResponseWriter, r *http.Request, c *Computer, script, dir, scriptName, pathInfo string, stderr io.Writer) {
	// A script is told its input's length, and is not kept waiting on the client for it.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		status := http.StatusBadRequest
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	out, in := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		code, err := c.shell(ctx, &gbash.ExecutionRequest{
			Script: script, WorkDir: dir, Env: cgiEnv(r, scriptName, pathInfo, len(body)),
			Stdin: bytes.NewReader(body), Stdout: in, Stderr: stderr,
		})
		if err == nil && code != 0 {
			err = fmt.Errorf("exit status %d", code)
		}
		in.CloseWithError(err)
	}()
	defer func() { cancel(); out.Close(); <-done }()

	page := bufio.NewReader(io.LimitReader(out, http.DefaultMaxHeaderBytes)) // the headers, and no more held of them
	headers, err := textproto.NewReader(page).ReadMIMEHeader()
	status := http.StatusOK
	switch {
	case err != nil:
	case headers.Get("Status") != "":
		code, _, _ := strings.Cut(headers.Get("Status"), " ")
		if status, err = strconv.Atoi(code); err == nil && (status < 200 || status > 599) {
			err = fmt.Errorf("status %d", status)
		}
	case headers.Get("Location") != "":
		status = http.StatusFound
	case headers.Get("Content-Type") == "":
		err = errors.New("no Content-Type")
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
		w.Header()[name] = append(w.Header()[name], values...) // beside the host's own, such as its cookies
	}
	w.WriteHeader(status)
	// A status that has no body, such as 204, has none of the script's either.
	if _, err := io.Copy(w, io.MultiReader(page, out)); err != nil && !errors.Is(err, http.ErrBodyNotAllowed) {
		// The status is sent already, so the client is told by a response that does not end as one should, also
		// when it is the request that ended.
		if ctx.Err() == nil {
			log.Printf("computer: the CGI response of %s: %v", r.URL.Path, err)
		}
		panic(http.ErrAbortHandler)
	}
}

// cgiEnv is the request as a CGI script's variables.
func cgiEnv(r *http.Request, scriptName, pathInfo string, length int) map[string]string {
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
		"SCRIPT_NAME": scriptName, "PATH_INFO": pathInfo, "HTTP_HOST": r.Host,
	}
	if length > 0 {
		env["CONTENT_LENGTH"] = strconv.Itoa(length)
	}
	if r.TLS != nil {
		env["HTTPS"] = "on"
	}
	for name, values := range r.Header {
		name = http.CanonicalHeaderKey(name)
		switch {
		case len(values) == 0, name == "Content-Length":
		case name == "Content-Type":
			env["CONTENT_TYPE"] = values[0]
		// Proxy would be HTTP_PROXY, which programs read as their proxy; a name with an underscore would pass for
		// another header's.
		case name == "Proxy", strings.Contains(name, "_"):
		case name == "Cookie":
			env["HTTP_COOKIE"] = strings.Join(values, "; ")
		default:
			env["HTTP_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_"))] = strings.Join(values, ", ")
		}
	}
	return env
}

// webFS is a folder of a computer's files, the one its files' relative names are under, as an fs.FS for
// http.FileServerFS, for one request.
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
