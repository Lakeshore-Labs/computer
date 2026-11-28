package computer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lakeshore-Labs/gbash"
	"github.com/Lakeshore-Labs/gbash/commands"
	"github.com/Lakeshore-Labs/gbash/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"

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

var testReleases sync.Map // directory to a function returning its compiled Python

// python is the wasi-python release unpacked in PYTHON_DIR, or the embedded one when it is not set, compiled once
// for all the tests.
func python(t *testing.T) *Python {
	t.Helper()
	return pythonFrom(t, os.Getenv("PYTHON_DIR"))
}

func pythonFrom(t *testing.T, dir string) *Python {
	t.Helper()
	load, _ := testReleases.LoadOrStore(dir, sync.OnceValues(func() (*Python, error) {
		program := wasm.Program{MaxRuns: 2, CacheDir: os.Getenv("PYTHON_CACHE")}
		if dir == "" {
			return EmbeddedPython(context.Background(), program)
		}
		return LoadPython(context.Background(), dir, program)
	}))
	py, err := load.(func() (*Python, error))()
	if err != nil {
		t.Fatal(err)
	}
	return py
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

// A sort larger than SQLite's sorter memory, at least 250 pages, spills to a temporary file. SQLite before 3.51.0
// trapped creating one on WASI (#28).
func TestPythonSQLiteSortsMoreThanFitsInMemory(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	got := run(t, c, `python3 -c "
import sqlite3
c = sqlite3.connect(':memory:')
c.execute('pragma temp_store=FILE')
c.execute('create table t(x)')
c.executemany('insert into t values (?)', ((str(i % 997) * 50,) for i in range(20000)))
c.execute('create index i on t(x)')
print(c.execute('pragma integrity_check').fetchone()[0])
"`)
	if got != "ok" {
		t.Errorf("got %q", got)
	}
}

func TestPythonAnalysesWithPandasAndPutsAChartInWordAndExcel(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	got := run(t, c, `cat > recap.py <<'END'
import pandas as pd, docx, openpyxl
df = pd.DataFrame({"store": ["A", "B", "A", "B"], "month": [1, 1, 2, 2], "sales": [100, 80, 120, 60]})
pivot = df.pivot_table(index="store", columns="month", values="sales")
pivot.T.plot.line(title="Sales").figure.savefig("sales.png")
d = docx.Document(); d.add_heading("Recap", 0); d.add_picture("sales.png"); d.save("recap.docx")
pivot.to_excel("recap.xlsx")
print(pivot.loc["A", 2], len(docx.Document("recap.docx").inline_shapes), openpyxl.load_workbook("recap.xlsx").active["C2"].value)
END
python3 recap.py`)
	if got != "120.0 1 120" {
		t.Errorf("got %q", got)
	}
}

// Every python3 starts from one image with the heavy modules imported, and is still a program of its own: what a run
// changes is gone in the next, the generators are seeded afresh, the clock is the host's, and a run's end still runs
// its atexit handlers and writes what it left unflushed.
func TestPythonStartsWithItsModulesImportedAndEachRunIsItsOwn(t *testing.T) {
	c := newComputer(t, Config{Tools: testTools(t, listNotes), Python: python(t)})
	script := `import sys, time, random, atexit, numpy, matplotlib
print(sorted(m for m in ("numpy", "pandas", "matplotlib.pyplot", "tools") if m in sys.modules))
print(hasattr(numpy, "marked"), abs(time.time() - float(sys.argv[1])) < 60, matplotlib.get_configdir())
print(random.random(), numpy.random.rand())
numpy.marked = True
open("left.txt", "a").write(sys.argv[2])
atexit.register(print, "at exit")`
	run(t, c, "cat > fresh.py <<'EOF'\n"+script+"\nEOF")
	var draws []string
	for _, arg := range []string{"one", "two"} {
		got := strings.Split(run(t, c, fmt.Sprintf("python3 fresh.py %d %s", time.Now().Unix(), arg)), "\n")
		if want := []string{"['matplotlib.pyplot', 'numpy', 'pandas']", "False True /tmp/.matplotlib", "at exit"}; len(got) != 4 ||
			got[0] != want[0] || got[1] != want[1] || got[3] != want[2] {
			t.Fatalf("got %q", got)
		}
		draws = append(draws, got[2])
	}
	if draws[0] == draws[1] {
		t.Errorf("two runs drew the same numbers: %q", draws[0])
	}
	if got := run(t, c, "cat left.txt; A=b python3 -c 'from tools import notes; import os; print(os.environ[\"A\"], len(notes.list_notes()))'"); got != "onetwob 3" {
		t.Errorf("got %q", got)
	}
}

func TestEmbeddedPythonStartsWithFlaskAndCanReadItsPackageMetadata(t *testing.T) {
	c := newComputer(t, Config{Python: pythonFrom(t, "")})
	script := `import sys
from importlib.metadata import version
print("flask" in sys.modules, bool(version("Flask")))`
	if got := run(t, c, "python3 -c '"+script+"'"); got != "True True" {
		t.Errorf("got %q", got)
	}
}

func TestAPythonScriptImportsTheModulesBesideItAndInTheWorkingDirectory(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	got := run(t, c, `mkdir d && echo 'X = 1' > d/helper.py && echo 'import helper, os; print(helper.X, os.path.isabs(__file__))' > d/main.py
python3 -u d/main.py; cd d && python3 main.py && python3 -m helper && python3 -c 'import helper; print(helper.X)'`)
	if got != "1 True\n1 True\n1" {
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
	c := newComputer(t, Config{Python: python(t), Tools: []tools.Source{{Name: "my-server", Session: testSession(t, listNotes)}}})
	if _, err := c.Session(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.About(c.ToolCommands(), c.ToolModules()); !strings.Contains(got, "my-server") || !strings.Contains(got, "`from tools import my_server`") {
		t.Errorf("got %q", got)
	}
}

func TestThePythonPackageIsNotRewrittenWhenTheToolsAreUnchanged(t *testing.T) {
	c := newComputer(t, Config{Python: python(t), Tools: testTools(t, listNotes)})
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
	if got := strings.TrimSpace(call("bash", map[string]any{"command": "cat note.txt"})); got != "kept" {
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

// uploadOnClose is a filesystem whose writable files store their contents only when closed, and fail to.
type uploadOnClose struct{ afero.Fs }

type failedUpload struct{ afero.File }

func (f failedUpload) Close() error {
	_ = f.File.Close()
	return &os.PathError{Op: "close", Path: f.Name(), Err: errors.New("upload failed")}
}

func (fs uploadOnClose) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f, err := fs.Fs.OpenFile(name, flag, perm)
	if err != nil || flag&(os.O_WRONLY|os.O_RDWR) == 0 {
		return f, err
	}
	return failedUpload{f}, nil
}

func TestARedirectReportsAFileThatFailsToClose(t *testing.T) {
	c := newComputer(t, Config{Mounts: []Mount{{Guest: "/archive", FS: uploadOnClose{afero.NewMemMapFs()}}}})
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: "set -e; echo Sunday > /archive/notice.txt; echo archived"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 1 || res.Stdout != "" || !strings.Contains(res.Stderr, "upload failed") {
		t.Errorf("exit %d, stdout %q, stderr %q", res.ExitCode, res.Stdout, res.Stderr)
	}
}

// matplotlib is imported before a run begins, and still reads the run's own settings.
func TestMatplotlibReadsARunsOwnBackendAndRcFile(t *testing.T) {
	c := newComputer(t, Config{Python: python(t)})
	got := run(t, c, `MPLBACKEND=svg python3 -c 'import matplotlib; print(matplotlib.get_backend())'
echo 'lines.linewidth: 7' > matplotlibrc
python3 -c 'import matplotlib; print(matplotlib.rcParams["lines.linewidth"])'
rm matplotlibrc
python3 -c 'import matplotlib; print(matplotlib.rcParams["lines.linewidth"])'`)
	if got != "svg\n7.0\n1.5" {
		t.Errorf("got %q", got)
	}
}
