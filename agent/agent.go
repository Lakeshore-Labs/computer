// Package agent runs an agent as a command of the computer: a JavaScript program on QuickJS, itself a WebAssembly
// program, whose only way to act is to ask this command, one JSON message a line on its standard streams. The
// command runs the agent's shell commands in the same computer, reads and writes its files there, and makes its
// model requests with a key the agent never sees.
//
// A request is {"id", "op", "arg"} and is answered by {"id", "result"} or {"id", "error"}, after any number of
// {"id", "event"}. A line {"event"} is progress, written to the command's standard output as it is. When the
// command is stopped it sends {"stop"}, and the agent ends its run and exits.
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/ewhauser/gbash/commands"

	"github.com/Lakeshore-Labs/computer/wasm"
)

// Config is one agent command.
type Config struct {
	Engine *wasm.Module // QuickJS built as a WASI command, compiled once
	Script string       // the host directory holding the agent's bundle, pi.js, built from agent/js
	Mounts []wasm.Dir   // the computer's directories

	ModelURL string // an OpenAI-compatible address, up to and not including /chat/completions
	ModelKey string // sent as a bearer token; empty sends none
	Model    string // the model's name

	Instructions string // what the agent is told about this computer
	Home         string // the agent's working directory, where it keeps its conversation under .pi
}

const (
	scripts   = "/usr/lib/agent"
	stopGrace = 5 * time.Second // how long a stopped agent has to record its run as aborted
)

// Command is the agent as the command name: `name "message"` answers one message, continuing the conversation.
func Command(name string, cfg Config) commands.Command {
	return commands.DefineCommand(name, func(ctx context.Context, inv *commands.Invocation) error {
		toAgent, replies, err := os.Pipe()
		if err != nil {
			return err
		}
		requests, fromAgent, err := os.Pipe()
		if err != nil {
			return err
		}
		defer toAgent.Close()
		defer requests.Close()

		var writing sync.Mutex
		reply := func(v map[string]any) {
			b, _ := json.Marshal(v)
			writing.Lock()
			defer writing.Unlock()
			_, _ = replies.Write(append(b, '\n'))
		}
		// When the command is stopped the agent is asked to stop, which it does in its own way: the run is aborted
		// and recorded as such, so that the next message starts a new run. An agent that does not exit in time
		// loses its input, and exits on that.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		ended := make(chan struct{})
		go func() {
			<-ctx.Done()
			reply(map[string]any{"stop": true})
			select {
			case <-ended:
			case <-time.After(stopGrace):
			}
			_ = replies.Close()
		}()

		exit := make(chan int, 1)
		go func() {
			code, _ := cfg.Engine.Run(context.WithoutCancel(ctx), wasm.Run{
				Args:  append([]string{"qjs", scripts + "/pi.js"}, inv.Args...),
				Env:   map[string]string{"HOME": cfg.Home, "PI_MODEL": cfg.Model, "PI_INSTRUCTIONS": cfg.Instructions},
				Stdin: toAgent, Stdout: fromAgent, Stderr: inv.Stderr,
				Dirs: append(cfg.Mounts[:len(cfg.Mounts):len(cfg.Mounts)], wasm.Dir{Guest: scripts, Host: cfg.Script, ReadOnly: true}),
			})
			_ = fromAgent.Close()
			close(ended)
			exit <- code
		}()

		var running sync.WaitGroup
		lines := bufio.NewScanner(requests)
		lines.Buffer(nil, 64<<20)
		for lines.Scan() {
			var m struct {
				ID    int
				Op    string
				Arg   json.RawMessage
				Event json.RawMessage
			}
			if json.Unmarshal(lines.Bytes(), &m) != nil {
				continue
			}
			if m.Op == "" {
				if len(m.Event) > 0 {
					fmt.Fprintf(inv.Stdout, "%s\n", m.Event)
				}
				continue
			}
			running.Add(1)
			go func() {
				defer running.Done()
				var result any
				var err error
				switch m.Op {
				case "sh":
					result, err = shell(ctx, inv, m.Arg)
				case "file":
					result = files(ctx, inv, m.Arg)
				case "model":
					err = model(ctx, cfg, m.Arg, func(event json.RawMessage) { reply(map[string]any{"id": m.ID, "event": event}) })
				default:
					err = errors.New("no such request: " + m.Op)
				}
				if err != nil {
					reply(map[string]any{"id": m.ID, "error": err.Error()})
					return
				}
				reply(map[string]any{"id": m.ID, "result": result})
			}()
		}
		running.Wait()
		if code := <-exit; code != 0 {
			return &commands.ExitError{Code: code}
		}
		return nil
	})
}

// shell runs one command line of the agent's in the computer the agent command is running in.
func shell(ctx context.Context, inv *commands.Invocation, arg json.RawMessage) (any, error) {
	var in struct{ Command, Cwd string }
	_ = json.Unmarshal(arg, &in)
	res, err := inv.Exec(ctx, &commands.ExecutionRequest{Script: in.Command, WorkDir: in.Cwd})
	if err != nil {
		return nil, err
	}
	return map[string]any{"exitCode": res.ExitCode, "stdout": res.Stdout, "stderr": res.Stderr}, nil
}

// files is one file request of the agent's, answered {"out": ...} or {"err": "ENOENT" or a message}.
func files(ctx context.Context, inv *commands.Invocation, arg json.RawMessage) any {
	var in struct {
		Op, Path, Data string
		Text           *string
	}
	_ = json.Unmarshal(arg, &in)
	var out any
	var err error
	switch p := path.Clean("/" + in.Path); in.Op {
	case "read":
		var b []byte
		if b, err = inv.FS.ReadFile(ctx, p); err == nil {
			out = base64.StdEncoding.EncodeToString(b)
		}
	case "write":
		b := []byte{}
		if in.Text != nil {
			b = []byte(*in.Text)
		} else if b, err = base64.StdEncoding.DecodeString(in.Data); err != nil {
			break
		}
		if err = inv.FS.MkdirAll(ctx, path.Dir(p), 0o755); err != nil {
			break
		}
		var f io.WriteCloser
		if f, err = inv.FS.OpenFile(ctx, p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644); err == nil {
			_, err = f.Write(b)
			err = errors.Join(err, f.Close())
		}
	case "mkdir":
		err = inv.FS.MkdirAll(ctx, p, 0o755)
	case "stat":
		var i fs.FileInfo
		if i, err = inv.FS.Stat(ctx, p); err == nil {
			out = map[string]any{"name": i.Name(), "isDirectory": i.IsDir(), "size": i.Size(), "mtimeMs": i.ModTime().UnixMilli()}
		}
	default:
		err = errors.New("no such file request: " + in.Op)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return map[string]any{"err": "ENOENT"}
	case err != nil:
		return map[string]any{"err": err.Error()}
	}
	return map[string]any{"out": out}
}

// model makes one chat-completions request for the agent and tells it the response's status, then each chunk of
// the answer as it arrives.
func model(ctx context.Context, cfg Config, arg json.RawMessage, event func(json.RawMessage)) error {
	var in struct{ Params json.RawMessage }
	_ = json.Unmarshal(arg, &in)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(cfg.ModelURL, "/")+"/chat/completions", bytes.NewReader(in.Params))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.ModelKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.ModelKey)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("the model answered %d: %s", res.StatusCode, b)
	}
	status, _ := json.Marshal(map[string]any{"status": res.StatusCode, "headers": map[string]string{}})
	event(status)
	chunks := bufio.NewScanner(res.Body)
	chunks.Buffer(nil, 16<<20)
	for chunks.Scan() {
		if data, ok := strings.CutPrefix(chunks.Text(), "data:"); ok {
			if data = strings.TrimSpace(data); data != "" && data != "[DONE]" && json.Valid([]byte(data)) {
				event(json.RawMessage(data))
			}
		}
	}
	return chunks.Err()
}
