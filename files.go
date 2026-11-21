package computer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	gbfs "github.com/ewhauser/gbash/fs"
	"github.com/ewhauser/gbash/policy"
)

// files is the computer's files for what acts on them outside a command, under the rules the shell's own commands
// are held to: the same policy decides what may be read and written, so a write lands only in a writable mount,
// where every program sees it, and a file may grow no larger than the shell lets one. A relative name is taken
// from the computer's WorkDir. It is asked for again at each use, since the session is a new one when the tools change.
func (c *Computer) files(ctx context.Context) (gbfs.FileSystem, error) {
	s, err := c.Session(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return &ruled{FileSystem: s.FileSystem(), policy: c.policy, maxFileBytes: s.Limits().MaxFileBytes, dir: c.cfg.WorkDir}, nil
}

// ruled is a session's filesystem behind the session's policy, as gbash puts a command's filesystem behind it.
// Each method asks the policy what the command's own asks: see gbash's commands.CommandFS.
type ruled struct {
	gbfs.FileSystem
	policy       policy.Policy
	maxFileBytes int64
	dir          string // what a relative name is taken from
}

// allowed asks the policy for each name and returns the names made absolute.
func (r *ruled) allowed(ctx context.Context, action policy.FileAction, names ...string) ([]string, error) {
	for i, name := range names {
		names[i] = gbfs.Resolve(r.dir, name)
		if err := policy.CheckPath(ctx, r.policy, r.FileSystem, action, names[i]); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// do runs op on the names once the policy allows action on each.
func (r *ruled) do(ctx context.Context, action policy.FileAction, op func(...string) error, names ...string) error {
	abs, err := r.allowed(ctx, action, names...)
	if err != nil {
		return err
	}
	return op(abs...)
}

func (r *ruled) Open(ctx context.Context, name string) (gbfs.File, error) {
	return r.OpenFile(ctx, name, os.O_RDONLY, 0)
}

func (r *ruled) OpenFile(ctx context.Context, name string, flag int, perm fs.FileMode) (gbfs.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR) != os.O_WRONLY {
		if _, err := r.allowed(ctx, policy.FileActionRead, name); err != nil {
			return nil, err
		}
	}
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		if _, err := r.allowed(ctx, policy.FileActionWrite, name); err != nil {
			return nil, err
		}
	}
	abs := gbfs.Resolve(r.dir, name)
	if flag&os.O_TRUNC == 0 || r.maxFileBytes <= 0 {
		f, err := r.FileSystem.OpenFile(ctx, abs, flag, perm)
		if err != nil {
			return nil, err
		}
		return gbfs.LimitFileSize(f, flag, r.maxFileBytes), nil
	}
	// A file written anew is emptied only once all of it is known to fit: emptied first, one too large would be
	// left cut off at the limit, and what it held before lost.
	f, err := r.FileSystem.OpenFile(ctx, abs, flag&^os.O_TRUNC, perm)
	if err != nil {
		return nil, err
	}
	return &whole{File: f, anew: func() (gbfs.File, error) { return r.FileSystem.OpenFile(ctx, abs, flag&^(os.O_CREATE|os.O_EXCL), perm) }, max: r.maxFileBytes}, nil
}

// whole is a file being written anew, kept in memory until it is closed and written then, if it fits.
type whole struct {
	gbfs.File
	anew    func() (gbfs.File, error) // opens the file again, emptied; it is there by then, so nothing is created
	max     int64
	content []byte
	tooMuch bool
}

func (w *whole) Write(p []byte) (int, error) {
	if w.tooMuch || int64(len(w.content)+len(p)) > w.max {
		w.tooMuch, w.content = true, nil
		return 0, gbfs.ErrFileTooLarge
	}
	w.content = append(w.content, p...)
	return len(p), nil
}

func (w *whole) Close() error {
	if err := w.File.Close(); err != nil || w.tooMuch {
		return err
	}
	f, err := w.anew()
	if err != nil {
		return err
	}
	_, err = f.Write(w.content)
	return errors.Join(err, f.Close())
}

func (r *ruled) Stat(ctx context.Context, name string) (info fs.FileInfo, err error) {
	return info, r.do(ctx, policy.FileActionStat, func(n ...string) error { info, err = r.FileSystem.Stat(ctx, n[0]); return err }, name)
}

func (r *ruled) Lstat(ctx context.Context, name string) (info fs.FileInfo, err error) {
	return info, r.do(ctx, policy.FileActionLstat, func(n ...string) error { info, err = r.FileSystem.Lstat(ctx, n[0]); return err }, name)
}

func (r *ruled) ReadDir(ctx context.Context, name string) (entries []fs.DirEntry, err error) {
	return entries, r.do(ctx, policy.FileActionReadDir, func(n ...string) error { entries, err = r.FileSystem.ReadDir(ctx, n[0]); return err }, name)
}

func (r *ruled) Readlink(ctx context.Context, name string) (target string, err error) {
	return target, r.do(ctx, policy.FileActionReadlink, func(n ...string) error { target, err = r.FileSystem.Readlink(ctx, n[0]); return err }, name)
}

func (r *ruled) Symlink(ctx context.Context, target, link string) error {
	return r.do(ctx, policy.FileActionWrite, func(n ...string) error { return r.FileSystem.Symlink(ctx, target, n[0]) }, link)
}

func (r *ruled) Link(ctx context.Context, from, to string) error {
	if _, err := r.allowed(ctx, policy.FileActionRead, from); err != nil {
		return err
	}
	return r.do(ctx, policy.FileActionWrite, func(n ...string) error { return r.FileSystem.Link(ctx, n[0], n[1]) }, from, to)
}

func (r *ruled) Chown(ctx context.Context, name string, uid, gid uint32, follow bool) error {
	return r.do(ctx, policy.FileActionWrite, func(n ...string) error { return r.FileSystem.Chown(ctx, n[0], uid, gid, follow) }, name)
}

func (r *ruled) Chmod(ctx context.Context, name string, mode fs.FileMode) error {
	return r.do(ctx, policy.FileActionWrite, func(n ...string) error { return r.FileSystem.Chmod(ctx, n[0], mode) }, name)
}

func (r *ruled) Chtimes(ctx context.Context, name string, atime, mtime time.Time) error {
	return r.do(ctx, policy.FileActionWrite, func(n ...string) error { return r.FileSystem.Chtimes(ctx, n[0], atime, mtime) }, name)
}

func (r *ruled) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	return r.do(ctx, policy.FileActionMkdir, func(n ...string) error { return r.FileSystem.MkdirAll(ctx, n[0], perm) }, name)
}

func (r *ruled) Remove(ctx context.Context, name string, recursive bool) error {
	return r.do(ctx, policy.FileActionRemove, func(n ...string) error { return r.FileSystem.Remove(ctx, n[0], recursive) }, name)
}

func (r *ruled) Rename(ctx context.Context, from, to string) error {
	return r.do(ctx, policy.FileActionRename, func(n ...string) error { return r.FileSystem.Rename(ctx, n[0], n[1]) }, from, to)
}
