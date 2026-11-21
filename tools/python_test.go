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

func TestThePackageNamesItsModulesAsPythonSpellsThem(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tools")
	tool := Tool{Tool: &mcp.Tool{Name: "fetch", InputSchema: map[string]any{"type": "object"}}, Group: "my-server", Short: "fetch"}
	if err := WritePython(dir, []Tool{tool}); err != nil {
		t.Fatal(err)
	}
	init, err := os.ReadFile(filepath.Join(dir, "__init__.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(init), `MODULES = ["my_server"]`) || !strings.Contains(string(init), "Modules: my_server") || strings.Contains(string(init), "my-server") {
		t.Errorf("the package does not name the module my_server:\n%s", init)
	}
	if _, err := os.Stat(filepath.Join(dir, "my_server.py")); err != nil {
		t.Error(err)
	}
}

func TestThePackageIsNeverMissingWhileItIsRewritten(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tools")
	tools := []Tool{{Tool: &mcp.Tool{Name: "fetch", InputSchema: map[string]any{"type": "object"}}, Group: "wiki", Short: "fetch"}}
	if err := WritePython(dir, tools); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 500 {
			if err := WritePython(dir, tools); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for missing := 0; ; {
		select {
		case <-done:
			if missing > 0 {
				t.Errorf("a module was missing or empty %d times while the package was rewritten", missing)
			}
			return
		default:
		}
		for _, name := range []string{"__init__.py", "wiki.py"} {
			if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || len(b) == 0 {
				missing++
			}
		}
	}
}

func TestExtensionIsTheSubtypeAndNothingElse(t *testing.T) {
	for mimeType, want := range map[string]string{"IMAGE/JPEG; q=1": ".jpeg", "image/png": ".png", "image/svg+xml": ".svg-xml", "../../x": ".x", "": ""} {
		if got := extension(mimeType); got != want {
			t.Errorf("extension(%q) = %q, want %q", mimeType, got, want)
		}
	}
}
