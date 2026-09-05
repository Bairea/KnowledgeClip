//go:build !windows

package engine

import (
	"os"
	"syscall"
)

// processAlive reports whether a process with the given pid is currently
// running, using signal 0 (probe-only, never delivered).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}