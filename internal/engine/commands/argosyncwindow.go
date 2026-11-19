package commands

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A sync Cogpit writes respects the AppProject's sync windows (A33, owner; H-67; the check in
// kinds.ArgoSyncBlocked): Sync is greyed with the reason while the cache holds the project and a
// window blocks, and the write is refused per Application once the project is read for it. A
// project Cogpit may not read, or that is not there, blocks nothing here: Argo CD's controller
// still honours its windows. A window that cannot be read blocks, with the reason.

// argoDefaultProject is the project an Application without spec.project belongs to.
const argoDefaultProject = "default"

// argoProjectRef is the AppProject of an Application: spec.project in Argo CD's own namespace,
// which the controller writes into the Application's status (kinds.ArgoProjectNamespace, as the
// drawer links it); an Application in another namespace finds its project there.
func argoProjectRef(ref model.ObjectRef, app *unstructured.Unstructured) model.ObjectRef {
	name, _, _ := unstructured.NestedString(app.Object, "spec", "project")
	if name == "" {
		name = argoDefaultProject
	}
	return model.ObjectRef{Context: ref.Context, Kind: kinds.ArgoProjectsKey, Namespace: kinds.ArgoProjectNamespace(app), Name: name}
}

// argoWindowBlock returns why the sync windows block a sync of app now, "" when they do not. With
// read, the projects of the Application's namespace are read through the cache first (a command's
// Run); without, only what the cache holds already counts (Enabled, which must not wait).
func argoWindowBlock(ctx context.Context, s *engine.Session, ref model.ObjectRef, app *unstructured.Unstructured, read bool) string {
	pref := argoProjectRef(ref, app)
	if read {
		_, _ = s.Objects(ctx, kinds.ArgoProjectsKey, []string{pref.Namespace}) // a refusal blocks nothing: see above
	}
	project, err := s.Lookup(pref)
	if err != nil {
		return ""
	}
	why, err := kinds.ArgoSyncBlocked(app, project, s.Now())
	if err != nil {
		return "the AppProject " + pref.Name + " has a sync window Cogpit cannot read (" + err.Error() + "); sync from Argo CD"
	}
	return why
}
