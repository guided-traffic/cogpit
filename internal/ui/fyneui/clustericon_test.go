package fyneui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// wideImageFile writes a 192×96 PNG, its left half red and its right half blue, and returns its path.
func wideImageFile(t *testing.T) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 192, 96))
	for y := range 96 {
		for x := range 192 {
			c := color.NRGBA{R: 0xdc, G: 0x32, B: 0x2f, A: 0xff}
			if x >= 96 {
				c = color.NRGBA{R: 0x26, G: 0x8b, B: 0xd2, A: 0xff}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wide.png")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// iconScene opens kind-dev's Cluster Settings on the real engine.
func iconScene(t *testing.T, mode ThemeMode) (*engine.Session, *shell, fyne.Window, *mainQueue) {
	t.Helper()
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "hotbar.addCluster", map[string]any{"context": "kind-dev"})
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	q.until(t, "the hotbar's tile", func() bool { return tileFor(sh, "kind-dev") != nil })
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev"})
	q.until(t, "the General section", func() bool { return sh.settings.visible() && sh.settings.controls["iconKind"] != nil })
	return s, sh, w, q
}

// The Internal icon's dialog: up to five letters (more refused before anything is sent), drawn
// smaller to fit; the colour; Cancel keeps what was; Save keeps the tile with its letters.
func TestIconTileDialog(t *testing.T) {
	s, sh, _, q := iconScene(t, ThemeDark)
	td := sh.settings.openTileDialog("kind-dev")
	if td.letters.PlaceHolder != "KD" || td.big.text != "KD" {
		t.Errorf("placeholder %q, preview %q: want the name's initials", td.letters.PlaceHolder, td.big.text)
	}
	td.letters.SetText("TOOLONG")
	if !td.save.Disabled() || !td.problem.Visible() {
		t.Error("six letters and more: Save enabled, no problem shown")
	}
	td.letters.SetText("STAGE")
	if td.save.Disabled() || td.problem.Visible() || td.big.text != "STAGE" || td.small.text != "STAGE" {
		t.Errorf("five letters: save disabled %v, previews %q and %q", td.save.Disabled(), td.big.text, td.small.text)
	}
	test.Tap(td.swatches[4]) // red
	if !td.swatches[4].selected || td.big.ink != identityColorName("red") {
		t.Error("the red swatch is not marked, or the preview not red")
	}
	td.d.Hide() // Cancel
	q.drain()
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.IconLetters != "" || cs.IconColor != "" {
		t.Errorf("Cancel kept %+v", cs)
	}

	td = sh.settings.openTileDialog("kind-dev")
	td.letters.SetText("STAGE")
	test.Tap(td.swatches[4])
	test.Tap(td.save)
	q.until(t, "the tile kept", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.IconLetters == "STAGE" && cs.IconColor == "red"
	})
	q.until(t, "the hotbar's tile", func() bool { return tileRenderer(tileFor(sh, "kind-dev")).initials.Text == "STAGE" })
	r := tileRenderer(tileFor(sh, "kind-dev"))
	th := sh.app.Settings().Theme()
	if size := r.initials.TextSize; size >= initialsSize(th, th.Size(sizeHotbarTile)) || r.initials.MinSize().Width > th.Size(sizeHotbarTile) {
		t.Errorf("five letters at %v pt, %v wide: not fitted to the tile", size, r.initials.MinSize().Width)
	}
}

// The Custom icon's dialog: a picked image fills the frame; the wheel and a drag move the cut, the
// previews follow; Save keeps it; opened again it shows where it was left; the Internal icon
// chosen keeps the image, and Custom chosen again shows it without a dialog.
func TestIconImageDialog(t *testing.T) {
	s, sh, _, q := iconScene(t, ThemeDark)
	file := wideImageFile(t)
	sh.settings.pickIcon = func(done func(string)) { done(file) }
	sh.settings.controls["iconKind"].(*widget.RadioGroup).SetSelected(iconKindImage)
	q.until(t, "the image in the editor", func() bool {
		id := sh.settings.imageDialog
		return id != nil && id.editor.src != nil
	})
	id := sh.settings.imageDialog
	if id.editor.crop != engine.DefaultIconCrop || id.zoom.Value != 1 {
		t.Errorf("a picked image at %+v, zoom %v: want filled and centred", id.editor.crop, id.zoom.Value)
	}
	id.editor.Scrolled(&fyne.ScrollEvent{PointEvent: fyne.PointEvent{Position: localCentre(id.editor)}, Scrolled: fyne.Delta{DY: 70}})
	if z := id.editor.crop.Zoom; math.Abs(z-math.Pow(1.1, 7)) > 1e-6 || math.Abs(id.zoom.Value-z) > 0.01 {
		t.Errorf("seven notches up: zoom %v, slider %v", z, id.zoom.Value)
	}
	id.editor.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DX: 400}}) // far right: the image's left edge reaches the frame's
	want := id.editor.crop
	if want.X >= 0.5 {
		t.Errorf("a drag to the right moved the cut to %v", want.X)
	}
	q.until(t, "the previews cut", func() bool { return id.big.img != nil && id.small.img != nil })
	test.Tap(id.save)
	q.until(t, "the cut kept", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.Icon != "" && !cs.IconTile && cs.IconCrop != nil && math.Abs(cs.IconCrop.Zoom-want.Zoom) < 1e-9
	})
	q.until(t, "Custom chosen", func() bool { return sh.settings.controls["iconKind"].(*widget.RadioGroup).Selected == iconKindImage })

	// Opened again: where it was left, the kept source.
	sh.settings.pickIcon = func(func(string)) { t.Error("the file panel opened with an image kept") }
	again := sh.settings.openImageDialog("kind-dev")
	q.until(t, "the kept image", func() bool { return again.editor.src != nil })
	if c := again.editor.crop; math.Abs(c.Zoom-want.Zoom) > 1e-9 || math.Abs(c.X-want.X) > 1e-9 || math.Abs(c.Y-want.Y) > 1e-9 {
		t.Errorf("opened again at %+v, left at %+v", c, want)
	}
	again.d.Hide()

	// The Internal icon keeps the image; Custom again shows it at once.
	kind := sh.settings.controls["iconKind"].(*widget.RadioGroup)
	kind.SetSelected(iconKindTile)
	q.until(t, "the tile shown", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.IconTile && cs.Icon != ""
	})
	q.until(t, "the hotbar's initials", func() bool { return tileRenderer(tileFor(sh, "kind-dev")).id.image == nil })
	kind.SetSelected(iconKindImage)
	q.until(t, "the image again", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return !cs.IconTile && cs.Icon != ""
	})
	if sh.settings.imageDialog != again {
		t.Error("Custom with an image kept opened a dialog")
	}

	// Reset: the initials in the accent, no image, no letters.
	test.Tap(sh.settings.controls["iconReset"].(*textButton))
	q.until(t, "reset", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.Icon == "" && cs.IconSource == "" && cs.IconColor == "" && cs.IconLetters == ""
	})
}

// localCentre is the middle of obj in its own coordinates.
func localCentre(obj fyne.CanvasObject) fyne.Position {
	return fyne.NewPos(obj.Size().Width/2, obj.Size().Height/2)
}

// The editor zooms at the pointer: the image's point under it stays there; the image always fills
// the frame.
func TestCropEditorZoomsAtThePointer(t *testing.T) {
	test.NewTempApp(t)
	e := newCropEditor(nil)
	e.Resize(fyne.NewSize(cropAreaWidth, cropAreaHeight))
	src := image.NewNRGBA(image.Rect(0, 0, 400, 200))
	e.setSource(src, engine.DefaultIconCrop)
	at := fyne.NewPos(60, -40) // from the frame's centre
	under := func() (float64, float64) {
		s := e.scale()
		return e.crop.X*e.srcW + float64(at.X)/s, e.crop.Y*e.srcH + float64(at.Y)/s
	}
	x0, y0 := under()
	e.zoomAt(3, at)
	if x1, y1 := under(); math.Abs(x1-x0) > 1e-6 || math.Abs(y1-y0) > 1e-6 {
		t.Errorf("the point under the pointer moved from %v,%v to %v,%v", x0, y0, x1, y1)
	}
	e.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DX: -10000, DY: -10000}})
	half := 200.0 / 3 / 2
	if math.Abs(e.crop.X-(1-half/400)) > 1e-9 || math.Abs(e.crop.Y-(1-half/200)) > 1e-9 {
		t.Errorf("dragged far: %+v, the frame left the image", e.crop)
	}
	e.zoomAt(100, fyne.Position{})
	if e.crop.Zoom != engine.MaxIconZoom {
		t.Errorf("zoom %v, want at most %d", e.crop.Zoom, engine.MaxIconZoom)
	}
}

// Two letters keep the badge's size; five are drawn smaller to fit.
func TestFitTextSize(t *testing.T) {
	test.NewTempApp(t)
	if got := fitTextSize("KD", 13, 24); got != 13 {
		t.Errorf("two letters at %v, want 13", got)
	}
	got := fitTextSize("STAGE", 13, 24)
	if got >= 13 || fyne.MeasureText("STAGE", got, fyne.TextStyle{Bold: true}).Width > 24.5 {
		t.Errorf("five letters at %v", got)
	}
}

func TestRenderIconImageDialogDark(t *testing.T) {
	renderIconImageDialog(t, ThemeDark, "icon_image_dialog_dark.png")
}
func renderIconImageDialog(t *testing.T, mode ThemeMode, name string) {
	_, sh, w, q := iconScene(t, mode)
	file := wideImageFile(t)
	sh.settings.pickIcon = func(done func(string)) { done(file) }
	id := sh.settings.openImageDialog("kind-dev")
	q.until(t, "the image", func() bool { return id.editor.src != nil })
	id.editor.zoomAt(1.5, fyne.NewPos(-40, 0))
	q.until(t, "the previews", func() bool { return id.big.img != nil })
	q.drain()
	savePNG(t, w, name)
}
