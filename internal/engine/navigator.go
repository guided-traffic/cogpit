package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The navigator's own state (D-51, the owner's A51 and A63): its search text, the row its keyboard
// is on, and which of the Local Kubeconfigs section's source groups are collapsed are engine state,
// so that an agent sees and sets what the user typed and where the keyboard stands (golden rule 2;
// architecture §4.3).
//
// How the build settled what D-51 left open (the lead's night decisions, for the owner to confirm;
// recorded in D-51):
//   - one state per window, as UIState is one per window (D-52 open), not per tab: the navigator
//     is one per window and its scope already decides which tabs show;
//   - the keyboard's row is held as a row of the level shown, not as a view key alone: at a
//     cluster NavRowRoot, a view key ("pods", "workloads/overview"), a group ("group:workloads")
//     or an alias ("alias:<kind key>", a kind listed again under Custom Resources); at the section
//     NavRowRoot, "context:<context id>" or "source:<label>", a source group's heading;
//   - the search takes at most MaxNavigatorSearch bytes, as the view's search, one line;
//   - workspace.json keeps the collapsed source groups (A63) and neither the search nor the row:
//     the navigator starts at the section, where a search or a row of the last run means little.
//
// The window reports every change of its row and its search (debounced as the view's search is)
// and applies the engine's when no report of its own is under way; the engine keeps them in step
// with the scope and the open view (syncNavigator), as the window moves its keyboard: a change of
// level clears the search and puts the keyboard on the level's root (into a cluster) or on the
// cluster it came from (up to the section); a view opened at a cluster puts it on the view's row.
// The engine cannot tell whether a row exists (the tree is built from kinds.list in the window):
// a row the window does not show leaves its keyboard where it is.

// Navigator rows that are not a view key or a group (NavigatorState.Row).
const (
	// NavRowRoot is the root of the level shown: the cluster, or Local Kubeconfigs.
	NavRowRoot = "root"
	// NavRowContextPrefix starts a cluster's row at the section: "context:<context id>".
	NavRowContextPrefix = "context:"
	// NavRowSourcePrefix starts a source group's heading at the section: "source:<label>".
	NavRowSourcePrefix = "source:"
)

// Bounds of the navigator's state (Cogpit's defaults, the lead's night decision).
const (
	// MaxNavigatorSearch bounds the search text, as the view's search (MaxFilterPattern).
	MaxNavigatorSearch = MaxFilterPattern
	// maxNavRow bounds a row: a context id is the longest (maxHotbarContext) plus its prefix.
	maxNavRow = maxHotbarContext + len(NavRowContextPrefix)
	// maxNavGroups bounds the collapsed source groups kept.
	maxNavGroups = maxStoredList
)

// Errors of the navigator's commands.
var (
	ErrNavRow    = errors.New("not a row of the navigator")
	ErrNavSearch = errors.New("not a search text the navigator takes")
)

// NavigatorState is the navigator's own state (D-51).
type NavigatorState struct {
	// Search is the navigator's search text: at a cluster it filters the tree's items by title, at
	// the section the clusters by name, display name and source; "" shows every row.
	Search string `json:"search,omitempty"`
	// Row is the row the navigator's keyboard is on at the level shown: NavRowRoot; at a cluster a
	// view key, "group:<path>" or "alias:<kind key>"; at the section "context:<context id>" or
	// "source:<label>". "" until the scope or the open view first changed: DefaultNavigatorRow.
	Row string `json:"row,omitempty"`
	// Collapsed are the Local Kubeconfigs section's source groups the user collapsed, by the label
	// their heading shows (".kube/", "dev.yaml"), in the order they were collapsed; kept across
	// restarts in workspace.json (A63, D-41).
	Collapsed []string `json:"collapsed,omitempty"`
}

func (n NavigatorState) clone() NavigatorState {
	n.Collapsed = slices.Clone(n.Collapsed)
	return n
}

// NavRowContext is the section's row of a cluster.
func NavRowContext(contextID string) string { return NavRowContextPrefix + contextID }

// NavRowSource is the section's row of a source group's heading.
func NavRowSource(label string) string { return NavRowSourcePrefix + label }

// DefaultNavigatorRow is the row the navigator's keyboard is on when the state names none: at a
// cluster the open view's row, else the tree's root; at the section the row of the context the state
// names, else its root.
func (u UIState) DefaultNavigatorRow() string {
	switch {
	case u.ScopeCluster() != "" && u.View != "":
		return u.View
	case u.ScopeCluster() == "" && u.Context != "":
		return NavRowContext(u.Context)
	}
	return NavRowRoot
}

// NavigatorRow is the navigator's row, DefaultNavigatorRow while the state names none.
func (u UIState) NavigatorRow() string {
	if u.Navigator.Row != "" {
		return u.Navigator.Row
	}
	return u.DefaultNavigatorRow()
}

// syncNavigator keeps the navigator's state and the palette's text in step with a change of the UI
// state from old to u, whichever command made it (UpdateUI, after syncNavScope):
//   - a closed palette has no text;
//   - a change of the navigator's level clears the search and puts the keyboard on the level's
//     default row (the cluster's root, or at the section the cluster it came from);
//   - a view opened at a cluster puts the keyboard on the view's row, as the window's tree moves it.
func (u *UIState) syncNavigator(old UIState) {
	if !u.Palette {
		u.PaletteText, u.PaletteTextOrigin = "", ""
	}
	if u.Scope() != old.Scope() {
		u.Navigator.Search, u.Navigator.Row = "", NavRowRoot
		if from := old.ScopeCluster(); u.ScopeCluster() == "" && from != "" {
			u.Navigator.Row = NavRowContext(from)
		}
		return
	}
	if u.View != "" && u.View != old.View && u.ScopeCluster() != "" {
		u.Navigator.Row = u.View
	}
}

// CheckNavigatorSearch returns why a search text cannot be the navigator's, or nil.
func CheckNavigatorSearch(text string) error {
	switch {
	case len(text) > MaxNavigatorSearch:
		return fmt.Errorf("%w: longer than %d bytes", ErrNavSearch, MaxNavigatorSearch)
	case !utf8.ValidString(text):
		return fmt.Errorf("%w: not UTF-8", ErrNavSearch)
	case strings.ContainsFunc(text, unicode.IsControl):
		return fmt.Errorf("%w: one line without control characters", ErrNavSearch)
	}
	return nil
}

// CheckNavigatorRow returns why row cannot be a row of the navigator at scope, or nil: a cluster's
// tree has the root, view keys, groups and aliases; the section its root, clusters and source
// groups. Whether such a row exists the window decides.
func CheckNavigatorRow(scope, row string) error {
	switch {
	case row == "":
		return fmt.Errorf("%w: empty", ErrNavRow)
	case len(row) > maxNavRow || !utf8.ValidString(row) || strings.ContainsFunc(row, unicode.IsControl):
		return fmt.Errorf("%w: at most %d bytes of one line", ErrNavRow, maxNavRow)
	case row == NavRowRoot:
		return nil
	}
	ctx, isCtx := strings.CutPrefix(row, NavRowContextPrefix)
	src, isSrc := strings.CutPrefix(row, NavRowSourcePrefix)
	if strings.HasPrefix(scope, navScopeClusterPrefix) {
		if isCtx || isSrc {
			return fmt.Errorf("%w %q: the navigator shows a cluster's tree, whose rows are root, a view key (pods, "+
				"workloads/overview), group:<path> and alias:<kind key>", ErrNavRow, row)
		}
		if strings.ContainsFunc(row, unicode.IsSpace) {
			return fmt.Errorf("%w %q: a view key, group or alias has no spaces", ErrNavRow, row)
		}
		return nil
	}
	switch {
	case isCtx && strings.TrimSpace(ctx) != "", isSrc && strings.TrimSpace(src) != "":
		return nil
	}
	return fmt.Errorf("%w %q: the navigator shows the Local Kubeconfigs section, whose rows are root, "+
		"context:<context id> and source:<label>", ErrNavRow, row)
}

// SetNavigatorSearch sets the navigator's search text (ui.setNavigatorSearch) and returns the new state.
func (s *Session) SetNavigatorSearch(text string) (UIState, error) {
	if err := CheckNavigatorSearch(text); err != nil {
		return s.UIState(), err
	}
	return s.UpdateUI(func(u *UIState) { u.Navigator.Search = text }), nil
}

// SetNavigatorRow puts the navigator's keyboard on a row of the level shown (ui.setNavigatorRow) and
// returns the new state.
func (s *Session) SetNavigatorRow(row string) (UIState, error) {
	var err error
	st := s.UpdateUI(func(u *UIState) {
		if err = CheckNavigatorRow(u.Scope(), row); err == nil {
			u.Navigator.Row = row
		}
	})
	return st, err
}

// SetNavigatorGroup collapses or opens a source group of the Local Kubeconfigs section
// (ui.setNavigatorGroup) and returns the new state. Collapsing one that is collapsed, or opening one
// that is open, changes nothing.
func (s *Session) SetNavigatorGroup(label string, collapsed bool) (UIState, error) {
	if label == "" || len(label) > maxStoredKey || !utf8.ValidString(label) || strings.ContainsFunc(label, unicode.IsControl) {
		return s.UIState(), fmt.Errorf("%w: a source group is named by its heading's label, at most %d bytes", ErrNavRow, maxStoredKey)
	}
	var err error
	st := s.UpdateUI(func(u *UIState) {
		i := slices.Index(u.Navigator.Collapsed, label)
		switch {
		case collapsed && i < 0 && len(u.Navigator.Collapsed) >= maxNavGroups:
			err = fmt.Errorf("at most %d source groups stay collapsed", maxNavGroups)
		case collapsed && i < 0:
			u.Navigator.Collapsed = append(slices.Clone(u.Navigator.Collapsed), label)
		case !collapsed && i >= 0:
			u.Navigator.Collapsed = slices.Delete(slices.Clone(u.Navigator.Collapsed), i, i+1)
			if len(u.Navigator.Collapsed) == 0 {
				u.Navigator.Collapsed = nil
			}
		}
	})
	return st, err
}
