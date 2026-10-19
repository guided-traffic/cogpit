package cluster

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// Cogpit's child processes on Linux, for ending a credential plugin's run (pluginproc.go): the
// children files of /proc/self/task (one per thread; a kernel without CONFIG_PROC_CHILDREN has
// none, and then every process's stat is read), each child's stat for its parent, process group and
// start time, and its cmdline for the command line.

// listChildren returns Cogpit's child processes, without their command lines.
func listChildren() []childProcess {
	self, group := os.Getpid(), syscall.Getpgrp()
	pids, ok := taskChildren()
	if !ok {
		pids = allProcesses()
	}
	var out []childProcess
	for _, pid := range pids {
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")) // #nosec G304 -- a /proc entry named by a process number
		if err != nil {
			continue
		}
		ppid, pgrp, start, ok := parseProcStat(stat)
		if !ok || ppid != self {
			continue
		}
		out = append(out, childProcess{id: procID{pid: pid, start: start}, group: pgrp == group})
	}
	return out
}

// taskChildren is the children of every thread of Cogpit's; ok is false where the kernel does not
// list them.
func taskChildren() ([]int, bool) {
	tasks, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return nil, false
	}
	var out []int
	listed := false
	for _, t := range tasks {
		data, err := os.ReadFile(filepath.Join("/proc/self/task", t.Name(), "children")) // #nosec G304 -- a /proc entry named by a thread number
		if err != nil {
			continue
		}
		listed = true
		for _, f := range strings.Fields(string(data)) {
			if pid, err := strconv.Atoi(f); err == nil && pid > 0 && !slices.Contains(out, pid) {
				out = append(out, pid)
			}
		}
	}
	return out, listed
}

// allProcesses is every process's number in /proc.
func allProcesses() []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && pid > 0 {
			out = append(out, pid)
		}
	}
	return out
}

// parseProcStat reads /proc/<pid>/stat ("pid (comm) state ppid pgrp … starttime …"; comm may hold
// spaces and parentheses, so the fields are read after its last ')'): the parent, the process group
// and the start time in clock ticks since boot (field 22).
func parseProcStat(stat []byte) (ppid, pgrp int, start int64, ok bool) {
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, 0, 0, false
	}
	fields := strings.Fields(string(stat[i+1:]))
	if len(fields) < 20 {
		return 0, 0, 0, false
	}
	ppid, err1 := strconv.Atoi(fields[1])
	pgrp, err2 := strconv.Atoi(fields[2])
	start, err3 := strconv.ParseInt(fields[19], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return ppid, pgrp, start, true
}

// commandLine is a process's command line, from /proc/<pid>/cmdline; nil when it cannot be read.
func commandLine(pid int) []string {
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline")) // #nosec G304 -- a /proc entry named by a process number
	if err != nil || len(cmdline) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(cmdline), "\x00"), "\x00")
}
