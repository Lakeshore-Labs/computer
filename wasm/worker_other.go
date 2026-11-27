//go:build !unix

package wasm

import (
	"syscall"

	"github.com/tetratelabs/wazero/experimental"
)

// Workers need memory mapped from the system, which is written here only for Unix systems; elsewhere programs run
// in this process.
const canWork = false

func procAttr() *syscall.SysProcAttr { return nil }

type mappedMemory struct{}

func (mappedMemory) Allocate(_, _ uint64) experimental.LinearMemory { return nil }
