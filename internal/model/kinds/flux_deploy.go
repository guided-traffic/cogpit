package kinds

import (
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Flux's two deployers: Helm releases (helm.toolkit.fluxcd.io) and Kustomizations
// (kustomize.toolkit.fluxcd.io), the Flux section's first entries (captured 2026-10-11). Columns
// and drawers inferred (flux.go).

// kustomizationsSpec: Source (spec.sourceRef, a link) and Path after Namespace; Revision is the
// last applied one. v1 since Flux 2.0; v1beta2 reads the same fields.
var kustomizationsSpec = &fluxSpec{
	group: fluxKustomize, resource: "kustomizations", kind: "Kustomization", title: "Kustomizations",
	version: "v1", fallback: []string{fluxV1beta2},
	suspend: true, reconcile: true,
	columns: []fluxColumn{
		{Column: model.Column{ID: idSource, Title: titleSource, Type: model.ColumnText}, cell: fluxRefCell("", fieldSpec, "sourceRef"), field: true},
		{Column: model.Column{ID: fieldPath, Title: "Path", Type: model.ColumnText}, cell: fluxTextCell("", fieldSpec, fieldPath), field: true},
	},
	revision: func(obj *unstructured.Unstructured) string {
		return model.NestedString(obj, "status", "lastAppliedRevision")
	},
	properties: func(obj *unstructured.Unstructured, d *detailItems) {
		d.put(fluxRefItem(obj, titleSource, "", fieldSpec, "sourceRef"))
		d.add("Path", model.NestedString(obj, fieldSpec, fieldPath))
		d.add("Target Namespace", model.NestedString(obj, fieldSpec, "targetNamespace"))
		d.add("Service Account", model.NestedString(obj, fieldSpec, "serviceAccountName"))
		d.add("Prune", fluxBool(obj, fieldSpec, "prune"))
		d.add("Wait", fluxBool(obj, fieldSpec, "wait"))
		d.add("Force", fluxBool(obj, fieldSpec, "force"))
		d.add("Retry Interval", model.NestedString(obj, fieldSpec, "retryInterval"))
		d.put(fluxDependsOn(obj, "kustomizations."+fluxKustomize))
		d.add("Attempted Revision", model.NestedString(obj, "status", "lastAttemptedRevision"))
		if entries := model.NestedSlice(obj, "status", "inventory", "entries"); len(entries) > 0 {
			d.add("Applied Objects", strconv.Itoa(len(entries)))
		}
	},
}

// helmReleasesSpec: Chart (the chart's name and a link to its source, or spec.chartRef as a link)
// and Version (the chart version asked for, "*" when the release takes the latest) after
// Namespace; Revision is the chart version of the latest release in the history (v2), else the
// last attempted (older versions' last applied) revision, as `flux get helmreleases` shows it. v2
// since Flux 2.3; v2beta2 and v2beta1 read the same fields. The release's values are never shown:
// they can hold credentials (they are in the object's YAML, H-63).
var helmReleasesSpec = &fluxSpec{
	group: fluxHelm, resource: "helmreleases", kind: "HelmRelease", title: "Helm Releases",
	version: "v2", fallback: []string{"v2beta2", "v2beta1"},
	suspend: true, reconcile: true,
	columns: []fluxColumn{
		{Column: model.Column{ID: "chart", Title: titleChart, Type: model.ColumnText}, cell: helmReleaseChartCell, field: true},
		{Column: model.Column{ID: idVersion, Title: titleVersion, Type: model.ColumnText, Width: 100}, cell: func(obj *unstructured.Unstructured) model.Cell {
			return model.Cell{Text: helmReleaseVersion(obj)}
		}},
	},
	revision:   helmReleaseRevision,
	properties: helmReleaseProperties,
	sections:   helmReleaseHistory,
}

// helmReleaseChartCell is the Chart column: spec.chart.spec.chart linked to the chart's source, or
// spec.chartRef ("OCIRepository/podinfo") linked to the object it names.
func helmReleaseChartCell(obj *unstructured.Unstructured) model.Cell {
	if chart := model.NestedString(obj, fieldSpec, "chart", fieldSpec, "chart"); chart != "" {
		_, link := fluxRef(obj, "", fieldSpec, "chart", fieldSpec, "sourceRef")
		return model.Cell{Text: chart, Link: link}
	}
	text, link := fluxRef(obj, "", fieldSpec, "chartRef")
	return model.Cell{Text: text, Link: link}
}

// helmReleaseVersion is the chart version a release asks for: spec.chart.spec.version, "*" (Flux's
// default, the latest) when it names none; "" for a release from a chartRef.
func helmReleaseVersion(obj any) string {
	if model.NestedMap(obj, fieldSpec, "chart", fieldSpec) == nil {
		return ""
	}
	if v := model.NestedString(obj, fieldSpec, "chart", fieldSpec, "version"); v != "" {
		return v
	}
	return "*"
}

// helmReleaseRevision is the chart version of the latest snapshot in status.history (v2), else
// status.lastAttemptedRevision, else v2beta1's status.lastAppliedRevision.
func helmReleaseRevision(obj *unstructured.Unstructured) string {
	if h := model.NestedSlice(obj, "status", "history"); len(h) > 0 {
		if v := model.NestedString(h[0], "chartVersion"); v != "" {
			return v
		}
	}
	if v := model.NestedString(obj, "status", "lastAttemptedRevision"); v != "" {
		return v
	}
	return model.NestedString(obj, "status", "lastAppliedRevision")
}

func helmReleaseProperties(obj *unstructured.Unstructured, d *detailItems) {
	d.add(titleChart, model.NestedString(obj, fieldSpec, "chart", fieldSpec, "chart"))
	d.add(titleVersion, helmReleaseVersion(obj))
	d.put(fluxRefItem(obj, "Chart Source", "", fieldSpec, "chart", fieldSpec, "sourceRef"))
	d.put(fluxRefItem(obj, "Chart Ref", "", fieldSpec, "chartRef"))
	d.add("Release Name", model.NestedString(obj, fieldSpec, "releaseName"))
	d.add("Target Namespace", model.NestedString(obj, fieldSpec, "targetNamespace"))
	d.add("Storage Namespace", model.NestedString(obj, fieldSpec, "storageNamespace"))
	d.add("Service Account", model.NestedString(obj, fieldSpec, "serviceAccountName"))
	if n, ok := model.NestedInt64(obj, fieldSpec, "maxHistory"); ok {
		d.add("Max History", itoa(n))
	}
	var from []model.Chip
	for _, v := range model.NestedSlice(obj, fieldSpec, "valuesFrom") {
		kind, name := model.NestedString(v, idKind), model.NestedString(v, fieldName)
		if name == "" {
			continue
		}
		text := kind + "/" + name
		if key := model.NestedString(v, "valuesKey"); key != "" {
			text += " (" + key + ")"
		}
		var link *model.ObjectRef
		if k := fluxRefKey(kind); k == keyConfigMaps || k == keySecrets {
			link = linkTo(k, obj.GetNamespace(), name)
		}
		from = append(from, model.Chip{Text: text, Link: link})
	}
	d.put(model.ChipsItem("Values From", from))
	d.put(fluxDependsOn(obj, "helmreleases."+fluxHelm))
	d.add("Attempted Revision", model.NestedString(obj, "status", "lastAttemptedRevision"))
	d.add("Attempted Action", model.NestedString(obj, "status", "lastAttemptedReleaseAction"))
	for _, f := range []struct{ name, field string }{{"Failures", "failures"}, {"Install Failures", "installFailures"}, {"Upgrade Failures", "upgradeFailures"}} {
		if n, ok := model.NestedInt64(obj, "status", f.field); ok && n > 0 {
			d.add(f.name, itoa(n))
		}
	}
	if hc := model.NestedString(obj, "status", "helmChart"); hc != "" {
		ns, name, ok := strings.Cut(hc, "/")
		if !ok {
			ns, name = obj.GetNamespace(), hc
		}
		d.put(linkedItem("Helm Chart", hc, linkTo(fluxRefKey(kindHelmChart), ns, name)))
	}
}

// helmReleaseHistory is status.history as a table, newest first, as the controller keeps it.
func helmReleaseHistory(obj *unstructured.Unstructured) []model.DetailSection {
	h := model.NestedSlice(obj, "status", "history")
	if len(h) == 0 {
		return nil
	}
	t := &model.DetailTable{Columns: []string{"Revision", titleChart, "App Version", "Status", "Deployed"}}
	for _, s := range h {
		rev := ""
		if n, ok := model.NestedInt64(s, "version"); ok {
			rev = itoa(n)
		}
		chart := model.NestedString(s, "chartName")
		if v := model.NestedString(s, "chartVersion"); v != "" {
			chart += "-" + v
		}
		t.Rows = append(t.Rows, []string{
			rev, chart, model.NestedString(s, "appVersion"), model.NestedString(s, "status"),
			timestamp(model.NestedTime(s, "lastDeployed")),
		})
	}
	return []model.DetailSection{{Title: "History", Table: t}}
}
