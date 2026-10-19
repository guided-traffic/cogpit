//go:build darwin || linux

package cluster

import "syscall"

// signalPlugin ends a credential plugin's process: SIGTERM, or SIGKILL when kill is set.
func signalPlugin(pid int, kill bool) error {
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	return syscall.Kill(pid, sig)
}

// canSignal reports whether Cogpit may signal process pid (a plugin that runs as another user, a
// setuid program, may not be).
func canSignal(pid int) bool { return syscall.Kill(pid, 0) == nil }
