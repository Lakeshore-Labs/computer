package computer

import (
	"context"
	"errors"
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
	"github.com/ewhauser/gbash/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
	"github.com/tetratelabs/wazero/api"

	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/Lakeshore-Labs/computer/wasm"
)

type note struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// testTools is an MCP server in this process, whose tools are added by add, as an unnamed source.
func testTools(t *testing.T, add func(*mcp.Server)) []tools.Source {
	return []tools.Source{{Session: testSession(t, add)}}
}

func testSession(t *testing.T, add func(*mcp.Server)) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	add(server)
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func listNotes(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "notes_listNotes", Description: "List the notes."},
		func(_ context.Context, _ *mcp.CallToolRequest, in struct {
			MinID int `json:"minId,omitempty" jsonschema:"the lowest id to list"`
		}) (*mcp.CallToolResult, []note, error) {
			all := []note{{1, "first"}, {2, "second"}, {3, "third"}}
			return nil, all[max(in.MinID, 1)-1:], nil
		})
}

func run(t *testing.T, c *Computer, script string) string {
	t.Helper()
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: script})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("%s: exit %d: %s", script, res.ExitCode, res.Stderr)
	}
	return strings.TrimSpace(res.Stdout)
}

func newComputer(t *testing.T, cfg Config) *Computer {
	t.Helper()
	cfg.Mounts = append(cfg.Mounts, Mount{Guest: "/home/user", Host: t.TempDir()})
	cfg.WorkDir = "/home/user"
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestAToolIsACommand(t *testing.T) {
	c := newComputer(t, Config{Tools: testTools(t, listNotes)})
	if got := run(t, c, "notes list-notes --min-id 2 | jq -r '.[].title' | sort | tr '\\n' ' '"); got != "second third" {
		t.Errorf("got %q", got)
	}
	if got := run(t, c, "notes list-notes --help"); !strings.Contains(got, "--min-id") || !strings.Contains(got, "the lowest id to list") {
		t.Errorf("help does not describe the argument: %q", got)
	}
}

func TestAToolAddedLaterIsACommand(t *testing.T) {
	var server *mcp.Server
	c := newComputer(t, Config{Tools: testTools(t, func(s *mcp.Server) { server = s; listNotes(s) })})
	run(t, c, "notes list-notes")
	mcp.AddTool(server, &mcp.Tool{Name: "tasks_count", Description: "Count the tasks."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]int, error) {
			return nil, map[string]int{"count": 7}, nil
		})
	if got := run(t, c, "tasks count | jq .count"); got != "7" {
		t.Errorf("got %q", got)
	}
}

var cpython struct {
	once   sync.Once
	python *Python
	err    error
}

// python is the CPython build named by PYTHON_WASM and PYTHON_LIB, compiled once for all the tests; those that
// need it are skipped without it.
func python(t *testing.T) *Python {
	t.Helper()
	binary, lib := os.Getenv("PYTHON_WASM"), os.Getenv("PYTHON_LIB")
	if binary == "" || lib == "" {
		t.Skip("PYTHON_WASM and PYTHON_LIB are not set")
	}
	cpython.once.Do(func() {
		cpython.python, cpython.err = LoadPython(context.Background(), binary, lib, wasm.Program{
			MemoryPages: 8192, MaxRuns: 2, CacheDir: os.Getenv("PYTHON_CACHE"), HostModule: os.Getenv("PYTHON_HOST_MODULE"), Features: api.CoreFeaturesV2,
		})
	})
	if cpython.err != nil {
		t.Fatal(cpython.err)
	}
	return cpython.python
}

func TestPythonCallsAToolAndSharesTheShellsFiles(t *testing.T) {
	c := newComputer(t, Config{Tools: testTools(t, listNotes), Python: python(t)})
	run(t, c, `cat > names.py <<'EOF'
from tools import notes
open("names.txt", "w").write(",".join(n["title"] for n in notes.list_notes(min_id=2)))
EOF
python3 names.py`)
	if got := run(t, c, "cat names.txt"); got != "second,third" {
		t.Errorf("got %q", got)
	}
}

func TestSQLiteKeepsADatabaseInAMount(t *testing.T) {
	c := newComputer(t, Config{})
	run(t, c, `sqlite3 t.db "create table n(x); insert into n values (1),(2),(3)"`)
	if got := run(t, c, `sqlite3 t.db "select sum(x) from n"`); got != "6" {
		t.Errorf("got %q", got)
	}
}

func TestAProgramCannotWriteAReadOnlyMount(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: "echo x > /lib/planted; python3 -c \"open('/lib/planted','w')\""})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Error("a write to a read-only mount succeeded")
	}
	if got := run(t, c, "ls /lib | grep -c planted || true"); got != "0" {
		t.Errorf("the file was written: %q", got)
	}
}

func TestCurlReachesOnlyAllowedURLs(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<h1>hello</h1>")) }))
	t.Cleanup(site.Close)
	c := newComputer(t, Config{Options: []gbash.Option{gbash.WithHTTPAccess(site.URL + "/ok")}})
	if got := run(t, c, "curl -s "+site.URL+"/ok | html-to-markdown"); got != "# hello" {
		t.Errorf("got %q", got)
	}
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: "curl -s " + site.URL + "/other"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Errorf("curl fetched a URL that is not allowed: %q", res.Stdout)
	}
	none := newComputer(t, Config{})
	if res, _ := none.Exec(context.Background(), &gbash.ExecutionRequest{Script: "curl -s " + site.URL + "/ok"}); res == nil || res.ExitCode == 0 {
		t.Error("a computer with no allowed URL has curl")
	}
}

func countTasks(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "count_all", Description: "Count the tasks."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]int, error) {
			return nil, map[string]int{"count": 7}, nil
		})
}

func TestANamedServerIsOneCommandBesideAnother(t *testing.T) {
	c := newComputer(t, Config{Python: python(t), Tools: []tools.Source{
		{Session: testSession(t, listNotes)}, {Name: "tasks", Session: testSession(t, countTasks)},
	}})
	if got := run(t, c, "tasks count-all | jq .count; notes list-notes | jq length"); got != "7\n3" {
		t.Errorf("got %q", got)
	}
	if got := run(t, c, `python3 -c "from tools import notes, tasks; print(tasks.count_all()['count'], len(notes.list_notes()))"`); got != "7 3" {
		t.Errorf("got %q", got)
	}
}

func TestACommandStartsWhereTheLastOneEnded(t *testing.T) {
	c := newComputer(t, Config{})
	run(t, c, "mkdir -p sub && cd sub && export MODE=debug")
	if got := run(t, c, "pwd; echo $MODE"); got != "/home/user/sub\ndebug" {
		t.Errorf("got %q", got)
	}
}

func TestARunawayScriptIsStoppedAtItsDeadline(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	start := time.Now()
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: `python3 -c "while True: pass"`, Timeout: 2 * time.Second})
	t.Logf("after %v: err %v, result %+v", time.Since(start).Round(100*time.Millisecond), err, res)
	if took := time.Since(start); took > 6*time.Second {
		t.Errorf("the script ran %v past a 2s deadline", took)
	}
	if res == nil || res.ExitCode != 124 {
		t.Errorf("a script stopped at its deadline should exit 124: %+v", res)
	}
	if got := run(t, c, "echo still here"); got != "still here" {
		t.Errorf("the computer is not usable after a stop: %q", got)
	}
}

func TestAScriptCannotHoldMoreThanItsMemoryCap(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: `python3 -c "x = bytearray(900 * 1024 * 1024)"`})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 || !strings.Contains(res.Stderr, "MemoryError") {
		t.Errorf("900 MiB under a 512 MiB cap: exit %d, stderr %q", res.ExitCode, res.Stderr)
	}
}

func TestPythonSleeps(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	if got := run(t, c, `python3 -c "import time; t = time.time(); time.sleep(0.3); print(0.25 < time.time() - t < 2)"`); got != "True" {
		t.Errorf("got %q", got)
	}
}

func TestTheShellWritesOnlyWhereProgramsSeeTheResult(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	if got := run(t, c, `f=$(mktemp); echo shared > $f; python3 -c "import sys; print(open(sys.argv[1]).read().strip())" $f; echo quiet > /dev/null`); got != "shared" {
		t.Errorf("got %q", got)
	}
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: "mkdir -p /data && echo x > /data/f"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Error("the shell wrote outside every mount")
	}
}

func TestAMountCanBeAnyFilesystem(t *testing.T) {
	memory := afero.NewMemMapFs()
	if err := afero.WriteFile(memory, "/given.txt", []byte("from the host"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newComputer(t, Config{Python: python(t), Mounts: []Mount{{Guest: "/mem", FS: memory}}})
	got := run(t, c, `cd /mem && cat given.txt && echo && echo from the shell > shell.txt &&
python3 -c "
print(open('shell.txt').read().strip())
open('python.txt', 'w').write('from python')
" && cat python.txt && echo &&
sqlite3 t.db "create table n(x); insert into n values (1),(2),(3)" && sqlite3 t.db "select sum(x) from n" && ls | tr '\n' ' '`)
	if got != "from the host\nfrom the shell\nfrom python\n6\ngiven.txt python.txt shell.txt t.db" {
		t.Errorf("got %q", got)
	}
	if b, _ := afero.ReadFile(memory, "/python.txt"); string(b) != "from python" {
		t.Errorf("the host does not see what the program wrote: %q", b)
	}
}

func TestTheShellHandlesAFileAndALoopLargerThanGbashsDefaults(t *testing.T) {
	data := t.TempDir()
	if err := os.WriteFile(data+"/big.csv", []byte(strings.Repeat("a,b,c,d,e,f,g,h,i\n", 1<<20)), 0o600); err != nil { // 18 MiB
		t.Fatal(err)
	}
	c := newComputer(t, Config{Mounts: []Mount{{Guest: "/data", Host: data}}})
	if got := run(t, c, "wc -l < /data/big.csv; grep -c a /data/big.csv; n=0; for i in $(seq 1 20000); do n=$((n+1)); done; echo $n"); got != "1048576\n1048576\n20000" {
		t.Errorf("got %q", got)
	}
}

func TestTheAgentIsToldEachToolModuleByItsPythonName(t *testing.T) {
	c := newComputer(t, Config{Python: &Python{lib: t.TempDir()}, Tools: []tools.Source{{Name: "my-server", Session: testSession(t, listNotes)}}})
	if _, err := c.Session(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.About(c.ToolCommands(), c.ToolModules()); !strings.Contains(got, "my-server") || !strings.Contains(got, "`from tools import my_server`") {
		t.Errorf("got %q", got)
	}
}

func TestThePythonPackageIsNotRewrittenWhenTheToolsAreUnchanged(t *testing.T) {
	c := newComputer(t, Config{Python: &Python{lib: t.TempDir()}, Tools: testTools(t, listNotes)})
	if _, err := c.Session(context.Background()); err != nil {
		t.Fatal(err)
	}
	init, past := filepath.Join(c.site, "tools", "__init__.py"), time.Now().Add(-time.Hour)
	if err := os.Chtimes(init, past, past); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Options(context.Background()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(init); err != nil || !info.ModTime().Equal(past) {
		t.Errorf("the package was written again for the same tools: %v %v", info.ModTime(), err)
	}
}

func TestTheComputerDescribesItsFolderAndItsOwnCommands(t *testing.T) {
	frob := commands.DefineCommand("frob", func(context.Context, *commands.Invocation) error { return nil })
	c := newComputer(t, Config{Commands: []commands.Command{frob}})
	about := c.About(nil, nil)
	for _, want := range []string{"Your files are under " + c.cfg.WorkDir, "frob"} {
		if !strings.Contains(about, want) {
			t.Errorf("the description lacks %q: %s", want, about)
		}
	}
}

func TestAToolsImageIsSavedAndNamedByItsPath(t *testing.T) {
	c := newComputer(t, Config{Tools: testTools(t, func(s *mcp.Server) {
		mcp.AddTool(s, &mcp.Tool{Name: "notes_drawNote"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "drawn"}, &mcp.ImageContent{Data: []byte("\x89PNG"), MIMEType: "image/png"}}}, nil, nil
		})
	})})
	lines := strings.Split(run(t, c, "notes draw-note"), "\n")
	if len(lines) != 2 || lines[0] != "drawn" || !strings.HasPrefix(lines[1], "/tmp/notes-draw-note-") || !strings.HasSuffix(lines[1], ".png") {
		t.Fatalf("got %q", lines)
	}
	if got := run(t, c, "cat "+lines[1]); got != "\x89PNG" {
		t.Fatalf("the saved file holds %q", got)
	}
}

func TestAFileBesideAStructuredResultIsNamedUnderFiles(t *testing.T) {
	c := newComputer(t, Config{Tools: testTools(t, func(s *mcp.Server) {
		mcp.AddTool(s, &mcp.Tool{Name: "notes_drawNote"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]note, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: []byte("png"), MIMEType: "image/png"}}}, map[string]note{"result": {ID: 7}}, nil
		})
	})})
	if got := run(t, c, `notes draw-note | jq -r '"\(.id) \(._files | length) \(._files[0] | startswith("/tmp/"))"'`); got != "7 1 true" {
		t.Fatalf("got %q", got)
	}
}

func TestAFailedCallLeavesNoFileBehind(t *testing.T) {
	tmp := afero.NewMemMapFs()
	c := newComputer(t, Config{Mounts: []Mount{{Guest: "/tmp", FS: tmp}}, Tools: testTools(t, func(s *mcp.Server) {
		mcp.AddTool(s, &mcp.Tool{Name: "notes_drawNote"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "boom"}, &mcp.ImageContent{Data: []byte("png"), MIMEType: "IMAGE/JPEG; q=1"}}}, nil, nil
		})
	})})
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: "notes draw-note"})
	if err != nil || res.ExitCode != 1 || !strings.Contains(res.Stderr, "boom") {
		t.Fatalf("got %v, %+v", err, res)
	}
	if left, _ := afero.ReadDir(tmp, "/"); len(left) != 0 {
		t.Fatalf("%d files were left in /tmp", len(left))
	}
}

func TestARefusedWriteOfAToolsFileIsSaidOnce(t *testing.T) {
	onlyHome := gbash.WithPolicy(policy.NewStatic(&policy.Config{ReadRoots: []string{"/"}, WriteRoots: []string{"/home/user"}}))
	c := newComputer(t, Config{Options: []gbash.Option{onlyHome}, Tools: testTools(t, func(s *mcp.Server) {
		mcp.AddTool(s, &mcp.Tool{Name: "notes_drawNote"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: []byte("png"), MIMEType: "image/png"}}}, nil, nil
		})
	})})
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: "notes draw-note"})
	if err != nil || res.ExitCode == 0 || strings.Count(res.Stderr, "denied") != 1 {
		t.Fatalf("got %v, exit %d, stderr %q", err, res.ExitCode, res.Stderr)
	}
}

func TestTheComputerIsAnMCPServerForAnAgentInThisProcess(t *testing.T) {
	ctx := context.Background()
	c := newComputer(t, Config{})
	server, err := c.MCPServer(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	agent, err := mcp.NewClient(&mcp.Implementation{Name: "agent"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	call := func(tool string, args map[string]any) string {
		res, err := agent.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil || res.IsError || len(res.Content) == 0 {
			t.Fatalf("%s: %v %+v", tool, err, res)
		}
		return res.Content[0].(*mcp.TextContent).Text
	}
	call("write", map[string]any{"path": "/home/user/note.txt", "content": "kept"})
	if got := strings.TrimSpace(call("bash", map[string]any{"commands": "cat note.txt"})); got != "kept" {
		t.Fatalf("bash read %q", got)
	}
	if res, _ := agent.CallTool(ctx, &mcp.CallToolParams{Name: "write", Arguments: map[string]any{"path": "/etc/x", "content": "no"}}); res == nil || !res.IsError {
		t.Fatalf("a write outside the folders was not refused: %+v", res)
	}
}

func TestAnEmbeddersFilesAreUnderTheShellsRules(t *testing.T) {
	ctx := context.Background()
	c := newComputer(t, Config{Options: []gbash.Option{gbash.WithLimitOverrides(policy.Limits{MaxFileBytes: 8})}})
	files, err := c.files(ctx)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) error {
		f, err := files.OpenFile(ctx, name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		_, err = f.Write([]byte(text))
		return errors.Join(err, f.Close())
	}
	if err := write("/etc/x", "no"); !policy.IsDenied(err) {
		t.Fatalf("a write outside the mounts: %v", err)
	}
	if err := write("note.txt", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := write("note.txt", "more than eight bytes"); err == nil {
		t.Fatal("a file past the shell's limit was written")
	}
	if got := run(t, c, "cat /home/user/note.txt"); got != "kept" {
		t.Fatalf("the shell read %q", got)
	}
}
