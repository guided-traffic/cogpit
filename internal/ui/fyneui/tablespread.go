package fyneui

import (
	"maps"
	"math"
	"slices"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A table's free width (D-145; the owner's QW7: "beim neu öffnen von Masken ... freien Platz auf
// bedürftige Spalten verteilen ... nicht erneut ausführen, wenn der Nutzer die Maske schon offen
// hat und die Inhalte sich verändern"). Every column gets its width (columnWidth); what the frame
// leaves free goes first to the columns whose content is cut, each up to its widest text, and the
// rest to the flex column (Name), which took all of it before. Preferences › Tables › Free width
// (engine.Preferences.TableSpreadWidth, on by default) turns it off.
//
// The lead's decisions (D-74, open to revision):
//   - what a column's content wants is measured once, when a view opens (another view, tab or
//     cluster, the first snapshot after none), from its first spreadRows rows as sorted, once its
//     rows are in (the first snapshot with rows, or synced without any); rows that change, come or
//     go while it stays open are not measured again, so no column moves under the user's eyes;
//   - the split follows the frame: a resized window or an opened drawer lays the same measure out
//     at the new width, the flex column's share giving way first, as it did before;
//   - a column shown while the view stays open is measured when it shows (the user's own act), a
//     column hidden again forgotten;
//   - too little free width for every cut column: each gets an equal share, at most what it needs,
//     and what a column needs less is shared among the others (water filling), so the columns cut
//     by a little show whole first;
//   - a column the user sized (dragged, fitted by a double click) keeps the user's width and takes
//     no share; so does the flex column, and then the rest of the free width stays free, as it did;
//     fixed columns (the warning, the grades) and icon columns take no share;
//   - a column of labels or of a selector (model.Column.Pairs) takes no share while the
//     preference's exception for it is on, the default (D-153): such lists are read in the drawer;
//   - turning the preference or an exception on or off lays the open table out at once: the
//     user's own act.

// spreadRows is how many rows, from the top of the list as sorted, a view's columns are measured
// in when it opens: far more than a window shows at once, and a bounded cost on a list of 10,000
// (BenchmarkSpreadMeasure10k).
const spreadRows = 500

// spreadWidth reports whether tables give their free width to the columns that are cut: the
// preference, on without preferences (the default).
func (sh *shell) spreadWidth() bool {
	p := sh.ui.Preferences
	return p == nil || p.TableSpreadWidth
}

// spreadExcepted reports whether column c takes no share of the free width: a column of labels or
// of a selector while Preferences › Tables › Free width excepts it (on without preferences, the
// default).
func (sh *shell) spreadExcepted(c model.Column) bool {
	p := sh.ui.Preferences
	switch c.Pairs {
	case model.PairsLabels:
		return p == nil || p.TableSpreadExceptLabels
	case model.PairsSelector:
		return p == nil || p.TableSpreadExceptSelectors
	}
	return false
}

// spreadExceptions are the exceptions in effect, for spreadChanged to compare.
func (sh *shell) spreadExceptions() [2]bool {
	return [2]bool{sh.spreadExcepted(model.Column{Pairs: model.PairsLabels}), sh.spreadExcepted(model.Column{Pairs: model.PairsSelector})}
}

// spreadSnapshot follows a snapshot shown: a view opened is measured once its rows are in, and a
// column shown while the view stays open when it shows. It reports whether the widths are to be
// laid out again.
func (tv *tableView) spreadSnapshot(opened, colsChanged bool) bool {
	if opened || tv.snap == nil {
		tv.wants, tv.wantsDue = nil, tv.snap != nil
	}
	if !tv.sh.spreadWidth() {
		tv.wants, tv.wantsDue = nil, false
		return false
	}
	switch {
	case tv.wantsDue && (len(tv.snap.Rows) > 0 || tv.snap.Synced):
		tv.wants, tv.wantsDue = map[string]float32{}, false
		tv.measureColumns()
		return true
	case colsChanged && tv.wants != nil:
		tv.forgetHidden()
		tv.measureColumns()
		return true
	}
	return false
}

// forgetHidden drops the measure of the columns no longer visible: shown again, a column is
// measured as it shows.
func (tv *tableView) forgetHidden() {
	shown := make(map[string]bool, len(tv.cols))
	for _, ci := range tv.cols {
		shown[tv.snap.Columns[ci].ID] = true
	}
	maps.DeleteFunc(tv.wants, func(id string, _ float32) bool { return !shown[id] })
}

// spreadChanged follows the preference and its exceptions: the open table is laid out at once,
// measured now when the preference is turned on.
func (tv *tableView) spreadChanged() {
	on, except := tv.sh.spreadWidth(), tv.sh.spreadExceptions()
	if on == tv.spreadOn && except == tv.spreadExcept {
		return
	}
	if on != tv.spreadOn {
		tv.spreadOn = on
		tv.wants, tv.wantsDue = nil, on && tv.snap != nil
		if tv.snap != nil {
			tv.spreadSnapshot(false, false)
		}
	}
	tv.spreadExcept = except
	tv.fitWidth = 0
	tv.fitColumns(tv.table.Size().Width)
}

// measureColumns measures what the visible columns not measured yet want: the width that shows
// their title and their first spreadRows rows whole.
func (tv *tableView) measureColumns() {
	n := min(len(tv.snap.Rows), spreadRows)
	for _, ci := range tv.cols {
		c := tv.snap.Columns[ci]
		if _, ok := tv.wants[c.ID]; ok || fixedWidth(c) || c.Type == model.ColumnIcon {
			continue
		}
		tv.wants[c.ID] = tv.contentWidth(ci, n)
	}
}

// spread gives the visible columns whose content is cut their share of surplus, the width the
// frame leaves free, in widths (by table column), and returns what it gave the columns other than
// flex: the flex column's share and the rest stay in its fill (fitColumns). flexUser says the user
// sized the flex column.
func (tv *tableView) spread(widths []float32, surplus float32, flex int, flexUser bool) float32 {
	if tv.wants == nil || surplus < 1 {
		return 0
	}
	need := make([]float32, len(tv.cols))
	for i, ci := range tv.cols {
		c := tv.snap.Columns[ci]
		if _, user := tv.userWidths[tv.snap.View+"/"+c.ID]; user || (i == flex && flexUser) || tv.sh.spreadExcepted(c) {
			continue
		}
		need[i] = max(0, tv.wants[c.ID]-widths[firstDataColumn+i])
	}
	given := float32(0)
	for i, e := range spreadExtra(need, surplus) {
		if i != flex {
			widths[firstDataColumn+i] += e
			given += e
		}
	}
	return given
}

// spreadExtra shares surplus points among columns by need, what each lacks to show its content
// whole (0: nothing): every column in need gets an equal share in whole points, at most its need,
// and what a column takes less is shared again among the others, the smallest needs met first.
// It returns each column's share; what is left (a need met in full leaves points over, or fewer
// points than columns) is the caller's.
func spreadExtra(need []float32, surplus float32) []float32 {
	extra := make([]float32, len(need))
	var open []int
	for i, n := range need {
		if n > 0 {
			open = append(open, i)
		}
	}
	slices.SortStableFunc(open, func(a, b int) int {
		switch {
		case need[a] < need[b]:
			return -1
		case need[a] > need[b]:
			return 1
		}
		return 0
	})
	left := surplus
	for len(open) > 0 {
		share := float32(math.Floor(float64(left) / float64(len(open))))
		if share < 1 {
			break
		}
		if i := open[0]; need[i]-extra[i] <= share {
			left -= need[i] - extra[i]
			extra[i] = need[i]
			open = open[1:]
			continue
		}
		for _, i := range open { // every need is larger than the share: each takes it
			extra[i] += share
		}
		left -= share * float32(len(open))
	}
	return extra
}
