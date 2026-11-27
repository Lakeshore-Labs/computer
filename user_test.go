package computer

import "testing"

// A computer given a user is that user's: whoami, id -un and logname name them, and $USER and $LOGNAME are theirs.
func TestAComputerIsItsUsers(t *testing.T) {
	c := newComputer(t, Config{User: "maya"})
	if got := run(t, c, "whoami; id -un; logname; echo $USER $LOGNAME"); got != "maya\nmaya\nmaya\nmaya maya" {
		t.Errorf("got %q", got)
	}
}

// A computer given a user has that user's home, /home/<user>, as on Unix, wherever its commands start.
func TestAUsersHomeIsTheirsWhereverCommandsStart(t *testing.T) {
	c, err := New(Config{
		User:    "maya",
		Mounts:  []Mount{{Guest: "/home/maya", Host: t.TempDir()}, {Guest: "/srv/notes", Host: t.TempDir()}},
		WorkDir: "/srv/notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if got := run(t, c, "echo $HOME; cd ~ && pwd"); got != "/home/maya\n/home/maya" {
		t.Errorf("got %q", got)
	}
}
