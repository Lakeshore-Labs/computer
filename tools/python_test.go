package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTheGeneratedCallTakesItsFirstTwoParametersPositionally(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tools")
	tool := Tool{
		Tool: &mcp.Tool{Name: "fetch", InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"source": map[string]any{"type": "string"}},
		}},
		Group: "wiki", Short: "fetch",
	}
	if err := WritePython(dir, []Tool{tool}); err != nil {
		t.Fatal(err)
	}
	init, err := os.ReadFile(filepath.Join(dir, "__init__.py"))
	if err != nil {
		t.Fatal(err)
	}
	// A tool argument named source or tool must not collide with call's own parameters.
	if !strings.Contains(string(init), "def call(source, tool, /, **arguments):") {
		t.Errorf("call does not take source and tool positionally:\n%s", init)
	}
}
