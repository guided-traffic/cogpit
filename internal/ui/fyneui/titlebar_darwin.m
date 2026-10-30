// Cogpit's title bar on macOS: the AppKit side of titlebar_darwin.go. Compiled without ARC, like
// the rest of the package's Objective-C (Fyne's).

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#include <string.h>

#include "titlebar_darwin.h"

extern void cogpitTitlebarChanged(uintptr_t id);

static void placeButtons(NSWindow *w, CGFloat barHeight);

// CogpitTitlebarObserver forwards a window's layout notifications to Go, and keeps the window
// buttons centred in Cogpit's top bar: AppKit puts them back where its own title bar has them on
// its layout passes (a resize, a style or key change, full screen left), button by button, and the
// observer, told of the buttons' frame changes, moves them again at once and once more after the
// pass (measured on macOS 26: at once alone leaves the zoom button where AppKit put it, after the
// pass alone shows AppKit's place for a frame).
@interface CogpitTitlebarObserver : NSObject
@property(nonatomic) uintptr_t titlebarID;
@property(nonatomic, assign) NSWindow *window; // not retained: the window holds the observer; nil once it closes
@property(nonatomic) CGFloat barHeight;        // the top bar's height in points; 0: AppKit's place
@property(nonatomic) BOOL placing;
@property(nonatomic) BOOL queued;
@end

@implementation CogpitTitlebarObserver
- (void)place {
	if (self.placing || self.barHeight <= 0 || self.window == nil) {
		return;
	}
	self.placing = YES;
	placeButtons(self.window, self.barHeight);
	self.placing = NO;
}
- (void)placeNowAndAfterThePass {
	[self place];
	if (self.queued || self.window == nil) {
		return;
	}
	self.queued = YES;
	// The block retains the observer, whose window is nil by then if the window closed meanwhile.
	dispatch_async(dispatch_get_main_queue(), ^{
		self.queued = NO;
		[self place];
	});
}
- (void)changed:(NSNotification *)note {
	[self placeNowAndAfterThePass];
	cogpitTitlebarChanged(self.titlebarID);
}
- (void)buttonMoved:(NSNotification *)note {
	if (!self.placing) {
		[self placeNowAndAfterThePass];
	}
}
- (void)closing:(NSNotification *)note {
	[[NSNotificationCenter defaultCenter] removeObserver:self];
	self.window = nil;
}
@end

// The observer lives as long as its window: the window holds it as an associated object.
static char observerKey;

static NSWindow *windowOf(uintptr_t window) { return (NSWindow *)(void *)window; }

static NSArray<NSButton *> *windowButtons(NSWindow *w) {
	NSButton *close = [w standardWindowButton:NSWindowCloseButton];
	NSButton *mini = [w standardWindowButton:NSWindowMiniaturizeButton];
	NSButton *zoom = [w standardWindowButton:NSWindowZoomButton];
	if (close == nil || mini == nil || zoom == nil) {
		return nil;
	}
	return @[ close, mini, zoom ];
}

// placeButtons centres the window buttons vertically in a top bar barHeight points high, as apps
// with a taller title bar have them (VS Code, apps with a toolbar), instead of in the upper part
// AppKit's own title bar takes (owner, 2026-10-13: "too high"; measured on macOS 26: a 32-point
// title bar, 14-point buttons centred 16 points from the top, 20 in Cogpit's 40-point bar). Their x
// stays AppKit's. They stay inside AppKit's title bar view, which AppKit sizes: in a taller bar
// than it has room for they sit at its bottom edge. Nothing happens in full screen, where AppKit
// shows the buttons in its own bar.
static void placeButtons(NSWindow *w, CGFloat barHeight) {
	if ((w.styleMask & NSWindowStyleMaskFullScreen) != 0) {
		return;
	}
	NSArray<NSButton *> *buttons = windowButtons(w);
	NSView *titlebar = buttons.firstObject.superview; // AppKit's title bar view
	if (buttons == nil || titlebar == nil) {
		return;
	}
	CGFloat buttonHeight = NSHeight(buttons.firstObject.frame);
	CGFloat room = NSHeight(titlebar.bounds) - buttonHeight; // the most the buttons can move down
	CGFloat fromTop = MAX(MIN(round((barHeight - buttonHeight) / 2), room), 0);
	CGFloat y = titlebar.isFlipped ? fromTop : room - fromTop;
	for (NSButton *b in buttons) {
		if (NSMinY(b.frame) != y) {
			[b setFrameOrigin:NSMakePoint(NSMinX(b.frame), y)];
		}
	}
}

void cogpitWindowKeepContentPlaced(uintptr_t window) {
	// While a resize is dragged AppKit sizes the content view's layer at once and shows its last
	// frame scaled to the new size until the next one is drawn (the layer's default contents
	// gravity); the text looked as if it scaled with the window, then jumped back (owner,
	// 2026-10-13). Anchored at the top left, the last frame stays as it was drawn, and only the
	// strip the window grew by waits for the next frame.
	NSView *v = windowOf(window).contentView;
	v.layerContentsPlacement = NSViewLayerContentsPlacementTopLeft;
}

void cogpitWindowUseSRGB(uintptr_t window) {
	// The theme's colours are sRGB. Without a colour space of its own the window takes its screen's,
	// and AppKit showed GLFW's OpenGL frames now converted from sRGB, now as the display's own values
	// (bluer, more saturated), alternating from frame to frame on either display (measured on macOS
	// 27: the two looks were exactly the theme's values and their conversion to the display's
	// profile). Not the device colour space: entering full screen hangs with it (kitty's GLFW).
	windowOf(window).colorSpace = [NSColorSpace sRGBColorSpace];
}

void cogpitTitlebarInstall(uintptr_t window, uintptr_t id) {
	NSWindow *w = windowOf(window);
	w.styleMask |= NSWindowStyleMaskFullSizeContentView;
	w.titlebarAppearsTransparent = YES;
	w.titleVisibility = NSWindowTitleHidden;

	CogpitTitlebarObserver *o = [[CogpitTitlebarObserver alloc] init];
	o.titlebarID = id;
	o.window = w;
	NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
	NSArray<NSNotificationName> *names = @[
		NSWindowDidEnterFullScreenNotification, NSWindowDidExitFullScreenNotification,
		NSWindowDidResizeNotification, NSWindowDidEndLiveResizeNotification,
		NSWindowDidChangeBackingPropertiesNotification, NSWindowDidChangeScreenNotification,
		NSWindowDidBecomeKeyNotification, NSWindowDidResignKeyNotification,
	];
	for (NSNotificationName name in names) {
		[nc addObserver:o selector:@selector(changed:) name:name object:w];
	}
	[nc addObserver:o selector:@selector(closing:) name:NSWindowWillCloseNotification object:w];
	NSMutableArray<NSView *> *watched = [NSMutableArray arrayWithArray:windowButtons(w) ?: @[]];
	if (watched.firstObject.superview != nil) {
		[watched addObject:watched.firstObject.superview];
	}
	for (NSView *v in watched) {
		v.postsFrameChangedNotifications = YES; // a button moved back, or the title bar view resized
		[nc addObserver:o selector:@selector(buttonMoved:) name:NSViewFrameDidChangeNotification object:v];
	}
	objc_setAssociatedObject(w, &observerKey, o, OBJC_ASSOCIATION_RETAIN);
	[o release];

	// The content view now covers the title bar's area too, without a resize event: announce one,
	// so that GLFW's window delegate (which observes the window's notifications) reads the larger
	// content size and Fyne lays the canvas out to it, and the observer above reads the geometry.
	[nc postNotificationName:NSWindowDidResizeNotification object:w];
}

void cogpitTitlebarCentreButtons(uintptr_t window, double barHeight) {
	NSWindow *w = windowOf(window);
	CogpitTitlebarObserver *o = objc_getAssociatedObject(w, &observerKey);
	if (o == nil) {
		return;
	}
	o.barHeight = barHeight;
	[o place];
}

void cogpitTitlebarGeometry(uintptr_t window, cogpitTitlebarGeom *geom) {
	NSWindow *w = windowOf(window);
	memset(geom, 0, sizeof(*geom));
	geom->fullScreen = (w.styleMask & NSWindowStyleMaskFullScreen) != 0;
	NSButton *close = [w standardWindowButton:NSWindowCloseButton];
	NSButton *zoom = [w standardWindowButton:NSWindowZoomButton];
	if (close == nil || zoom == nil) {
		return;
	}
	// The buttons' frames in the window's coordinates, whose origin is at the bottom left; x is the
	// same from the top left.
	NSRect c = [close convertRect:close.bounds toView:nil];
	NSRect z = [zoom convertRect:zoom.bounds toView:nil];
	geom->buttonsMinX = NSMinX(c);
	geom->buttonsMaxX = NSMaxX(z);
	geom->height = NSHeight(w.frame) - NSHeight(w.contentLayoutRect);
	geom->ok = 1;
}

static NSEvent *leftMouseDownIn(NSWindow *w) {
	NSEvent *e = [NSApp currentEvent];
	if (e == nil || e.type != NSEventTypeLeftMouseDown || e.window != w) {
		return nil;
	}
	return e;
}

int cogpitTitlebarClickCount(uintptr_t window) {
	NSEvent *e = leftMouseDownIn(windowOf(window));
	return e == nil ? 0 : (int)e.clickCount;
}

void cogpitTitlebarDrag(uintptr_t window) {
	NSWindow *w = windowOf(window);
	NSEvent *e = leftMouseDownIn(w);
	if (e == nil) {
		return;
	}
	[w performWindowDragWithEvent:e];
	// The window server moves the window and keeps the mouse-up that ends the drag: post one, so
	// that GLFW and Fyne, which saw the mouse-down, do not keep the button pressed and take the
	// next move for a drag.
	NSEvent *up = [NSEvent mouseEventWithType:NSEventTypeLeftMouseUp
	                                 location:e.locationInWindow
	                            modifierFlags:e.modifierFlags
	                                timestamp:e.timestamp
	                             windowNumber:e.windowNumber
	                                  context:nil
	                              eventNumber:e.eventNumber
	                               clickCount:e.clickCount
	                                 pressure:0];
	[NSApp postEvent:up atStart:NO];
}

void cogpitTitlebarDoubleClickSetting(char *action, int size, int *legacySet, int *legacyMinimize) {
	NSUserDefaults *d = [NSUserDefaults standardUserDefaults];
	NSString *a = [d stringForKey:@"AppleActionOnDoubleClick"];
	if (size > 0) {
		action[0] = 0;
		if (a != nil) {
			strlcpy(action, a.UTF8String, (size_t)size);
		}
	}
	id legacy = [d objectForKey:@"AppleMiniaturizeOnDoubleClick"];
	*legacySet = legacy != nil;
	*legacyMinimize = legacy != nil && [d boolForKey:@"AppleMiniaturizeOnDoubleClick"];
}

void cogpitTitlebarZoom(uintptr_t window) { [windowOf(window) zoom:nil]; }

void cogpitTitlebarMiniaturize(uintptr_t window) { [windowOf(window) miniaturize:nil]; }
