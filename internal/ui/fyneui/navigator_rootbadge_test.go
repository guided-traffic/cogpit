package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The rule (the owner's LS19): as tall as the lines, at most 50 and at least 28; narrower where the
// lines that must stay whole would be cut, in whole points.
func TestRootBadgeSideRule(t *testing.T) {
	for _, c := range []struct {
		name                  string
		linesHigh, room, need float32
		want                  float32
	}{
		{"three lines, room", 55.7, 132, 50.6, 50},
		{"two lines", 36.3, 132, 50.6, 36},
		{"one line", 17.3, 132, 80, 28},
		{"a long name gives the badge less", 55.7, 132, 100.7, 31},
		{"a name too long for any badge", 55.7, 132, 200, 28},
		{"a wider navigator", 55.7, 150.7, 100.7, 50},
	} {
		if got := rootBadgeSide(28, 50, c.linesHigh, c.room, c.need); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// rootBadgeScene is the user's cluster connected at the root, its version known, the navigator w
// points wide; name, when set, is the cluster's display name.
func rootBadgeScene(t *testing.T, mode ThemeMode, name string, user *engine.ClusterUser, w float32) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := userFake(t, user, engine.Connected)
	withSettingsCommand(t, f) // the gear before the dot, as every window has it (D-92)
	f.ui.ClusterVersion = clusterVersionOf("v1.36.5")
	sh, win, q := userScene(t, f, mode)
	if name != "" {
		f.update(func(u *engine.UIState) {
			u.ClusterSettings = map[string]engine.ClusterSettings{userCtx: {Name: name}}
		})
		q.until(t, "the display name", func() bool { return sh.side.name.text == name })
	}
	if w != defaultSidebarWidth {
		sh.setSidebarWidth(w)
	}
	settle(win)
	return sh, win, q, f
}

// shownText is what a textLine draws: its text, cut with an ellipsis where it does not fit.
func shownText(l *textLine) string { return test.WidgetRenderer(l).(*textLineRenderer).obj.Text }

// TestRootBadgeFillsItsRoom (the owner's LS19 of 2026-10-24): the root's badge is 50 points beside
// its three lines and no taller than they make the root, its letters in proportion; with two lines
// it is as tall as they are, with one (no cluster) 28 as before. It gives way, down to 28, before it
// cuts the name or the version further than the 28-point badge did, or a user's line that showed
// whole beside it; a user's line cut beside 28 too (alice@example.com and her groups at the default
// width) does not hold it back.
func TestRootBadgeFillsItsRoom(t *testing.T) {
	sh, w, q, f := rootBadgeScene(t, ThemeDark, "", confirmedUser, defaultSidebarWidth)
	s := sh.side
	th := s.badge.Theme()
	side := func(s *sidebar) float32 { return test.WidgetRenderer(s.badge).(*clusterBadgeRenderer).sideOf() }
	lines := s.rootRow.body.(*fyne.Container).Objects[0].(*fyne.Container).Objects[1]
	if got := side(s); got != th.Size(sizeNavHeadBadge) || s.badge.Size() != fyne.NewSize(got, got) {
		t.Fatalf("beside three lines the badge is %v (%v), want %v", got, s.badge.Size(), th.Size(sizeNavHeadBadge))
	}
	if s.badge.Size().Height > lines.Size().Height {
		t.Errorf("the badge %v is taller than the lines %v", s.badge.Size().Height, lines.Size().Height)
	}
	if got, want := test.WidgetRenderer(s.badge).(*clusterBadgeRenderer).txt.TextSize, initialsSize(th, 50); got != want || got <= th.Size(theme.SizeNameSubHeadingText) {
		t.Errorf("the letters at %v points on 50, want %v", got, want)
	}
	if x := lines.Position().X; x != 50+2*th.Size(theme.SizeNamePadding) {
		t.Errorf("the lines start at %v, two paddings right of the badge", x)
	}
	lookPNG(t, w, "ls19_root_three_lines_dark.png")

	// The version not known yet: two lines, the badge their height.
	f.update(func(u *engine.UIState) { u.ClusterVersion = nil })
	q.until(t, "the version hidden", func() bool { return !s.version.Visible() })
	settle(w)
	if got, high := side(s), lines.MinSize().Height; got < th.Size(sizeNavBadge) || got > high || got >= th.Size(sizeNavHeadBadge) {
		t.Errorf("beside two lines (%v high) the badge is %v", high, got)
	}

	// No cluster: one line and the smallest badge, as before LS19.
	f.update(func(u *engine.UIState) { u.Context, u.Connection = "", engine.Connection{} })
	q.until(t, "no cluster", func() bool { return s.name.text == notConnected })
	settle(w)
	if got := side(s); got != th.Size(sizeNavBadge) {
		t.Errorf("without a cluster the badge is %v, want %v", got, th.Size(sizeNavBadge))
	}

	// The owner's user, whole beside 28 at the default width, stays whole beside a short name.
	dex := &engine.ClusterUser{Context: userCtx, Username: "dex:HFischer", Groups: []string{"admins"}, Source: engine.UserFromAPIServer}
	sh, w, _, _ = rootBadgeScene(t, ThemeDark, "", dex, defaultSidebarWidth)
	if got, shown := side(sh.side), shownText(sh.side.subtitle); got <= th.Size(sizeNavBadge) || got >= th.Size(sizeNavHeadBadge) || shown != dex.Username {
		t.Errorf("prod-eu as dex:HFischer: the badge %v, the user shown as %q", got, shown)
	}
	lookPNG(t, w, "ls19_root_user_whole_dark.png")

	// The owner's cluster: its 14 letters, 101 points in bold, keep their room; at the narrowest
	// navigator it is cut beside 28 as before.
	const long = "awe-d-pinniped"
	for _, c := range []struct {
		width    float32
		png      string
		whole    bool
		min, max float32
	}{
		{defaultSidebarWidth, "ls19_root_long_name_216_dark.png", true, 29, 49},
		{235, "ls19_root_long_name_235_dark.png", true, 50, 50},
		{minSidebarWidth, "ls19_root_long_name_160_dark.png", false, 28, 28},
	} {
		sh, w, _, _ := rootBadgeScene(t, ThemeDark, long, dex, c.width)
		got := side(sh.side)
		if got < c.min || got > c.max {
			t.Errorf("at %v points: the badge is %v, want %v to %v", c.width, got, c.min, c.max)
		}
		if shown := shownText(sh.side.name); (shown == long) != c.whole {
			t.Errorf("at %v points: the name shows as %q (whole wanted: %v)", c.width, shown, c.whole)
		}
		if shown := shownText(sh.side.subtitle); c.whole && shown != dex.Username {
			t.Errorf("at %v points: the user shows as %q", c.width, shown)
		}
		lookPNG(t, w, c.png)
	}
	_, w, _, _ = rootBadgeScene(t, ThemeLight, long, dex, 235)
	lookPNG(t, w, "ls19_root_long_name_235_light.png")
}

// TestHotbarTilesFifty (LS19): the hotbar's tiles and its "+" are 50-point squares in a 76-point
// bar, with the room on either side a 38-point tile had in 64; the letters and the "+" in proportion.
func TestHotbarTilesFifty(t *testing.T) {
	sh, w, q, _, _ := hotbarScene(t, ThemeDark)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)
	th := sh.app.Settings().Theme()
	if got := th.Size(sizeHotbarTile); got != 50 {
		t.Fatalf("the tile's side %v, want 50", got)
	}
	if got := sh.hot.root.Size().Width; got != 76 {
		t.Errorf("the bar is %v wide, want 76", got)
	}
	for i, tl := range tilesOf(sh.hot) {
		r := tileRenderer(tl)
		if r.bg.Size() != fyne.NewSize(50, 50) {
			t.Errorf("tile %d: its square %v", i, r.bg.Size())
		}
		// 10.5 points on either side, as the 38-point tile had in the 64-point bar.
		if left, right := r.bg.Position().X, tl.Size().Width-r.bg.Position().X-50; left != right || left != 10.5 {
			t.Errorf("tile %d: %v room left of its square and %v right", i, left, right)
		}
		if r.initials.Visible() && r.initials.TextSize != initialsSize(th, 50) {
			t.Errorf("tile %d: its letters at %v, want %v", i, r.initials.TextSize, initialsSize(th, 50))
		}
	}
	add := test.WidgetRenderer(sh.hot.add).(*hotbarButtonRenderer)
	if add.bg.Size() != fyne.NewSize(50, 50) || add.txt.TextSize <= th.Size(theme.SizeNameHeadingText) {
		t.Errorf("the +'s square %v, its + at %v", add.bg.Size(), add.txt.TextSize)
	}
}
