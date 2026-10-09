//go:build windows

package cdp

import (
	"os/exec"
	"syscall"
)

func configureRendererProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
