// A native check of titlebar_darwin.m, built and run by TestTitlebarButtonsCentred (a window needs
// the main thread of its own process): it creates a window as GLFW does, installs Cogpit's title
// bar, centres the buttons in a 40-point bar, lets AppKit lay the title bar out again (a resize, a
// style change) and prints each step's button centres from the window's top and their x, one line
// per step: "<step> <close centre> <close x> <zoom centre> <zoom x>", the content view's layer
// contents placement against AppKit's top left ("placement <got> <top left>") and the window's
// colour space by name ("colorspace <name>"). Exit status 3: no window buttons (no window server).

#import <Cocoa/Cocoa.h>
#include "titlebar_darwin.h"

void cogpitTitlebarChanged(uintptr_t id) {}

static void report(NSWindow *w, const char *step) {
	NSButton *close = [w standardWindowButton:NSWindowCloseButton];
	NSButton *zoom = [w standardWindowButton:NSWindowZoomButton];
	NSRect c = [close convertRect:close.bounds toView:nil];
	NSRect z = [zoom convertRect:zoom.bounds toView:nil];
	CGFloat h = NSHeight(w.frame);
	printf("%s %.1f %.1f %.1f %.1f\n", step, h - NSMidY(c), NSMinX(c), h - NSMidY(z), NSMinX(z));
}

// buttonsY is the three buttons' y in their title bar view.
static NSString *buttonsY(NSWindow *w) {
	NSMutableString *s = [NSMutableString string];
	for (NSNumber *k in @[ @(NSWindowCloseButton), @(NSWindowMiniaturizeButton), @(NSWindowZoomButton) ]) {
		[s appendFormat:@"%.1f ", [w standardWindowButton:k.intValue].frame.origin.y];
	}
	return s;
}

// settle runs the main run loop until the buttons stayed put for five slices in a row, at most two
// seconds: AppKit's layout passes and the title bar's re-placement after them each take a turn of
// the loop, and a slow machine (CI) needs more than one fixed slice (red on CI, 2026-10-13).
static void settle(NSWindow *w) {
	NSString *last = nil;
	int still = 0;
	for (int i = 0; i < 100 && still < 5; i++) {
		[[NSRunLoop mainRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:0.02]];
		NSString *now = buttonsY(w);
		still = [now isEqualToString:last] ? still + 1 : 0;
		last = now;
	}
}

int main(void) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		NSWindow *w = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 800, 600)
		                                          styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
		                                                    NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
		                                            backing:NSBackingStoreBuffered
		                                              defer:NO];
		w.releasedWhenClosed = NO;
		if ([w standardWindowButton:NSWindowCloseButton] == nil || [w standardWindowButton:NSWindowZoomButton] == nil) {
			return 3;
		}
		report(w, "appkit");
		cogpitWindowKeepContentPlaced((uintptr_t)w);
		printf("placement %ld %ld\n", (long)w.contentView.layerContentsPlacement, (long)NSViewLayerContentsPlacementTopLeft);
		cogpitWindowUseSRGB((uintptr_t)w);
		CFStringRef space = CGColorSpaceCopyName(w.colorSpace.CGColorSpace);
		printf("colorspace %s\n", space != NULL ? [(NSString *)space UTF8String] : "none");
		if (space != NULL) {
			CFRelease(space);
		}
		cogpitTitlebarInstall((uintptr_t)w, 1);
		cogpitTitlebarCentreButtons((uintptr_t)w, 40);
		settle(w);
		report(w, "centred");
		[w setFrame:NSMakeRect(0, 0, 900, 700) display:NO];
		[w layoutIfNeeded];
		settle(w);
		report(w, "resized");
		w.styleMask &= ~NSWindowStyleMaskResizable;
		w.styleMask |= NSWindowStyleMaskResizable;
		[w layoutIfNeeded];
		settle(w);
		report(w, "restyled");
		[w close];
		[[NSRunLoop mainRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:0.05]];
		[w release];
	}
	return 0;
}
