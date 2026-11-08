package fyneui

import (
	"context"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// jumpShell is the palette scene with the kinds' short names, as kinds.list reports them from the
// cluster's discovery, and the contexts of the navigator's section loaded; a context's login program
// has nothing left to allow.
func jumpShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f, _ := paletteFake(t)
	short := map[string]string{"nodes": "no", "pods": "po", "deployments": "deploy", "services": "svc", "namespaces": "ns", "events": "ev"}
	for i, k := range f.kinds {
		f.kinds[i].Singular, f.kinds[i].ShortNames = k.APIKind, []string{short[k.Kind]}
	}
	f.handlers["kubeconfig.pendingApprovals"] = func(context.Context, engine.Invocation) (any, error) {
		return map[string]any{"groups": []any{}}, nil
	}
	sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "the shell", func() bool {
		return len(sh.kinds) > 0 && len(sh.side.sec.contexts) > 0 && sh.drawer.last != nil && sh.nsFor == sh.ui.Context
	})
	return sh, w, q, f
}

// The `:` key's openJump opens the palette through the engine with ":" typed, the field focused and
// the caret after the colon: the user goes on typing a jump, k9s's :po kube-sys, whose first entry
// completes the namespace and runs ui.jump once the palette closed.
func TestOpenJumpTypesAColon(t *testing.T) {
	sh, w, q, f := jumpShell(t, ThemeDark)
	w.Canvas().Focus(sh.view.search)
	sh.openJump()
	p := sh.palette
	q.until(t, "the palette with a colon", func() bool { return p.visible() && p.filter.Text == ":" && len(p.shown) > 0 })
	if w.Canvas().Focused() != p.filter || p.filter.CursorColumn != 1 {
		t.Errorf("focus on %T, caret at %d; want the palette's field, after the colon", w.Canvas().Focused(), p.filter.CursorColumn)
	}
	if n := len(f.callsOf(engine.PaletteOpenCommand)); n != 1 || !sh.ui.Palette {
		t.Errorf("%d ui.palette.open, open state %v", n, sh.ui.Palette)
	}
	if p.shown[0].Label != "Switch to Cluster Overview" || p.hint.text != jumpHint {
		t.Errorf("first entry %q, hint %q; want every view, the jump's words", p.shown[0].Label, p.hint.text)
	}

	typeInPalette(t, sh, q, "po kube-sys")
	if p.filter.Text != ":po kube-sys" {
		t.Fatalf("typed %q", p.filter.Text)
	}
	if got := shownLabels(p); len(got) != 1 || got[0] != "Switch to Pods in kube-system" {
		t.Fatalf("the jump's entries %q", got)
	}
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "ui.jump", func() bool { return len(f.callsOf(engine.JumpCommand)) == 1 && !p.visible() })
	if got := string(f.callsOf(engine.JumpCommand)[0].Params); got != `{"view":"pods","namespace":"kube-system"}` {
		t.Errorf("ui.jump with %s", got)
	}

	sh.openJump() // the palette opens anew with the colon, not with the last text
	q.until(t, "the palette again", func() bool { return p.visible() && p.filter.Text == ":" })
	typeInPalette(t, sh, q, "xyz")
	if len(p.shown) != 1 || p.shown[0].Reason != "no view named “xyz”" {
		t.Errorf("an unknown name: %+v", p.shown)
	}
	pressInPalette(sh, q, fyne.KeyReturn)
	if !p.visible() || len(f.callsOf(engine.JumpCommand)) != 1 {
		t.Error("a greyed jump ran or closed the palette")
	}
	p.filter.SetText("")
	q.until(t, "the commands again", func() bool { return len(p.shown) > 0 && p.shown[0].Command != engine.JumpCommand })
	if p.hint.text != paletteHint {
		t.Errorf("without the colon: hint %q, want the palette's", p.hint.text)
	}
}

// A jump to a cluster that is not connected, and :ctx's entry of one, ask for its login program's
// approval first, as every connect in the window does, then run.
func TestJumpToAnotherClusterReviewsItsLogin(t *testing.T) {
	sh, _, q, f := jumpShell(t, ThemeDark)
	p := sh.palette
	sh.openJump()
	q.until(t, "the palette", func() bool { return p.visible() && p.filter.Text == ":" })
	typeInPalette(t, sh, q, "svc @staging")
	if got := shownLabels(p); len(got) != 1 || got[0] != "Switch to Services on staging-us-east" {
		t.Fatalf("the jump's entries %q", got)
	}
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "ui.jump", func() bool { return len(f.callsOf(engine.JumpCommand)) == 1 })
	if n := len(f.callsOf("kubeconfig.pendingApprovals")); n != 1 {
		t.Errorf("%d reviews of the login before the jump, want 1", n)
	}
	if got := string(f.callsOf(engine.JumpCommand)[0].Params); got != `{"view":"services","context":"staging-us-east"}` {
		t.Errorf("ui.jump with %s", got)
	}

	sh.openJump()
	q.until(t, "the palette again", func() bool { return p.visible() && p.filter.Text == ":" })
	typeInPalette(t, sh, q, "ctx prod")
	if got := shownLabels(p); len(got) != 1 || got[0] != "Open prod-eu-central" {
		t.Fatalf(":ctx prod: %q", got)
	}
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "the connect", func() bool { return sh.ui.Context == "prod-eu-central" })
	if n := len(f.callsOf("kubeconfig.pendingApprovals")); n != 2 {
		t.Errorf("%d reviews, want one more before the connect", n)
	}
}

func TestRenderJumpDark(t *testing.T)  { renderJump(t, ThemeDark, "command_palette_jump_dark.png") }
func TestRenderJumpLight(t *testing.T) { renderJump(t, ThemeLight, "command_palette_jump_light.png") }

// renderJump: the palette opened by the `:` key over the Pods view, "po kube" typed: Pods in the
// namespaces kube completes, the jump's hint below.
func renderJump(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := jumpShell(t, mode)
	sh.openJump()
	q.until(t, "the palette", func() bool { return sh.palette.visible() && sh.palette.filter.Text == ":" })
	typeInPalette(t, sh, q, "po kube")
	savePNG(t, w, name)
}
