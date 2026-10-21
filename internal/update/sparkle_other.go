//go:build (!darwin || !cgo) && !linux && !windows

package update

// Elsewhere (macOS built without cgo, the BSDs) there is no updater.

func load() error             { return ErrNotMacOS }
func startFailure() error     { return nil }
func start(bool)              {}
func check()                  {}
func setAutomaticChecks(bool) {}
