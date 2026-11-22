// File the evening notice and keep one printed counterfoil under a second local name.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Lakeshore-Labs/computer/files"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type observation struct{ Method, Result string }

func run() (records []observation, err error) {
	dir, err := os.MkdirTemp("", "computer-accession-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	root, err := files.Dir(dir)
	if err != nil {
		return nil, err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, root.Close())
		}
	}()
	add := func(name, result string) { records = append(records, observation{name, result}) }
	add("files.Dir", "Opened a newly created temporary archive")
	add("files.Root.Name", root.Name())
	if err = root.Mkdir("archive", 0755); err != nil {
		return nil, err
	}
	add("files.Root.Mkdir", "Created archive")
	if err = root.MkdirAll("drafts/2026", 0755); err != nil {
		return nil, err
	}
	add("files.Root.MkdirAll", "Created drafts/2026")
	draft := "drafts/2026/times.txt"
	f, err := root.Create(draft)
	if err != nil {
		return nil, err
	}
	_, err = f.WriteString("Friday 17:00\n")
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, err
	}
	add("files.Root.Create", "Wrote the first closing time")
	f, err = root.OpenFile(draft, os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	// The Unix append API is available on the mounted OS file.
	raw, ok := f.(interface {
		IsAppend() bool
		SetAppend(bool) error
	})
	if !ok {
		f.Close()
		return nil, errors.New("this demonstration requires Unix file append flags")
	}
	if raw.IsAppend() {
		f.Close()
		return nil, errors.New("unexpected initial append mode")
	}
	if err = raw.SetAppend(true); err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err == nil {
		_, err = f.WriteString("Sunday 16:00\n")
	}
	if err == nil && !raw.IsAppend() {
		err = errors.New("append flag did not change")
	}
	if err == nil {
		err = raw.SetAppend(false)
	}
	if err == nil && raw.IsAppend() {
		err = errors.New("append flag did not reset")
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, err
	}
	add("files.Root.OpenFile", "Opened the draft for editing")
	add("files.File.SetAppend", "Enabled and then cleared the file descriptor’s append flag")
	add("files.File.IsAppend", "Observed false, true, false")
	if err = root.Rename(draft, "archive/notice.txt"); err != nil {
		return nil, err
	}
	add("files.Root.Rename", "Filed the completed notice")
	f, err = root.Open("archive/notice.txt")
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, err
	}
	if string(b) != "Friday 17:00\nSunday 16:00\n" {
		return nil, errors.New("unexpected notice")
	}
	add("files.Root.Open", "Read the two closing times after appending at offset zero")
	if err = root.Chmod("archive/notice.txt", 0640); err != nil {
		return nil, err
	}
	add("files.Root.Chmod", "Set the file mode to 0640")
	// Passing -1 preserves each ownership ID and exercises the operation without changing the owner.
	if err = root.Chown("archive/notice.txt", -1, -1); err != nil {
		return nil, err
	}
	add("files.Root.Chown", "Kept the existing owner and group with -1, -1")
	date := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	if err = root.Chtimes("archive/notice.txt", date, date); err != nil {
		return nil, err
	}
	add("files.Root.Chtimes", "Set the accession timestamp")
	info, err := root.Stat("archive/notice.txt")
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm() != 0640 || !info.ModTime().Equal(date) {
		return nil, errors.New("metadata differs")
	}
	add("files.Root.Stat", "Verified the exact file mode and timestamp")
	if err = root.Link("archive/notice.txt", "archive/counterfoil.txt"); err != nil {
		return nil, err
	}
	other, err := root.Stat("archive/counterfoil.txt")
	if err != nil || !os.SameFile(info, other) {
		return nil, errors.New("counterfoil differs")
	}
	add("files.Root.Link", "Verified two catalog names for the same file")
	if err = os.Symlink("notice.txt", filepath.Join(dir, "archive", "current")); err != nil {
		return nil, err
	}
	link, available, err := root.LstatIfPossible("archive/current")
	if err != nil || !available || link.Mode()&fs.ModeSymlink == 0 {
		return nil, errors.New("expected an in-directory alias")
	}
	add("files.Root.LstatIfPossible", "Identified the prepared in-directory alias")
	target, err := root.ReadlinkIfPossible("archive/current")
	if err != nil || target != "notice.txt" {
		return nil, errors.New("unexpected alias target")
	}
	add("files.Root.ReadlinkIfPossible", "Read the alias target notice.txt")
	if err = root.Remove("archive/current"); err != nil {
		return nil, err
	}
	add("files.Root.Remove", "Removed the temporary alias")
	if err = root.RemoveAll("drafts"); err != nil {
		return nil, err
	}
	if _, err = root.Stat("drafts"); !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("draft directory remained")
	}
	add("files.Root.RemoveAll", "Removed the emptied draft directory")
	err = root.Close()
	closed = true
	if err != nil {
		return nil, err
	}
	add("files.Root.Close", "Closed the archive")
	return records, nil
}
func main() {
	rows, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err = enc.Encode(rows); err != nil {
		panic(err)
	}
}
