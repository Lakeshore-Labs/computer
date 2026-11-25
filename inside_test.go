package computer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lakeshore-Labs/gbash"
	"github.com/spf13/afero"
)

// inHome is a computer whose home is a host directory with a read-only filesystem mounted on its folder files.
func inHome(t *testing.T, home string) (*Computer, afero.Fs, error) {
	t.Helper()
	files := afero.NewMemMapFs()
	if err := files.MkdirAll("/app", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(files, "/app/notes.txt", []byte("the app's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Python: python(t), WorkDir: "/home/user", Mounts: []Mount{
		{Guest: "/home/user/files", FS: files, ReadOnly: true}, // the inner one first: their order says nothing
		{Guest: "/home/user/", Host: home},
	}})
	if c != nil {
		t.Cleanup(func() { _ = c.Close() })
	}
	return c, files, err
}

func TestAMountInsideAMountIsAFolderOfIt(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, files, err := inHome(t, home)
	if err != nil {
		t.Fatal(err)
	}
	const list = `python3 -c "import os; print(sorted(os.listdir('.')), sorted(os.listdir('files/app')), open('files/app/notes.txt').read().strip())"`
	if got := run(t, c, "echo mine > own.txt; ls | tr '\\n' ' '; cat files/app/notes.txt; "+list); got != "files own.txt the app's\n['files', 'own.txt'] ['notes.txt'] the app's" {
		t.Errorf("the shell and a program see: %q", got)
	}
	// The inner mount is read-only though the home around it is not, and says so as any read-only mount does.
	for _, script := range []string{"mkdir files/made", "echo x > files/app/notes.txt", "rm files/app/notes.txt", "mv files/app/notes.txt .", "cp own.txt files/app/",
		`python3 -c "open('files/app/notes.txt','w')"`} {
		res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: script})
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode == 0 || !strings.Contains(strings.ToLower(res.Stderr), "read-only file system") {
			t.Errorf("%s: exit %d, %q; want it refused as read-only", script, res.ExitCode, res.Stderr)
		}
	}
	if got := run(t, c, "rm -rf /home/user/* ; ls files/app"); got != "notes.txt" {
		t.Errorf("after the home is emptied the mount holds %q", got)
	}
	if b, _ := afero.ReadFile(files, "/app/notes.txt"); string(b) != "the app's\n" {
		t.Errorf("the mounted file is now %q", b)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, "files")); len(entries) != 0 {
		t.Errorf("the host's folder under the mount holds %v", entries)
	}
}

// A program lists the outer directory itself, so a mount with no folder there would be in the shell's listing and
// not in the program's.
func TestAMountInsideAMountNeedsItsFolder(t *testing.T) {
	if _, _, err := inHome(t, t.TempDir()); err == nil {
		t.Error("a mount on a folder that is not there was made")
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "files"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inHome(t, home); err == nil {
		t.Error("a mount on a file was made")
	}
}

func TestAWritableMountInsideAReadOnlyOneIsWritten(t *testing.T) {
	outer := afero.NewMemMapFs()
	if err := outer.MkdirAll("/w", 0o755); err != nil {
		t.Fatal(err)
	}
	c := newComputer(t, Config{Mounts: []Mount{{Guest: "/ro", FS: outer, ReadOnly: true}, {Guest: "/ro/w", Host: t.TempDir()}}})
	if got := run(t, c, "echo hi > /ro/w/x; cat /ro/w/x"); got != "hi" {
		t.Errorf("got %q", got)
	}
	if res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: "echo y > /ro/y"}); err != nil || res.ExitCode == 0 {
		t.Errorf("a write beside the writable mount: %v, %v", res, err)
	}
}

// gbash makes the working directory before each command, which changes nothing when it is there.
func TestACommandRunsAfterCdIntoAReadOnlyMount(t *testing.T) {
	ro := afero.NewMemMapFs()
	if err := ro.MkdirAll("/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	c := newComputer(t, Config{Mounts: []Mount{{Guest: "/ro", FS: ro, ReadOnly: true}}})
	run(t, c, "cd /ro/sub")
	if got := run(t, c, "pwd; cd /home/user; pwd"); got != "/ro/sub\n/home/user" {
		t.Errorf("got %q", got)
	}
}
