package engine

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The Velero page's schedules and the namespaces outside them (velero.go).

// Velero's schedule phases.
const (
	schedEnabled          = "Enabled"
	schedFailedValidation = "FailedValidation"
)

// veleroSched is one schedule as the page reads it.
type veleroSched struct {
	name      string
	scope     veleroScope
	paused    bool
	phase     string
	invalid   []string // status.validationErrors
	inPage    bool     // its scope touches the page's namespaces
	tileState string   // its line in the Schedules tile
	attention []VeleroAttention
	row       VeleroSchedule
}

// covering reports whether the schedule makes backups: neither paused nor failing validation.
func (sc *veleroSched) covering() bool { return !sc.paused && sc.phase != schedFailedValidation }

// schedules reads every schedule with its backups, fills the Schedules table and returns them all
// (the tiles, Needs attention and Outside every schedule read them too).
func (mb *veleroModeler) schedules() []*veleroSched {
	bySched := map[string][]*unstructured.Unstructured{}
	for _, o := range mb.objs(VeleroBackupsKey) {
		if name := o.GetLabels()[veleroScheduleLabel]; name != "" {
			bySched[name] = append(bySched[name], o)
		}
	}
	location := mb.server.location
	for _, o := range mb.objs(VeleroLocationsKey) {
		if d, _ := model.NestedBool(o.Object, "spec", "default"); d {
			location = o.GetName()
			break
		}
	}
	out := make([]*veleroSched, 0, len(mb.objs(VeleroSchedulesKey)))
	for _, o := range mb.objs(VeleroSchedulesKey) {
		sc := mb.schedule(o, bySched[o.GetName()], location)
		out = append(out, sc)
		if sc.inPage {
			mb.m.Schedules = append(mb.m.Schedules, sc.row)
		}
	}
	rank := func(r VeleroSchedule) int {
		switch {
		case r.Status == model.StatusError:
			return 0
		case r.Status == model.StatusWarning:
			return 1
		case r.State == lblPaused:
			return 3
		}
		return 2
	}
	slices.SortStableFunc(mb.m.Schedules, func(a, b VeleroSchedule) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.Ref.Name, b.Ref.Name))
	})
	return out
}

// schedule reads one schedule and its backups; location is where a backup that names none goes.
func (mb *veleroModeler) schedule(o *unstructured.Unstructured, backups []*unstructured.Unstructured, location string) *veleroSched {
	spec := model.NestedMap(o.Object, "spec")
	tmpl := model.NestedMap(spec, "template")
	paused, _ := model.NestedBool(spec, "paused")
	sc := &veleroSched{name: o.GetName(), scope: veleroScopeOf(tmpl), paused: paused, phase: model.NestedString(o.Object, "status", "phase"),
		invalid: model.NestedStrings(o.Object, "status", "validationErrors")}
	sc.inPage = sc.scope.touches(mb.nsSet)
	c := parseVeleroCron(model.NestedString(spec, "schedule"), mb.server.loc)
	runs := mb.backupRuns(sc.name, backups)
	base := model.NestedTime(o.Object, "status", "lastBackup")
	if base.IsZero() {
		base = o.GetCreationTimestamp().Time
	}
	skipped := model.NestedTime(o.Object, "status", "lastSkipped")
	if skipped.After(base) {
		base = skipped
	}
	enabled := !paused && sc.phase == schedEnabled
	trailing := base
	if !enabled {
		trailing = mb.now // a paused or failing schedule is due nowhere
	}
	row := VeleroSchedule{Ref: mb.ref(VeleroSchedulesKey, o), Cron: model.NestedString(spec, "schedule"),
		Runs: scheduleRuns(c, runs, trailing, skipped, mb.now), Scope: sc.scope.text(), Location: location}
	if l := model.NestedString(tmpl, "storageLocation"); l != "" {
		row.Location = l
	}
	row.TTL = veleroTTL(mb.server.ttl)
	if d, err := time.ParseDuration(model.NestedString(tmpl, "ttl")); err == nil && d > 0 {
		row.TTL = veleroTTL(d)
	}
	if !paused && sc.phase != schedFailedValidation {
		row.NextRun = c.next(base)
		if !row.NextRun.IsZero() && !row.NextRun.After(mb.now) {
			row.NextRun = c.next(mb.now)
		}
	}
	row.When = c.words
	if namesTimeOfDay(c.words) { // "daily 02:00 UTC"; "hourly :05" and "every 6 h" are the same in every zone
		row.When += c.zoneNote(mb.tf.Zone, mb.now)
	}
	mb.lastCompleted(&row, backups)
	var due []time.Time
	if enabled {
		due = c.fires(base, mb.now.Add(-veleroGrace), 1)
	}
	mb.scheduleState(sc, &row, c, runs, due)
	sc.row = row
	return sc
}

// backupRuns are a schedule's backups as runs, oldest first.
func (mb *veleroModeler) backupRuns(schedule string, backups []*unstructured.Unstructured) []backupRun {
	runs := make([]backupRun, 0, len(backups))
	for _, b := range backups {
		runs = append(runs, backupRun{name: b.GetName(), phase: model.NestedString(b.Object, "status", "phase"),
			at: runTimeOf(b.GetName(), schedule, model.NestedTime(b.Object, "status", "startTimestamp"), b.GetCreationTimestamp().Time, mb.server.loc)})
	}
	slices.SortStableFunc(runs, func(a, b backupRun) int { return cmp.Or(a.at.Compare(b.at), cmp.Compare(a.name, b.name)) })
	return runs
}

// lastCompleted fills when the newest Completed backup finished and how long it ran.
func (mb *veleroModeler) lastCompleted(row *VeleroSchedule, backups []*unstructured.Unstructured) {
	for _, b := range backups {
		if model.NestedString(b.Object, "status", "phase") != phaseCompleted {
			continue
		}
		done := model.NestedTime(b.Object, "status", "completionTimestamp")
		if done.IsZero() || !done.After(row.LastCompleted) {
			continue
		}
		row.LastCompleted = done
		row.Duration = veleroDuration(done.Sub(model.NestedTime(b.Object, "status", "startTimestamp")))
	}
}

// scheduleState sets a schedule's state, its tile line and its rows in Needs attention: failing
// validation, paused, overdue (due is the run due and not created), its latest finished backup
// failed or partly failed, new, enabled; the first that holds is its state.
func (mb *veleroModeler) scheduleState(sc *veleroSched, row *VeleroSchedule, c veleroCron, runs []backupRun, due []time.Time) {
	obj := "Schedule " + sc.name
	var latest *backupRun
	var blocker *backupRun
	for i := len(runs) - 1; i >= 0; i-- {
		r := &runs[i]
		if latest == nil && finished(runState(r.phase)) {
			latest = r
		}
		if blocker == nil && blocksSchedule(r.phase) {
			blocker = r
		}
	}
	if sc.phase == schedFailedValidation {
		sc.attention = append(sc.attention, VeleroAttention{Problem: lblFailedValidation, Status: model.StatusError, Ref: row.Ref,
			Object: obj, Scope: row.Scope, Detail: strings.Join(sc.invalid, "; ")})
	}
	if len(due) > 0 {
		detail := "due " + mb.dueText(c, due[0]) + ", no backup created"
		if blocker != nil {
			phase := blocker.phase
			if phase == "" {
				phase = phaseNew
			}
			detail += "; " + blocker.name + " still " + phase
		}
		sc.attention = append(sc.attention, VeleroAttention{Problem: lblOverdue, Status: model.StatusError, Ref: row.Ref,
			Object: obj, Scope: row.Scope, Since: due[0], Detail: detail})
	}
	var latestProblem string
	var latestStatus model.Status
	if latest != nil {
		latestProblem, latestStatus = backupProblem(runState(latest.phase))
		if latestProblem != "" {
			if b := mb.backupObject(latest.name); b != nil {
				sc.attention = append(sc.attention, mb.backupRow(b, latestProblem, latestStatus))
			}
		}
	}
	switch {
	case sc.phase == schedFailedValidation:
		row.State, row.Status, sc.tileState = lblFailedValidation, model.StatusError, lblFailedValidation
	case sc.paused:
		row.State, row.Status, sc.tileState = lblPaused, model.StatusPending, lblPaused
	case len(due) > 0:
		row.State, row.Status, sc.tileState = lblOverdue, model.StatusError, lblOverdue
	case latestProblem != "":
		row.State, row.Status, sc.tileState = latestProblem, latestStatus, lblEnabled
	case sc.phase != schedEnabled:
		row.State, row.Status, sc.tileState = lblNew, model.StatusPending, lblNew
	default:
		row.State, row.Status, sc.tileState = lblEnabled, model.StatusOK, lblEnabled
	}
}

// namesTimeOfDay reports whether a cron's words name a time of day ("02:00").
func namesTimeOfDay(words string) bool {
	for i := 0; i+4 < len(words); i++ {
		if isDigit(words[i]) && isDigit(words[i+1]) && words[i+2] == ':' && isDigit(words[i+3]) && isDigit(words[i+4]) {
			return true
		}
	}
	return false
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// dueText writes when a run was due: its time of day within a day, else its day and time, in the
// user's format (D-31) and in the cron's zone, as When writes the cron, named where the user's zone
// shows another time.
func (mb *veleroModeler) dueText(c veleroCron, t time.Time) string {
	tf := model.TimeFormat{Layout: mb.tf.Layout, Zone: c.loc}
	text := tf.FormatClock(t)
	if mb.now.Sub(t) >= 24*time.Hour {
		text = tf.FormatDay(t) + " " + text
	}
	return text + c.zoneNote(mb.tf.Zone, t)
}

// backupObject is a backup by name in Velero's namespace, or nil.
func (mb *veleroModeler) backupObject(name string) *unstructured.Unstructured {
	for _, o := range mb.objs(VeleroBackupsKey) {
		if o.GetName() == name {
			return o
		}
	}
	return nil
}

// nsContents counts what a namespace holds and which GitOps tools manage it.
type nsContents struct {
	pvcs, workloads, managed int
	flux, argo               bool
}

// contents counts each namespace's PVCs and workloads and reads their GitOps marks.
func (mb *veleroModeler) contents() map[string]*nsContents {
	out := map[string]*nsContents{}
	at := func(ns string) *nsContents {
		c := out[ns]
		if c == nil {
			c = &nsContents{}
			out[ns] = c
		}
		return c
	}
	for _, o := range mb.objs(kindPVCs) {
		c := at(o.GetNamespace())
		c.pvcs++
		f, a := gitOpsTools(o)
		c.flux, c.argo = c.flux || f, c.argo || a
	}
	for _, key := range []string{logKindDeployments, kindStatefulSets, logKindDaemonSets} {
		for _, o := range mb.objs(key) {
			c := at(o.GetNamespace())
			c.workloads++
			f, a := gitOpsTools(o)
			c.flux, c.argo = c.flux || f, c.argo || a
			if f || a {
				c.managed++
			}
		}
	}
	return out
}

// outside fills Outside every schedule: each active namespace of the page's that no schedule that
// makes backups takes in full, with why. A namespace labelled out of backups keeps its objects in
// scope (veleroExcludeLabel): it is partly in scope.
func (mb *veleroModeler) outside(scheds []*veleroSched) {
	if !mb.vb.read[kindNamespaces] {
		mb.m.OutsideError = "Cogpit cannot tell which namespaces are outside every schedule: it may not list namespaces."
		return
	}
	contents := mb.contents()
	for _, nsObj := range mb.objs(kindNamespaces) {
		ns := nsObj.GetName()
		if !mb.inNamespaces(ns) || model.NestedString(nsObj.Object, "status", "phase") == "Terminating" {
			continue // Velero backs up active namespaces only
		}
		partly, reason, ok := namespaceCoverage(ns, excludedByLabel(nsObj), scheds)
		if ok {
			continue
		}
		c := contents[ns]
		if c == nil {
			c = &nsContents{}
		}
		f, a := gitOpsTools(nsObj)
		row := VeleroOutside{Ref: mb.ref(kindNamespaces, nsObj), PVCs: c.pvcs, Workloads: c.workloads, Partly: partly,
			ManagedBy: managedBy(c.flux || f, c.argo || a), Reason: reason}
		switch {
		case row.ManagedBy != "" && c.pvcs > 0:
			row.Reason += " · Git holds no volume data"
		case row.ManagedBy != "" && c.workloads > 0 && c.managed == c.workloads:
			row.Reason += " · no volumes; rebuilt from Git"
		}
		mb.m.Outside = append(mb.m.Outside, row)
	}
	slices.SortStableFunc(mb.m.Outside, func(a, b VeleroOutside) int {
		return cmp.Or(boolFirst(a.PVCs > 0, b.PVCs > 0), boolFirst(!a.Partly, !b.Partly), boolFirst(a.ManagedBy == "", b.ManagedBy == ""),
			cmp.Compare(a.Ref.Name, b.Ref.Name))
	})
}

// boolFirst orders true before false.
func boolFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

// managedBy names the GitOps tools.
func managedBy(flux, argo bool) string {
	switch {
	case flux && argo:
		return veleroFluxCD + ", " + veleroArgoCD
	case flux:
		return veleroFluxCD
	case argo:
		return veleroArgoCD
	}
	return ""
}

// namespaceCoverage decides a namespace against the schedules: ok when one that makes backups takes
// it in full; else whether one takes in part of it, and why it is not in full.
func namespaceCoverage(ns string, labelledOut bool, scheds []*veleroSched) (partly bool, reason string, ok bool) {
	var full, partial, excluding, idle []*veleroSched
	for _, sc := range scheds {
		switch {
		case !sc.covering():
			if sc.scope.covers(ns) {
				idle = append(idle, sc)
			}
		case sc.scope.covers(ns) && !sc.scope.partial():
			full = append(full, sc)
		case sc.scope.covers(ns):
			partial = append(partial, sc)
		case sc.scope.excludes(ns):
			excluding = append(excluding, sc)
		}
	}
	sortScheds := func(l []*veleroSched) {
		slices.SortFunc(l, func(a, b *veleroSched) int { return cmp.Compare(a.name, b.name) })
	}
	for _, l := range [][]*veleroSched{full, partial, excluding, idle} {
		sortScheds(l)
	}
	switch {
	case len(full) > 0 && !labelledOut:
		return false, "", true
	case len(full) > 0:
		return true, "partly in scope: its Namespace object carries " + veleroExcludeLabel + "; the rest is in scope of " + full[0].name, false
	case len(partial) > 0:
		return true, "partly in scope: " + partial[0].name + " " + partial[0].scope.narrowing(), false
	case len(idle) > 0 && idle[0].paused:
		return false, "in scope of " + idle[0].name + " only, which is paused", false
	case len(idle) > 0:
		return false, "in scope of " + idle[0].name + " only, which fails validation", false
	case len(excluding) > 0:
		return false, "excluded from " + schedNames(excluding) + ", no other schedule names it", false
	case len(scheds) == 0:
		return false, "no schedule exists", false
	}
	return false, "no schedule names it", false
}

// schedNames writes up to two schedules' names, "a, b +1".
func schedNames(l []*veleroSched) string {
	names := make([]string, 0, min(2, len(l)))
	for _, sc := range l[:min(2, len(l))] {
		names = append(names, sc.name)
	}
	out := strings.Join(names, ", ")
	if len(l) > 2 {
		out += " +" + strconv.Itoa(len(l)-2)
	}
	return out
}
