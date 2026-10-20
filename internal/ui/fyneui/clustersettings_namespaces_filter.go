package fyneui

import (
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The namespace filter when Namespaces cannot be listed (G3; engine/accessiblens_plan.go): it
// offers the cluster's accessible namespaces (Cluster Settings › Namespaces) and the namespaces
// the open view reads one by one, instead of nothing (inferred: Lens's filter offers the
// accessible namespaces then). Both come from the engine's state: UIState.ClusterSettings and the
// view's snapshot (TableSnapshot.Watched and Denied).

// namespaceChoices are the namespaces the filter offers: the cluster's, once listed, with those the
// filter names that are gone (nsgone.go, GW9: listed and marked until unticked); else the
// accessible namespaces, those the open view reads or names as denied, and the filter's own
// selection, sorted.
func (sh *shell) namespaceChoices(snap *engine.TableSnapshot) []string {
	if len(sh.namespaces) > 0 {
		gone := sh.goneNamespaces()
		if len(gone) == 0 {
			return sh.namespaces
		}
		out := append(slices.Clone(sh.namespaces), gone...)
		slices.Sort(out)
		return out
	}
	out := slices.Clone(sh.ui.ClusterSettings[sh.ui.Context].AccessibleNamespaces)
	if snap != nil && snap.View == sh.ui.View {
		out = append(append(out, snap.Watched...), snap.Denied...)
	}
	out = append(out, sh.ui.Namespaces...)
	slices.Sort(out)
	return slices.Compact(out)
}

// scopeNote says how the view's kind is read when the user may not list it in all namespaces:
// "Read in team-a, team-b only. Not permitted in team-c." (Cogpit's wording, inferred); empty
// when it is read in all namespaces.
func scopeNote(snap *engine.TableSnapshot) string {
	if snap == nil || (len(snap.Watched) == 0 && len(snap.Denied) == 0) {
		return ""
	}
	note := "You may not list " + snap.Title + " in all namespaces"
	if len(snap.Watched) > 0 {
		note += ": read in " + strings.Join(snap.Watched, ", ") + " only."
	} else {
		note += "."
	}
	if len(snap.Denied) > 0 {
		note += " Not permitted in " + strings.Join(snap.Denied, ", ") + "."
	}
	return note
}

// showScope shows the view's scope note under its header, a state, not an error.
func (tv *tableView) showScope(snap *engine.TableSnapshot) {
	if note := scopeNote(snap); note != "" {
		tv.scopeLine.SetText(note)
		tv.scopeLine.Show()
		return
	}
	tv.scopeLine.Hide()
}

// namespacesListable reports whether the namespace filter may try to list the cluster's
// namespaces: not when the access reviews said the user may not (the list would only be refused).
func (sh *shell) namespacesListable() bool {
	for _, k := range sh.kinds {
		if k.Kind == kindNamespaces {
			return k.Listable == nil || *k.Listable
		}
	}
	return true
}
