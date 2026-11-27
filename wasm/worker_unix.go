//go:build unix

package wasm

import (
	"syscall"

	"github.com/tetratelabs/wazero/experimental"
	"golang.org/x/sys/unix"
)

const canWork = true

// procAttr puts a worker in a process group of its own, so that an interrupt typed at a terminal reaches only the
// parent, which stops the run it is for, and not every worker.
func procAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// mappedMemory keeps a run's memory in pages mapped from the system: the most the run may grow to is reserved, and
// made usable as it grows, so that growing neither moves nor copies it, and the Go heap, which a worker does not
// collect during a run, holds none of it.
type mappedMemory struct{}

func (mappedMemory) Allocate(_, max uint64) experimental.LinearMemory {
	return &mapped{max: max}
}

type mapped struct {
	max      uint64
	reserved []byte
}

func (m *mapped) Reallocate(size uint64) []byte {
	if m.reserved == nil {
		b, err := unix.Mmap(-1, 0, int(m.max), unix.PROT_NONE, unix.MAP_ANON|unix.MAP_PRIVATE)
		if err != nil {
			return nil
		}
		m.reserved = b
	}
	if size > m.max || size > 0 && unix.Mprotect(m.reserved[:size], unix.PROT_READ|unix.PROT_WRITE) != nil {
		return nil
	}
	return m.reserved[:size:size]
}

func (m *mapped) Free() {
	if m.reserved != nil {
		_ = unix.Munmap(m.reserved)
		m.reserved = nil
	}
}
