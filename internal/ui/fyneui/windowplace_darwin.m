// The window's place on macOS: the AppKit side of windowplace_darwin.go (D-95). Compiled without
// ARC, like the rest of the package's Objective-C (Fyne's).

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#include <string.h>

#include "windowplace_darwin.h"

extern void cogpitWindowPlaceChanged(uintptr_t id, cogpitPlaceState *state);

static cogpitPlaceRect placeRectOf(NSRect r) {
	cogpitPlaceRect p = {r.origin.x, r.origin.y, r.size.width, r.size.height};
	return p;
}

static void describeScreen(NSScreen *s, cogpitPlaceScreen *out) {
	memset(out, 0, sizeof(*out));
	if (s == nil) {
		return;
	}
	NSNumber *n = s.deviceDescription[@"NSScreenNumber"];
	out->id = n.unsignedIntValue;
	NSString *name = s.localizedName;
	if (name != nil) {
		strlcpy(out->name, name.UTF8String, sizeof(out->name));
	}
	out->frame = placeRectOf(s.frame);
	out->visible = placeRectOf(s.visibleFrame);
}

int cogpitWindowPlaceScreens(cogpitPlaceScreen *screens, int max) {
	NSArray<NSScreen *> *all = [NSScreen screens];
	int n = 0;
	for (NSScreen *s in all) {
		if (n < max) {
			describeScreen(s, &screens[n]);
		}
		n++;
	}
	return n;
}

double cogpitWindowPlaceTitleBarHeight(void) {
	NSWindowStyleMask style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
	                          NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable;
	NSRect content = NSMakeRect(0, 0, 400, 300);
	return NSHeight([NSWindow frameRectForContentRect:content styleMask:style]) - NSHeight(content);
}

static BOOL inFullScreenStyle(NSWindow *w) { return (w.styleMask & NSWindowStyleMaskFullScreen) != 0; }

// CogpitPlaceObserver reports a window's place to Go on every move, resize, change of display and
// full-screen change. It keeps the frame outside full screen: entering full screen AppKit resizes
// the window between its will-enter and did-enter notifications, and leaving it back to the frame
// before, so the frames between a will and a did are not the window's place.
//
// A transition AppKit gives up on ends without a did notification (only the window's delegate,
// GLFW's, hears of the failure): measured on macOS 26, a window of an app that is not active takes
// the full-screen style and the screen's frame at toggleFullScreen:, then gets its frame back and
// loses the style. So every half second of a transition the window's style is looked at: entering
// without the style is a failed transition, and after 5 seconds, longer than any animation, the
// style says where the window is.
@interface CogpitPlaceObserver : NSObject
@property(nonatomic) uintptr_t placeID;
@property(nonatomic, assign) NSWindow *window; // not retained: the window holds the observer; nil once it closes
@property(nonatomic) NSRect normalFrame;
@property(nonatomic) BOOL fullScreen;
@property(nonatomic) BOOL transition;
@property(nonatomic) BOOL entering;
@property(nonatomic) NSUInteger generation; // of the transition the checks belong to
@property(nonatomic) BOOL wantFullScreen;   // full screen once the app is active (cogpitWindowPlaceFollow)
@end

@implementation CogpitPlaceObserver
- (void)state:(cogpitPlaceState *)st {
	memset(st, 0, sizeof(*st));
	st->frame = placeRectOf(self.normalFrame);
	st->fullScreen = self.fullScreen || self.wantFullScreen; // full screen waiting for the app's activation stays kept
	describeScreen(self.window.screen, &st->display);
}
- (void)report {
	if (self.window == nil) {
		return;
	}
	cogpitPlaceState st;
	[self state:&st];
	cogpitWindowPlaceChanged(self.placeID, &st);
}
- (void)framed:(NSNotification *)note {
	NSWindow *w = self.window;
	if (w == nil || self.transition || self.fullScreen || inFullScreenStyle(w) || w.miniaturized) {
		return;
	}
	self.normalFrame = w.frame;
	[self report];
}
// settle ends a transition without its did notification: where the window's style says it is.
- (void)settle {
	self.transition = NO;
	self.fullScreen = inFullScreenStyle(self.window);
	if (!self.fullScreen) {
		self.normalFrame = self.window.frame;
	}
	[self report];
}
- (void)check:(NSUInteger)generation tick:(int)tick {
	// The block retains the observer, whose window is nil by then if it closed.
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC / 2), dispatch_get_main_queue(), ^{
		if (!self.transition || self.window == nil || self.generation != generation) {
			return;
		}
		if ((self.entering && !inFullScreenStyle(self.window)) || tick >= 10) {
			[self settle];
			return;
		}
		[self check:generation tick:tick + 1];
	});
}
- (void)willChangeFullScreen:(BOOL)entering {
	self.transition = YES;
	self.entering = entering;
	self.wantFullScreen = NO;
	self.generation++;
	[self check:self.generation tick:1];
}
- (void)willEnterFullScreen:(NSNotification *)note {
	[self willChangeFullScreen:YES];
}
- (void)willExitFullScreen:(NSNotification *)note {
	[self willChangeFullScreen:NO];
}
- (void)didEnterFullScreen:(NSNotification *)note {
	self.transition = NO;
	self.fullScreen = YES;
	[self report];
}
- (void)didExitFullScreen:(NSNotification *)note {
	self.transition = NO;
	self.fullScreen = NO;
	if (self.window != nil) {
		self.normalFrame = self.window.frame;
	}
	[self report];
}
// enterFullScreen takes the window into full screen now if the app is active, else once it is:
// AppKit gives up on full screen for an app that is not (measured on macOS 26, see above).
- (void)enterFullScreen {
	self.wantFullScreen = YES;
	if (NSApp.isActive) {
		[self becameActive:nil];
		return;
	}
	[[NSNotificationCenter defaultCenter] addObserver:self
	                                         selector:@selector(becameActive:)
	                                             name:NSApplicationDidBecomeActiveNotification
	                                           object:nil];
}
- (void)becameActive:(NSNotification *)note {
	[[NSNotificationCenter defaultCenter] removeObserver:self name:NSApplicationDidBecomeActiveNotification object:nil];
	NSWindow *w = self.window;
	if (!self.wantFullScreen || w == nil || self.transition || inFullScreenStyle(w)) {
		return;
	}
	self.wantFullScreen = NO;
	[w toggleFullScreen:nil];
}
- (void)closing:(NSNotification *)note {
	[[NSNotificationCenter defaultCenter] removeObserver:self];
	self.window = nil;
}
@end

// The observer lives as long as its window: the window holds it as an associated object.
static char placeObserverKey;

static NSWindow *placeWindowOf(uintptr_t window) { return (NSWindow *)(void *)window; }

static CogpitPlaceObserver *installObserver(NSWindow *w) {
	CogpitPlaceObserver *o = objc_getAssociatedObject(w, &placeObserverKey);
	if (o != nil) {
		return o;
	}
	o = [[CogpitPlaceObserver alloc] init];
	o.window = w;
	o.normalFrame = w.frame;
	o.fullScreen = inFullScreenStyle(w);
	NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
	for (NSNotificationName name in @[
		     NSWindowDidMoveNotification, NSWindowDidResizeNotification, NSWindowDidChangeScreenNotification
	     ]) {
		[nc addObserver:o selector:@selector(framed:) name:name object:w];
	}
	[nc addObserver:o selector:@selector(willEnterFullScreen:) name:NSWindowWillEnterFullScreenNotification object:w];
	[nc addObserver:o selector:@selector(willExitFullScreen:) name:NSWindowWillExitFullScreenNotification object:w];
	[nc addObserver:o selector:@selector(didEnterFullScreen:) name:NSWindowDidEnterFullScreenNotification object:w];
	[nc addObserver:o selector:@selector(didExitFullScreen:) name:NSWindowDidExitFullScreenNotification object:w];
	[nc addObserver:o selector:@selector(closing:) name:NSWindowWillCloseNotification object:w];
	objc_setAssociatedObject(w, &placeObserverKey, o, OBJC_ASSOCIATION_RETAIN);
	[o release];
	return o;
}

void cogpitWindowPlaceFollow(uintptr_t window, uintptr_t id, cogpitPlaceRect frame, int fullScreen) {
	NSWindow *w = placeWindowOf(window);
	BOOL full = inFullScreenStyle(w);
	if (frame.width > 0 && frame.height > 0 && !full) {
		// Fyne made the window from the same frame (its size and GLFW's position); this sets the
		// frame exactly where a rounding or Fyne's scale left it a point off, and is no change
		// otherwise.
		NSRect want = NSMakeRect(frame.x, frame.y, frame.width, frame.height);
		if (!NSEqualRects(w.frame, want)) {
			[w setFrame:want display:YES];
		}
	}
	CogpitPlaceObserver *o = installObserver(w);
	o.placeID = id;
	if (!full) {
		o.normalFrame = w.frame;
	}
	if (fullScreen && !full) {
		[o enterFullScreen];
	}
}

void cogpitWindowPlaceState(uintptr_t window, cogpitPlaceState *state) {
	NSWindow *w = placeWindowOf(window);
	CogpitPlaceObserver *o = objc_getAssociatedObject(w, &placeObserverKey);
	if (o == nil) {
		memset(state, 0, sizeof(*state));
		state->frame = placeRectOf(w.frame);
		state->fullScreen = inFullScreenStyle(w);
		describeScreen(w.screen, &state->display);
		return;
	}
	[o state:state];
}
