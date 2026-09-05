//go:build windows

package engine

import "syscall"

// processAlive reports whether a process with the given pid is currently
// running, using OpenProcess (the reliable kernel-level probe).
// OpenProcess returns ERROR_INVALID_PARAMETER (87) for a dead pid;
// PROCESS_QUERY_INFORMATION (0x400) is sufficient to probe existence.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(handle)
	return true
}