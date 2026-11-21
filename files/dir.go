// Package files has the filesystems a computer can be given. A mount is an afero.Fs, so anything that implements
// one can be a directory of the computer: memory, a database, an object store. Dir is a directory of the host.
package files

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

// A Root is a host directory as an afero.Fs that cannot be left. Every operation goes through os.Root, which
// resolves a name inside the directory and refuses one that leads out of it, by "..", by a symbolic link, or by a
// link swapped in while the name is being walked. afero's own BasePathFs compares path prefixes, which a symbolic
// link defeats.
//
// Only regular files and directories can be opened, and no symbolic link can be made.
type Root struct{ root *os.Root }

// Dir opens the host directory at path. Close it when the computer it was given to is done.
func Dir(path string) (*Root, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &Root{root}, nil
}

func (r *Root) Close() error { return r.root.Close() }
func (r *Root) Name() string { return "Root" }

// in is name as os.Root takes it: relative to the directory, which is itself ".". A name that ends in a slash
// names a directory, and the host follows a symbolic link there even where os.Root asks it not to (on macOS, with
// Go 1.26), so the slash is taken off and the name must be a directory itself, not a link to one.
func (r *Root) in(name string) (string, error) {
	trimmed := strings.TrimRight(name, "/")
	if rel := strings.TrimLeft(trimmed, "/"); rel == "" {
		return ".", nil
	} else if trimmed == name {
		return rel, nil
	} else if info, err := r.root.Lstat(rel); err != nil {
		return "", fix(err)
	} else if !info.IsDir() {
		return "", &fs.PathError{Op: "open", Path: name, Err: syscall.ENOTDIR}
	} else {
		return rel, nil
	}
}

// fix is err as a caller understands it: a name os.Root refused is one the caller may not use.
func fix(err error) error {
	if err != nil && strings.Contains(err.Error(), "path escapes") {
		return &fs.PathError{Op: "open", Path: "", Err: syscall.EPERM}
	}
	return err
}

// with runs op on name, or on both names.
func (r *Root) with(op func(...string) error, names ...string) error {
	for i, name := range names {
		in, err := r.in(name)
		if err != nil {
			return err
		}
		names[i] = in
	}
	return fix(op(names...))
}

func (r *Root) Create(name string) (afero.File, error) {
	return r.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
}

func (r *Root) Open(name string) (afero.File, error) { return r.OpenFile(name, os.O_RDONLY, 0) }

func (r *Root) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	in, err := r.in(name)
	if err != nil {
		return nil, err
	}
	// Opened without blocking, so that a named pipe returns at once and can be refused below.
	f, err := r.root.OpenFile(in, flag|syscall.O_NONBLOCK, perm)
	// On macOS, when two programs create one file at the same moment, os.Root reports to one of them that the file
	// does not exist. It does by then, so the open is tried again.
	for try := 0; try < 3 && flag&os.O_CREATE != 0 && errors.Is(err, fs.ErrNotExist); try++ {
		f, err = r.root.OpenFile(in, flag|syscall.O_NONBLOCK, perm)
	}
	if err != nil {
		return nil, fix(err)
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() && !info.IsDir() {
		_ = f.Close()
		return nil, errors.Join(err, &fs.PathError{Op: "open", Path: name, Err: syscall.EPERM})
	}
	return &File{f}, nil
}

func (r *Root) Mkdir(name string, perm os.FileMode) error {
	return r.with(func(n ...string) error { return r.root.Mkdir(n[0], perm) }, name)
}

func (r *Root) MkdirAll(name string, perm os.FileMode) error {
	return r.with(func(n ...string) error { return r.root.MkdirAll(n[0], perm) }, name)
}

func (r *Root) Remove(name string) error {
	return r.with(func(n ...string) error { return r.root.Remove(n[0]) }, name)
}

func (r *Root) RemoveAll(name string) error {
	return r.with(func(n ...string) error { return r.root.RemoveAll(n[0]) }, name)
}

func (r *Root) Rename(from, to string) error {
	return r.with(func(n ...string) error { return r.root.Rename(n[0], n[1]) }, from, to)
}

func (r *Root) Chmod(name string, mode os.FileMode) error {
	return r.with(func(n ...string) error { return r.root.Chmod(n[0], mode) }, name)
}

func (r *Root) Chown(name string, uid, gid int) error {
	return r.with(func(n ...string) error { return r.root.Chown(n[0], uid, gid) }, name)
}

func (r *Root) Chtimes(name string, atime, mtime time.Time) error {
	return r.with(func(n ...string) error { return r.root.Chtimes(n[0], atime, mtime) }, name)
}

func (r *Root) Stat(name string) (os.FileInfo, error) {
	in, err := r.in(name)
	if err != nil {
		return nil, err
	}
	info, err := r.root.Stat(in)
	return info, fix(err)
}

// LstatIfPossible is afero's Lstater.
func (r *Root) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	in, err := r.in(name)
	if err != nil {
		return nil, true, err
	}
	info, err := r.root.Lstat(in)
	return info, true, fix(err)
}

// ReadlinkIfPossible is afero's LinkReader.
func (r *Root) ReadlinkIfPossible(name string) (string, error) {
	in, err := r.in(name)
	if err != nil {
		return "", err
	}
	target, err := r.root.Readlink(in)
	return target, fix(err)
}

// Link makes a hard link. A link to a symbolic link is refused: some systems would link the file it points to.
func (r *Root) Link(from, to string) error {
	return r.with(func(n ...string) error {
		if info, err := r.root.Lstat(n[0]); err != nil {
			return err
		} else if info.Mode()&fs.ModeSymlink != 0 {
			return &os.LinkError{Op: "link", Old: from, New: to, Err: syscall.EPERM}
		}
		return r.root.Link(n[0], n[1])
	}, from, to)
}

// A File is an open file or directory of a Root. Nothing it does names a path again, so it stays the file that
// was opened whatever happens to the name it was opened by.
type File struct{ *os.File }

// IsAppend reports whether the system places each write at the end of the file.
func (f *File) IsAppend() bool {
	flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
	return err == nil && flags&unix.O_APPEND != 0
}

// SetAppend changes the open file's own append mode, which holds also when another program appends to the file.
func (f *File) SetAppend(on bool) error {
	flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	if flags &^= unix.O_APPEND; on {
		flags |= unix.O_APPEND
	}
	_, err = unix.FcntlInt(f.Fd(), unix.F_SETFL, flags)
	return err
}
