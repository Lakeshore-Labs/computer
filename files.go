package computer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	gbfs "github.com/Lakeshore-Labs/gbash/fs"
	"github.com/Lakeshore-Labs/gbash/policy"
	"github.com/spf13/afero/mem"
)

// files is the computer's files for what acts on them outside a command, under the rules the shell's own commands
// are held to: the same policy decides what may be read and written, so a write lands only in a writable mount,
// where every program sees it, and a file may grow no larger than the shell lets one. A relative name is taken
// from the computer's WorkDir. It is asked for again at each use, since the session is a new one when the tools
// change; the tools are listed only when there is no session yet, so a file is not kept waiting on an MCP server.
func (c *Computer) files(ctx context.Context) (*ruled, error) {
	c.mu.Lock()
	s := c.session
	c.mu.Unlock()
	if s == nil {
		var err error
		if s, err = c.Session(ctx); err != nil {
			return nil, err
		}
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
	anew, adding := flag&os.O_TRUNC != 0, flag&os.O_APPEND != 0
	if !anew && !adding || r.maxFileBytes <= 0 {
		f, err := r.FileSystem.OpenFile(ctx, abs, flag, perm)
		if err != nil {
			return nil, err
		}
		return gbfs.LimitFileSize(f, flag, r.maxFileBytes), nil
	}
	// A file written anew, or added to, is changed only once all of what is written is known to fit: changed as it
	// is written, one too large would be left cut off at the limit, and what a file written anew held before lost.
	// A file this open makes is taken away again if the write is refused. Only an exclusive open proves the file is
	// this one's; one another command made meanwhile is opened as it is. ponytail: a name that is a dangling
	// symlink is not followed here, so a refused write through one leaves its target made and empty.
	open, mine := flag&^os.O_TRUNC, false
	if _, err := r.FileSystem.Lstat(ctx, abs); errors.Is(err, fs.ErrNotExist) && flag&os.O_CREATE != 0 {
		open, mine = open|os.O_EXCL, true
	}
	f, err := r.FileSystem.OpenFile(ctx, abs, open, perm)
	if mine && errors.Is(err, fs.ErrExist) && flag&os.O_EXCL == 0 {
		mine = false
		f, err = r.FileSystem.OpenFile(ctx, abs, flag&^os.O_TRUNC, perm)
	}
	if err != nil {
		return nil, err
	}
	w := &whole{File: f, room: r.maxFileBytes}
	if mine {
		w.made = func() error {
			made, errMade := f.Stat()
			now, errNow := r.FileSystem.Lstat(ctx, abs)
			if errMade != nil || errNow != nil || !same(made, now) || now.Size() != 0 {
				return nil // no longer only what this open made
			}
			return r.FileSystem.Remove(ctx, abs, false)
		}
	}
	if anew {
		w.put = func(content []byte) error { // emptied now; it is there by now, so nothing is created
			if err := f.Close(); err != nil {
				return err
			}
			again, err := r.FileSystem.OpenFile(ctx, abs, flag&^(os.O_CREATE|os.O_EXCL), perm)
			if err != nil {
				return err
			}
			_, err = again.Write(content)
			return errors.Join(err, again.Close())
		}
		return w, nil
	}
	if info, err := f.Stat(); err == nil {
		w.room -= info.Size()
	}
	w.put = func(content []byte) error {
		_, err := f.Write(content)
		return errors.Join(err, f.Close())
	}
	return w, nil
}

// whole is a file being written anew or added to, whose writes are kept in memory until it is closed and made
// then, if they fit.
type whole struct {
	gbfs.File
	put     func(content []byte) error // makes the writes and closes the file
	made    func() error               // removes the file, when opening it made it; nil when it was there before
	room    int64                      // how much may be written
	content []byte
	tooMuch bool
}

func (w *whole) Write(p []byte) (int, error) {
	if w.tooMuch || int64(len(w.content)+len(p)) > w.room {
		w.tooMuch, w.content = true, nil
		return 0, gbfs.ErrFileTooLarge
	}
	w.content = append(w.content, p...)
	return len(p), nil
}

func (w *whole) Close() error {
	if w.tooMuch {
		var err error
		if w.made != nil {
			err = w.made() // while the file is open, to know it is still the one made
		}
		return errors.Join(err, w.File.Close())
	}
	return w.put(w.content)
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

// same reports whether a and b describe one file. ponytail: kinds of mount other than the host's directories and
// memory report false, so a refused write leaves their new file; add a case for one that matters.
func same(a, b fs.FileInfo) bool {
	ma, okA := a.(*mem.FileInfo)
	mb, okB := b.(*mem.FileInfo)
	if okA && okB {
		return ma.FileData == mb.FileData
	}
	return os.SameFile(a, b)
}
