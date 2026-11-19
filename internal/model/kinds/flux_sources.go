package kinds

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Flux's sources (source.toolkit.fluxcd.io), the Flux section's Sources sub-group in the captured
// order: Git Repositories, OCI Repositories, Helm Repositories, Buckets, External Artifacts. A
// source's revision is its artifact's. URLs are shown with any credentials in them masked
// (fluxURL). Columns and drawers inferred (flux.go).

// gitRepositoriesSpec: URL and Ref (the branch, tag, semver range, name or commit it follows). v1
// since Flux 2.0; v1beta2 reads the same fields.
var gitRepositoriesSpec = &fluxSpec{
	group: fluxSource, resource: "gitrepositories", kind: "GitRepository", title: "Git Repositories",
	version: "v1", fallback: []string{fluxV1beta2}, section: FluxSectionSources,
	suspend: true, reconcile: true,
	columns: []fluxColumn{
		{Column: model.Column{ID: idURL, Title: titleURL, Type: model.ColumnText}, cell: fluxURLCell(fieldSpec, idURL), field: true},
		{Column: model.Column{ID: idRef, Title: "Ref", Type: model.ColumnText}, cell: func(obj *unstructured.Unstructured) model.Cell {
			return model.Cell{Text: gitRef(obj)}
		}, field: true},
	},
	revision: fluxArtifactRevision,
	properties: func(obj *unstructured.Unstructured, d *detailItems) {
		d.add(titleURL, fluxURL(model.NestedString(obj, fieldSpec, idURL)))
		d.add("Ref", gitRef(obj))
		d.add("Provider", model.NestedString(obj, fieldSpec, "provider"))
		d.put(fluxSecretItem(obj, titleSecret, fieldSpec, "secretRef"))
		d.add("Recurse Submodules", fluxBool(obj, fieldSpec, "recurseSubmodules"))
		d.add("Verify", model.NestedString(obj, fieldSpec, "verify", "mode"))
		fluxArtifactItems(obj, d)
	},
}

// gitRef is a GitRepository's spec.ref in Flux's order of precedence: commit, name, semver, tag,
// branch; "" when it sets none (the controller then takes master).
func gitRef(obj any) string {
	ref := model.NestedMap(obj, fieldSpec, idRef)
	for _, k := range []string{"commit", fieldName, "semver", "tag", "branch"} {
		if v := model.NestedString(ref, k); v != "" {
			return k + " " + v
		}
	}
	return ""
}

// ociRepositoriesSpec: URL and Ref (the digest, semver range or tag it follows). v1 since Flux 2.6;
// v1beta2 reads the same fields.
var ociRepositoriesSpec = &fluxSpec{
	group: fluxSource, resource: "ocirepositories", kind: "OCIRepository", title: "OCI Repositories",
	version: "v1", fallback: []string{fluxV1beta2}, section: FluxSectionSources,
	suspend: true, reconcile: true,
	columns: []fluxColumn{
		{Column: model.Column{ID: idURL, Title: titleURL, Type: model.ColumnText}, cell: fluxURLCell(fieldSpec, idURL), field: true},
		{Column: model.Column{ID: idRef, Title: "Ref", Type: model.ColumnText}, cell: func(obj *unstructured.Unstructured) model.Cell {
			return model.Cell{Text: ociRef(obj)}
		}, field: true},
	},
	revision: fluxArtifactRevision,
	properties: func(obj *unstructured.Unstructured, d *detailItems) {
		d.add(titleURL, fluxURL(model.NestedString(obj, fieldSpec, idURL)))
		d.add("Ref", ociRef(obj))
		d.add("Provider", model.NestedString(obj, fieldSpec, "provider"))
		d.add("Insecure", fluxBool(obj, fieldSpec, "insecure"))
		d.put(fluxSecretItem(obj, titleSecret, fieldSpec, "secretRef"))
		d.put(fluxSecretItem(obj, "Cert Secret", fieldSpec, "certSecretRef"))
		d.add("Service Account", model.NestedString(obj, fieldSpec, "serviceAccountName"))
		d.add("Verify", model.NestedString(obj, fieldSpec, "verify", "provider"))
		fluxArtifactItems(obj, d)
	},
}

// ociRef is an OCIRepository's spec.ref in Flux's order of precedence: digest, semver, tag; "" when
// it sets none (the controller then takes latest).
func ociRef(obj any) string {
	ref := model.NestedMap(obj, fieldSpec, idRef)
	for _, k := range []string{"digest", "semver", "tag"} {
		if v := model.NestedString(ref, k); v != "" {
			return k + " " + v
		}
	}
	return ""
}

// helmRepositoriesSpec: URL and Type ("default" when unset, or "oci"). An OCI Helm repository has
// no artifact, so no revision. v1 since Flux 2.2; v1beta2 reads the same fields.
var helmRepositoriesSpec = &fluxSpec{
	group: fluxSource, resource: "helmrepositories", kind: "HelmRepository", title: "Helm Repositories",
	version: "v1", fallback: []string{fluxV1beta2}, section: FluxSectionSources,
	suspend: true, reconcile: true,
	// Since Flux 2.1 no controller reconciles an OCI repository: it has no status, and the flux
	// CLI reports it Ready ("Helm repository is Ready"; seen on a real cluster).
	staticOf: ociHelmRepository,
	columns: []fluxColumn{
		{Column: model.Column{ID: idURL, Title: titleURL, Type: model.ColumnText}, cell: fluxURLCell(fieldSpec, idURL), field: true},
		{Column: model.Column{ID: idType, Title: titleType, Type: model.ColumnText, Width: 90}, cell: fluxTextCell("default", fieldSpec, idType), field: true},
	},
	revision: fluxArtifactRevision,
	properties: func(obj *unstructured.Unstructured, d *detailItems) {
		d.add(titleURL, fluxURL(model.NestedString(obj, fieldSpec, idURL)))
		d.add(titleType, fluxTextCell("default", fieldSpec, idType)(obj).Text)
		d.add("Provider", model.NestedString(obj, fieldSpec, "provider"))
		d.add("Insecure", fluxBool(obj, fieldSpec, "insecure"))
		d.add("Pass Credentials", fluxBool(obj, fieldSpec, "passCredentials"))
		d.put(fluxSecretItem(obj, titleSecret, fieldSpec, "secretRef"))
		d.put(fluxSecretItem(obj, "Cert Secret", fieldSpec, "certSecretRef"))
		fluxArtifactItems(obj, d)
	},
}

// bucketsSpec: Endpoint and Bucket (spec.bucketName). v1 since Flux 2.5; v1beta2 reads the same
// fields.
var bucketsSpec = &fluxSpec{
	group: fluxSource, resource: "buckets", kind: "Bucket", title: "Buckets",
	version: "v1", fallback: []string{fluxV1beta2}, section: FluxSectionSources,
	suspend: true, reconcile: true,
	columns: []fluxColumn{
		{Column: model.Column{ID: "endpoint", Title: "Endpoint", Type: model.ColumnText}, cell: func(obj *unstructured.Unstructured) model.Cell {
			return model.Cell{Text: fluxURL(model.NestedString(obj, fieldSpec, "endpoint"))}
		}, field: true},
		{Column: model.Column{ID: "bucket", Title: "Bucket", Type: model.ColumnText}, cell: fluxTextCell("", fieldSpec, "bucketName"), field: true},
	},
	revision: fluxArtifactRevision,
	properties: func(obj *unstructured.Unstructured, d *detailItems) {
		d.add("Endpoint", fluxURL(model.NestedString(obj, fieldSpec, "endpoint")))
		d.add("Bucket", model.NestedString(obj, fieldSpec, "bucketName"))
		d.add("Provider", fluxTextCell("generic", fieldSpec, "provider")(obj).Text)
		d.add("Region", model.NestedString(obj, fieldSpec, "region"))
		d.add("Prefix", model.NestedString(obj, fieldSpec, "prefix"))
		d.add("Insecure", fluxBool(obj, fieldSpec, "insecure"))
		d.put(fluxSecretItem(obj, titleSecret, fieldSpec, "secretRef"))
		fluxArtifactItems(obj, d)
	},
}

// externalArtifactsSpec: Source (spec.sourceRef, the object another controller makes the artifact
// for). Another controller produces it, so Cogpit offers no Suspend, Resume or Reconcile. v1
// since Flux 2.7.
var externalArtifactsSpec = &fluxSpec{
	group: fluxSource, resource: "externalartifacts", kind: "ExternalArtifact", title: "External Artifacts",
	version: "v1", section: FluxSectionSources,
	columns: []fluxColumn{
		{Column: model.Column{ID: idSource, Title: titleSource, Type: model.ColumnText}, cell: fluxRefCell("", fieldSpec, "sourceRef"), field: true},
	},
	revision: fluxArtifactRevision,
	properties: func(obj *unstructured.Unstructured, d *detailItems) {
		d.put(fluxRefItem(obj, titleSource, "", fieldSpec, "sourceRef"))
		fluxArtifactItems(obj, d)
	},
}
