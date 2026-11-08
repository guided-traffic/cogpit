package engine

import (
	"fmt"
	"slices"
	"strings"
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

// Row keys (Spec.RowKeys, D-158): k9s's single keys, which act on the object the
// resource table's keyboard is on or on the drawer's object. A row key is one printable character
// other than a space ("l", "J", "?"), or a key with modifiers in the registry's notation
// ("Ctrl+D", "Cmd+Backspace") or a named key alone ("Delete").

// rowKeyNames are the named keys a row key may use, as Fyne names them.
var rowKeyNames = []string{"Backspace", "Delete", "Return", "Escape"}

// rowKeyMods are the modifiers a row key may use, in the order the notation writes them.
var rowKeyMods = []string{"Ctrl", "Cmd", "Alt", "Shift"}

// checkRowKeys refuses a row key that is neither one character nor a key the notation knows.
func checkRowKeys(keys []string) error {
	for _, k := range keys {
		if err := checkRowKey(k); err != nil {
			return fmt.Errorf("row key %q: %w", k, err)
		}
	}
	return nil
}

func checkRowKey(k string) error {
	if r := []rune(k); len(r) == 1 {
		if r[0] == ' ' || r[0] < ' ' {
			return fmt.Errorf("not a printable character")
		}
		return nil
	}
	parts := strings.Split(k, "+")
	name := parts[len(parts)-1]
	for _, m := range parts[:len(parts)-1] {
		if !slices.Contains(rowKeyMods, m) {
			return fmt.Errorf("modifier %q: one of %v", m, rowKeyMods)
		}
	}
	if len(parts) == 1 && !slices.Contains(rowKeyNames, name) {
		return fmt.Errorf("a single character or one of %v", rowKeyNames)
	}
	if !slices.Contains(rowKeyNames, name) && (len(name) != 1 || name[0] < 'A' || name[0] > 'Z') && name != `\` {
		return fmt.Errorf("key %q: an upper-case letter, \\ or one of %v", name, rowKeyNames)
	}
	return nil
}

// RowKeyActions returns the commands a row key runs on an object of kind, best first: the kind's
// own actions (Kinds names the kind) before those of every kind, each in menu order. The window
// runs the first that applies to the object (Applicable): Suspend and Resume share a key and only
// one applies to a Cron Job. The drawer-only actions count: the key acts on the drawer's object
// too.
func (r *Registry) RowKeyActions(kind, key string) []*Command {
	var own, all []*Command
	for _, c := range r.Actions(kind) {
		if !slices.Contains(c.RowKeys, key) {
			continue
		}
		if len(c.Kinds) > 0 {
			own = append(own, c)
		} else {
			all = append(all, c)
		}
	}
	return append(own, all...)
}

// RowKeySelectionActions returns the commands a row key runs on a selection of objects of the given
// kinds, best first as RowKeyActions orders them.
func (r *Registry) RowKeySelectionActions(kinds []string, key string) []*Command {
	var own, all []*Command
	for _, c := range r.SelectionActions(kinds) {
		if !slices.Contains(c.RowKeys, key) {
			continue
		}
		if len(c.Kinds) > 0 {
			own = append(own, c)
		} else {
			all = append(all, c)
		}
	}
	return append(own, all...)
}
