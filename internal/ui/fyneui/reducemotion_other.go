//go:build !darwin

package fyneui

// systemReduceMotion is false outside macOS: Cogpit reads no such setting on Linux and Windows yet;
// its own preference applies there too (shell.reduceMotion, preferences_motion.go).
func systemReduceMotion() bool { return false }

// watchReduceMotion watches nothing outside macOS: there is no setting to follow.
func watchReduceMotion(func()) {}
