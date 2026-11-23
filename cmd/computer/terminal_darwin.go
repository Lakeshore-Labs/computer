package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// On macOS /dev/fd/N duplicates the open file description, including its blocking
// mode. Reopen the terminal device itself to give each command a separate handle.
func terminalPath(fd int) (string, error) {
	var path [unix.PathMax]byte
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETPATH, uintptr(unsafe.Pointer(&path[0])))
	if errno != 0 {
		return "", errno
	}
	return unix.ByteSliceToString(path[:]), nil
}
