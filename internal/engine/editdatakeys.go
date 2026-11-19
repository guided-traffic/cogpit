package engine

import (
	"context"
	"maps"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// A data key removed through an Edit tab (the owner's answer; D-55). The drawer's save that removes
// a key of a Config Map or a Secret is destructive (datakeys.go, N152); so is object.apply when the
// applied text no longer holds a key the cached object (or the version the edit is based on) holds:
// the same loss of data a workload may read, through the other way of writing it. object.apply
// escalates for it beside a scale to zero (EditScalesToZero), so the user confirms it for every
// origin, the window included, and an agent always asks, agent writes allowed or not (D-2).
//
// The keys compared are the object's, not one field's: a Config Map's data and binaryData, a
// Secret's data and stringData (the API server merges stringData into data and never returns it,
// so a key moved from data to stringData stays). A key moved between a Config Map's data and
// binaryData stays too: its value changes form, as a changed value does, which does not escalate
// (the lead's decisions, night run, in decisions.md D-55, with the comparison against the cache and
// the base together). Only key names are compared and named, never a value: the confirmation's
// detail lists the removed keys (DataKeysDetail).

// dataKeyFields are the fields of a kind's object whose keys an Edit tab's apply must not drop
// unconfirmed: a Config Map's and a Secret's; nil for every other kind.
func dataKeyFields(t ObjectTarget) []string {
	if t.GVR.Group != "" {
		return nil
	}
	switch t.GVR.Resource {
	case configMapsKind:
		return []string{"data", "binaryData"}
	case secretsKind:
		return []string{"data", "stringData"}
	}
	return nil
}

// objectDataKeys are the keys an object holds in the fields, as a set. A field that is not a
// mapping holds none.
func objectDataKeys(obj *unstructured.Unstructured, fields []string) map[string]bool {
	keys := map[string]bool{}
	for _, f := range fields {
		m, ok := obj.Object[f].(map[string]any)
		if !ok {
			continue
		}
		for k := range m {
			keys[k] = true
		}
	}
	return keys
}

// EditRemovesDataKeys returns, sorted, the keys of a Config Map's or a Secret's data that applying
// an Edit tab's text (or text) removes: the keys the object holds that the text holds in none of
// the kind's data fields. The object is both the cache's and the version the edit is based on,
// which is the one an apply overwrites (its precondition): right after an apply the cache may still
// hold the version before it, and a cache ahead of the base means a conflict, nothing written. Nil
// for another kind, a tab that is no Edit tab, a text that does not parse (the apply refuses it)
// and a text that keeps every key. Names only: no value is read into the result.
func (s *Session) EditRemovesDataKeys(id, text string) []string {
	e, err := s.editorOf(id)
	if err != nil || e.tab.Type != DockEdit {
		return nil
	}
	fields := dataKeyFields(e.target)
	if fields == nil {
		return nil
	}
	if text == "" {
		text = e.text
	}
	edited, err := ParseManifest(text)
	if err != nil {
		return nil
	}
	kept := objectDataKeys(edited, fields)
	var removed []string
	for _, k := range slices.Sorted(maps.Keys(s.editHeldKeys(e, fields))) {
		if !kept[k] {
			removed = append(removed, k)
		}
	}
	return removed
}

// editHeldKeys are the keys an Edit tab's object holds in the fields: in the tab's cluster's cache
// (shown or not, D-69) and in the version the edit is based on.
func (s *Session) editHeldKeys(e editor, fields []string) map[string]bool {
	held := map[string]bool{}
	if b := s.BackendOf(e.tab.Context); b != nil {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()
		if cached, err := cachedObject(ctx, b, e.target); err == nil {
			maps.Copy(held, objectDataKeys(cached, fields))
		}
	}
	if base, err := ParseManifest(e.baseText); err == nil {
		maps.Copy(held, objectDataKeys(base, fields))
	}
	return held
}
