package model

import "strings"

// A row's states, which the views' status filter offers and matches (D-34, owner 2026-10-14):
// the texts of the kind's state columns (a Pod's Status "Running", a Flux object's "Not Ready", a
// Deployment's Conditions "Available" and "Progressing", an Argo CD Application's Sync and Health),
// or, for a kind without one, its rows' status tokens by name ("OK", "Pending", "Error"). Cogpit's
// rules (Cogpit's defaults, not the owner's decision, 2026-10-14):
//   - a kind names its state columns (KindProfile.States), else the columns with the ids of
//     DefaultStateColumns that it has are;
//   - a cell drawn in parts gives one state per part; a Words column one per word; any other cell
//     its whole text; an empty cell none;
//   - a kind with several state columns qualifies each state by its column's title ("Sync:
//     OutOfSync", "Health: Unknown"), since two columns may share a word; a kind with one does not;
//   - the filter keeps a row when, for every state column the selection names, the row carries
//     one of the states selected for that column (one column: any selected state); an unqualified
//     state selected in a kind with several matches that text in any of its columns.

// StateColumn is a column whose cells are a row's states.
type StateColumn struct {
	// ID is the column's id.
	ID string
	// Words: the cell lists several states separated by spaces, as a Conditions column lists the
	// condition types that are True ("Available Progressing"); else its whole text is one state
	// ("Not Ready", "CrashLoopBackOff").
	Words bool
}

// DefaultStateColumns are a kind's state columns when its profile names none: those of these
// ids it has, in its own column order.
var DefaultStateColumns = []StateColumn{
	{ID: "status"}, {ID: "state"}, {ID: "phase"}, {ID: "conditions", Words: true}, {ID: "sync"}, {ID: "health"},
}

// statusTitles name the status tokens as states, for the kinds without a state column.
var statusTitles = map[Status]string{
	StatusOK: "OK", StatusSucceeded: "Succeeded", StatusPending: "Pending", StatusWarning: "Warning",
	StatusError: "Error", StatusTerminating: "Terminating", StatusUnknown: "Unknown", StatusStopped: "Stopped",
	StatusEphemeral: "Ephemeral",
}

// StatusTitle names a status token as a state: "OK", "Pending"; "" for StatusNone.
func StatusTitle(s Status) string { return statusTitles[s] }

// RowState is one state of a row.
type RowState struct {
	// State is the state as the filter names it: the text, qualified by its column's title in a
	// kind with several state columns ("Health: Degraded").
	State string
	// Text is the state's own text ("Degraded").
	Text string
	// Status is the colour the row's cell draws the state in; none for a word of a Words cell
	// without parts, whose colour is the cell's, not the word's.
	Status Status
	// Column is the index of the state's column among the kind's state columns (0 for tokens).
	Column int
}

// States says where a kind's rows carry their states. Its zero value finds none.
type States struct {
	cols   []stateCol
	tokens bool // no state column: the rows' status tokens
}

type stateCol struct {
	index  int    // into the kind's columns
	words  bool   // StateColumn.Words
	title  string // the column's title
	prefix string // "<title>: " when the kind has several state columns
}

// StatesOf reads a kind's state columns: its profile's States, else those of
// DefaultStateColumns it has; a kind with neither takes its rows' status tokens.
func StatesOf(p *KindProfile) States {
	if p == nil {
		return States{}
	}
	var out States
	declared := p.States
	if declared == nil {
		declared = DefaultStateColumns
	}
	for i, c := range p.Columns {
		for _, sc := range declared {
			if sc.ID == c.ID {
				out.cols = append(out.cols, stateCol{index: i, words: sc.Words, title: c.Title})
				break
			}
		}
	}
	if len(out.cols) > 1 {
		for i := range out.cols {
			out.cols[i].prefix = out.cols[i].title + ": "
		}
	}
	out.tokens = len(out.cols) == 0
	return out
}

// Columns returns the titles of the state columns, in the kind's order; nil for a kind whose
// states are its rows' status tokens.
func (s States) Columns() []string {
	if len(s.cols) == 0 {
		return nil
	}
	out := make([]string, len(s.cols))
	for i, c := range s.cols {
		out[i] = c.title
	}
	return out
}

// Qualified returns a text of the column with the given id as the filter names the state:
// "<title>: <text>" in a kind with several state columns, else the text. A column that is no
// state column returns the text.
func (s States) Qualified(p *KindProfile, columnID, text string) string {
	i := p.ColumnIndex(columnID)
	for _, c := range s.cols {
		if c.index == i {
			return c.prefix + text
		}
	}
	return text
}

// Of returns the states of a row, in its state columns' order; nil for none.
func (s States) Of(row *Row) []RowState {
	if s.tokens {
		if t := StatusTitle(row.Status); t != "" {
			return []RowState{{State: t, Text: t, Status: row.Status}}
		}
		return nil
	}
	var out []RowState
	for ci, c := range s.cols {
		if c.index >= len(row.Cells) {
			continue
		}
		cell := &row.Cells[c.index]
		add := func(text string, st Status) {
			if text == "" {
				return
			}
			out = append(out, RowState{State: c.prefix + text, Text: text, Status: st, Column: ci})
		}
		switch {
		case cell.Parts != nil:
			for _, p := range *cell.Parts {
				add(p.Text, p.Status)
			}
		case c.words:
			for _, w := range strings.Fields(cell.Text) {
				add(w, StatusNone)
			}
		default:
			add(strings.TrimSpace(cell.Text), cell.Status)
		}
	}
	return out
}

// Facets returns how many state columns the kind has (1 for a kind of status tokens, 0 for none).
func (s States) Facets() int {
	if s.tokens {
		return 1
	}
	return len(s.cols)
}

// ColumnOf returns the index of the state column a selected state names: the column whose title
// qualifies it in a kind with several, 0 in a kind with one; -1 for an unqualified state in a kind
// with several (it matches its text in any column).
func (s States) ColumnOf(state string) int {
	if len(s.cols) < 2 {
		return 0
	}
	for i, c := range s.cols {
		if strings.HasPrefix(state, c.prefix) {
			return i
		}
	}
	return -1
}

// StateMatcher decides which rows a status filter keeps. Its zero value keeps every row.
type StateMatcher struct {
	want   map[string]int  // a selected state → its column
	plain  map[string]bool // unqualified states in a kind with several columns: their text in any
	groups int             // the columns the selection names, the any-column group counted as one
}

// maxStateGroups bounds the columns a matcher tells apart (a bit each); more never occur.
const maxStateGroups = 63

// Matcher returns the matcher of a selection of states.
func (s States) Matcher(selected []string) StateMatcher {
	if len(selected) == 0 {
		return StateMatcher{}
	}
	m := StateMatcher{want: make(map[string]int, len(selected))}
	used := map[int]bool{}
	for _, st := range selected {
		col := s.ColumnOf(st)
		if col < 0 || col >= maxStateGroups {
			if m.plain == nil {
				m.plain = map[string]bool{}
			}
			m.plain[st] = true
			col = -1
		} else {
			m.want[st] = col
		}
		used[col] = true
	}
	m.groups = len(used)
	return m
}

// All reports whether the matcher keeps every row.
func (m StateMatcher) All() bool { return m.groups == 0 }

// Match reports whether the matcher keeps a row with the given states.
func (m StateMatcher) Match(states []RowState) bool {
	if m.groups == 0 {
		return true
	}
	var seen uint64
	n := 0
	for _, st := range states {
		if col, ok := m.want[st.State]; ok && seen&(1<<col) == 0 {
			seen |= 1 << col
			n++
		}
		if m.plain[st.Text] && seen&(1<<maxStateGroups) == 0 {
			seen |= 1 << maxStateGroups
			n++
		}
	}
	return n == m.groups
}

// RowMatchesStates reports whether a row of a kind carries the states a status filter asks for;
// an empty selection keeps every row.
func RowMatchesStates(p *KindProfile, row Row, selected []string) bool {
	if len(selected) == 0 {
		return true
	}
	s := StatesOf(p)
	return s.Matcher(selected).Match(s.Of(&row))
}
