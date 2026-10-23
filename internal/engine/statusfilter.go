package engine

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The status filter of every view (D-34, owner 2026-10-14; ui-parity › Tables, beyond-lens ›
// Everyday use): a tab's own selection of the states its view's rows carry (model.States: a Pod's
// Status "Running", a Flux object's "Not Ready", an Argo CD Application's "Health: Degraded", a
// kind without a state column its rows' status tokens), applied to the view's snapshot like the
// search, so that the window's table, view.rows, select all and the CSV export show the same rows.
// Lens's views have no such filter: it is Cogpit's addition.
//
// Cogpit's defaults (not the owner's decision, 2026-10-14):
//   - the filter belongs to the view it was set for, as the search text does: opening another
//     view in the tab clears it (the states mean nothing there), and ui.openView with states opens
//     a view filtered (the dashboards' state links);
//   - the states offered (TableSnapshot.StateChoices) are those of the rows the namespace filter
//     and the search keep, each with its count, and the selected ones no row carries (count 0, so
//     that they can be unchecked);
//   - a selected state no row carries keeps nothing of its column: the filter is not dropped when
//     the last such row goes, as the search is not;
//   - ui.navigate clears the filter when it hides the object, as it clears the search.

// MaxStates bounds a status filter: the states selected, each at most MaxStateLength bytes.
const (
	MaxStates      = 100
	MaxStateLength = 512
)

// StateCount is one state the rows of a view carry: its name as the filter takes it, how many of
// the rows the namespace filter and the search keep carry it, the colour its cells draw it in (the
// most severe one when the rows differ), and the title of its column (empty for a kind whose states
// are its rows' status tokens).
type StateCount struct {
	State  string       `json:"state"`
	Count  int          `json:"count"`
	Status model.Status `json:"status,omitempty"`
	Column string       `json:"column,omitempty"`
}

// SetStates sets the status filter of the open view: the states its rows must carry (one of them
// per state column the selection names); empty clears it. Commands call it inside UpdateUI, after
// opening the view they filter.
func (u *UIState) SetStates(states []string) {
	u.States = slices.Clone(states)
	if len(u.States) == 0 {
		u.States = nil
	}
	u.statesView = u.View
}

// syncStates ends a status filter whose view is no longer the open one: a command changed the view
// without setting the filter for it. UpdateUI runs it before the tabs take the live state.
func (u *UIState) syncStates() {
	if u.statesView != u.View {
		u.States, u.statesView = nil, u.View
	}
}

// CleanStates trims, drops empty and repeated states, and bounds a status filter
// (MaxStates, MaxStateLength); nil for none.
func CleanStates(states []string) []string {
	var out []string
	for _, s := range states {
		s = strings.TrimSpace(s)
		if s == "" || len(s) > MaxStateLength || slices.Contains(out, s) {
			continue
		}
		if len(out) == MaxStates {
			break
		}
		out = append(out, s)
	}
	return out
}

// stateFilter is a snapshot's status filter: the kind's state columns, the selection's matcher,
// and the counts of the states the kept rows carry.
type stateFilter struct {
	spec     model.States
	cols     []string // the state columns' titles
	selected []string
	match    model.StateMatcher
	counts   map[string]*stateTally
}

type stateTally struct {
	count  int
	status model.Status
	column int // -1: a selected state of no column (an unqualified one in a kind with several)
}

func newStateFilter(p *model.KindProfile, selected []string) *stateFilter {
	spec := model.StatesOf(p)
	return &stateFilter{spec: spec, cols: spec.Columns(), selected: selected, match: spec.Matcher(selected), counts: map[string]*stateTally{}}
}

// keep counts a row's states, which the namespace filter and the search kept, and reports
// whether the status filter keeps the row too.
func (f *stateFilter) keep(states []model.RowState) bool {
	for _, st := range states {
		t := f.counts[st.State]
		if t == nil {
			t = &stateTally{column: st.Column}
			f.counts[st.State] = t
		}
		t.count++
		if moreSevere(st.Status, t.status) {
			t.status = st.Status
		}
	}
	return f.match.Match(states)
}

// choices returns the states counted, and the selected ones no kept row carries, by column in the
// kind's order, then by name ignoring case.
func (f *stateFilter) choices() []StateCount {
	for _, s := range f.selected {
		if f.counts[s] == nil {
			col := f.spec.ColumnOf(s)
			if col >= len(f.cols) {
				col = -1
			}
			f.counts[s] = &stateTally{column: col}
		}
	}
	if len(f.counts) == 0 {
		return nil
	}
	out := make([]StateCount, 0, len(f.counts))
	cols := make(map[string]int, len(f.counts))
	for s, t := range f.counts {
		sc := StateCount{State: s, Count: t.count, Status: t.status}
		if t.column >= 0 && t.column < len(f.cols) {
			sc.Column = f.cols[t.column]
		}
		cols[s] = t.column
		out = append(out, sc)
	}
	slices.SortFunc(out, func(a, b StateCount) int {
		ca, cb := cols[a.State], cols[b.State]
		if ca < 0 {
			ca = len(f.cols) // the unqualified ones last
		}
		if cb < 0 {
			cb = len(f.cols)
		}
		if c := cmp.Compare(ca, cb); c != 0 {
			return c
		}
		if c := compareFold(a.State, b.State); c != 0 {
			return c
		}
		return strings.Compare(a.State, b.State)
	})
	return out
}

// severity ranks the status tokens for a state's colour: an error outranks everything.
var severity = map[model.Status]int{
	model.StatusOK: 1, model.StatusSucceeded: 2, model.StatusStopped: 3, model.StatusUnknown: 4,
	model.StatusTerminating: 5, model.StatusPending: 6, model.StatusWarning: 7, model.StatusError: 8,
}

func moreSevere(a, b model.Status) bool { return severity[a] > severity[b] }

// statesCaughtUp reports whether a snapshot was filtered with the UI state's status filter.
func statesCaughtUp(t *TableSnapshot, u UIState) bool { return slices.Equal(t.States, u.States) }

// MatchesStates reports whether a row of a kind carries the states a status filter asks for (an
// empty filter keeps every row): ui.navigate's check that the filter does not hide an object.
func MatchesStates(p *model.KindProfile, row model.Row, states []string) bool {
	return model.RowMatchesStates(p, row, states)
}

// FilterRowsByStates keeps the rows of a kind that carry the states a status filter asks for, in
// place, and returns them: resources.list's states.
func FilterRowsByStates(p *model.KindProfile, rows []model.Row, states []string) []model.Row {
	if len(states) == 0 {
		return rows
	}
	spec := model.StatesOf(p)
	m := spec.Matcher(states)
	return slices.DeleteFunc(rows, func(r model.Row) bool { return !m.Match(spec.Of(&r)) })
}

// OpenTabFiltered opens a view of the connected cluster as OpenTab does in its preview tab (or the
// fixed tab that shows it), its status filter set to states: a dashboard's state link.
func (s *Session) OpenTabFiltered(view string, states []string) UIState {
	s.noteKindTitle(view)
	sort := s.ViewSort(view)
	return s.DropGoneSelected(context.Background(), s.UpdateUI(func(u *UIState) {
		u.openTab(view, sort, false)
		u.SetStates(states)
	}))
}
