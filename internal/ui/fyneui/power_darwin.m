// The computer's power source: the IOKit side of power_darwin.go. Compiled without ARC, like the
// rest of the package's Objective-C (Fyne's).

#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/ps/IOPSKeys.h>
#include <IOKit/ps/IOPowerSources.h>
#include <dispatch/dispatch.h>

#include "power_darwin.h"

extern void cogpitPowerChanged(void);

int cogpitOnBattery(void) {
	CFTypeRef info = IOPSCopyPowerSourcesInfo();
	if (info == NULL) {
		return 0;
	}
	// A constant string, not owned: compared before the snapshot goes.
	CFStringRef type = IOPSGetProvidingPowerSourceType(info);
	int battery = type != NULL && CFStringCompare(type, CFSTR(kIOPSBatteryPowerValue), 0) == kCFCompareEqualTo;
	CFRelease(info);
	return battery;
}

static void cogpitPowerSourcesChanged(void *context) {
	(void)context;
	cogpitPowerChanged();
}

void cogpitWatchPower(void) {
	// IOKit calls back on the run loop the source is in: the main one, in every mode, so that a
	// change during a drag or an open menu arrives too.
	dispatch_async(dispatch_get_main_queue(), ^{
		CFRunLoopSourceRef source = IOPSNotificationCreateRunLoopSource(cogpitPowerSourcesChanged, NULL);
		if (source == NULL) {
			return;
		}
		CFRunLoopAddSource(CFRunLoopGetMain(), source, kCFRunLoopCommonModes);
		CFRelease(source); // the run loop keeps it, for the process
	});
}
