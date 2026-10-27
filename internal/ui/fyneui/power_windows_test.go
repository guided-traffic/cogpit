//go:build windows

package fyneui

import (
	"testing"
	"unsafe"
)

// GetSystemPowerStatus writes SYSTEM_POWER_STATUS's 12 bytes into the Go struct: one of another
// size would let it write past it. The call itself must answer on any machine; what it says is
// logged (a runner's virtual machine has no battery).
func TestSystemPowerStatus(t *testing.T) {
	if n := unsafe.Sizeof(systemPowerStatus{}); n != 12 {
		t.Fatalf("systemPowerStatus is %d bytes, SYSTEM_POWER_STATUS 12", n)
	}
	t.Logf("on battery: %v", systemOnBattery())
}
