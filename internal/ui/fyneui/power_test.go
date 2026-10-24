package fyneui

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Linux's rule over a power supply tree as sysfs lays it out: line power online wins, a battery of
// the computer's own discharging means battery, a wireless mouse's battery does not count.
func TestSysfsOnBattery(t *testing.T) {
	type supply map[string]string // file → content
	for _, c := range []struct {
		name     string
		supplies map[string]supply
		want     bool
	}{
		{"no supplies (a desktop, a virtual machine)", nil, false},
		{"a laptop on its adapter", map[string]supply{
			"AC":   {"type": "Mains\n", "online": "1\n"},
			"BAT0": {"type": "Battery\n", "status": "Charging\n", "scope": "System\n"}}, false},
		{"a laptop on its battery", map[string]supply{
			"AC":   {"type": "Mains\n", "online": "0\n"},
			"BAT0": {"type": "Battery\n", "status": "Discharging\n"}}, true},
		{"a battery without a status and no adapter listed", map[string]supply{
			"BAT0": {"type": "Battery\n"}}, true},
		{"a full battery and no adapter listed", map[string]supply{
			"BAT0": {"type": "Battery\n", "status": "Full\n"}}, false},
		{"a battery held at its threshold, no adapter listed", map[string]supply{
			"BAT1": {"type": "Battery\n", "status": "Not charging\n"}}, false},
		{"a desktop with a wireless mouse", map[string]supply{
			"hidpp_battery_0": {"type": "Battery\n", "scope": "Device\n", "status": "Discharging\n"}}, false},
		{"a USB-C charger, the adapter not listed", map[string]supply{
			"ucsi-source-psy-USBC000:001": {"type": "USB\n", "online": "1\n"},
			"BAT0":                        {"type": "Battery\n", "status": "Discharging\n"}}, false},
		{"a USB-C port without a charger", map[string]supply{
			"ucsi-source-psy-USBC000:001": {"type": "USB\n", "online": "0\n"},
			"BAT0":                        {"type": "Battery\n", "status": "Discharging\n"}}, true},
		{"two adapters, one online", map[string]supply{
			"ACAD": {"type": "Mains\n", "online": "0\n"},
			"ADP1": {"type": "Mains\n", "online": "1\n"},
			"BAT0": {"type": "Battery\n", "status": "Discharging\n"}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			// sysfs lists each supply as a symbolic link to its device's directory; Windows's test
			// runs may not make links, and there the directories stand in the tree itself.
			devices, link := t.TempDir(), runtime.GOOS != "windows"
			if !link {
				devices = root
			}
			for name, files := range c.supplies {
				dev := filepath.Join(devices, name)
				if err := os.MkdirAll(dev, 0o755); err != nil {
					t.Fatal(err)
				}
				for f, content := range files {
					if err := os.WriteFile(filepath.Join(dev, f), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if !link {
					continue
				}
				if err := os.Symlink(dev, filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
			if got := sysfsOnBattery(os.DirFS(root)); got != c.want {
				t.Errorf("on battery %v, want %v", got, c.want)
			}
		})
	}
	if sysfsOnBattery(os.DirFS(filepath.Join(t.TempDir(), "missing"))) {
		t.Error("a missing tree reads as battery")
	}
}

// Windows's rule: ACLineStatus 0 is the battery, 1 the adapter, 255 unknown.
func TestACLineOnBattery(t *testing.T) {
	for status, want := range map[byte]bool{0: true, 1: false, 255: false} {
		if got := acLineOnBattery(status); got != want {
			t.Errorf("ACLineStatus %d: %v, want %v", status, got, want)
		}
	}
}

// The poll calls its handler on a change only, never for the same state read again, and ends with
// its stop.
func TestPollPowerCallsOnAChangeOnly(t *testing.T) {
	var (
		mu    sync.Mutex
		reads = []bool{false, false, true, true, true, false} // then false for ever
		n     int
	)
	read := func() bool {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n <= len(reads) {
			return reads[n-1]
		}
		return false
	}
	reads0 := func() int { mu.Lock(); defer mu.Unlock(); return n }
	changes := make(chan struct{}, 10)
	stop := make(chan struct{})
	pollPower(stop, time.Millisecond, read, func() { changes <- struct{}{} })
	for i := range 2 {
		select {
		case <-changes:
		case <-time.After(5 * time.Second):
			t.Fatalf("change %d never came", i+1)
		}
	}
	for reads0() < len(reads)+5 { // long after the last change
		time.Sleep(time.Millisecond)
	}
	if len(changes) != 0 {
		t.Errorf("%d calls more than the two changes", len(changes))
	}
	close(stop)
	time.Sleep(10 * time.Millisecond)
	before := reads0()
	time.Sleep(20 * time.Millisecond)
	if after := reads0(); after != before {
		t.Errorf("the poll read %d times after its stop", after-before)
	}
}

// A panic in the read ends the watch, not the process (D-11).
func TestPollPowerSurvivesAPanic(t *testing.T) {
	var n atomic.Int32
	stop := make(chan struct{})
	defer close(stop)
	pollPower(stop, time.Millisecond, func() bool {
		if n.Add(1) == 2 {
			panic("the power source")
		}
		return false
	}, func() { t.Error("a change after a panic") })
	time.Sleep(30 * time.Millisecond)
	if got := n.Load(); got != 2 {
		t.Errorf("%d reads, want the watch ended at the panic's second", got)
	}
}

// Windows and Linux read the power source every half minute (powerPoll), and Linux reads it from
// powerSupplyRoot; a system without that folder (macOS, a container) reads "not on battery".
func TestPowerPollAndRoot(t *testing.T) {
	if powerPoll != 30*time.Second || powerSupplyRoot != "/sys/class/power_supply" {
		t.Errorf("powerPoll %v, powerSupplyRoot %q", powerPoll, powerSupplyRoot)
	}
	if _, err := os.Stat(powerSupplyRoot); err != nil && sysfsOnBattery(os.DirFS(powerSupplyRoot)) {
		t.Error("no power supplies listed, yet on battery")
	}
}
