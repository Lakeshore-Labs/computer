package wasm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lakeshore-Labs/computer/files"

	"github.com/spf13/afero"
	"github.com/tetratelabs/wazero/experimental/sys"
)

func TestAReadOnlyDirectoryIsNotWrittenThroughASyncedRead(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := afero.WriteFile(mem, "/f.txt", []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, fs := range map[string]afero.Fs{"memory": mem, "afero read-only": afero.NewReadOnlyFs(mem)} {
		d := &dirFS{fs: fs, readOnly: true}
		f, errno := d.OpenFile("f.txt", sys.O_RDONLY|sys.O_SYNC, 0)
		if errno != 0 {
			t.Fatalf("%s: open: %v", name, errno)
		}
		if _, errno := f.Write([]byte("PWN")); errno == 0 {
			t.Errorf("%s: a file opened for reading was written", name)
		}
		f.Close()
	}
	if got, _ := afero.ReadFile(mem, "/f.txt"); string(got) != "kept" {
		t.Errorf("the file holds %q", got)
	}
}

func TestADirectoryWithEntriesIsNotRemoved(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := afero.WriteFile(mem, "/d/inner.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if errno := (&dirFS{fs: mem}).Rmdir("d"); errno != sys.ENOTEMPTY {
		t.Errorf("rmdir of a directory with entries: %v, want ENOTEMPTY", errno)
	}
	if ok, _ := afero.Exists(mem, "/d/inner.txt"); !ok {
		t.Error("the entry is gone")
	}
}

func TestAReadPastTheEndOfAFileInMemoryIsTheEnd(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := afero.WriteFile(mem, "/f.txt", []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, errno := (&dirFS{fs: mem}).OpenFile("f.txt", sys.O_RDONLY, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer f.Close()
	if n, errno := f.Pread(make([]byte, 10), 1<<20); n != 0 || errno != 0 {
		t.Errorf("pread past the end: %d, %v", n, errno)
	}
	if _, errno := f.Seek(1<<20, 0); errno != 0 {
		t.Fatal(errno)
	}
	if n, errno := f.Read(make([]byte, 10)); n != 0 || errno != 0 {
		t.Errorf("read past the end: %d, %v", n, errno)
	}
}

func TestFilesInMemoryHaveTheirOwnInodes(t *testing.T) {
	mem := afero.NewMemMapFs()
	for _, name := range []string{"/d/a.txt", "/d/b.txt"} {
		if err := afero.WriteFile(mem, name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	d := &dirFS{fs: mem}
	a, _ := d.Stat("d/a.txt")
	b, _ := d.Stat("d/b.txt")
	dir, _ := d.Stat("d")
	if a.Ino == 0 || a.Ino == b.Ino || a.Ino == dir.Ino {
		t.Errorf("inodes: a %d, b %d, d %d", a.Ino, b.Ino, dir.Ino)
	}
	f, errno := d.OpenFile("d", sys.O_RDONLY|sys.O_DIRECTORY, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer f.Close()
	entries, _ := f.Readdir(-1)
	for _, e := range entries {
		if want, _ := d.Stat("d/" + e.Name); e.Ino != want.Ino {
			t.Errorf("%s: the entry's inode is %d, its stat's %d", e.Name, e.Ino, want.Ino)
		}
	}
	if opened, _ := f.Ino(); opened != dir.Ino {
		t.Errorf("the open directory's inode is %d, its stat's %d", opened, dir.Ino)
	}
}

func TestADirectoryInMemoryIsListedAgainAfterARewind(t *testing.T) {
	mem := afero.NewMemMapFs()
	if err := afero.WriteFile(mem, "/d/a.txt", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, errno := (&dirFS{fs: mem}).OpenFile("d", sys.O_RDONLY|sys.O_DIRECTORY, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer f.Close()
	for i := range 2 {
		if entries, errno := f.Readdir(-1); len(entries) != 1 || errno != 0 {
			t.Fatalf("listing %d: %v, %v", i+1, entries, errno)
		}
		if _, errno := f.Seek(0, 0); errno != 0 {
			t.Fatal(errno)
		}
	}
}

func TestAJoinedDirectoryOfTwoLayersIsListedAgainAfterARewind(t *testing.T) {
	base, layer := afero.NewMemMapFs(), afero.NewMemMapFs()
	if err := errors.Join(afero.WriteFile(base, "/d/given.txt", nil, 0o644), afero.WriteFile(layer, "/d/own.txt", nil, 0o644)); err != nil {
		t.Fatal(err)
	}
	layered := afero.NewCopyOnWriteFs(base, layer)
	for _, mount := range []afero.Fs{layered, afero.NewBasePathFs(layered, "/")} {
		listedTwice(t, mount)
	}
}

func listedTwice(t *testing.T, mount afero.Fs) {
	t.Helper()
	f, errno := (&dirFS{fs: mount}).OpenFile("d", sys.O_RDONLY|sys.O_DIRECTORY, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer f.Close()
	for i := range 2 {
		var entries []sys.Dirent
		for part, errno := f.Readdir(1); len(part) > 0 || errno != 0; part, errno = f.Readdir(1) { // as wazero reads, in parts
			if errno != 0 {
				t.Fatal(errno)
			}
			entries = append(entries, part...)
		}
		if len(entries) != 2 {
			t.Fatalf("listing %d: %v, want both layers' files", i+1, entries)
		}
		if _, errno := f.Seek(0, 0); errno != 0 {
			t.Fatal(errno)
		}
	}
}

func TestAHostDirectoryIsRewoundByItselfNotByItsName(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "d", "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	host, err := files.Dir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	f, errno := (&dirFS{fs: host}).OpenFile("d", sys.O_RDONLY|sys.O_DIRECTORY, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	defer f.Close()
	if _, errno := f.Readdir(-1); errno != 0 {
		t.Fatal(errno)
	}
	if err := os.Rename(filepath.Join(root, "d"), filepath.Join(root, "e")); err != nil {
		t.Fatal(err)
	}
	if _, errno := f.Seek(0, 0); errno != 0 {
		t.Fatalf("rewind: %v", errno)
	}
	// Not the entries: on macOS a renamed directory's rewound listing is empty for os.File itself.
	if _, errno := f.Readdir(-1); errno != 0 {
		t.Errorf("listing after the rename: %v", errno)
	}
}

func TestAPathInTwoMemoryMountsIsTwoFiles(t *testing.T) {
	var stats [][2]uint64 // device, inode
	for range 2 {
		mem := afero.NewMemMapFs()
		if err := afero.WriteFile(mem, "/a.txt", nil, 0o644); err != nil {
			t.Fatal(err)
		}
		st, errno := newDirFS(mem, false).Stat("a.txt")
		if errno != 0 {
			t.Fatal(errno)
		}
		stats = append(stats, [2]uint64{st.Dev, st.Ino})
	}
	if stats[0] == stats[1] {
		t.Errorf("both are device and inode %v", stats[0])
	}
}
