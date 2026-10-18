package fyneui

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// draftsFake answers editor.drafts with a list a test sets, and records restores and discards.
type draftsFake struct {
	f    *fakeEngine
	mu   sync.Mutex
	list []engine.DraftInfo
}

func newDraftsFake(f *fakeEngine) *draftsFake {
	d := &draftsFake{f: f}
	f.handlers["editor.drafts"] = func(context.Context, engine.Invocation) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return append([]engine.DraftInfo(nil), d.list...), nil
	}
	gone := func(_ context.Context, inv engine.Invocation) (any, error) {
		var p draftParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		for i, x := range d.list {
			if x.ID == p.ID {
				d.list = append(d.list[:i:i], d.list[i+1:]...)
				break
			}
		}
		d.mu.Unlock()
		go f.emit(engine.Event{Type: engine.EventDrafts})
		return nil, nil
	}
	f.handlers["editor.restoreDraft"] = gone
	f.handlers["editor.discardDraft"] = gone
	return d
}

func (d *draftsFake) set(list []engine.DraftInfo) {
	d.mu.Lock()
	d.list = list
	d.mu.Unlock()
	d.f.emit(engine.Event{Type: engine.EventDrafts, Message: "Cogpit kept the unsaved text of 2 editor tabs"})
}

// fixtureDrafts: an Edit tab's changes to a Service and a Create resource tab's text, both kept
// when Cogpit ended.
func fixtureDrafts() []engine.DraftInfo {
	saved := time.Date(2026, 10, 11, 14, 2, 0, 0, time.UTC)
	return []engine.DraftInfo{
		{ID: strings.Repeat("a", 32), Type: engine.DockEdit, Context: "demo", Title: "Service checkout",
			Ref:                 &model.ObjectRef{Context: "demo", Kind: "services", Namespace: "storefront", Name: "checkout"},
			BaseResourceVersion: "41", Saved: saved, Lines: 21, Bytes: 410},
		{ID: strings.Repeat("b", 32), Type: engine.DockCreate, Context: "demo", Title: "Create resource",
			Saved: saved.Add(3 * time.Minute), Lines: 12, Bytes: 230},
	}
}

func draftsButtons(b *draftsBanner) map[string][]*widget.Button {
	out := map[string][]*widget.Button{}
	for _, x := range findAll[*widget.Button](b.root) {
		out[x.Text] = append(out[x.Text], x)
	}
	return out
}

func draftsScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine, *draftsFake) {
	t.Helper()
	f := connectedFake(t, 200)
	d := newDraftsFake(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	return sh, w, q, f, d
}

// TestDraftsBannerOffersAndActs: the drafts the engine offers show below the top bar, one row
// each, and Restore and Discard dispatch editor.restoreDraft and editor.discardDraft with the
// draft's id; the row goes once the engine says so.
func TestDraftsBannerOffersAndActs(t *testing.T) {
	sh, _, q, f, d := draftsScene(t, ThemeDark)
	if sh.drafts.root.Visible() {
		t.Fatal("the banner shows without drafts")
	}
	d.set(fixtureDrafts())
	q.until(t, "the banner", func() bool { return sh.drafts.root.Visible() && len(sh.drafts.drafts) == 2 })
	if got := draftTitle(fixtureDrafts()[0]); got != "Unsaved changes to Service checkout" {
		t.Errorf("title %q", got)
	}
	if got := draftDetail(fixtureDrafts()[0], germanTime); got != "Context demo, namespace storefront, kept 11.10.2026 14:02:00 · 21 lines" {
		t.Errorf("detail %q", got)
	}
	if got := draftTitle(fixtureDrafts()[1]); got != "Unsaved text of a Create resource tab" {
		t.Errorf("title %q", got)
	}
	drawer := engine.DraftInfo{ID: strings.Repeat("c", 32), Type: engine.DraftConfigMapData, Context: "demo", Title: "ConfigMap cilium-config",
		Ref: &ciliumRef, BaseResourceVersion: "4211", Saved: fixtureDrafts()[0].Saved, Keys: []string{"allow-localhost", "bpf-lb-map-max"}}
	if got := draftTitle(drawer); got != "Unsaved values of ConfigMap cilium-config (drawer)" {
		t.Errorf("a drawer draft's title %q", got)
	}
	if got := draftDetail(drawer, germanTime); got != "Context demo, namespace kube-system, kept 11.10.2026 14:02:00 · 2 values" {
		t.Errorf("a drawer draft's detail %q", got)
	}
	b := draftsButtons(sh.drafts)
	if len(b["Restore"]) != 2 || len(b["Discard"]) != 2 {
		t.Fatalf("buttons %v", b)
	}
	test.Tap(b["Restore"][0])
	q.until(t, "the restore", func() bool { return len(f.callsOf("editor.restoreDraft")) == 1 && len(sh.drafts.drafts) == 1 })
	if p := decodeParams[draftParams](t, f.callsOf("editor.restoreDraft")[0]); p.ID != strings.Repeat("a", 32) {
		t.Errorf("restored %+v", p)
	}
	test.Tap(draftsButtons(sh.drafts)["Discard"][0])
	q.until(t, "the discard", func() bool { return len(f.callsOf("editor.discardDraft")) == 1 && !sh.drafts.root.Visible() })
	if p := decodeParams[draftParams](t, f.callsOf("editor.discardDraft")[0]); p.ID != strings.Repeat("b", 32) {
		t.Errorf("discarded %+v", p)
	}
}

func renderDraftsBanner(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, d := draftsScene(t, mode)
	d.set(fixtureDrafts())
	q.until(t, "the banner", func() bool { return sh.drafts.root.Visible() && len(sh.drafts.drafts) == 2 })
	savePNG(t, w, name)
}

func TestRenderDraftsBannerDark(t *testing.T) {
	renderDraftsBanner(t, ThemeDark, "drafts_banner_dark.png")
}

func TestRenderDraftsBannerLight(t *testing.T) {
	renderDraftsBanner(t, ThemeLight, "drafts_banner_light.png")
}
