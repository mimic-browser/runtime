//go:build !windows

package cdp

import "os/exec"

func configureRendererProcess(command *exec.Cmd) {}
