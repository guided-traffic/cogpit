// macOS's Reduce motion setting: the AppKit side of reducemotion_darwin.go. Compiled without ARC,
// like the rest of the package's Objective-C (Fyne's).

#import <Cocoa/Cocoa.h>

#include "reducemotion_darwin.h"

extern void cogpitReduceMotionChanged(void);

int cogpitReduceMotion(void) {
	return [[NSWorkspace sharedWorkspace] accessibilityDisplayShouldReduceMotion] ? 1 : 0;
}

void cogpitWatchReduceMotion(void) {
	// AppKit posts it on the workspace's own notification centre, on the main thread.
	dispatch_async(dispatch_get_main_queue(), ^{
		[[[NSWorkspace sharedWorkspace] notificationCenter] addObserverForName:NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification
		                                                                  object:nil
		                                                                   queue:nil
		                                                              usingBlock:^(NSNotification *note) {
			(void)note;
			cogpitReduceMotionChanged();
		}];
	});
}
