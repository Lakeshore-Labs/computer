package computer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ewhauser/gbash"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// python is the CPython build named by PYTHON_WASM and PYTHON_LIB; the tests that need it are skipped without it.
func python(t *testing.T) *Python {
	t.Helper()
	binary, lib := os.Getenv("PYTHON_WASM"), os.Getenv("PYTHON_LIB")
	if binary == "" || lib == "" {
		t.Skip("PYTHON_WASM and PYTHON_LIB are not set")
	}
	p, err := LoadPython(context.Background(), binary, lib, wasm.Program{
		MemoryPages: 8192, CacheDir: os.Getenv("PYTHON_CACHE"), HostModule: os.Getenv("PYTHON_HOST_MODULE"), Features: api.CoreFeaturesV2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
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
