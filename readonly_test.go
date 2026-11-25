package computer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// contents is every name in a filesystem with its mode, time and content, to see that nothing changed.
func contents(t *testing.T, fsys afero.Fs) string {
	t.Helper()
	var lines []string
	err := afero.Walk(fsys, "/", func(name string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		line := name + " " + info.Mode().String() + " " + info.ModTime().String()
		if info.Mode().IsRegular() {
			content, err := afero.ReadFile(fsys, name)
			if err != nil {
				return err
			}
			line += " " + string(content)
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// refusedAsReadOnly reports whether a message is a permission error, in the words a read-only directory or a
// refused permission is told in, and not the words for a path outside the computer.
func refusedAsReadOnly(message string) bool {
	m := strings.ToLower(message)
	return (strings.Contains(m, "read-only file system") || strings.Contains(m, "permission denied")) && !strings.Contains(m, "outside allowed roots")
}

// Each way of changing a file, tried on a read-only mount by the shell, the file tools and Python, is refused as a
// permission error, not as a path outside the computer, and the mount is as it was: a host directory and an afero
// filesystem alike.
func TestAReadOnlyMountRefusesAChangeAsReadOnly(t *testing.T) {
	shell := []string{
		"echo x > /ro/new.txt",
		"echo x > /ro/keep.txt",
		"echo x >> /ro/keep.txt",
		": > /ro/keep.txt",
		"truncate -s 0 /ro/keep.txt",
		"touch /ro/new.txt",
		"touch /ro/keep.txt",
		"mkdir /ro/made",
		"mkdir -p /ro/dir/made",
		"rm /ro/keep.txt",
		"rm -r /ro/dir",
		"rm -rf /ro",
		"rmdir /ro/empty",
		"chmod 600 /ro/keep.txt",
		"mv /ro/keep.txt /ro/moved.txt",
		"mv /home/user/w.txt /ro/w.txt",
		"mv /ro/keep.txt /home/user/keep.txt",
		"mv /ro /home/user/moved",
		"cp /home/user/w.txt /ro/w.txt",
		"ln -s keep.txt /ro/link",
		"tee /ro/tee.txt < /home/user/w.txt",
	}
	host := t.TempDir()
	memory := afero.NewMemMapFs()
	for _, fsys := range []afero.Fs{afero.NewBasePathFs(afero.NewOsFs(), host), memory} {
		if err := errors.Join(afero.WriteFile(fsys, "/keep.txt", []byte("kept"), 0o644), fsys.MkdirAll("/dir/sub", 0o755),
			afero.WriteFile(fsys, "/dir/sub/f.txt", []byte("f"), 0o644), fsys.Mkdir("/empty", 0o755)); err != nil {
			t.Fatal(err)
		}
	}
	for name, mount := range map[string]Mount{
		"host directory": {Guest: "/ro", Host: host, ReadOnly: true},
		"afero":          {Guest: "/ro", FS: memory, ReadOnly: true},
	} {
		t.Run(name, func(t *testing.T) {
			view := memory
			if mount.FS == nil {
				view = afero.NewBasePathFs(afero.NewOsFs(), host)
			}
			before := contents(t, view)
			c := newComputer(t, Config{Python: python(t), Mounts: []Mount{mount}})
			for _, script := range shell {
				run(t, c, "echo w > /home/user/w.txt")
				res := exec(t, c, script, 0)
				if res.ExitCode == 0 && script != "rm -rf /ro" { // rm -f reports, but does not fail
					t.Errorf("%s: succeeded", script)
				}
				if !refusedAsReadOnly(res.Stderr) {
					t.Errorf("%s: told %q", script, res.Stderr)
				}
			}

			ctx, env := context.Background(), c.Environment()
			for op, err := range map[string]error{
				"write":            env.Write(ctx, "/ro/new.txt", []byte("x"), false),
				"overwrite":        env.Write(ctx, "/ro/keep.txt", []byte("x"), false),
				"append":           env.Write(ctx, "/ro/keep.txt", []byte("x"), true),
				"truncate":         env.Truncate(ctx, "/ro/keep.txt", 0),
				"mkdir":            env.Mkdir(ctx, "/ro/made", true),
				"remove":           env.Remove(ctx, "/ro/keep.txt", false),
				"remove all":       env.Remove(ctx, "/ro/dir", true),
				"rename within":    env.Rename(ctx, "/ro/keep.txt", "/ro/moved.txt"),
				"rename in":        env.Rename(ctx, "/home/user/w.txt", "/ro/w.txt"),
				"rename out":       env.Rename(ctx, "/ro/keep.txt", "/home/user/keep.txt"),
				"rename the mount": env.Rename(ctx, "/ro", "/home/user/moved"),
			} {
				if !errors.Is(err, fs.ErrPermission) || !refusedAsReadOnly(err.Error()) {
					t.Errorf("the file tools' %s: %v", op, err)
				}
			}

			// Python is told what WASI tells it. A rename between two mounts is not a call WASI has, and os.chmod
			// changes nothing under WASI, which has no call for it either; the mount is seen unchanged below.
			res := exec(t, c, `python3 -c "
import os
tries = [
    lambda: open('/ro/new.txt', 'w'),
    lambda: open('/ro/keep.txt', 'a'),
    lambda: os.truncate('/ro/keep.txt', 0),
    lambda: os.mkdir('/ro/made'),
    lambda: os.remove('/ro/keep.txt'),
    lambda: os.rmdir('/ro/empty'),
    lambda: os.rename('/ro/keep.txt', '/ro/moved.txt'),
    lambda: os.utime('/ro/keep.txt', (0, 0)),
    lambda: os.symlink('keep.txt', '/ro/link'),
    lambda: os.rename('/home/user/w.txt', '/ro/w.txt'),
    lambda: os.rename('/ro/keep.txt', '/home/user/keep.txt'),
    lambda: os.chmod('/ro/keep.txt', 0o600),
]
for change in tries:
    try:
        change()
        print('changed')
    except OSError as e:
        print(e.strerror)
"`, 0)
			want := strings.Repeat("Read-only file system\n", 8) + "Operation not permitted\n" + strings.Repeat("Function not implemented\n", 2) + "changed"
			if got := strings.TrimSpace(res.Stdout); got != want {
				t.Errorf("python was told\n%s\nnot\n%s\n%s", got, want, res.Stderr)
			}

			if after := contents(t, view); after != before {
				t.Errorf("the read-only mount changed:\nbefore\n%s\nafter\n%s", before, after)
			}
			if _, err := os.Stat(host); err != nil {
				t.Fatal(err)
			}
		})
	}
}
