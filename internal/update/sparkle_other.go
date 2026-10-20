//go:build (!darwin || !cgo) && !linux

package update

// Elsewhere (Windows, and macOS built without cgo) there is no updater yet: Windows with M11.

func load() error             { return ErrNotMacOS }
func startFailure() error     { return nil }
func start(bool)              {}
func check()                  {}
func setAutomaticChecks(bool) {}
