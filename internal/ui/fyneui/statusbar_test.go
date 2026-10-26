package fyneui

import (
	"context"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// statusSizes lays the status bar out at width and returns the link's and the items' positions,
// the link's width and whether it is shown.
func statusSizes(sh *shell, width float32) (link, left fyne.Position, linkW float32, linkShown bool) {
	s := sh.status
	s.root.Resize(fyne.NewSize(width, s.root.MinSize().Height))
	return s.link.Position(), s.layout.left.Position(), s.link.Size().Width, s.link.Visible()
}

// TestStatusBarOwnerLink: D-23. The status bar shows "Need a Kubernetes Expert? Visit:
// hans-fischer.com" directly left of the MCP item, also before a connection; a click opens
// https://hans-fischer.com in the browser, and nothing opens it before.
func TestStatusBarOwnerLink(t *testing.T) {
	for name, connected := range map[string]bool{"before connecting": false, "connected": true} {
		t.Run(name, func(t *testing.T) {
			f := connectedFake(t, 20)
			if !connected {
				f.ui.Connection = engine.Connection{State: engine.Disconnected}
				f.ui.Context = ""
			}
			browser := recordBrowser(t)
			sh, w, q := newTestShell(t, f, Options{MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
			q.until(t, "the kinds", func() bool { return len(sh.kinds) > 0 || !connected })
			settle(w)
			statusSizes(sh, 2000)

			l, s := sh.status.link, sh.status
			if !l.Visible() || l.text != "Need a Kubernetes Expert? Visit: hans-fischer.com" {
				t.Fatalf("link visible %v, text %q", l.Visible(), l.text)
			}
			// Left to right: the status items, the link, its divider, the MCP item.
			linkEnd, sepX, mcpX := l.Position().X+l.Size().Width, s.layout.sep.Position().X, s.layout.right.Position().X
			if l.Position().X < s.layout.left.Position().X+s.layout.left.Size().Width {
				t.Errorf("the link starts at x=%v, inside the status items", l.Position().X)
			}
			if !(linkEnd <= sepX && sepX < mcpX && mcpX+s.layout.right.Size().Width == 2000-2*fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)) {
				t.Errorf("link ends at %v, divider at %v, MCP item at %v: want the link directly left of the MCP item at the bar's right end", linkEnd, sepX, mcpX)
			}
			if len(browser.opened()) != 0 {
				t.Fatalf("the browser was opened before a click: %v", browser.opened())
			}
			test.Tap(l)
			if got := browser.opened(); len(got) != 1 || got[0] != "https://hans-fischer.com" {
				t.Fatalf("opened %v, want https://hans-fischer.com once", got)
			}
		})
	}
}

// TestStatusBarOwnerLinkIsNoCommand: the link is a GUI link, not an engine command (D-23): no
// command of the registry opens a URL, so no agent can.
func TestStatusBarOwnerLinkIsNoCommand(t *testing.T) {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Commands() {
		text := strings.ToLower(c.ID + " " + c.Label + " " + c.Description)
		if strings.Contains(text, "hans-fischer") || strings.Contains(text, "openurl") {
			t.Errorf("command %q mentions the owner's page: %q", c.ID, text)
		}
	}
}

// TestStatusBarOwnerLinkGivesWayFirst: when the bar is too narrow the link is shortened and then
// hidden, and the status items keep their widths and their place at the left end; the link sits
// between them and the MCP item.
func TestStatusBarOwnerLinkGivesWayFirst(t *testing.T) {
	f := connectedFake(t, 20)
	sh, w, q := newTestShell(t, f, Options{MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "the diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)

	s := sh.status
	th := fyne.CurrentApp().Settings().Theme()
	items := s.layout.left.MinSize().Width + s.layout.right.MinSize().Width
	full := fyne.MeasureText(ownerLinkText, th.Size(sizeStatusText), fyne.TextStyle{}).Width
	gaps := 2*th.Size(theme.SizeNameInnerPadding) + 1
	pad := 2 * th.Size(theme.SizeNameInnerPadding) // the bar's own inset

	// Wide: the whole link, then the items.
	link, left, linkW, shown := statusSizes(sh, items+pad+full+gaps+40)
	if !shown || linkW < full {
		t.Fatalf("wide: link shown %v, %v wide, its text needs %v", shown, linkW, full)
	}
	if left.X != 0 || link.X < s.layout.left.Size().Width {
		t.Errorf("wide: the items start at %v and end at %v, the link at %v", left.X, s.layout.left.Size().Width, link.X)
	}
	itemsW := s.layout.left.Size().Width
	if itemsW != s.layout.left.MinSize().Width {
		t.Errorf("wide: the items are %v wide, want their minimum %v", itemsW, s.layout.left.MinSize().Width)
	}

	// Half the link's room: shortened, the items unharmed.
	link, left, linkW, shown = statusSizes(sh, items+pad+gaps+full/2)
	if !shown || linkW >= full || linkW <= 0 {
		t.Errorf("narrow: link shown %v, %v wide, want shortened below %v", shown, linkW, full)
	}
	if got := s.layout.left.Size().Width; got != itemsW {
		t.Errorf("narrow: the items are %v wide, want %v as when wide", got, itemsW)
	}
	if left.X != 0 || link.X < s.layout.left.Size().Width {
		t.Errorf("narrow: the items start at %v and end at %v, the link at %v", left.X, s.layout.left.Size().Width, link.X)
	}
	if link.X+linkW > s.layout.sep.Position().X {
		t.Errorf("narrow: the link ends at %v, past its divider at %v", link.X+linkW, s.layout.sep.Position().X)
	}
	if got := s.link.Size().Width; fyne.MeasureText(shortened(s), th.Size(sizeStatusText), fyne.TextStyle{}).Width > got {
		t.Errorf("narrow: the link's text overflows its %v", got)
	}

	// No room beyond the items: the link and its divider are hidden, the items keep everything.
	_, left, _, shown = statusSizes(sh, items+pad)
	if shown || s.layout.sep.Visible() {
		t.Errorf("no room: link shown %v, divider shown %v", shown, s.layout.sep.Visible())
	}
	if left.X != 0 || s.layout.left.Size().Width != itemsW {
		t.Errorf("no room: items at x=%v, %v wide; want 0 and %v", left.X, s.layout.left.Size().Width, itemsW)
	}
	if got := s.layout.right.Position().X + s.layout.right.Size().Width; got != items {
		t.Errorf("no room: the right items end at %v, want the bar's inner width %v", got, items)
	}

	// The bar's minimum width is the items' alone: the link never asks for room.
	if got := s.root.MinSize().Width; got != items+pad {
		t.Errorf("minimum width %v, want the items' %v", got, items+pad)
	}

	// Widening the bar brings the link back.
	_, _, _, shown = statusSizes(sh, items+pad+full+gaps+40)
	if !shown {
		t.Error("the link did not come back when the bar widened")
	}
}

// TestSnapshotsOnlyInDiagnostics: the count of table snapshots built since start is a developer's
// diagnostic (the coalescing evidence) that only the Diagnostics tab shows, not the status bar
// (owner, 2026-10-14): connected to a cluster, on the demo cluster and with no cluster. The status
// bar's other items stay, and the report agents read with diagnostics.get keeps the count.
func TestSnapshotsOnlyInDiagnostics(t *testing.T) {
	noCluster := func(t *testing.T) *fakeEngine {
		f := connectedFake(t, 20)
		f.ui.Connection, f.ui.Context = engine.Connection{State: engine.Disconnected}, ""
		f.diag = engine.Diagnostics{Snapshots: 42} // no backend: the count since start alone
		return f
	}
	for _, tc := range []struct {
		name      string
		fake      func(t *testing.T) *fakeEngine
		connected bool
		bar       []string // texts the status bar shows
		tip       string   // in the Diagnostics button's tooltip: the counters the bar showed before (D-133)
		summary   string   // in the Diagnostics tab's first line
	}{
		{"connected", diagnosticsFake, true, []string{"kind-dev", "Diagnostics"}, "41 API requests (list 6, watch 7) · 6 informers", "318 table snapshots"},
		{"demo", func(t *testing.T) *fakeEngine { return connectedFake(t, 20) }, true,
			[]string{"demo", "Diagnostics", "demo 10,870 objects · 1,000 events/s"}, "1,284 API requests (list 12, watch 9) · 6 informers", "5,321 table snapshots"},
		{"no cluster", noCluster, false, []string{"Not connected", "Diagnostics"}, "0 API requests (list 0, watch 0) · 0 informers", "Not connected · 42 table snapshots built"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.fake(t)
			f.handlers["diagnostics.get"] = func(context.Context, engine.Invocation) (any, error) {
				return engine.ReportOf(f.Diagnostics(), tc.connected), nil
			}
			sh, w, q := newTestShell(t, f, Options{MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
			openDiagnostics(f)
			q.until(t, "the status bar's counters and the Diagnostics tab", func() bool {
				d := diagnosticsTabOf(sh)
				return d != nil && d.summary.text != "" && sh.dock.open() && sh.diag == newDiagView(f.Diagnostics())
			})
			settle(w)

			s := sh.status
			for _, l := range findAll[*textLine](s.root) { // shown or hidden
				if strings.Contains(l.text, "snapshot") {
					t.Errorf("the status bar shows %q", l.text)
				}
			}
			shown := visibleTexts(s.root)
			for _, want := range tc.bar {
				if !hasText(shown, want) {
					t.Errorf("the status bar shows %q, want %q among them", shown, want)
				}
			}
			if hasText(shown, "informers") || hasText(shown, "API ") {
				t.Errorf("the status bar still shows the counters: %q", shown)
			}
			if !strings.Contains(s.diag.tip, tc.tip) {
				t.Errorf("the Diagnostics button's tooltip %q, want %q in it", s.diag.tip, tc.tip)
			}

			d := diagnosticsTabOf(sh)
			if !strings.Contains(d.summary.text, tc.summary) {
				t.Errorf("the Diagnostics tab says %q, want %q in it", d.summary.text, tc.summary)
			}
			if d.report.Snapshots != f.Diagnostics().Snapshots {
				t.Errorf("diagnostics.get reports %d snapshots, the engine counted %d", d.report.Snapshots, f.Diagnostics().Snapshots)
			}
			// The whole first line fits the tab in the 1440-point window: the count is not cut off.
			th := fyne.CurrentApp().Settings().Theme()
			if need := fyne.MeasureText(d.summary.text, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width; need > d.summary.Size().Width {
				t.Errorf("the first line needs %v, has %v: %q is shortened", need, d.summary.Size().Width, d.summary.text)
			}
		})
	}
}

// visibleTexts are the texts shown under root: hidden objects and their children left out.
func visibleTexts(root fyne.CanvasObject) []string {
	var out []string
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch x := o.(type) {
		case *textLine:
			out = append(out, x.text)
		case *tipLine:
			out = append(out, x.text)
		case *linkText:
			out = append(out, x.text)
		case *diagButton:
			out = append(out, x.pill.text)
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

// shortened is the text the link shows now (the renderer's ellipsized text).
func shortened(s *statusBar) string {
	th := fyne.CurrentApp().Settings().Theme()
	return ellipsize(s.link.text, s.link.Size().Width, th.Size(sizeStatusText), fyne.TextStyle{})
}
