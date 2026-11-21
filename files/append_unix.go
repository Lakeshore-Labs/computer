//go:build unix

package files

import "golang.org/x/sys/unix"

// The append mode is read and set with fcntl, which exists only on Unix systems. Elsewhere File has neither
// method, and the filesystem reports the file as not append-only.

// IsAppend reports whether the system places each write at the end of the file.
func (f *File) IsAppend() bool {
	flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
	return err == nil && flags&unix.O_APPEND != 0
}

// SetAppend changes the open file's own append mode, which holds also when another program appends to the file.
func (f *File) SetAppend(on bool) error {
	flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	if flags &^= unix.O_APPEND; on {
		flags |= unix.O_APPEND
	}
	_, err = unix.FcntlInt(f.Fd(), unix.F_SETFL, flags)
	return err
}
