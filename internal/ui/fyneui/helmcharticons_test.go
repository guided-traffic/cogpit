package fyneui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"slices"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The charts' icon URLs in the fixtures: one an SVG, one a PNG, one that fails; cert-manager and
// old-app name none.
const (
	fixtureIconNginx   = "https://icons.example.com/charts/nginx.svg"
	fixtureIconRedis   = "https://icons.example.com/charts/redis.png"
	fixtureIconFailing = "https://icons.example.com/charts/gone.png"
)

// fixtureIconSVG is an icon as an index may name one (Cogpit's drawing, no project's logo).
const fixtureIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">` +
	`<circle cx="12" cy="12" r="11" fill="#2a9d8f"/><path fill="#ffffff" d="M7 7h10v10H7z"/></svg>`

// fixtureIconPNG is a 64×64 PNG icon: a red disc on transparency.
func fixtureIconPNG(t testing.TB) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			if dx, dy := x-32, y-32; dx*dx+dy*dy < 30*30 {
				img.Set(x, y, color.NRGBA{R: 0xd8, G: 0x2c, B: 0x20, A: 0xff})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// iconEngine is the fake engine with the engine's chart icons: what it keeps of each URL.
type iconEngine struct {
	*fakeEngine
	png   []byte
	mu    sync.Mutex
	asked []string
}

func (e *iconEngine) ChartIcon(_ context.Context, iconURL string) ([]byte, string, error) {
	e.mu.Lock()
	e.asked = append(e.asked, iconURL)
	e.mu.Unlock()
	switch iconURL {
	case fixtureIconNginx:
		return []byte(fixtureIconSVG), "svg", nil
	case fixtureIconRedis:
		return e.png, "png", nil
	}
	return nil, "", errors.New("https://icons.example.com/charts/gone.png: 404 Not Found")
}

func (e *iconEngine) askedFor() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.asked)
}

// newIconShell is newTestShell on the icon engine.
func newIconShell(t testing.TB, e *iconEngine, opts Options) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(opts.Theme))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(e, a, w, opts, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.kc.startDir = t.TempDir
	sh.confirmer.armDelay = 0
	e.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	return sh, w, q
}

// iconCells are the Charts view's icon cells by row, the resources they draw.
func iconCells(sh *shell) map[int]fyne.Resource {
	out := map[int]fyne.Resource{}
	for _, c := range sh.view.cells {
		if c.content.kind == cellImage && c.icon != nil && c.icon.Visible() {
			out[c.row] = c.iconRes
		}
	}
	return out
}

// chartRow is the row index of a chart in the Charts view.
func chartRow(sh *shell, name string) int {
	for i, r := range sh.view.snap.Rows {
		if r.Ref.Name == name {
			return i
		}
	}
	return -1
}

// The Charts view's first column draws each chart's icon: the window asks the engine once per
// URL shown, draws the glyph while it loads and where there is none or it failed; the column has
// no title and does not sort. The drawer's header shows the icon left of the title. With Load
// chart icons off the glyph shows everywhere and nothing is asked for; on again, the icons
// fetched before show.
func TestChartIconsInTheWindow(t *testing.T) {
	f, ref := chartDrawerFake(t, "18.2.0")
	f.details[ref.String()].HelmChart.Icon = fixtureIconNginx
	e := &iconEngine{fakeEngine: f, png: fixtureIconPNG(t)}
	prefs := engine.DefaultPreferences()
	f.ui.Preferences = &prefs
	sh, _, q := newIconShell(t, e, Options{Theme: ThemeDark})
	q.until(t, "the icons", func() bool {
		cells := iconCells(sh)
		return sh.view.snap != nil && len(cells) == 7 && cells[chartRow(sh, "bitnami.nginx")] != sh.chartGlyph() &&
			cells[chartRow(sh, "bitnami.redis")] != sh.chartGlyph() && sh.drawer.lead.Visible()
	})
	cells := iconCells(sh)
	for _, name := range []string{"jetstack.cert-manager", "stable.old-app", "prometheus-community.kube-prometheus-stack"} {
		if cells[chartRow(sh, name)] != sh.chartGlyph() {
			t.Errorf("%s draws %v, want the glyph", name, cells[chartRow(sh, name)])
		}
	}
	asked := e.askedFor()
	slices.Sort(asked)
	if want := []string{fixtureIconFailing, fixtureIconNginx, fixtureIconRedis}; !slices.Equal(slices.Compact(asked), want) || len(asked) != 3 {
		t.Errorf("asked for %v", asked)
	}
	if img := findAll[*canvas.Image](sh.drawer.lead); len(img) != 1 || img[0].Resource != sh.charts.res[fixtureIconNginx] {
		t.Errorf("the drawer's icon %v", img)
	}
	before := f.mark()
	sh.view.headerTapped(firstDataColumn) // the icon column
	q.drain()
	if calls := f.callsSince(before); len(calls) != 0 {
		t.Errorf("the icon column sorted: %v", calls)
	}

	f.update(func(u *engine.UIState) {
		p := engine.DefaultPreferences()
		p.LoadChartIcons = false
		u.Preferences = &p
	})
	q.until(t, "the glyphs", func() bool {
		for _, r := range iconCells(sh) {
			if r != sh.chartGlyph() {
				return false
			}
		}
		img := findAll[*canvas.Image](sh.drawer.lead)
		return len(img) == 1 && img[0].Resource == sh.chartGlyph()
	})
	f.update(func(u *engine.UIState) { p := engine.DefaultPreferences(); u.Preferences = &p })
	q.until(t, "the icons again", func() bool { return iconCells(sh)[chartRow(sh, "bitnami.nginx")] != sh.chartGlyph() })
	if n := len(e.askedFor()); n != 3 {
		t.Errorf("asked %d times, want the 3 of before", n)
	}
}

func renderChartIcons(t *testing.T, mode ThemeMode, name string) {
	f, ref := chartDrawerFake(t, "18.2.0")
	f.details[ref.String()].HelmChart.Icon = fixtureIconNginx
	e := &iconEngine{fakeEngine: f, png: fixtureIconPNG(t)}
	sh, w, q := newIconShell(t, e, Options{Theme: mode})
	q.until(t, "the icons and the README", func() bool {
		cells := iconCells(sh)
		return sh.view.snap != nil && len(cells) == 7 && cells[chartRow(sh, "bitnami.redis")] != sh.chartGlyph() &&
			readmeOf(sh) != nil && sh.diag.total > 0 && sh.drawer.lead.Visible()
	})
	savePNG(t, w, name)
}

func TestRenderHelmChartIconsDark(t *testing.T) {
	renderChartIcons(t, ThemeDark, "helm_chart_icons_dark.png")
}

// Preferences › Kubernetes › Load chart icons: on at first; unchecking keeps it off through
// preferences.set, and the check follows a change made elsewhere.
func TestPreferencesLoadChartIcons(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool {
		c, ok := sh.settings.controls["loadChartIcons"].(*widget.Check)
		return ok && c.Checked
	})
	sh.settings.controls["loadChartIcons"].(*widget.Check).SetChecked(false)
	q.until(t, "icons off, kept and shown", func() bool {
		p, _ := s.Preferences()
		return !p.LoadChartIcons && sh.ui.Preferences != nil && !sh.ui.Preferences.LoadChartIcons && !sh.chartIconsOn()
	})
	mustDispatch(t, s, "preferences.set", map[string]any{"loadChartIcons": true})
	q.until(t, "the check on again", func() bool { return sh.settings.controls["loadChartIcons"].(*widget.Check).Checked })
}
