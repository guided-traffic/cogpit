package kinds

import (
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Flux CD (G23, D-9, D-33): curated profiles of the Flux toolkit's kinds, registered with the
// other curated CRD profiles (verticalpodautoscalers.go) by this file's init. Lens shows them in its
// paid Flux section (captured: the navigator's structure only); its columns, drawers and actions
// are not captured, so everything here is Cogpit's reading of Flux's own conventions (inferred):
// the Ready condition is the state, spec.suspend a state of its own, and the columns follow Flux's
// printer columns (the kind's own, Age, Ready, Status) with the revision and the suspension added,
// as `flux get` shows them.

// The Flux toolkit's API groups.
const (
	fluxSource       = "source.toolkit.fluxcd.io"
	fluxKustomize    = "kustomize.toolkit.fluxcd.io"
	fluxHelm         = "helm.toolkit.fluxcd.io"
	fluxImage        = "image.toolkit.fluxcd.io"
	fluxNotification = "notification.toolkit.fluxcd.io"
)

// fluxV1beta2 is the version most Flux kinds served before their v1 (and still read the same).
const fluxV1beta2 = "v1beta2"

// Titles, Kinds and texts the Flux profiles share.
const (
	titleURL      = "URL"
	titleSource   = "Source"
	titleSecret   = "Secret"
	titleChart    = "Chart"
	kindHelmChart = "HelmChart"
	kindConfigMap = "ConfigMap"
	kindSecret    = "Secret"
	keyConfigMaps = "configmaps"
	textFalse     = "false"
)

// The Flux section's sub-groups in the navigator, as captured.
const (
	FluxSectionImages        = "Image Automation"
	FluxSectionNotifications = "Notifications"
	FluxSectionSources       = "Sources"
)

// FluxState is a Flux object's state as the Flux views colour it and the Flux dashboard counts it.
type FluxState string

// Flux states (Cogpit's reading of Flux's conditions, inferred).
const (
	// FluxReady: the Ready condition is True and the controller has seen the object's generation.
	FluxReady FluxState = "ready"
	// FluxNotReady: Ready is False, or Stalled is True (the controller gave up until something
	// changes).
	FluxNotReady FluxState = "notReady"
	// FluxReconciling: Reconciling is True, Ready is Unknown, or the controller has not seen the
	// object's latest generation yet.
	FluxReconciling FluxState = "reconciling"
	// FluxSuspended: spec.suspend is true; the controller leaves the object alone.
	FluxSuspended FluxState = "suspended"
	// FluxUnknown: no Ready condition: no controller has reported on the object (yet).
	FluxUnknown FluxState = "unknown"
)

// FluxStates lists the states in the dashboard's order.
var FluxStates = []FluxState{FluxReady, FluxNotReady, FluxReconciling, FluxSuspended, FluxUnknown}

// fluxStateTexts are the states as the Status column writes them.
var fluxStateTexts = map[FluxState]string{
	FluxReady: "Ready", FluxNotReady: "Not Ready", FluxReconciling: fluxCondReconciling, FluxSuspended: "Suspended", FluxUnknown: textUnknown,
}

// fluxStateTokens colour the states: a suspended object is grey, as a stopped container is.
var fluxStateTokens = map[FluxState]model.Status{
	FluxReady: model.StatusOK, FluxNotReady: model.StatusError, FluxReconciling: model.StatusPending,
	FluxSuspended: model.StatusStopped, FluxUnknown: model.StatusUnknown,
}

// FluxStateText returns a state as the Status column writes it: "Ready", "Not Ready".
func FluxStateText(s FluxState) string { return fluxStateTexts[s] }

// FluxStateToken returns a state's status token.
func FluxStateToken(s FluxState) model.Status { return fluxStateTokens[s] }

// FluxKind is one kind of the Flux section: its kind key, its titles, its navigator sub-group and
// the actions it takes.
type FluxKind struct {
	Key     string `json:"kind"`              // "kustomizations.kustomize.toolkit.fluxcd.io"
	APIKind string `json:"apiKind"`           // "Kustomization"
	Title   string `json:"title"`             // "Kustomizations"
	Section string `json:"section,omitempty"` // "", FluxSectionImages, FluxSectionNotifications, FluxSectionSources
	// Suspend: the kind has spec.suspend, so Suspend and Resume apply.
	Suspend bool `json:"suspend,omitempty"`
	// Reconcile: a Flux controller reconciles the kind on request (reconcile.fluxcd.io/requestedAt).
	Reconcile bool `json:"reconcile,omitempty"`
	// static: at the version the profile reads first, the controller reports no status (notification
	// v1beta3's Alerts and Providers): such an object counts as ready unless suspended.
	static bool
}

// fluxSpecs are the Flux kinds in the navigator's order (captured): Helm Releases, Kustomizations;
// Image Automation; Notifications; Sources. Each file of the toolkit's groups adds its specs to
// fluxSpecs through fluxRegister, in that order.
var fluxSpecs []*fluxSpec

// fluxRegister adds specs to the Flux kinds, in order.
func fluxRegister(specs ...*fluxSpec) { fluxSpecs = append(fluxSpecs, specs...) }

func init() {
	fluxRegister(helmReleasesSpec, kustomizationsSpec)
	fluxRegister(imageRepositoriesSpec, imagePoliciesSpec, imageUpdateAutomationsSpec)
	fluxRegister(alertsSpec, providersSpec, receiversSpec)
	fluxRegister(gitRepositoriesSpec, ociRepositoriesSpec, helmRepositoriesSpec, bucketsSpec, externalArtifactsSpec)
	for _, s := range fluxSpecs {
		s.built = s.profile()
		curated = append(curated, s.built)
	}
}

// FluxKinds returns the Flux kinds in the navigator's order.
func FluxKinds() []FluxKind {
	out := make([]FluxKind, len(fluxSpecs))
	for i, s := range fluxSpecs {
		out[i] = s.info()
	}
	return out
}

// FluxKindOf returns the Flux kind of a kind key, and whether the key names one.
func FluxKindOf(key string) (FluxKind, bool) {
	if s := fluxSpecOf(key); s != nil {
		return s.info(), true
	}
	return FluxKind{}, false
}

func fluxSpecOf(key string) *fluxSpec {
	for _, s := range fluxSpecs {
		if s.key() == key {
			return s
		}
	}
	return nil
}

// FluxStateOf returns the state of an object of a Flux kind key; FluxUnknown for another key's.
func FluxStateOf(key string, obj *unstructured.Unstructured) FluxState {
	s := fluxSpecOf(key)
	if s == nil {
		return FluxUnknown
	}
	return fluxStatusOf(obj, s.isStatic(obj)).state
}

// FluxStatusOf returns an object's state, the Ready condition's message (one line) and when the
// Ready condition last changed (zero without one), for the Flux dashboard's table.
func FluxStatusOf(key string, obj *unstructured.Unstructured) (state FluxState, message string, since time.Time) {
	s := fluxSpecOf(key)
	if s == nil {
		return FluxUnknown, "", time.Time{}
	}
	st := fluxStatusOf(obj, s.isStatic(obj))
	return st.state, st.message, st.since
}

// IsFluxSuspended reports whether a Flux object is suspended (spec.suspend: true).
func IsFluxSuspended(obj any) bool {
	v, _ := model.NestedBool(obj, fieldSpec, "suspend")
	return v
}

// fluxRefKeys map a Kind that Flux references (a source, a chart, a provider) to its kind key: the
// curated kinds, and HelmChart, which has no view of its own in the Flux section.
func fluxRefKey(kind string) string {
	for _, s := range fluxSpecs {
		if s.kind == kind {
			return s.key()
		}
	}
	switch kind {
	case kindHelmChart:
		return "helmcharts." + fluxSource
	case kindConfigMap:
		return keyConfigMaps
	case kindSecret:
		return keySecrets
	}
	return ""
}

// Flux condition types beyond Ready.
const (
	fluxCondReady       = "Ready"
	fluxCondReconciling = "Reconciling"
	fluxCondStalled     = "Stalled"
)

// fluxStatus is what the Status and Message columns show.
type fluxStatus struct {
	state   FluxState
	message string
	since   time.Time // the Ready condition's last transition
}

// fluxStatusOf reads an object's state: suspended first (the controller does not touch it, whatever
// its last conditions say), then a failure (Ready False, Stalled True), then a reconciliation in
// progress (Reconciling True, Ready Unknown, a generation the controller has not observed yet),
// then Ready True. Without a Ready condition, a static object (notification v1beta3's Alerts and
// Providers, an OCI HelmRepository) is ready; any other is unknown.
func fluxStatusOf(obj *unstructured.Unstructured, static bool) fluxStatus {
	var ready, reconciling, stalled string
	var st fluxStatus
	for _, c := range model.NestedSlice(obj, "status", "conditions") {
		switch model.NestedString(c, "type") {
		case fluxCondReady:
			ready = model.NestedString(c, "status")
			st.message = singleLine(model.NestedString(c, "message"))
			st.since = model.NestedTime(c, "lastTransitionTime")
		case fluxCondReconciling:
			reconciling = model.NestedString(c, "status")
		case fluxCondStalled:
			stalled = model.NestedString(c, "status")
		}
	}
	stale := false
	if observed, ok := model.NestedInt64(obj, "status", "observedGeneration"); ok {
		stale = observed < obj.GetGeneration()
	}
	switch {
	case IsFluxSuspended(obj):
		st.state = FluxSuspended
	case ready == condFalse, stalled == condTrue:
		st.state = FluxNotReady
	case reconciling == condTrue, ready != "" && ready != condTrue, ready == condTrue && stale:
		st.state = FluxReconciling
	case ready == condTrue, ready == "" && static:
		st.state = FluxReady
	default:
		st.state = FluxUnknown
	}
	return st
}

// fluxConditionToken colours one condition in the drawer (inferred): Ready, Healthy, Released and
// the sources' positive conditions green when True and red when False; Reconciling pending,
// Stalled and the failure conditions red, the warnings orange when True.
func fluxConditionToken(c condition) model.Status {
	switch c.Type {
	case fluxCondReconciling:
		return conditionToken(c.Status, model.StatusPending, model.StatusNone)
	case fluxCondStalled, "FetchFailed", "BuildFailed", "StorageOperationFailed", "ReconciliationFailed":
		return conditionToken(c.Status, model.StatusError, model.StatusNone)
	case "IncludeUnavailable", "ArtifactOutdated", "Remediated":
		return conditionToken(c.Status, model.StatusWarning, model.StatusNone)
	case fluxCondReady:
		if c.Status != condTrue && c.Status != condFalse {
			return model.StatusPending
		}
	}
	return conditionToken(c.Status, model.StatusOK, model.StatusError)
}

// Column ids every Flux profile has besides name, namespace, age and status.
const (
	idRevision  = "revision"
	idSuspended = "suspended"
	idMessage   = "message"
	idURL       = "url"
	idRef       = "ref"
)

var (
	fluxRevisionColumn  = model.Column{ID: idRevision, Title: "Revision", Type: model.ColumnText}
	fluxSuspendedColumn = model.Column{ID: idSuspended, Title: "Suspended", Type: model.ColumnText, Width: 90}
	fluxMessageColumn   = model.Column{ID: idMessage, Title: "Message", Type: model.ColumnText}
)

// fluxColumn is a kind's own column: its cell, and whether it is a searchable field too (its id
// names the field, its cell's text the value).
type fluxColumn struct {
	model.Column
	cell  func(obj *unstructured.Unstructured) model.Cell
	field bool
}

// fluxSpec describes one Flux kind; profile builds its curated profile.
type fluxSpec struct {
	group, resource, kind, title string
	version                      string
	fallback                     []string
	section                      string
	suspend, reconcile, static   bool
	// staticOf names the objects of a kind the controller does not reconcile, which report no
	// status (an OCI HelmRepository); nil: static decides for every object.
	staticOf func(obj *unstructured.Unstructured) bool

	// columns are the kind's own, after Namespace.
	columns []fluxColumn
	// revision returns the Revision column's text; nil: the kind has no revision.
	revision func(obj *unstructured.Unstructured) string
	// properties adds the kind's own drawer items.
	properties func(obj *unstructured.Unstructured, d *detailItems)
	// sections returns the kind's drawer sections after Properties (a Helm release's history).
	sections func(obj *unstructured.Unstructured) []model.DetailSection

	built *model.KindProfile
}

func (s *fluxSpec) key() string { return s.resource + "." + s.group }

// isStatic reports whether obj is an object its controller reports no status for: every object
// of a static kind, and those staticOf names. Such an object without a Ready condition is ready
// unless suspended, as the flux CLI shows it.
func (s *fluxSpec) isStatic(obj *unstructured.Unstructured) bool {
	return s.static || (s.staticOf != nil && s.staticOf(obj))
}

func (s *fluxSpec) info() FluxKind {
	return FluxKind{Key: s.key(), APIKind: s.kind, Title: s.title, Section: s.section, Suspend: s.suspend, Reconcile: s.reconcile, static: s.static}
}

// profile builds the kind's profile: Name, Namespace, the kind's columns, Revision (when it has
// one), Suspended, Age, Status (the state) and Message (the Ready condition's).
func (s *fluxSpec) profile() *model.KindProfile {
	cols := []model.Column{nameColumn, namespaceColumn}
	fields := map[string]fieldFunc{
		idStatus: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(FluxStateText(fluxStatusOf(obj, s.isStatic(obj)).state))
		},
		idSuspended: func(obj *unstructured.Unstructured) []string { return []string{fluxSuspendedText(obj)} },
	}
	for _, c := range s.columns {
		cols = append(cols, c.Column)
		if c.field {
			cell := c.cell
			fields[c.ID] = func(obj *unstructured.Unstructured) []string { return nonEmpty(cell(obj).Text) }
		}
	}
	if s.revision != nil {
		cols = append(cols, fluxRevisionColumn)
		fields[idRevision] = func(obj *unstructured.Unstructured) []string { return nonEmpty(s.revision(obj)) }
	}
	cols = append(cols, fluxSuspendedColumn, ageColumn, statusColumn, fluxMessageColumn)
	return &model.KindProfile{
		Key: s.key(), Group: s.group, Version: s.version, FallbackVersions: s.fallback,
		Resource: s.resource, Kind: s.kind, Title: s.title, Namespaced: true,
		Columns: cols,
		Cells:   s.cells,
		Fields:  fields,
		Detail:  s.detail,
	}
}

func (s *fluxSpec) cells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	st := fluxStatusOf(obj, s.isStatic(obj))
	token := FluxStateToken(st.state)
	cells := make([]model.Cell, 0, 7+len(s.columns))
	cells = append(cells, nameCell(obj), namespaceCell(obj))
	for _, c := range s.columns {
		cells = append(cells, c.cell(obj))
	}
	if s.revision != nil {
		cells = append(cells, model.Cell{Text: s.revision(obj)})
	}
	cells = append(cells,
		model.Cell{Text: fluxSuspendedText(obj)},
		model.AgeCell(created(obj)),
		model.Cell{Text: FluxStateText(st.state), Status: token},
		model.Cell{Text: st.message},
	)
	return cells, token
}

// detail is the drawer: the kind's properties with the interval, the timeout, the suspension and
// the last reconcile request the controller handled; the kind's own sections; the conditions.
func (s *fluxSpec) detail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	st := fluxStatusOf(obj, s.isStatic(obj))
	d.addStatus(titleStatus, FluxStateText(st.state), FluxStateToken(st.state))
	d.add("Message", readyMessage(obj))
	if s.properties != nil {
		s.properties(obj, &d)
	}
	if s.revision != nil {
		d.add("Revision", s.revision(obj))
	}
	d.add("Interval", model.NestedString(obj, fieldSpec, "interval"))
	d.add("Timeout", model.NestedString(obj, fieldSpec, "timeout"))
	if s.suspend || IsFluxSuspended(obj) {
		d.add("Suspended", fluxSuspendedText(obj))
	}
	d.add("Reconcile Handled", fluxRequestTime(model.NestedString(obj, "status", "lastHandledReconcileAt")))
	sections := withSection(nil, model.TitleProperties, d)
	if s.sections != nil {
		sections = append(sections, s.sections(obj)...)
	}
	if cs, ok := conditionSection(conditions(obj, "status", "conditions"), fluxConditionToken); ok {
		sections = append(sections, cs)
	}
	return sections
}

// fluxRequestTime renders a reconcile request's token as the controller echoes it
// (status.lastHandledReconcileAt): a time mark when it is a time, as the flux CLI and Cogpit write
// it (RFC 3339), else the token as written.
func fluxRequestTime(token string) string {
	if t, err := time.Parse(time.RFC3339Nano, token); err == nil {
		return timestamp(t)
	}
	return token
}

// readyMessage is the Ready condition's message as the controller wrote it (several lines kept).
func readyMessage(obj any) string {
	for _, c := range model.NestedSlice(obj, "status", "conditions") {
		if model.NestedString(c, "type") == fluxCondReady {
			return model.NestedString(c, "message")
		}
	}
	return ""
}

// fluxSuspendedText is the Suspended column: "true" or "false", as the Cron Jobs' Suspend column
// (captured).
func fluxSuspendedText(obj any) string {
	if IsFluxSuspended(obj) {
		return textTrue
	}
	return textFalse
}

// fluxRef reads a Flux object reference (a sourceRef, a chartRef, a providerRef) at the path:
// "Kind/name" ("Kind/namespace/name" when it names another namespace), and a link to the object
// when Cogpit knows its kind. A reference that names no kind (a providerRef, whose kind its field
// implies) reads "name" and takes defaultKind for its link.
func fluxRef(obj *unstructured.Unstructured, defaultKind string, fields ...string) (string, *model.ObjectRef) {
	ref := model.NestedMap(obj, fields...)
	name := model.NestedString(ref, fieldName)
	if name == "" {
		return "", nil
	}
	ns := model.NestedString(ref, "namespace")
	if ns == "" {
		ns = obj.GetNamespace()
	}
	text := name
	if ns != obj.GetNamespace() {
		text = ns + "/" + name
	}
	kind := model.NestedString(ref, idKind)
	if kind != "" {
		text = kind + "/" + text
	} else {
		kind = defaultKind
	}
	var link *model.ObjectRef
	if key := fluxRefKey(kind); key != "" {
		link = linkTo(key, ns, name)
	}
	return text, link
}

// fluxRefCell is a reference as a linked cell.
func fluxRefCell(defaultKind string, fields ...string) func(obj *unstructured.Unstructured) model.Cell {
	return func(obj *unstructured.Unstructured) model.Cell {
		text, link := fluxRef(obj, defaultKind, fields...)
		return model.Cell{Text: text, Link: link}
	}
}

// fluxRefItem is a reference as a drawer item.
func fluxRefItem(obj *unstructured.Unstructured, name, defaultKind string, fields ...string) model.DetailItem {
	text, link := fluxRef(obj, defaultKind, fields...)
	return linkedItem(name, text, link)
}

// fluxDependsOn lists spec.dependsOn as chips that link to the objects of the same kind key.
func fluxDependsOn(obj *unstructured.Unstructured, key string) model.DetailItem {
	var chips []model.Chip
	for _, dep := range model.NestedSlice(obj, fieldSpec, "dependsOn") {
		name := model.NestedString(dep, fieldName)
		if name == "" {
			continue
		}
		ns := model.NestedString(dep, "namespace")
		text := name
		if ns == "" {
			ns = obj.GetNamespace()
		} else if ns != obj.GetNamespace() {
			text = ns + "/" + name
		}
		chips = append(chips, model.Chip{Text: text, Link: linkTo(key, ns, name)})
	}
	return model.ChipsItem("Depends On", chips)
}

// fluxSecretItem is a secretRef as a link to the Secret (its name only, never its data).
func fluxSecretItem(obj *unstructured.Unstructured, name string, fields ...string) model.DetailItem {
	secret := model.NestedString(obj, append(fields, fieldName)...)
	return linkedItem(name, secret, linkTo(keySecrets, obj.GetNamespace(), secret))
}

// fluxArtifactRevision is a source's revision: status.artifact.revision.
func fluxArtifactRevision(obj *unstructured.Unstructured) string {
	return model.NestedString(obj, "status", "artifact", "revision")
}

// fluxArtifactItems adds a source's artifact: its digest and when it was last updated.
func fluxArtifactItems(obj *unstructured.Unstructured, d *detailItems) {
	d.add("Artifact Digest", model.NestedString(obj, "status", "artifact", "digest"))
	d.add("Artifact Updated", timestamp(model.NestedTime(obj, "status", "artifact", "lastUpdateTime")))
}

// fluxBool renders an optional boolean field: "" when unset.
func fluxBool(obj any, fields ...string) string {
	v, ok := model.NestedBool(obj, fields...)
	switch {
	case !ok:
		return ""
	case v:
		return textTrue
	}
	return textFalse
}

// fluxURL is a URL with any user information in it masked: a source's URL may carry a token
// ("https://user:token@host/…", "https://token@host/…"), which must reach no table, search or
// agent (AGENTS.md rule 9). SSH's conventional "git@" stays.
func fluxURL(s string) string {
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok {
		return s
	}
	host := rest
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		host = rest[:i]
	}
	at := strings.LastIndex(host, "@")
	if at < 0 || host[:at] == "git" {
		return s
	}
	return scheme + "://" + model.Redacted + "@" + rest[at+1:]
}

// fluxURLCell is a spec field holding a URL, masked by fluxURL.
func fluxURLCell(fields ...string) func(obj *unstructured.Unstructured) model.Cell {
	return func(obj *unstructured.Unstructured) model.Cell {
		return model.Cell{Text: fluxURL(model.NestedString(obj, fields...))}
	}
}

// fluxTextCell is a string field as a cell, def when it is unset.
func fluxTextCell(def string, fields ...string) func(obj *unstructured.Unstructured) model.Cell {
	return func(obj *unstructured.Unstructured) model.Cell {
		if v := model.NestedString(obj, fields...); v != "" {
			return model.Cell{Text: v}
		}
		return model.Cell{Text: def}
	}
}
