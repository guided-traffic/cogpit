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

// The Velero page's model from one reading of its kinds (velero.go): the header, the tiles, Needs
// attention, the schedules and the namespaces outside every schedule.

// veleroManualWindow is how long a failed backup that no schedule made stays in Needs attention:
// a week, unless a later backup of the same scope that no schedule made completed meanwhile.
const veleroManualWindow = 7 * 24 * time.Hour

// veleroDefaultTTL is Velero's default time to live of a backup (the server's
// --default-backup-ttl), and veleroDefaultLocation the location a backup that names none goes
// to when no location is marked default (--default-backup-storage-location).
const (
	veleroDefaultTTL      = 30 * 24 * time.Hour
	veleroDefaultLocation = "default"
)

// The labels Velero puts on what a schedule makes.
const veleroScheduleLabel = "velero.io/schedule-name"

// maintenanceFailed is a repository maintenance's result that failed.
const maintenanceFailed = "Failed"

// veleroModeler builds one model: the reading, the namespaces it is narrowed to, the time, and
// what it learnt of Velero's server.
type veleroModeler struct {
	b      *Backend
	vb     *veleroBuild
	nsSet  map[string]struct{}
	now    time.Time
	tf     model.TimeFormat
	server veleroServer
	m      *VeleroDashboardModel

	failed map[string][]VeleroFailedVolume // the failed volume backups by backup name (velero_volumes.go)
}

// veleroServer is what the page reads from Velero's Deployment: its namespace, its zone, its
// default time to live and default location.
type veleroServer struct {
	namespace string
	loc       *time.Location
	ttl       time.Duration
	location  string
}

// veleroModel builds the page for the namespaces (empty: all).
func (s *Session) veleroModel(b *Backend, vb *veleroBuild, namespaces []string) *VeleroDashboardModel {
	return buildVeleroModel(b, vb, namespaces, s.Now(), s.TimeFormat())
}

// buildVeleroModel builds the page at now, the user's times in tf.
func buildVeleroModel(b *Backend, vb *veleroBuild, namespaces []string, now time.Time, tf model.TimeFormat) *VeleroDashboardModel {
	mb := &veleroModeler{b: b, vb: vb, now: now, tf: tf,
		m: &VeleroDashboardModel{Served: vb.served, Namespaces: slices.Clone(namespaces), Synced: vb.synced, Error: vb.errText,
			Tiles: []VeleroTile{}, Attention: []VeleroAttention{}, FailedVolumes: []VeleroVolumeGroup{}, Schedules: []VeleroSchedule{},
			Outside: []VeleroOutside{}}}
	if len(namespaces) > 0 {
		mb.nsSet = setOf(namespaces)
	}
	if vb.unknown != "" {
		mb.m.Error = vb.unknown
	}
	if !vb.served {
		return mb.m
	}
	mb.header()
	scheds := mb.schedules()
	mb.tiles(scheds)
	mb.storageAttention()
	mb.backupAttention(scheds)
	mb.m.FailedVolumes = mb.volumeGroups()
	mb.outside(scheds)
	mb.sortAttention()
	return mb.m
}

func (mb *veleroModeler) objs(key string) []*unstructured.Unstructured { return mb.vb.objs[key] }

// ref is an object's reference in the backend's context.
func (mb *veleroModeler) ref(kind string, o *unstructured.Unstructured) model.ObjectRef {
	return model.ObjectRef{Context: mb.b.Context, Kind: kind, Namespace: o.GetNamespace(), Name: o.GetName()}
}

// inNamespaces reports whether a namespace is one the page is narrowed to.
func (mb *veleroModeler) inNamespaces(ns string) bool {
	if mb.nsSet == nil {
		return true
	}
	_, ok := mb.nsSet[ns]
	return ok
}

// header fills the header line and learns Velero's server settings.
func (mb *veleroModeler) header() {
	mb.server = veleroServer{loc: time.UTC, ttl: veleroDefaultTTL, location: veleroDefaultLocation}
	ns := mb.veleroNamespace()
	mb.server.namespace = ns
	h := &mb.m.Header
	h.Namespace = ns
	if dep := mb.veleroDeployment(ns); dep != nil {
		mb.readDeployment(dep, h)
		if ns == "" {
			h.Namespace, mb.server.namespace = dep.GetNamespace(), dep.GetNamespace()
		}
	}
	for _, ds := range mb.objs(logKindDaemonSets) {
		if ds.GetNamespace() == mb.server.namespace && (ds.GetName() == "node-agent" || ds.GetName() == "restic") {
			ready, _ := model.NestedInt64(ds.Object, "status", "numberReady")
			desired, _ := model.NestedInt64(ds.Object, "status", "desiredNumberScheduled")
			h.NodeAgents = &VeleroReady{Ready: int(ready), Desired: int(desired), Ref: mb.ref(logKindDaemonSets, ds)}
			break
		}
	}
}

// veleroNamespace is where Velero's objects live: its storage locations', else its schedules',
// backups' or repositories'; "" when the page reads none.
func (mb *veleroModeler) veleroNamespace() string {
	for _, key := range []string{VeleroLocationsKey, VeleroSchedulesKey, VeleroBackupsKey, VeleroRepositoriesKey, VeleroSnapshotLocationsKey} {
		if objs := mb.objs(key); len(objs) > 0 {
			return objs[0].GetNamespace()
		}
	}
	return ""
}

// veleroDeployment is Velero's server: the Deployment named velero in its namespace, else one
// there (or anywhere, without a namespace) whose container runs Velero's image.
func (mb *veleroModeler) veleroDeployment(ns string) *unstructured.Unstructured {
	var found *unstructured.Unstructured
	for _, d := range mb.objs(logKindDeployments) {
		if ns != "" && d.GetNamespace() != ns {
			continue
		}
		if veleroContainer(d) == nil {
			continue
		}
		if d.GetName() == "velero" {
			return d
		}
		if found == nil {
			found = d
		}
	}
	return found
}

// veleroContainer is the container of a Deployment that runs Velero's server image (its
// repository's last part is velero), or nil.
func veleroContainer(d *unstructured.Unstructured) map[string]any {
	for _, c := range model.NestedSlice(d.Object, "spec", "template", "spec", "containers") {
		cm, _ := c.(map[string]any)
		repo, _ := imageParts(model.NestedString(cm, "image"))
		if repo == "velero" || strings.HasSuffix(repo, "/velero") {
			return cm
		}
	}
	return nil
}

// readDeployment reads the version, the readiness, the plugins and the server's settings.
func (mb *veleroModeler) readDeployment(d *unstructured.Unstructured, h *VeleroHeader) {
	c := veleroContainer(d)
	_, tag := imageParts(model.NestedString(c, "image"))
	h.Version = strings.TrimPrefix(tag, "v")
	desired, ok := model.NestedInt64(d.Object, "spec", "replicas")
	if !ok {
		desired = 1
	}
	ready, _ := model.NestedInt64(d.Object, "status", "readyReplicas")
	h.Server = &VeleroReady{Ready: int(ready), Desired: int(desired), Ref: mb.ref(logKindDeployments, d)}
	for _, ic := range model.NestedSlice(d.Object, "spec", "template", "spec", "initContainers") {
		icm, _ := ic.(map[string]any)
		repo, tag := imageParts(model.NestedString(icm, "image"))
		name := repo[strings.LastIndexByte(repo, '/')+1:]
		name = strings.TrimPrefix(strings.TrimPrefix(name, "velero-plugin-for-"), "velero-plugin-")
		if name != "" {
			h.Plugins = append(h.Plugins, VeleroPlugin{Name: name, Version: strings.TrimPrefix(tag, "v")})
		}
	}
	args := append(model.NestedStrings(c, "command"), model.NestedStrings(c, "args")...)
	if v := flagValue(args, "--default-backup-ttl"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			mb.server.ttl = d
		}
	}
	if v := flagValue(args, "--default-backup-storage-location"); v != "" {
		mb.server.location = v
	}
	for _, e := range model.NestedSlice(c, "env") {
		em, _ := e.(map[string]any)
		if model.NestedString(em, "name") == "TZ" {
			if loc, err := time.LoadLocation(model.NestedString(em, "value")); err == nil {
				mb.server.loc = loc
			}
		}
	}
}

// imageParts splits an image into its repository and tag ("" without one, or with a digest only).
func imageParts(image string) (repo, tag string) {
	image, _, _ = strings.Cut(image, "@")
	if i := strings.LastIndexByte(image, ':'); i > strings.LastIndexByte(image, '/') {
		return image[:i], image[i+1:]
	}
	return image, ""
}

// flagValue is a flag's value in a command line, "--flag=value" or "--flag value".
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v
		}
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// The tiles' line labels.
const (
	lblEnabled          = "Enabled"
	lblPaused           = "Paused"
	lblOverdue          = "Overdue"
	lblFailedValidation = "Failed validation"
	lblNew              = "New"
	lblCompleted        = "Completed"
	lblPartiallyFailed  = "Partially failed"
	lblFailed           = "Failed"
	lblInProgress       = "In progress"
	lblDeleting         = "Deleting"
)

// tiles fills Schedules, Backups, Restores and Storage.
func (mb *veleroModeler) tiles(scheds []*veleroSched) {
	schedules := map[string]int{}
	for _, sc := range scheds {
		if sc.inPage {
			schedules[sc.tileState]++
		}
	}
	mb.m.Tiles = append(mb.m.Tiles,
		mb.tile(VeleroSchedulesKey, "Schedules", []string{VeleroSchedulesKey}, schedules, []VeleroCount{
			{Label: lblEnabled, Status: model.StatusOK}, {Label: lblPaused, Status: model.StatusPending},
			{Label: lblOverdue, Status: model.StatusError}, {Label: lblFailedValidation, Status: model.StatusError},
			{Label: lblNew, Status: model.StatusPending},
		}),
		mb.tile(VeleroBackupsKey, "Backups", []string{VeleroBackupsKey}, mb.phaseCounts(VeleroBackupsKey), backupLines(true)),
		mb.tile(VeleroRestoresKey, "Restores", []string{VeleroRestoresKey}, mb.phaseCounts(VeleroRestoresKey), backupLines(false)),
		mb.storageTile())
}

// backupLines are the Backups' (and without Deleting the Restores') lines in order.
func backupLines(deleting bool) []VeleroCount {
	out := []VeleroCount{
		{Label: lblCompleted, Status: model.StatusOK}, {Label: lblPartiallyFailed, Status: model.StatusWarning},
		{Label: lblFailed, Status: model.StatusError}, {Label: lblFailedValidation, Status: model.StatusError},
		{Label: lblInProgress, Status: model.StatusPending},
	}
	if deleting {
		out = append(out, VeleroCount{Label: lblDeleting, Status: model.StatusPending})
	}
	return out
}

// runLabels are the tile labels of the run states.
var runLabels = map[string]string{
	runCompleted: lblCompleted, runPartiallyFailed: lblPartiallyFailed, runFailed: lblFailed,
	runFailedValidation: lblFailedValidation, runRunning: lblInProgress, runDeleting: lblDeleting,
}

// phaseCounts counts a kind's backups or restores in the page's namespaces by their tile label.
func (mb *veleroModeler) phaseCounts(key string) map[string]int {
	out := map[string]int{}
	for _, o := range mb.objs(key) {
		if !veleroScopeOf(model.NestedMap(o.Object, "spec")).touches(mb.nsSet) {
			continue
		}
		out[runLabels[runState(model.NestedString(o.Object, "status", "phase"))]]++
	}
	return out
}

// tile is a tile of the given lines, each with its count; zero counts are left out.
func (mb *veleroModeler) tile(kind, title string, keys []string, counts map[string]int, lines []VeleroCount) VeleroTile {
	t := VeleroTile{Kind: kind, Title: title, Synced: true}
	for _, l := range lines {
		if n := counts[l.Label]; n > 0 {
			l.Count = n
			t.Count += n
			t.Lines = append(t.Lines, l)
		}
	}
	var errs []string
	for _, k := range keys {
		if !mb.vb.read[k] {
			if _, served := mb.b.Profile(k); served {
				errs = append(errs, "Cogpit may not list "+k) // newVeleroSource left it out
			}
			continue
		}
		t.Synced = t.Synced && mb.vb.kindOK[k]
		if e := mb.vb.kindErr[k]; e != "" {
			errs = append(errs, e)
		}
	}
	t.Error = strings.Join(errs, "; ")
	return t
}

// veleroPlural is a label's singular or plural form by count: "Location available", "Locations
// available".
func veleroPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// storageTile counts the storage locations, snapshot locations and repositories by phase.
func (mb *veleroModeler) storageTile() VeleroTile {
	type part struct {
		one, many string
		status    model.Status
		n         int
	}
	var parts []part
	add := func(one, many string, status model.Status, n int) {
		if n > 0 {
			parts = append(parts, part{one, many, status, n})
		}
	}
	bsl := map[string]int{}
	for _, o := range mb.objs(VeleroLocationsKey) {
		bsl[model.NestedString(o.Object, "status", "phase")]++
	}
	add("Location available", "Locations available", model.StatusOK, bsl["Available"])
	add("Location unavailable", "Locations unavailable", model.StatusError, bsl["Unavailable"])
	add("Location pending", "Locations pending", model.StatusPending, bsl[""])
	vsl := map[string]int{}
	for _, o := range mb.objs(VeleroSnapshotLocationsKey) {
		vsl[model.NestedString(o.Object, "status", "phase")]++
	}
	add("Snapshot location", "Snapshot locations", model.StatusOK, len(mb.objs(VeleroSnapshotLocationsKey))-vsl["Unavailable"])
	add("Snapshot location unavailable", "Snapshot locations unavailable", model.StatusError, vsl["Unavailable"])
	repo := map[string]int{}
	for _, o := range mb.objs(VeleroRepositoriesKey) {
		if mb.inNamespaces(model.NestedString(o.Object, "spec", "volumeNamespace")) {
			repo[model.NestedString(o.Object, "status", "phase")]++
		}
	}
	add("Repository ready", "Repositories ready", model.StatusOK, repo["Ready"])
	add("Repository not ready", "Repositories not ready", model.StatusError, repo["NotReady"])
	add("Repository pending", "Repositories pending", model.StatusPending, repo[""]+repo[phaseNew])
	counts := map[string]int{}
	lines := make([]VeleroCount, 0, len(parts))
	for _, p := range parts {
		label := veleroPlural(p.n, p.one, p.many)
		counts[label] = p.n
		lines = append(lines, VeleroCount{Label: label, Status: p.status})
	}
	return mb.tile(VeleroLocationsKey, "Storage", []string{VeleroLocationsKey, VeleroSnapshotLocationsKey, VeleroRepositoriesKey}, counts, lines)
}

// attend adds a row to Needs attention.
func (mb *veleroModeler) attend(a VeleroAttention) { mb.m.Attention = append(mb.m.Attention, a) }

// storageAttention adds the storage locations that are unavailable, a missing default location,
// the repositories not ready or whose last maintenance failed, and node agents not ready.
func (mb *veleroModeler) storageAttention() {
	defaultFound := false
	for _, o := range mb.objs(VeleroLocationsKey) {
		if d, _ := model.NestedBool(o.Object, "spec", "default"); d || o.GetName() == mb.server.location {
			defaultFound = true
		}
		if model.NestedString(o.Object, "status", "phase") != "Unavailable" {
			continue
		}
		since := model.NestedTime(o.Object, "status", "lastSyncedTime") // unavailable since its last sync at most
		if since.IsZero() {
			since = o.GetCreationTimestamp().Time
		}
		mb.attend(VeleroAttention{Problem: "Location unavailable", Status: model.StatusError, Ref: mb.ref(VeleroLocationsKey, o),
			Object: "BackupStorageLocation " + o.GetName(), Scope: "—", Since: since, Detail: model.NestedString(o.Object, "status", "message")})
	}
	if mb.vb.kindOK[VeleroLocationsKey] && !defaultFound {
		mb.noDefaultLocation()
	}
	for _, o := range mb.objs(VeleroRepositoriesKey) {
		mb.repositoryAttention(o)
	}
	if na := mb.m.Header.NodeAgents; na != nil && na.Ready < na.Desired {
		mb.attend(VeleroAttention{Problem: "Node agents not ready", Status: model.StatusWarning, Ref: na.Ref,
			Object: "DaemonSet " + na.Ref.Name, Scope: "—",
			Detail: "not ready on " + strconv.Itoa(na.Desired-na.Ready) + " of " + strconv.Itoa(na.Desired) + " nodes"})
	}
}

// noDefaultLocation adds the row for a missing default location: an error when an enabled schedule
// names no location (its next backup fails validation), else a warning.
func (mb *veleroModeler) noDefaultLocation() {
	var relying []string
	for _, o := range mb.objs(VeleroSchedulesKey) {
		paused, _ := model.NestedBool(o.Object, "spec", "paused")
		if !paused && model.NestedString(o.Object, "spec", "template", "storageLocation") == "" {
			relying = append(relying, o.GetName())
		}
	}
	a := VeleroAttention{Problem: "No default location", Status: model.StatusWarning,
		Ref:    model.ObjectRef{Context: mb.b.Context, Kind: VeleroLocationsKey},
		Object: "BackupStorageLocations", Scope: "—", Detail: "a backup that names no location fails validation"}
	if len(mb.objs(VeleroLocationsKey)) == 0 {
		a.Detail = "no BackupStorageLocation exists: every backup fails validation"
	}
	if len(relying) > 0 {
		slices.Sort(relying)
		a.Status = model.StatusError
		a.Detail += "; naming none: " + strings.Join(relying, ", ")
	}
	mb.attend(a)
}

// repositoryAttention adds a repository that is not ready, or whose last maintenance failed.
func (mb *veleroModeler) repositoryAttention(o *unstructured.Unstructured) {
	vns := model.NestedString(o.Object, "spec", "volumeNamespace")
	if !mb.inNamespaces(vns) {
		return
	}
	scope := vns
	if scope == "" {
		scope = "—"
	}
	obj := "BackupRepository " + o.GetName()
	if model.NestedString(o.Object, "status", "phase") == "NotReady" {
		mb.attend(VeleroAttention{Problem: "Repository not ready", Status: model.StatusError, Ref: mb.ref(VeleroRepositoriesKey, o),
			Object: obj, Scope: scope, Detail: model.NestedString(o.Object, "status", "message")})
	}
	var last map[string]any
	var lastAt time.Time
	for _, r := range model.NestedSlice(o.Object, "status", "recentMaintenance") {
		rm, _ := r.(map[string]any)
		at := model.NestedTime(rm, "completeTimestamp")
		if at.IsZero() {
			at = model.NestedTime(rm, "startTimestamp")
		}
		if last == nil || at.After(lastAt) {
			last, lastAt = rm, at
		}
	}
	if last != nil && model.NestedString(last, "result") == maintenanceFailed {
		mb.attend(VeleroAttention{Problem: "Maintenance failed", Status: model.StatusWarning, Ref: mb.ref(VeleroRepositoriesKey, o),
			Object: obj, Scope: scope, Since: lastAt, Detail: model.NestedString(last, "message")})
	}
}

// backupOutcome is the Detail of a backup that did not complete: its counts of errors and
// warnings, its failed volume backups (failedVolumes of them) and the errors no volume backup
// explains ("28 errors · 28 volume backups failed", "49 errors · 28 volume backups failed · 21
// counted only"), its volume snapshots that did not complete, its failed item operations, its
// failure reason or validation errors. Never the errors' own text: only a DownloadRequest has it
// (D-151); a failed volume backup's message is in its own object (Failed volume backups).
func backupOutcome(o *unstructured.Unstructured, failedVolumes int) string {
	st := model.NestedMap(o.Object, "status")
	num := func(f string) int {
		n, _ := model.NestedInt64(st, f)
		return int(n)
	}
	var parts []string
	var counts []string
	if n := num("errors"); n > 0 {
		counts = append(counts, strconv.Itoa(n)+" "+veleroPlural(n, "error", "errors"))
	}
	if n := num("warnings"); n > 0 {
		counts = append(counts, strconv.Itoa(n)+" "+veleroPlural(n, "warning", "warnings"))
	}
	if len(counts) > 0 {
		parts = append(parts, strings.Join(counts, ", "))
	}
	if failedVolumes > 0 {
		parts = append(parts, strconv.Itoa(failedVolumes)+" volume "+veleroPlural(failedVolumes, "backup", "backups")+" failed")
		if rest := num("errors") - failedVolumes; rest > 0 {
			parts = append(parts, strconv.Itoa(rest)+" counted only")
		}
	}
	attempted := num("volumeSnapshotsAttempted") + num("csiVolumeSnapshotsAttempted")
	completed := num("volumeSnapshotsCompleted") + num("csiVolumeSnapshotsCompleted")
	if attempted > completed {
		parts = append(parts, strconv.Itoa(completed)+" of "+strconv.Itoa(attempted)+" volume snapshots")
	}
	if n := num("backupItemOperationsFailed"); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" item "+veleroPlural(n, "operation", "operations")+" failed")
	}
	if r := model.NestedString(st, "failureReason"); r != "" {
		parts = append(parts, r)
	}
	if v := model.NestedStrings(st, "validationErrors"); len(v) > 0 {
		parts = append(parts, strings.Join(v, "; "))
	}
	return strings.Join(parts, " · ")
}

// backupProblem is the Problem and Status of a backup run's state, "" for one that completed or
// still runs.
func backupProblem(state string) (string, model.Status) {
	switch state {
	case runFailed:
		return lblFailed, model.StatusError
	case runFailedValidation:
		return lblFailedValidation, model.StatusError
	case runPartiallyFailed:
		return lblPartiallyFailed, model.StatusWarning
	}
	return "", ""
}

// finishedAt is when a backup ended: its completion, else its start, else its creation.
func finishedAt(o *unstructured.Unstructured) time.Time {
	for _, f := range []string{"completionTimestamp", "startTimestamp"} {
		if t := model.NestedTime(o.Object, "status", f); !t.IsZero() {
			return t
		}
	}
	return o.GetCreationTimestamp().Time
}

// backupRow is Needs attention's row of a backup that did not complete.
func (mb *veleroModeler) backupRow(o *unstructured.Unstructured, problem string, status model.Status) VeleroAttention {
	return VeleroAttention{Problem: problem, Status: status, Ref: mb.ref(VeleroBackupsKey, o), Object: "Backup " + o.GetName(),
		Scope: veleroScopeOf(model.NestedMap(o.Object, "spec")).text(), Since: finishedAt(o),
		Detail: backupOutcome(o, len(mb.failedVolumes()[o.GetName()]))}
}

// backupAttention adds each schedule's problems (failed validation, overdue, its latest finished
// backup failed or partly) and the backups no schedule made that failed in the last week and were
// not done again since.
func (mb *veleroModeler) backupAttention(scheds []*veleroSched) {
	for _, sc := range scheds {
		if !sc.inPage {
			continue
		}
		for _, a := range sc.attention {
			mb.attend(a)
		}
	}
	type manual struct {
		o     *unstructured.Unstructured
		scope veleroScope
		at    time.Time
		state string
	}
	var manuals []manual
	for _, o := range mb.objs(VeleroBackupsKey) {
		if o.GetLabels()[veleroScheduleLabel] != "" {
			continue
		}
		manuals = append(manuals, manual{o: o, scope: veleroScopeOf(model.NestedMap(o.Object, "spec")), at: finishedAt(o),
			state: runState(model.NestedString(o.Object, "status", "phase"))})
	}
	for _, m := range manuals {
		problem, status := backupProblem(m.state)
		if problem == "" || mb.now.Sub(m.at) > veleroManualWindow || !m.scope.touches(mb.nsSet) {
			continue
		}
		redone := slices.ContainsFunc(manuals, func(o manual) bool {
			return o.state == runCompleted && o.at.After(m.at) && o.scope.key() == m.scope.key()
		})
		if !redone {
			mb.attend(mb.backupRow(m.o, problem, status))
		}
	}
}

// sortAttention orders Needs attention: errors before warnings, then the newest first (a row
// without a time after those with one), then by object.
func (mb *veleroModeler) sortAttention() {
	rank := func(s model.Status) int {
		if s == model.StatusError {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(mb.m.Attention, func(a, b VeleroAttention) int {
		if c := cmp.Compare(rank(a.Status), rank(b.Status)); c != 0 {
			return c
		}
		if a.Since.IsZero() != b.Since.IsZero() {
			if a.Since.IsZero() {
				return 1
			}
			return -1
		}
		if c := b.Since.Compare(a.Since); c != 0 {
			return c
		}
		return cmp.Compare(a.Object, b.Object)
	})
}
