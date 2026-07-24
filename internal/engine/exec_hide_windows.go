//go:build windows

package engine

import (
	"os/exec"
	"syscall"
)

// hideWindow sets the CREATE_NO_WINDOW flag on Windows to prevent console
// popup windows from flashing each time browser-act CLI is invoked.
// Without this, every exec.Command call spawns a visible cmd window that
// appears briefly and disappears, which is disruptive with high-frequency
// polling (dozens of popups per minute across multiple sites).
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}
