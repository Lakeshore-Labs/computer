package computer

import (
	"os"
	"path/filepath"
	"testing"
)

// The shell's cd asks the mode bits for the shell's user, who is not the host's: a home that others on the host
// may not enter is still the shell's to enter, as it is the shell's to read and write.
func TestTheShellEntersADirectoryOthersOnTheHostMayNot(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sub", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o750); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{WorkDir: "/home/user", Mounts: []Mount{{Guest: "/home/user", Host: home}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if got := run(t, c, "cd sub/deep && pwd && cd .. && pwd && cd ~ && pwd && cd /home/user/sub && echo x > f && cat f && test -O . && echo mine"); got != "/home/user/sub/deep\n/home/user/sub\n/home/user\nx\nmine" {
		t.Errorf("got %q", got)
	}
	// What tells two files apart still does, for the shell and for a file written through the computer.
	if got := run(t, c, "cd ~; echo a > a; ln a b 2>/dev/null; cp a c; test a -ef a && echo same; test a -ef c || echo different; ls -ld sub | cut -c1-10"); got != "same\ndifferent\ndrwx------" {
		t.Errorf("got %q", got)
	}
}
