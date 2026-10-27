package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestSearchToggles: "Aa" and ".*" sit inside the search field, each click sends ui.setFilter with
// the field's text and the flipped switch (a pending debounced send dropped), the switches follow
// the engine's state whoever set it, and a pattern that does not compile shows the error border, a
// red ".*" and the reason in its tooltip.
func TestSearchToggles(t *testing.T) {
	f := connectedFake(t, 20)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	tg := sh.view.toggles
	if sh.view.search.ActionItem == nil || len(findAll[*tipButton](sh.view.search.ActionItem)) != 2 ||
		tg.matchCase.Text != "Aa" || tg.regex.Text != ".*" || tg.matchCase.tip != tipMatchCase || tg.regex.tip != tipRegex {
		t.Fatal("the search field has no Aa and .* switches")
	}
	if tg.matchCase.Importance != widget.LowImportance || tg.regex.Importance != widget.LowImportance || tg.ring.Visible() {
		t.Fatal("the switches start on, or the field in error")
	}

	// Typing, then a click before the debounce fires: one call with the text and the switch.
	sh.searchWait = time.Hour // the click comes before the pause ends, however slow the runner
	test.Type(sh.view.search, "web")
	test.Tap(tg.matchCase)
	if fireTimer(sh.view.searchTimer) {
		t.Error("the typed text is still pending after the click sent it with the switch")
	}
	q.until(t, "ui.setFilter", func() bool { return len(f.callsOf("ui.setFilter")) == 1 })
	p := decodeParams[filterParams](t, f.callsOf("ui.setFilter")[0])
	if p.Filter != "web" || p.MatchCase == nil || !*p.MatchCase || p.Regex != nil {
		t.Fatalf("Aa sent %+v", p)
	}
	q.until(t, "Aa on", func() bool { return tg.matchCase.Importance == widget.HighImportance && sh.view.filterBusy == 0 })
	if n := len(f.callsOf("ui.setFilter")); n != 1 {
		t.Fatalf("%d ui.setFilter calls: the debounced text was sent besides the switch", n)
	}

	test.Tap(tg.regex)
	q.until(t, "the second call", func() bool { return len(f.callsOf("ui.setFilter")) == 2 })
	if p := decodeParams[filterParams](t, f.callsOf("ui.setFilter")[1]); p.Regex == nil || !*p.Regex || p.MatchCase != nil {
		t.Fatalf(".* sent %+v", p)
	}
	q.until(t, ".* on", func() bool { return tg.regex.Importance == widget.HighImportance && f.UIState().FilterOptions.Regex })

	// A pattern half typed: the error shows at once, before the engine has the text.
	test.Type(sh.view.search, "(")
	if !tg.ring.Visible() || tg.regex.Importance != widget.DangerImportance || !strings.Contains(tg.regex.tip, "missing closing )") {
		t.Fatalf("invalid pattern: ring %v, .* %v, tip %q", tg.ring.Visible(), tg.regex.Importance, tg.regex.tip)
	}
	test.Type(sh.view.search, ")")
	if tg.ring.Visible() || tg.regex.Importance != widget.HighImportance || tg.regex.tip != tipRegex {
		t.Fatalf("valid again: ring %v, .* %v, tip %q", tg.ring.Visible(), tg.regex.Importance, tg.regex.tip)
	}
	if !fireTimer(sh.view.searchTimer) {
		t.Fatal("no pause pending after the typing")
	}
	q.until(t, "the text sent", func() bool { return f.UIState().Filter == "web()" })

	// An agent switches regex off: the switches and the field follow.
	go f.update(func(u *engine.UIState) {
		u.Filter, u.FilterOptions = "web(", engine.FilterOptions{MatchCase: true}
	})
	q.until(t, ".* off", func() bool { return tg.regex.Importance == widget.LowImportance && sh.view.search.Text == "web(" })
	if tg.ring.Visible() || tg.matchCase.Importance != widget.HighImportance {
		t.Errorf("without regex %q is terms: ring %v, Aa %v", sh.view.search.Text, tg.ring.Visible(), tg.matchCase.Importance)
	}
}

// renderSearchToggles renders the Pods view with both switches on and a pattern that does not
// compile: "Aa" in the accent colour, ".*" red, the field's border red.
func renderSearchToggles(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 20)
	f.ui.Filter, f.ui.FilterOptions = "^web-(", engine.FilterOptions{MatchCase: true, Regex: true}
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 && sh.view.search.Text == "^web-("
	})
	if !sh.view.toggles.ring.Visible() {
		t.Fatal("no error border")
	}
	settle(w)
	savePNG(t, w, name)
}

func TestRenderSearchTogglesDark(t *testing.T) {
	renderSearchToggles(t, ThemeDark, "search_toggles_dark.png")
}
