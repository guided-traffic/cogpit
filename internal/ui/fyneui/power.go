package fyneui

import (
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"runtime/debug"
	"strings"
	"time"
)

// The computer's power source, for Preferences › App › Light strip on battery
// (preferences_power.go): systemOnBattery reads it, watchPower follows its changes; each system's
// file has the two (power_darwin.go, power_windows.go, power_other.go). The parts here are the
// systems' shared and pure ones, so that every system's rule is tested on every system.

// powerPoll is how often Windows and Linux read the power source to follow a change: Fyne's window
// offers neither a change notification (Windows's WM_POWERBROADCAST goes to a window procedure
// GLFW keeps; Linux's is UPower over D-Bus, a dependency for one bit), and a read costs
// microseconds. Half a minute is the lead's choice (2026-10-24): the strip stops or starts its
// animation at most that long after the cable moved, and the poll wakes the process twice a minute,
// nothing beside the strip's frames (stripFPS a second) it saves.
const powerPoll = 30 * time.Second

// pollPower reads the power source every period and calls fn whenever it changed, until stop
// closes. A panic in read or fn ends the watch with a log line instead of the process (D-11); the
// strip then follows the power source with its next change of state instead of at once.
func pollPower(stop <-chan struct{}, every time.Duration, read func() bool, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("the power source watch panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			}
		}()
		last := read()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if now := read(); now != last {
					last = now
					fn()
				}
			}
		}
	}()
}

// acLineOnBattery is Windows's rule: SYSTEM_POWER_STATUS's ACLineStatus 0 is offline, the computer
// on its battery; 1 is online and 255 unknown (a desktop without a battery reports 1).
func acLineOnBattery(status byte) bool { return status == 0 }

// powerSupplyRoot is where Linux lists the computer's power supplies, one directory each.
const powerSupplyRoot = "/sys/class/power_supply"

// sysfsOnBattery is Linux's rule, read from the power supplies in fsys (os.DirFS of
// powerSupplyRoot, whose entries are symbolic links to their devices' directories, followed): the
// computer runs on battery when no line power is online and one of its own batteries discharges.
// Line power is a supply of type Mains, or a USB charger (USB, USB_C, USB_PD and the other USB
// types), whose online reads 1. A battery of the computer's own is of type Battery without the
// scope Device, which a wireless mouse's or headset's battery has: a desktop with one is not on
// battery. A battery whose status cannot be read counts as discharging; Charging, Full, Not
// charging and Unknown count as on the power adapter. No supply at all (a desktop, a virtual
// machine, a system without sysfs): not on battery.
func sysfsOnBattery(fsys fs.FS) bool {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return false
	}
	discharging := false
	for _, e := range entries {
		read := func(name string) string {
			b, err := fs.ReadFile(fsys, path.Join(e.Name(), name))
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(b))
		}
		switch typ := read("type"); {
		case typ == "Mains" || strings.HasPrefix(typ, "USB"):
			if read("online") == "1" {
				return false
			}
		case typ == "Battery" && read("scope") != "Device":
			if st := read("status"); st == "" || st == "Discharging" {
				discharging = true
			}
		}
	}
	return discharging
}
