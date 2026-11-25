package computer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sync"
	"syscall"
	"time"

	gbfs "github.com/Lakeshore-Labs/gbash/fs"
	"github.com/spf13/afero"
)

// shellFS is an afero.Fs as the shell sees a mounted directory. The shell and the programs run as WebAssembly are
// given the same filesystem, so a file one writes the other reads, and what a name may reach is decided in one
// place: the filesystem.
//
// No link can be made through it, and a read-only directory cannot be changed.
type shellFS struct {
	fs       afero.Fs
	readOnly bool

	mu  sync.Mutex
	cwd string
}

func (s *shellFS) refused(op, name string) error {
	return &fs.PathError{Op: op, Path: name, Err: fs.ErrPermission}
}

func (s *shellFS) Open(ctx context.Context, name string) (gbfs.File, error) {
	return s.OpenFile(ctx, name, os.O_RDONLY, 0)
}

func (s *shellFS) OpenFile(_ context.Context, name string, flag int, perm fs.FileMode) (gbfs.File, error) {
	if s.readOnly && flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		return nil, s.refused("open", name)
	}
	f, err := s.fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() && !info.IsDir() {
		_ = f.Close()
		return nil, errors.Join(err, s.refused("open", name))
	}
	return f, nil
}

// owned is a mounted file's information with its owner as the shell sees it. gbash's cd asks the mode bits for the
// shell's user, who is not the host's, so in a host directory the shell would be anyone else and need a directory
// open to everyone on the host. As a user namespace maps one id to another, the host process's user and group are
// the shell's own here and in Chown, and any other owner of the host is itself: what the host's process may open
// the shell may, and no more. A file with no owner of the host's, as in memory, is the shell user's own. gbash's
// own host filesystems wrap their information the same way, and tell two files apart by Sys, which is kept.
type owned struct{ fs.FileInfo }

func (o owned) Ownership() (gbfs.FileOwnership, bool) {
	own, ok := gbfs.OwnershipFromSys(o.Sys())
	if !ok {
		return gbfs.DefaultOwnership(), true
	}
	if own.UID == uint32(os.Getuid()) {
		own.UID = gbfs.DefaultOwnerUID
	}
	if own.GID == uint32(os.Getgid()) {
		own.GID = gbfs.DefaultOwnerGID
	}
	return own, true
}

func own(info fs.FileInfo, err error) (fs.FileInfo, error) {
	if err != nil {
		return nil, err
	}
	return owned{info}, nil
}

func (s *shellFS) Stat(_ context.Context, name string) (fs.FileInfo, error) {
	return own(s.fs.Stat(name))
}

func (s *shellFS) Lstat(_ context.Context, name string) (fs.FileInfo, error) {
	if l, ok := s.fs.(afero.Lstater); ok {
		info, _, err := l.LstatIfPossible(name)
		return own(info, err)
	}
	return own(s.fs.Stat(name))
}

func (s *shellFS) ReadDir(_ context.Context, name string) ([]fs.DirEntry, error) {
	infos, err := afero.ReadDir(s.fs, name)
	entries := make([]fs.DirEntry, len(infos))
	for i, info := range infos {
		entries[i] = fs.FileInfoToDirEntry(owned{info})
	}
	return entries, err
}

func (s *shellFS) Readlink(_ context.Context, name string) (string, error) {
	if l, ok := s.fs.(afero.LinkReader); ok {
		return l.ReadlinkIfPossible(name)
	}
	return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid}
}

// Realpath is name itself once it is known to exist: the filesystem resolves names, and none leads out of it. So
// the shell follows a link that is already in a mount, to wherever in that mount it leads, and gbash's policy
// against links refuses only one whose target is missing. What keeps a link from leading out is the filesystem.
func (s *shellFS) Realpath(ctx context.Context, name string) (string, error) {
	if _, err := s.Lstat(ctx, name); err != nil {
		return "", err
	}
	return gbfs.Clean(name), nil
}

func (s *shellFS) Symlink(_ context.Context, _, link string) error { return s.refused("symlink", link) }
func (s *shellFS) Link(_ context.Context, _, link string) error    { return s.refused("link", link) }

// change runs op, which changes the directory, unless the directory is read-only.
func (s *shellFS) change(op, name string, do func() error) error {
	if s.readOnly {
		return s.refused(op, name)
	}
	return do()
}

// Chown takes the shell's user and group to be the host process's, as owned gives them: cp -p and a move between
// mounts give a copy its source's owner, which the host would refuse as the shell names it.
func (s *shellFS) Chown(_ context.Context, name string, uid, gid uint32, _ bool) error {
	host := func(id, shells uint32, hosts int) int {
		if id == shells {
			return hosts
		}
		return int(id)
	}
	return s.change("chown", name, func() error {
		return s.fs.Chown(name, host(uid, gbfs.DefaultOwnerUID, os.Getuid()), host(gid, gbfs.DefaultOwnerGID, os.Getgid()))
	})
}

func (s *shellFS) Chmod(_ context.Context, name string, mode fs.FileMode) error {
	return s.change("chmod", name, func() error { return s.fs.Chmod(name, mode) })
}

func (s *shellFS) Chtimes(_ context.Context, name string, atime, mtime time.Time) error {
	return s.change("chtimes", name, func() error { return s.fs.Chtimes(name, atime, mtime) })
}

// MkdirAll of a directory that is there changes nothing, read-only or not: gbash makes the working directory
// before each command.
func (s *shellFS) MkdirAll(_ context.Context, name string, perm fs.FileMode) error {
	if info, err := s.fs.Stat(name); err == nil && info.IsDir() {
		return nil
	}
	return s.change("mkdir", name, func() error { return s.fs.MkdirAll(name, perm) })
}

func (s *shellFS) Remove(_ context.Context, name string, recursive bool) error {
	return s.change("remove", name, func() error {
		if recursive {
			return s.fs.RemoveAll(name)
		}
		// afero's memory filesystem removes a directory that has entries and strands them.
		if entries, _ := afero.ReadDir(s.fs, name); len(entries) > 0 {
			return &fs.PathError{Op: "remove", Path: name, Err: syscall.ENOTEMPTY}
		}
		return s.fs.Remove(name)
	})
}

func (s *shellFS) Rename(_ context.Context, from, to string) error {
	return s.change("rename", from, func() error { return s.fs.Rename(from, to) })
}

func (s *shellFS) Getwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

func (s *shellFS) Chdir(name string) error {
	if info, err := s.fs.Stat(name); err != nil {
		return err
	} else if !info.IsDir() {
		return &fs.PathError{Op: "chdir", Path: name, Err: errors.New("not a directory")}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cwd = gbfs.Clean(name)
	return nil
}
