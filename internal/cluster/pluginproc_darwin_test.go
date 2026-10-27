package cluster

import (
	"encoding/binary"
	"os"
	"slices"
	"syscall"
	"testing"
	"time"
)

// procArgs is kern.procargs2's layout: argc, the executable's path, its NUL and padding, then the
// strings that follow it, each NUL-terminated (the arguments, then the environment).
func procArgs(argc uint32, exe string, after ...string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, argc)
	b = append(append(b, exe...), 0, 0, 0)
	for _, s := range after {
		b = append(append(b, s...), 0)
	}
	return b
}

func TestParseProcArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want []string
	}{
		{"arguments", procArgs(3, "/usr/local/bin/kubelogin", "kubelogin", "get-token", ""), []string{"kubelogin", "get-token", ""}},
		{"the environment is not read", procArgs(1, "/bin/sleep", "sleep", "HOME=/Users/jane"), []string{"sleep"}},
		{"no arguments", procArgs(0, "/bin/sleep"), []string{}},
		{"fewer arguments than argc", procArgs(3, "/bin/sleep", "sleep"), nil},
		{"no NUL after the path", append(binary.LittleEndian.AppendUint32(nil, 1), "/bin/sleep"...), nil},
		{"shorter than argc's integer", []byte{1, 0}, nil},
	} {
		if got := parseProcArgs(tc.in); !slices.Equal(got, tc.want) || (got == nil) != (tc.want == nil) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The kernel's own answers for this process: its command line and struct proc_bsdinfo's fields at
// the offsets read, on the macOS the test runs on. Off by a field, a plugin's process would not be
// found and Cogpit could not end it.
func TestDarwinProcessInfoOfThisProcess(t *testing.T) {
	if got := commandLine(os.Getpid()); !slices.Equal(got, os.Args) {
		t.Errorf("command line %q, want %q", got, os.Args)
	}
	ppid, pgid, start, ok := procBSDInfo(os.Getpid())
	if !ok || ppid != os.Getppid() || pgid != syscall.Getpgrp() {
		t.Fatalf("proc_bsdinfo: parent %d, group %d, read %v; want %d and %d", ppid, pgid, ok, os.Getppid(), syscall.Getpgrp())
	}
	if began := time.UnixMicro(start); began.After(time.Now()) || time.Since(began) > 24*time.Hour {
		t.Errorf("started at %v", began)
	}
}
