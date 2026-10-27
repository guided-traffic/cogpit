package fyneui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The editor tabs against the fake engine: editorFake answers the editor commands the way the
// engine does (text and revision per tab, a diff, a dry run, an apply or a conflict).

const serviceYAML = `apiVersion: v1
kind: Service
metadata:
  name: checkout
  namespace: storefront
  resourceVersion: "41"
  uid: 7d1c2a7e-93d8-4b6e-a0c5-1d2e3f4a5b6c
spec:
  clusterIP: 10.96.100.100
  ports:
  - name: http
    port: 80
    protocol: TCP
    targetPort: 8080
  selector:
    app: checkout
  type: ClusterIP
status:
  loadBalancer: {}
`

var checkoutRef = model.ObjectRef{Context: "demo", Kind: "services", Namespace: "storefront", Name: "checkout"}

type editorFake struct {
	f *fakeEngine

	mu       sync.Mutex
	texts    map[string]string
	revs     map[string]uint64
	base     string // the object as the cluster holds it: what editor.diff compares with
	conflict bool   // the next apply answers with a conflict
	created  engine.CreateResult
	createEr error
}

func newEditorFake(f *fakeEngine) *editorFake {
	e := &editorFake{f: f, texts: map[string]string{}, revs: map[string]uint64{}, base: serviceYAML}
	params := func(inv engine.Invocation, p any) error { return json.Unmarshal(normalize(inv.Params), p) }
	tabOf := func(id string) engine.DockTab {
		t, _ := f.UIState().DockTab(id)
		return t
	}
	f.handlers["editor.get"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p dockParams
		if err := params(inv, &p); err != nil {
			return nil, err
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		return engine.EditorState{Tab: tabOf(p.ID), Text: e.texts[p.ID], Revision: e.revs[p.ID], BaseResourceVersion: "41", CachedResourceVersion: "41"}, nil
	}
	f.handlers["editor.setText"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p editorTextParams
		if err := params(inv, &p); err != nil {
			return nil, err
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.texts[p.ID] != p.YAML {
			e.texts[p.ID] = p.YAML
			e.revs[p.ID]++
		}
		return map[string]any{"revision": e.revs[p.ID]}, nil
	}
	f.handlers["editor.diff"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p editorDiffParams
		if err := params(inv, &p); err != nil {
			return nil, err
		}
		e.mu.Lock()
		base := e.base
		e.mu.Unlock()
		lines := engine.CompactDiff(engine.DiffLines(base, p.YAML), p.Context)
		d := engine.EditDiff{Ref: checkoutRef, Changed: engine.DiffChanged(lines), Lines: lines, BaseResourceVersion: "41", CachedResourceVersion: "41"}
		for _, l := range lines {
			switch l.Op {
			case engine.DiffAdded:
				d.Added++
			case engine.DiffRemoved:
				d.Removed++
			}
		}
		return d, nil
	}
	f.handlers["object.dryRun"] = func(context.Context, engine.Invocation) (any, error) {
		return engine.DryRunResult{Ref: checkoutRef, Accepted: true, BaseResourceVersion: "41"}, nil
	}
	f.handlers["object.apply"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p editorTextParams
		if err := params(inv, &p); err != nil {
			return nil, err
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.conflict {
			e.conflict = false
			lines := engine.DiffLines(strings.Replace(serviceYAML, "  selector:\n", "  selector:\n    tier: web\n", 1), p.YAML)
			return nil, &engine.ConflictError{Ref: checkoutRef, BaseResourceVersion: "41", CurrentResourceVersion: "57",
				Diff: engine.CompactDiff(lines, engine.DefaultDiffContext), Rebased: true} // an apply rebases
		}
		stored := strings.Replace(p.YAML, `resourceVersion: "41"`, `resourceVersion: "58"`, 1)
		e.texts[p.ID] = stored
		e.revs[p.ID]++
		e.base = stored
		return engine.ApplyResult{Ref: checkoutRef, ResourceVersion: "58", Text: stored, Revision: e.revs[p.ID]}, nil
	}
	f.handlers["resource.templates"] = func(context.Context, engine.Invocation) (any, error) {
		return engine.Templates()
	}
	f.handlers["object.create"] = func(context.Context, engine.Invocation) (any, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.created, e.createEr
	}
	f.handlers["resource.createTab"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p createTabParams
		if err := params(inv, &p); err != nil {
			return nil, err
		}
		name := p.Template
		if name == "" {
			name = engine.DefaultTemplate
		}
		tpl, err := engine.TemplateByName(name)
		if err != nil {
			return nil, err
		}
		f.mu.Lock()
		id := fmt.Sprintf("create-%d", len(f.ui.Dock)+1)
		f.mu.Unlock()
		e.setText(id, tpl.Text) // before the tab appears: the window reads it when it opens the tab
		var tab engine.DockTab
		f.update(func(u *engine.UIState) {
			tab = engine.DockTab{ID: id, Type: engine.DockCreate, Title: "Create resource", Context: u.Context}
			u.Dock = append(u.Dock, tab)
			u.ActiveDock = tab.ID
		})
		return commands.EditResult{Tab: tab, Created: true, Text: tpl.Text}, nil // as the engine answers (commands/edit.go)
	}
	reg := func(id string, d engine.Danger) {
		spec := engine.Spec[struct{}]{ID: id, Label: id, Danger: d, Run: func(context.Context, *engine.Session, struct{}) (any, error) { return nil, nil }}
		if d == engine.DangerMutating {
			spec.Targets = func(*engine.Session, struct{}) ([]model.ObjectRef, error) { return nil, nil }
		}
		_ = engine.Register(f.reg, spec)
	}
	reg("resource.createTab", engine.DangerUI)
	return e
}

func (e *editorFake) setText(id, text string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.texts[id] = text
	e.revs[id]++
}

// editScene is the shell with an Edit tab on a service open in the dock.
func editScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine, *editorFake, *editorTab) {
	t.Helper()
	f := connectedFake(t, 200)
	ed := newEditorFake(f)
	r := checkoutRef
	tab := engine.DockTab{ID: "edit-1", Type: engine.DockEdit, Title: "Service checkout", Context: "demo", Ref: &r}
	f.ui.Dock = []engine.DockTab{tab}
	f.ui.ActiveDock = tab.ID
	ed.setText(tab.ID, serviceYAML)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	var et *editorTab
	q.until(t, "the editor to load", func() bool {
		c, ok := sh.dock.contents[tab.ID]
		if !ok {
			return false
		}
		et, _ = c.editor.(*editorTab)
		return et != nil && et.pane.view.Text() == serviceYAML && len(sh.kinds) > 0
	})
	return sh, w, q, f, ed, et
}

// typeAt types text into the editor at a 0-based line and column, as the user would.
func typeAt(e *editorPane, line, col int, text string) {
	e.view.SetCursorPosition(line, col)
	test.Type(e.view, text)
}

// typeLineAfter opens a new line after a 0-based line with Return, which indents it like that
// line, and types text into it.
func typeLineAfter(e *editorPane, line int, text string) {
	e.view.SetCursorPosition(line, 1<<20)
	e.view.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	test.Type(e.view, text)
}

func TestEditTabSaveReviewsThenApplies(t *testing.T) {
	sh, _, q, f, ed, et := editScene(t, ThemeDark)
	if et.pane.view.ReadOnly() || et.reviewing {
		t.Fatal("the edit tab starts in the editor, editable")
	}
	// The title and the time it loaded, nothing else: no resourceVersion ("41", D-85). On the shell's
	// clock, so that a wall clock at hh:41 or hh:mm:41 cannot match (CI, 2026-10-18: run 39161105286).
	if want := "Service checkout · loaded at " + sh.timeFormat().FormatTime(fixedNow); et.status.text != want {
		t.Errorf("status %q, want %q", et.status.text, want)
	}
	typeLineAfter(et.pane, 16, "sessionAffinity: ClientIP")
	q.until(t, "the text to reach the engine", func() bool { return len(f.callsOf("editor.setText")) == 1 })
	edited := et.pane.view.Text()

	test.Tap(et.saveBtn)
	q.until(t, "the review", func() bool { return et.reviewing && et.applyBtn.Disabled() == false })
	if p := decodeParams[editorDiffParams](t, f.callsOf("editor.diff")[0]); p.YAML != edited || p.Context != engine.DefaultDiffContext {
		t.Errorf("diff params %+v", p)
	}
	if n := len(f.callsOf("object.dryRun")); n != 1 {
		t.Errorf("%d dry runs", n)
	}
	if !strings.Contains(et.banner.text, "1 added, 0 removed") || !strings.Contains(et.status.text, "accepts") {
		t.Errorf("banner %q status %q", et.banner.text, et.status.text)
	}
	if len(et.diff.rows) == 0 {
		t.Fatal("no diff rows")
	}

	test.Tap(et.applyBtn)
	q.until(t, "the apply", func() bool { return !et.reviewing })
	if p := decodeParams[editorTextParams](t, f.callsOf("object.apply")[0]); p.YAML != edited || p.ID != "edit-1" {
		t.Errorf("apply params %+v", p)
	}
	if !strings.Contains(et.pane.view.Text(), `resourceVersion: "58"`) || !strings.Contains(et.status.text, "Saved") {
		t.Errorf("after apply: status %q text %q", et.status.text, et.pane.view.Text())
	}

	// Nothing changed since: Save says so and does not review.
	test.Tap(et.saveBtn)
	q.until(t, "the second diff", func() bool { return len(f.callsOf("editor.diff")) == 2 && !et.busy })
	if et.reviewing || et.status.text != "No changes to save." || len(f.callsOf("object.dryRun")) != 1 {
		t.Errorf("save without changes: reviewing %v, status %q", et.reviewing, et.status.text)
	}
	_ = ed
	_ = sh
}

func TestEditTabConflictAsksAgain(t *testing.T) {
	_, _, q, f, ed, et := editScene(t, ThemeDark)
	typeLineAfter(et.pane, 16, "sessionAffinity: ClientIP")
	ed.mu.Lock()
	ed.conflict = true
	ed.mu.Unlock()
	test.Tap(et.saveBtn)
	q.until(t, "the review", func() bool { return et.reviewing && !et.applyBtn.Disabled() })
	test.Tap(et.applyBtn)
	q.until(t, "the conflict", func() bool { return strings.Contains(et.banner.text, "nothing was applied") })
	if !et.reviewing || et.applyBtn.Disabled() || !strings.Contains(et.status.text, "overwrite the cluster's change") {
		t.Errorf("after the conflict: reviewing %v apply disabled %v status %q", et.reviewing, et.applyBtn.Disabled(), et.status.text)
	}
	removed := false
	for _, r := range et.diff.rows {
		removed = removed || (r.line.Op == engine.DiffRemoved && strings.Contains(r.line.Text, "tier: web"))
	}
	if !removed {
		t.Error("the conflict's diff does not show the other change")
	}
	test.Tap(et.applyBtn)
	q.until(t, "the second apply", func() bool { return !et.reviewing })
	if n := len(f.callsOf("object.apply")); n != 2 {
		t.Errorf("%d applies", n)
	}
}

func TestEditorFollowsTextFromAgents(t *testing.T) {
	sh, _, q, f, ed, et := editScene(t, ThemeDark)
	ed.setText("edit-1", "apiVersion: v1\nkind: Service\n")
	go f.emit(engine.Event{Type: engine.EventEditor, View: "edit-1", Revision: 2})
	q.until(t, "the agent's text", func() bool { return et.pane.view.Text() == "apiVersion: v1\nkind: Service\n" })

	// The user's unsent typing wins over a text from elsewhere.
	typeAt(et.pane, 0, 0, "# mine\n")
	ed.setText("edit-1", "other\n")
	go f.emit(engine.Event{Type: engine.EventEditor, View: "edit-1", Revision: 3})
	q.until(t, "the user's text to be sent", func() bool { return len(f.callsOf("editor.setText")) == 1 })
	if !strings.HasPrefix(et.pane.view.Text(), "# mine") {
		t.Errorf("the user's typing was replaced: %q", et.pane.view.Text())
	}
	_ = sh
}

func TestCreateTabTemplatesAndCreate(t *testing.T) {
	f := connectedFake(t, 20)
	ed := newEditorFake(f)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })

	// The floating "+" opens a Create resource tab from the view's kind's template.
	if !sh.view.create.Visible() {
		t.Fatal("the + button is hidden on a connected list")
	}
	test.Tap(sh.view.create)
	q.until(t, "the create tab", func() bool { return len(sh.dock.contents) == 1 })
	if p := decodeParams[createTabParams](t, f.callsOf("resource.createTab")[0]); p.Template != "Pod" {
		t.Errorf("create tab params %+v", p)
	}
	var ct *createTab
	q.until(t, "the editor to load", func() bool {
		ct, _ = sh.dock.contents["create-1"].editor.(*createTab)
		return ct != nil && strings.Contains(ct.pane.view.Text(), "kind: Pod") && len(ct.templates) > 0
	})

	heading, list := ct.templateList()
	if heading.text != "Cogpit" || list.Length() != len(ct.templates) || ct.templates[0].Name != "ClusterRole" {
		t.Errorf("template picker: heading %q, %d of %d templates", heading.text, list.Length(), len(ct.templates))
	}
	for i, tp := range ct.templates {
		if tp.Name == "ConfigMap" {
			list.Select(i)
		}
	}
	if !strings.Contains(ct.pane.view.Text(), "kind: ConfigMap") || ct.picker.Text != "ConfigMap" {
		t.Errorf("after choosing ConfigMap: %q", ct.pane.view.Text())
	}
	q.until(t, "the template text to reach the engine", func() bool { return len(f.callsOf("editor.setText")) == 1 })

	ed.mu.Lock()
	ed.created = engine.CreateResult{Items: []engine.CreateItem{{Ref: model.ObjectRef{Kind: "configmaps", Namespace: "default", Name: "example-config"}, Created: true}}, Created: 1}
	ed.mu.Unlock()
	test.Tap(ct.closeBtn)
	q.until(t, "the create", func() bool { return len(f.callsOf("object.create")) == 1 && !ct.busy })
	if p := decodeParams[createParams](t, f.callsOf("object.create")[0]); !strings.Contains(p.YAML, "kind: ConfigMap") {
		t.Errorf("create params %+v", p)
	}
	q.until(t, "the tab to close", func() bool { return len(sh.dock.contents) == 0 })

	// A partial failure lists every object and keeps the tab.
	test.Tap(sh.view.create)
	q.until(t, "a second create tab", func() bool {
		c, ok := sh.dock.contents["create-1"]
		if !ok {
			return false
		}
		ct, _ = c.editor.(*createTab)
		return ct != nil && ct.pane.view.Text() != ""
	})
	ed.mu.Lock()
	ed.created = engine.CreateResult{Created: 1, Failed: 1, Items: []engine.CreateItem{
		{Ref: model.ObjectRef{Kind: "configmaps", Namespace: "default", Name: "a"}, Created: true},
		{Ref: model.ObjectRef{Kind: "Secret", Namespace: "default", Name: "b"}, Error: "editing and creating Secrets from YAML is off"},
	}}
	ed.createEr = fmt.Errorf("1 of 2 objects not created")
	ed.mu.Unlock()
	test.Tap(ct.closeBtn)
	q.until(t, "the results", func() bool { return len(ct.results.Objects) == 2 })
	if ct.status.text != "Created 1 of 2 objects." || len(sh.dock.contents) != 1 {
		t.Errorf("status %q, %d tabs", ct.status.text, len(sh.dock.contents))
	}
}

func TestCreateButtonHiddenWithoutCluster(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	sh, _, q := newTestShell(t, f, Options{})
	q.drain()
	if sh.view.create.Visible() {
		t.Error("the + button shows without a cluster")
	}
}
