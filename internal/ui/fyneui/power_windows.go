//go:build windows

package fyneui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// procGetSystemPowerStatus is kernel32's GetSystemPowerStatus, loaded from System32 only.
var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

// systemPowerStatus is Windows's SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// systemOnBattery reports whether the computer runs on its battery: GetSystemPowerStatus's
// ACLineStatus offline (acLineOnBattery). A failed call, an unknown status and a desktop without a
// battery: not on battery.
func systemOnBattery() bool {
	var st systemPowerStatus
	// #nosec G103 -- the address of st, which the call fills, for the duration of the call.
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return false
	}
	return acLineOnBattery(st.ACLineStatus)
}

// watchPower calls fn whenever the computer goes from its power adapter to its battery or back,
// read every powerPoll until stop closes (power.go: GLFW keeps the window procedure that
// WM_POWERBROADCAST would reach).
func watchPower(stop <-chan struct{}, fn func()) { pollPower(stop, powerPoll, systemOnBattery, fn) }
