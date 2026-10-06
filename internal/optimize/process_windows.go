//go:build windows

package optimize

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processPlatform struct{ job windows.Handle }

var ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")
var getProcessMemory = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

func (p *processPlatform) start(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	p.job = job
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		p.close()
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP}
	if err = cmd.Start(); err != nil {
		p.close()
		return err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(cmd.Process.Pid))
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		p.close()
		return err
	}
	defer windows.CloseHandle(h)
	if err = windows.AssignProcessToJobObject(job, h); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		p.close()
		return err
	}
	status, _, _ := ntResumeProcess.Call(uintptr(h))
	if status != 0 {
		cmd.Process.Kill()
		cmd.Wait()
		p.close()
		return windows.NTStatus(status)
	}
	return nil
}

func (p *processPlatform) stop(cmd *exec.Cmd) {
	if p.job != 0 {
		_ = windows.TerminateJobObject(p.job, 1)
	}
}
func (p *processPlatform) close() {
	if p.job != 0 {
		windows.CloseHandle(p.job)
		p.job = 0
	}
}

type jobAccounting struct {
	user, kernel, periodUser, periodKernel int64
	faults, processes, active, terminated  uint32
}
type memoryCounters struct {
	size, faults                                                                           uint32
	peakWorking, working, peakPaged, paged, peakNonPaged, nonPaged, pagefile, peakPagefile uintptr
}

func (p *processPlatform) stats(cmd *exec.Cmd) (float64, uint64) {
	var accounting jobAccounting
	if p.job == 0 {
		return 0, 0
	}
	_ = windows.QueryInformationJobObject(p.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	// The job retains CPU of exited descendants. RSS is an instantaneous tree
	// sample and is intentionally not labelled exact owned heap memory.
	var list struct {
		assigned, count uint32
		pids            [256]uintptr
	}
	_ = windows.QueryInformationJobObject(p.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil)
	var rss uint64
	for n := uint32(0); n < list.count && n < 256; n++ {
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(list.pids[n]))
		if err != nil {
			continue
		}
		m := memoryCounters{size: uint32(unsafe.Sizeof(memoryCounters{}))}
		ok, _, _ := getProcessMemory.Call(uintptr(h), uintptr(unsafe.Pointer(&m)), uintptr(m.size))
		if ok != 0 {
			rss += uint64(m.working)
		}
		windows.CloseHandle(h)
	}
	return float64(accounting.user+accounting.kernel) / 1e4, rss
}

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err != windows.ERROR_INVALID_PARAMETER
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return true
	}
	return code == 259
}
