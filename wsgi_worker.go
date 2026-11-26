package computer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Lakeshore-Labs/gbash"
)

const wsgiResponseLimit = 1 << 20

// Frames have a four-byte big-endian length followed by a payload.
// Requests contain JSON with byte strings encoded as base64. Responses
// contain CGI headers and body, or an empty frame if the app failed.
const wsgiLauncher = `
import base64
import importlib
import io
import json
import os
import struct
import sys
import traceback
from wsgiref.handlers import BaseCGIHandler

class ResponseBuffer(io.BytesIO):
    def write(self, data):
        if self.tell() + len(data) > 1048576:
            raise ValueError("WSGI response exceeds 1 MiB")
        return super().write(data)

class BufferedCGIHandler(BaseCGIHandler):
    def handle_error(self):
        if self.headers_sent:
            raise
        super().handle_error()

module, name = sys.argv[1].split(":", 1)
sys.path.append(os.path.join(os.getcwd(), "vendor"))
source, sink = sys.stdin.buffer, sys.stdout.buffer
sys.stdin = io.StringIO()
sys.stdout = sys.stderr
application = getattr(importlib.import_module(module), name)
while header := source.read(4):
    size, = struct.unpack(">I", header)
    payload = source.read(size)
    if len(payload) != size:
        raise EOFError("incomplete WSGI request frame")
    request = json.loads(payload)
    environ = {key: base64.b64decode(value).decode("latin-1")
               for key, value in request["environ"].items()}
    stdin = io.BytesIO(base64.b64decode(request["body"] or ""))
    output = ResponseBuffer()
    try:
        BufferedCGIHandler(stdin, output, sys.stderr, environ,
                           multithread=False, multiprocess=False).run(application)
        body = output.getvalue()
    except Exception:
        traceback.print_exc()
        body = b""
    sink.write(struct.pack(">I", len(body)))
    sink.write(body)
    sink.flush()
`

type wsgiWorker struct {
	ctx    context.Context
	cancel context.CancelFunc
	input  *io.PipeWriter
	output *io.PipeReader
	done   chan struct{}
	err    error // available after done closes
	logs   wsgiDiagnostics
}

// Buffer guest logs independently of the host's log writer so a slow log
// consumer cannot block the worker or prevent its cancellation.
type wsgiDiagnostics struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (d *wsgiDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if room := (16 << 10) - d.buffer.Len(); room > 0 {
		_, _ = d.buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (d *wsgiDiagnostics) take() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	log := d.buffer.String()
	d.buffer.Reset()
	return log
}

func (c *Computer) startWSGIWorker(app string) (*wsgiWorker, error) {
	c.wsgiMu.Lock()
	defer c.wsgiMu.Unlock()
	if c.wsgiClosed {
		return nil, errors.New("computer is closed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	input, writeInput := io.Pipe()
	readOutput, output := io.Pipe()
	worker := &wsgiWorker{ctx: ctx, cancel: cancel, input: writeInput, output: readOutput, done: make(chan struct{})}
	if c.wsgiWorkers == nil {
		c.wsgiWorkers = make(map[*wsgiWorker]struct{})
	}
	c.wsgiWorkers[worker] = struct{}{}

	go func() {
		defer close(worker.done)
		defer func() {
			c.wsgiMu.Lock()
			delete(c.wsgiWorkers, worker)
			c.wsgiMu.Unlock()
		}()
		defer cancel()
		defer input.Close()
		defer output.Close()
		err := c.runWSGIWorker(ctx, app, input, output, &worker.logs)
		if err != nil && ctx.Err() == nil {
			worker.err = err
			_ = output.CloseWithError(err)
		}
	}()
	return worker, nil
}

func (c *Computer) runWSGIWorker(ctx context.Context, app string, stdin io.Reader, stdout, stderr io.Writer) error {
	if _, err := c.Session(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	runtime := c.runtime
	c.mu.Unlock()
	session, err := runtime.NewSession(ctx)
	if err != nil {
		return err
	}
	result, err := session.Exec(ctx, &gbash.ExecutionRequest{
		Script: "python3 -c '" + wsgiLauncher + "' \"$1\"", Args: []string{app},
		WorkDir: c.cfg.WorkDir, Stdin: stdin, Stdout: stdout, Stderr: stderr,
	})
	if err == nil && result.ExitCode != 0 {
		err = fmt.Errorf("Python exited with code %d", result.ExitCode)
	}
	return err
}

func (c *Computer) closeWSGIWorkers() {
	c.wsgiMu.Lock()
	c.wsgiClosed = true
	workers := make([]*wsgiWorker, 0, len(c.wsgiWorkers))
	for worker := range c.wsgiWorkers {
		workers = append(workers, worker)
	}
	c.wsgiMu.Unlock()
	for _, worker := range workers {
		worker.stop()
	}
	for _, worker := range workers {
		<-worker.done
	}
}

func (w *wsgiWorker) stop() {
	w.cancel()
	_ = w.input.Close()
	_ = w.output.Close()
}

func (w *wsgiWorker) request(ctx context.Context, env map[string]string, body []byte) ([]byte, error) {
	environ := make(map[string][]byte, len(env))
	for name, value := range env {
		environ[name] = []byte(value)
	}
	payload, err := json.Marshal(struct {
		Environ map[string][]byte `json:"environ"`
		Body    []byte            `json:"body"`
	}{environ, body})
	if err != nil {
		return nil, err
	}

	// Closing both pipes unblocks the exchange and cancels Python even if
	// the app is still computing or blocked on a tool call.
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		w.stop()
		close(canceled)
	})
	var size uint32
	err = binary.Write(w.input, binary.BigEndian, uint32(len(payload)))
	if err == nil {
		_, err = w.input.Write(payload)
	}
	if err == nil {
		err = binary.Read(w.output, binary.BigEndian, &size)
	}
	if err == nil && size > wsgiResponseLimit {
		err = errors.New("WSGI response exceeds 1 MiB")
	}
	var output []byte
	if err == nil {
		output = make([]byte, size)
		_, err = io.ReadFull(w.output, output)
	}
	if !stop() {
		<-canceled
		return nil, ctx.Err()
	}
	if err != nil {
		w.stop()
		select {
		case <-w.done:
			if w.err != nil {
				err = w.err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return output, err
}
