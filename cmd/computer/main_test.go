package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lakeshore-Labs/gbash"

	"github.com/Lakeshore-Labs/computer"
)

func TestTheCacheMayNotBeInsideAMount(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, "cache", "computer")
	other := t.TempDir()
	for name, c := range map[string]struct {
		host string
		ok   bool
	}{"a mount that holds it": {home, false}, "the cache itself": {cache, false}, "a mount within it": {filepath.Join(cache, "wazero"), false}, "a mount elsewhere": {other, true}} {
		if err := os.MkdirAll(c.host, 0o700); err != nil {
			t.Fatal(err)
		}
		err := outside(cache, []computer.Mount{{Guest: "/home/user", Host: c.host}})
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTabRunsOnlyACommandThatCallsTools(t *testing.T) {
	dir := t.TempDir()
	c, err := computer.New(computer.Config{Mounts: []computer.Mount{{Guest: "/home/user", Host: dir}}, WorkDir: "/home/user"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: "true"}); err != nil {
		t.Fatal(err)
	}
	for _, before := range []string{"touch ran-touch -", "$(touch${IFS}ran-substitution) -", "touch "} {
		candidates(ctx, c, "/home/user", before, before[len(before)-1:])
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("completing a line ran a command: %s exists", e.Name())
	}
}

func TestTheConfigFileIsReadAsClaudeCodesIs(t *testing.T) {
	t.Setenv("WIKI_TOKEN", "secret")
	path := filepath.Join(t.TempDir(), "computer.json")
	if err := os.WriteFile(path, []byte(`{
		"mounts": ["./work:/home/user", "./docs:/reference:ro"],
		"mcpServers": {
			"wiki": {"type": "http", "url": "https://example.com/mcp", "headers": {"Authorization": "Bearer ${WIKI_TOKEN}"}},
			"everything": {"command": "npx", "args": ["-y", "a package"], "env": {"KEY": "$WIKI_TOKEN"}}
		}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mounts, servers, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[1] != "./docs:/reference:ro" {
		t.Errorf("mounts: %q", mounts)
	}
	if len(servers) != 2 || servers[0].name != "everything" || servers[1].name != "wiki" {
		t.Fatalf("servers: %+v", servers)
	}
	if e := servers[0]; e.Command != "npx" || len(e.Args) != 2 || e.Args[1] != "a package" || e.Env["KEY"] != "secret" {
		t.Errorf("the program: %+v", e)
	}
	if w := servers[1]; w.URL != "https://example.com/mcp" || w.Headers["Authorization"] != "Bearer secret" {
		t.Errorf("the URL: %+v", w)
	}

	for name, text := range map[string]string{"a misspelt key": `{"mcpServer": {}}`, "a name that is not a command's": `{"mcpServers": {"a b": {"url": "https://example.com"}}}`} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readConfig(path); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// gbash's bash tool imports its contrib python, which embeds a native library and a loader for it. The binary must
// not carry either, though neither is ever called. go-pdfium has a second backend that loads a native pdfium, and
// plugin support that starts a process; only its WebAssembly backend may be linked.
func TestTheBinaryLinksNoNativeLibrary(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if strings.Contains(pkg, "purego") || strings.Contains(pkg, "gomonty") ||
			strings.Contains(pkg, "go-pdfium/internal/implementation_cgo") || strings.Contains(pkg, "go-plugin") {
			t.Errorf("the binary links %s", pkg)
		}
	}
}

func TestAMountWithoutAGuestPathIsSeenAtItsHostPath(t *testing.T) {
	dir := t.TempDir()
	for value, want := range map[string]computer.Mount{
		dir:                 {Host: dir, Guest: dir},
		dir + ":ro":         {Host: dir, Guest: dir, ReadOnly: true},
		dir + ":/home/user": {Host: dir, Guest: "/home/user"},
		dir + ":/ref:ro":    {Host: dir, Guest: "/ref", ReadOnly: true},
	} {
		if got, err := parseMount(value); err != nil || got != want {
			t.Errorf("%s: %+v, %v", value, got, err)
		}
	}
	if got, err := parseMount("work"); err != nil || !filepath.IsAbs(got.Guest) || got.Host != "work" {
		t.Errorf("a relative directory: %+v, %v", got, err)
	}
	for _, bad := range []string{"", "a:b:c", "a:b:c:ro"} {
		if _, err := parseMount(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestInterruptReturnsToThePrompt(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("the pseudo-terminal driver needs Python 3")
	}
	binary := filepath.Join(t.TempDir(), "computer")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	for _, mode := range []string{"blocking", "nonblocking", "inherited-only", "sigterm", "sighup-ignored"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, python, "testdata/interrupt.py", binary, t.TempDir(), mode).CombinedOutput(); err != nil {
				t.Fatalf("interactive shell: %v: %s", err, out)
			}
		})
	}
}

// A mount that holds the cache, such as the home directory, costs only the cache of compiled Python.
func TestAMountHoldingTheCacheStillStarts(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "computer")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	home := t.TempDir()
	cmd := exec.Command(binary, "-mount", home+":/home/user", "-c", "python3 -c 'print(\"ok\")'")
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, ".cache"))
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("%v: %s", err, out)
	}
}
