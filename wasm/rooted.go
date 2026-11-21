package wasm

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/tetratelabs/wazero/experimental/sys"
	wsys "github.com/tetratelabs/wazero/sys"
	"golang.org/x/sys/unix"
)

// rooted is a host directory as a program sees it. Every operation goes through os.Root, which resolves a path
// inside the directory and refuses one that leaves it, by "..", by a symbolic link, or by a link swapped in while
// the path is being walked. wazero's own directory mount resolves paths on the host and can be led out.
//
// A program cannot make a symbolic link, and can open only regular files and directories.
type rooted struct {
	sys.UnimplementedFS
	root     *os.Root
	readOnly bool
}

func newRooted(dir string, readOnly bool) (*rooted, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &rooted{root: root, readOnly: readOnly}, nil
}

// errno is err as WASI reports it. A path os.Root refused has no errno of its own and is a permission error.
func errno(err error) sys.Errno {
	if err == nil {
		return 0
	}
	if strings.Contains(err.Error(), "path escapes") {
		return sys.EPERM
	}
	return sys.UnwrapOSError(err)
}

// at is p as os.Root takes it. A path that ends in a slash names a directory, and the host would follow a
// symbolic link there even where os.Root asks it not to, so the slash is taken off and the name must be a
// directory itself, not a link to one.
func (r *rooted) at(p string) (string, sys.Errno) {
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		return ".", 0
	}
	if trimmed != p {
		info, err := r.root.Lstat(trimmed)
		if err != nil {
			return "", errno(err)
		}
		if !info.IsDir() {
			return "", sys.ENOTDIR
		}
	}
	return trimmed, 0
}

// write is at for an operation that changes the directory, which a read-only one refuses.
func (r *rooted) write(p string) (string, sys.Errno) {
	if r.readOnly {
		return "", sys.EROFS
	}
	return r.at(p)
}

func (r *rooted) OpenFile(p string, flag sys.Oflag, perm fs.FileMode) (sys.File, sys.Errno) {
	const writes = sys.O_RDWR | sys.O_WRONLY | sys.O_CREAT | sys.O_TRUNC | sys.O_APPEND
	if r.readOnly && flag&writes != 0 {
		return nil, sys.EROFS
	}
	p, e := r.at(p)
	if e != 0 {
		return nil, e
	}
	if flag&sys.O_NOFOLLOW != 0 {
		if info, err := r.root.Lstat(p); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return nil, sys.ELOOP
		}
	}
	// Opened without blocking, so that a named pipe returns at once and can be refused below.
	mode := syscall.O_NONBLOCK
	switch flag & (sys.O_RDWR | sys.O_WRONLY) {
	case sys.O_RDWR:
		mode |= os.O_RDWR
	case sys.O_WRONLY:
		mode |= os.O_WRONLY
	}
	for bit, osFlag := range map[sys.Oflag]int{sys.O_APPEND: os.O_APPEND, sys.O_CREAT: os.O_CREATE, sys.O_EXCL: os.O_EXCL, sys.O_TRUNC: os.O_TRUNC,
		sys.O_SYNC: os.O_SYNC, sys.O_DSYNC: os.O_SYNC, sys.O_RSYNC: os.O_SYNC} {
		if flag&bit != 0 {
			mode |= osFlag
		}
	}
	f, err := r.root.OpenFile(p, mode, perm)
	// On macOS, when two programs create one file at the same moment, os.Root reports to one of them that the file
	// does not exist. It does by then, so the open is tried again.
	for try := 0; try < 3 && mode&os.O_CREATE != 0 && errors.Is(err, fs.ErrNotExist); try++ {
		f, err = r.root.OpenFile(p, mode, perm)
	}
	if err != nil {
		return nil, errno(err)
	}
	info, err := f.Stat()
	switch {
	case err != nil:
		_ = f.Close()
		return nil, errno(err)
	case !info.Mode().IsRegular() && !info.IsDir():
		_ = f.Close()
		return nil, sys.EPERM
	case flag&sys.O_DIRECTORY != 0 && !info.IsDir():
		_ = f.Close()
		return nil, sys.ENOTDIR
	}
	return &file{f: f}, 0
}

func (r *rooted) Stat(p string) (wsys.Stat_t, sys.Errno) {
	p, e := r.at(p)
	if e != 0 {
		return wsys.Stat_t{}, e
	}
	info, err := r.root.Stat(p)
	if err != nil {
		return wsys.Stat_t{}, errno(err)
	}
	return wsys.NewStat_t(info), 0
}

func (r *rooted) Lstat(p string) (wsys.Stat_t, sys.Errno) {
	p, e := r.at(p)
	if e != 0 {
		return wsys.Stat_t{}, e
	}
	info, err := r.root.Lstat(p)
	if err != nil {
		return wsys.Stat_t{}, errno(err)
	}
	return wsys.NewStat_t(info), 0
}

func (r *rooted) Readlink(p string) (string, sys.Errno) {
	p, e := r.at(p)
	if e != 0 {
		return "", e
	}
	target, err := r.root.Readlink(p)
	return target, errno(err)
}

func (r *rooted) Mkdir(p string, perm fs.FileMode) sys.Errno {
	p, e := r.write(p)
	if e != 0 {
		return e
	}
	return errno(r.root.Mkdir(p, perm))
}

func (r *rooted) Chmod(p string, perm fs.FileMode) sys.Errno {
	p, e := r.write(p)
	if e != 0 {
		return e
	}
	return errno(r.root.Chmod(p, perm))
}

func (r *rooted) Utimens(p string, atim, mtim int64) sys.Errno {
	p, e := r.write(p)
	if e != 0 {
		return e
	}
	when := func(ns int64) time.Time { // the zero time leaves the file's time as it is
		if ns == sys.UTIME_OMIT {
			return time.Time{}
		}
		return time.Unix(0, ns)
	}
	return errno(r.root.Chtimes(p, when(atim), when(mtim)))
}

func (r *rooted) Rename(from, to string) sys.Errno {
	from, e := r.write(from)
	if e != 0 {
		return e
	}
	if to, e = r.write(to); e != 0 {
		return e
	}
	return errno(r.root.Rename(from, to))
}

// remove removes p when it is a directory, or when it is not, as dir asks.
func (r *rooted) remove(p string, dir bool) sys.Errno {
	p, e := r.write(p)
	if e != 0 {
		return e
	}
	info, err := r.root.Lstat(p)
	switch {
	case err != nil:
		return errno(err)
	case dir && !info.IsDir():
		return sys.ENOTDIR
	case !dir && info.IsDir():
		return sys.EISDIR
	}
	return errno(r.root.Remove(p))
}

func (r *rooted) Rmdir(p string) sys.Errno  { return r.remove(p, true) }
func (r *rooted) Unlink(p string) sys.Errno { return r.remove(p, false) }

// Link makes a hard link. A link to a symbolic link is refused: some systems would link the file it points to.
func (r *rooted) Link(from, to string) sys.Errno {
	from, e := r.write(from)
	if e != 0 {
		return e
	}
	if to, e = r.write(to); e != 0 {
		return e
	}
	info, err := r.root.Lstat(from)
	if err != nil {
		return errno(err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return sys.EPERM
	}
	return errno(r.root.Link(from, to))
}

func (r *rooted) Symlink(string, string) sys.Errno { return sys.EPERM }

// file is an open file or directory of a rooted directory. Nothing it does names a path again, so it stays the
// file that was opened whatever happens to the name it was opened by.
type file struct {
	sys.UnimplementedFile
	f *os.File
}

func (f *file) stat() (wsys.Stat_t, sys.Errno) {
	info, err := f.f.Stat()
	if err != nil {
		return wsys.Stat_t{}, errno(err)
	}
	return wsys.NewStat_t(info), 0
}

func (f *file) Stat() (wsys.Stat_t, sys.Errno) { return f.stat() }
func (f *file) Dev() (uint64, sys.Errno)       { st, e := f.stat(); return st.Dev, e }
func (f *file) Ino() (wsys.Inode, sys.Errno)   { st, e := f.stat(); return st.Ino, e }
func (f *file) IsDir() (bool, sys.Errno)       { st, e := f.stat(); return st.Mode.IsDir(), e }

// flags is the open file's status flags, and the error of asking for them.
func (f *file) flags() (int, error) { return unix.FcntlInt(f.f.Fd(), unix.F_GETFL, 0) }

func (f *file) IsAppend() bool {
	flags, err := f.flags()
	return err == nil && flags&unix.O_APPEND != 0
}

// SetAppend changes the open file's own append mode, so that the system places each write at the end, also when
// another program is appending to the same file.
func (f *file) SetAppend(on bool) sys.Errno {
	flags, err := f.flags()
	if err != nil {
		return errno(err)
	}
	if flags &^= unix.O_APPEND; on {
		flags |= unix.O_APPEND
	}
	_, err = unix.FcntlInt(f.f.Fd(), unix.F_SETFL, flags)
	return errno(err)
}

// read is a read's result as WASI reports it: the end of the file is no bytes and no error.
func read(n int, err error) (int, sys.Errno) {
	if err == io.EOF {
		return n, 0
	}
	return n, errno(err)
}

func (f *file) Read(b []byte) (int, sys.Errno)             { return read(f.f.Read(b)) }
func (f *file) Pread(b []byte, off int64) (int, sys.Errno) { return read(f.f.ReadAt(b, off)) }

func (f *file) Write(b []byte) (int, sys.Errno) { n, err := f.f.Write(b); return n, errno(err) }

func (f *file) Pwrite(b []byte, off int64) (int, sys.Errno) {
	n, err := f.f.WriteAt(b, off)
	return n, errno(err)
}

func (f *file) Seek(offset int64, whence int) (int64, sys.Errno) {
	at, err := f.f.Seek(offset, whence)
	return at, errno(err)
}

func (f *file) Readdir(n int) ([]sys.Dirent, sys.Errno) {
	entries, err := f.f.ReadDir(n)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errno(err)
	}
	dirents := make([]sys.Dirent, 0, len(entries))
	for _, e := range entries {
		d := sys.Dirent{Name: e.Name(), Type: e.Type()}
		if info, err := e.Info(); err == nil {
			d.Ino = wsys.NewStat_t(info).Ino
		}
		dirents = append(dirents, d)
	}
	return dirents, 0
}

func (f *file) Truncate(size int64) sys.Errno { return errno(f.f.Truncate(size)) }
func (f *file) Sync() sys.Errno               { return errno(f.f.Sync()) }
func (f *file) Datasync() sys.Errno           { return errno(f.f.Sync()) }
func (f *file) Close() sys.Errno              { return errno(f.f.Close()) }
