package helm

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/model"
)

// ReleaseKind is the kind key of Helm releases, a virtual kind (architecture §4.5, §5): Cogpit's
// own name, a bare plural without a dot, so that it never collides with a custom resource kind
// such as Flux's helmreleases.helm.toolkit.fluxcd.io.
const ReleaseKind = "helmreleases"

// The apiVersion and kind of a derived release object: Cogpit's own, not an API the cluster serves.
const (
	releaseAPIVersion = model.LocalGroup + "/v1"
	releaseObjectKind = "HelmRelease"
)

// Titles of the release drawer's sections, as Lens titles them (captured 2026-10-12): the values,
// the chart's notes, and the release's resources, one section per kind under the group heading.
const (
	TitleValues    = "Values"
	TitleNotes     = "Notes"
	GroupResources = "Resources"
)

// Fields of a derived release object; the list's columns and the searchable fields take the same
// names.
const (
	fieldChart        = "chart"
	fieldChartVersion = "chartVersion"
	fieldAppVersion   = "appVersion"
	fieldRevision     = "revision"
	fieldStatus       = "status"
	fieldUpdated      = "updated"
	fieldDescription  = "description"
	fieldHistory      = "history"
)

// The name and namespace the list, the drawer and its resource tables share.
const (
	fieldName      = "name"
	fieldNamespace = "namespace"
	titleNamespace = "Namespace"
	kindNamespaces = "namespaces" // the kind key a namespace links to
)

// Titles the list's columns, the drawer's Properties and the History table share.
const (
	titleChart      = "Chart"
	titleRevision   = "Revision"
	titleAppVersion = "App Version"
	titleStatus     = "Status"
	titleUpdated    = "Updated"
)

// Bounds on what the drawer keeps of a release. A release's history is at most what Helm keeps
// (10 revisions by default); the values and the notes are cut for the drawer, which renders one
// line per line; the resources are the first maxResources objects of the manifest.
const (
	historyRows  = 100      // revisions in the release object's history
	valuesBytes  = 16 << 10 // of the user-supplied values as YAML
	valuesLines  = 200
	notesBytes   = 16 << 10 // of the chart's rendered notes
	notesLines   = 200
	maxResources = 500
)

// The memo's bounds: entries for records that are gone leave it after two generations of this
// many Derive calls without being seen; it never holds more than memoEntries entries.
const (
	memoGeneration = 64
	memoEntries    = 4096
)

// memoIdle is how long the memo keeps what it decoded after the last Derive: Derive runs while a
// view, a drawer or an agent reads the releases, and stops when nobody does and the release
// records' watches end (the cache's warm period, five minutes by default). The decoded values then
// leave memory with the records they came from (H-8). A var for tests.
var memoIdle = 5 * time.Minute

// NewReleaseProfile returns the profile of the Helm releases of one cluster connection: a virtual
// kind over the records labelled owner=helm, one object per release, its newest revision winning.
// The profile's scope is the release Secrets (D-13); the engine joins the release ConfigMaps
// (HELM_DRIVER=configmap) to it (Backend.Acquire, cache.Join), so Derive sees the records of both
// drivers. Each call returns a profile with its own memo of decoded payloads; the connector makes
// one per backend.
func NewReleaseProfile() *model.KindProfile {
	m := &memo{cur: map[types.UID]*revision{}, prev: map[types.UID]*revision{}}
	return &model.KindProfile{
		Key:           ReleaseKind,
		Version:       "v1",
		Resource:      resourceSecrets,
		Kind:          releaseObjectKind,
		Title:         "Releases",
		DetailTitle:   "Release",
		Namespaced:    true,
		LabelSelector: ReleaseSelector,
		Columns: []model.Column{
			{ID: fieldName, Title: "Name", Type: model.ColumnText},
			{ID: fieldNamespace, Title: titleNamespace, Type: model.ColumnText},
			{ID: fieldChart, Title: titleChart, Type: model.ColumnText},
			{ID: fieldRevision, Title: titleRevision, Type: model.ColumnNumber},
			{ID: "version", Title: "Version", Type: model.ColumnText},
			{ID: fieldAppVersion, Title: titleAppVersion, Type: model.ColumnText},
			{ID: fieldStatus, Title: titleStatus, Type: model.ColumnText},
			{ID: fieldUpdated, Title: titleUpdated, Type: model.ColumnAge},
		},
		Cells: releaseCells,
		Fields: map[string]func(*unstructured.Unstructured) []string{
			fieldChart:  func(o *unstructured.Unstructured) []string { return nonEmpty(model.NestedString(o, fieldChart)) },
			fieldStatus: func(o *unstructured.Unstructured) []string { return nonEmpty(model.NestedString(o, fieldStatus)) },
		},
		Derive: m.derive,
		Detail: m.detail,
	}
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// StatusText is a release status as Lens writes it: capitalized, "Deployed" (captured 2026-10-12);
// the other states capitalized the same way, "Pending-upgrade", "Superseded" (inferred). The
// release object, helm.pendingOperations and resources_find keep Helm's own spelling.
func StatusText(status string) string {
	if status == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(status)
	return string(unicode.ToUpper(r)) + status[n:]
}

// StatusToken colours a release status (inferred: Lens's colours are not captured): deployed is
// good, failed an error, the pending states pending, uninstalling like a deletion; superseded and
// uninstalled (a release uninstalled with its history kept) have no colour.
func StatusToken(status string) model.Status {
	switch common.Status(status) {
	case common.StatusDeployed:
		return model.StatusOK
	case common.StatusFailed:
		return model.StatusError
	case common.StatusPendingInstall, common.StatusPendingUpgrade, common.StatusPendingRollback:
		return model.StatusPending
	case common.StatusUninstalling:
		return model.StatusTerminating
	case common.StatusSuperseded, common.StatusUninstalled:
		return model.StatusNone
	}
	return model.StatusUnknown
}

// Pending reports whether a status is one a crash can leave behind: an install, upgrade or
// rollback that did not finish, or an uninstall.
func Pending(status string) bool {
	return common.Status(status).IsPending() || common.Status(status) == common.StatusUninstalling
}

// ReleaseRevision returns a release object's newest revision.
func ReleaseRevision(o *unstructured.Unstructured) int {
	n, _ := model.NestedInt64(o, fieldRevision)
	return int(n)
}

// ReleaseStatus returns the status of a release object's newest revision.
func ReleaseStatus(o *unstructured.Unstructured) string { return model.NestedString(o, fieldStatus) }

// ReleaseRevisions returns the revisions in a release object's history, newest first.
func ReleaseRevisions(o *unstructured.Unstructured) []int {
	var out []int
	for _, h := range model.NestedSlice(o, fieldHistory) {
		if n, ok := model.NestedInt64(h, fieldRevision); ok {
			out = append(out, int(n))
		}
	}
	return out
}

// ReleaseChart returns the chart name and version of a release object's newest revision.
func ReleaseChart(o *unstructured.Unstructured) (name, version string) {
	return model.NestedString(o, fieldChart), model.NestedString(o, fieldChartVersion)
}

// ReleaseSecret returns the name of the release Secret that holds a release object's newest
// revision, in the release's namespace; empty for a release kept in ConfigMaps (ReleaseRecord).
func ReleaseSecret(o *unstructured.Unstructured) string { return model.NestedString(o, fieldSecret) }

// ReleaseStorage returns the storage driver whose records hold a release object's revisions:
// DriverConfigMap for a release Helm keeps in ConfigMaps, else DriverSecret.
func ReleaseStorage(o *unstructured.Unstructured) Driver {
	if Driver(model.NestedString(o, fieldStorage)) == DriverConfigMap {
		return DriverConfigMap
	}
	return DriverSecret
}

// ReleaseRecord returns the driver and the name of the record (a Secret or a ConfigMap in the
// release's namespace) that holds a release object's newest revision.
func ReleaseRecord(o *unstructured.Unstructured) (Driver, string) {
	if d := ReleaseStorage(o); d == DriverConfigMap {
		return d, model.NestedString(o, fieldConfigMap)
	}
	return DriverSecret, model.NestedString(o, fieldSecret)
}

// Fields of a release object that name where its records are.
const (
	fieldStorage   = "storage"   // the driver: "secret" or "configmap"
	fieldSecret    = "secret"    // the newest revision's Secret
	fieldConfigMap = "configMap" // the newest revision's ConfigMap
	// fieldHidden names the other driver when it holds a release of the same namespace and name,
	// which the object does not show; fieldHiddenRevisions counts that release's records;
	// fieldHiddenOlder says that release was deployed before the one shown (false: at the same
	// time, the tie shownBefore breaks for Secrets).
	fieldHidden          = "hiddenStorage"
	fieldHiddenRevisions = "hiddenRevisions"
	fieldHiddenOlder     = "hiddenOlder"
)

// HistoryEntry is one revision of a release, as the release object's history holds it.
type HistoryEntry struct {
	Revision     int       `json:"revision"`
	Chart        string    `json:"chart"` // the chart's name
	ChartVersion string    `json:"chartVersion"`
	AppVersion   string    `json:"appVersion,omitempty"`
	Status       string    `json:"status"` // Helm's own spelling: "deployed", "superseded"
	Updated      time.Time `json:"updated,omitzero"`
	Description  string    `json:"description,omitempty"`
}

// ReleaseHistory returns the revisions in a release object's history, newest first: the release
// Secrets the cache holds, at most historyRows.
func ReleaseHistory(o *unstructured.Unstructured) []HistoryEntry {
	hist := model.NestedSlice(o, fieldHistory)
	out := make([]HistoryEntry, 0, len(hist))
	for _, h := range hist {
		n, _ := model.NestedInt64(h, fieldRevision)
		e := HistoryEntry{
			Revision: int(n), ChartVersion: model.NestedString(h, fieldChartVersion),
			AppVersion: model.NestedString(h, fieldAppVersion), Status: model.NestedString(h, fieldStatus),
			Updated: model.NestedTime(h, fieldUpdated), Description: model.NestedString(h, fieldDescription),
		}
		e.Chart = model.NestedString(h, fieldChart)
		if e.ChartVersion != "" {
			e.Chart = strings.TrimSuffix(e.Chart, "-"+e.ChartVersion)
		}
		out = append(out, e)
	}
	return out
}

func releaseCells(o *unstructured.Unstructured) ([]model.Cell, model.Status) {
	status := model.NestedString(o, fieldStatus)
	token := StatusToken(status)
	rev, _ := model.NestedInt64(o, fieldRevision)
	ns := o.GetNamespace()
	return []model.Cell{
		{Text: o.GetName()},
		{Text: ns, Link: &model.ObjectRef{Kind: kindNamespaces, Name: ns}},
		{Text: model.NestedString(o, fieldChart)},
		{Text: strconv.FormatInt(rev, 10), Num: rev},
		{Text: model.NestedString(o, fieldChartVersion)},
		{Text: model.NestedString(o, fieldAppVersion)},
		{Text: StatusText(status), Status: token},
		model.AgeCell(model.NestedTime(o, fieldUpdated)),
	}, token
}

// revision is what the memo keeps of one release record (a Secret or a ConfigMap): one revision
// of one release.
type revision struct {
	uid       types.UID
	rv        string
	storage   Driver // the record's driver: its kind
	record    string // the record's name
	namespace string
	created   time.Time // the record's creation

	name, status                    string
	number                          int
	chart, chartVersion, appVersion string
	updated, firstDeployed          time.Time
	description                     string
	err                             string // why the payload could not be read; the rest is from the labels

	// heavy are the values, notes and resources, kept for the newest revision of each release only.
	heavy *heavy
}

// heavy is what the drawer shows of a release's newest revision beyond its summary.
type heavy struct {
	values    string     // the user-supplied values as YAML, cut to valuesBytes and valuesLines
	notes     string     // the chart's rendered notes, cut to notesBytes and notesLines
	resources []resource // the objects of the manifest, at most maxResources
}

// memo keeps the revisions Derive decoded, by the record's UID (unique across Secrets and
// ConfigMaps), valid for one resourceVersion, so that a snapshot decodes only the records that
// changed. Entries move from prev to cur when they
// are used; every memoGeneration calls (or when cur grows past memoEntries) cur becomes prev and
// the old prev, entries no Derive asked for since, is dropped: that is how entries for records
// that are gone leave it, whichever namespaces a call asked for.
type memo struct {
	mu    sync.Mutex
	cur   map[types.UID]*revision
	prev  map[types.UID]*revision
	calls int
	idle  *time.Timer // purges the memo memoIdle after the last Derive
}

// touch arms the idle purge again. Called with m.mu held.
func (m *memo) touch() {
	if m.idle == nil {
		m.idle = time.AfterFunc(memoIdle, m.purge)
		return
	}
	m.idle.Reset(memoIdle)
}

// purge forgets every decoded revision, values and manifests included. A Derive after it decodes
// the records it is given again.
func (m *memo) purge() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cur, m.prev = map[types.UID]*revision{}, map[types.UID]*revision{}
}

// get returns the memo's revision for a record, or nil. Called with m.mu held.
func (m *memo) get(uid types.UID, rv string) *revision {
	if r, ok := m.cur[uid]; ok && r.rv == rv {
		return r
	}
	if r, ok := m.prev[uid]; ok && r.rv == rv {
		delete(m.prev, uid)
		m.put(r)
		return r
	}
	return nil
}

// put stores a revision. Called with m.mu held.
func (m *memo) put(r *revision) {
	if len(m.cur) >= memoEntries {
		m.rotate()
	}
	m.cur[r.uid] = r
}

func (m *memo) rotate() {
	m.prev, m.cur = m.cur, map[types.UID]*revision{}
}

// size returns how many revisions the memo holds, for tests.
func (m *memo) size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.cur) + len(m.prev)
}

// derive is the profile's Derive: one object per release in the given release records, Secrets
// and ConfigMaps (one or all namespaces), from its newest revision, with the history of the
// revisions present. It decodes only records the memo does not hold in their current version,
// outside the memo's lock.
func (m *memo) derive(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
	byUID := make(map[types.UID]*unstructured.Unstructured, len(objs))
	revs := make([]*revision, 0, len(objs))
	var misses []*unstructured.Unstructured
	m.mu.Lock()
	for _, o := range objs {
		if o == nil || o.GetUID() == "" {
			continue
		}
		byUID[o.GetUID()] = o
		if r := m.get(o.GetUID(), o.GetResourceVersion()); r != nil {
			revs = append(revs, r)
		} else {
			misses = append(misses, o)
		}
	}
	m.mu.Unlock()

	decoded := make([]*revision, 0, len(misses))
	releases := make(map[types.UID]*rspb.Release, len(misses)) // this call's decodes, until the newest are known
	for _, o := range misses {
		r, rls := decodeRevision(o)
		decoded = append(decoded, r)
		if rls != nil {
			releases[r.uid] = rls
		}
	}

	m.mu.Lock()
	for _, r := range decoded {
		m.put(r)
	}
	revs = append(revs, decoded...)
	groups := groupReleases(revs)
	var refill []*revision
	for _, g := range groups {
		for _, r := range g.revs[1:] {
			r.heavy = nil // only the newest revision's values and manifest are kept
		}
		for _, r := range g.hidden {
			r.heavy = nil // nor those of a release the object does not show
		}
		if newest := g.revs[0]; newest.heavy == nil && newest.err == "" {
			refill = append(refill, newest)
		}
	}
	m.mu.Unlock()

	// The newest revision's values and manifest: from this call's decode, or, for a revision that
	// was older and became the newest (a newer one was deleted), decoded again.
	for _, r := range refill {
		rls := releases[r.uid]
		if o := byUID[r.uid]; rls == nil && o != nil {
			_, rls = decodeRevision(o)
		}
		if rls == nil {
			continue
		}
		h := heavyOf(rls, r.namespace)
		m.mu.Lock()
		if r.heavy == nil {
			r.heavy = h
		}
		m.mu.Unlock()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*unstructured.Unstructured, 0, len(groups))
	for _, g := range groups {
		out = append(out, releaseObject(g))
	}
	if m.calls++; m.calls%memoGeneration == 0 {
		m.rotate()
	}
	m.touch()
	return out
}

// releaseGroup is one release's revisions, newest first, and the revisions of a release of the
// same namespace and name that the other driver holds, which the release object does not show.
type releaseGroup struct {
	revs   []*revision
	hidden []*revision
}

// groupReleases groups revisions by namespace, release name and driver, newest revision first in
// each group, the groups in order of namespace and name. Helm's drivers do not see each other's
// records: a release of one name kept in both Secrets and ConfigMaps of a namespace is two
// releases to Helm, whose revision numbers say nothing about each other. Cogpit shows one object
// per namespace and name (Cogpit's choice, inferred: Lens reads one driver), the release whose
// newest revision was deployed last (then created last, then the Secrets', Helm's default), and
// keeps the other's revisions as hidden. Called with m.mu held: it reads revisions.
func groupReleases(revs []*revision) []releaseGroup {
	idx := map[string]int{}
	var groups [][]*revision
	for _, r := range revs {
		key := r.namespace + "/" + r.name + "/" + string(r.storage)
		i, ok := idx[key]
		if !ok {
			i = len(groups)
			idx[key] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], r)
	}
	for _, g := range groups {
		slices.SortFunc(g, func(a, b *revision) int {
			if c := cmp.Compare(b.number, a.number); c != 0 {
				return c
			}
			return cmp.Compare(b.record, a.record) // two records claiming one revision: a stable choice
		})
	}
	byName := map[string]int{}
	out := make([]releaseGroup, 0, len(groups))
	for _, g := range groups {
		key := g[0].namespace + "/" + g[0].name
		i, ok := byName[key]
		if !ok {
			byName[key] = len(out)
			out = append(out, releaseGroup{revs: g})
			continue
		}
		if shownBefore(g[0], out[i].revs[0]) {
			out[i].revs, out[i].hidden = g, out[i].revs
		} else {
			out[i].hidden = g
		}
	}
	slices.SortFunc(out, func(a, b releaseGroup) int {
		return cmp.Or(cmp.Compare(a.revs[0].namespace, b.revs[0].namespace), cmp.Compare(a.revs[0].name, b.revs[0].name))
	})
	return out
}

// shownBefore reports whether the release whose newest revision is a is shown rather than the one
// of the other driver whose newest revision is b: deployed later, else created later, else kept in
// Secrets.
func shownBefore(a, b *revision) bool {
	if c := newerThan(a, b); c != 0 {
		return c > 0
	}
	return a.storage == DriverSecret
}

// newerThan compares two releases by their newest revisions a and b: deployed later, else created
// later, is newer (1); 0 when both times are the same.
func newerThan(a, b *revision) int {
	if c := a.updated.Compare(b.updated); c != 0 {
		return c
	}
	return a.created.Compare(b.created)
}

// releaseObject builds a release's object from its revisions, newest first: the newest revision's
// summary, its record's UID and resourceVersion (a confirmation binds to the revision the user
// saw), the driver and the record that holds it, and the history. It holds no values, notes or
// manifest: the YAML tab shows this object, and the drawer's Values, Notes and Resources come from
// the memo (detail). Called with m.mu held.
func releaseObject(rg releaseGroup) *unstructured.Unstructured {
	g := rg.revs
	r := g[0]
	created := r.firstDeployed
	if created.IsZero() {
		created = g[len(g)-1].created
	}
	meta := map[string]any{
		fieldName: r.name, fieldNamespace: r.namespace, "uid": string(r.uid), "resourceVersion": r.rv,
	}
	if !created.IsZero() {
		meta["creationTimestamp"] = created.UTC().Format(time.RFC3339)
	}
	history := make([]any, 0, min(len(g), historyRows))
	for _, h := range g[:min(len(g), historyRows)] {
		history = append(history, map[string]any{
			fieldRevision: int64(h.number), fieldUpdated: timeText(h.updated), fieldStatus: h.status,
			fieldChart: chartText(h.chart, h.chartVersion), fieldChartVersion: h.chartVersion, fieldAppVersion: h.appVersion,
			fieldDescription: h.description,
		})
	}
	obj := map[string]any{
		"apiVersion": releaseAPIVersion, "kind": releaseObjectKind, "metadata": meta,
		fieldChart: r.chart, fieldChartVersion: r.chartVersion, fieldAppVersion: r.appVersion,
		fieldRevision: int64(r.number), fieldStatus: r.status, fieldUpdated: timeText(r.updated),
		fieldDescription: r.description, fieldStorage: string(r.storage), fieldHistory: history,
	}
	if r.storage == DriverConfigMap {
		obj[fieldConfigMap] = r.record
	} else {
		obj[fieldSecret] = r.record
	}
	if len(rg.hidden) > 0 {
		obj[fieldHidden], obj[fieldHiddenRevisions] = string(rg.hidden[0].storage), int64(len(rg.hidden))
		obj[fieldHiddenOlder] = newerThan(rg.hidden[0], r) < 0
	}
	if r.err != "" {
		obj["error"] = r.err
	}
	return &unstructured.Unstructured{Object: obj}
}

func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// chartText is a chart as helm list shows it: "web-1.2.0".
func chartText(name, version string) string {
	if version == "" {
		return name
	}
	return name + "-" + version
}

// decodeRevision reads one release record (a Secret or a ConfigMap) without its values and
// manifest (heavyOf reads those, for the newest revision of a release only), and returns the
// decoded release for that, nil when it does not decode. The payload is untrusted input: a record
// that does not decode yields a revision from its labels and name, with the error, never a panic.
func decodeRevision(o *unstructured.Unstructured) (*revision, *rspb.Release) {
	labels := o.GetLabels()
	r := &revision{
		uid: o.GetUID(), rv: o.GetResourceVersion(), storage: RecordDriver(o), record: o.GetName(), namespace: o.GetNamespace(),
		created: o.GetCreationTimestamp().Time,
		name:    labels["name"], status: labels["status"],
	}
	r.number, _ = strconv.Atoi(labels["version"])
	if r.name == "" || r.number == 0 {
		name, number := parseSecretName(o.GetName())
		r.name = cmp.Or(r.name, name)
		r.number = cmp.Or(r.number, number)
	}
	rls, err := decodeSafely(o)
	if err != nil {
		r.err = err.Error()
		r.name = cmp.Or(r.name, o.GetName()) // a release needs a name: the record's, when nothing else says one
		return r, nil
	}
	r.name = cmp.Or(rls.Name, r.name)
	r.number = cmp.Or(rls.Version, r.number)
	if rls.Info != nil {
		r.status = cmp.Or(string(rls.Info.Status), r.status)
		r.updated, r.firstDeployed = rls.Info.LastDeployed, rls.Info.FirstDeployed
	}
	if rls.Chart != nil && rls.Chart.Metadata != nil {
		r.chart, r.chartVersion, r.appVersion = rls.Chart.Metadata.Name, rls.Chart.Metadata.Version, rls.Chart.Metadata.AppVersion
	}
	if rls.Info != nil {
		// A failed revision's description quotes the error, which can quote a rendered value
		// (the API server's "Invalid value: …"); the release object, which agents read, shows it
		// scrubbed of the revision's own values, but for what the object shows anyway: Helm's
		// `Upgrade "<release>" failed` keeps its name though the values repeat it.
		public := []string{r.name, r.namespace, r.chart, r.chartVersion, r.appVersion, chartText(r.chart, r.chartVersion)}
		r.description = ScrubValuesExcept(rls.Info.Description, rls.Config, public)
	}
	return r, rls
}

// heavyBuilt counts heavyOf's calls, for the test that derive builds one per release.
var heavyBuilt atomic.Int64

// heavyOf is what the drawer shows of a release's newest revision: its resources from the manifest
// (a YAML parse per document), notes and values. Derive builds it once per release, not once per
// revision (R-13 of the fourth run's review).
func heavyOf(rls *rspb.Release, namespace string) *heavy {
	heavyBuilt.Add(1)
	h := &heavy{resources: manifestResources(rls.Manifest, namespace)}
	if rls.Info != nil && strings.TrimSpace(rls.Info.Notes) != "" {
		h.notes = cut(strings.TrimRight(rls.Info.Notes, "\n"), notesBytes, notesLines)
	}
	if len(rls.Config) > 0 {
		if y, err := yaml.Marshal(rls.Config); err == nil {
			h.values = cut(strings.TrimSuffix(string(y), "\n"), valuesBytes, valuesLines)
		}
	}
	return h
}

// errUnreadable stands for a decoder that panicked on a payload.
var errUnreadable = errors.New("the release payload could not be read")

// decodeSafely decodes a release record, turning a panic in a decoder into an error.
func decodeSafely(o *unstructured.Unstructured) (rls *rspb.Release, err error) {
	defer func() {
		if p := recover(); p != nil {
			rls, err = nil, errUnreadable
		}
	}()
	return DecodeRecord(o)
}

// parseSecretName reads the release name and revision from a record named the way Helm's drivers
// name them (Secrets and ConfigMaps alike), sh.helm.release.v1.<name>.v<revision>.
func parseSecretName(s string) (string, int) {
	rest, ok := strings.CutPrefix(s, "sh.helm.release.v1.")
	if !ok {
		return "", 0
	}
	i := strings.LastIndex(rest, ".v")
	if i <= 0 {
		return rest, 0
	}
	n, err := strconv.Atoi(rest[i+2:])
	if err != nil {
		return rest, 0
	}
	return rest[:i], n
}

// cut returns text within a byte and a line limit, cut at a line boundary, with a note saying how
// much was left out.
func cut(text string, maxBytes, maxLines int) string {
	total := strings.Count(text, "\n") + 1
	shown := text
	if len(shown) > maxBytes {
		end := maxBytes
		for end > 0 && !utf8.RuneStart(shown[end]) {
			end--
		}
		shown = shown[:end]
		if i := strings.LastIndexByte(shown, '\n'); i > 0 {
			shown = shown[:i]
		}
	}
	if lines := strings.SplitAfterN(shown, "\n", maxLines+1); len(lines) > maxLines {
		shown = strings.TrimSuffix(strings.Join(lines[:maxLines], ""), "\n")
	}
	if len(shown) == len(text) {
		return text
	}
	return shown + "\n# [Cogpit shows the first " + strconv.Itoa(strings.Count(shown, "\n")+1) + " of " +
		strconv.Itoa(total) + " lines here]"
}
