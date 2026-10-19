//go:build !darwin && !linux && !windows

package cluster

import "errors"

// listChildren finds no process where Cogpit cannot list them: a credential plugin's run that
// Cogpit stops waiting for there runs on, and its login is not used (pluginproc.go).
func listChildren() []childProcess { return nil }

// commandLine is not known here.
func commandLine(int) []string { return nil }

// canSignal: no process can be ended here.
func canSignal(int) bool { return false }

// signalPlugin cannot end a process here.
func signalPlugin(int, bool) error { return errors.New("not supported on this system") }
