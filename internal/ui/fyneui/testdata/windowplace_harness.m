// A native check of windowplace_darwin.m, built and run by TestWindowPlaceFollowsTheWindow (a
// window needs the main thread of its own process): it describes the displays, creates a window
// as GLFW does, gives it a frame through cogpitWindowPlaceFollow, moves and resizes it, takes it
// into and out of full screen (AppKit's notifications posted around the resizes AppKit would make,
// as a harness cannot run the animation) and closes it, and prints one line per step:
// "<step> <reports> <x> <y> <width> <height> <fullScreen> <display id>", the reports counted since
// the step before and the last one's place; first "screens <n> <x> <y> <width> <height> <id>" for
// the primary display and "titlebar <height> <frame less content>". Exit status 3: no display (no
// window server).

#import <Cocoa/Cocoa.h>
#include "windowplace_darwin.h"

static int reports;
static cogpitPlaceState last;

void cogpitWindowPlaceChanged(uintptr_t id, cogpitPlaceState *state) {
	if (id != 7) {
		printf("wrong-id %lu\n", (unsigned long)id);
	}
	reports++;
	last = *state;
}

static void step(const char *name) {
	printf("%s %d %.0f %.0f %.0f %.0f %d %u\n", name, reports, last.frame.x, last.frame.y, last.frame.width,
	       last.frame.height, last.fullScreen, last.display.id);
	reports = 0;
}

int main(void) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		cogpitPlaceScreen screens[16];
		int n = cogpitWindowPlaceScreens(screens, 16);
		if (n == 0) {
			return 3;
		}
		printf("screens %d %.0f %.0f %.0f %.0f %u\n", n, screens[0].frame.x, screens[0].frame.y, screens[0].frame.width,
		       screens[0].frame.height, screens[0].id);

		NSWindowStyleMask style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
		                          NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable;
		NSWindow *w = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 800, 600)
		                                          styleMask:style
		                                            backing:NSBackingStoreBuffered
		                                              defer:NO];
		w.releasedWhenClosed = NO;
		printf("titlebar %.0f %.0f\n", cogpitWindowPlaceTitleBarHeight(), NSHeight(w.frame) - NSHeight(w.contentView.frame));

		cogpitPlaceRect want = {120, 140, 1000, 640};
		cogpitWindowPlaceFollow((uintptr_t)w, 7, want, 0);
		printf("followed %.0f %.0f %.0f %.0f\n", NSMinX(w.frame), NSMinY(w.frame), NSWidth(w.frame), NSHeight(w.frame));
		step("installed"); // setting the frame came before the observer: no report

		[w setFrameOrigin:NSMakePoint(200, 150)];
		step("moved");
		[w setFrame:NSMakeRect(200, 150, 1100, 700) display:NO];
		step("resized");

		NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
		[nc postNotificationName:NSWindowWillEnterFullScreenNotification object:w];
		[w setFrame:NSMakeRect(screens[0].frame.x, screens[0].frame.y, screens[0].frame.width, screens[0].frame.height) display:NO];
		[nc postNotificationName:NSWindowDidEnterFullScreenNotification object:w];
		step("entered"); // the frame before full screen, in full screen
		[w setFrameOrigin:NSMakePoint(5, 5)]; // AppKit's own moves in full screen are not the place
		step("inside");
		[nc postNotificationName:NSWindowWillExitFullScreenNotification object:w];
		[w setFrame:NSMakeRect(210, 160, 1100, 700) display:NO];
		[nc postNotificationName:NSWindowDidExitFullScreenNotification object:w];
		step("exited");

		cogpitPlaceRect none = {0, 0, 0, 0};
		cogpitWindowPlaceFollow((uintptr_t)w, 7, none, 0); // again: one observer, the frame left
		[w setFrameOrigin:NSMakePoint(230, 170)];
		step("again");

		cogpitPlaceState st;
		cogpitWindowPlaceState((uintptr_t)w, &st);
		printf("state %.0f %.0f %.0f %.0f %d\n", st.frame.x, st.frame.y, st.frame.width, st.frame.height, st.fullScreen);

		[w close];
		[w setFrameOrigin:NSMakePoint(300, 300)];
		step("closed");
		[w release];

		// Full screen kept: entered at once by an active app, else once it is (a harness started by
		// a test is not); the place keeps it meanwhile.
		NSWindow *w2 = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 800, 600)
		                                           styleMask:style
		                                             backing:NSBackingStoreBuffered
		                                               defer:NO];
		w2.releasedWhenClosed = NO;
		cogpitPlaceRect at = {150, 160, 900, 650};
		cogpitWindowPlaceFollow((uintptr_t)w2, 7, at, 1);
		cogpitWindowPlaceState((uintptr_t)w2, &st);
		printf("fullscreen %d %d %d\n", st.fullScreen, (w2.styleMask & NSWindowStyleMaskFullScreen) != 0, NSApp.isActive);
		[w2 close];
		[w2 release];
	}
	return 0;
}
