package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ewhauser/gbash"
	"github.com/ewhauser/gbash/commands"

	"github.com/Lakeshore-Labs/computer/agent"
	"github.com/Lakeshore-Labs/computer/wasm"
)

// A model is a stand-in for a chat-completions service: it answers each request with the next reply in turn and
// keeps the requests it was sent. A reply is a shell command for the agent to run, the text of an answer, or
// "hang", which never answers.
type model struct {
	mu       sync.Mutex
	replies  []string
	requests []string
	*httptest.Server
}

func newModel(t *testing.T, replies ...string) *model {
	m := &model{replies: replies}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.requests = append(m.requests, string(body))
		reply := "no more replies"
		if len(m.replies) > 0 {
			reply, m.replies = m.replies[0], m.replies[1:]
		}
		m.mu.Unlock()
		if reply == "hang" {
			<-r.Context().Done()
			return
		}
		delta, finish := map[string]any{"role": "assistant", "content": reply}, "stop"
		if command, ok := strings.CutPrefix(reply, "$ "); ok {
			arguments, _ := json.Marshal(map[string]string{"command": command})
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": "bash", "arguments": string(arguments)}}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": delta}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			chunk, _ := json.Marshal(map[string]any{"id": "c", "object": "chat.completion.chunk", "model": "test", "choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *model) sent() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.requests...)
}

var engine struct {
	once   sync.Once
	module *wasm.Module
	err    error
}

// agentComputer is a computer with the command agent, over the model, and the host directory that is its home. The tests
// that use it are skipped unless AGENT_ENGINE names QuickJS built for WASI and AGENT_BUNDLE the directory of agent.js.
func agentComputer(t *testing.T, m *model) (*Computer, string) {
	t.Helper()
	binary, bundle := os.Getenv("AGENT_ENGINE"), os.Getenv("AGENT_BUNDLE")
	if binary == "" || bundle == "" {
		t.Skip("AGENT_ENGINE and AGENT_BUNDLE are not set")
	}
	engine.once.Do(func() {
		var b []byte
		if b, engine.err = os.ReadFile(binary); engine.err == nil {
			engine.module, engine.err = wasm.Compile(context.Background(), wasm.Program{Binary: b, MemoryPages: 8192, Unstoppable: true})
		}
	})
	if engine.err != nil {
		t.Fatal(engine.err)
	}
	home := t.TempDir()
	c, err := New(Config{Mounts: []Mount{{Guest: "/home/user", Host: home}}, WorkDir: "/home/user",
		Agent: &agent.Config{Engine: engine.module, Script: bundle, ModelURL: m.URL, ModelKey: "secret-key", Model: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, home
}

// lastLine is the last JSON line a run of the agent printed.
func lastLine(t *testing.T, out string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("the last line is not JSON: %q", out)
	}
	return last
}

func TestTheAgentAnswersByWorkingInTheComputer(t *testing.T) {
	m := newModel(t, "$ echo made by the agent > made.txt; cat made.txt", "It is done.")
	c, home := agentComputer(t, m)
	res := exec(t, c, `agent "make a file"`, time.Minute)
	if got := lastLine(t, res.Stdout)["answer"]; res.ExitCode != 0 || got != "It is done." {
		t.Fatalf("exit %d, answer %v, stderr %q", res.ExitCode, got, res.Stderr)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "made.txt")); strings.TrimSpace(string(b)) != "made by the agent" {
		t.Errorf("the agent's command did not run in the computer: made.txt holds %q", b)
	}
	sent := m.sent()
	if len(sent) != 2 || !strings.Contains(sent[1], "made by the agent") {
		t.Errorf("the model was not sent the command's output: %d requests", len(sent))
	}
	if !strings.Contains(sent[0], "You work on a computer through a shell") {
		t.Error("the model was not told about the computer")
	}
	if !strings.Contains(res.Stdout, `"pi.tool-result"`) {
		t.Error("the run's entries were not printed as they were added")
	}
	if strings.Contains(res.Stdout+res.Stderr, "secret-key") || strings.Contains(strings.Join(sent, ""), "secret-key") {
		t.Error("the model's key reached the agent or a request body")
	}
}

func TestTheAgentContinuesItsConversation(t *testing.T) {
	m := newModel(t, "Noted.", "You said the word was heliotrope.")
	c, _ := agentComputer(t, m)
	exec(t, c, `agent "remember the word heliotrope"`, time.Minute)
	res := exec(t, c, `agent "what was the word?"`, time.Minute)
	if got := lastLine(t, res.Stdout)["answer"]; got != "You said the word was heliotrope." {
		t.Fatalf("answer %v, stderr %q", got, res.Stderr)
	}
	if sent := m.sent(); len(sent) != 2 || !strings.Contains(sent[1], "remember the word heliotrope") || !strings.Contains(sent[1], "Noted.") {
		t.Error("the second message was not sent with the first and its answer")
	}
}

func TestAStoppedAgentEndsItsRunAndTakesTheNextMessage(t *testing.T) {
	m := newModel(t, "hang", "Back again.")
	c, _ := agentComputer(t, m)
	start := time.Now()
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: `agent "this one is stopped"`, Timeout: 2 * time.Second})
	if took := time.Since(start); took > 6*time.Second {
		t.Fatalf("a stopped run took %v to end (%v)", took, err)
	}
	if res != nil && res.ExitCode == 0 {
		t.Error("a stopped run exited 0")
	}
	res = exec(t, c, `agent "and this one is answered"`, time.Minute)
	if got := lastLine(t, res.Stdout)["answer"]; got != "Back again." {
		t.Fatalf("answer %v, stdout %q, stderr %q", got, res.Stdout, res.Stderr)
	}
	if sent := m.sent(); len(sent) != 2 {
		t.Errorf("the model was asked %d times; the stopped run should not have been taken up again", len(sent))
	}
}

// Pi Durable ships a conformance suite for an environment its tools run in. It is bundled with the agent and run
// here against the computer's, as a command that prints a JSON line a case.
func TestTheAgentsEnvironmentConformsToPiDurable(t *testing.T) {
	m := newModel(t)
	_, _ = agentComputer(t, m) // compiles the engine, or skips
	home := t.TempDir()
	mounts := []Mount{{Guest: "/home/user", Host: home}, {Guest: "/tmp", Host: t.TempDir()}}
	c, err := New(Config{Mounts: mounts, WorkDir: "/home/user", Commands: []commands.Command{agent.Command("conformance", agent.Config{
		Engine: engine.module, Script: os.Getenv("AGENT_BUNDLE"), Entry: "conformance.js", Mounts: mounts, Home: "/home/user"})}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	res := exec(t, c, "conformance /home/user", 2*time.Minute)
	passed, unsupported := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		var r struct {
			Case, Error string
			OK          bool
		}
		if json.Unmarshal([]byte(line), &r) != nil || r.Case == "" {
			t.Errorf("not a case: %q", line)
		} else if strings.HasPrefix(r.Case, "watch ") {
			// Not implemented: the environment reports no file changes. Pi Durable's tools do not use it; a host
			// that reloads resources from the environment would.
			unsupported++
		} else if !r.OK {
			t.Errorf("%s: %s", r.Case, r.Error)
		} else {
			passed++
		}
	}
	if passed != 14 || unsupported != 8 {
		t.Errorf("%d cases passed and %d are unsupported, where 14 and 8 are expected; stderr %q", passed, unsupported, res.Stderr)
	}
}
