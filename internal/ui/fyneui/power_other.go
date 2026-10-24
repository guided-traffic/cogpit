//go:build !darwin && !windows

package fyneui

import "os"

// systemOnBattery reports whether the computer runs on its battery, read from Linux's power
// supplies (sysfsOnBattery, power.go); a system without them is not on battery.
func systemOnBattery() bool { return sysfsOnBattery(os.DirFS(powerSupplyRoot)) }

// watchPower calls fn whenever the computer goes from its power adapter to its battery or back,
// read every powerPoll until stop closes (power.go).
func watchPower(stop <-chan struct{}, fn func()) { pollPower(stop, powerPoll, systemOnBattery, fn) }
