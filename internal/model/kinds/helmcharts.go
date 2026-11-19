package kinds

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The Helm charts are a local kind (architecture §5, "Scopes, local and virtual kinds"; G11):
// Cogpit makes one object per chart of the chart repositories Helm's repositories.yaml lists (D-29), from their
// downloaded indexes (engine/helmrepos.go), and writes them into the connected backend's local
// store. Nothing about them comes from a cluster. Their shape:
//
//	apiVersion: cogpit.io/v1
//	kind: HelmChart
//	metadata: {name: bitnami.nginx, uid, resourceVersion, creationTimestamp}  # HelmChartName
//	spec:
//	  repository: bitnami        # the repository's name, as Helm's file names it (a dot allowed)
//	  name: nginx                # the chart's name
//	  version: 18.2.0            # the newest version that is not a prerelease (helm search's)
//	  appVersion: 1.27.1
//	  description: ...
//	  home, icon, kubeVersion, deprecated, created, keywords, maintainers, sources
//	  digest: <sha256>           # the index's digest of that version's archive, if it lists one
//	  origin: http               # HelmOriginHTTP or HelmOriginInsecureTLS; absent for checked https
//	  versions:                  # every version, newest first
//	  - {version, appVersion, created}

// HelmChartsKey is the kind key of the Helm charts.
const HelmChartsKey = "helmcharts"

// Column ids and fields of the Helm charts.
const (
	fieldRepository  = "repository"
	fieldChartVer    = "version"
	fieldAppVersion  = "appVersion"
	fieldDescription = "description"
	fieldVersions    = "versions"
	fieldKeywords    = "keywords"
	fieldCreated     = "created"
	fieldIcon        = "icon"
	fieldOrigin      = "origin"
	titleAppVersion  = "App Version"
	titleVersion     = "Version"
)

var helmCharts = &model.KindProfile{
	Key:         HelmChartsKey,
	Group:       model.LocalGroup,
	Version:     "v1",
	Resource:    HelmChartsKey,
	Kind:        "HelmChart",
	Title:       "Charts",
	DetailTitle: "Chart",
	Namespaced:  false,
	Local:       true,
	// The columns of Lens's Charts view (resource-parity › Helm), the chart's icon first, without a
	// title (inferred: Lens draws icons; their column is not captured). The window fetches an icon
	// only while it shows the row and Preferences › Load chart icons is on (D-46).
	Columns: []model.Column{
		{ID: fieldIcon, Title: "Icon", Type: model.ColumnIcon, Width: 36},
		nameColumn,
		{ID: fieldDescription, Title: "Description", Type: model.ColumnText},
		{ID: fieldChartVer, Title: titleVersion, Type: model.ColumnText},
		{ID: fieldAppVersion, Title: titleAppVersion, Type: model.ColumnText},
		// Wide enough for a repository's name and its origin's mark, "charts.edge (insecure TLS)"
		// (H-41, A14; Cogpit's width, Lens's is not captured).
		{ID: fieldRepository, Title: "Repository", Type: model.ColumnText, Width: 210},
	},
	Cells: helmChartCells,
	Fields: map[string]fieldFunc{
		fieldRepository: func(obj *unstructured.Unstructured) []string {
			return appendUnique(nil, model.NestedString(obj, "spec", fieldRepository))
		},
		"chart": func(obj *unstructured.Unstructured) []string {
			return appendUnique(nil, model.NestedString(obj, "spec", fieldName))
		},
		"keyword": func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, k := range model.NestedSlice(obj, "spec", fieldKeywords) {
				if s, ok := k.(string); ok {
					out = appendUnique(out, s)
				}
			}
			return out
		},
	},
	Detail: helmChartDetail,
}

// helmChartCells: Name is the chart's name (the object's name also carries its repository, to be
// unique); a deprecated chart's row is marked as a warning (inferred: Lens's marking is not
// captured).
func helmChartCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	status := model.StatusNone
	if deprecated, _ := model.NestedBool(obj, "spec", "deprecated"); deprecated {
		status = model.StatusWarning
	}
	return []model.Cell{
		{Image: model.NestedString(obj, "spec", fieldIcon)},
		{Text: model.NestedString(obj, "spec", fieldName)},
		{Text: model.NestedString(obj, "spec", fieldDescription)},
		{Text: model.NestedString(obj, "spec", fieldChartVer)},
		{Text: model.NestedString(obj, "spec", fieldAppVersion)},
		repositoryCell(model.NestedString(obj, "spec", fieldRepository), model.NestedString(obj, "spec", fieldOrigin)),
	}, status
}

// repositoryCell is a chart's Repository cell: the repository's name and, for an origin nothing
// authenticates (H-41, A14), its mark in the warning colour, "lab (http)" (Cogpit's: Lens has no
// such repositories, D-29).
func repositoryCell(repository, origin string) model.Cell {
	mark := HelmOriginMark(origin)
	if mark == "" {
		return model.Cell{Text: repository}
	}
	return model.PartsCell(model.StatusWarning, model.CellPart{Text: repository}, model.CellPart{Text: "(" + mark + ")", Status: model.StatusWarning})
}

// Chart origins that nothing authenticates (H-41; owner, A14): a repository served over plain http,
// or over https without the server's certificate checked (insecure_skip_tls_verify in Helm's file).
// A chart object's spec.origin, a Helm Install and Upgrade tab's origin, helm.chart.preview's;
// empty for https with the certificate checked (helm.Repository.Origin decides).
const (
	HelmOriginHTTP        = "http"
	HelmOriginInsecureTLS = "insecure-tls"
)

// HelmOriginMark is an origin's mark beside a repository's name: "http", "insecure TLS"; "" for
// none (an unknown origin too).
func HelmOriginMark(origin string) string {
	switch origin {
	case HelmOriginHTTP:
		return "http"
	case HelmOriginInsecureTLS:
		return "insecure TLS"
	}
	return ""
}

// HelmOriginWarning says what an origin means for a chart of a repository: the sentence the
// chart's drawer, the Install tab, the install's preview and the install and upgrade
// confirmations show in the warning colour. "" for none.
func HelmOriginWarning(repository, origin string) string {
	switch origin {
	case HelmOriginHTTP:
		return fmt.Sprintf("Repository %s is served over plain http: nothing authenticates the chart, and anyone on the "+
			"network path can change it.", repository)
	case HelmOriginInsecureTLS:
		return fmt.Sprintf("Repository %s's server certificate is not checked (insecure_skip_tls_verify in Helm's file): "+
			"nothing authenticates the chart, and anyone on the network path can change it.", repository)
	}
	return ""
}

// helmChartDetail is the chart's drawer (inferred: Lens's chart drawer is not captured): the
// listed version's properties, its description, and every version the repository holds.
func helmChartDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	d.add(titleVersion, model.NestedString(obj, "spec", fieldChartVer))
	d.add(titleAppVersion, model.NestedString(obj, "spec", fieldAppVersion))
	repository, origin := model.NestedString(obj, "spec", fieldRepository), model.NestedString(obj, "spec", fieldOrigin)
	d.add("Repository", repository)
	if w := HelmOriginWarning(repository, origin); w != "" {
		d.addStatus("Origin", w, model.StatusWarning)
	}
	d.add("Home", model.NestedString(obj, "spec", "home"))
	d.add("Maintainers", strings.Join(stringsOf(model.NestedSlice(obj, "spec", "maintainers")), ", "))
	d.add("Keywords", strings.Join(stringsOf(model.NestedSlice(obj, "spec", fieldKeywords)), ", "))
	d.add("Sources", strings.Join(stringsOf(model.NestedSlice(obj, "spec", "sources")), "\n"))
	d.add("Kubernetes Version", model.NestedString(obj, "spec", "kubeVersion"))
	d.add("Created", timestamp(model.NestedTime(obj, "spec", fieldCreated)))
	if deprecated, _ := model.NestedBool(obj, "spec", "deprecated"); deprecated {
		d.addStatus("Deprecated", "yes", model.StatusWarning)
	}
	if digest := model.NestedString(obj, "spec", "digest"); digest != "" {
		d.add("Digest", digest)
	} else {
		d.addStatus("Digest", "none: the index lists no digest, so an install cannot verify the archive", model.StatusWarning)
	}
	out := []model.DetailSection{{Title: model.TitleProperties, Items: d}}
	if desc := model.NestedString(obj, "spec", fieldDescription); desc != "" {
		out = append(out, model.DetailSection{Title: "Description", Items: []model.DetailItem{{Value: desc}}})
	}
	columns := []model.Column{
		{ID: fieldChartVer, Title: titleVersion, Type: model.ColumnText},
		{ID: fieldAppVersion, Title: titleAppVersion, Type: model.ColumnText},
		{ID: fieldCreated, Title: "Created", Type: model.ColumnText},
	}
	versions := model.NestedSlice(obj, "spec", fieldVersions)
	rows := make([][]model.Cell, 0, len(versions))
	for _, v := range versions {
		created := model.MarkDate(model.NestedTime(v, fieldCreated)) // the date in the user's format (D-31)
		rows = append(rows, []model.Cell{{Text: model.NestedString(v, fieldChartVer)}, {Text: model.NestedString(v, fieldAppVersion)}, {Text: created}})
	}
	if len(rows) > 0 {
		out = append(out, model.DetailSection{Title: "Versions", Table: model.CellTable(columns, rows)})
	}
	return out
}

func stringsOf(in []any) []string {
	var out []string
	for _, v := range in {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// HelmChart is what HelmChartObject makes a chart's object from: the listed version of a chart in a
// repository's index and its versions.
type HelmChart struct {
	Repository, Name, Version, AppVersion, Description string
	Home, Icon, KubeVersion, Digest                    string
	// Origin is HelmOriginHTTP or HelmOriginInsecureTLS for a repository whose content nothing
	// authenticates (H-41); empty otherwise.
	Origin                         string
	Keywords, Maintainers, Sources []string
	Deprecated                     bool
	Created                        time.Time
	Versions                       []HelmChartVersion
}

// HelmChartVersion is one version of a chart.
type HelmChartVersion struct {
	Version, AppVersion string
	Created             time.Time
}

// HelmChartName is a chart's object name, Cogpit's chart id (A36, owner; the form the lead's
// decision for the owner to confirm): the repository's name with every dot doubled, a dot, and the
// chart's name: "bitnami.nginx", and "charts..example.nginx" for the chart nginx of the repository
// "charts.example". A chart's name starts with a letter or a digit (helm.chartNamePattern), so the
// first dot that is not one of a doubled pair separates the two, whatever dots either name holds;
// the id of a repository without a dot is what it was before repositories with a dot were used, so
// no id Cogpit kept before changes.
func HelmChartName(repository, chart string) string {
	return strings.ReplaceAll(repository, ".", "..") + "." + chart
}

// SplitHelmChartName splits a chart's object name (HelmChartName) into its repository and chart:
// at the first dot that is not one of a doubled pair, the doubled dots before it single again.
func SplitHelmChartName(name string) (repository, chart string, ok bool) {
	for i := 0; i < len(name); i++ {
		if name[i] != '.' {
			continue
		}
		if i+1 < len(name) && name[i+1] == '.' {
			i++ // a doubled dot: part of the repository's name
			continue
		}
		repository, chart = strings.ReplaceAll(name[:i], "..", "."), name[i+1:]
		return repository, chart, repository != "" && chart != ""
	}
	return "", "", false
}

// HelmChartObject returns a chart's object. uid names the chart for as long as its repository is
// added; resourceVersion changes with the index the object is made from.
func HelmChartObject(c HelmChart, uid, resourceVersion string) *unstructured.Unstructured {
	spec := map[string]any{
		fieldRepository: c.Repository, fieldName: c.Name, fieldChartVer: c.Version,
	}
	for k, v := range map[string]string{fieldAppVersion: c.AppVersion, fieldDescription: c.Description, "home": c.Home,
		fieldIcon: c.Icon, "kubeVersion": c.KubeVersion, "digest": c.Digest, fieldOrigin: c.Origin} {
		if v != "" {
			spec[k] = v
		}
	}
	for k, v := range map[string][]string{fieldKeywords: c.Keywords, "maintainers": c.Maintainers, "sources": c.Sources} {
		if len(v) > 0 {
			list := make([]any, len(v))
			for i, s := range v {
				list[i] = s
			}
			spec[k] = list
		}
	}
	if c.Deprecated {
		spec["deprecated"] = true
	}
	if !c.Created.IsZero() {
		spec[fieldCreated] = c.Created.UTC().Format(time.RFC3339)
	}
	versions := make([]any, 0, len(c.Versions))
	for _, v := range c.Versions {
		e := map[string]any{fieldChartVer: v.Version}
		if v.AppVersion != "" {
			e[fieldAppVersion] = v.AppVersion
		}
		if !v.Created.IsZero() {
			e[fieldCreated] = v.Created.UTC().Format(time.RFC3339)
		}
		versions = append(versions, e)
	}
	spec[fieldVersions] = versions
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": model.LocalGroup + "/v1",
		"kind":       "HelmChart",
		fieldSpec:    spec,
	}}
	obj.SetName(HelmChartName(c.Repository, c.Name))
	obj.SetUID(types.UID(uid))
	obj.SetResourceVersion(resourceVersion)
	return obj
}
