// The computer's power source: the IOKit side of power_darwin.go.

// cogpitOnBattery returns 1 while the computer draws its power from its battery (IOKit's providing
// power source is kIOPSBatteryPowerValue), else 0: on the power adapter, on a UPS, and on a Mac
// without a battery.
int cogpitOnBattery(void);

// cogpitWatchPower adds IOKit's power source notification (IOPSNotificationCreateRunLoopSource) to
// the main run loop and calls the Go side's cogpitPowerChanged for each. Call it once, with the app
// running.
void cogpitWatchPower(void);
