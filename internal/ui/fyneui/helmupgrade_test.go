package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Helm upgrade in the GUI: the release drawer's values editor and Save, the Rollback dialog
// as the rollback's confirmation, the Helm Upgrade dock tab, and the banner's offer for an
// interrupted upgrade. Every name is made up.

// releaseDrawerText returns every text the drawer's details show: lines, labels, pills and links, joined.
func releaseDrawerText(d *drawer) string {
	var out []string
	for _, l := range findAll[*textLine](d.details) {
		out = append(out, l.text)
	}
	for _, l := range findAll[*sectionHead](d.details) { // a section's bracket label (stage_header.go)
		out = append(out, l.title)
	}
	for _, l := range findAll[*widget.Label](d.details) {
		out = append(out, l.Text)
	}
	for _, p := range findAll[*pill](d.details) {
		out = append(out, p.text)
	}
	for _, l := range findAll[*linkText](d.details) {
		out = append(out, l.text)
	}
	return strings.Join(out, "\n")
}

// helmDispatches are the Helm commands and parameters the shell dispatches, which
// TestDispatchedParamsMatchTheRegistry checks against the real registry.
func helmDispatches() []struct {
	id     string
	params any
} {
	ref := model.ObjectRef{Context: "demo", Kind: helm.ReleaseKind, Namespace: "apps", Name: "web"}
	values := "a: 1\n"
	return []struct {
		id     string
		params any
	}{
		{"helm.release.values", helmValuesParams{refParams: refParams{Ref: ref}, All: true}},
		{"helm.release.history", refParams{Ref: ref}},
		{"helm.release.upgrade", helmUpgradeParams{refParams: refParams{Ref: ref}, Tab: "helmUpgrade-1", TextRevision: 2}},
		{"helm.release.upgrade", helmUpgradeParams{refParams: refParams{Ref: ref}, Values: &values, Revision: 3}},
		{"helm.release.rollback", helmRollbackParams{refParams: refParams{Ref: ref}, Revision: 2}},
		{helm.UpgradeTabCommand, refParams{Ref: ref}},
		{helm.UpgradeTabCommand, helmUpgradeTabParams{refParams: refParams{Ref: ref}, Repository: "ingress-nginx", Version: "4.13.0"}},
		{"helm.release.upgrade", helmUpgradeParams{refParams: refParams{Ref: ref}, Tab: "helmUpgrade-1", TextRevision: 2, Version: "4.13.0", Repository: "ingress-nginx"}},
		{"editor.setText", editorTextParams{ID: "helmUpgrade-1", YAML: values}},
	}
}

// upgradeRecorder answers helm.release.upgrade (and records it) for a fake.
type upgradeRecorder struct {
	mu    sync.Mutex
	calls []helmUpgradeParams
	err   error
}

func (u *upgradeRecorder) install(f *fakeEngine) {
	f.handlers["helm.release.upgrade"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p helmUpgradeParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		u.mu.Lock()
		defer u.mu.Unlock()
		u.calls = append(u.calls, p)
		if u.err != nil {
			return nil, u.err
		}
		return map[string]any{"action": "upgraded", "revision": 5}, nil
	}
}

func (u *upgradeRecorder) recorded() []helmUpgradeParams {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]helmUpgradeParams(nil), u.calls...)
}

// releaseDrawerShell opens the shell on the Releases view with ingress-nginx's drawer and waits
// until its values are read.
func releaseDrawerShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine, *upgradeRecorder) {
	t.Helper()
	f, _ := releasesFake(t)
	up := &upgradeRecorder{}
	up.install(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer's values", func() bool {
		rv, _ := sh.drawer.kept[releaseValuesKey].(*releaseValues)
		return sh.view.snap != nil && sh.drawer.last != nil && len(sh.drawer.actions.Objects) == 3 && rv != nil && rv.revision == 4 &&
			sh.diag.total > 0
	})
	return sh, w, q, f, up
}

// The drawer's text as Lens's: the status written "Deployed", the resources by kind with links,
// no events.
func TestReleaseDrawerContent(t *testing.T) {
	sh, _, _, _, _ := releaseDrawerShell(t, ThemeDark)
	texts := releaseDrawerText(sh.drawer)
	for _, s := range []string{"Deployed", "ingress-nginx-controller", "Deployment", "Service", "Notes", "User-supplied values only"} {
		if !strings.Contains(texts, s) && !hasCheck(sh.drawer, s) {
			t.Errorf("the drawer lacks %q", s)
		}
	}
	if strings.Contains(texts, "No events found") {
		t.Error("a release's drawer shows events")
	}
}

// The drawer's look as Lens's (captured 2026-10-12): the title on one line, no Details and YAML
// tabs, Chart as "<chart>-<version>", Namespace as plain text, Status as a neutral grey pill, the
// checkbox unchecked at first (the editor holds every value), Save a bar under the editor.
func TestReleaseDrawerLooksLikeLens(t *testing.T) {
	sh, w, _, _, _ := releaseDrawerShell(t, ThemeDark)
	settle(w)
	d := sh.drawer
	if d.title.text != "Release: ingress-nginx" || d.kind.Visible() {
		t.Errorf("title %q, kind line shown %v", d.title.text, d.kind.Visible())
	}
	if len(d.body.Objects) != 1 || d.body.Objects[0] != d.scroll || len(findAll[*container.AppTabs](d.root)) != 0 {
		t.Error("the release drawer has tabs")
	}
	if d.status.Visible() {
		t.Error("a status pill beside the icons")
	}
	texts := releaseDrawerText(d)
	if !strings.Contains(texts, "ingress-nginx-4.12.1") {
		t.Errorf("Chart is not <chart>-<version>: %s", texts)
	}
	for _, it := range d.last.Sections[0].Items {
		if it.Name == "Namespace" && (it.Link != nil || it.Value != d.shown.Namespace) {
			t.Errorf("Namespace %+v is not plain text", it)
		}
	}
	var deployed *pill
	for _, p := range findAll[*pill](d.details) {
		if p.text == "Deployed" {
			deployed = p
		}
	}
	if deployed == nil || deployed.fg != colorMuted {
		t.Errorf("Status pill %+v, want a neutral one", deployed)
	}
	rv := d.kept[releaseValuesKey].(*releaseValues)
	if rv.userOnly.Checked || !rv.all || !strings.Contains(rv.view.Text(), "image:") {
		t.Errorf("at first: checked %v, all %v, editor %q", rv.userOnly.Checked, rv.all, rv.view.Text())
	}
	// Save is a bar the editor's width, below it.
	editorPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(rv.view)
	savePos := fyne.CurrentApp().Driver().AbsolutePositionForObject(rv.save)
	if rv.save.Size().Width < rv.view.Size().Width || savePos.Y <= editorPos.Y+rv.view.Size().Height-1 {
		t.Errorf("Save %v at %v, editor %v at %v", rv.save.Size(), savePos, rv.view.Size(), editorPos)
	}
	// Another kind's drawer keeps its tabs.
	if sh.drawer.plain && sh.drawer.shown.Kind != helm.ReleaseKind {
		t.Error("plain drawer for another kind")
	}
}

func hasCheck(d *drawer, label string) bool {
	for _, c := range findAll[*widget.Check](d.details) {
		if c.Text == label {
			return true
		}
	}
	return false
}

// The values editor: the user-supplied values, all values when the checkbox is cleared; Save
// upgrades with the editor's text based on the revision it read; typing survives a rebuild of the
// drawer; a release that moved since is followed, the text kept.
func TestReleaseDrawerValuesSave(t *testing.T) {
	sh, _, q, f, up := releaseDrawerShell(t, ThemeDark)
	rv := sh.drawer.kept[releaseValuesKey].(*releaseValues)
	if !rv.all || !strings.Contains(rv.view.Text(), "image:") || !strings.Contains(rv.status.text, "every value") {
		t.Errorf("at first: all %v, status %q", rv.all, rv.status.text)
	}
	rv.userOnly.SetChecked(true)
	q.until(t, "the user-supplied values", func() bool { return !rv.all && !strings.Contains(rv.view.Text(), "image:") })
	rv.userOnly.SetChecked(false)
	q.until(t, "all values", func() bool { return rv.all && strings.Contains(rv.view.Text(), "image:") })
	rv.view.SetText(rv.view.Text() + "typed: 1\n")
	rv.changed()
	if !strings.Contains(rv.status.text, "makes every value in the editor a user-supplied value") {
		t.Errorf("status of an edit of every value: %q", rv.status.text)
	}
	rv.userOnly.SetChecked(true)
	q.until(t, "the user-supplied values again", func() bool { return !rv.all && !strings.Contains(rv.view.Text(), "image:") })

	rv.view.SetText(rv.view.Text() + "extra: true\n")
	rv.changed()
	sh.drawer.rebuild() // a refresh of the release must not drop the typing
	if again := sh.drawer.kept[releaseValuesKey].(*releaseValues); again != rv || !strings.Contains(rv.view.Text(), "extra: true") {
		t.Fatal("a rebuild replaced the edited values")
	}
	test.Tap(rv.save)
	q.until(t, "the upgrade", func() bool { return len(up.recorded()) == 1 && !rv.busy })
	got := up.recorded()[0]
	if got.Ref.Name != "ingress-nginx" || got.Revision != 4 || got.Values == nil || !strings.Contains(*got.Values, "extra: true") || got.Tab != "" {
		t.Errorf("upgrade %+v", got)
	}
	if !strings.Contains(rv.status.text, "Upgraded to revision 5") {
		t.Errorf("status %q", rv.status.text)
	}

	up.mu.Lock()
	up.err = fmt.Errorf("%w: release ingress-nginx is at revision 6 now", engine.ErrTargetsChanged)
	up.mu.Unlock()
	rv.view.SetText(rv.view.Text() + "more: 1\n")
	test.Tap(rv.save)
	q.until(t, "the refusal", func() bool { return len(up.recorded()) == 2 && !rv.busy })
	q.until(t, "the rebase", func() bool {
		return strings.Contains(rv.status.text, "moved to revision 4") || strings.Contains(rv.status.text, "Not upgraded") || rv.loaded != ""
	})
	if !strings.Contains(rv.view.Text(), "more: 1") {
		t.Error("a refused upgrade dropped the typed text")
	}
	_ = f
}

// The Rollback dialog: every revision newest first, the current one selected and Rollback enabled
// with it, as Lens's; picking an older one and Rollback dispatches the rollback and answers the engine's
// confirmation of it without a second dialog; a confirmation whose release moved meanwhile, an
// agent's, or another command's is not answered by the dialog.
func TestRollbackDialogConfirms(t *testing.T) {
	sh, _, q, f, _ := releaseDrawerShell(t, ThemeDark)
	var asked []engine.ConfirmRequest
	var answers []bool
	var mu sync.Mutex
	f.handlers["helm.release.rollback"] = func(ctx context.Context, inv engine.Invocation) (any, error) {
		var p helmRollbackParams
		_ = json.Unmarshal(normalize(inv.Params), &p)
		req := engine.ConfirmRequest{Command: inv.ID, Label: "Rollback", Danger: engine.DangerDestructive, Origin: inv.Origin,
			Context: "demo", Namespaces: []string{p.Ref.Namespace}, Objects: []model.ObjectRef{p.Ref}, UIDs: []string{"uid-ingress-nginx-sh.helm.release.v1.ingress-nginx.v4"},
			Detail: commands.RollbackDetail(p.Revision)}
		ok, err := sh.confirmer.Confirm(ctx, req)
		mu.Lock()
		asked = append(asked, req)
		answers = append(answers, ok)
		mu.Unlock()
		return nil, err
	}
	var rd *rollbackDialog
	sh.dispatch("helm.release.history", refParams{Ref: *sh.drawer.shown}, func(res any, err error) {
		var h releaseHistory
		if err != nil || decode(res, &h) != nil {
			t.Errorf("history: %v", err)
			return
		}
		rd = sh.showRollbackDialog(h)
	})
	q.until(t, "the dialog", func() bool { return rd != nil })
	if len(rd.revision.Options) != 4 || rd.revision.Selected != rd.revision.Options[0] ||
		rd.revision.Options[0] != "4 - ingress-nginx-4.12.1 - 1.12.1, updated: 2026-10-10 12:30:00" || rd.confirm.Disabled() {
		t.Fatalf("options %q selected %q, Rollback disabled %v", rd.revision.Options, rd.revision.Selected, rd.confirm.Disabled())
	}
	rd.revision.SetSelectedIndex(2)
	if rd.confirm.Disabled() {
		t.Fatal("Rollback stays disabled for an older revision")
	}
	test.Tap(rd.confirm)
	q.until(t, "the rollback", func() bool { mu.Lock(); defer mu.Unlock(); return len(asked) == 1 })
	inv := f.callsOf("helm.release.rollback")
	if len(inv) != 1 || !strings.Contains(string(inv[0].Params), `"revision":2`) || !answers[0] || sh.confirmer.current != nil {
		t.Errorf("rollback %s, answered %v, a second dialog %v", inv[0].Params, answers, sh.confirmer.current != nil)
	}

	// The release moved while the dialog was open: refused, not asked again.
	ref := *sh.drawer.shown
	sh.confirmer.confirmedByDialog(rollbackCommand, ref, "uid-of-revision-3", rollbackDetail(2))
	req := engine.ConfirmRequest{Command: rollbackCommand, Origin: engine.OriginGUI, Context: ref.Context, Objects: []model.ObjectRef{ref}, UIDs: []string{"uid-now"},
		Detail: rollbackDetail(2)}
	if ok, handled, err := sh.confirmer.takeDialogAnswer(req); !handled || ok || err == nil {
		t.Errorf("moved: ok %v handled %v err %v", ok, handled, err)
	}
	// An agent's request, or another command, is never answered by the dialog.
	sh.confirmer.confirmedByDialog(rollbackCommand, ref, "uid-now", rollbackDetail(2))
	for _, other := range []engine.ConfirmRequest{
		{Command: rollbackCommand, Origin: engine.OriginExternalAgent, Context: ref.Context, Objects: []model.ObjectRef{ref}, UIDs: []string{"uid-now"}, Detail: rollbackDetail(2)},
		{Command: "helm.release.uninstall", Origin: engine.OriginGUI, Context: ref.Context, Objects: []model.ObjectRef{ref}, UIDs: []string{"uid-now"}, Detail: rollbackDetail(2)},
		{Command: rollbackCommand, Origin: engine.OriginGUI, Context: "elsewhere", Objects: []model.ObjectRef{ref}, UIDs: []string{"uid-now"}, Detail: rollbackDetail(2)},
		{Command: rollbackCommand, Origin: engine.OriginGUI, Context: ref.Context, Objects: []model.ObjectRef{ref}, UIDs: []string{"uid-now"}, Detail: rollbackDetail(1)},
	} {
		if _, handled, _ := sh.confirmer.takeDialogAnswer(other); handled {
			t.Errorf("answered %+v", other)
		}
	}
	if ok, handled, err := sh.confirmer.takeDialogAnswer(req); !handled || !ok || err != nil {
		t.Errorf("the user's own request: ok %v handled %v err %v", ok, handled, err)
	}
	if _, handled, _ := sh.confirmer.takeDialogAnswer(req); handled {
		t.Error("a dialog's answer was used twice")
	}
}

// Rollback with the current revision selected, as the dialog opens (captured 2026-10-12: enabled):
// the rollback is dispatched for the current revision and the dialog's answer confirms it.
func TestRollbackDialogCurrentRevision(t *testing.T) {
	sh, _, q, f, _ := releaseDrawerShell(t, ThemeDark)
	var mu sync.Mutex
	var answered []bool
	f.handlers["helm.release.rollback"] = func(ctx context.Context, inv engine.Invocation) (any, error) {
		var p helmRollbackParams
		_ = json.Unmarshal(normalize(inv.Params), &p)
		ok, err := sh.confirmer.Confirm(ctx, engine.ConfirmRequest{Command: inv.ID, Label: "Rollback", Danger: engine.DangerDestructive, Origin: inv.Origin,
			Context: "demo", Namespaces: []string{p.Ref.Namespace}, Objects: []model.ObjectRef{p.Ref},
			UIDs: []string{"uid-ingress-nginx-sh.helm.release.v1.ingress-nginx.v4"}, Detail: commands.RollbackDetail(p.Revision)})
		mu.Lock()
		answered = append(answered, ok)
		mu.Unlock()
		return nil, err
	}
	var rd *rollbackDialog
	sh.dispatch("helm.release.history", refParams{Ref: *sh.drawer.shown}, func(res any, err error) {
		var h releaseHistory
		if err == nil && decode(res, &h) == nil {
			rd = sh.showRollbackDialog(h)
		}
	})
	q.until(t, "the dialog", func() bool { return rd != nil })
	if rd.confirm.Disabled() || rd.revision.SelectedIndex() != 0 {
		t.Fatalf("Rollback disabled %v with %q selected", rd.confirm.Disabled(), rd.revision.Selected)
	}
	test.Tap(rd.confirm)
	q.until(t, "the rollback", func() bool { mu.Lock(); defer mu.Unlock(); return len(answered) == 1 })
	inv := f.callsOf("helm.release.rollback")
	if len(inv) != 1 || !strings.Contains(string(inv[0].Params), `"revision":4`) || !answered[0] || sh.confirmer.current != nil {
		t.Errorf("rollback %v, answered %v, a second dialog %v", inv, answered, sh.confirmer.current != nil)
	}
}

// The Rollback dialog's answer names the revision the user picked, in the engine's own words, and
// is dropped when its dispatch ends without taking it (refused before the confirmation), so it
// confirms no other rollback in its window (review of the fourth run, R-3).
func TestRollbackDialogAnswerBinding(t *testing.T) {
	if got, want := rollbackDetail(7), commands.RollbackDetail(7); got != want {
		t.Fatalf("the dialog's detail %q is not the engine's %q", got, want)
	}
	sh, _, q, f, _ := releaseDrawerShell(t, ThemeDark)
	var mu sync.Mutex
	done := false
	f.handlers["helm.release.rollback"] = func(context.Context, engine.Invocation) (any, error) {
		mu.Lock()
		done = true
		mu.Unlock()
		return nil, errors.New("release ingress-nginx is pending-upgrade") // refused before any confirmation
	}
	var rd *rollbackDialog
	sh.dispatch("helm.release.history", refParams{Ref: *sh.drawer.shown}, func(res any, err error) {
		var h releaseHistory
		if err == nil && decode(res, &h) == nil {
			rd = sh.showRollbackDialog(h)
		}
	})
	q.until(t, "the dialog", func() bool { return rd != nil })
	rd.revision.SetSelectedIndex(2)
	test.Tap(rd.confirm)
	q.until(t, "the refused rollback", func() bool { mu.Lock(); defer mu.Unlock(); return done })
	ref := *sh.drawer.shown
	req := engine.ConfirmRequest{Command: rollbackCommand, Origin: engine.OriginGUI, Context: ref.Context, Objects: []model.ObjectRef{ref},
		UIDs: []string{"uid-ingress-nginx-sh.helm.release.v1.ingress-nginx.v4"}, Detail: rollbackDetail(2)}
	q.until(t, "the answer dropped", func() bool {
		sh.confirmer.answers.mu.Lock()
		defer sh.confirmer.answers.mu.Unlock()
		return sh.confirmer.answers.c == nil
	})
	if _, handled, _ := sh.confirmer.takeDialogAnswer(req); handled {
		t.Error("the refused rollback's answer confirmed another request")
	}
}

// The Helm Upgrade tab: the chart, the release's own version in the picker, the info line and the
// values; Save sends the text, then upgrades with the tab; search moves the caret to a match.
func TestHelmUpgradeTabSaves(t *testing.T) {
	f, ref := releasesFake(t)
	up := &upgradeRecorder{}
	up.install(f)
	ed := newEditorFake(f)
	tab := engine.DockTab{ID: "helmUpgrade-1", Type: engine.DockHelmUpgrade, Title: "Helm Upgrade: ingress-nginx", Context: "demo", Ref: &ref,
		Helm: &engine.HelmUpgradeTab{Chart: "ingress-nginx", Version: "4.12.1", Versions: []string{"4.12.1"}, UserSuppliedOnly: true}}
	ed.texts[tab.ID] = "controller:\n  replicaCount: 2\n  service:\n    type: LoadBalancer\n"
	ed.revs[tab.ID] = 1
	f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{tab}, tab.ID
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	var u *helmUpgradeTab
	q.until(t, "the tab", func() bool {
		if c := sh.dock.contents[tab.ID]; c != nil {
			u, _ = c.editor.(*helmUpgradeTab)
		}
		return u != nil && !u.busy && strings.Contains(u.pane.view.Text(), "replicaCount")
	})
	// The version is text, the picker a separate element with its placeholder (captured 2026-10-12).
	if u.versionText.text != "version: 4.12.1 (no repository)" || u.version.Selected != "" || u.version.PlaceHolder != "Select…" ||
		len(u.version.Options) != 1 || u.version.Options[0] != "4.12.1" {
		t.Errorf("version text %q, picker %q %q %v", u.versionText.text, u.version.Selected, u.version.PlaceHolder, u.version.Options)
	}
	// The info line: the release and the namespace are links; the spinner gave way to the version.
	if u.releaseLink.text != "ingress-nginx" || u.nsLink.text != "ingress-nginx" || u.spinner.Visible() || !u.infoVersion.Visible() || u.infoVersion.text != "4.12.1" {
		t.Errorf("info line: %q %q, spinner %v, version %q", u.releaseLink.text, u.nsLink.text, u.spinner.Visible(), u.infoVersion.text)
	}
	u.pane.view.SetText("controller:\n  replicaCount: 3\n")
	test.Tap(u.saveBtn)
	q.until(t, "the upgrade", func() bool { return len(up.recorded()) == 1 && !u.busy })
	calls := f.calls
	var order []string
	for _, c := range calls {
		if c.ID == "editor.setText" || c.ID == "helm.release.upgrade" {
			order = append(order, c.ID)
		}
	}
	if strings.Join(order, ",") != "editor.setText,helm.release.upgrade" || up.recorded()[0].Tab != tab.ID || up.recorded()[0].Values != nil {
		t.Errorf("order %v, upgrade %+v", order, up.recorded()[0])
	}
	ed.mu.Lock()
	sent, rev := ed.texts[tab.ID], ed.revs[tab.ID]
	ed.mu.Unlock()
	if sent != "controller:\n  replicaCount: 3\n" || !strings.Contains(u.status.text, "Upgraded to revision 5") {
		t.Errorf("sent %q, status %q", sent, u.status.text)
	}
	// The upgrade is bound to the text revision Save's own editor.setText made (R-2).
	if got := up.recorded()[0].TextRevision; got == 0 || got != rev {
		t.Errorf("upgrade at text revision %d, Save's setText made %d", got, rev)
	}

	u.toggleSearch()
	u.find.find.SetText("REPLICA")
	if line, col := u.pane.view.CursorPosition(); line != 1 || col != 9 || u.pane.view.SelectedText() != "replica" {
		t.Errorf("find moved the caret to %d:%d, selected %q", line, col, u.pane.view.SelectedText())
	}
}

// The info line's links lead to the release and its namespace.
func TestHelmUpgradeTabLinks(t *testing.T) {
	f, ref := releasesFake(t)
	ed := newEditorFake(f)
	tab := engine.DockTab{ID: "helmUpgrade-1", Type: engine.DockHelmUpgrade, Title: "Helm Upgrade: ingress-nginx", Context: "demo", Ref: &ref,
		Helm: &engine.HelmUpgradeTab{Chart: "ingress-nginx", Version: "4.12.1", Versions: []string{"4.12.1"}, UserSuppliedOnly: true}}
	ed.texts[tab.ID], ed.revs[tab.ID] = "a: 1\n", 1
	f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{tab}, tab.ID
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	var u *helmUpgradeTab
	q.until(t, "the tab", func() bool {
		if c := sh.dock.contents[tab.ID]; c != nil {
			u, _ = c.editor.(*helmUpgradeTab)
		}
		return u != nil && !u.busy && u.pane.view.Text() == "a: 1\n"
	})
	test.Tap(u.nsLink)
	q.until(t, "the namespace", func() bool {
		for _, c := range f.callsOf("ui.navigate") {
			if strings.Contains(string(c.Params), `"kind":"namespaces"`) && strings.Contains(string(c.Params), `"name":"ingress-nginx"`) {
				return true
			}
		}
		return false
	})
	test.Tap(u.releaseLink)
	q.until(t, "the release", func() bool {
		for _, c := range f.callsOf("ui.navigate") {
			if strings.Contains(string(c.Params), `"kind":"`+helm.ReleaseKind+`"`) && strings.Contains(string(c.Params), `"name":"ingress-nginx"`) {
				return true
			}
		}
		return false
	})
}

// The banner offers an interrupted upgrade's remedy: a rollback to the revision it started from.
func TestHelmBannerOffersUpgradeRollback(t *testing.T) {
	sh, _, q, h := newHelmTestShell(t, ThemeDark)
	ops := []engine.PendingHelmOpInfo{{
		PendingHelmOp: engine.PendingHelmOp{Context: "kind-cogpit", Namespace: "monitoring", Release: "kube-prometheus-stack",
			Action: engine.HelmUpgrade, FromRevision: 11, Chart: "kube-prometheus-stack", ChartVersion: "66.3.0",
			Started: time.Date(2026, 10, 12, 7, 2, 0, 0, time.UTC), StartedBy: engine.OriginGUI, State: engine.HelmOpRunning},
		Status: engine.HelmOpStatusInterrupted, ReleaseStatus: "pending-upgrade", ReleaseRevision: 12,
	}}
	h.set(ops)
	q.until(t, "the banner", func() bool { return sh.helm.root.Visible() && len(sh.helm.ops) == 1 })
	b := helmBannerButtons(sh.helm)
	if len(b["Roll back to revision 11"]) != 1 {
		t.Fatalf("buttons %v", b)
	}
	if got := helmOpTitle(ops[0]); got != "The upgrade of release kube-prometheus-stack from revision 11 to kube-prometheus-stack 66.3.0 did not finish" {
		t.Errorf("title %q", got)
	}
	test.Tap(b["Roll back to revision 11"][0])
	q.until(t, "the rollback", func() bool { return len(h.recorded()) == 1 })
	if got := h.recorded()[0]; got.ID != "helm.release.rollback" || !strings.Contains(string(got.Params), `"revision":11`) {
		t.Errorf("dispatched %s %s", got.ID, got.Params)
	}
}

// renderReleaseDrawer renders the release drawer, its top (Properties, values) or scrolled to its
// end (notes, resources). Fyne's software painter does not clip a scrollable (the values editor)
// inside the drawer's scroll by the outer scroll, as the GL painter's clip stack does: scrolled to
// its end, the editor's text shows through between the top bar and the drawer's header in the
// PNG only. The committed PNGs show the top.
func renderReleaseDrawer(t *testing.T, mode ThemeMode, name string, end bool) {
	sh, w, _, _, _ := releaseDrawerShell(t, mode)
	if end {
		settle(w)
		for _, s := range findAll[*container.Scroll](sh.drawer.root) {
			s.ScrollToBottom()
		}
	}
	savePNG(t, w, name)
}

func TestRenderReleaseDrawerDark(t *testing.T) {
	renderReleaseDrawer(t, ThemeDark, "release_drawer_dark.png", false)
}

// failedReleaseDrawerShell opens checkout-api's drawer: revision 5 failed, revision 4 deployed.
func failedReleaseDrawerShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window) {
	t.Helper()
	f, _ := releasesFake(t)
	ref := model.ObjectRef{Context: "demo", Kind: helm.ReleaseKind, Namespace: "checkout", Name: "checkout-api"}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "checkout-api's drawer", func() bool {
		return sh.view.snap != nil && sh.drawer.last != nil && sh.drawer.last.Ref == ref && sh.diag.total > 0
	})
	settle(w)
	return sh, w
}

// The History at the drawer's end: every revision newest first, the failed one in the error
// colour, its description (Helm's error) on a plate under its row across the drawer, wrapped onto
// lines instead of cut, each line's copy icon copying the whole message (the owner's colleague,
// 2026-10-25, after k9s's history; the owner placed it and the plate the same day).
func TestReleaseHistoryShowsTheDescriptionUnderItsRow(t *testing.T) {
	sh, _ := failedReleaseDrawerShell(t, ThemeDark)
	d := sh.drawer
	msg := `Upgrade "checkout-api" failed: client rate limiter Wait returned an error: context deadline exceeded`
	var parts []*selLine
	for _, l := range findAll[*selLine](d.details) {
		if l.hook != nil && l.hook.text == msg && l.line.color == colorStatusError {
			parts = append(parts, l)
		}
	}
	var words []string
	for _, l := range parts {
		words = append(words, l.line.text)
		if w := fyne.MeasureText(l.line.text, sh.app.Settings().Theme().Size(theme.SizeNameText), fyne.TextStyle{}).Width; w > l.Size().Width+0.5 {
			t.Errorf("line %q needs %v, has %v: cut", l.line.text, w, l.Size().Width)
		}
	}
	if len(parts) < 2 || strings.Join(words, " ") != msg {
		t.Fatalf("the failed revision's description in %d lines: %q", len(parts), words)
	}
	// Properties' Description: the newest revision's, wrapped within the value's width, not cut.
	var props []string
	for _, l := range findAll[*selLine](d.details) {
		if l.hook != nil && l.hook.text == msg && l.line.color == theme.ColorNameForeground {
			props = append(props, l.line.text)
			if fyne.MeasureText(l.line.text, sh.app.Settings().Theme().Size(theme.SizeNameText), fyne.TextStyle{}).Width > d.valueWidth()+0.5 {
				t.Errorf("Properties' line %q is wider than the value's %v", l.line.text, d.valueWidth())
			}
		}
	}
	if len(props) < 2 || strings.Join(props, " ") != msg {
		t.Errorf("Properties' description in %d lines: %q", len(props), props)
	}
	// Under its row: below revision 5's status, from the table's left edge.
	var failed *textLine
	for _, l := range findAll[*textLine](d.details) {
		if l.text == "Failed" && l.color == colorStatusError {
			failed = l
		}
	}
	drv := fyne.CurrentApp().Driver()
	if failed == nil {
		t.Fatal("no Failed in the History")
	}
	at, top := drv.AbsolutePositionForObject(parts[0]), drv.AbsolutePositionForObject(failed)
	if at.Y <= top.Y || at.X >= top.X {
		t.Errorf("the description at %v, the row's status at %v: want it under the row, from the table's left", at, top)
	}
	if secs := d.last.Sections; secs[len(secs)-1].Title != helm.TitleHistory {
		t.Errorf("the last section is %q, want the History", secs[len(secs)-1].Title)
	}
	texts := releaseDrawerText(d)
	for _, s := range []string{"History", "Revision", "Failed", "checkout-api-2.15.0", "checkout-api-2.14.0", "Upgrade complete"} {
		if !strings.Contains(texts, s) {
			t.Errorf("the drawer lacks %q", s)
		}
	}
}

// renderReleaseHistory renders checkout-api's drawer scrolled to its end, the History.
func renderReleaseHistory(t *testing.T, mode ThemeMode, name string) {
	sh, w := failedReleaseDrawerShell(t, mode)
	sh.drawer.scroll.ScrollToBottom()
	savePNG(t, w, name)
}

func TestRenderReleaseHistoryDark(t *testing.T) {
	renderReleaseHistory(t, ThemeDark, "release_history_dark.png")
}

// TestRenderRollbackDialog and its dropdown's renders: render_dialogs_test.go.

func renderHelmUpgradeTab(t *testing.T, mode ThemeMode, name string, find bool) {
	f, ref := releasesFake(t)
	ed := newEditorFake(f)
	tab := engine.DockTab{ID: "helmUpgrade-1", Type: engine.DockHelmUpgrade, Title: "Helm Upgrade: ingress-nginx", Context: "demo", Ref: &ref,
		Helm: &engine.HelmUpgradeTab{Chart: "ingress-nginx", Version: "4.12.1", Versions: []string{"4.12.1"}, UserSuppliedOnly: true}}
	ed.texts[tab.ID] = "auth:\n  adminPassword: correct-horse-battery-staple\ncontroller:\n  replicaCount: 2\n  service:\n    type: LoadBalancer\n"
	ed.revs[tab.ID] = 1
	f.handlers["editor.get"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		ed.mu.Lock()
		defer ed.mu.Unlock()
		return engine.EditorState{Tab: tab, Text: ed.texts[tab.ID], Revision: 1, BaseRevision: 4, CachedRevision: 4}, nil
	}
	f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{tab}, tab.ID
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the tab", func() bool {
		c := sh.dock.contents[tab.ID]
		if c == nil {
			return false
		}
		u, _ := c.editor.(*helmUpgradeTab)
		rv, _ := sh.drawer.kept[releaseValuesKey].(*releaseValues)
		return u != nil && !u.busy && strings.Contains(u.status.text, "revision 4") && sh.drawer.last != nil && sh.diag.total > 0 &&
			rv != nil && rv.revision == 4
	})
	if find { // the editor's find widget with a match
		u := sh.dock.contents[tab.ID].editor.(*helmUpgradeTab)
		u.toggleSearch()
		settle(w) // the editor shrinks for the widget before the match is revealed
		u.find.replace.SetText("3")
		u.find.find.SetText("replica")
	}
	savePNG(t, w, name)
}

func TestRenderHelmUpgradeTabDark(t *testing.T) {
	renderHelmUpgradeTab(t, ThemeDark, "helm_upgrade_tab_dark.png", false)
}
