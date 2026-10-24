// The window's backing properties on macOS: the AppKit side of pixelscale_darwin.go (D-121).
// Compiled without ARC, like the rest of the package's Objective-C (Fyne's).

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

#include "pixelscale_darwin.h"

extern void cogpitBackingScaleChanged(void);

// CogpitBackingObserver hands AppKit's report that the window's backing properties changed (its
// scale factor or its colour space) to Go, which compares the density Fyne draws at. It hands it
// on in the main queue's next pass: GLFW's view tells Fyne the new framebuffer size from the same
// change (viewDidChangeBackingProperties), whatever the order AppKit tells the two in.
@interface CogpitBackingObserver : NSObject
@end

@implementation CogpitBackingObserver
- (void)changed:(NSNotification *)note {
	(void)note;
	dispatch_async(dispatch_get_main_queue(), ^{
		cogpitBackingScaleChanged();
	});
}
- (void)closing:(NSNotification *)note {
	(void)note;
	[[NSNotificationCenter defaultCenter] removeObserver:self];
}
@end

// The observer lives as long as its window: the window holds it as an associated object.
static char backingObserverKey;

void cogpitWatchBackingScale(uintptr_t window) {
	NSWindow *w = (NSWindow *)(void *)window;
	if (objc_getAssociatedObject(w, &backingObserverKey) != nil) {
		return;
	}
	CogpitBackingObserver *o = [[CogpitBackingObserver alloc] init];
	NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
	[nc addObserver:o selector:@selector(changed:) name:NSWindowDidChangeBackingPropertiesNotification object:w];
	[nc addObserver:o selector:@selector(closing:) name:NSWindowWillCloseNotification object:w];
	objc_setAssociatedObject(w, &backingObserverKey, o, OBJC_ASSOCIATION_RETAIN);
	[o release];
}
