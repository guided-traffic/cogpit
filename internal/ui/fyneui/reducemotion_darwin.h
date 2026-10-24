// macOS's Reduce motion setting: the AppKit side of reducemotion_darwin.go.

// cogpitReduceMotion returns 1 while System Settings › Accessibility › Display › Reduce motion is
// on (NSWorkspace's accessibilityDisplayShouldReduceMotion), else 0.
int cogpitReduceMotion(void);

// cogpitWatchReduceMotion observes NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification and
// calls the Go side's cogpitReduceMotionChanged for each. Call it once, with the app running.
void cogpitWatchReduceMotion(void);
