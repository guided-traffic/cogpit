package engine

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Helm's storage drivers (G11, architecture §5 and §8; helm/configmaps.go). Helm keeps a release's
// records in Secrets by default and in ConfigMaps with HELM_DRIVER=configmap, both labelled
// owner=helm. The Releases view reads both: Backend.Acquire joins the release Secrets' scope (the
// profile's own, served by the Secrets view's full watch while that runs) with the release
// ConfigMaps' scope (served by the Config Maps view's full watch while that runs), each a watch of
// its own otherwise, and the profile's Derive sees both drivers' records. An action on a release
// runs the Helm SDK with the driver its records are in; a new install with the HELM_DRIVER
// preference (Preferences.HelmDriver).

// configMapsKind is the kind key (and resource) of core ConfigMaps.
const configMapsKind = "configmaps"

// isHelmReleases reports whether a profile is the Helm releases' (helm.NewReleaseProfile).
func isHelmReleases(p *model.KindProfile) bool {
	return p != nil && p.Key == helm.ReleaseKind && p.Derive != nil && !p.Local && p.Group == ""
}

// releaseConfigMapsScope is the scope of the release ConfigMaps: every ConfigMap labelled
// owner=helm, in all namespaces, in full (a record's payload is its data).
func releaseConfigMapsScope() cache.Scope {
	return cache.Scope{GVR: schema.GroupVersionResource{Version: "v1", Resource: configMapsKind}, LabelSelector: helm.ReleaseSelector}
}

// servesConfigMaps reports whether the backend's cluster serves core ConfigMaps (discovery gave it
// their profile; every Kubernetes cluster does, a test backend may not).
func (b *Backend) servesConfigMaps() bool {
	p, ok := b.Profile(configMapsKind)
	return ok && p.Group == "" && !p.Local
}

// releaseScopes are the scopes Helm's releases are read from: the release Secrets, then the
// release ConfigMaps where the cluster serves ConfigMaps.
func (b *Backend) releaseScopes(p *model.KindProfile) []cache.Scope {
	scopes := []cache.Scope{b.Scope(p)}
	if b.servesConfigMaps() {
		scopes = append(scopes, releaseConfigMapsScope())
	}
	return scopes
}

// acquireReleases returns a handle on both drivers' release records: one handle per scope on the
// cluster's cache, joined (cache.Join). Each scope is one watch, or none while its kind's full
// watch covers it. Each is read as its kind is (accessiblens_plan.go): in all namespaces, or, for
// a user who may not list Secrets (ConfigMaps) there, in each namespace the plan reads, covered
// by that namespace's full watch while it runs, so that a namespace-limited user sees the
// releases of the namespaces they may read.
func (b *Backend) acquireReleases(p *model.KindProfile) *cache.Handle {
	scopes := b.releaseScopes(p)
	parts := make([]*cache.Handle, len(scopes))
	for i, sc := range scopes {
		if plan := b.releasePartPlan(p, i); plan.perNamespace {
			parts[i] = b.Cache.AcquireNamespaces(sc, plan.watch)
		} else {
			parts[i] = b.Cache.Acquire(sc)
		}
	}
	return cache.Join(parts...)
}

// releasePartPlan is how the i-th of releaseScopes is read: the release Secrets as the Secrets
// (the releases' own plan asks about listing secrets), the release ConfigMaps as the ConfigMaps.
func (b *Backend) releasePartPlan(p *model.KindProfile, i int) nsPlan {
	if i == 0 {
		return b.namespacePlan(p)
	}
	if cp, ok := b.Profile(configMapsKind); ok {
		return b.namespacePlan(cp)
	}
	return nsPlan{}
}

// releasesCached reports whether every scope the releases are read from is watched already, so
// that reading them costs no request (Cached).
func (b *Backend) releasesCached(p *model.KindProfile) bool {
	if b.Cache == nil {
		return false
	}
	for _, sc := range b.releaseScopes(p) {
		if !b.Cache.Running(sc) {
			return false
		}
	}
	return true
}

// releasesListable reports whether the user may list the release records of either driver: one is
// enough, and one not known yet keeps the view shown (Listable).
func (b *Backend) releasesListable(p *model.KindProfile, filter []string) (listable, known bool) {
	known = true
	resources := []string{p.Resource}
	if b.servesConfigMaps() {
		resources = append(resources, configMapsKind)
	}
	for _, res := range resources {
		l, k := b.Listable(&model.KindProfile{Key: res, Version: "v1", Resource: res, Namespaced: true}, filter)
		if l && k {
			return true, true
		}
		if !k {
			known = false
		}
	}
	return !known, known
}

// releaseRecord returns the record that holds a release object's newest revision, read from a
// handle on the releases (Backend.Acquire): from the part of the release's driver, so that a
// Secret and a ConfigMap of the same name are never taken for each other.
func releaseRecord(h *cache.Handle, obj *unstructured.Unstructured) (*unstructured.Unstructured, bool) {
	driver, name := helm.ReleaseRecord(obj)
	if name == "" {
		return nil, false
	}
	parts := h.Parts()
	if parts == nil {
		parts = []*cache.Handle{h}
	}
	for _, part := range parts {
		if part.Scope().GVR.Resource == driver.Resource() {
			return part.Get(obj.GetNamespace(), name)
		}
	}
	return nil, false
}

// releaseVerbs keeps of a command's verbs on a Helm release those its driver needs: none on
// ConfigMaps for a release kept in Secrets, none on Secrets for one kept in ConfigMaps (the
// commands declare both, privilege-footprint.md). The release is read from the cache without
// waiting; one it does not hold yet keeps every verb, and the command's own checks refuse it.
func (s *Session) releaseVerbs(ref model.ObjectRef, verbs []Verb) []Verb {
	obj, err := s.Lookup(ref)
	if err != nil {
		return verbs
	}
	other := helm.DriverConfigMap
	if helm.ReleaseStorage(obj) == helm.DriverConfigMap {
		other = helm.DriverSecret
	}
	out := make([]Verb, 0, len(verbs))
	for _, v := range verbs {
		if v.Group == "" && v.Resource == other.Resource() {
			continue
		}
		out = append(out, v)
	}
	return out
}

// ErrHelmDriverGUIOnly refuses an agent or the CLI the ConfigMaps driver for new installs: a
// release kept in ConfigMaps has its values readable by whoever may read ConfigMaps in its
// namespace, which RBAC often grants more widely than Secrets, so moving the user's future
// installs there is the user's choice (Cogpit's default, as for the proxy: agents may set it back
// to Helm's default).
var ErrHelmDriverGUIOnly = errors.New("only the user, in Cogpit's own window, can choose the configmap Helm storage driver: " +
	"a release kept in ConfigMaps has its values readable by whoever may read ConfigMaps in its namespace; agents and the " +
	"CLI can read the choice and set it back to secret, Helm's default")

// RefuseHelmDriver refuses a HELM_DRIVER choice an origin may not make: configmap from anywhere but
// Cogpit's window. Secret (or empty) is anyone's, and a choice that does not parse is left to the
// preference's own check.
func RefuseHelmDriver(ctx context.Context, choice *string) error {
	if choice == nil || OriginOf(ctx) == OriginGUI {
		return nil
	}
	if d, err := helm.ParseDriver(*choice); err == nil && d == helm.DriverConfigMap {
		return ErrHelmDriverGUIOnly
	}
	return nil
}

// HelmDriverPreference reads a HELM_DRIVER choice as the preference keeps it: "" for Helm's
// default (secret, secrets or empty), "configmap" (configmap, configmaps); sql and anything else
// are refused.
func HelmDriverPreference(s string) (string, error) {
	d, err := helm.ParseDriver(s)
	if err != nil {
		return "", err
	}
	if d == helm.DriverSecret {
		return "", nil
	}
	return string(d), nil
}

// HelmDriver returns the storage driver a new install writes its records with: the HELM_DRIVER
// preference, Helm's default (Secrets) without one or in a session that keeps no preferences.
// Never the process's HELM_DRIVER: Cogpit's setting is its own.
func (s *Session) HelmDriver() helm.Driver {
	p, err := s.Preferences()
	if err != nil {
		return helm.DriverSecret
	}
	d, err := helm.ParseDriver(p.HelmDriver)
	if err != nil {
		return helm.DriverSecret
	}
	return d
}
