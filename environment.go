package computer

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/ewhauser/gbash"
	gbfs "github.com/ewhauser/gbash/fs"
	"github.com/ewhauser/gbash/policy"
)

// An Environment is the computer as the place an agent harness's own tools act: shell commands, and files, under
// the rules the shell's commands are held to. Its methods use only the standard library's types, so a
// harness can declare the interface it needs and be given this without either importing the other.
//
// A name is an absolute path in the computer. An error is a *fs.PathError naming that path and never the host's,
// and errors.Is tells fs.ErrNotExist, fs.ErrPermission (also a write the rules refuse), fs.ErrExist, fs.ErrInvalid
// (also a file past the size limit), syscall.ENOTDIR, syscall.EISDIR and syscall.ENOTEMPTY apart. Which of them a
// misuse gets is the mount's to say: a host directory answers as a file system does, and a mount that is another
// afero.Fs as that one does: afero's memory filesystem allows a file made under a file, which turns that file
// into a directory and loses what it held.
type Environment struct{ c *Computer }

// Environment is the computer as an Environment. Every method may be called from any goroutine.
func (c *Computer) Environment() *Environment { return &Environment{c} }

// Describe is what a model is told about this computer, with the commands that call tools as they are now.
func (c *Computer) Describe(ctx context.Context) (string, error) {
	if _, err := c.Session(ctx); err != nil {
		return "", err
	}
	return c.About(c.ToolCommands(), c.ToolModules()), nil
}

// Run runs script in the computer's shell with dir as its working directory, and returns its exit code. env is
// added to the shell's environment, or is the whole of it when replaceEnv is set. Each Run has a shell of its own
// over the computer's files: nothing is carried from one to the next, and one neither waits for another nor for
// the computer's own Exec. Output is written to stdout and stderr while the command runs, and a pipeline's
// commands may write at the same time. When ctx ends the command is stopped and ctx's error returned. A command
// that ran and failed, or that the shell does not have (127), is an exit code and no error.
func (e *Environment) Run(ctx context.Context, script, dir string, env map[string]string, replaceEnv bool, stdout, stderr io.Writer) (int, error) {
	if _, err := e.c.Session(ctx); err != nil { // the tools as they are now
		return 0, err
	}
	e.c.mu.Lock()
	rt := e.c.runtime
	e.c.mu.Unlock()
	s, err := rt.NewSession(ctx)
	if err != nil {
		return 0, err
	}
	if env == nil {
		env = map[string]string{}
	}
	res, err := s.Exec(ctx, &gbash.ExecutionRequest{Script: script, WorkDir: dir, Env: env, ReplaceEnv: replaceEnv, Stdout: stdout, Stderr: stderr})
	switch {
	case ctx.Err() != nil:
		return 0, ctx.Err()
	case res == nil:
		return 0, err
	}
	return res.ExitCode, nil
}

// told is err as a caller is told it: about name, the path in the computer, with the kind of failure kept.
func told(op, name string, err error) error {
	if err == nil {
		return nil
	}
	var path *fs.PathError
	var link *os.LinkError
	var errno syscall.Errno
	kind := err
	switch {
	case policy.IsDenied(err):
		kind = fs.ErrPermission
	case errors.Is(err, gbfs.ErrFileTooLarge):
		kind = tooLarge{}
	case errors.Is(err, syscall.EINVAL):
		kind = fs.ErrInvalid
	case errors.As(err, &errno):
		kind = errno
	case errors.As(err, &path):
		kind = path.Err
	case errors.As(err, &link):
		kind = link.Err
	default:
		for _, known := range []error{fs.ErrNotExist, fs.ErrPermission, fs.ErrExist, fs.ErrInvalid} {
			if errors.Is(err, known) {
				kind = known
			}
		}
	}
	return &fs.PathError{Op: op, Path: name, Err: kind}
}

// tooLarge is a file past the shell's limit, which a caller is told as invalid.
type tooLarge struct{}

func (tooLarge) Error() string        { return "file too large" }
func (tooLarge) Is(target error) bool { return target == fs.ErrInvalid }

// Stat describes a file; a symbolic link itself when follow is false.
func (e *Environment) Stat(ctx context.Context, name string, follow bool) (fs.FileInfo, error) {
	files, err := e.c.files(ctx)
	if err != nil {
		return nil, err
	}
	stat := files.Lstat
	if follow {
		stat = files.Stat
	}
	info, err := stat(ctx, name)
	return info, told("stat", name, err)
}

// Read reads length bytes of a file from offset, or as many as it has; the rest of it when length is negative.
func (e *Environment) Read(ctx context.Context, name string, offset, length int64) ([]byte, error) {
	files, err := e.c.files(ctx)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, told("read", name, fs.ErrInvalid)
	}
	if info, err := files.Stat(ctx, name); err == nil && info.IsDir() {
		return nil, told("read", name, syscall.EISDIR)
	}
	f, err := files.Open(ctx, name)
	if err != nil {
		return nil, told("read", name, err)
	}
	defer f.Close()
	if seeker, ok := f.(io.Seeker); ok {
		_, err = seeker.Seek(offset, io.SeekStart)
	} else { // a file that cannot seek is read up to the offset, and that much thrown away
		_, err = io.CopyN(io.Discard, f, offset)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, told("read", name, err)
	}
	var from io.Reader = f
	if length >= 0 {
		from = io.LimitReader(f, length)
	}
	b, err := io.ReadAll(from)
	return b, told("read", name, err)
}

// Write writes the file anew, creating it, or adds to its end when appending.
func (e *Environment) Write(ctx context.Context, name string, content []byte, appending bool) error {
	files, err := e.c.files(ctx)
	if err != nil {
		return err
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if appending {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := files.OpenFile(ctx, name, flag, 0o644)
	if err != nil {
		return told("write", name, err)
	}
	_, err = f.Write(content)
	return told("write", name, errors.Join(err, f.Close()))
}

// Truncate makes the file size bytes long, cutting its end off or adding zero bytes to it.
func (e *Environment) Truncate(ctx context.Context, name string, size int64) error {
	// ponytail: the filesystem has no truncate, so the kept part is read and written back; a truncate of its own
	// when files too large to hold twice are cut.
	files, err := e.c.files(ctx)
	switch {
	case err != nil:
		return err
	case size < 0:
		return told("truncate", name, fs.ErrInvalid)
	case files.maxFileBytes > 0 && size > files.maxFileBytes: // before that much memory is asked for
		return told("truncate", name, gbfs.ErrFileTooLarge)
	}
	kept, err := e.Read(ctx, name, 0, size)
	if err != nil {
		return err
	}
	return e.Write(ctx, name, append(kept, make([]byte, size-int64(len(kept)))...), false)
}

// List is a directory's entries in the order of their names.
func (e *Environment) List(ctx context.Context, name string) ([]fs.FileInfo, error) {
	files, err := e.c.files(ctx)
	if err != nil {
		return nil, err
	}
	if info, err := files.Stat(ctx, name); err == nil && !info.IsDir() {
		return nil, told("list", name, syscall.ENOTDIR)
	}
	entries, err := files.ReadDir(ctx, name)
	if err != nil {
		return nil, told("list", name, err)
	}
	infos := make([]fs.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil { // one removed since the listing is left out
			infos = append(infos, info)
		}
	}
	slices.SortFunc(infos, func(a, b fs.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })
	return infos, nil
}

// Mkdir makes a directory, and with parents every directory above it that is missing; with parents, one that is
// there already is no error.
func (e *Environment) Mkdir(ctx context.Context, name string, parents bool) error {
	files, err := e.c.files(ctx)
	if err != nil {
		return err
	}
	if !parents { // the filesystem makes parents always, so what a plain mkdir refuses is refused here
		if _, err := files.Lstat(ctx, name); err == nil {
			return told("mkdir", name, fs.ErrExist)
		}
		if info, err := files.Stat(ctx, gbfs.Resolve(name, "..")); err != nil {
			return told("mkdir", name, err)
		} else if !info.IsDir() {
			return told("mkdir", name, syscall.ENOTDIR)
		}
	}
	return told("mkdir", name, files.MkdirAll(ctx, name, 0o755))
}

// Remove removes a file or an empty directory, or with recursive a directory and everything in it. Nothing there
// to remove is fs.ErrNotExist.
func (e *Environment) Remove(ctx context.Context, name string, recursive bool) error {
	files, err := e.c.files(ctx)
	if err != nil {
		return err
	}
	if _, err := files.Lstat(ctx, name); err != nil {
		return told("remove", name, err)
	}
	return told("remove", name, files.Remove(ctx, name, recursive))
}

// Rename moves a file or a directory.
func (e *Environment) Rename(ctx context.Context, from, to string) error {
	files, err := e.c.files(ctx)
	if err != nil {
		return err
	}
	return told("rename", from, files.Rename(ctx, from, to))
}
