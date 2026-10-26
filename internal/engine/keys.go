package engine

import (
	"fmt"
	"slices"
)

// A command's keys on its registry's platform (D-134; NewRegistryFor): its Shortcut, which menus
// show, and its MoreKeys, which run it too, from Spec.Shortcut and Spec.MoreKeys, or from
// Spec.PlatformKeys where that names the platform. The notation is the registry's: "Cmd" is ⌘ on macOS and Ctrl elsewhere,
// "Ctrl" the Control key everywhere.

// keyPlatforms are the platforms Spec.PlatformKeys may name.
var keyPlatforms = []string{"darwin", goosWindows, "linux"}

// keysOn returns a command's shortcut and further keys on the platform goos.
func keysOn(goos, shortcut string, more []string, platform map[string][]string) (string, []string) {
	if keys, ok := platform[goos]; ok && len(keys) > 0 {
		return keys[0], slices.Clone(keys[1:])
	}
	return shortcut, slices.Clone(more)
}

// checkKeys refuses keys a spec cannot mean: further keys without a shortcut, a platform Cogpit
// does not run on, a platform named without keys.
func checkKeys(shortcut string, more []string, platform map[string][]string) error {
	if shortcut == "" && len(more) > 0 {
		return fmt.Errorf("further keys %v without a shortcut", more)
	}
	for goos, keys := range platform {
		if !slices.Contains(keyPlatforms, goos) {
			return fmt.Errorf("keys for %q: one of %v", goos, keyPlatforms)
		}
		if len(keys) == 0 || keys[0] == "" {
			return fmt.Errorf("keys for %s: none", goos)
		}
	}
	return nil
}
