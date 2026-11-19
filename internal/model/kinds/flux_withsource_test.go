package kinds_test

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestFluxSourceOf: the source the flux CLI's --with-source reconciles first, as flux2's
// cmd/flux reads it: a Kustomization's sourceRef (in its namespace unless it names one), a Helm
// release's chartRef or the HelmChart helm-controller makes of its chart template
// ("<namespace>-<name>" in the template source's namespace), a HelmChart's sourceRef (always in the
// chart's namespace); none for an ExternalArtifact or a kind without a source.
func TestFluxSourceOf(t *testing.T) {
	obj := func(apiVersion, kind, ns, name string, spec map[string]any) *unstructured.Unstructured {
		return testkit.Object(apiVersion, kind, ns, name, map[string]any{"spec": spec})
	}
	ref := func(key, ns, name string) model.ObjectRef {
		return model.ObjectRef{Kind: key, Namespace: ns, Name: name}
	}
	const (
		helmCharts = kinds.FluxHelmChartsKey
		helmRepos  = "helmrepositories.source.toolkit.fluxcd.io"
		ociRepos   = "ocirepositories.source.toolkit.fluxcd.io"
		buckets    = "buckets.source.toolkit.fluxcd.io"
	)
	ks := func(src map[string]any) *unstructured.Unstructured {
		return obj("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "apps", map[string]any{"sourceRef": src})
	}
	hr := func(spec map[string]any) *unstructured.Unstructured {
		return obj("helm.toolkit.fluxcd.io/v2", "HelmRelease", "shop", "podinfo", spec)
	}
	for name, tc := range map[string]struct {
		key     string
		obj     *unstructured.Unstructured
		want    model.ObjectRef
		wantErr string
	}{
		"a Kustomization's Git repository": {fluxKustomizations, ks(map[string]any{"kind": "GitRepository", "name": "flux-system"}), ref(fluxGitRepos, "flux-system", "flux-system"), ""},
		"in another namespace":             {fluxKustomizations, ks(map[string]any{"kind": "OCIRepository", "name": "platform", "namespace": "platform"}), ref(ociRepos, "platform", "platform"), ""},
		"a bucket":                         {fluxKustomizations, ks(map[string]any{"kind": "Bucket", "name": "manifests"}), ref(buckets, "flux-system", "manifests"), ""},
		"an External Artifact":             {fluxKustomizations, ks(map[string]any{"kind": "ExternalArtifact", "name": "bundle"}), model.ObjectRef{}, "is of kind ExternalArtifact"},
		"no kind":                          {fluxKustomizations, ks(map[string]any{"name": "bundle"}), model.ObjectRef{}, "names no kind"},
		"no source":                        {fluxKustomizations, obj("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "apps", nil), model.ObjectRef{}, "names no source"},
		"a release's chart template": {fluxHelmReleases, hr(map[string]any{"chart": map[string]any{"spec": map[string]any{
			"chart": "podinfo", "sourceRef": map[string]any{"kind": "HelmRepository", "name": "podinfo", "namespace": "flux-system"}}}}),
			ref(helmCharts, "flux-system", "shop-podinfo"), ""},
		"a template in the release's namespace": {fluxHelmReleases, hr(map[string]any{"chart": map[string]any{"spec": map[string]any{
			"chart": "podinfo", "sourceRef": map[string]any{"kind": "GitRepository", "name": "charts"}}}}),
			ref(helmCharts, "shop", "shop-podinfo"), ""},
		"a release's chartRef":         {fluxHelmReleases, hr(map[string]any{"chartRef": map[string]any{"kind": "OCIRepository", "name": "podinfo"}}), ref(ociRepos, "shop", "podinfo"), ""},
		"a chartRef to a HelmChart":    {fluxHelmReleases, hr(map[string]any{"chartRef": map[string]any{"kind": "HelmChart", "name": "podinfo", "namespace": "flux-system"}}), ref(helmCharts, "flux-system", "podinfo"), ""},
		"a release without its source": {fluxHelmReleases, hr(map[string]any{"chart": map[string]any{"spec": map[string]any{"chart": "podinfo"}}}), model.ObjectRef{}, "names no chart source"},
		"a HelmChart's repository": {helmCharts, obj("source.toolkit.fluxcd.io/v1", "HelmChart", "flux-system", "shop-podinfo", map[string]any{
			"sourceRef": map[string]any{"kind": "HelmRepository", "name": "podinfo", "namespace": "elsewhere"}}), ref(helmRepos, "flux-system", "podinfo"), ""},
		"a kind without a source": {fluxGitRepos, obj("source.toolkit.fluxcd.io/v1", "GitRepository", "flux-system", "flux-system", nil), model.ObjectRef{}, "has no source"},
	} {
		got, err := kinds.FluxSourceOf(tc.key, tc.obj)
		switch {
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: %v, want an error with %q", name, err, tc.wantErr)
		case tc.wantErr == "" && (err != nil || got != tc.want):
			t.Errorf("%s: %+v, %v; want %+v", name, got, err, tc.want)
		}
	}

	if !kinds.FluxHasSource(fluxKustomizations) || !kinds.FluxHasSource(fluxHelmReleases) || kinds.FluxHasSource(helmCharts) || kinds.FluxHasSource(fluxGitRepos) {
		t.Error("FluxHasSource: Kustomizations and Helm releases only")
	}
	oci := obj("source.toolkit.fluxcd.io/v1", "HelmRepository", "flux-system", "bitnami", map[string]any{"type": "oci"})
	plain := obj("source.toolkit.fluxcd.io/v1", "HelmRepository", "flux-system", "podinfo", nil)
	if !kinds.FluxSourceStatic(helmRepos, oci) || kinds.FluxSourceStatic(helmRepos, plain) || kinds.FluxSourceStatic(ociRepos, oci) {
		t.Error("FluxSourceStatic: an OCI Helm repository only")
	}
	// The dashboard's count: a status-less OCI repository is ready, a plain one unknown, and an OCI
	// repository is not static for another kind (seen on a real cluster).
	if got := kinds.FluxStateOf(helmRepos, oci); got != kinds.FluxReady {
		t.Errorf("FluxStateOf an OCI Helm repository without a status: %v, want ready", got)
	}
	if got := kinds.FluxStateOf(helmRepos, plain); got != kinds.FluxUnknown {
		t.Errorf("FluxStateOf a plain Helm repository without a status: %v, want unknown", got)
	}
	if got := kinds.FluxStateOf(ociRepos, oci); got != kinds.FluxUnknown {
		t.Errorf("FluxStateOf an OCI repository without a status: %v, want unknown", got)
	}
	if !kinds.FluxSourceHasSource(helmCharts) || kinds.FluxSourceHasSource(helmRepos) {
		t.Error("FluxSourceHasSource: HelmCharts only")
	}
}
