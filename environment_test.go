package computer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ewhauser/gbash"
	"github.com/ewhauser/gbash/policy"
)

// The interface pi-durable-go declares for a session's sandbox, which an Environment is to satisfy as it stands.
var _ interface {
	Run(ctx context.Context, script, dir string, env map[string]string, replaceEnv bool, stdout, stderr io.Writer) (exitCode int, err error)
	Stat(ctx context.Context, name string, follow bool) (fs.FileInfo, error)
	Read(ctx context.Context, name string, offset, length int64) ([]byte, error)
	Write(ctx context.Context, name string, content []byte, appending bool) error
	Truncate(ctx context.Context, name string, size int64) error
	List(ctx context.Context, name string) ([]fs.FileInfo, error)
	Mkdir(ctx context.Context, name string, parents bool) error
	Remove(ctx context.Context, name string, recursive bool) error
	Rename(ctx context.Context, from, to string) error
} = (*Environment)(nil)

// arrivals keeps when each piece of output arrived.
type arrivals struct {
	mu   sync.Mutex
	text bytes.Buffer
	at   []time.Time
}

func (a *arrivals) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.at = append(a.at, time.Now())
	return a.text.Write(p)
}

func TestAnEnvironmentRunsCommandsAndKeepsFilesUnderTheShellsRules(t *testing.T) {
	ctx := context.Background()
	c := newComputer(t, Config{Options: []gbash.Option{gbash.WithLimitOverrides(policy.Limits{MaxFileBytes: 64})}})
	e := c.Environment()

	// A command's output arrives while it runs, it starts in the directory given, and nothing is carried over.
	out := new(arrivals)
	began := time.Now()
	code, err := e.Run(ctx, "pwd; export KEPT=1; sleep 0.5; nosuchcommand", "/tmp", nil, false, out, out)
	if err != nil || code != 127 || !strings.HasPrefix(out.text.String(), "/tmp\n") || !strings.Contains(out.text.String(), "not found") {
		t.Fatalf("got %d, %v, %q", code, err, out.text.String())
	}
	if first := out.at[0].Sub(began); first > 400*time.Millisecond {
		t.Errorf("the first output arrived after %v, when the command had ended", first)
	}
	out = new(arrivals)
	if code, err := e.Run(ctx, `echo "${KEPT:-none} $PWD"`, "/home/user", nil, false, out, out); err != nil || code != 0 || out.text.String() != "none /home/user\n" {
		t.Fatalf("a second command got %d, %v, %q", code, err, out.text.String())
	}
	stopped, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, err := e.Run(stopped, "sleep 30", "/home/user", nil, false, io.Discard, io.Discard); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a command past its deadline: %v", err)
	}

	// Files, and each failure a harness tells apart.
	if err := e.Write(ctx, "/home/user/a.txt", []byte("0123456789"), false); err != nil {
		t.Fatal(err)
	}
	if err := e.Write(ctx, "/home/user/a.txt", []byte("ab"), true); err != nil {
		t.Fatal(err)
	}
	if b, err := e.Read(ctx, "/home/user/a.txt", 8, 3); err != nil || string(b) != "89a" {
		t.Fatalf("a range read %q, %v", b, err)
	}
	if b, err := e.Read(ctx, "/home/user/a.txt", 10, -1); err != nil || string(b) != "ab" {
		t.Fatalf("the rest read %q, %v", b, err)
	}
	if err := e.Truncate(ctx, "/home/user/a.txt", 4); err != nil {
		t.Fatal(err)
	}
	if info, err := e.Stat(ctx, "/home/user/a.txt", true); err != nil || info.Size() != 4 || !info.Mode().IsRegular() {
		t.Fatalf("stat: %v, %v", info, err)
	}
	if err := e.Mkdir(ctx, "/home/user/d/e", true); err != nil {
		t.Fatal(err)
	}
	if err := e.Rename(ctx, "/home/user/a.txt", "/home/user/d/b.txt"); err != nil {
		t.Fatal(err)
	}
	if infos, err := e.List(ctx, "/home/user/d"); err != nil || len(infos) != 2 || infos[0].Name() != "b.txt" || !infos[1].IsDir() {
		t.Fatalf("list: %v, %v", infos, err)
	}
	for what, c := range map[string]struct{ got, want error }{
		"reading what is not there":           {second(e.Read(ctx, "/home/user/none", 0, -1)), fs.ErrNotExist},
		"removing what is not there":          {e.Remove(ctx, "/home/user/none", true), fs.ErrNotExist},
		"writing outside the folders":         {e.Write(ctx, "/etc/x", []byte("no"), false), fs.ErrPermission},
		"writing a file past the limit":       {e.Write(ctx, "/home/user/big", bytes.Repeat([]byte("x"), 100), false), fs.ErrInvalid},
		"reading a directory":                 {second(e.Read(ctx, "/home/user/d", 0, -1)), syscall.EISDIR},
		"listing a file":                      {second(e.List(ctx, "/home/user/d/b.txt")), syscall.ENOTDIR},
		"making a directory that is there":    {e.Mkdir(ctx, "/home/user/d", false), fs.ErrExist},
		"making one under a missing parent":   {e.Mkdir(ctx, "/home/user/x/y", false), fs.ErrNotExist},
		"removing a directory that has files": {e.Remove(ctx, "/home/user/d", false), syscall.ENOTEMPTY},
	} {
		if !errors.Is(c.got, c.want) {
			t.Errorf("%s: got %v, want %v", what, c.got, c.want)
		}
		if c.got != nil && !strings.Contains(c.got.Error(), "/home/user") && !strings.Contains(c.got.Error(), "/etc/x") {
			t.Errorf("%s: the error does not name the computer's path: %v", what, c.got)
		}
	}
	if err := e.Remove(ctx, "/home/user/d", true); err != nil {
		t.Fatal(err)
	}
	if text, err := c.Describe(ctx); err != nil || !strings.Contains(text, "/home/user") {
		t.Fatalf("describe: %q, %v", text, err)
	}
}

func second[T any](_ T, err error) error { return err }
