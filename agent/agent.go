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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ewhauser/gbash/commands"

	"github.com/Lakeshore-Labs/computer/wasm"
)

// Config is one agent command.
type Config struct {
	Engine *wasm.Module // QuickJS built as a WASI command, compiled once
	Script string       // the host directory holding the agent's bundle, built from agent/js
	Entry  string       // the bundle's file to run; agent.js when empty
	Mounts []wasm.Dir   // the computer's directories

	ModelURL string // an OpenAI-compatible address, up to and not including /chat/completions
	ModelKey string // sent as a bearer token; empty sends none
	Model    string // the model's name

	Instructions string // what the agent is told about this computer
	Home         string // the agent's working directory, where it keeps its conversation under .agent
}

const (
	scripts   = "/usr/lib/agent"
	stopGrace = 5 * time.Second // how long a stopped agent has to record its run as aborted
)

// Command is the agent as the command name: `name "message"` answers one message, continuing the conversation.
func Command(name string, cfg Config) commands.Command {
	return commands.DefineCommand(name, func(ctx context.Context, inv *commands.Invocation) error {
		entry := cfg.Entry
		if entry == "" {
			entry = "agent.js"
		}
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
				Args:  append([]string{"qjs", scripts + "/" + entry}, inv.Args...),
				Env:   map[string]string{"HOME": cfg.Home, "AGENT_MODEL": cfg.Model, "AGENT_INSTRUCTIONS": cfg.Instructions},
				Stdin: toAgent, Stdout: fromAgent, Stderr: inv.Stderr,
				Dirs: append(cfg.Mounts[:len(cfg.Mounts):len(cfg.Mounts)], wasm.Dir{Guest: scripts, Host: cfg.Script, ReadOnly: true}),
			})
			_ = fromAgent.Close()
			close(ended)
			exit <- code
		}()

		var running sync.WaitGroup
		var inFlight sync.Map // a request's id to what stops it
		lines := bufio.NewScanner(requests)
		lines.Buffer(nil, 64<<20)
		for lines.Scan() {
			var m struct {
				ID     int
				Op     string
				Arg    json.RawMessage
				Event  json.RawMessage
				Cancel int
			}
			if json.Unmarshal(lines.Bytes(), &m) != nil {
				continue
			}
			if m.Cancel != 0 { // the agent gives up one request, as when a person stops a command
				if stop, ok := inFlight.Load(m.Cancel); ok {
					stop.(context.CancelFunc)()
				}
				continue
			}
			if m.Op == "" {
				if len(m.Event) > 0 {
					fmt.Fprintf(inv.Stdout, "%s\n", m.Event)
				}
				continue
			}
			running.Add(1)
			ctx, done := context.WithCancel(ctx)
			inFlight.Store(m.ID, done)
			go func() {
				defer running.Done()
				defer inFlight.Delete(m.ID)
				defer done()
				var result any
				var err error
				switch m.Op {
				case "sh":
					result, err = shell(ctx, inv, m.Arg)
				case "model":
					err = model(ctx, cfg, m.Arg, func(event json.RawMessage) { reply(map[string]any{"id": m.ID, "event": event}) })
				default:
					err = errors.New("no such request: " + m.Op)
				}
				var refused *modelError
				if errors.As(err, &refused) { // the agent's retry reads what the model answered
					reply(map[string]any{"id": m.ID, "error": err.Error(), "detail": refused})
					return
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

// shell runs one command of the agent's in the computer the agent command is running in: a command line through
// the shell, or a program and its arguments as they are.
func shell(ctx context.Context, inv *commands.Invocation, arg json.RawMessage) (any, error) {
	var in struct {
		Command    string
		Argv       []string
		Cwd        string
		Env        map[string]string
		InheritEnv bool
		TimeoutMs  int64
	}
	_ = json.Unmarshal(arg, &in)
	if in.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(in.TimeoutMs)*time.Millisecond)
		defer cancel()
	}
	res, err := inv.Exec(ctx, &commands.ExecutionRequest{Script: in.Command, Command: in.Argv, WorkDir: in.Cwd, Env: in.Env, ReplaceEnv: !in.InheritEnv})
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	if res == nil {
		if timedOut {
			return map[string]any{"exitCode": 124, "stdout": "", "stderr": "", "timedOut": true}, nil
		}
		return nil, err
	}
	return map[string]any{"exitCode": res.ExitCode, "stdout": res.Stdout, "stderr": res.Stderr, "timedOut": timedOut,
		// A program given as arguments that the shell does not have is reported, not run to a status of 127.
		"notFound": res.CommandNotFound || len(in.Argv) > 0 && res.ExitCode == 127 && strings.Contains(res.Stderr, "not found")}, nil
}

// A modelError is a model's refusal of a request: its status and, when it gave one, how long to wait.
type modelError struct {
	Status     int    `json:"status"`
	RetryAfter string `json:"retryAfter,omitempty"`
	body       string
}

func (e *modelError) Error() string {
	return fmt.Sprintf("the model answered %d: %s", e.Status, e.body)
}

// model makes one chat-completions request for the agent and tells it the response's status, then each chunk of
// the answer as it arrives.
func model(ctx context.Context, cfg Config, arg json.RawMessage, event func(json.RawMessage)) error {
	var in struct {
		Params    json.RawMessage
		TimeoutMs int64
	}
	_ = json.Unmarshal(arg, &in)
	if in.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(in.TimeoutMs)*time.Millisecond)
		defer cancel()
	}
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
		return &modelError{Status: res.StatusCode, RetryAfter: res.Header.Get("Retry-After"), body: string(b)}
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
