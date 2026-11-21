package wasm

import (
	"testing"

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
