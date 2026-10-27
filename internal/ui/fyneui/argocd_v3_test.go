package fyneui

import (
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// renderArgoV3Drawer renders the drawer of an Application as a live Argo CD 3.5 controller writes
// it (checked on a real cluster, 2026-10-16; the fixture is synthetic): outside Argo CD's own
// namespace, its project linked where the controller runs (status.controllerNamespace), from a
// Helm repository's chart, and its Resources without a Health column, which Argo CD 3 keeps in
// its resource tree (status.resourceHealthSource: appTree), with the note saying so above the
// table.
func renderArgoV3Drawer(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, src, _ := newArgoShell(t, mode, false)
	for _, crd := range testkit.ArgoCRDs() {
		src.Set(testkit.CRDsGVR, crd)
	}
	src.Set(testkit.ArgoProjectsGVR, testkit.Object("argoproj.io/v1alpha1", "AppProject", "argocd", "team-billing", nil))
	src.Set(testkit.ArgoApplicationsGVR, testkit.Object("argoproj.io/v1alpha1", "Application", "team-billing", "billing", map[string]any{
		"metadata": map[string]any{"creationTimestamp": argoTS(4 * 24 * time.Hour)},
		"spec": map[string]any{
			"project": "team-billing",
			"source": map[string]any{"repoURL": "https://charts.example.com/stable", "chart": "billing", "targetRevision": "2.3.1",
				"helm": map[string]any{"releaseName": "billing"}},
			"destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": "billing"},
		},
		"status": map[string]any{
			"controllerNamespace": "argocd", "resourceHealthSource": "appTree", "sourceType": "Helm",
			"reconciledAt": argoTS(time.Minute),
			"sync":         map[string]any{"status": "OutOfSync", "revision": "2.3.1"},
			"health":       map[string]any{"status": "Missing"},
			"resources": []any{
				map[string]any{"version": "v1", "kind": "Service", "namespace": "billing", "name": "billing", "status": "OutOfSync"},
				map[string]any{"group": "apps", "version": "v1", "kind": "Deployment", "namespace": "billing", "name": "billing", "status": "OutOfSync"},
			},
		},
	}))
	ref := model.ObjectRef{Context: "argo", Kind: kinds.ArgoApplicationsKey, Namespace: "team-billing", Name: "billing"}
	q.until(t, "Argo CD's kinds", func() bool {
		return slices.ContainsFunc(sh.kinds, func(k kindInfo) bool { return k.Kind == kinds.ArgoApplicationsKey })
	})
	sh.dispatch("ui.openView", openViewParams{View: kinds.ArgoApplicationsKey}, nil)
	q.until(t, "the Application", func() bool { return sh.view.snap != nil && sh.view.snap.Synced && len(sh.view.snap.Rows) == 1 })
	sh.dispatch("ui.select", selectParams{Refs: []model.ObjectRef{ref}}, nil)
	q.until(t, "the drawer", func() bool {
		if sh.drawer.last == nil || sh.drawer.last.Ref.Name != "billing" || sh.drawer.last.Loading {
			return false
		}
		for _, sec := range sh.drawer.last.Sections {
			if sec.Title == "Resources" && len(sec.Items) == 1 && sec.Items[0].Value == kinds.ArgoResourceHealthNote &&
				sec.Table != nil && len(sec.Table.Columns) == 4 {
				return true
			}
		}
		return false
	})
	savePNG(t, w, name)
}

func TestRenderArgoV3ApplicationDrawerDark(t *testing.T) {
	renderArgoV3Drawer(t, ThemeDark, "argocd_application_drawer_v3_dark.png")
}
