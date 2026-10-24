// The window's backing properties on macOS: the AppKit side of pixelscale_darwin.go (D-121).
// Windows are passed as the NSWindow pointer Fyne hands out (driver.MacWindowContext), as an
// integer.

#include <stdint.h>

// cogpitWatchBackingScale observes the window's NSWindowDidChangeBackingPropertiesNotification
// until it closes, and calls the Go function cogpitBackingScaleChanged for each, in the main
// queue's pass after it. Called again for the same window, it changes nothing.
void cogpitWatchBackingScale(uintptr_t window);
