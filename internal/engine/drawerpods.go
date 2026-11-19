package engine

import (
	"slices"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The CPU and Memory of the pods a drawer lists (owner: a node's drawer lists its pods with CPU and
// Memory, which came from the Metrics Server nowhere; architecture §9). A drawer's pod table is a
// sortable table of cells (model.SortableCellTable) whose rows link to pods and that has the Pods
// list's metric columns (ids model.ColumnCPU and model.ColumnMemory, type model.ColumnMetric): a
// node's Pods. Its CPU and Memory cells are filled the way the Pods list fills its own: from the
// metrics hub's latest measurement of each pod (Hub.Pod), through the Pods profile's UsageCells, so
// the texts and sort keys are the list's. Nothing here sends a request.
//
// The open drawer polls those pods' metrics while it shows such a table: the first model built
// for it with one subscribes the drawer to the pods' list scopes, the object's namespace for a
// namespaced object, else (a node) the namespaces the Pods view reads pods in: every namespace at
// once, or one list per namespace for a user who may not list pods everywhere (D-39); the Pods
// list polls the same scopes, so with both open there is one poll of each. A model without such a
// table releases them, and so does the drawer when it closes or shows another object. Each poll
// wakes the drawer (EventDetail). The cluster's settings apply as they do to the Pods list: a
// hidden CPU or Memory column (Cluster Settings › Metrics: Pods, CPU, Memory) is left out of the
// table, and with both hidden nothing is polled.

// podTableScopes are the metrics scopes that cover the pods a drawer's table lists: the drawer
// object's namespace, or for a cluster-scoped object the namespaces the pods are read in (none
// while that is undecided: the drawer starts again once it is, drawerPlanKey).
func (d *drawerWatch) podTableScopes() []metrics.Scope {
	if ns := d.namespace(); ns != "" {
		return []metrics.Scope{{Kind: metrics.KindPods, Namespace: ns}}
	}
	pp, ok := d.backend.Profile(logKindPods)
	if !ok {
		return []metrics.Scope{{Kind: metrics.KindPods}}
	}
	return usageListScopes(model.UsagePods, d.backend.namespacePlan(pp))
}

// isPodTable reports whether a drawer table lists pods with the Pods list's metric columns.
func isPodTable(t *model.DetailTable) bool {
	if t == nil || len(t.Cells) == 0 {
		return false
	}
	metric := false
	for _, id := range []string{model.ColumnCPU, model.ColumnMemory} {
		if i := t.ColumnIndex(id); i >= 0 && i < len(t.Types) && t.Types[i] == model.ColumnMetric {
			metric = true
		}
	}
	return metric && podOfRow(t, 0) != nil
}

// podOfRow returns the pod a row of a pod table stands for: the first of its cells that links to a
// pod (its Name).
func podOfRow(t *model.DetailTable, row int) *model.ObjectRef {
	if row < 0 || row >= len(t.Cells) {
		return nil
	}
	for _, c := range t.Cells[row] {
		if c.Link != nil && c.Link.Kind == logKindPods && c.Link.Name != "" {
			return c.Link
		}
	}
	return nil
}

// drawerPods fills the CPU and Memory of the model's pod tables from the metrics the hub holds, and
// leaves out the columns the context's settings hide. While the open drawer shows ref it starts its
// poll of those pods' metrics when one shows, and stops it when none does.
func (s *Session) drawerPods(b *Backend, ref model.ObjectRef, m *DetailModel) {
	hidden := s.hiddenMetrics(b.Context)
	cpu, memory := hidden.listColumns(model.UsagePods)
	shows := false
	for i := range m.Sections {
		t := m.Sections[i].Table
		if !isPodTable(t) {
			continue
		}
		t = withoutMetricColumns(t, !cpu, !memory)
		if cpu || memory {
			shows = true
			fillPodUsage(b, t)
		}
		m.Sections[i].Table = t
	}
	s.followDrawerPods(b, ref, shows)
}

// withoutMetricColumns returns the table without its CPU column (cpu) and its Memory column
// (memory); the table itself when it keeps both. The table returned is a copy whose rows the
// caller may change.
func withoutMetricColumns(t *model.DetailTable, cpu, memory bool) *model.DetailTable {
	drop := map[int]bool{}
	if i := t.ColumnIndex(model.ColumnCPU); cpu && i >= 0 {
		drop[i] = true
	}
	if i := t.ColumnIndex(model.ColumnMemory); memory && i >= 0 {
		drop[i] = true
	}
	keep := func(i int) bool { return !drop[i] }
	nt := *t
	nt.Columns = pick(t.Columns, keep)
	nt.Types = pick(t.Types, keep)
	nt.IDs = pick(t.IDs, keep)
	nt.Rows = make([][]string, len(t.Rows))
	for r, row := range t.Rows {
		nt.Rows[r] = pick(row, keep)
	}
	nt.Cells = make([][]model.Cell, len(t.Cells))
	for r, row := range t.Cells {
		nt.Cells[r] = pick(row, keep)
	}
	if nt.Sort != nil && slices.Index(nt.IDs, nt.Sort.Column) < 0 {
		nt.Sort = nil
	}
	return &nt
}

// pick returns a copy of the elements of s whose index keep accepts.
func pick[T any](s []T, keep func(int) bool) []T {
	if s == nil {
		return nil
	}
	out := make([]T, 0, len(s))
	for i, v := range s {
		if keep(i) {
			out = append(out, v)
		}
	}
	return out
}

// fillPodUsage writes each listed pod's CPU and Memory into its cells and texts: the hub's latest
// fresh measurement through the Pods profile's UsageCells, the Pods list's texts and sort keys. A
// pod without a measurement keeps empty cells, as in the list.
func fillPodUsage(b *Backend, t *model.DetailTable) {
	if b.Metrics == nil {
		return
	}
	pp, ok := b.Profile(logKindPods)
	if !ok || pp.UsageCells == nil {
		return
	}
	FillPodTable(t, func(namespace, name string) (model.Usage, bool) {
		pu, ok := b.Metrics.Pod(namespace, name)
		return pu.Total(), ok
	}, func(u model.Usage) (model.Cell, model.Cell) { return usageCellsOf(pp, u) })
}

// FillPodTable writes the CPU and Memory of each pod a pod table lists into its cells and texts:
// usage says what a pod uses (false: no measurement, the cells stay empty), cells turns it into the
// two cells (a profile's UsageCells). The table's rows are changed in place: give it a copy. The
// engine fills the drawer's tables with it from the metrics hub; the window's tests with their own
// numbers.
func FillPodTable(t *model.DetailTable, usage func(namespace, name string) (model.Usage, bool), cells func(model.Usage) (cpu, memory model.Cell)) {
	cpuCol, memCol := t.ColumnIndex(model.ColumnCPU), t.ColumnIndex(model.ColumnMemory)
	if cpuCol < 0 && memCol < 0 {
		return
	}
	for r := range t.Cells {
		pod := podOfRow(t, r)
		if pod == nil {
			continue
		}
		u, ok := usage(pod.Namespace, pod.Name)
		if !ok {
			continue
		}
		cpuCell, memCell := cells(u)
		set := func(col int, c model.Cell) {
			if col >= 0 && col < len(t.Cells[r]) {
				t.Cells[r][col] = c
			}
			if col >= 0 && r < len(t.Rows) && col < len(t.Rows[r]) {
				t.Rows[r][col] = c.Text
			}
		}
		set(cpuCol, cpuCell)
		set(memCol, memCell)
	}
}

// usageCellsOf runs a profile's UsageCells for a pod table's row, which has no object at hand: the
// Pods profile's cells read the usage only. A panic leaves the cells empty.
func usageCellsOf(p *model.KindProfile, u model.Usage) (cpu, memory model.Cell) {
	defer func() {
		if recover() != nil {
			cpu, memory = model.Cell{}, model.Cell{}
		}
	}()
	return p.UsageCells(nil, u)
}

// followDrawerPods starts or stops the open drawer's poll of its pod table's metrics, when the
// drawer shows ref on b.
func (s *Session) followDrawerPods(b *Backend, ref model.ObjectRef, shows bool) {
	st := s.state
	st.mu.Lock()
	d := st.drawer
	if d == nil || d.backend != b || !d.ref.Same(ref) {
		st.mu.Unlock()
		return
	}
	st.mu.Unlock()
	d.showPods(shows)
}

// showPods subscribes the drawer to its pod table's metrics (shows) or releases that subscription.
// A stopped drawer subscribes to nothing: stop releases what showPods made before it.
func (d *drawerWatch) showPods(shows bool) {
	hub := d.backend.Metrics
	if hub == nil {
		return
	}
	d.wmu.Lock()
	if d.stopped.Load() {
		d.wmu.Unlock()
		return
	}
	cur := d.pods
	switch {
	case shows && cur == nil:
		d.pods = subscribeAll(hub, d.podTableScopes(), func() { signal(d.wake) })
	case !shows && cur != nil:
		d.pods = nil
	}
	d.wmu.Unlock()
	if !shows && cur != nil {
		releaseAll(cur)
	}
}

// podsSubs returns the drawer's polls of its pod table's metrics; nil while none runs.
func (d *drawerWatch) podsSubs() []*metrics.Subscription {
	d.wmu.Lock()
	defer d.wmu.Unlock()
	return d.pods
}
