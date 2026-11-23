package computer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lakeshore-Labs/gbash"
)

func TestAProgramCannotFollowALinkOutOfAMount(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	rel, err := filepath.Rel(home, outside)
	if err != nil {
		t.Fatal(err)
	}
	// A link a host process left in the mount, which neither the shell nor a program made.
	if err := os.Symlink(outside, filepath.Join(home, "planted")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "inside.txt"), []byte("INSIDE"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newComputer(t, Config{Python: python(t), Mounts: []Mount{{Guest: "/data", Host: home}}})
	// A positive control: a program can read a file inside the mount, so a missing secret below is the
	// mount's doing and not a mount that cannot be read at all.
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: `python3 -c "print(open('/data/inside.txt').read())"`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout) != "INSIDE" {
		t.Fatalf("a file inside the mount is not readable: stdout %q stderr %q", res.Stdout, res.Stderr)
	}
	for name, script := range map[string]string{
		"python links by a relative path": `cd /data; python3 -c "import os; os.symlink('` + rel + `', 'r'); print(open('r').read())"`,
		"python reads a planted link":     `cd /data; python3 -c "print(open('planted').read())"`,
		"shell reads a planted link":      `cat /data/planted`,
		"python makes the link":           `python3 -c "import os; os.symlink('` + outside + `', 'a'); print(open('a').read())"`,
		"shell makes the link":            `ln -s ` + outside + ` b; python3 -c "print(open('b').read())"`,
		"shell reads the link":            `ln -s ` + outside + ` c; cat c`,
		"relative climb":                  `python3 -c "print(open('../../../../../../../../` + outside + `').read())"`,
	} {
		res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: script})
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(res.Stderr), "\n")
		t.Logf("%s: exit %d stdout %q stderr %q", name, res.ExitCode, res.Stdout, lines[len(lines)-1])
		if strings.Contains(res.Stdout, "TOPSECRET") {
			t.Errorf("%s: read a file outside every mount", name)
		}
	}
}
