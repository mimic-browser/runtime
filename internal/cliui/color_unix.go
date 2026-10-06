//go:build !windows

package cliui

import "os"

func enableTerminalColor(out *os.File) bool { return true }
