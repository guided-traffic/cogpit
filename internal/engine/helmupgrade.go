package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Helm Upgrade dock tab (architecture §8, G11): Lens's "Helm Upgrade: <name>" tab (captured
// 2026-10-12) holds a release's user-supplied values in an editor whose text lives in the engine
// (editor.get, editor.setText, as Edit tabs), with the chart and the version the upgrade installs
// on the tab (DockTab.Helm). The values come from the cache: the newest revision's release record
// (a Secret, or a ConfigMap for a release kept in ConfigMaps), decoded. The tab remembers the revision its values are based on, as an Edit tab remembers its
// object's resourceVersion: helm.release.upgrade refuses to upgrade a release that moved to
// another revision since, so that an upgrade never overwrites a change the tab has not shown.

// helmUpgradeTitle starts a Helm Upgrade tab's title, as Lens titles it.
const helmUpgradeTitle = "Helm Upgrade: "

// helmBase is the revision a Helm Upgrade tab's values are based on: the release's newest
// revision, and that revision's release Secret's UID, which "" stands for while the cache has not
// seen the revision an upgrade from the tab made.
type helmBase struct {
	revision int
	uid      string
}

// cachedRelease returns a release object and the record (the release Secret, or ConfigMap for a
// release kept in ConfigMaps) of its newest revision from the cache, waiting (bounded by ctx and
// syncTimeout) for the release records' initial lists.
func (s *Session) cachedRelease(ctx context.Context, ref model.ObjectRef) (*Backend, model.ObjectRef, *unstructured.Unstructured, *unstructured.Unstructured, error) {
	if ref.Kind != helm.ReleaseKind {
		return nil, ref, nil, nil, fmt.Errorf("%w: %s is not a Helm release", ErrNotApplicable, ref.String())
	}
	b, ref, p, err := s.resolve(ref)
	if err != nil {
		return nil, ref, nil, nil, err
	}
	h := b.Acquire(p)
	defer h.Release()
	if err := waitSynced(ctx, h, p.Key); err != nil {
		return nil, ref, nil, nil, err
	}
	obj, ok := KindGet(h, p, ref.Namespace, ref.Name)
	if !ok {
		return nil, ref, nil, nil, fmt.Errorf("%s: %w", ref.String(), ErrNotFound)
	}
	record, ok := releaseRecord(h, obj) // the Secret or ConfigMap of its driver (helmdriver.go)
	if !ok {
		return nil, ref, nil, nil, fmt.Errorf("%s: the release %s of revision %d: %w", ref.String(), helm.ReleaseStorage(obj).Kind(), helm.ReleaseRevision(obj), ErrNotFound)
	}
	return b, ref, obj, record, nil
}

// ReleaseValues returns a release's values from the cache: the user-supplied ones, or with all
// every value with the chart's defaults. The caller decides who may see them (helm.release.values
// withholds them from agents).
func (s *Session) ReleaseValues(ctx context.Context, ref model.ObjectRef, all bool) (helm.Values, error) {
	_, ref, _, secret, err := s.cachedRelease(ctx, ref)
	if err != nil {
		return helm.Values{}, err
	}
	v, err := helm.ReleaseValues(secret, all)
	if err != nil {
		return helm.Values{}, fmt.Errorf("release %s: %w", ref.Name, err)
	}
	return v, nil
}

// OpenHelmUpgradeTab opens a release's Helm Upgrade tab, its editor filled with the release's
// user-supplied values from the cache, or activates the tab already open on it. The tab offers the
// versions of a repository that lists the release's chart (helmupgrade_repos.go): pick chooses the
// repository and the version, an empty field keeps the open tab's choice or takes the default.
func (s *Session) OpenHelmUpgradeTab(ctx context.Context, ref model.ObjectRef, pick HelmUpgradePick) (EditorState, bool, error) {
	b, ref, obj, secret, err := s.cachedRelease(ctx, ref)
	if err != nil {
		return EditorState{}, false, err
	}
	chart, version := helm.ReleaseChart(obj)
	for _, t := range s.UIState().Dock {
		if t.Type != DockHelmUpgrade || t.Ref == nil || !t.Ref.Same(ref) || t.Context != b.Context {
			continue
		}
		choice, err := s.upgradeChoice(chart, version, pick, t.Helm)
		if err != nil {
			return EditorState{}, false, fmt.Errorf("release %s: %w", ref.Name, err)
		}
		if t.Helm == nil || !reflect.DeepEqual(*choice, *t.Helm) {
			if err := s.setHelmUpgradeChoice(t.ID, choice); err != nil {
				return EditorState{}, false, err
			}
		}
		id := t.ID
		s.UpdateUI(func(u *UIState) { u.ShowDockTab(id) })
		st, err := s.EditorState(ctx, id)
		return st, false, err
	}
	choice, err := s.upgradeChoice(chart, version, pick, nil)
	if err != nil {
		return EditorState{}, false, fmt.Errorf("release %s: %w", ref.Name, err)
	}
	vals, err := helm.ReleaseValues(secret, false)
	if err != nil {
		return EditorState{}, false, fmt.Errorf("release %s: %w", ref.Name, err)
	}
	if len(vals.YAML) > MaxManifestBytes {
		return EditorState{}, false, fmt.Errorf("release %s: the values are larger than the %s an editor takes", ref.Name, model.FormatBytes(MaxManifestBytes))
	}
	r := ref
	e := &editor{
		tab: DockTab{
			ID: s.NewDockID(DockHelmUpgrade), Type: DockHelmUpgrade, Title: helmUpgradeTitle + ref.Name, Context: b.Context, Ref: &r,
			Helm: choice,
		},
		text: vals.YAML, rev: 1,
		helm: &helmBase{revision: helm.ReleaseRevision(obj), uid: string(obj.GetUID())},
	}
	s.putEditor(e)
	tab, created := e.tab, true
	ui := s.UpdateUI(func(u *UIState) {
		for _, x := range u.Dock {
			if x.Type == DockHelmUpgrade && x.Ref != nil && x.Ref.Same(ref) && x.Context == ref.Context {
				tab, created = x, false
				break
			}
		}
		if created {
			u.Dock = append(u.Dock, tab)
		}
		u.ShowDockTab(tab.ID)
	})
	s.settleEditor(e.tab.ID, created, ui.Revision)
	st, err := s.EditorState(ctx, tab.ID)
	return st, created, err
}

// helmEditorState fills an editor state of a Helm Upgrade tab with the revision its values are
// based on and what the cache holds now: a release at another revision is stale (an upgrade would
// be refused), a release that is gone missing.
func (s *Session) helmEditorState(st *EditorState, e editor) {
	if e.helm == nil || e.tab.Ref == nil {
		return
	}
	st.BaseRevision = e.helm.revision
	if s.BackendOf(e.tab.Context) == nil { // the tab's cluster, shown or not (D-69)
		return
	}
	obj, err := s.Lookup(*e.tab.Ref)
	switch {
	case errors.Is(err, ErrNotFound):
		st.Missing = true
	case err == nil:
		st.CachedRevision = helm.ReleaseRevision(obj)
		st.Stale = helmMoved(*e.helm, obj)
	}
}

// helmMoved reports whether a release object is no longer at the revision a tab's values are
// based on: another revision, or the same number on another release Secret (uninstalled and
// installed again).
func helmMoved(base helmBase, obj *unstructured.Unstructured) bool {
	return helm.ReleaseRevision(obj) != base.revision || (base.uid != "" && base.uid != string(obj.GetUID()))
}

// HelmUpgradeTabInfo is a Helm Upgrade tab as helm.release.upgrade uses it: the tab, its values
// and the revision they are based on.
type HelmUpgradeTabInfo struct {
	Tab          DockTab
	Values       string
	TextRevision uint64 // the editor's text revision Values is at
	BaseRevision int
	baseUID      string
}

// HelmUpgradeTab returns an open Helm Upgrade tab's state.
func (s *Session) HelmUpgradeTab(id string) (HelmUpgradeTabInfo, error) {
	e, err := s.editorOf(id)
	if err != nil {
		return HelmUpgradeTabInfo{}, err
	}
	if e.tab.Type != DockHelmUpgrade || e.helm == nil || e.tab.Ref == nil {
		return HelmUpgradeTabInfo{}, fmt.Errorf("dock tab %q is a %s tab, not a Helm Upgrade tab", id, e.tab.Type)
	}
	return HelmUpgradeTabInfo{Tab: e.tab, Values: e.text, TextRevision: e.rev, BaseRevision: e.helm.revision, baseUID: e.helm.uid}, nil
}

// CheckReleaseBase fails with ErrTargetsChanged when a release object is no longer at the revision
// a tab's (or a caller's) values are based on: uid "" checks the revision only.
func CheckReleaseBase(obj *unstructured.Unstructured, revision int, uid string) error {
	if !helmMoved(helmBase{revision: revision, uid: uid}, obj) {
		return nil
	}
	return fmt.Errorf("%w: release %s is at revision %d now, the values are based on revision %d; nothing was done: read the values again",
		ErrTargetsChanged, obj.GetName(), helm.ReleaseRevision(obj), revision)
}

// CheckTabBase is CheckReleaseBase for a Helm Upgrade tab.
func (t HelmUpgradeTabInfo) CheckTabBase(obj *unstructured.Unstructured) error {
	return CheckReleaseBase(obj, t.BaseRevision, t.baseUID)
}

// RebaseHelmUpgradeTab moves a Helm Upgrade tab's base to the revision an upgrade from it made,
// with that revision's release Secret's UID once the cache has seen it (uid "" until then), and its
// choice to the chart version the release now has, which the picker marks as the current one.
func (s *Session) RebaseHelmUpgradeTab(id string, revision int, uid, chartVersion string) {
	e, err := s.updateEditor(id, func(e *editor) {
		if e.helm != nil {
			e.helm = &helmBase{revision: revision, uid: uid} // a new value: copies of the editor share the old one
		}
	})
	if err != nil || e.tab.Helm == nil || chartVersion == "" || chartVersion == e.tab.Helm.ReleaseVersion {
		return
	}
	cur := e.tab.Helm
	choice, err := s.upgradeChoice(cur.Chart, chartVersion, HelmUpgradePick{Repository: cur.Repository, Version: chartVersion}, cur)
	if err != nil { // the repository went meanwhile: the release's own version alone
		choice, err = s.upgradeChoice(cur.Chart, chartVersion, HelmUpgradePick{}, nil)
	}
	if err == nil {
		_ = s.setHelmUpgradeChoice(id, choice)
	}
}
