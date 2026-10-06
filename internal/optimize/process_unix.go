//go:build !windows

package optimize

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type processPlatform struct{}

func (p *processPlatform) start(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}
func (p *processPlatform) stop(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
func (p *processPlatform) close() {}
func (p *processPlatform) stats(cmd *exec.Cmd) (float64, uint64) {
	// Linux procfs snapshots; other Unix platforms report unavailable metrics.
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return -1, 0
	}
	var cpu float64
	var rss uint64
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		end := strings.LastIndexByte(string(b), ')')
		if end < 0 {
			continue
		}
		f := strings.Fields(string(b[end+1:]))
		if len(f) < 22 {
			continue
		}
		group, _ := strconv.Atoi(f[2])
		if group != cmd.Process.Pid {
			continue
		}
		user, _ := strconv.ParseFloat(f[11], 64)
		kernel, _ := strconv.ParseFloat(f[12], 64)
		childUser, _ := strconv.ParseFloat(f[13], 64)
		childKernel, _ := strconv.ParseFloat(f[14], 64)
		// Linux exposes these counters in USER_HZ (100), not kernel scheduler HZ.
		cpu += (user + kernel + childUser + childKernel) * 10
		pages, _ := strconv.ParseUint(f[21], 10, 64)
		rss += pages * uint64(os.Getpagesize())
	}
	return cpu, rss
}

func processAlive(pid int) bool { return syscall.Kill(pid, 0) != syscall.ESRCH }
