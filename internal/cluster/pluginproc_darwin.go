package cluster

import (
	"bytes"
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"
)

// Cogpit's child processes on macOS, for ending a credential plugin's run (pluginproc.go): libproc's
// proc_listchildpids, each child's start time and process group from proc_pidinfo
// (PROC_PIDTBSDINFO), and the command line from sysctl kern.procargs2, called as the kernel's own
// system calls so that the package stays free of cgo (cogpit-cli is pure Go).

const (
	procInfoCallListPIDs = 1  // PROC_INFO_CALL_LISTPIDS (sys/proc_info.h)
	procInfoCallPIDInfo  = 2  // PROC_INFO_CALL_PIDINFO
	procPPIDOnly         = 6  // PROC_PPID_ONLY: the processes whose parent is the given one
	procPIDTBSDInfo      = 3  // PROC_PIDTBSDINFO: struct proc_bsdinfo
	ctlKern              = 1  // CTL_KERN
	kernProcArgs2        = 49 // KERN_PROCARGS2: argc, the executable's path, argv, the environment
	maxChildren          = 1024
)

// struct proc_bsdinfo (sys/proc_info.h): its size and the offsets of the fields read.
const (
	bsdInfoSize     = 136
	bsdInfoPPID     = 16  // pbi_ppid, uint32
	bsdInfoPGID     = 100 // pbi_pgid, uint32
	bsdInfoStartSec = 120 // pbi_start_tvsec, uint64
	bsdInfoStartUs  = 128 // pbi_start_tvusec, uint64
)

// listChildren returns Cogpit's child processes, without their command lines.
func listChildren() []childProcess {
	pids := make([]int32, maxChildren)
	// #nosec G103 -- proc_info(PROC_INFO_CALL_LISTPIDS, PROC_PPID_ONLY, ppid) fills the slice's array, bounded by its size.
	n, _, errno := syscall.Syscall6(syscall.SYS_PROC_INFO, procInfoCallListPIDs, procPPIDOnly, uintptr(os.Getpid()), 0,
		uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)*4))
	if errno != 0 {
		return nil
	}
	self, group := os.Getpid(), syscall.Getpgrp()
	var out []childProcess
	for _, pid := range pids[:min(int(n)/4, len(pids))] {
		if pid <= 0 {
			continue
		}
		ppid, pgid, start, ok := procBSDInfo(int(pid))
		if !ok || ppid != self {
			continue
		}
		out = append(out, childProcess{id: procID{pid: int(pid), start: start}, group: pgid == group})
	}
	return out
}

// procBSDInfo is a process's parent, process group and start time (µs since the epoch); ok is false
// when it cannot be read (it ended, or is another user's).
func procBSDInfo(pid int) (ppid, pgid int, start int64, ok bool) {
	var buf [bsdInfoSize]byte
	// #nosec G103 -- proc_info(PROC_INFO_CALL_PIDINFO, pid, PROC_PIDTBSDINFO) fills buf, bounded by its size.
	n, _, errno := syscall.Syscall6(syscall.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDTBSDInfo, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if errno != 0 || n != bsdInfoSize {
		return 0, 0, 0, false
	}
	le := binary.LittleEndian
	sec, us := le.Uint64(buf[bsdInfoStartSec:]), le.Uint64(buf[bsdInfoStartUs:])
	// #nosec G115 -- a start time in µs since the epoch fits an int64 for 290,000 years
	return int(le.Uint32(buf[bsdInfoPPID:])), int(le.Uint32(buf[bsdInfoPGID:])), int64(sec*1_000_000 + us), true
}

// commandLine is a process's command line, from kern.procargs2; nil when it cannot be read.
func commandLine(pid int) []string {
	mib := []int32{ctlKern, kernProcArgs2, int32(pid)} // #nosec G115 -- a process number fits an int32
	size := uintptr(0)
	// #nosec G103 -- sysctl's size query: the MIB and the size's address, no buffer.
	if _, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), 0,
		uintptr(unsafe.Pointer(&size)), 0, 0); errno != 0 || size == 0 {
		return nil
	}
	buf := make([]byte, size)
	// #nosec G103 -- sysctl fills buf, bounded by size, which it sets to the length written.
	if _, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, 0); errno != 0 {
		return nil
	}
	return parseProcArgs(buf[:min(size, uintptr(len(buf)))])
}

// parseProcArgs reads kern.procargs2's layout: argc (a 32-bit integer), the executable's path, NUL
// padding, then argc NUL-terminated arguments (the environment follows; it is not read, and macOS
// leaves it out for a caller that is not root).
func parseProcArgs(b []byte) []string {
	if len(b) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(b))
	rest := b[4:]
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return nil
	}
	rest = rest[i:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	argv := make([]string, 0, min(argc, 64))
	for len(argv) < argc && len(rest) > 0 {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			j = len(rest)
		}
		argv = append(argv, string(rest[:j]))
		rest = rest[min(j+1, len(rest)):]
	}
	if len(argv) != argc {
		return nil
	}
	return argv
}
