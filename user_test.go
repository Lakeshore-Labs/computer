package computer

import "testing"

// A computer given a user is that user's: whoami, id -un and logname name them, and $USER and $LOGNAME are theirs.
func TestAComputerIsItsUsers(t *testing.T) {
	c := newComputer(t, Config{User: "maya"})
	if got := run(t, c, "whoami; id -un; logname; echo $USER $LOGNAME"); got != "maya\nmaya\nmaya\nmaya maya" {
		t.Errorf("got %q", got)
	}
}
