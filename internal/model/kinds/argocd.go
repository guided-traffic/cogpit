package kinds

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Argo CD (resource-parity › Paid Lens Desktop views › Argo CD, G23, D-9): curated profiles of the
// three kinds of Argo CD's custom resource definitions (argoproj.io/v1alpha1 Application,
// ApplicationSet, AppProject). Lens's documentation (read 2026-10-13, "Using Argo CD"; not
// captured) gives each list's columns: Applications Name, Namespace, Sync, Health, Project,
// Destination, Revision, Age; Application Sets Name, Namespace, Generators, Age; Projects Name,
// Namespace, Description, Source Repos, Age. The other columns are Cogpit's and start hidden; every
// text, colour and the drawers are inferred from Argo CD's API, not from Lens. Argo CD's settings
// (repositories, clusters) are Secrets, not custom resources: argocd_settings.go.

// Kind keys of Argo CD's kinds (architecture §4.5: "<plural>.<group>").
const (
	ArgoCDGroup           = "argoproj.io"
	ArgoApplicationsKey   = "applications." + ArgoCDGroup
	ArgoApplicationSetKey = "applicationsets." + ArgoCDGroup
	ArgoProjectsKey       = "appprojects." + ArgoCDGroup
	argoVersion           = "v1alpha1"
	kindArgoApplication   = "Application"
	kindArgoAppSet        = "ApplicationSet"
	kindArgoProject       = "AppProject"
)

// Argo CD's sync and health states as the Application's status writes them.
const (
	ArgoSynced      = "Synced"
	ArgoOutOfSync   = "OutOfSync"
	ArgoHealthy     = "Healthy"
	ArgoProgressing = "Progressing"
	ArgoDegraded    = "Degraded"
	ArgoSuspended   = "Suspended"
	ArgoMissing     = "Missing"
	argoUnknown     = "Unknown"
	// ArgoPhaseRunning and ArgoPhaseTerminating are the phases of an operation in progress
	// (status.operationState.phase).
	ArgoPhaseRunning     = "Running"
	ArgoPhaseTerminating = "Terminating"
	argoPhaseFailed      = "Failed"
	argoPhaseError       = "Error"
	argoPhaseSucceeded   = "Succeeded"
)

// argoInCluster is the API server address Argo CD names its own cluster by, and that name.
const (
	argoInClusterServer = "https://kubernetes.default.svc"
	argoInClusterName   = "in-cluster"
)

// Column ids and titles of the Argo CD profiles.
const (
	idArgoSync        = "sync"
	idArgoHealth      = "health"
	idArgoProject     = "project"
	idArgoDest        = "destination"
	idArgoRevision    = "revision"
	idArgoRepository  = "repository"
	idArgoTarget      = "targetRevision"
	idArgoPath        = fieldPath
	idArgoMessage     = "message"
	idArgoGenerators  = "generators"
	idArgoApps        = "applications"
	titleArgoProject  = "Project"
	titleArgoRevision = "Revision"
	titleArgoRepo     = "Repository"
	titleArgoMessage  = "Message"
	titleArgoServer   = "Server"
	titleApplications = "Applications"
	titleArgoSync     = "Sync"
	titleArgoHealth   = "Health"
	textYes           = "Yes"
	fieldArgoSource   = "source"
	fieldArgoSources  = "sources"
	fieldArgoRepoURL  = "repoURL"
	fieldArgoTarget   = "targetRevision"
	fieldArgoDest     = "destination"
	fieldArgoServer   = "server"
	fieldArgoStatus   = "status"
	fieldArgoOpState  = "operationState"
	fieldArgoSyncPol  = "syncPolicy"
	fieldArgoMessage  = "message"
	fieldArgoRevision = "revision"
)

// Bounds of the Application drawer's tables: a large application manages thousands of objects.
const (
	argoResourcesShown = 500
	argoHistoryShown   = 20
	argoRevisionShort  = 7 // a Git commit as Argo CD's UI shortens it
)

// ArgoProfiles returns the curated profiles of Argo CD's kinds, in the navigator's order.
func ArgoProfiles() []*model.KindProfile {
	return []*model.KindProfile{argoApplications, argoApplicationSets, argoProjects}
}

// The curated profiles join the shared list here, so that no shared file grows (Flux CD's do the
// same in their own file).
func init() { curated = append(curated, argoApplications, argoApplicationSets, argoProjects) }

var argoApplications = &model.KindProfile{
	Key:        ArgoApplicationsKey,
	Group:      ArgoCDGroup,
	Version:    argoVersion,
	Resource:   "applications",
	Kind:       kindArgoApplication,
	Title:      titleApplications,
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: idArgoSync, Title: titleArgoSync, Type: model.ColumnText, Width: 100},
		{ID: idArgoHealth, Title: titleArgoHealth, Type: model.ColumnText, Width: 100},
		{ID: idArgoProject, Title: titleArgoProject, Type: model.ColumnText, Width: 110},
		{ID: idArgoDest, Title: "Destination", Type: model.ColumnText},
		{ID: idArgoRevision, Title: titleArgoRevision, Type: model.ColumnText, Width: 90},
		ageColumn,
		{ID: idArgoRepository, Title: titleArgoRepo, Type: model.ColumnText, Hidden: true},
		{ID: idArgoTarget, Title: "Target Revision", Type: model.ColumnText, Hidden: true},
		{ID: idArgoPath, Title: "Path/Chart", Type: model.ColumnText, Hidden: true},
		{ID: idArgoMessage, Title: titleArgoMessage, Type: model.ColumnText, Hidden: true},
	},
	Cells: argoAppCells,
	Fields: map[string]fieldFunc{
		idArgoProject: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, fieldSpec, idArgoProject))
		},
		idArgoSync:       func(obj *unstructured.Unstructured) []string { return nonEmpty(ArgoSyncStatus(obj)) },
		idArgoHealth:     func(obj *unstructured.Unstructured) []string { return nonEmpty(ArgoHealthStatus(obj)) },
		idArgoRepository: func(obj *unstructured.Unstructured) []string { return argoSourceValues(obj, fieldArgoRepoURL) },
		idArgoTarget:     func(obj *unstructured.Unstructured) []string { return argoSourceValues(obj, fieldArgoTarget) },
		idArgoPath:       func(obj *unstructured.Unstructured) []string { return argoPaths(obj) },
		idArgoDest:       func(obj *unstructured.Unstructured) []string { return nonEmpty(argoDestination(obj)) },
		idArgoRevision:   func(obj *unstructured.Unstructured) []string { return argoRevisions(obj) },
	},
	Detail: argoAppDetail,
}

// ArgoSyncStatus is an Application's status.sync.status: Synced, OutOfSync or Unknown; "" before
// the controller has compared it.
func ArgoSyncStatus(obj any) string {
	return model.NestedString(obj, fieldArgoStatus, "sync", fieldArgoStatus)
}

// ArgoHealthStatus is an Application's status.health.status: Healthy, Progressing, Degraded,
// Suspended, Missing or Unknown; "" before the controller has assessed it.
func ArgoHealthStatus(obj any) string {
	return model.NestedString(obj, fieldArgoStatus, idArgoHealth, fieldArgoStatus)
}

// ArgoOperationPhase is the phase of an Application's last or current operation
// (status.operationState.phase), "" without one.
func ArgoOperationPhase(obj any) string {
	return model.NestedString(obj, fieldArgoStatus, fieldArgoOpState, "phase")
}

// ArgoOperationBusy says why an Application cannot take a new operation now, or "": an operation
// was requested and the controller has not taken it yet (the object's operation field is set), or
// one is running or terminating (status.operationState.phase). argocd app sync refuses the same.
func ArgoOperationBusy(obj any) string {
	if v, ok := model.NestedValue(obj, "operation"); ok && v != nil {
		return "an operation was requested and has not finished yet"
	}
	switch phase := ArgoOperationPhase(obj); phase {
	case ArgoPhaseRunning, ArgoPhaseTerminating:
		return "an operation is in progress (phase " + phase + ")"
	}
	return ""
}

// ArgoSyncToken colours a sync status (inferred): Synced is good, OutOfSync a warning.
func ArgoSyncToken(sync string) model.Status {
	switch sync {
	case ArgoSynced:
		return model.StatusOK
	case ArgoOutOfSync:
		return model.StatusWarning
	case "":
		return model.StatusNone
	}
	return model.StatusUnknown
}

// ArgoHealthToken colours a health status as Argo CD's own UI does (inferred): Healthy good,
// Progressing pending, Degraded an error, Missing a warning, Suspended stopped.
func ArgoHealthToken(health string) model.Status {
	switch health {
	case ArgoHealthy:
		return model.StatusOK
	case ArgoProgressing:
		return model.StatusPending
	case ArgoDegraded:
		return model.StatusError
	case ArgoMissing:
		return model.StatusWarning
	case ArgoSuspended:
		return model.StatusStopped
	case "":
		return model.StatusNone
	}
	return model.StatusUnknown
}

// argoAppStatus is an Application's row status: health first, then sync (inferred). A Degraded
// application, or one with an error condition or a failed operation, is an error; a Missing one a
// warning; one progressing or with an operation in progress pending; an OutOfSync one a warning;
// a Suspended one stopped; Healthy and Synced is good; anything not known yet unknown.
func argoAppStatus(obj any) model.Status {
	health, sync := ArgoHealthStatus(obj), ArgoSyncStatus(obj)
	switch {
	case health == ArgoDegraded, argoHasError(obj):
		return model.StatusError
	case health == ArgoMissing:
		return model.StatusWarning
	case health == ArgoProgressing, ArgoOperationBusy(obj) != "":
		return model.StatusPending
	case sync == ArgoOutOfSync:
		return model.StatusWarning
	case health == ArgoSuspended:
		return model.StatusStopped
	case health == ArgoHealthy && sync == ArgoSynced:
		return model.StatusOK
	}
	return model.StatusUnknown
}

// argoHasError reports an error condition (ComparisonError, SyncError, InvalidSpecError, …) or a
// last operation that failed.
func argoHasError(obj any) bool {
	for _, c := range model.NestedSlice(obj, fieldArgoStatus, "conditions") {
		if strings.HasSuffix(model.NestedString(c, "type"), "Error") {
			return true
		}
	}
	switch ArgoOperationPhase(obj) {
	case argoPhaseFailed, argoPhaseError:
		return true
	}
	return false
}

// ArgoAppMessage is what an Application says about its trouble, for the Message column and the
// dashboard: the first error or warning condition's message, else a failed operation's, else the
// health message; "" when it says nothing.
func ArgoAppMessage(obj any) string {
	for _, c := range model.NestedSlice(obj, fieldArgoStatus, "conditions") {
		if m := model.NestedString(c, fieldArgoMessage); m != "" {
			return singleLine(m)
		}
	}
	switch ArgoOperationPhase(obj) {
	case argoPhaseFailed, argoPhaseError, ArgoPhaseRunning, ArgoPhaseTerminating:
		if m := model.NestedString(obj, fieldArgoStatus, fieldArgoOpState, fieldArgoMessage); m != "" {
			return singleLine(m)
		}
	}
	return singleLine(model.NestedString(obj, fieldArgoStatus, idArgoHealth, fieldArgoMessage))
}

func argoAppCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	sync, health := ArgoSyncStatus(obj), ArgoHealthStatus(obj)
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: sync, Status: ArgoSyncToken(sync)},
		{Text: health, Status: ArgoHealthToken(health)},
		{Text: model.NestedString(obj, fieldSpec, idArgoProject), Link: argoProjectLink(obj)},
		{Text: argoDestination(obj)},
		{Text: strings.Join(argoShortRevisions(obj), ", ")},
		model.AgeCell(created(obj)),
		{Text: strings.Join(argoSourceValues(obj, fieldArgoRepoURL), ", ")},
		{Text: strings.Join(argoSourceValues(obj, fieldArgoTarget), ", ")},
		{Text: strings.Join(argoPaths(obj), ", ")},
		{Text: ArgoAppMessage(obj)},
	}, argoAppStatus(obj)
}

// ArgoSources returns an Application's sources: spec.sources when it has several (Argo CD's
// multi-source applications), else spec.source; nil without either.
func ArgoSources(obj any) []any {
	if list := model.NestedSlice(obj, fieldSpec, fieldArgoSources); len(list) > 0 {
		return list
	}
	if src := model.NestedMap(obj, fieldSpec, fieldArgoSource); src != nil {
		return []any{src}
	}
	return nil
}

// argoSourceValues returns a field of every source, empty ones left out: the repositories, the
// target revisions.
func argoSourceValues(obj any, field string) []string {
	var out []string
	for _, s := range ArgoSources(obj) {
		out = appendUnique(out, model.NestedString(s, field))
	}
	return out
}

// argoPaths returns each source's path, or its chart for a Helm repository's chart.
func argoPaths(obj any) []string {
	var out []string
	for _, s := range ArgoSources(obj) {
		p := model.NestedString(s, idArgoPath)
		if p == "" {
			p = model.NestedString(s, "chart")
		}
		out = appendUnique(out, p)
	}
	return out
}

// argoDestination renders spec.destination as "<cluster>/<namespace>": the cluster by its name,
// else its server, Argo CD's own server as in-cluster (inferred).
func argoDestination(obj any) string {
	dest := model.NestedMap(obj, fieldSpec, fieldArgoDest)
	cluster := model.NestedString(dest, "name")
	if cluster == "" {
		cluster = model.NestedString(dest, fieldArgoServer)
	}
	if cluster == argoInClusterServer {
		cluster = argoInClusterName
	}
	ns := model.NestedString(dest, "namespace")
	switch {
	case cluster == "":
		return ns
	case ns == "":
		return cluster
	}
	return cluster + "/" + ns
}

// argoRevisions returns the revisions the application is synced to: status.sync.revision, or
// status.sync.revisions of a multi-source application.
func argoRevisions(obj any) []string {
	if r := model.NestedString(obj, fieldArgoStatus, "sync", fieldArgoRevision); r != "" {
		return []string{r}
	}
	var out []string
	for _, r := range model.NestedStrings(obj, fieldArgoStatus, "sync", "revisions") {
		out = appendUnique(out, r)
	}
	return out
}

// argoShortRevisions shortens Git commits to seven characters, as Argo CD's UI does; a chart
// version or a tag stays as it is.
func argoShortRevisions(obj any) []string {
	revs := argoRevisions(obj)
	out := make([]string, len(revs))
	for i, r := range revs {
		out[i] = ArgoShortRevision(r)
	}
	return out
}

// ArgoShortRevision shortens a Git commit (40 or 64 hexadecimal characters) to seven.
func ArgoShortRevision(r string) string {
	if (len(r) == 40 || len(r) == 64) && isHex(r) {
		return r[:argoRevisionShort]
	}
	return r
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// argoProjectLink links an Application's project, an AppProject in Argo CD's own namespace
// (ArgoProjectNamespace).
func argoProjectLink(obj any) *model.ObjectRef {
	return linkTo(ArgoProjectsKey, ArgoProjectNamespace(obj), model.NestedString(obj, fieldSpec, idArgoProject))
}

// ArgoProjectNamespace is the namespace of an Application's AppProject: Argo CD keeps its
// projects in the namespace its controller runs in, which the controller writes into the
// Application's status.controllerNamespace; an Application in another namespace (Argo CD's
// "Applications in any namespace") finds its project there, not in its own namespace (checked
// against a live Argo CD controller on a real cluster, 2026-10-16). Before the controller has
// written it, the Application's own namespace.
func ArgoProjectNamespace(obj any) string {
	if ns := model.NestedString(obj, fieldArgoStatus, "controllerNamespace"); ns != "" {
		return ns
	}
	return model.NestedString(obj, "metadata", "namespace")
}

func argoAppDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	sync, health := ArgoSyncStatus(obj), ArgoHealthStatus(obj)
	d.put(linkedItem(titleArgoProject, model.NestedString(obj, fieldSpec, idArgoProject), argoProjectLink(obj)))
	d.addStatus("Sync Status", sync, ArgoSyncToken(sync))
	d.addStatus("Health", health, ArgoHealthToken(health))
	d.add("Health Message", model.NestedString(obj, fieldArgoStatus, idArgoHealth, fieldArgoMessage))
	d.add(titleArgoRevision, strings.Join(argoRevisions(obj), "\n"))
	if owner := argoOwner(obj, kindArgoAppSet); owner != "" {
		d.put(linkedItem("Application Set", owner, linkTo(ArgoApplicationSetKey, obj.GetNamespace(), owner)))
	}
	sourceTypes := model.NestedStrings(obj, fieldArgoStatus, "sourceTypes")
	if len(sourceTypes) == 0 {
		sourceTypes = nonEmpty(model.NestedString(obj, fieldArgoStatus, "sourceType"))
	}
	d.add("Source Type", strings.Join(sourceTypes, ", "))
	d.add("Reconciled", timestamp(model.NestedTime(obj, fieldArgoStatus, "reconciledAt")))
	d.add("Images", strings.Join(model.NestedStrings(obj, fieldArgoStatus, "summary", "images"), "\n"))
	d.add("External URLs", strings.Join(model.NestedStrings(obj, fieldArgoStatus, "summary", "externalURLs"), "\n"))
	sections := withSection(nil, model.TitleProperties, d)
	sections = append(sections, argoSourceSections(obj)...)
	sections = withSection(sections, "Destination", argoDestinationItems(obj))
	sections = withSection(sections, "Sync Policy", argoSyncPolicyItems(obj))
	if cs, ok := conditionSection(argoConditions(obj), argoConditionToken); ok {
		sections = append(sections, cs)
	}
	sections = withSection(sections, "Operation", argoOperationItems(obj))
	if s, ok := argoResourcesSection(obj); ok {
		sections = append(sections, s)
	}
	if s, ok := argoHistorySection(obj); ok {
		sections = append(sections, s)
	}
	return sections
}

// argoOwner returns the name of the owner of a kind (ApplicationSet), or "".
func argoOwner(obj *unstructured.Unstructured, kind string) string {
	for _, o := range obj.GetOwnerReferences() {
		if o.Kind == kind && strings.HasPrefix(o.APIVersion, ArgoCDGroup+"/") {
			return o.Name
		}
	}
	return ""
}

// argoSourceSections renders each source: "Source", or "Source 1", "Source 2" of a multi-source
// application.
func argoSourceSections(obj any) []model.DetailSection {
	srcs := ArgoSources(obj)
	var out []model.DetailSection
	for i, s := range srcs {
		var d detailItems
		d.add(titleArgoRepo, model.NestedString(s, fieldArgoRepoURL))
		d.add("Target Revision", model.NestedString(s, fieldArgoTarget))
		d.add("Path", model.NestedString(s, idArgoPath))
		d.add("Chart", model.NestedString(s, "chart"))
		d.add("Ref", model.NestedString(s, "ref"))
		d.add("Name", model.NestedString(s, "name"))
		d.add("Helm Release Name", model.NestedString(s, "helm", "releaseName"))
		d.add("Helm Value Files", strings.Join(model.NestedStrings(s, "helm", "valueFiles"), "\n"))
		d.add("Kustomize Images", strings.Join(model.NestedStrings(s, "kustomize", "images"), "\n"))
		d.add("Plugin", model.NestedString(s, "plugin", "name"))
		if rec, ok := model.NestedBool(s, "directory", "recurse"); ok && rec {
			d.add("Directory", "recursive")
		}
		title := "Source"
		if len(srcs) > 1 {
			title += " " + strconv.Itoa(i+1)
		}
		out = withSection(out, title, d)
	}
	return out
}

func argoDestinationItems(obj any) detailItems {
	var d detailItems
	dest := model.NestedMap(obj, fieldSpec, fieldArgoDest)
	d.add("Cluster", model.NestedString(dest, "name"))
	d.add(titleArgoServer, model.NestedString(dest, fieldArgoServer))
	d.add("Namespace", model.NestedString(dest, "namespace"))
	return d
}

// argoSyncPolicyItems renders spec.syncPolicy: automated or manual, with prune and self-heal.
func argoSyncPolicyItems(obj any) detailItems {
	var d detailItems
	pol := model.NestedMap(obj, fieldSpec, fieldArgoSyncPol)
	auto, isAuto := model.NestedValue(pol, "automated")
	enabled := isAuto && auto != nil
	if e, ok := model.NestedBool(pol, "automated", "enabled"); ok {
		enabled = enabled && e
	}
	if !enabled {
		d.add("Automated", "No (manual sync)")
	} else {
		d.add("Automated", textYes)
		d.add("Prune", yesNo(pol, "automated", "prune"))
		d.add("Self Heal", yesNo(pol, "automated", "selfHeal"))
		d.add("Allow Empty", yesNo(pol, "automated", "allowEmpty"))
	}
	d.add("Sync Options", strings.Join(model.NestedStrings(pol, "syncOptions"), "\n"))
	if n, ok := model.NestedInt64(pol, "retry", "limit"); ok {
		d.add("Retry Limit", itoa(n))
	}
	return d
}

// yesNo renders a boolean field: "Yes", or "No" when false or unset.
func yesNo(root any, fields ...string) string {
	if v, _ := model.NestedBool(root, fields...); v {
		return textYes
	}
	return "No"
}

// argoConditions reads the Application's conditions: Argo CD's carry no status, only a type and a
// message.
func argoConditions(obj any) []condition {
	var out []condition
	for _, e := range model.NestedSlice(obj, fieldArgoStatus, "conditions") {
		c := condition{Type: model.NestedString(e, "type"), Message: model.NestedString(e, fieldArgoMessage)}
		if c.Type != "" {
			c.Status = condTrue
			out = append(out, c)
		}
	}
	return out
}

// argoConditionToken colours an Application condition by its type: "…Error" an error,
// "…Warning" a warning.
func argoConditionToken(c condition) model.Status {
	switch {
	case strings.HasSuffix(c.Type, "Error"):
		return model.StatusError
	case strings.HasSuffix(c.Type, "Warning"):
		return model.StatusWarning
	}
	return model.StatusNone
}

// argoPhaseToken colours an operation's phase.
func argoPhaseToken(phase string) model.Status {
	switch phase {
	case argoPhaseSucceeded:
		return model.StatusOK
	case ArgoPhaseRunning, ArgoPhaseTerminating:
		return model.StatusPending
	case argoPhaseFailed, argoPhaseError:
		return model.StatusError
	}
	return model.StatusNone
}

// argoOperationItems renders the requested operation and the state of the last one.
func argoOperationItems(obj any) detailItems {
	var d detailItems
	if op := model.NestedMap(obj, "operation"); op != nil {
		d.addStatus("Requested", "Sync"+argoInitiator(model.NestedMap(op, "initiatedBy")), model.StatusPending)
	}
	st := model.NestedMap(obj, fieldArgoStatus, fieldArgoOpState)
	if st == nil {
		return d
	}
	phase := model.NestedString(st, "phase")
	d.addStatus("Phase", phase, argoPhaseToken(phase))
	d.add(titleArgoMessage, model.NestedString(st, fieldArgoMessage))
	if by := argoInitiator(model.NestedMap(st, "operation", "initiatedBy")); by != "" {
		d.add("Initiated By", strings.TrimPrefix(by, " by "))
	}
	d.add(titleArgoRevision, model.NestedString(st, "syncResult", fieldArgoRevision))
	if prune, _ := model.NestedBool(st, "operation", "sync", "prune"); prune {
		d.add("Prune", textYes)
	}
	d.add("Started", timestamp(model.NestedTime(st, "startedAt")))
	d.add("Finished", timestamp(model.NestedTime(st, "finishedAt")))
	if n, ok := model.NestedInt64(st, "retryCount"); ok && n > 0 {
		d.add("Retries", itoa(n))
	}
	return d
}

// argoInitiator says who started an operation: " by <user>", " by the controller (automated)".
func argoInitiator(by map[string]any) string {
	if u := model.NestedString(by, "username"); u != "" {
		return " by " + u
	}
	if auto, _ := model.NestedBool(by, "automated"); auto {
		return " by the controller (automated)"
	}
	return ""
}

// argoResourceHealthInTree is the Application's status.resourceHealthSource when Argo CD keeps
// each resource's health in its resource tree rather than in status.resources (Argo CD 3's
// default).
const argoResourceHealthInTree = "appTree"

// ArgoResourceHealthNote is what the Resources section says in place of a Health column when Argo
// CD keeps the resources' health in its resource tree: Cogpit reads only the Application, never
// Argo CD's API server, so a resource's state is in its own drawer.
const ArgoResourceHealthNote = "In Argo CD's resource tree, not in the Application (appTree)\nOpen a resource for its state"

// argoResourcesSection is the table of the objects the Application manages (status.resources):
// Kind, Namespace, Name, Sync, Health; a name links to the object when it is of a kind Cogpit knows
// without discovery (a built-in kind, or one of Argo CD's). The Health column is left out when no
// resource carries a health: Argo CD 3 keeps it in its resource tree by default
// (status.resourceHealthSource: appTree, seen on a live Argo CD 3.5 controller on a real cluster,
// 2026-10-16), and the section says so instead of showing an empty column.
func argoResourcesSection(obj any) (model.DetailSection, bool) {
	list := model.NestedSlice(obj, fieldArgoStatus, "resources")
	if len(list) == 0 {
		return model.DetailSection{}, false
	}
	shown := list[:min(len(list), argoResourcesShown)]
	withHealth := false
	for _, r := range shown {
		withHealth = withHealth || model.NestedString(r, idArgoHealth, fieldArgoStatus) != ""
	}
	cols := []model.Column{
		{Title: titleKind, Type: model.ColumnText}, {Title: namespaceColumn.Title, Type: model.ColumnText},
		{Title: titleName, Type: model.ColumnText}, {Title: titleArgoSync, Type: model.ColumnText},
	}
	if withHealth {
		cols = append(cols, model.Column{Title: titleArgoHealth, Type: model.ColumnText})
	}
	rows := make([][]model.Cell, 0, len(shown))
	for _, r := range shown {
		ns, name := model.NestedString(r, "namespace"), model.NestedString(r, "name")
		sync := model.NestedString(r, fieldArgoStatus)
		kind := model.NestedString(r, idKind)
		row := []model.Cell{
			{Text: kind},
			{Text: ns, Link: linkTo(keyNamespaces, "", ns)},
			{Text: name, Link: argoResourceLink(model.NestedString(r, "group"), model.NestedString(r, "version"), kind, ns, name)},
			{Text: sync, Status: ArgoSyncToken(sync)},
		}
		if withHealth {
			health := model.NestedString(r, idArgoHealth, fieldArgoStatus)
			row = append(row, model.Cell{Text: health, Status: ArgoHealthToken(health)})
		}
		rows = append(rows, row)
	}
	title := titleResources
	if len(list) > len(shown) {
		title += " (the first " + strconv.Itoa(len(shown)) + " of " + strconv.Itoa(len(list)) + ")"
	}
	sec := model.DetailSection{Title: title, Table: model.CellTable(cols, rows)}
	if !withHealth && model.NestedString(obj, fieldArgoStatus, "resourceHealthSource") == argoResourceHealthInTree {
		sec.Items = []model.DetailItem{{Name: "Resource Health", Value: ArgoResourceHealthNote}}
	}
	return sec, true
}

// argoKindKeys are Argo CD's own kinds by Kind, for links from an application's resources (an
// application of applications).
var argoKindKeys = map[string]string{
	kindArgoApplication: ArgoApplicationsKey, kindArgoAppSet: ArgoApplicationSetKey, kindArgoProject: ArgoProjectsKey,
}

func argoResourceLink(group, version, kind, ns, name string) *model.ObjectRef {
	if group == ArgoCDGroup {
		if key, ok := argoKindKeys[kind]; ok {
			return linkTo(key, ns, name)
		}
		return nil
	}
	apiVersion := version
	if group != "" {
		apiVersion = group + "/" + version
	}
	return model.RefTo(apiVersion, kind, ns, name)
}

// argoHistorySection is the table of the last syncs (status.history), newest first.
func argoHistorySection(obj any) (model.DetailSection, bool) {
	list := model.NestedSlice(obj, fieldArgoStatus, "history")
	if len(list) == 0 {
		return model.DetailSection{}, false
	}
	t := &model.DetailTable{Columns: []string{"ID", titleArgoRevision, "Deployed", "Initiated By", titleArgoRepo}}
	for i := len(list) - 1; i >= 0 && len(t.Rows) < argoHistoryShown; i-- {
		h := list[i]
		id := ""
		if n, ok := model.NestedInt64(h, "id"); ok {
			id = itoa(n)
		}
		rev := model.NestedString(h, fieldArgoRevision)
		if rev == "" {
			rev = strings.Join(model.NestedStrings(h, "revisions"), ", ")
		}
		repo := model.NestedString(h, fieldArgoSource, fieldArgoRepoURL)
		if repo == "" {
			var repos []string
			for _, s := range model.NestedSlice(h, fieldArgoSources) {
				repos = appendUnique(repos, model.NestedString(s, fieldArgoRepoURL))
			}
			repo = strings.Join(repos, ", ")
		}
		t.Rows = append(t.Rows, []string{
			id, rev, timestamp(model.NestedTime(h, "deployedAt")),
			strings.TrimPrefix(argoInitiator(model.NestedMap(h, "initiatedBy")), " by "), repo,
		})
	}
	return model.DetailSection{Title: "History", Table: t}, true
}

// ArgoActivity is one entry of the Argo CD dashboard's recent activity: an operation that started
// or finished, or a sync of the history.
type ArgoActivity struct {
	At       time.Time
	Text     string
	Status   model.Status
	Revision string
}

// ArgoAppActivity returns an Application's activity: its current or last operation (when it
// started or finished) and the syncs of its history, newest first (inferred: Lens's timeline is
// not captured).
func ArgoAppActivity(obj any) []ArgoActivity {
	var out []ArgoActivity
	if st := model.NestedMap(obj, fieldArgoStatus, fieldArgoOpState); st != nil {
		phase := model.NestedString(st, "phase")
		at := model.NestedTime(st, "finishedAt")
		if at.IsZero() {
			at = model.NestedTime(st, "startedAt")
		}
		text := "Sync " + strings.ToLower(phase) + argoInitiator(model.NestedMap(st, "operation", "initiatedBy"))
		if m := model.NestedString(st, fieldArgoMessage); m != "" && phase != argoPhaseSucceeded {
			text += ": " + singleLine(m)
		}
		if !at.IsZero() && phase != "" {
			out = append(out, ArgoActivity{At: at, Text: text, Status: argoPhaseToken(phase),
				Revision: ArgoShortRevision(model.NestedString(st, "syncResult", fieldArgoRevision))})
		}
	}
	for _, h := range model.NestedSlice(obj, fieldArgoStatus, "history") {
		at := model.NestedTime(h, "deployedAt")
		if at.IsZero() {
			continue
		}
		dup := false
		for _, a := range out {
			dup = dup || a.At.Equal(at)
		}
		if !dup {
			out = append(out, ArgoActivity{At: at, Text: "Deployed" + argoInitiator(model.NestedMap(h, "initiatedBy")),
				Status: model.StatusOK, Revision: ArgoShortRevision(model.NestedString(h, fieldArgoRevision))})
		}
	}
	sortActivity(out)
	return out
}

// sortActivity orders activity newest first.
func sortActivity(a []ArgoActivity) {
	slices.SortStableFunc(a, func(x, y ArgoActivity) int { return y.At.Compare(x.At) })
}
