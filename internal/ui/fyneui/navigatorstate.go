package fyneui

import (
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The navigator's search text and keyboard row are engine state (D-51, the owner's A51;
// engine/navigator.go), so that an agent sees what the user typed and where the keyboard stands,
// and sets them as the user does:
//   - the window reports every change of them, whatever moved the keyboard (a key, a click, a
//     search hiding the row, a view opened), a pause after the last change (searchDebounce, as the
//     view's search); a value the engine holds already is not sent;
//   - it shows a value the engine's state changed to (an agent's, or the engine's own rules: a new
//     level puts the keyboard on its root, a view opened on the view's row), unless a report of its
//     own still waits or runs, so that the echo of an older report never overwrites the user's
//     typing; once its reports are done it shows the engine's value if it differs;
//   - a row it does not show leaves its keyboard where it is: the engine cannot tell which rows
//     exist, the tree is built here from kinds.list;
//   - an agent's value never takes the keyboard focus: the search field opens without it, and the
//     ring shows only while the navigator has the focus from the keyboard (A53).
//
// The Local Kubeconfigs section's collapsed source groups (A63) are engine state too, set through
// ui.setNavigatorGroup (navigator_sections.go), so that workspace.json keeps them.

// navReport sends one of the navigator's values to the engine a pause after its last change: the
// newest value only.
type navReport struct {
	sh    *shell
	id    string           // the command
	param func(string) any // its parameters for a value
	delay time.Duration
	busy  int // reports waiting or under way; main goroutine
	timer *time.Timer
	idle  func() // runs on the main goroutine when the last report is done
}

func newNavReport(sh *shell, id string, param func(string) any, idle func()) *navReport {
	return &navReport{sh: sh, id: id, param: param, delay: searchDebounce, idle: idle}
}

// send reports v, unless the engine holds it (current) and no report waits: a value typed and typed
// back before it was sent is sent too, so that the last word is the window's.
func (r *navReport) send(v, current string) {
	if r.busy == 0 && v == current {
		return
	}
	r.cancel()
	r.busy++
	sh, id, p := r.sh, r.id, r.param(v)
	r.timer = time.AfterFunc(r.delay, func() {
		// A report the engine refuses (a row of the level the scope just left) changes nothing the
		// user can act on: the engine keeps its value, and idle shows it.
		sh.dispatch(id, p, func(any, error) {
			r.busy--
			if r.busy == 0 && r.idle != nil {
				r.idle()
			}
		})
	})
}

// cancel drops a report that waits; one under way finishes.
func (r *navReport) cancel() {
	if r.timer != nil && r.timer.Stop() {
		r.busy--
	}
	r.timer = nil
}

// Parameters of the navigator's commands, mirroring the engine's schemas.
type (
	navSearchParams struct {
		Search string `json:"search"`
	}
	navRowParams struct {
		Row string `json:"row"`
	}
	navGroupParams struct {
		Group     string `json:"group"`
		Collapsed bool   `json:"collapsed"`
	}
)

// initNavigatorState makes the reporters; newSidebar calls it.
func (s *sidebar) initNavigatorState() {
	s.searchRep = newNavReport(s.sh, "ui.setNavigatorSearch", func(v string) any { return navSearchParams{Search: v} }, s.applySearch)
	s.rowRep = newNavReport(s.sh, "ui.setNavigatorRow", func(v string) any { return navRowParams{Row: v} }, s.applyRow)
}

// navRow is the engine's form of the row the keyboard is on at the level shown.
func (s *sidebar) navRow() string {
	if s.section {
		return s.sec.engineRow(s.sec.focus)
	}
	if s.tree.focus == "" || s.tree.focus == rootNode {
		return engine.NavRowRoot
	}
	return s.tree.focus
}

// noteRow reports the keyboard's row when it is not the engine's. Called wherever the row may have
// moved: it costs a comparison when it did not.
func (s *sidebar) noteRow() {
	if s.rowRep == nil || s.applying || !s.sh.uiApplied {
		return // the shell is being built, the engine's row is being shown, or its state is not known yet
	}
	s.rowRep.send(s.navRow(), s.sh.ui.NavigatorRow())
}

// noteSearch reports the search field's text when it is not the engine's.
func (s *sidebar) noteSearch(text string) {
	if s.searchRep == nil || s.applying || !s.sh.uiApplied {
		return
	}
	s.searchRep.send(text, s.sh.ui.Navigator.Search)
}

// navigatorChanged shows what changed of the engine's navigator state, from old (applyUIState).
func (s *sidebar) navigatorChanged(old engine.NavigatorState) {
	now := s.sh.ui.Navigator
	if now.Search != old.Search {
		s.applySearch()
	}
	if now.Row != old.Row {
		s.applyRow()
	}
	if !slices.Equal(now.Collapsed, old.Collapsed) {
		s.sec.collapsedChanged()
	}
}

// applySearch shows the engine's search text, unless a report of the window's waits or runs. A text
// opens the field without the keyboard focus; an empty one closes it unless the user is in it.
func (s *sidebar) applySearch() {
	want, h := s.sh.ui.Navigator.Search, s.head
	if s.searchRep.busy > 0 || h.entry.Text == want {
		return
	}
	s.applying = true
	if want != "" && !h.searching() {
		h.field.Show()
		s.root.Refresh() // the header grew
	}
	h.entry.SetText(want) // filters at once (OnChanged)
	h.entry.CursorColumn = len([]rune(want))
	h.entry.Refresh()
	if want == "" && h.searching() && s.sh.win.Canvas().Focused() != h.entry {
		h.field.Hide()
		s.root.Refresh()
	}
	s.applying = false
	s.noteRow() // the search may have hidden the keyboard's row
}

// applyRow moves the keyboard to the engine's row, unless a report of the window's waits or runs or
// the navigator does not show the row. Where the row led elsewhere (a cluster of a collapsed group
// to its heading), the window reports where its keyboard is.
func (s *sidebar) applyRow() {
	want := s.sh.ui.NavigatorRow()
	if s.rowRep.busy > 0 || want == s.navRow() {
		return
	}
	s.applying = true
	moved := false
	if s.section {
		moved = s.sec.moveTo(want)
	} else {
		moved = s.tree.moveTo(want)
	}
	s.applying = false
	if moved {
		s.noteRow()
	}
}

// moveTo puts the tree's keyboard on an engine row: the root, or a node the tree has, opening the
// groups above it (as a view opened inside a closed group shows it). A node the tree does not have,
// or one the search hides, leaves the keyboard where it is.
func (t *navTree) moveTo(row string) bool {
	id, s := row, t.s
	if row == engine.NavRowRoot {
		id = rootNode
	}
	if id != rootNode {
		if _, known := s.parents[id]; !known {
			return false
		}
		if s.shown != nil && !s.shown[id] {
			return false
		}
		opened := false
		for p := s.parents[id]; p != ""; p = s.parents[p] {
			if !s.open[p] {
				s.open[p], opened = true, true
			}
		}
		if opened {
			t.rebuild()
		}
	}
	t.setFocus(id)
	return true
}

// engineRow is the engine's form of a row of the section.
func (n *navSections) engineRow(id string) string {
	switch {
	case id == "" || id == navSectionRoot:
		return engine.NavRowRoot
	case isSourceHead(id):
		return engine.NavRowSource(strings.TrimPrefix(id, navSourcePrefix))
	}
	return engine.NavRowContext(id)
}

// moveTo puts the section's keyboard on an engine row: its root, a source group's heading, or a
// cluster; a cluster inside a collapsed group puts it on the group's heading. A row the section does
// not show leaves the keyboard where it is.
func (n *navSections) moveTo(row string) bool {
	id := navSectionRoot
	switch {
	case row == engine.NavRowRoot:
	case strings.HasPrefix(row, engine.NavRowSourcePrefix):
		id = navSourcePrefix + strings.TrimPrefix(row, engine.NavRowSourcePrefix)
	case strings.HasPrefix(row, engine.NavRowContextPrefix):
		id = strings.TrimPrefix(row, engine.NavRowContextPrefix)
		if c, ok := n.context(id); ok && !n.isShown(id) {
			id = navSourcePrefix + sourceGroup(c)
		}
	default:
		return false
	}
	if !n.isShown(id) {
		return false
	}
	n.setFocus(id)
	return true
}
