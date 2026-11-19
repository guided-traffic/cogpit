package kinds

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Reconcile with Source (D-33, owner): the source the flux CLI's `--with-source` reconciles before
// an object, as flux2's cmd/flux reconcile_kustomization.go, reconcile_helmrelease.go,
// reconcile_source_chart.go and reconcile_source_helm.go read it (verified against flux2's main
// branch):
//
//   - a Kustomization: spec.sourceRef, a GitRepository, OCIRepository or Bucket, in the
//     Kustomization's namespace unless the reference names one;
//   - a Helm release: spec.chartRef, a HelmChart or an OCIRepository (the release's namespace unless
//     it names one); else the HelmChart helm-controller makes of spec.chart's template, named
//     "<namespace>-<name>" of the release, in the namespace of the template's sourceRef (the
//     release's unless it names one);
//   - a HelmChart (the flux CLI reconciles a release's HelmChart with its source): spec.sourceRef, a
//     HelmRepository, GitRepository or Bucket, in the chart's namespace.
//
// A HelmRepository of type oci is static: no controller reconciles it, and the flux CLI skips it
// ("reconciliation not supported by the object").

// fluxSourceKinds are the Kinds each kind's --with-source reconciles first, by the kind's key.
var fluxSourceKinds = map[string][]string{
	kustomizationsSpec.key(): {gitRepositoriesSpec.kind, ociRepositoriesSpec.kind, bucketsSpec.kind},
	helmReleasesSpec.key():   {kindHelmChart, ociRepositoriesSpec.kind},
	FluxHelmChartsKey:        {helmRepositoriesSpec.kind, gitRepositoriesSpec.kind, bucketsSpec.kind},
}

// FluxHelmChartsKey is the kind key of Flux's HelmCharts, which have no view of their own in the
// Flux section (a served definition shows them under Custom Resources).
const FluxHelmChartsKey = "helmcharts." + fluxSource

// FluxHasSource reports whether Reconcile with Source applies to a Flux kind key: Kustomizations
// and Helm releases (HelmCharts as a release's source).
func FluxHasSource(key string) bool {
	_, ok := fluxSourceKinds[key]
	return ok && key != FluxHelmChartsKey
}

// FluxSourceOf returns the source the flux CLI's --with-source reconciles before obj, an object of
// the kind key (a reference with an empty context), or an error that says why there is none: the
// kind has no source, the object names none, or its source is of a kind no Flux controller
// reconciles on request (an ExternalArtifact).
func FluxSourceOf(key string, obj *unstructured.Unstructured) (model.ObjectRef, error) {
	allowed, ok := fluxSourceKinds[key]
	if !ok {
		return model.ObjectRef{}, fmt.Errorf("%s has no source to reconcile", obj.GetName())
	}
	ns := obj.GetNamespace()
	var fields []string
	switch key {
	case helmReleasesSpec.key():
		if model.NestedMap(obj, fieldSpec, "chartRef") == nil {
			return helmReleaseChart(obj)
		}
		fields = []string{fieldSpec, "chartRef"}
	default:
		fields = []string{fieldSpec, "sourceRef"}
	}
	ref := model.NestedMap(obj, fields...)
	kind, name := model.NestedString(ref, idKind), model.NestedString(ref, fieldName)
	if name == "" {
		return model.ObjectRef{}, fmt.Errorf("%s names no source", obj.GetName())
	}
	switch {
	case kind == "":
		return model.ObjectRef{}, fmt.Errorf("the source reference of %s names no kind", obj.GetName())
	case !slices.Contains(allowed, kind):
		return model.ObjectRef{}, fmt.Errorf("the source of %s is of kind %s, which Flux does not reconcile on request", obj.GetName(), kind)
	}
	if other := model.NestedString(ref, "namespace"); other != "" && key != FluxHelmChartsKey {
		ns = other // a HelmChart's sourceRef is local to the chart's namespace
	}
	return model.ObjectRef{Kind: fluxRefKey(kind), Namespace: ns, Name: name}, nil
}

// helmReleaseChart is the HelmChart helm-controller makes of a release's spec.chart template.
func helmReleaseChart(obj *unstructured.Unstructured) (model.ObjectRef, error) {
	src := model.NestedMap(obj, fieldSpec, "chart", fieldSpec, "sourceRef")
	if model.NestedString(src, fieldName) == "" {
		return model.ObjectRef{}, fmt.Errorf("%s names no chart source", obj.GetName())
	}
	ns := model.NestedString(src, "namespace")
	if ns == "" {
		ns = obj.GetNamespace()
	}
	return model.ObjectRef{Kind: FluxHelmChartsKey, Namespace: ns, Name: obj.GetNamespace() + "-" + obj.GetName()}, nil
}

// FluxSourceStatic reports whether the flux CLI skips a source as static: a HelmRepository of type
// oci, which no controller reconciles.
func FluxSourceStatic(key string, obj *unstructured.Unstructured) bool {
	return key == helmRepositoriesSpec.key() && ociHelmRepository(obj)
}

// ociHelmRepository reports whether a HelmRepository is of type oci.
func ociHelmRepository(obj *unstructured.Unstructured) bool {
	return model.NestedString(obj, fieldSpec, idType) == "oci"
}

// FluxSourceHasSource reports whether the flux CLI reconciles a source's own source first: a
// release's HelmChart, whose chart comes from a HelmRepository, GitRepository or Bucket.
func FluxSourceHasSource(key string) bool { return key == FluxHelmChartsKey }

// FluxSourceStatus reads the state of an object in a Reconcile with Source chain as FluxStatusOf
// reads a Flux view's rows, for any kind with Flux's conditions, Flux's HelmCharts among them,
// which have no curated profile (A91: the job waits for each object's controller): the state and
// the Ready condition's message, one line.
func FluxSourceStatus(obj *unstructured.Unstructured) (FluxState, string) {
	st := fluxStatusOf(obj, false)
	return st.state, st.message
}
