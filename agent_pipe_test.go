package computer

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Lakeshore-Labs/gbash"
	"github.com/Lakeshore-Labs/gbash/commands"

	"github.com/Lakeshore-Labs/computer/agent"
	"github.com/Lakeshore-Labs/computer/wasm"
)

// pipeComputer has a command, fake, that runs a stand-in agent program from testdata/agent.
func pipeComputer(t *testing.T, entry string) *Computer {
	if os.Getenv("AGENT_ENGINE") == "" {
		t.Skip("AGENT_ENGINE is not set")
	}
	b, err := os.ReadFile(os.Getenv("AGENT_ENGINE"))
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.Compile(context.Background(), wasm.Program{Binary: b, MemoryPages: 8192})
	if err != nil {
		t.Fatal(err)
	}
	mounts := []Mount{{Guest: "/home/user", Host: t.TempDir()}}
	c, err := New(Config{Mounts: mounts, WorkDir: "/home/user", Commands: []commands.Command{agent.Command("fake", agent.Config{
		Engine: module, Script: "testdata/agent", Entry: entry, Mounts: mounts, Home: "/home/user", State: t.TempDir()})}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// An agent that is computing (not reading its input) while a reply larger than a pipe's buffer is owed to it.
func TestStoppingReachesABusyAgentOwedALargeReply(t *testing.T) {
	c := pipeComputer(t, "busy.js")
	ctx, stop := context.WithCancel(context.Background())
	time.AfterFunc(time.Second, stop)
	done := make(chan struct{})
	start := time.Now()
	go func() {
		_, _ = c.Exec(ctx, &gbash.ExecutionRequest{Script: "fake x"})
		close(done)
	}()
	select {
	case <-done:
		t.Logf("returned %v after start (stop was at 1s, grace is 5s)", time.Since(start).Round(100*time.Millisecond))
	case <-time.After(12 * time.Second):
		t.Fatalf("the agent command had not returned 11s after it was stopped")
	}
}

// An agent that exits while a reply larger than a pipe's buffer is owed to it. Nothing stops the command.
func TestAnAgentThatExitsWhileOwedALargeReplyEndsItsCommand(t *testing.T) {
	c := pipeComputer(t, "quit.js")
	done := make(chan struct{})
	start := time.Now()
	go func() {
		_, _ = c.Exec(context.Background(), &gbash.ExecutionRequest{Script: "fake x"})
		close(done)
	}()
	select {
	case <-done:
		t.Logf("returned after %v", time.Since(start).Round(100*time.Millisecond))
	case <-time.After(8 * time.Second):
		t.Fatalf("the agent program exited, and 8s later its command has not returned")
	}
}
