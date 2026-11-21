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
// keeps the requests it was sent. A reply is a shell command for the agent to run, the text of an answer, "hang",
// which never answers, or "overloaded", which refuses with a 503 and asks to be tried again in a second.
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
		if reply == "overloaded" {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "try again", http.StatusServiceUnavailable)
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
			engine.module, engine.err = wasm.Compile(context.Background(), wasm.Program{Binary: b, MemoryPages: 8192})
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
	res := exec(t, c, `agent --json "make a file"`, time.Minute)
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
	exec(t, c, `agent --json "remember the word heliotrope"`, time.Minute)
	res := exec(t, c, `agent --json "what was the word?"`, time.Minute)
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
	// The run is stopped once its request has reached the model, which then never answers.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var stopped time.Time
	go func() {
		for len(m.sent()) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		stopped = time.Now()
		stop()
	}()
	res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: `agent --json "this one is stopped"`})
	if took := time.Since(stopped); took > 4*time.Second {
		t.Fatalf("a stopped run took %v to end (%v)", took, err)
	}
	if res != nil && res.ExitCode == 0 {
		t.Error("a stopped run exited 0")
	}
	res = exec(t, c, `agent --json "and this one is answered"`, time.Minute)
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
	res := exec(t, c, "conformance --json /home/user", 2*time.Minute)
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

func TestTheAgentTriesAnOverloadedModelAgain(t *testing.T) {
	m := newModel(t, "overloaded", "Here now.")
	c, _ := agentComputer(t, m)
	start := time.Now()
	res := exec(t, c, `agent --json "are you there?"`, time.Minute)
	if got := lastLine(t, res.Stdout)["answer"]; got != "Here now." {
		t.Fatalf("answer %v, stdout %q, stderr %q", got, res.Stdout, res.Stderr)
	}
	if took := time.Since(start); len(m.sent()) != 2 || took < time.Second {
		t.Errorf("%d requests in %v; the second should come a second after the first was refused", len(m.sent()), took.Round(100*time.Millisecond))
	}
}

func TestTheAgentPrintsItsAnswerAndShowsItsStepsApart(t *testing.T) {
	m := newModel(t, "$ echo working", "All done.")
	c, _ := agentComputer(t, m)
	res := exec(t, c, `agent "do it" > answer.txt; cat answer.txt`, time.Minute)
	if res.Stdout != "All done.\n" {
		t.Errorf("standard output holds %q, not the answer alone", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "$ echo working") || !strings.Contains(res.Stderr, "working") {
		t.Errorf("the steps were not shown: %q", res.Stderr)
	}
	m.replies = []string{"Quietly."}
	if res := exec(t, c, `agent -q "again"`, time.Minute); res.Stdout != "Quietly.\n" || res.Stderr != "" {
		t.Errorf("-q printed %q and showed %q", res.Stdout, res.Stderr)
	}
}

// The third review.

func TestTheModelCannotChangeWhatTheNextRunIsToldOrFindItsConversation(t *testing.T) {
	m := newModel(t, "$ find / -name '*.jsonl' 2>/dev/null | head -5; ls -a /var/lib/agent /home/user 2>&1", "Looked.", "Second.")
	c, home := agentComputer(t, m)
	exec(t, c, `agent -q "look for your own conversation"`, time.Minute)
	sent := m.sent()
	if len(sent) < 2 || strings.Contains(sent[1], "main.jsonl") || !strings.Contains(sent[1], "/var/lib/agent") {
		t.Errorf("the model's command found the conversation's files")
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("the conversation was kept in the working directory: %v", entries)
	}
	exec(t, c, `agent -q "again"`, time.Minute)
	if sent = m.sent(); !strings.Contains(sent[len(sent)-1], "You work on a computer through a shell") {
		t.Error("the second run was not told about the computer")
	}
}

func TestOneAgentRunsAtATime(t *testing.T) {
	m := newModel(t, "$ agent -q nested; echo status $?", "Outer done.")
	c, _ := agentComputer(t, m)
	res := exec(t, c, `agent --json "start another agent"`, time.Minute)
	if got := lastLine(t, res.Stdout)["answer"]; got != "Outer done." {
		t.Fatalf("answer %v, stderr %q", got, res.Stderr)
	}
	sent := m.sent()
	if len(sent) != 2 || !strings.Contains(sent[1], "already running") || !strings.Contains(sent[1], "status 1") {
		t.Errorf("the nested agent was not refused: %d requests", len(sent))
	}
}

func TestTheModelsKeyIsNotSentWhereAReplyRedirects(t *testing.T) {
	var got string
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Header.Get("Authorization") }))
	t.Cleanup(other.Close)
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirecting.Close)
	m := &model{Server: redirecting}
	c, _ := agentComputer(t, m)
	res := exec(t, c, `agent -q "hello"`, time.Minute)
	if got != "" {
		t.Errorf("the key followed the redirect: %q", got)
	}
	if strings.Contains(res.Stdout+res.Stderr, "127.0.0.1") {
		t.Errorf("the model's address reached the agent's output: %q", res.Stderr)
	}
}
