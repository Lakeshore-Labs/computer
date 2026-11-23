// atlasprobe exercises four atlas rows through the public Go API of Lakeshore-Labs/computer, with ordinary
// positive calls only. It makes no network request and reads no credential. Output: JSON on stdout.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/agent"
	"github.com/Lakeshore-Labs/computer/wasm"
	"github.com/ewhauser/gbash"
	"github.com/ewhauser/gbash/commands"
)

type check struct {
	ID       string `json:"id"`
	Row      string `json:"row"`
	Expected string `json:"expected"`
	Observed any    `json:"observed"`
	Pass     bool   `json:"pass"`
}

var checks []check
var tmpRoot string

// clean keeps host temp paths out of the output.
func clean(v any) any {
	b, _ := json.Marshal(v)
	s := strings.ReplaceAll(string(b), tmpRoot, "<tmp>")
	var out any
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func rec(id, row, expected string, observed any, pass bool) {
	checks = append(checks, check{id, row, expected, clean(observed), pass})
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup failed:", err)
		os.Exit(2)
	}
}

// a WebAssembly module with one exported, empty `_start` function: no imports, no memory, does nothing.
var tiny = []byte{0, 'a', 's', 'm', 1, 0, 0, 0,
	1, 4, 1, 0x60, 0, 0, // type () -> ()
	3, 2, 1, 0, // one function
	7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0, // export "_start"
	10, 4, 1, 2, 0, 0x0b} // body: no locals, end

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var err error
	tmpRoot, err = os.MkdirTemp("", "atlasprobe")
	must(err)
	tmpRoot, _ = filepath.EvalSymlinks(tmpRoot)
	defer os.RemoveAll(tmpRoot)
	work := filepath.Join(tmpRoot, "work")
	state := filepath.Join(tmpRoot, "state")
	must(os.MkdirAll(work, 0o755))
	must(os.MkdirAll(state, 0o755))
	// two conversations already on disk, so --list has something to say
	for _, n := range []string{"20260101T000000.000001", "20260101T000000.000002"} {
		must(os.Mkdir(filepath.Join(state, n), 0o700))
	}

	c, err := computer.New(computer.Config{
		Mounts:   []computer.Mount{{Guest: "/work", Host: work}},
		WorkDir:  "/work",
		Commands: []commands.Command{agent.Command("probe-agent", agent.Config{State: state})},
	})
	must(err)

	// (1) Computer.Session, twice, listing unchanged (no tool sources, so the listing is empty both times).
	s1, e1 := c.Session(ctx)
	s2, e2 := c.Session(ctx)
	rec("session-no-error", "go-method:computer.Computer.Session", "both calls return a session and no error",
		map[string]any{"err1": fmt.Sprint(e1), "err2": fmt.Sprint(e2), "nil1": s1 == nil, "nil2": s2 == nil}, e1 == nil && e2 == nil && s1 != nil && s2 != nil)
	rec("session-same-identity", "go-method:computer.Computer.Session", "unchanged listing returns the same *gbash.Session pointer",
		map[string]any{"same_pointer": s1 == s2}, s1 == s2)
	r, err := s1.Exec(ctx, &gbash.ExecutionRequest{Script: "echo session-says-hi; pwd; echo data > /work/from-session.txt; cat /work/from-session.txt", WorkDir: "/work"})
	ok := err == nil && r != nil && r.ExitCode == 0 && r.Stdout == "session-says-hi\n/work\ndata\n"
	var obs any = map[string]any{"err": fmt.Sprint(err)}
	if r != nil {
		obs = map[string]any{"exit": r.ExitCode, "stdout": r.Stdout, "stderr": r.Stderr}
	}
	rec("session-shell-command", "go-method:computer.Computer.Session", "echo, pwd and a redirected write/read run in the returned session, exit 0", obs, ok)
	hostBytes, herr := os.ReadFile(filepath.Join(work, "from-session.txt"))
	rec("session-file-on-host", "go-method:computer.Computer.Session", "the file the session wrote is on the host mount", map[string]any{"host_content": string(hostBytes), "err": fmt.Sprint(herr)}, string(hostBytes) == "data\n")
	sOld := s2
	// (control) a later Session call after other computer activity, still unchanged listing
	_, _ = c.Exec(ctx, &gbash.ExecutionRequest{Script: "true"})
	s3, _ := c.Session(ctx)
	rec("session-same-after-exec", "go-method:computer.Computer.Session", "a third call after Exec still returns that pointer", map[string]any{"same_pointer": s3 == sOld}, s3 == sOld)

	// (2) wasm.Module.Close: compile, run once, close; record what Close returned.
	m, err := wasm.Compile(ctx, wasm.Program{Binary: tiny})
	must(err)
	code, runErr := m.Run(ctx, wasm.Run{Args: []string{"tiny"}})
	rec("wasm-run-before-close", "go-method:wasm.Module.Close", "the tiny module runs and exits 0", map[string]any{"exit": code, "err": fmt.Sprint(runErr)}, code == 0 && runErr == nil)
	closeErr := m.Close(ctx)
	rec("wasm-close-status", "go-method:wasm.Module.Close", "Close returns a nil error after a normal compile and run", map[string]any{"returned_nil": closeErr == nil, "err": fmt.Sprint(closeErr)}, closeErr == nil)

	// (3) agent.Command installed under the chosen name "probe-agent" (see Config.Commands above).
	before := dirNames(state)
	help := shell(ctx, c, "probe-agent --help")
	list := shell(ctx, c, "probe-agent --list")
	after := dirNames(state)
	rec("agent-help", "go-function:agent.Command", "--help under the chosen name prints a usage line naming that name, exit 0",
		help, help.Exit == 0 && strings.HasPrefix(help.Stdout, "usage: probe-agent [-q] [--json]") && strings.Contains(help.Stdout, "probe-agent --list"))
	rec("agent-list-reads-state", "go-function:agent.Command", "--list prints the two conversation directories of Config.State, newest first, exit 0",
		list, list.Exit == 0 && list.Stdout == "20260101T000000.000002\n20260101T000000.000001\n")
	rec("agent-help-list-leave-state-alone", "go-function:agent.Command", "--help and --list create no conversation",
		map[string]any{"before": before, "after": after}, strings.Join(before, ",") == strings.Join(after, ","))
	ctl1 := shell(ctx, c, "other-agent --list")
	ctl2 := shell(ctx, c, "agent --list")
	rec("agent-control-unregistered-names", "go-function:agent.Command", "names never registered (other-agent, agent) are not found: exit 127",
		map[string]any{"other-agent": ctl1, "agent": ctl2}, ctl1.Exit == 127 && ctl2.Exit == 127)

	// (4) Environment
	env := c.Environment()
	var o1, o2 bytes.Buffer
	script := `echo "A=${A-unset} B=${B-unset} HOME=${HOME-unset} PATH=${PATH+set}${PATH-unset} PWD=$PWD"`
	x1, e1b := env.Run(ctx, script, "/work", map[string]string{"A": "1"}, false, &o1, &o1)
	x2, e2b := env.Run(ctx, script, "/work", map[string]string{"A": "1"}, true, &o2, &o2)
	rec("env-run-replace-false-vs-true", "go-method:computer.Environment.Run", "replaceEnv=false adds A=1 to the shell's own environment; replaceEnv=true drops the shell's own HOME while keeping A=1 (PATH is observed, not assumed)",
		map[string]any{"false": map[string]any{"exit": x1, "err": fmt.Sprint(e1b), "out": o1.String()}, "true": map[string]any{"exit": x2, "err": fmt.Sprint(e2b), "out": o2.String()}},
		x1 == 0 && x2 == 0 && o1.String() != o2.String() && strings.Contains(o2.String(), "A=1") && strings.Contains(o2.String(), "HOME=unset"))

	// symlink: made in the tree by the computer's own shell (ln -s), target is a file in the same mount.
	must(env.Write(ctx, "/work/target.txt", []byte("hello target"), false))
	ln := shell(ctx, c, "ln -s target.txt /work/link.txt")
	linkHow := "shell ln -s"
	if ln.Exit != 0 { // fall back to the host, still inside the mount, relative target
		must(os.Symlink("target.txt", filepath.Join(work, "link.txt")))
		linkHow = "host os.Symlink (shell ln -s exit " + fmt.Sprint(ln.Exit) + ": " + strings.TrimSpace(ln.Stderr) + ")"
	}
	fi, ef := env.Stat(ctx, "/work/link.txt", true)
	li, el := env.Stat(ctx, "/work/link.txt", false)
	fo := map[string]any{"link_made_by": linkHow, "follow_true": info(fi, ef), "follow_false": info(li, el)}
	rec("stat-symlink-follow", "go-method:computer.Environment.Stat", "follow=true describes the target (regular, 12 bytes); follow=false describes the link (symlink mode)",
		fo, ef == nil && el == nil && fi.Mode().IsRegular() && fi.Size() == 12 && li.Mode()&fs.ModeSymlink != 0)

	// Truncate: growth and zero
	must(env.Write(ctx, "/work/t.bin", []byte("abc"), false))
	tg := env.Truncate(ctx, "/work/t.bin", 10)
	grown, _ := env.Read(ctx, "/work/t.bin", 0, 100)
	gi, _ := env.Stat(ctx, "/work/t.bin", true)
	rec("truncate-grow", "go-method:computer.Environment.Truncate", "3-byte file truncated to 10 reads back 10 bytes: abc then 7 zero bytes",
		map[string]any{"err": fmt.Sprint(tg), "size": gi.Size(), "hex": fmt.Sprintf("%x", grown)}, tg == nil && bytes.Equal(grown, append([]byte("abc"), make([]byte, 7)...)) && gi.Size() == 10)
	tz := env.Truncate(ctx, "/work/t.bin", 0)
	zeroed, _ := env.Read(ctx, "/work/t.bin", 0, 100)
	zi, _ := env.Stat(ctx, "/work/t.bin", true)
	rec("truncate-zero", "go-method:computer.Environment.Truncate", "truncate to 0 leaves an empty file that still exists",
		map[string]any{"err": fmt.Sprint(tz), "size": zi.Size(), "read_bytes": len(zeroed)}, tz == nil && zi.Size() == 0 && len(zeroed) == 0)

	// empty folder: create (single, parent exists), rename, remove (non-recursive)
	mk := env.Mkdir(ctx, "/work/empty", false)
	ls1, _ := env.List(ctx, "/work/empty")
	rn := env.Rename(ctx, "/work/empty", "/work/renamed")
	_, oldErr := env.Stat(ctx, "/work/empty", true)
	ni, newErr := env.Stat(ctx, "/work/renamed", true)
	rm := env.Remove(ctx, "/work/renamed", false)
	_, goneErr := env.Stat(ctx, "/work/renamed", true)
	rec("mkdir-single", "go-method:computer.Environment.Mkdir", "Mkdir without parents makes one missing folder whose parent exists; it lists empty",
		map[string]any{"err": fmt.Sprint(mk), "entries": len(ls1)}, mk == nil && len(ls1) == 0)
	rec("rename-folder", "go-method:computer.Environment.Rename", "renaming an empty folder moves it: old name gone, new name is a directory",
		map[string]any{"err": fmt.Sprint(rn), "old_not_exist": errors.Is(oldErr, fs.ErrNotExist), "new_is_dir": newErr == nil && ni.IsDir()}, rn == nil && errors.Is(oldErr, fs.ErrNotExist) && newErr == nil && ni.IsDir())
	rec("remove-empty-folder-nonrecursive", "go-method:computer.Environment.Remove", "non-recursive Remove deletes the empty folder; it is then not found",
		map[string]any{"err": fmt.Sprint(rm), "not_exist_after": errors.Is(goneErr, fs.ErrNotExist)}, rm == nil && errors.Is(goneErr, fs.ErrNotExist))
	rf := env.Remove(ctx, "/work/target.txt", false)
	_, rfErr := env.Stat(ctx, "/work/target.txt", true)
	rec("remove-file-nonrecursive", "go-method:computer.Environment.Remove", "non-recursive Remove deletes an ordinary file",
		map[string]any{"err": fmt.Sprint(rf), "not_exist_after": errors.Is(rfErr, fs.ErrNotExist)}, rf == nil && errors.Is(rfErr, fs.ErrNotExist))

	must(c.Close())
	pass := 0
	for _, k := range checks {
		if k.Pass {
			pass++
		}
	}
	out, _ := json.MarshalIndent(map[string]any{"checks": checks, "passed": pass, "total": len(checks)}, "", " ")
	fmt.Println(string(out))
	if pass != len(checks) {
		os.Exit(1)
	}
}

type result struct {
	Exit   int    `json:"exit"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func shell(ctx context.Context, c *computer.Computer, script string) result {
	var o, e bytes.Buffer
	code, err := c.Environment().Run(ctx, script, "/work", nil, false, &o, &e)
	if err != nil {
		e.WriteString("go error: " + err.Error())
	}
	return result{code, o.String(), e.String()}
}

func info(i fs.FileInfo, err error) any {
	if err != nil {
		return map[string]any{"err": err.Error()}
	}
	return map[string]any{"name": i.Name(), "size": i.Size(), "mode": i.Mode().String(), "regular": i.Mode().IsRegular(), "symlink": i.Mode()&fs.ModeSymlink != 0}
}

func dirNames(dir string) []string {
	es, _ := os.ReadDir(dir)
	var n []string
	for _, e := range es {
		n = append(n, e.Name())
	}
	return n
}
