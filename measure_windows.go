// +build windows

package main

import (
	"context"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsTimer struct {
	userTime   int64
	kernelTime int64
	realTime   int64
	jobObject  windows.Handle
}

const (
	JobObjectBasicAccountingInformation = 1
	HundredNSTicks                      = 100
)

type JOBOBJECT_BASIC_AND_IO_ACCOUNTING_INFORMATION struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func init() {
	processTimer = new(windowsTimer)
}

func (w *windowsTimer) Run(ctx context.Context, name string, arg ...string) error {
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags:    windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED,
		NoInheritHandles: false,
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	pid := uint32(cmd.Process.Pid)

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer terminateJobObject(job)

	hProcess, err := windows.OpenProcess(windows.SPECIFIC_RIGHTS_ALL, false, pid)
	if err != nil {
		return err
	}
	if err := windows.AssignProcessToJobObject(job, hProcess); err != nil {
		return err
	}
	windows.CloseHandle(hProcess)

	if hProcess, err = getMainThreadOfPID(pid); err != nil {
		return err
	}
	defer windows.CloseHandle(hProcess)

	startTime := time.Now()
	windows.ResumeThread(hProcess)
	if err := cmd.Wait(); err != nil {
		return err
	}
	w.realTime = int64(time.Since(startTime))

	var info JOBOBJECT_BASIC_AND_IO_ACCOUNTING_INFORMATION
	if err := queryBasicJobBasicAccountingInfo(job, &info); err != nil {
		return err
	}
	w.userTime = info.TotalUserTime
	w.kernelTime = info.TotalKernelTime

	return nil
}

func (w *windowsTimer) GetUserTime() int64 { return w.userTime * HundredNSTicks }

func (w *windowsTimer) GetKernelTime() int64 { return w.kernelTime * HundredNSTicks }

func (w *windowsTimer) GetRealTime() int64 { return w.realTime }

func (w *windowsTimer) Reset() {
	w.userTime = 0
	w.kernelTime = 0
	w.realTime = 0
}

// getMainThreadOfPID retrieves the main handle of a process.
func getMainThreadOfPID(pid uint32) (windows.Handle, error) {
	hSnapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return windows.InvalidHandle, err
	}
	defer windows.CloseHandle(hSnapshot)

	var threadEntry windows.ThreadEntry32
	threadEntry.Size = uint32(unsafe.Sizeof(threadEntry))

	var hThread windows.Handle
	err = windows.Thread32First(hSnapshot, &threadEntry)
	for err == nil {
		if threadEntry.OwnerProcessID == pid {
			hThread, err = windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, threadEntry.ThreadID)
			if err != nil {
				return windows.InvalidHandle, err
			}
			break
		}
		err = windows.Thread32Next(hSnapshot, &threadEntry)
	}
	return hThread, err
}

// queryBasicJobBasicAccountingInfo retrieves basic accounting information for a job object.
func queryBasicJobBasicAccountingInfo(job windows.Handle, info *JOBOBJECT_BASIC_AND_IO_ACCOUNTING_INFORMATION) error {
	return windows.QueryInformationJobObject(job,
		JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(info)),
		uint32(unsafe.Sizeof(*info)), nil)
}

// terminateJobObject terminates the job object and its child processes before closing the handle.
func terminateJobObject(job windows.Handle) error {
	if err := windows.TerminateJobObject(job, 0); err != nil {
		return err
	}
	return windows.CloseHandle(job)
}
