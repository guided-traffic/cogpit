package engine

import (
	"context"
	"slices"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The integration overlay (D-79; architecture › "Integrations", "The integration overlay"): an
// integration that marks objects (Kyverno's policy violations; Trivy's image ratings next) adds
// its cells to the rows the kind profiles build and its sections to the drawer, without a kind
// profile knowing it, and only while it is on. Each integration's follower puts its overlay on the
// backend while the integration is on and takes it off when it is switched off (overlays.put); the
// overlay reads its own data from the cache, and tells the backend when that data changed
// (overlays.changed), which wakes the open view and the drawer.
//
//   - Columns: an overlay names, per kind, the columns it gives the kind's rows (rowOverlay.columns).
//     A column the kind has (by id: the warning column of Pods, Nodes, Cron Jobs, …) gets the
//     overlay's cell merged into the kind's own (mergeCell: the reasons joined, the worse status);
//     a column the kind lacks is added after the kind's columns in the snapshot (the rows keep
//     their indexes) and shown after the column the overlay names (overlayColumn.After), unless the
//     user arranged it elsewhere. A column only shows while an overlay gives it to the kind: off,
//     it is gone, as hidden metrics' columns are (hidemetrics.go).
//   - Rows: the view's row cache makes a row's overlaid copy once per change of the row or of the
//     overlays' generation (snapshot.go, rowEntry.ov); a row the overlays leave alone is shared as
//     the profile built it. Search, sort, the CSV export and view.rows see the overlaid row, as the
//     window does; resources.list too (BuildRowsShown).
//   - Drawer: an overlay's sections follow the kind's own (rowOverlay.sections), and an overlay that
//     adds to the kind's own sections does so through sectionAmender (Trivy's grade in each
//     container's section), in the GUI's drawer and in object_get alike.
//   - Off: no overlay on the backend, so no column, no cell, no section; and the integration's
//     follower released its reads, so nothing is asked of the cluster.

// rowOverlay is one integration's part of the rows and the drawer of a cluster's objects. Its
// methods read the overlay's own data (never the cluster) and are called concurrently.
type rowOverlay interface {
	// columns are the columns the overlay gives a kind's rows; none for a kind it leaves alone.
	columns(p *model.KindProfile) []overlayColumn
	// cell is the overlay's cell of an object in one of its columns; ok is false for none.
	cell(p *model.KindProfile, column string, obj *unstructured.Unstructured) (model.Cell, bool)
	// sections are the overlay's drawer sections of an object, after the kind's own; nil for none.
	sections(p *model.KindProfile, obj *unstructured.Unstructured, f model.TimeFormat) []model.DetailSection
	// waitSynced waits, bounded by ctx, until the overlay's reads have listed: a read that waits for
	// the cache anyway (an agent's object_get) sees what a connect just started to read.
	waitSynced(ctx context.Context)
	// stop ends the overlay's reads: it was switched off, or its backend closes.
	stop()
}

// sectionAmender is an overlay that also adds to the kind's own drawer sections: Trivy's image grade
// in each container section of a pod (trivy.go, D-new). amend returns the sections with its
// additions, copying what it changes and never changing the input; it reads the overlay's own data,
// never the cluster.
type sectionAmender interface {
	amend(p *model.KindProfile, obj *unstructured.Unstructured, sections []model.DetailSection, f model.TimeFormat) []model.DetailSection
}

// overlayColumn is a column an overlay gives a kind.
type overlayColumn struct {
	model.Column
	// After is the id of the column an added column shows right after ("name"); empty: last.
	After string
}

// overlays are a backend's overlays, one per integration that is on and marks objects, and
// Cogpit's own after them: the version check of the Nodes list (k8sversion.go, D-91).
type overlays struct {
	mu     sync.Mutex
	by     map[Integration]rowOverlay
	own    rowOverlay // Cogpit's own overlay; nil for none
	gen    uint64     // counts every change of the set or of an overlay's data
	subs   map[uint64]func()
	seq    uint64
	closed bool
}

// put sets an integration's overlay (nil takes it off); the one it replaces is stopped.
func (o *overlays) put(name Integration, ov rowOverlay) {
	o.mu.Lock()
	if o.closed && ov != nil {
		o.mu.Unlock()
		ov.stop()
		return
	}
	old := o.by[name]
	if ov == nil {
		delete(o.by, name)
	} else {
		if o.by == nil {
			o.by = map[Integration]rowOverlay{}
		}
		o.by[name] = ov
	}
	o.mu.Unlock()
	if old != nil && old != ov {
		old.stop()
	}
	o.changed()
}

// overlayOf returns a backend's overlay of type T (an integration's own type), if one is on.
func overlayOf[T rowOverlay](b *Backend) (T, bool) {
	list, _ := b.overlays.current()
	for _, ov := range list {
		if t, ok := ov.(T); ok {
			return t, true
		}
	}
	var none T
	return none, false
}

// changed tells the open view and the drawer that what the overlays show changed.
func (o *overlays) changed() {
	o.mu.Lock()
	o.gen++
	subs := make([]func(), 0, len(o.subs))
	for _, fn := range o.subs {
		subs = append(subs, fn)
	}
	o.mu.Unlock()
	for _, fn := range subs {
		fn()
	}
}

// current returns the overlays in Integrations' order and their generation.
func (o *overlays) current() ([]rowOverlay, uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.by) == 0 && o.own == nil {
		return nil, o.gen
	}
	out := make([]rowOverlay, 0, len(o.by)+1)
	for _, in := range Integrations {
		if ov := o.by[in.Name]; ov != nil {
			out = append(out, ov)
		}
	}
	if o.own != nil {
		out = append(out, o.own)
	}
	return out, o.gen
}

// putOwn sets Cogpit's own overlay (nil takes it off); the one it replaces is stopped.
func (o *overlays) putOwn(ov rowOverlay) {
	o.mu.Lock()
	if o.closed && ov != nil {
		o.mu.Unlock()
		ov.stop()
		return
	}
	old := o.own
	o.own = ov
	o.mu.Unlock()
	if old != nil && old != ov {
		old.stop()
	}
	o.changed()
}

// subscribe has fn called after every change; it must not block (a signal). The returned
// function ends it.
func (o *overlays) subscribe(fn func()) func() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	id := o.seq
	if o.subs == nil {
		o.subs = map[uint64]func(){}
	}
	o.subs[id] = fn
	return func() {
		o.mu.Lock()
		delete(o.subs, id)
		o.mu.Unlock()
	}
}

// close stops every overlay: the backend closes.
func (o *overlays) close() {
	o.mu.Lock()
	o.closed = true
	all := o.by
	own := o.own
	o.by, o.own = nil, nil
	o.mu.Unlock()
	for _, ov := range all {
		ov.stop()
	}
	if own != nil {
		own.stop()
	}
}

// overlayLayout is how the overlays meet one kind's rows: the snapshot's columns (the kind's, then
// those the overlays add) and where each overlay's cells go.
type overlayLayout struct {
	cols  []model.Column
	added map[int]string // index of an added column in cols → the id it shows after
	parts []overlayPart
	gen   uint64
	width int // the cells an overlaid row has at least
}

type overlayPart struct {
	ov    rowOverlay
	id    string
	index int
}

// layoutOverlays returns how the overlays meet a kind's rows, nil when none gives it a column.
func layoutOverlays(p *model.KindProfile, list []rowOverlay, gen uint64) *overlayLayout {
	if p == nil || len(list) == 0 {
		return nil
	}
	var l *overlayLayout
	for _, ov := range list {
		for _, c := range ov.columns(p) {
			if l == nil {
				l = &overlayLayout{cols: p.Columns, gen: gen}
			}
			i := slices.IndexFunc(l.cols, func(k model.Column) bool { return k.ID == c.ID })
			if i < 0 { // the kind lacks it: added after its columns, shown after c.After
				if len(l.cols) == len(p.Columns) {
					l.cols = slices.Clone(p.Columns)
				}
				l.cols = append(l.cols, c.Column)
				i = len(l.cols) - 1
				if l.added == nil {
					l.added = map[int]string{}
				}
				l.added[i] = c.After
			}
			l.parts = append(l.parts, overlayPart{ov: ov, id: c.ID, index: i})
			l.width = max(l.width, i+1)
		}
	}
	return l
}

// columns returns the snapshot's columns: the kind's, then the added ones.
func (l *overlayLayout) columns(p *model.KindProfile) []model.Column {
	if l == nil {
		return p.Columns
	}
	return l.cols
}

// apply returns a row with the overlays' cells, and whether any overlay had one for the object.
// The row passed is never changed: an overlaid row has cells of its own.
func (l *overlayLayout) apply(p *model.KindProfile, row model.Row, obj *unstructured.Unstructured) (model.Row, bool) {
	if l == nil {
		return row, false
	}
	var cells []model.Cell
	for _, part := range l.parts {
		c, ok := part.ov.cell(p, part.id, obj)
		if !ok {
			continue
		}
		if cells == nil {
			cells = make([]model.Cell, max(len(row.Cells), l.width))
			copy(cells, row.Cells)
		}
		cells[part.index] = mergeCell(cells[part.index], c)
	}
	if cells == nil {
		return row, false
	}
	row.Cells = cells
	return row, true
}

// order returns the order the table shows the columns in (TableSnapshot.Order): the user's
// arrangement, with each added column right after the column its overlay names unless the user
// placed it; nil for the columns' own order.
func (l *overlayLayout) order(cols []model.Column, ids []string) []int {
	base := ColumnOrder(cols, ids)
	if l == nil || len(l.added) == 0 {
		return base
	}
	if base == nil {
		base = make([]int, len(cols))
		for i := range base {
			base[i] = i
		}
	}
	added := make([]int, 0, len(l.added))
	for i := range l.added {
		added = append(added, i)
	}
	slices.Sort(added)
	for _, i := range added {
		after := l.added[i]
		if after == "" || slices.Contains(ids, cols[i].ID) {
			continue
		}
		at := slices.IndexFunc(base, func(ci int) bool { return cols[ci].ID == after })
		if at < 0 {
			continue
		}
		base = slices.DeleteFunc(base, func(ci int) bool { return ci == i })
		at = slices.IndexFunc(base, func(ci int) bool { return cols[ci].ID == after })
		base = slices.Insert(base, at+1, i)
	}
	for i, ci := range base {
		if i != ci {
			return base
		}
	}
	return nil
}

// mergeCell merges an overlay's cell into the kind's own cell of the same column: an empty cell
// takes the overlay's; otherwise the texts are joined ("Containers not ready: web; Kyverno: 1 policy
// violation"), the worse status and the larger sort key kept, and the tooltips joined a line each.
// An overlay's cell in coloured parts redraws the cell: its text and parts replace the kind's (a
// kubelet's version with a ⚠ after it, k8sversion.go).
func mergeCell(base, add model.Cell) model.Cell {
	if base.Text == "" && base.Num == 0 && base.Status == model.StatusNone {
		return add
	}
	out := base
	switch {
	case add.Parts != nil:
		out.Text, out.Parts = add.Text, add.Parts
	case add.Text != "":
		out.Text = strings.TrimPrefix(base.Text+"; "+add.Text, "; ")
	}
	if add.Tip != "" {
		out.Tip = strings.TrimPrefix(base.Tip+"\n"+add.Tip, "\n")
	}
	if statusRank(add.Status) > statusRank(base.Status) {
		out.Status = add.Status
	}
	out.Num = max(base.Num, add.Num)
	return out
}

// statusRank orders the statuses a merged cell keeps the worse of.
func statusRank(s model.Status) int {
	switch s {
	case model.StatusError:
		return 3
	case model.StatusWarning:
		return 2
	case model.StatusNone:
		return 0
	}
	return 1
}

// waitSynced waits, bounded by ctx and syncTimeout, until every overlay's reads have listed.
func (o *overlays) waitSynced(ctx context.Context) {
	list, _ := o.current()
	if len(list) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	for _, ov := range list {
		ov.waitSynced(ctx)
	}
}

// sections returns the overlays' drawer sections of an object.
func (o *overlays) sections(p *model.KindProfile, obj *unstructured.Unstructured, f model.TimeFormat) []model.DetailSection {
	list, _ := o.current()
	out := make([]model.DetailSection, 0, len(list))
	for _, ov := range list {
		out = append(out, ov.sections(p, obj, f)...)
	}
	return out
}

// amend returns a drawer's sections with what the overlays add to the kind's own (sectionAmender),
// in the overlays' order.
func (o *overlays) amend(p *model.KindProfile, obj *unstructured.Unstructured, sections []model.DetailSection, f model.TimeFormat) []model.DetailSection {
	list, _ := o.current()
	for _, ov := range list {
		if a, ok := ov.(sectionAmender); ok {
			sections = a.amend(p, obj, sections, f)
		}
	}
	return sections
}

// ViewColumns returns the columns the view of a kind has on the cluster the window shows: the
// kind's, then the columns the integrations' overlays add to it (a Kyverno warning column on
// Deployments). ui.sort, ui.setColumns and ui.setColumnOrder take their ids.
func (s *Session) ViewColumns(p *model.KindProfile) []model.Column {
	if p == nil {
		return nil
	}
	b := s.Backend()
	if b == nil {
		return p.Columns
	}
	list, gen := b.overlays.current()
	return layoutOverlays(p, list, gen).columns(p)
}

// WithViewColumns returns a kind's profile with the columns its view has on the cluster the window
// shows (ViewColumns): itself when no overlay adds one, else a copy. Commands that check a column id
// against the open view (ui.sort, ui.setColumns, ui.setColumnOrder) read it.
func (s *Session) WithViewColumns(p *model.KindProfile) *model.KindProfile {
	cols := s.ViewColumns(p)
	if p == nil || len(cols) == len(p.Columns) {
		return p
	}
	at := *p
	at.Columns = cols
	return &at
}

// hasViewColumn reports whether a kind's view on the backend has a column: the kind's, or one an
// overlay adds (a sort by it is remembered as by the kind's own).
func (b *Backend) hasViewColumn(p *model.KindProfile, id string) bool {
	if p == nil {
		return false
	}
	if p.ColumnIndex(id) >= 0 {
		return true
	}
	if b == nil {
		return false
	}
	list, gen := b.overlays.current()
	return slices.ContainsFunc(layoutOverlays(p, list, gen).columns(p), func(c model.Column) bool { return c.ID == id })
}

// BuildRowsShown is BuildRows with the integrations' overlays (overlay.go): the rows as the view
// shows them, and the columns they fit (the kind's, then the added ones).
func (s *Session) BuildRowsShown(ctx context.Context, b *Backend, p *model.KindProfile, objs []*unstructured.Unstructured) ([]model.Column, []model.Row) {
	rows := s.BuildRows(ctx, b, p, objs)
	list, gen := b.overlays.current()
	l := layoutOverlays(p, list, gen)
	if l == nil {
		return p.Columns, rows
	}
	for i := range rows {
		if i < len(objs) {
			rows[i], _ = l.apply(p, rows[i], objs[i])
		}
	}
	return l.columns(p), rows
}
