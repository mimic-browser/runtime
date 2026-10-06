//go:build windows

package cliui

import (
	"golang.org/x/sys/windows"
	"os"
)

func enableTerminalColor(out *os.File) bool {
	var mode uint32
	handle := windows.Handle(out.Fd())
	if windows.GetConsoleMode(handle, &mode) != nil {
		return false
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
