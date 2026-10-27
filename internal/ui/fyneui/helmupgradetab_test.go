package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The Helm Upgrade tab with a chart repository that lists the release's chart (G11, 2026-10-13):
// the repository's versions in the picker, the release's own marked, the engine's choice selected;
// a pick goes to the engine, whose answer the pickers follow; Save names the version and the
// repository. Every name is made up.

// repoTabFixture is an open Helm Upgrade tab on ingress-nginx (its chart at 4.12.1) whose chart
// the repositories list, the engine's choice the newest, 4.13.0; helm.release.upgradeTab applies
// a pick to the fake's dock tab (refusing a version the repository does not list) and records it.
func repoTabFixture(t testing.TB, f *fakeEngine, repositories ...string) (engine.DockTab, *pickRecorder) {
	t.Helper()
	_, ref := releasesFake(t)
	ref.Context = "demo"
	h := &engine.HelmUpgradeTab{Chart: "ingress-nginx", Version: "4.13.0", ReleaseVersion: "4.12.1", Repository: repositories[0],
		Repositories: repositories, Versions: []string{"4.13.0", "4.12.2", "4.12.1", "4.12.0", "4.13.1-beta.0"}, UserSuppliedOnly: true}
	tab := engine.DockTab{ID: "helmUpgrade-1", Type: engine.DockHelmUpgrade, Title: "Helm Upgrade: ingress-nginx", Context: "demo", Ref: &ref, Helm: h}
	ed := newEditorFake(f)
	ed.texts[tab.ID] = "controller:\n  replicaCount: 2\n  service:\n    type: LoadBalancer\n"
	ed.revs[tab.ID] = 1
	f.handlers["editor.get"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		cur, _ := f.UIState().DockTab(tab.ID)
		ed.mu.Lock()
		defer ed.mu.Unlock()
		return engine.EditorState{Tab: cur, Text: ed.texts[tab.ID], Revision: ed.revs[tab.ID], BaseRevision: 4, CachedRevision: 4}, nil
	}
	f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{tab}, tab.ID
	rec := &pickRecorder{}
	f.handlers[upgradeTabCommand] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p helmUpgradeTabParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		rec.mu.Lock()
		rec.picks = append(rec.picks, p)
		rec.mu.Unlock()
		if p.Version == "9.9.9" {
			return nil, errors.New("chart ingress-nginx/ingress-nginx has no version 9.9.9")
		}
		f.update(func(u *engine.UIState) {
			for i, d := range u.Dock {
				if d.ID != tab.ID || d.Helm == nil {
					continue
				}
				c := *d.Helm
				if p.Repository != "" && p.Repository != c.Repository {
					c.Repository, c.Version = p.Repository, c.Versions[0]
				}
				if p.Version != "" {
					c.Version = p.Version
				}
				u.Dock[i].Helm = &c
			}
		})
		return map[string]any{"created": false}, nil
	}
	return tab, rec
}

// pickRecorder records helm.release.upgradeTab's picks.
type pickRecorder struct {
	mu    sync.Mutex
	picks []helmUpgradeTabParams
}

func (r *pickRecorder) recorded() []helmUpgradeTabParams {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]helmUpgradeTabParams(nil), r.picks...)
}

func repoUpgradeTabOf(t *testing.T, sh *shell, q *mainQueue, id string) *helmUpgradeTab {
	t.Helper()
	var u *helmUpgradeTab
	q.until(t, "the tab", func() bool {
		if c := sh.dock.contents[id]; c != nil {
			u, _ = c.editor.(*helmUpgradeTab)
		}
		return u != nil && !u.busy && strings.Contains(u.pane.view.Text(), "replicaCount") && strings.Contains(u.status.text, "revision 4")
	})
	return u
}

// The picker lists the repository's versions newest first, prereleases after, the release's own
// marked "(current)", on the engine's choice (the newest); the chart reads "<repository>/<chart>"
// and the version text has no "(no repository)"; the status line names the change of chart
// version. Picking the release's own version goes to the engine, and the tab follows its answer;
// Save names the version (and, for another one than the release's, the repository).
func TestHelmUpgradeTabRepositoryVersions(t *testing.T) {
	f, _ := releasesFake(t)
	up := &upgradeRecorder{}
	up.install(f)
	tab, picks := repoTabFixture(t, f, "ingress-nginx")
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	u := repoUpgradeTabOf(t, sh, q, tab.ID)
	if got := strings.Join(u.version.Options, ","); got != "4.13.0,4.12.2,4.12.1 (current),4.12.0,4.13.1-beta.0" || u.version.Selected != "4.13.0" {
		t.Errorf("picker %q, selected %q", got, u.version.Selected)
	}
	if u.chartText.text != "ingress-nginx/ingress-nginx" || u.versionText.text != "version: 4.12.1" || u.infoVersion.text != "4.13.0" || u.repoBox.Visible() {
		t.Errorf("chart %q, version text %q, info %q, repository picker shown %v", u.chartText.text, u.versionText.text, u.infoVersion.text, u.repoBox.Visible())
	}
	if !strings.Contains(u.status.text, "from chart version 4.12.1 to 4.13.0, downloaded from ingress-nginx") {
		t.Errorf("status %q", u.status.text)
	}

	u.version.SetSelected("4.12.1 (current)")
	q.until(t, "the engine's answer", func() bool { return u.choice().Version == "4.12.1" })
	if p := picks.recorded(); len(p) != 1 || p[0].Version != "4.12.1" || p[0].Repository != "ingress-nginx" || p[0].Ref.Name != "ingress-nginx" {
		t.Errorf("picks %+v", p)
	}
	if u.infoVersion.text != "4.12.1" || u.version.Selected != "4.12.1 (current)" || strings.Contains(u.status.text, "from chart version") {
		t.Errorf("after the pick: info %q, selected %q, status %q", u.infoVersion.text, u.version.Selected, u.status.text)
	}
	test.Tap(u.saveBtn)
	q.until(t, "the upgrade", func() bool { return len(up.recorded()) == 1 && !u.busy })
	if got := up.recorded()[0]; got.Tab != tab.ID || got.Version != "4.12.1" || got.Repository != "" || got.TextRevision == 0 {
		t.Errorf("upgrade with the release's own version %+v", got)
	}

	u.version.SetSelected("4.12.2")
	q.until(t, "the engine's answer", func() bool { return u.choice().Version == "4.12.2" })
	if !strings.Contains(u.status.text, "Upgraded to revision 5") {
		t.Errorf("a new choice replaced the upgrade's result: %q", u.status.text)
	}

	// An agent's choice arrives with the tab's EventEditor (the dock does not sync on a choice).
	f.update(func(st *engine.UIState) {
		c := *st.Dock[0].Helm
		c.Version = "4.12.0"
		st.Dock[0].Helm = &c
	})
	f.emit(engine.Event{Type: engine.EventEditor, View: tab.ID, Revision: 1})
	q.until(t, "the agent's choice", func() bool { return u.version.Selected == "4.12.0" && u.infoVersion.text == "4.12.0" })
	if n := len(picks.recorded()); n != 2 {
		t.Errorf("following the engine sent %d picks back, want the user's 2", n)
	}
	u.version.SetSelected("4.12.2")
	q.until(t, "the user's choice again", func() bool { return u.choice().Version == "4.12.2" })
	test.Tap(u.saveBtn)
	q.until(t, "the second upgrade", func() bool { return len(up.recorded()) == 2 && !u.busy })
	if got := up.recorded()[1]; got.Version != "4.12.2" || got.Repository != "ingress-nginx" {
		t.Errorf("upgrade to 4.12.2 %+v", got)
	}
}

// Several repositories list the chart: a second picker chooses the repository, which goes to the
// engine; a pick the engine refuses puts the pickers back on its choice and says why.
func TestHelmUpgradeTabRepositoryChoice(t *testing.T) {
	f, _ := releasesFake(t)
	tab, picks := repoTabFixture(t, f, "ingress-nginx", "mirror")
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	u := repoUpgradeTabOf(t, sh, q, tab.ID)
	if !u.repoBox.Visible() || strings.Join(u.repo.Options, ",") != "ingress-nginx,mirror" || u.repo.Selected != "ingress-nginx" {
		t.Fatalf("repository picker shown %v, %q on %q", u.repoBox.Visible(), u.repo.Options, u.repo.Selected)
	}
	u.repo.SetSelected("mirror")
	q.until(t, "the engine's answer", func() bool { return u.choice().Repository == "mirror" })
	if p := picks.recorded(); len(p) != 1 || p[0].Repository != "mirror" || p[0].Version != "" {
		t.Errorf("picks %+v", p)
	}
	if u.chartText.text != "mirror/ingress-nginx" || u.repo.Selected != "mirror" {
		t.Errorf("chart %q, repository %q", u.chartText.text, u.repo.Selected)
	}

	u.labels["9.9.9"] = "9.9.9" // a version the engine refuses
	u.version.Options = append(u.version.Options, "9.9.9")
	u.version.SetSelected("9.9.9")
	q.until(t, "the refusal", func() bool { return strings.Contains(u.status.text, "was not chosen") })
	if u.version.Selected != "4.13.0" || !strings.Contains(u.status.text, "no version 9.9.9") {
		t.Errorf("after a refused pick: selected %q, status %q", u.version.Selected, u.status.text)
	}
}

// renderHelmUpgradePicker renders the tab with the version picker open over it.
func renderHelmUpgradePicker(t *testing.T, mode ThemeMode, name string) {
	f, _ := releasesFake(t)
	tab, _ := repoTabFixture(t, f, "ingress-nginx")
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	u := repoUpgradeTabOf(t, sh, q, tab.ID)
	q.until(t, "the drawer", func() bool {
		rv, _ := sh.drawer.kept[releaseValuesKey].(*releaseValues)
		return sh.drawer.last != nil && sh.diag.total > 0 && rv != nil && rv.revision == 4
	})
	settle(w)
	test.Tap(u.version)
	if len(w.Canvas().Overlays().List()) == 0 {
		t.Fatal("the picker did not open")
	}
	if pop, ok := w.Canvas().Overlays().Top().(*widget.PopUpMenu); ok && len(pop.Items) != len(u.version.Options) {
		t.Errorf("the open picker lists %d versions, want %d", len(pop.Items), len(u.version.Options))
	}
	savePNG(t, w, name)
}

func TestRenderHelmUpgradePickerDark(t *testing.T) {
	renderHelmUpgradePicker(t, ThemeDark, "helm_upgrade_picker_dark.png")
}
