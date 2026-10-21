package cluster

import (
	"os"
	"syscall"
	"unsafe"
)

// Cogpit's child processes on Windows, for ending a credential plugin's run (pluginproc.go): the
// Toolhelp snapshot names each process's parent and program file, not its command line, so a
// plugin is found by its program's file name alone; its start time comes from GetProcessTimes.
// Windows has no process groups as the search means them, and no exec that keeps a process's
// number, so a run's process is never taken for the one new child (pluginproc.go). Its finding and
// ending of a child is tried on Windows by pluginproc_windows_test.go (first run 2026-10-21), which
// CI's Unit Tests (windows) runs.

// processQueryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION, enough for GetProcessTimes.
const processQueryLimitedInformation = 0x1000

// listChildren returns Cogpit's child processes with their programs' file names.
func listChildren() []childProcess {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer func() { _ = syscall.CloseHandle(snap) }()
	self := uint32(os.Getpid()) // #nosec G115 -- a process number fits a DWORD
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []childProcess
	for err = syscall.Process32First(snap, &entry); err == nil; err = syscall.Process32Next(snap, &entry) {
		if entry.ParentProcessID != self || entry.ProcessID == 0 {
			continue
		}
		start, ok := processStart(entry.ProcessID)
		if !ok {
			continue
		}
		out = append(out, childProcess{id: procID{pid: int(entry.ProcessID), start: start}, exe: syscall.UTF16ToString(entry.ExeFile[:])})
	}
	return out
}

// processStart is when process pid was created, as a FILETIME's 100 ns units; ok is false when it
// cannot be read.
func processStart(pid uint32) (int64, bool) {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return 0, false
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	return int64(creation.HighDateTime)<<32 | int64(creation.LowDateTime), true
}

// commandLine is not known on Windows: another process's command line is not readable without its
// memory.
func commandLine(int) []string { return nil }

// canSignal reports whether Cogpit may terminate process pid.
func canSignal(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid)) // #nosec G115 -- a process number fits a DWORD
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}

// signalPlugin ends a credential plugin's process; Windows has no SIGTERM, so both steps terminate
// it.
func signalPlugin(pid int, _ bool) error {
	h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid)) // #nosec G115 -- a process number fits a DWORD
	if err != nil {
		return err
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	return syscall.TerminateProcess(h, 1)
}
