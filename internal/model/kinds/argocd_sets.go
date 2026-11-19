package kinds

import (
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Argo CD's Application Sets and Projects (argocd.go has the Applications and the shared rules).

// ArgoAppsByAppSet indexes Argo CD Applications by the ApplicationSet that owns them, as
// "<namespace>/<name>": an Application Set's Applications.
var ArgoAppsByAppSet = model.RelatedIndex{Kind: ArgoApplicationsKey, Name: "byApplicationSet", Keys: argoAppSetKeys}

func argoAppSetKeys(app *unstructured.Unstructured) []string {
	if owner := argoOwner(app, kindArgoAppSet); owner != "" {
		return []string{app.GetNamespace() + "/" + owner}
	}
	return nil
}

// Titles of the Projects' drawer.
const (
	titleArgoDescription = "Description"
	titleArgoRoles       = "Roles"
	titleArgoNamespaces  = "Namespaces"
)

// ApplicationSet condition types (argoproj.io/v1alpha1).
const (
	argoSetErrorOccurred      = "ErrorOccurred"
	argoSetParametersGen      = "ParametersGenerated"
	argoSetResourcesUpToDate  = "ResourcesUpToDate"
	argoSetRolloutProgressing = "RolloutProgressing"
)

var argoApplicationSets = &model.KindProfile{
	Key:        ArgoApplicationSetKey,
	Group:      ArgoCDGroup,
	Version:    argoVersion,
	Resource:   "applicationsets",
	Kind:       kindArgoAppSet,
	Title:      "Application Sets",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: idArgoGenerators, Title: "Generators", Type: model.ColumnText},
		ageColumn,
		{ID: idArgoProject, Title: titleArgoProject, Type: model.ColumnText, Hidden: true},
		{ID: idArgoApps, Title: titleApplications, Type: model.ColumnNumber, Hidden: true, Related: ArgoApplicationsKey},
		{ID: idStatus, Title: titleStatus, Type: model.ColumnText, Hidden: true},
	},
	Needs: []model.RelatedIndex{ArgoAppsByAppSet},
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return argoSetCells(obj, model.NoRelated)
	},
	CellsWith: argoSetCells,
	Fields: map[string]fieldFunc{
		idArgoGenerators: func(obj *unstructured.Unstructured) []string { return argoGenerators(obj) },
		idArgoProject:    func(obj *unstructured.Unstructured) []string { return nonEmpty(argoSetProject(obj)) },
		idStatus: func(obj *unstructured.Unstructured) []string {
			text, _ := argoSetStatus(obj)
			return nonEmpty(text)
		},
	},
	Detail:     func(obj *unstructured.Unstructured) []model.DetailSection { return argoSetDetail(obj, model.NoRelated) },
	DetailWith: argoSetDetail,
}

func argoSetCells(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	text, token := argoSetStatus(obj)
	apps := model.Cell{Text: "-"}
	if rel.Listed(ArgoAppsByAppSet) {
		n := int64(len(rel.Objects(ArgoAppsByAppSet, obj.GetNamespace()+"/"+obj.GetName())))
		apps = model.Cell{Text: itoa(n), Num: n}
	}
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: strings.Join(argoGenerators(obj), ", ")},
		model.AgeCell(created(obj)),
		{Text: argoSetProject(obj), Link: linkTo(ArgoProjectsKey, obj.GetNamespace(), argoSetProject(obj))},
		apps,
		{Text: text, Status: token},
	}, token
}

// argoGenerators returns the types of an ApplicationSet's generators, in order, each once: list,
// clusters, git, matrix, … (a generator is an object with one key, its type).
func argoGenerators(obj any) []string {
	var out []string
	for _, g := range model.NestedSlice(obj, fieldSpec, "generators") {
		m, ok := g.(map[string]any)
		if !ok {
			continue
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			if k != "selector" && k != "values" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		out = appendUnique(out, keys...)
	}
	return out
}

// argoSetProject is the project the template gives its Applications.
func argoSetProject(obj any) string {
	return model.NestedString(obj, fieldSpec, "template", fieldSpec, idArgoProject)
}

// argoSetStatus renders an ApplicationSet's status as the condition types that are True, sorted
// (a generator's error, ErrorOccurred, is the one that matters), and colours it (inferred): an
// error occurred or parameters not generated is an error; a rollout in progress pending; resources
// up to date good; no conditions yet pending (the controller has not seen it).
func argoSetStatus(obj any) (string, model.Status) {
	conds := conditions(obj, fieldArgoStatus, "conditions")
	var trueTypes []string
	is := map[string]string{}
	for _, c := range conds {
		is[c.Type] = c.Status
		if c.Status == condTrue {
			trueTypes = append(trueTypes, c.Type)
		}
	}
	sort.Strings(trueTypes)
	text := strings.Join(trueTypes, " ")
	switch {
	case len(conds) == 0:
		return text, model.StatusPending
	case is[argoSetErrorOccurred] == condTrue, is[argoSetParametersGen] == condFalse:
		return text, model.StatusError
	case is[argoSetRolloutProgressing] == condTrue:
		return text, model.StatusPending
	case is[argoSetResourcesUpToDate] == condTrue:
		return text, model.StatusOK
	case is[argoSetResourcesUpToDate] == condFalse:
		return text, model.StatusWarning
	}
	return text, model.StatusUnknown
}

func argoSetConditionToken(c condition) model.Status {
	switch c.Type {
	case argoSetErrorOccurred:
		return conditionToken(c.Status, model.StatusError, model.StatusNone)
	case argoSetParametersGen:
		return conditionToken(c.Status, model.StatusOK, model.StatusError)
	case argoSetResourcesUpToDate:
		return conditionToken(c.Status, model.StatusOK, model.StatusWarning)
	case argoSetRolloutProgressing:
		return conditionToken(c.Status, model.StatusPending, model.StatusNone)
	}
	return model.StatusNone
}

func argoSetDetail(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	var d detailItems
	text, token := argoSetStatus(obj)
	d.addStatus(titleStatus, text, token)
	d.put(linkedItem(titleArgoProject, argoSetProject(obj), linkTo(ArgoProjectsKey, obj.GetNamespace(), argoSetProject(obj))))
	d.add("Generators", strings.Join(argoGenerators(obj), "\n"))
	tmpl := model.NestedMap(obj, fieldSpec, "template")
	d.add("Application Name", model.NestedString(tmpl, "metadata", "name"))
	var repos []string
	// A new slice: the template's sources alias the cached object, which no one may append to.
	for _, s := range slices.Concat(model.NestedSlice(tmpl, fieldSpec, fieldArgoSources), []any{model.NestedMap(tmpl, fieldSpec, fieldArgoSource)}) {
		repos = appendUnique(repos, model.NestedString(s, fieldArgoRepoURL))
	}
	d.add(titleArgoRepo, strings.Join(repos, "\n"))
	d.add("Destination", argoDestination(tmpl))
	if v, ok := model.NestedBool(obj, fieldSpec, fieldArgoSyncPol, "preserveResourcesOnDeletion"); ok {
		d.add("Preserve Resources on Deletion", boolText(v))
	}
	d.add("Applications Sync", model.NestedString(obj, fieldSpec, fieldArgoSyncPol, "applicationsSync"))
	d.add("Go Template", goTemplate(obj))
	sections := withSection(nil, model.TitleProperties, d)
	if cs, ok := conditionSection(conditions(obj, fieldArgoStatus, "conditions"), argoSetConditionToken); ok {
		sections = append(sections, cs)
	}
	if s, ok := argoSetAppsSection(obj, rel); ok {
		sections = append(sections, s)
	}
	return sections
}

func goTemplate(obj any) string {
	if v, ok := model.NestedBool(obj, fieldSpec, "goTemplate"); ok {
		return boolText(v)
	}
	return ""
}

func boolText(v bool) string {
	if v {
		return textYes
	}
	return "No"
}

// argoSetAppsSection lists the Applications the set owns, from the cache: name (a link), sync and
// health, sorted by name.
func argoSetAppsSection(obj *unstructured.Unstructured, rel model.Related) (model.DetailSection, bool) {
	apps := rel.Objects(ArgoAppsByAppSet, obj.GetNamespace()+"/"+obj.GetName())
	if len(apps) == 0 {
		return model.DetailSection{}, false
	}
	apps = slices.Clone(apps)
	slices.SortFunc(apps, func(a, b *unstructured.Unstructured) int { return strings.Compare(a.GetName(), b.GetName()) })
	cols := []model.Column{{Title: titleName, Type: model.ColumnText}, {Title: titleArgoSync, Type: model.ColumnText}, {Title: titleArgoHealth, Type: model.ColumnText}}
	rows := make([][]model.Cell, 0, len(apps))
	for _, a := range apps {
		sync, health := ArgoSyncStatus(a), ArgoHealthStatus(a)
		rows = append(rows, []model.Cell{
			{Text: a.GetName(), Link: linkTo(ArgoApplicationsKey, a.GetNamespace(), a.GetName())},
			{Text: sync, Status: ArgoSyncToken(sync)},
			{Text: health, Status: ArgoHealthToken(health)},
		})
	}
	return model.DetailSection{Title: titleApplications, Table: model.CellTable(cols, rows)}, true
}

var argoProjects = &model.KindProfile{
	Key:        ArgoProjectsKey,
	Group:      ArgoCDGroup,
	Version:    argoVersion,
	Resource:   "appprojects",
	Kind:       kindArgoProject,
	Title:      "Projects",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: "description", Title: titleArgoDescription, Type: model.ColumnText},
		{ID: "sourceRepos", Title: "Source Repos", Type: model.ColumnText},
		ageColumn,
		{ID: "destinations", Title: "Destinations", Type: model.ColumnText, Hidden: true},
	},
	Cells: argoProjectCells,
	Fields: map[string]fieldFunc{
		"sourceRepos": func(obj *unstructured.Unstructured) []string {
			return model.NestedStrings(obj, fieldSpec, "sourceRepos")
		},
		"destinations": func(obj *unstructured.Unstructured) []string { return argoProjectDestinations(obj) },
	},
	Detail: argoProjectDetail,
}

// argoProjectCells: a project has no state of its own, so no status token (inferred).
func argoProjectCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: singleLine(model.NestedString(obj, fieldSpec, "description"))},
		{Text: strings.Join(model.NestedStrings(obj, fieldSpec, "sourceRepos"), ", ")},
		model.AgeCell(created(obj)),
		{Text: strings.Join(argoProjectDestinations(obj), ", ")},
	}, model.StatusNone
}

// argoProjectDestinations renders each destination as "<cluster>/<namespace>", as an
// Application's Destination.
func argoProjectDestinations(obj any) []string {
	var out []string
	for _, d := range model.NestedSlice(obj, fieldSpec, "destinations") {
		out = appendUnique(out, argoDestination(map[string]any{fieldSpec: map[string]any{fieldArgoDest: d}}))
	}
	return out
}

func argoProjectDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	d.add(titleArgoDescription, model.NestedString(obj, fieldSpec, "description"))
	d.add("Source Repos", strings.Join(model.NestedStrings(obj, fieldSpec, "sourceRepos"), "\n"))
	d.add("Source Namespaces", strings.Join(model.NestedStrings(obj, fieldSpec, "sourceNamespaces"), "\n"))
	if v, ok := model.NestedBool(obj, fieldSpec, "orphanedResources", "warn"); ok {
		d.add("Warn on Orphaned Resources", boolText(v))
	}
	sections := withSection(nil, model.TitleProperties, d)
	if dests := model.NestedSlice(obj, fieldSpec, "destinations"); len(dests) > 0 {
		t := &model.DetailTable{Columns: []string{titleArgoServer, titleName, namespaceColumn.Title}}
		for _, x := range dests {
			t.Rows = append(t.Rows, []string{model.NestedString(x, fieldArgoServer), model.NestedString(x, "name"), model.NestedString(x, "namespace")})
		}
		sections = append(sections, model.DetailSection{Title: "Destinations", Table: t})
	}
	sections = appendGroupKinds(sections, obj, "Cluster Resources Allowed", "clusterResourceWhitelist")
	sections = appendGroupKinds(sections, obj, "Cluster Resources Denied", "clusterResourceBlacklist")
	sections = appendGroupKinds(sections, obj, "Namespace Resources Allowed", "namespaceResourceWhitelist")
	sections = appendGroupKinds(sections, obj, "Namespace Resources Denied", "namespaceResourceBlacklist")
	if roles := model.NestedSlice(obj, fieldSpec, "roles"); len(roles) > 0 {
		t := &model.DetailTable{Columns: []string{titleName, titleArgoDescription, "Policies", "Groups"}}
		for _, r := range roles {
			t.Rows = append(t.Rows, []string{
				model.NestedString(r, "name"), model.NestedString(r, "description"),
				strings.Join(model.NestedStrings(r, "policies"), "\n"), strings.Join(model.NestedStrings(r, "groups"), "\n"),
			})
		}
		sections = append(sections, model.DetailSection{Title: titleArgoRoles, Table: t})
	}
	if wins := model.NestedSlice(obj, fieldSpec, "syncWindows"); len(wins) > 0 {
		t := &model.DetailTable{Columns: []string{titleKind, "Schedule", "Duration", titleApplications, titleArgoNamespaces, "Clusters", "Manual Sync"}}
		for _, w := range wins {
			manual, _ := model.NestedBool(w, "manualSync")
			t.Rows = append(t.Rows, []string{
				model.NestedString(w, idKind), model.NestedString(w, "schedule"), model.NestedString(w, "duration"),
				strings.Join(model.NestedStrings(w, "applications"), "\n"), strings.Join(model.NestedStrings(w, "namespaces"), "\n"),
				strings.Join(model.NestedStrings(w, "clusters"), "\n"), boolText(manual),
			})
		}
		sections = append(sections, model.DetailSection{Title: "Sync Windows", Table: t})
	}
	return sections
}

// appendGroupKinds adds a table of API groups and kinds (a project's allow and deny lists).
func appendGroupKinds(sections []model.DetailSection, obj any, title, field string) []model.DetailSection {
	list := model.NestedSlice(obj, fieldSpec, field)
	if len(list) == 0 {
		return sections
	}
	t := &model.DetailTable{Columns: []string{"Group", titleKind}}
	for _, e := range list {
		g := model.NestedString(e, "group")
		if g == "" {
			g = `"" (core)`
		}
		t.Rows = append(t.Rows, []string{g, model.NestedString(e, idKind)})
	}
	return append(sections, model.DetailSection{Title: title, Table: t})
}
