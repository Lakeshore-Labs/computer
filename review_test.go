package computer

// Each test here is a defect an independent review proved, kept as the check that it stays fixed.

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ewhauser/gbash"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Lakeshore-Labs/computer/tools"
)

func exec(t *testing.T, c *Computer, script string, timeout time.Duration) *gbash.ExecutionResult {
	t.Helper()
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: script, Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestTheShellCannotMakeASymbolicLink(t *testing.T) {
	home := t.TempDir()
	c := newComputer(t, Config{Mounts: []Mount{{Guest: "/data", Host: home}}})
	if res := exec(t, c, "cd /data && ln -s ../../../../etc/passwd esc", 0); res.ExitCode == 0 {
		t.Error("ln -s succeeded")
	}
	if _, err := os.Lstat(filepath.Join(home, "esc")); err == nil {
		t.Error("a symbolic link was left on the host")
	}
}

func TestAProgramCannotHardLinkThroughASymbolicLink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "esc")); err != nil {
		t.Fatal(err)
	}
	c := newComputer(t, Config{Python: python(t), Mounts: []Mount{{Guest: "/data", Host: home}}})
	res := exec(t, c, `cd /data; python3 -c "import os; os.link('esc', 'h'); print(open('h').read()); open('h', 'w').write('OWNED')"`, 0)
	if got, _ := os.ReadFile(outside); string(got) != "TOPSECRET" || strings.Contains(res.Stdout, "TOPSECRET") {
		t.Errorf("the file outside every mount was reached: holds %q, script printed %q", got, res.Stdout)
	}
}

func TestALinkSwappedInDuringARunLeadsNowhere(t *testing.T) {
	parent := t.TempDir()
	home, outside := filepath.Join(parent, "home"), filepath.Join(parent, "out")
	for _, d := range []string{home, outside, filepath.Join(home, "d")} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The host swaps the directory d for a link out of the mount, over and over, while a script reads through d.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		d, link, aside := filepath.Join(home, "d"), filepath.Join(home, "lnk"), filepath.Join(home, "aside")
		_ = os.Symlink(outside, link)
		for {
			select {
			case <-stop:
				return
			default:
				_ = os.Rename(d, aside)
				_ = os.Rename(link, d)
				_ = os.Rename(d, link)
				_ = os.Rename(aside, d)
			}
		}
	}()
	c := newComputer(t, Config{Python: python(t), Mounts: []Mount{{Guest: "/data", Host: home}}})
	res := exec(t, c, `cd /data; python3 -c "
import time
hits = 0
end = time.time() + 3
while time.time() < end:
    try:
        hits += 'TOPSECRET' in open('d/secret.txt').read()
    except OSError:
        pass
print(hits)"`, 0)
	if strings.TrimSpace(res.Stdout) != "0" {
		t.Errorf("read the file outside the mount: %q %q", res.Stdout, res.Stderr)
	}
}

func TestAToolNameCannotBecomePythonCode(t *testing.T) {
	evil := "x\"\"\"\nimport os\nos.environ['INJECTED'] = '1'\n\"\"\"_y"
	c := newComputer(t, Config{Python: python(t), Tools: testTools(t, func(s *mcp.Server) {
		listNotes(s)
		s.AddTool(&mcp.Tool{Name: evil, Description: `Match C:\Users\name and """quotes"""`, InputSchema: map[string]any{"type": "object"}},
			func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			})
	})})
	got := run(t, c, `python3 -c "
import os
from tools import notes
print(os.environ.get('INJECTED'), len(notes.list_notes()))"`)
	if got != "None 3" {
		t.Errorf("got %q", got)
	}
	// Every generated module must at least be valid Python.
	if got := run(t, c, `for f in /usr/lib/python-tools/tools/*.py; do python3 -c "import ast, sys; ast.parse(open(sys.argv[1]).read())" $f || echo BAD $f; done; echo checked`); got != "checked" {
		t.Errorf("a generated module does not parse: %q", got)
	}
}

func TestTheDeadlineReachesASleepingOrBlockedScript(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	blocked, writer := io.Pipe() // standard input that nothing writes to
	t.Cleanup(func() { _ = writer.Close() })
	for name, req := range map[string]*gbash.ExecutionRequest{
		"sleep":  {Script: `python3 -c "import time; time.sleep(30)"`},
		"select": {Script: `python3 -c "import select; select.select([], [], [], 30)"`},
		"stdin":  {Script: `python3 -c "input()"`, Stdin: blocked},
	} {
		req.Timeout = 2 * time.Second
		start := time.Now()
		if _, err := c.Exec(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s: ran %v past a 2s deadline", name, took.Round(100*time.Millisecond))
		}
	}
	if got := run(t, c, "echo still here"); got != "still here" {
		t.Errorf("got %q", got)
	}
}

func TestAToolCommandSurvivesOddArguments(t *testing.T) {
	c := newComputer(t, Config{Tools: testTools(t, listNotes)})
	if got := run(t, c, "notes list-notes --json null --min-id 2 | jq length; echo after"); got != "2\nafter" {
		t.Errorf("got %q", got)
	}
}

func TestAToolCommandReadsFlagsAsTheSchemaAllows(t *testing.T) {
	str := map[string]any{"type": "string"}
	c := newComputer(t, Config{Tools: testTools(t, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "t_echo", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"title":   map[string]any{"anyOf": []any{str, map[string]any{"type": "null"}}},
			"name":    map[string]any{"type": []any{"string", "null"}},
			"free":    map[string]any{},
			"verbose": map[string]any{"type": "boolean"},
			"quiet":   map[string]any{"type": "boolean"},
			"count":   map[string]any{"type": "integer"},
			"json":    str, "help": str, "h": str, "noteId": str, "note_id": str,
		}}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var arguments map[string]any
			_ = json.Unmarshal(req.Params.Arguments, &arguments)
			b, _ := json.Marshal(arguments) // with its keys sorted
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
		})
	})})
	got := run(t, c, "t echo --title 2024 --name true --free null --verbose --quiet=false --count=3 --json text --help x --h y --note-id a --note-id-2 b")
	if want := `{"count":3,"free":"null","h":"y","help":"x","json":"text","name":"true","noteId":"a","note_id":"b","quiet":false,"title":"2024","verbose":true}`; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := run(t, c, "t echo -h"); !strings.Contains(got, "--note-id-2") || strings.Contains(got, "every argument as one JSON object") {
		t.Errorf("help: %s", got)
	}
}

func TestGroupsWhoseNamesSpellTheSameBothGetACommand(t *testing.T) {
	c := newComputer(t, Config{Tools: testTools(t, func(s *mcp.Server) {
		for _, name := range []string{"fooBar_a", "foo.bar_b"} {
			s.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}},
				func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				})
		}
	})})
	got := run(t, c, "foo-bar --help; foo-bar-tools --help")
	if !strings.Contains(got, "\n  a") || !strings.Contains(got, "\n  b") {
		t.Errorf("a group's tools are missing: %q", got)
	}
}

func TestTheShellOutlivesAnMCPServer(t *testing.T) {
	cs := testSession(t, listNotes)
	c := newComputer(t, Config{Tools: []tools.Source{{Session: cs}}})
	run(t, c, "notes list-notes")
	_ = cs.Close()
	if got := run(t, c, "echo still here"); got != "still here" {
		t.Errorf("got %q", got)
	}
}

// The second review.

func TestATrailingSlashDoesNotFollowALinkOutOfAMount(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "outdir")); err != nil {
		t.Fatal(err)
	}
	c := newComputer(t, Config{Python: python(t), Mounts: []Mount{{Guest: "/data", Host: home}}})
	res := exec(t, c, `cd /data; python3 -c "
import os
for attempt in (lambda: os.listdir('outdir/'), lambda: os.rename('outdir/', 'stolen'), lambda: open('stolen/secret.txt').read(),
                lambda: os.rmdir('outdir/'), lambda: open('outdir/secret.txt').read()):
    try:
        print('REACHED', attempt())
    except OSError as e:
        print('refused', e.errno)
"`, 0)
	if strings.Contains(res.Stdout, "REACHED") || !strings.Contains(res.Stdout, "refused") {
		t.Errorf("a path ending in a slash went through the link: %q %q", res.Stdout, res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Errorf("the directory outside the mount was changed: %v", err)
	}
}

func TestAReadOnlyMountCannotBeEmptiedByOpeningAFile(t *testing.T) {
	lib := t.TempDir()
	if err := os.WriteFile(filepath.Join(lib, "keep.txt"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newComputer(t, Config{Python: python(t), Mounts: []Mount{{Guest: "/ro", Host: lib, ReadOnly: true}}})
	exec(t, c, `python3 -c "
import os
for flags, name in ((os.O_RDONLY | os.O_TRUNC, 'keep.txt'), (os.O_RDONLY | os.O_CREAT, 'new.txt'), (os.O_WRONLY | os.O_APPEND, 'keep.txt')):
    try:
        os.close(os.open('/ro/' + name, flags))
    except OSError:
        pass
"; echo x > /ro/shell.txt`, 0)
	if got, _ := os.ReadFile(filepath.Join(lib, "keep.txt")); string(got) != "kept" {
		t.Errorf("keep.txt now holds %q", got)
	}
	if entries, _ := os.ReadDir(lib); len(entries) != 1 {
		t.Errorf("the read-only mount gained files: %v", entries)
	}
}

func TestANamedPipeInAMountDoesNotHoldARun(t *testing.T) {
	home := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(home, "pipe"), 0o600); err != nil {
		t.Skip("no named pipes here:", err)
	}
	c := newComputer(t, Config{Python: python(t), Mounts: []Mount{{Guest: "/data", Host: home}}})
	start := time.Now()
	res := exec(t, c, `cd /data; echo x > reg; python3 -c "
import os
f = open('reg')
os.rename('pipe', 'reg')
try:
    os.set_blocking(f.fileno(), True)
except OSError:
    pass
print(f.read().strip())
try:
    open('reg').read()
except OSError as e:
    print('refused')
"`, 3*time.Second)
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("ran %v past a 3s deadline", took)
	}
	if got := strings.TrimSpace(res.Stdout); got != "x\nrefused" {
		t.Errorf("got %q %q", got, res.Stderr)
	}
}

func TestToolsWhoseNamesSpellTheSameAllStayReachable(t *testing.T) {
	c := newComputer(t, Config{Python: python(t), Tools: testTools(t, func(s *mcp.Server) {
		answer := func(text string) mcp.ToolHandler {
			return func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
			}
		}
		object := map[string]any{"type": "object"}
		s.AddTool(&mcp.Tool{Name: "crm_listNotes", InputSchema: object}, answer("camel"))
		s.AddTool(&mcp.Tool{Name: "crm_list_notes", InputSchema: object}, answer("snake"))
		s.AddTool(&mcp.Tool{Name: "crm_٣x", InputSchema: object}, answer("digit"))
		s.AddTool(&mcp.Tool{Name: "call_now", InputSchema: object}, answer("call"))
		s.AddTool(&mcp.Tool{Name: "fooBar_a", InputSchema: object}, answer("a"))
		s.AddTool(&mcp.Tool{Name: "foo.bar_b", InputSchema: object}, answer("b"))
		s.AddTool(&mcp.Tool{Name: "crm_both", InputSchema: map[string]any{"type": "object",
			"properties": map[string]any{"a_b": map[string]any{"type": "string"}, "aB": map[string]any{"type": "string"}, "class": map[string]any{"type": "string"}}}}, answer("both"))
	})})
	if got := run(t, c, "crm list-notes; crm list-notes-2"); got != "camel\nsnake" {
		t.Errorf("shell: got %q", got)
	}
	got := run(t, c, `python3 -c "
import tools
from tools import crm, call_2, foo_bar, foo_bar_2
print(crm.list_notes(), crm.list_notes_2(), crm.x(), crm.both(a_b='1', a_b_2='2', x_class='3'), call_2.now(), foo_bar.b() + foo_bar_2.a(), callable(tools.call))"`)
	if got != "camel snake digit both call ba True" {
		t.Errorf("python: got %q", got)
	}
}

// The third review.

func TestTwoProgramsAppendingToOneFileLoseNothing(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	script := `python3 -c "
import os, sys
fd = os.open('log.txt', os.O_WRONLY | os.O_CREAT | os.O_APPEND)
for i in range(3000):
    os.write(fd, (sys.argv[1] + '\\n').encode())
" `
	run(t, c, script+"A & "+script+"B & wait")
	if got := run(t, c, "wc -l < log.txt"); got != "6000" {
		t.Errorf("%s lines of 6000 were kept", got)
	}
}
