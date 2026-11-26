package computer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/Lakeshore-Labs/gbash"
)

const wsgiLauncher = `
import importlib
import os
import sys
from wsgiref.handlers import CGIHandler

class BufferedCGIHandler(CGIHandler):
    def handle_error(self):
        if self.headers_sent:
            # The host can discard a buffered response if the app fails midway.
            raise
        super().handle_error()

module, name = sys.argv[1].split(":", 1)
sys.path.append(os.path.join(os.getcwd(), "vendor"))
gateway = BufferedCGIHandler()
sys.stdout = sys.stderr
gateway.run(getattr(importlib.import_module(module), name))
`

// WSGIHandler adapts an application in the computer's working directory to
// net/http. app is a module:callable reference, and vendor/ in that directory
// is available for dependencies. Each request runs in a fresh Python instance
// with the computer's configured mounts and tools. The timeout bounds Python
// execution when positive. The caller owns the HTTP server and must finish
// serving requests before closing the computer.
func (c *Computer) WSGIHandler(app string, timeout time.Duration) (http.Handler, error) {
	module, callable, ok := strings.Cut(app, ":")
	if !ok || module == "" || callable == "" || strings.Contains(callable, ":") {
		return nil, fmt.Errorf("expected a WSGI module:callable, got %q", app)
	}
	if c.cfg.Python == nil {
		return nil, errors.New("WSGI requires Python")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const maxBody = 1 << 20
		if strings.ContainsRune(r.URL.Path, 0) {
			http.Error(w, "invalid request path", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				http.Error(w, "request body exceeds 1 MiB", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "could not read request body", http.StatusBadRequest)
			}
			return
		}

		local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
		host, port := r.Host, "80"
		if r.TLS != nil {
			port = "443"
		}
		if name, p, err := net.SplitHostPort(r.Host); err == nil {
			host, port = name, p
		} else if local != nil {
			_, port, _ = net.SplitHostPort(local.String())
		}
		remote, _, _ := net.SplitHostPort(r.RemoteAddr)
		env := map[string]string{
			"GATEWAY_INTERFACE": "CGI/1.1",
			"REQUEST_METHOD":    r.Method, "SCRIPT_NAME": "", "PATH_INFO": r.URL.Path,
			"QUERY_STRING": r.URL.RawQuery, "SERVER_PROTOCOL": r.Proto,
			"SERVER_NAME": host, "SERVER_PORT": port, "REMOTE_ADDR": remote,
			"CONTENT_TYPE":   r.Header.Get("Content-Type"),
			"CONTENT_LENGTH": strconv.Itoa(len(body)), "HTTP_HOST": r.Host,
		}
		if r.TLS != nil {
			env["HTTPS"] = "on"
		}
		connection := connectionHeaders(r.Header)
		for name, values := range r.Header {
			if name == "Content-Type" || name == "Content-Length" || name == "Proxy" ||
				strings.ContainsRune(name, '_') || hopByHop(name) || connection[name] {
				continue
			}
			separator := ", "
			if name == "Cookie" {
				separator = "; "
			}
			env["HTTP_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_"))] = strings.Join(values, separator)
		}

		ctx := r.Context()
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		result, err := c.Exec(ctx, &gbash.ExecutionRequest{
			Script: "python3 -c '" + wsgiLauncher + "' \"$1\"", Args: []string{app},
			Env: env, WorkDir: c.cfg.WorkDir, Timeout: timeout, Stdin: bytes.NewReader(body),
		})
		if result != nil && result.Stderr != "" {
			log.Print(result.Stderr)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || (result != nil && result.TimedOut) {
			http.Error(w, "WSGI request timed out", http.StatusGatewayTimeout)
			return
		}
		if err != nil || result == nil || result.ExitCode != 0 || result.StdoutTruncated {
			if err != nil {
				log.Printf("WSGI execution: %v", err)
			}
			http.Error(w, "WSGI application failed", http.StatusBadGateway)
			return
		}
		if err := writeCGIResponse(w, result.Stdout); err != nil {
			log.Printf("WSGI response: %v", err)
			http.Error(w, "invalid WSGI response", http.StatusBadGateway)
		}
	}), nil
}

func writeCGIResponse(w http.ResponseWriter, output string) error {
	reader := bufio.NewReader(strings.NewReader(output))
	headers, err := textproto.NewReader(reader).ReadMIMEHeader()
	if err != nil {
		return err
	}
	statusText, _, _ := strings.Cut(headers.Get("Status"), " ")
	status, err := strconv.Atoi(statusText)
	if err != nil || status < 200 || status > 599 {
		return fmt.Errorf("invalid CGI Status %q", headers.Get("Status"))
	}
	headers.Del("Status")
	connection := connectionHeaders(http.Header(headers))
	for name, values := range headers {
		if hopByHop(name) || connection[name] {
			continue
		}
		w.Header()[name] = values
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, reader)
	return nil
}

func connectionHeaders(headers http.Header) map[string]bool {
	names := make(map[string]bool)
	for _, value := range headers.Values("Connection") {
		for name := range strings.SplitSeq(value, ",") {
			names[http.CanonicalHeaderKey(strings.TrimSpace(name))] = true
		}
	}
	return names
}

func hopByHop(name string) bool {
	switch name {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}
