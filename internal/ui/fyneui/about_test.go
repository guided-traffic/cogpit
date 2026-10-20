package fyneui

import (
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
)

// Builds the About tests show: a release, and a development build with changes not committed.
var (
	aboutRelease = buildinfo.Info{Version: "0.7.0", Release: true, Commit: "0691fb32abcd"}
	aboutDev     = buildinfo.Info{Version: buildinfo.Dev, Commit: "0691fb32abcd", Modified: true}
)

// A release shows its version and no commit; a development build says so and names its commit
// when Go recorded one.
func TestAboutVersion(t *testing.T) {
	for _, tt := range []struct {
		info            buildinfo.Info
		version, detail string
	}{
		{aboutRelease, "Version 0.7.0", ""},
		{aboutDev, "Development build", "Commit 0691fb32abcd (modified)"},
		{buildinfo.Info{Version: buildinfo.Dev, Commit: "0691fb32abcd"}, "Development build", "Commit 0691fb32abcd"},
		{buildinfo.Info{Version: buildinfo.Dev}, "Development build", ""},
	} {
		if v, d := aboutVersion(tt.info); v != tt.version || d != tt.detail {
			t.Errorf("%+v: %q, %q; want %q, %q", tt.info, v, d, tt.version, tt.detail)
		}
	}
}

// A build whose Fyne thread check is not its kind's default says so in About (D-103); the
// defaults, a release without the check and a development build with it, add no line.
func TestAboutThreadCheck(t *testing.T) {
	for _, tt := range []struct {
		info   buildinfo.Info
		checks bool
		want   string
	}{
		{aboutRelease, false, ""},
		{aboutRelease, true, "Fyne thread check on"},
		{aboutDev, true, ""},
		{aboutDev, false, "Fyne thread check off"},
	} {
		if got := aboutThreadCheck(tt.info, tt.checks); got != tt.want {
			t.Errorf("release %v, checks %v: %q, want %q", tt.info.Release, tt.checks, got, tt.want)
		}
	}
}

// The dialog: the name, the version, one line of what Cogpit is, the licence and two links that
// open in the browser on a click; no title above the name and none of the toolchain's details.
func TestAboutDialog(t *testing.T) {
	browser := recordBrowser(t)
	sh, w, q := newTestShell(t, newFakeEngine(t), Options{})
	sh.showAboutFor(aboutRelease)
	q.drain()
	texts := dialogTexts(w)
	want := []string{"Cogpit", "Version 0.7.0", aboutTagline, aboutLicence, "Repository", "Licence", "Close"}
	if check := aboutThreadCheck(aboutRelease, buildinfo.FyneThreadChecks); check != "" { // a test binary has the check
		want = slices.Insert(want, 2, check)
	}
	if !slices.Equal(texts, want) {
		t.Errorf("a release's About %q, want %q", texts, want)
	}
	top := w.Canvas().Overlays().Top()
	links := findAll[*widget.Hyperlink](top)
	if len(links) != 2 {
		t.Fatalf("%d links, want 2", len(links))
	}
	for _, l := range links { // a hyperlink opens on a tap over its text, not its padding
		test.TapAt(l, fyne.NewPos(l.Size().Width/2, l.Size().Height/2))
	}
	if got := browser.opened(); !slices.Equal(got, []string{repoURL, licenceURL}) {
		t.Errorf("the links opened %q", got)
	}
	top.Hide()

	sh.showAboutFor(aboutDev)
	q.drain()
	if got := strings.Join(dialogTexts(w), "|"); !strings.HasPrefix(got, "Cogpit|Development build|Commit 0691fb32abcd (modified)|") {
		t.Errorf("a development build's About %q", got)
	}
}

// No main-menu item is labelled exactly "About": Fyne would move it into macOS's application menu
// and take over GLFW's About item, losing it at the next rebuild of the menu bar (menus.go).
func TestNoMenuItemLabelledAbout(t *testing.T) {
	sh, _, _ := newTestShell(t, newFakeEngine(t), Options{})
	var walk func(items []*fyne.MenuItem)
	walk = func(items []*fyne.MenuItem) {
		for _, it := range items {
			if it.Label == "About" {
				t.Errorf("an item labelled %q", it.Label)
			}
			if it.ChildMenu != nil {
				walk(it.ChildMenu.Items)
			}
		}
	}
	for _, m := range sh.mainMenu().Items {
		walk(m.Items)
	}
	for _, goos := range []string{goosMac, "linux", "windows"} {
		walk(sh.helpMenuOn(goos).Items)
	}
}

// About Cogpit's PNGs over the welcome page (AGENTS.md rule 7): a release dark and light, a
// development build dark.
func TestRenderAbout(t *testing.T) {
	for _, tt := range []struct {
		mode ThemeMode
		info buildinfo.Info
		name string
	}{
		{ThemeDark, aboutRelease, "about_dark.png"},
		{ThemeLight, aboutRelease, "about_light.png"},
		{ThemeDark, aboutDev, "about_dev_dark.png"},
	} {
		s := welcomeEngine(t)
		sh, w, q := newEngineShell(t, s, Options{Theme: tt.mode})
		welcomeShown(t, sh, q)
		sh.showAboutFor(tt.info)
		q.drain()
		savePNG(t, w, tt.name)
	}
}
