package wasm

import (
	"errors"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/afero/mem"
	"github.com/tetratelabs/wazero/experimental/sys"
	wsys "github.com/tetratelabs/wazero/sys"
)

// dirFS is an afero.Fs as a program sees a directory. wazero's own directory mount resolves paths on the host and
// can be led out of the directory; here the filesystem given decides what a name reaches.
//
// A program cannot make a symbolic link, can open only regular files and directories, and cannot change a
// read-only directory.
type dirFS struct {
	sys.UnimplementedFS
	fs       afero.Fs
	readOnly bool
	dev      uint64 // the device of its files that have no inode of their own
}

// devices numbers each dirFS, so that the files of two memory filesystems are not the same file.
var devices atomic.Uint64

func newDirFS(fs afero.Fs, readOnly bool) *dirFS {
	return &dirFS{fs: fs, readOnly: readOnly, dev: devices.Add(1)}
}

// errno is err as WASI reports it.
func errno(err error) sys.Errno {
	if err == nil {
		return 0
	}
	return sys.UnwrapOSError(err)
}

// lstat is the file itself, where the filesystem can tell a link from what it points to.
func (d *dirFS) lstat(name string) (os.FileInfo, error) {
	if l, ok := d.fs.(afero.Lstater); ok {
		info, _, err := l.LstatIfPossible(name)
		return info, err
	}
	return d.fs.Stat(name)
}

// change runs op, which changes the directory, unless the directory is read-only.
func (d *dirFS) change(op func() error) sys.Errno {
	if d.readOnly {
		return sys.EROFS
	}
	return errno(op())
}

func (d *dirFS) OpenFile(p string, flag sys.Oflag, perm fs.FileMode) (sys.File, sys.Errno) {
	const writes = sys.O_RDWR | sys.O_WRONLY | sys.O_CREAT | sys.O_TRUNC | sys.O_APPEND
	if d.readOnly && flag&writes != 0 {
		return nil, sys.EROFS
	}
	name := "/" + p
	if flag&sys.O_NOFOLLOW != 0 {
		if info, err := d.lstat(name); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return nil, sys.ELOOP
		}
	}
	mode := os.O_RDONLY
	switch flag & (sys.O_RDWR | sys.O_WRONLY) {
	case sys.O_RDWR:
		mode = os.O_RDWR
	case sys.O_WRONLY:
		mode = os.O_WRONLY
	}
	for bit, osFlag := range map[sys.Oflag]int{sys.O_APPEND: os.O_APPEND, sys.O_CREAT: os.O_CREATE, sys.O_EXCL: os.O_EXCL,
		sys.O_TRUNC: os.O_TRUNC} {
		if flag&bit != 0 {
			mode |= osFlag
		}
	}
	// A read stays exactly O_RDONLY: afero's memory and read-only filesystems hand out a file that can be written
	// for any other flags, and syncing means nothing to a read.
	if mode != os.O_RDONLY && flag&(sys.O_SYNC|sys.O_DSYNC|sys.O_RSYNC) != 0 {
		mode |= os.O_SYNC
	}
	f, err := open(d.fs, name, mode, perm)
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
	return &file{f: f, fs: d.fs, name: name, dev: d.dev, appends: flag&sys.O_APPEND != 0}, 0
}

// open is OpenFile, a read by the filesystem's Open: afero's layered filesystem (CopyOnWriteFs) joins a
// directory's layers there alone, and a directory opened by OpenFile lists one layer.
func open(fsys afero.Fs, name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag == os.O_RDONLY {
		return fsys.Open(name)
	}
	return fsys.OpenFile(name, flag, perm)
}

// stat is info, of the file at name on device dev, as WASI reports it. A filesystem without inodes, such as
// afero's memory one, would give every file inode 0, which makes any two files the same file; such a file's inode
// is then a hash of its name, and its device dev.
func stat(info os.FileInfo, name string, dev uint64) wsys.Stat_t {
	st := wsys.NewStat_t(info)
	if st.Ino == 0 {
		h := fnv.New64a()
		h.Write([]byte(path.Clean(name)))
		st.Ino, st.Dev = h.Sum64(), dev
	}
	return st
}

// inMemory reports whether f is a file of afero's memory filesystem, alone or under a BasePathFs.
func inMemory(f afero.File) bool {
	if b, ok := f.(*afero.BasePathFile); ok {
		f = b.File
	}
	_, ok := f.(*mem.File)
	return ok
}

// keepsItsPlace reports whether f, a directory, lists from where it was after a seek to its start: afero's
// memory directories, and the joined directory of its layered filesystem, alone or under a BasePathFs.
func keepsItsPlace(f afero.File) bool {
	for b, ok := f.(*afero.BasePathFile); ok; b, ok = f.(*afero.BasePathFile) {
		f = b.File
	}
	_, joined := f.(*afero.UnionFile)
	return joined || inMemory(f)
}

func (d *dirFS) Stat(p string) (wsys.Stat_t, sys.Errno) {
	info, err := d.fs.Stat("/" + p)
	if err != nil {
		return wsys.Stat_t{}, errno(err)
	}
	return stat(info, "/"+p, d.dev), 0
}

func (d *dirFS) Lstat(p string) (wsys.Stat_t, sys.Errno) {
	info, err := d.lstat("/" + p)
	if err != nil {
		return wsys.Stat_t{}, errno(err)
	}
	return stat(info, "/"+p, d.dev), 0
}

func (d *dirFS) Readlink(p string) (string, sys.Errno) {
	l, ok := d.fs.(afero.LinkReader)
	if !ok {
		return "", sys.EINVAL
	}
	target, err := l.ReadlinkIfPossible("/" + p)
	return target, errno(err)
}

func (d *dirFS) Mkdir(p string, perm fs.FileMode) sys.Errno {
	return d.change(func() error { return d.fs.Mkdir("/"+p, perm) })
}

func (d *dirFS) Chmod(p string, perm fs.FileMode) sys.Errno {
	return d.change(func() error { return d.fs.Chmod("/"+p, perm) })
}

func (d *dirFS) Rename(from, to string) sys.Errno {
	return d.change(func() error { return d.fs.Rename("/"+from, "/"+to) })
}

func (d *dirFS) Utimens(p string, atim, mtim int64) sys.Errno {
	return d.change(func() error {
		info, err := d.fs.Stat("/" + p)
		if err != nil {
			return err
		}
		when := func(ns int64) time.Time { // a time left out stays what it is, as nearly as the filesystem tells it
			if ns == sys.UTIME_OMIT {
				return info.ModTime()
			}
			return time.Unix(0, ns)
		}
		return d.fs.Chtimes("/"+p, when(atim), when(mtim))
	})
}

// remove removes p when it is a directory, or when it is not, as dir asks.
func (d *dirFS) remove(p string, dir bool) sys.Errno {
	return d.change(func() error {
		info, err := d.lstat("/" + p)
		switch {
		case err != nil:
			return err
		case dir && !info.IsDir():
			return sys.ENOTDIR
		case !dir && info.IsDir():
			return sys.EISDIR
		}
		if dir { // afero's memory filesystem removes a directory that has entries and strands them
			if entries, _ := afero.ReadDir(d.fs, "/"+p); len(entries) > 0 {
				return sys.ENOTEMPTY
			}
		}
		return d.fs.Remove("/" + p)
	})
}

func (d *dirFS) Rmdir(p string) sys.Errno  { return d.remove(p, true) }
func (d *dirFS) Unlink(p string) sys.Errno { return d.remove(p, false) }

// Link makes a hard link where the filesystem has them.
func (d *dirFS) Link(from, to string) sys.Errno {
	l, ok := d.fs.(interface{ Link(from, to string) error })
	if !ok {
		return sys.ENOSYS
	}
	return d.change(func() error { return l.Link("/"+from, "/"+to) })
}

func (d *dirFS) Symlink(string, string) sys.Errno { return sys.EPERM }

// file is an open file or directory.
type file struct {
	sys.UnimplementedFile
	f    afero.File
	fs   afero.Fs // the filesystem f was opened from, and its name there
	name string
	dev  uint64
	// appends is used for a file that cannot set its own append mode: each write is then placed at the end by a
	// seek before it, which is sound only while nothing else appends to the file.
	appends bool
}

// appender is a file whose own append mode can be read and changed, as a host file's can.
type appender interface {
	IsAppend() bool
	SetAppend(bool) error
}

func (f *file) stat() (wsys.Stat_t, sys.Errno) {
	info, err := f.f.Stat()
	if err != nil {
		return wsys.Stat_t{}, errno(err)
	}
	return stat(info, f.name, f.dev), 0
}

func (f *file) Stat() (wsys.Stat_t, sys.Errno) { return f.stat() }
func (f *file) Dev() (uint64, sys.Errno)       { st, e := f.stat(); return st.Dev, e }
func (f *file) Ino() (wsys.Inode, sys.Errno)   { st, e := f.stat(); return st.Ino, e }
func (f *file) IsDir() (bool, sys.Errno)       { st, e := f.stat(); return st.Mode.IsDir(), e }

func (f *file) IsAppend() bool {
	if a, ok := f.f.(appender); ok {
		return a.IsAppend()
	}
	return f.appends
}

func (f *file) SetAppend(on bool) sys.Errno {
	if a, ok := f.f.(appender); ok {
		return errno(a.SetAppend(on))
	}
	f.appends = on
	return 0
}

// read is a read's result as WASI reports it: the end of the file is no bytes and no error. afero's memory files
// report a read that starts past the end as io.ErrUnexpectedEOF, which from another file is a real error.
func (f *file) read(n int, err error) (int, sys.Errno) {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) && inMemory(f.f) {
		return n, 0
	}
	return n, errno(err)
}

func (f *file) Read(b []byte) (int, sys.Errno)             { return f.read(f.f.Read(b)) }
func (f *file) Pread(b []byte, off int64) (int, sys.Errno) { return f.read(f.f.ReadAt(b, off)) }

func (f *file) Write(b []byte) (int, sys.Errno) {
	if _, own := f.f.(appender); !own && f.appends {
		if _, err := f.f.Seek(0, io.SeekEnd); err != nil {
			return 0, errno(err)
		}
	}
	n, err := f.f.Write(b)
	return n, errno(err)
}

func (f *file) Pwrite(b []byte, off int64) (int, sys.Errno) {
	n, err := f.f.WriteAt(b, off)
	return n, errno(err)
}

// Seek of a directory to its start is how wazero rewinds it to list it again. afero's memory directories, and a
// layered filesystem's joined one, keep their place in the listing across a seek, so such a directory is opened
// again by its name instead; any other
// file seeks itself, which stays right when the directory is renamed.
func (f *file) Seek(offset int64, whence int) (int64, sys.Errno) {
	if offset == 0 && whence == io.SeekStart && keepsItsPlace(f.f) {
		if info, err := f.f.Stat(); err == nil && info.IsDir() {
			again, err := f.fs.Open(f.name)
			if err != nil {
				return 0, errno(err)
			}
			_ = f.f.Close()
			f.f = again
			return 0, 0
		}
	}
	at, err := f.f.Seek(offset, whence)
	return at, errno(err)
}

func (f *file) Readdir(n int) ([]sys.Dirent, sys.Errno) {
	entries, err := f.f.Readdir(n)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errno(err)
	}
	dirents := make([]sys.Dirent, 0, len(entries))
	for _, info := range entries {
		dirents = append(dirents, sys.Dirent{Name: info.Name(), Type: info.Mode().Type(), Ino: stat(info, path.Join(f.name, info.Name()), f.dev).Ino})
	}
	return dirents, 0
}

func (f *file) Truncate(size int64) sys.Errno { return errno(f.f.Truncate(size)) }
func (f *file) Sync() sys.Errno               { return errno(f.f.Sync()) }
func (f *file) Datasync() sys.Errno           { return errno(f.f.Sync()) }
func (f *file) Close() sys.Errno              { return errno(f.f.Close()) }
