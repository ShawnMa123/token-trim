//go:build windows

package exec

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

const (
	createSuspended       = 0x00000004
	createNewProcessGroup = 0x00000200
	processSetQuota       = 0x0100
	processTerminate      = 0x0001
	threadSuspendResume   = 0x0002
	jobObjectExtendedInfo = 9
	jobLimitKillOnClose   = 0x2000
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procOpenThread               = kernel32.NewProc("OpenThread")
	procResumeThread             = kernel32.NewProc("ResumeThread")
	procThread32First            = kernel32.NewProc("Thread32First")
	procThread32Next             = kernel32.NewProc("Thread32Next")
)

type jobBasicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobExtendedLimit struct {
	BasicLimitInformation jobBasicLimit
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type threadEntry32 struct {
	Size           uint32
	CntUsage       uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePri        int32
	DeltaPri       int32
	Flags          uint32
}

// procTree owns a kill-on-close Job Object. The child is created suspended,
// assigned to the job, and only then resumed, so descendant processes cannot
// escape containment by racing the assignment.
type procTree struct {
	job syscall.Handle
}

func startTree(cmd *exec.Cmd) (*procTree, error) {
	job, err := createJob()
	if err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createSuspended | createNewProcessGroup,
	}
	if err := cmd.Start(); err != nil {
		syscall.CloseHandle(job)
		return nil, err
	}
	t := &procTree{job: job}
	if err := t.assign(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		syscall.CloseHandle(job)
		return nil, err
	}
	if err := resumeProcess(uint32(cmd.Process.Pid)); err != nil {
		t.kill()
		syscall.CloseHandle(job)
		_ = cmd.Process.Kill()
		return nil, err
	}
	return t, nil
}

func createJob() (syscall.Handle, error) {
	r1, _, e1 := procCreateJobObjectW.Call(0, 0)
	if r1 == 0 {
		return 0, fmt.Errorf("CreateJobObject: %v", e1)
	}
	job := syscall.Handle(r1)
	var info jobExtendedLimit
	info.BasicLimitInformation.LimitFlags = jobLimitKillOnClose
	r2, _, e2 := procSetInformationJobObject.Call(
		uintptr(job),
		jobObjectExtendedInfo,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r2 == 0 {
		syscall.CloseHandle(job)
		return 0, fmt.Errorf("SetInformationJobObject: %v", e2)
	}
	return job, nil
}

func (t *procTree) assign(pid int) error {
	h, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer syscall.CloseHandle(h)
	r1, _, e1 := procAssignProcessToJobObject.Call(uintptr(t.job), uintptr(h))
	if r1 == 0 {
		return fmt.Errorf("AssignProcessToJobObject: %v", e1)
	}
	return nil
}

func resumeProcess(pid uint32) error {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer syscall.CloseHandle(snap)

	var entry threadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	r1, _, e1 := procThread32First.Call(uintptr(snap), uintptr(unsafe.Pointer(&entry)))
	resumed := 0
	for r1 != 0 {
		if entry.OwnerProcessID == pid {
			h, _, _ := procOpenThread.Call(threadSuspendResume, 0, uintptr(entry.ThreadID))
			if h != 0 {
				r2, _, e2 := procResumeThread.Call(h)
				syscall.CloseHandle(syscall.Handle(h))
				if r2 == ^uintptr(0) {
					return fmt.Errorf("ResumeThread: %v", e2)
				}
				resumed++
			}
		}
		r1, _, _ = procThread32Next.Call(uintptr(snap), uintptr(unsafe.Pointer(&entry)))
	}
	if resumed == 0 {
		return fmt.Errorf("no thread found for suspended process %d: %v", pid, e1)
	}
	return nil
}

func (t *procTree) kill() {
	_, _, _ = procTerminateJobObject.Call(uintptr(t.job), 1)
}

func (t *procTree) close() {
	if t.job != 0 {
		syscall.CloseHandle(t.job)
		t.job = 0
	}
}
