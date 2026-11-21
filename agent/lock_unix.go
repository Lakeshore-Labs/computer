//go:build unix

package agent

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// lock takes the conversation in dir for this run, or fails when another process has it. Pi Durable's storage
// has one writer and no lock of its own. The operating system releases the lock when the file is closed, however
// the process ends.
func lock(dir string) (release func(), err error) {
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
