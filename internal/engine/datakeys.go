package engine

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// Keys added and removed in a Config Map's or a Secret's drawer (the owner's audit decision of
// 2026-10-16, N152 (b); decisions D-55 and D-59). The drawers' saves change values of keys the data
// holds, and add keys with their values and remove keys, in one write: configMap.saveData and
// secret.saveData take them as add and remove beside data. A key is added only when it is named in
// add (a key of data that the data does not hold is refused, so that a typo never adds one), and
// its name follows the API server's rule for ConfigMap and Secret keys (CheckDataKey). A removal is
// a loss of data a workload may read: a save that removes a key is destructive (the commands
// escalate), so the user confirms it for every origin, the window's review being that
// confirmation, and an agent always asks. The review's rebase (D-55's (9)) moves an edit onto a
// newer version only when none of its keys changed there: a changed or removed key holds the value
// it had (from), an added key is still absent. Against the version the edit is based on an added
// key the data holds and a removed key it does not hold are refused; against a newer version, which
// a review shows as stale, an added key that appeared there is a change of its value and a removed
// key that is gone there is left out, so that the review shows what a save over that version would
// write.

// DataEdit is a change of a Config Map's or a Secret's data as a drawer saves it: new values of keys
// the data holds (Values), keys added with their values (Add), keys removed (Remove), and each
// changed or removed key's value at the version the edit is based on (From), for the review's
// rebase; an added key has none.
type DataEdit struct {
	Values map[string]string
	Add    map[string]string
	Remove []string
	From   map[string]string
}

// empty reports whether the edit changes nothing.
func (e DataEdit) empty() bool { return len(e.Values)+len(e.Add)+len(e.Remove) == 0 }

// size is the text the edit carries: its keys and values.
func (e DataEdit) size() int {
	n := 0
	for k, v := range e.Values {
		n += len(k) + len(v)
	}
	for k, v := range e.Add {
		n += len(k) + len(v)
	}
	for _, k := range e.Remove {
		n += len(k)
	}
	return n
}

// check refuses an edit that names a key twice (changed and added, changed and removed, added and
// removed, removed twice) or adds a key the API server would refuse. Its errors name keys, never a
// value.
func (e DataEdit) check() error {
	if e.empty() {
		return errors.New("give data, add or remove: the keys whose values change, the keys to add with their values, or the " +
			"keys to remove; nothing was saved")
	}
	for _, k := range slices.Sorted(maps.Keys(e.Add)) {
		if err := CheckDataKey(k); err != nil {
			return fmt.Errorf("%w; nothing was saved", err)
		}
		if _, ok := e.Values[k]; ok {
			return fmt.Errorf("the key %q is both changed and added; nothing was saved", k)
		}
	}
	seen := map[string]bool{}
	for _, k := range e.Remove {
		switch _, inValues := e.Values[k]; {
		case k == "":
			return errors.New("a key to remove has no name; nothing was saved")
		case seen[k]:
			return fmt.Errorf("the key %q is removed twice; nothing was saved", k)
		case inValues:
			return fmt.Errorf("the key %q is both changed and removed; nothing was saved", k)
		}
		if _, ok := e.Add[k]; ok {
			return fmt.Errorf("the key %q is both added and removed; nothing was saved", k)
		}
		seen[k] = true
	}
	return nil
}

// rebasable reports whether the edit names what its rebase compares: each changed or removed key's
// value at the base version in from. An edit that only adds keys compares their absence only.
func (e DataEdit) rebasable(from map[string]string) bool {
	if e.empty() {
		return false
	}
	for k := range e.Values {
		if _, ok := from[k]; !ok {
			return false
		}
	}
	for _, k := range e.Remove {
		if _, ok := from[k]; !ok {
			return false
		}
	}
	return true
}

// unchangedSince reports whether none of the edit's keys changed in data since the version the edit
// is based on: every changed and removed key holds the value from names for it (value reads one of
// data), every added key is absent from data and from other (a Config Map's binary data). A key
// from does not name, or a changed or removed one data no longer holds, has changed.
func (e DataEdit) unchangedSince(data, other map[string]any, from map[string]string, value func(any) (string, bool)) bool {
	if !e.rebasable(from) {
		return false
	}
	same := func(k string) bool {
		now, ok := value(data[k])
		return ok && now == from[k]
	}
	for k := range e.Values {
		if !same(k) {
			return false
		}
	}
	for _, k := range e.Remove {
		if !same(k) {
			return false
		}
	}
	for k := range e.Add {
		if _, ok := data[k]; ok {
			return false
		}
		if _, ok := other[k]; ok {
			return false
		}
	}
	return true
}

// dataChanges are what an edit changes in an object's data, each sorted: the keys whose values
// differ, the keys added, the keys removed.
type dataChanges struct {
	changed, added, removed []string
}

func (c dataChanges) none() bool { return len(c.changed)+len(c.added)+len(c.removed) == 0 }

// CheckDataKey checks a key of a Config Map's or a Secret's data as the API server does
// (validation.IsConfigMapKey): letters, digits, '-', '_' and '.', at most 253 characters, not "."
// or ".." and not starting with "..". Its error names the key, which is no value.
func CheckDataKey(key string) error {
	if key == "" {
		return errors.New("a key needs a name")
	}
	if len(validation.IsConfigMapKey(key)) == 0 {
		return nil
	}
	switch {
	case len(key) > validation.DNS1123SubdomainMaxLength:
		return fmt.Errorf("a key has at most %d characters, %q has %d", validation.DNS1123SubdomainMaxLength, truncateKey(key), len(key))
	case key == "." || key == ".." || strings.HasPrefix(key, ".."):
		return fmt.Errorf("the key %q is not allowed: a key is not \".\" or \"..\" and does not start with \"..\"", key)
	}
	return fmt.Errorf("the key %q is not allowed: a key holds only letters, digits, '-', '_' and '.'", key)
}

// truncateKey shortens an overlong key for a message.
func truncateKey(key string) string {
	if len(key) <= 40 {
		return key
	}
	return key[:40] + "…"
}

// maxDetailKeys is how many keys a confirmation's effect names before "and N more".
const maxDetailKeys = 10

// DataKeysEffects are the confirmation's rows of a save or an apply that adds or removes keys of
// a Config Map's or a Secret's data (configMap.saveData, object.apply): Removes, in the error
// colour, then Adds, the keys' names only, never a value; none when it does neither. The drawer's
// save review shows the same rows (the owner's WF33, 2026-10-19).
func DataKeysEffects(added, removed []string) []Effect {
	return dataEffects(nil, added, removed)
}

// DataKeysDetail is DataKeysEffects' text, the confirmation's Detail, which the drawer's review
// binds as its confirmation of a removal.
func DataKeysDetail(added, removed []string) string {
	return EffectsText(DataKeysEffects(added, removed))
}

// SecretSaveEffects are secret.saveData's rows: the keys it removes, adds and changes, and that
// the values are not shown.
func SecretSaveEffects(changed, added, removed []string) []Effect {
	return append(dataEffects(changed, added, removed), Effect{Name: "Values", Value: "not shown"})
}

// SecretSaveDetail is SecretSaveEffects' text, the confirmation's Detail.
func SecretSaveDetail(changed, added, removed []string) string {
	return EffectsText(SecretSaveEffects(changed, added, removed))
}

func dataEffects(changed, added, removed []string) []Effect {
	var out []Effect
	for _, c := range []struct {
		name string
		keys []string
		tone EffectTone
	}{{"Removes", removed, EffectLost}, {"Adds", added, EffectNames}, {"Changes", changed, EffectNames}} {
		if len(c.keys) > 0 {
			out = append(out, Effect{Name: c.name, Value: keyList(c.keys), Tone: c.tone})
		}
	}
	return out
}

// keyList names keys, sorted: "a", "a, b", "a, … and 3 more".
func keyList(keys []string) string {
	sorted := slices.Sorted(slices.Values(keys))
	if len(sorted) > maxDetailKeys {
		return strings.Join(sorted[:maxDetailKeys], ", ") + fmt.Sprintf(" and %d more", len(sorted)-maxDetailKeys)
	}
	return strings.Join(sorted, ", ")
}
