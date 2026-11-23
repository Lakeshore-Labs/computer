//go:build !darwin

package main

import "fmt"

func terminalPath(fd int) (string, error) {
	return fmt.Sprintf("/dev/fd/%d", fd), nil
}
