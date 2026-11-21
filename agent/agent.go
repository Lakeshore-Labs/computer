// Package agent runs an agent as a command of the computer: a JavaScript program on QuickJS, itself a WebAssembly
// program, whose only way to act is to ask this command, one JSON message a line on its standard streams. The
// command runs the agent's shell commands in the same computer, reads and writes its files there, and makes its
// model requests with a key the agent never sees.
//
// A request is {"id", "op", "arg"} and is answered by {"id", "result"} or {"id", "error"}, after any number of
// {"id", "event"}. A line {"event"} is progress, shown to whoever ran the command. When the
// command is stopped it sends {"stop"}, and the agent ends its run and exits.
package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	// The model's limits; zero leaves the agent program's default. ContextWindow is how many tokens the model
	// reads, from which Pi Durable decides when to compact the conversation; MaxTokens is the most the model may
	// write in one reply; MaxTurns is the most tool rounds one run takes.
	ContextWindow, MaxTokens, MaxTurns int

	Instructions string // what the agent is told about this computer
	Home         string // the agent's working directory
	// Cache is a host directory in which the bundle is kept compiled to the engine's bytecode, so that a run does
	// not parse it again; empty keeps it in a temporary directory for the life of the process.
	Cache string
	// State is a host directory in which the conversations are kept, each in a directory of its own. It is the agent program's alone: no command
	// and none of the agent's own tools can read or change it, so what the model does cannot alter the instructions
	// and tools the next run starts with.
	State string
}

const (
	scripts   = "/usr/lib/agent"
	state     = "/var/lib/agent" // where the agent program sees Config.State
	stopGrace = 5 * time.Second  // how long a stopped agent has to record its run as aborted
)

// conversations are the names of the conversations kept in state, oldest first: each is a directory named for
// the time it began.
func conversations(state string) []string {
	entries, _ := os.ReadDir(state) // sorted by name
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// Command is the agent as the command name: `name "message"` answers one message in a new conversation, and
// `name -c "message"` in the latest one, as the agents people run at a terminal do.
func Command(name string, cfg Config) commands.Command {
	return commands.DefineCommand(name, func(ctx context.Context, inv *commands.Invocation) error {
		// The answer goes to standard output and the steps taken to standard error, so a person watches the run
		// and a pipe receives the answer alone. -q shows no steps. --json prints the whole run as JSON lines, for
		// a program: the transcript entries as they are added and the live state, as Pi Durable's JSON mode does.
		args, quiet, asJSON, conversation := inv.Args, false, false, ""
	flags:
		for ; len(args) > 0; args = args[1:] {
			kept := conversations(cfg.State)
			switch args[0] {
			case "-q":
				quiet = true
			case "--json":
				asJSON = true
			case "--help", "-h":
				fmt.Fprintf(inv.Stdout, "usage: %s [-q] [--json] [-c | -r NAME] \"message\"\n       %s --list\n\nAnswers one message in a new conversation and prints the answer; the steps taken go to standard error.\n  -q        show no steps\n  --json    print the whole run as JSON lines\n  -c        continue the latest conversation\n  -r NAME   continue the conversation NAME\n  --list    print the conversations' names, newest first\n", name, name)
				return nil
			case "--list":
				for _, c := range slices.Backward(kept) {
					fmt.Fprintln(inv.Stdout, c)
				}
				return nil
			case "-c", "--continue":
				if len(kept) == 0 {
					return commands.Exitf(inv, 1, "%s: there is no conversation to continue", name)
				}
				conversation = kept[len(kept)-1]
			case "-r", "--resume":
				if len(args) < 2 || !slices.Contains(kept, args[1]) {
					return commands.Exitf(inv, 1, "%s: -r needs the name of a conversation; %s --list prints them", name, name)
				}
				conversation, args = args[1], args[1:]
			default:
				break flags
			}
		}
		// A conversation is a directory of State, made here for a new one.
		dir := cfg.State
		if cfg.State != "" {
			if conversation == "" {
				conversation = time.Now().UTC().Format("20060102T150405.000000")
			}
			dir = filepath.Join(cfg.State, conversation)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return commands.Exitf(inv, 1, "%s: %v", name, err)
			}
		}
		// One agent at a time over one conversation: a second would write the same files, and a run that starts
		// agents without end would take the host's memory.
		busy, _ := running.LoadOrStore(cfg.State, new(sync.Mutex))
		if !busy.(*sync.Mutex).TryLock() {
			return commands.Exitf(inv, 1, "%s: an agent is already running in this computer", name)
		}
		defer busy.(*sync.Mutex).Unlock()
		if dir != "" { // and one at a time among the processes given this conversation
			release, err := lock(dir)
			if err != nil {
				return commands.Exitf(inv, 1, "%s: an agent is already running over this conversation, in another process", name)
			}
			defer release()
		}
		entry := cfg.Entry
		if entry == "" {
			entry = "agent.js"
		}
		compiled, err := bytecode(ctx, cfg, entry)
		if err != nil {
			return commands.Exitf(inv, 1, "%s: %v", name, err)
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
		// is stopped where it stands.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		run, kill := context.WithCancel(context.WithoutCancel(ctx))
		defer kill()
		ended := make(chan struct{})
		go func() {
			<-ctx.Done()
			// The request is written beside the wait, not before it: an agent that is not reading, with a reply
			// already owed to it, would otherwise hold this up for ever.
			go reply(map[string]any{"stop": true})
			select {
			case <-ended:
			case <-time.After(stopGrace):
			}
			_ = replies.Close()
			kill()
		}()

		dirs := append(cfg.Mounts[:len(cfg.Mounts):len(cfg.Mounts)], wasm.Dir{Guest: scripts, Host: compiled, ReadOnly: true})
		if dir != "" {
			dirs = append(dirs, wasm.Dir{Guest: state, Host: dir})
		}
		env := map[string]string{"HOME": cfg.Home, "AGENT_STATE": state, "AGENT_MODEL": cfg.Model, "AGENT_INSTRUCTIONS": cfg.Instructions}
		for name, n := range map[string]int{"AGENT_CONTEXT_WINDOW": cfg.ContextWindow, "AGENT_MAX_TOKENS": cfg.MaxTokens, "AGENT_MAX_TURNS": cfg.MaxTurns} {
			if n > 0 {
				env[name] = strconv.Itoa(n)
			}
		}
		exit := make(chan int, 1)
		var failed error // why the engine could not run the agent, when it was not stopped
		go func() {
			code, err := cfg.Engine.Run(run, wasm.Run{
				Args:  append([]string{"qjs", scripts + "/load.js", scripts + "/" + entry}, args...),
				Env:   env,
				Stdin: toAgent, Stdout: fromAgent, Stderr: inv.Stderr,
				Dirs: dirs,
			})
			if run.Err() == nil {
				failed = err
			}
			_ = fromAgent.Close()
			close(ended)
			exit <- code
		}()

		var requested sync.WaitGroup
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
				switch steps, answer := readable(m.Event); {
				case len(m.Event) == 0:
				case asJSON:
					fmt.Fprintf(inv.Stdout, "%s\n", m.Event)
				default:
					if !quiet {
						fmt.Fprint(inv.Stderr, steps)
					}
					fmt.Fprint(inv.Stdout, answer)
				}
				continue
			}
			requested.Add(1)
			ctx, done := context.WithCancel(ctx)
			inFlight.Store(m.ID, done)
			go func() {
				defer requested.Done()
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
		if lines.Err() != nil { // a line too long to be a request: the agent is stopped, not left writing it
			kill()
			go func() { _, _ = io.Copy(io.Discard, requests) }()
		}
		// Nothing reads what is still being written to the agent, which has ended or is being stopped. Closing
		// the pipe releases a reply that is waiting for room in it.
		_ = toAgent.Close()
		_ = replies.Close()
		requested.Wait()
		code := <-exit
		if failed != nil {
			return commands.Exitf(inv, code, "%s: %v", name, failed)
		}
		if code != 0 {
			return &commands.ExitError{Code: code}
		}
		return nil
	})
}

// compileJS compiles the module at its first argument to bytecode at its second. loadJS runs such a module: it is
// what the engine is started with, and it takes itself out of the arguments so the module sees its own as before.
const (
	compileJS = `import * as std from "qjs:std";
import * as bjson from "qjs:bjson";
const module = std.evalScript(std.loadFile(scriptArgs[1]), { compile_only: true, compile_module: true });
const bytes = bjson.write(module, bjson.WRITE_OBJ_BYTECODE | bjson.WRITE_OBJ_STRIP_SOURCE);
const out = std.open(scriptArgs[2], "wb");
out.write(bytes, 0, bytes.byteLength);
out.close();
`
	loadJS = `import * as std from "qjs:std";
import * as bjson from "qjs:bjson";
const bytes = std.loadFile(scriptArgs[1] + ".bc", { binary: true });
scriptArgs.splice(0, 1);
std.evalScript(bjson.read(bytes.buffer, 0, bytes.byteLength, bjson.READ_OBJ_BYTECODE), { eval_module: true });
`
)

var compiling sync.Mutex

// bytecode is a host directory holding entry compiled to the engine's bytecode, and the loader that runs it.
// Parsing the bundle's two megabytes of source took a run 0.4 s, and 2 s on an engine that can be stopped; loading
// the bytecode takes a tenth of that. Bytecode is good only for the engine that wrote it, so it is made by the
// engine given and kept under the engine's and the bundle's sum.
func bytecode(ctx context.Context, cfg Config, entry string) (string, error) {
	source, err := os.ReadFile(filepath.Join(cfg.Script, entry))
	if err != nil {
		return "", err
	}
	compiling.Lock()
	defer compiling.Unlock()
	cache := cfg.Cache
	if cache == "" { // ponytail: left behind when the process ends; an embedder that minds names a Cache
		if temporary == "" {
			if temporary, err = os.MkdirTemp("", "computer-agent-bytecode"); err != nil {
				return "", err
			}
		}
		cache = temporary
	}
	engine := cfg.Engine.Sum()
	dir := filepath.Join(cache, fmt.Sprintf("%x", sha256.Sum256(append(engine[:], source...))))
	if _, err := os.Stat(filepath.Join(dir, entry+".bc")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(cache, "compiling")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	for file, text := range map[string]string{"compile.js": compileJS, "load.js": loadJS} {
		if err := os.WriteFile(filepath.Join(work, file), []byte(text), 0o600); err != nil {
			return "", err
		}
	}
	var failed strings.Builder
	code, err := cfg.Engine.Run(ctx, wasm.Run{Args: []string{"qjs", "/out/compile.js", "/in/" + entry, "/out/" + entry + ".bc"},
		Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: &failed,
		Dirs: []wasm.Dir{{Guest: "/in", Host: cfg.Script, ReadOnly: true}, {Guest: "/out", Host: work}}})
	if err != nil || code != 0 {
		return "", fmt.Errorf("the agent's bundle could not be compiled: %s %v", strings.TrimSpace(failed.String()), err)
	}
	_ = os.Remove(filepath.Join(work, "compile.js"))
	if err := os.Rename(work, dir); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	return dir, nil
}

// temporary is the directory bytecode is kept in when an agent names no Cache.
var temporary string

// running holds, for each State directory, the lock of the agent at work over it.
var running sync.Map

// readable is one line of a run as a person reads it: the steps the agent took and what came of them, and the
// answer, when the line holds it.
func readable(event json.RawMessage) (steps, answer string) {
	var e struct {
		Entries []struct {
			Model []struct {
				Role    string
				Content json.RawMessage
			}
		}
		Answer, Unanswered *string
	}
	_ = json.Unmarshal(event, &e)
	var b strings.Builder
	for _, entry := range e.Entries {
		for _, m := range entry.Model {
			var parts []struct {
				Type, Name, Text string
				Arguments        map[string]any
			}
			_ = json.Unmarshal(m.Content, &parts) // a person's message is plain text, and is not shown again
			for _, p := range parts {
				switch {
				case p.Type == "toolCall" && p.Arguments["command"] != nil:
					fmt.Fprintf(&b, "\x1b[33m$ %v\x1b[0m\n", p.Arguments["command"])
				case p.Type == "toolCall":
					arguments, _ := json.Marshal(p.Arguments)
					fmt.Fprintf(&b, "\x1b[33m%s %.300s\x1b[0m\n", p.Name, arguments)
				case p.Type == "text" && m.Role == "toolResult":
					lines := strings.Split(strings.TrimRight(p.Text, "\n"), "\n")
					if len(lines) > 12 {
						lines = append(lines[:12], fmt.Sprintf("… %d more lines", len(lines)-12))
					}
					fmt.Fprintf(&b, "\x1b[2m%s\x1b[0m\n", strings.Join(lines, "\n"))
				}
			}
		}
	}
	if e.Unanswered != nil {
		fmt.Fprintf(&b, "no answer: %s\n", *e.Unanswered)
	}
	if e.Answer != nil {
		answer = strings.TrimRight(*e.Answer, "\n") + "\n"
	}
	return b.String(), answer
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

// modelClient follows no redirect: the key goes to the address the embedder named and nowhere a reply points.
var modelClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

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
	address, err := url.JoinPath(cfg.ModelURL, "chat/completions")
	if err != nil {
		return errors.New("the model's address is not a URL")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", address, bytes.NewReader(in.Params))
	if err != nil {
		return errors.New("the model's address is not a URL")
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.ModelKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.ModelKey)
	}
	res, err := modelClient.Do(req)
	if err != nil {
		// What failed is said without the address, which the agent, and through its transcript the model, is
		// not to learn.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return errors.New("the request to the model was stopped")
		}
		return errors.New("the model could not be reached")
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
