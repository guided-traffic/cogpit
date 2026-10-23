package engine

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// halves is a w×h image, its left half red and its right half blue.
func halves(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.NRGBA{B: 255, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// mostly reports whether the pixel at x, y of img is nearly c.
func mostly(img image.Image, x, y int, c color.NRGBA) bool {
	got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	near := func(a, b uint8) bool { return math.Abs(float64(a)-float64(b)) < 24 }
	return near(got.R, c.R) && near(got.G, c.G) && near(got.B, c.B) && near(got.A, c.A)
}

var (
	red  = color.NRGBA{R: 255, A: 255}
	blue = color.NRGBA{B: 255, A: 255}
)

// The cut is the square the crop names: the middle by default, a corner of the source when its
// centre is moved there, the square never leaving the source, a zoom within its bounds.
func TestCutIcon(t *testing.T) {
	src := halves(200, 100)
	mid := CutIcon(src, DefaultIconCrop, 64)
	if !mostly(mid, 4, 32, red) || !mostly(mid, 60, 32, blue) {
		t.Error("the default cut is not the middle square: red left, blue right")
	}
	left := CutIcon(src, IconCrop{Zoom: 1, X: 0, Y: 0.5}, 64) // moved inward to the left edge
	if !mostly(left, 4, 32, red) || !mostly(left, 60, 32, red) {
		t.Error("a cut at the left edge is not all red")
	}
	zoomed := CutIcon(src, IconCrop{Zoom: 2, X: 0.75, Y: 0.5}, 64)
	if !mostly(zoomed, 4, 4, blue) || !mostly(zoomed, 60, 60, blue) {
		t.Error("a cut zoomed into the right half is not all blue")
	}

	for _, tc := range []struct {
		in, want IconCrop
	}{
		{IconCrop{Zoom: 0.5, X: 0.5, Y: 0.5}, IconCrop{Zoom: 1, X: 0.5, Y: 0.5}},
		{IconCrop{Zoom: math.NaN(), X: math.NaN(), Y: 2}, IconCrop{Zoom: 1, X: 0.5, Y: 0.5}},
		{IconCrop{Zoom: 20, X: 1, Y: 0}, IconCrop{Zoom: MaxIconZoom, X: 1 - 100.0/MaxIconZoom/2/200, Y: 100.0 / MaxIconZoom / 2 / 100}},
		{IconCrop{Zoom: 1, X: 0, Y: 0}, IconCrop{Zoom: 1, X: 0.25, Y: 0.5}},
	} {
		if got := tc.in.Clamp(200, 100); math.Abs(got.Zoom-tc.want.Zoom) > 1e-9 || math.Abs(got.X-tc.want.X) > 1e-9 || math.Abs(got.Y-tc.want.Y) > 1e-9 {
			t.Errorf("Clamp(%+v) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
	if err := (IconCrop{Zoom: 9, X: 0.5, Y: 0.5}).Check(); err == nil {
		t.Error("a zoom of 9 accepted")
	}
	if err := (IconCrop{Zoom: 1, X: -0.1, Y: 0.5}).Check(); err == nil {
		t.Error("a centre outside the image accepted")
	}
}

// A source is kept at most 1,024 pixels a side; an SVG is drawn into one.
func TestIconSource(t *testing.T) {
	var big bytes.Buffer
	if err := png.Encode(&big, halves(2000, 1000)); err != nil {
		t.Fatal(err)
	}
	img, data, err := iconSource(big.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 512 {
		t.Errorf("source %v, want 1024×512", b)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 1024 {
		t.Errorf("kept PNG %+v, %v", cfg, err)
	}

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><rect x="0" y="0" width="100" height="100" fill="#ff0000"/><rect x="100" y="0" width="100" height="100" fill="#0000ff"/></svg>`)
	img, _, err = iconSource(svg)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 512 {
		t.Errorf("SVG drawn %v, want 1024×512", b)
	}
	if cut := CutIcon(img, DefaultIconCrop, 64); !mostly(cut, 4, 32, red) || !mostly(cut, 60, 32, blue) {
		t.Error("the SVG's middle square is not red left, blue right")
	}
	if _, _, err := iconSource([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)); err == nil {
		t.Error("an SVG with a script accepted as a source")
	}
}

// A cut again takes the kept source and keeps the new cut; an icon kept before sources were serves
// as one; a source no context uses any more goes.
func TestClusterIconCutAgain(t *testing.T) {
	cfg := t.TempDir()
	s := New(Options{ClusterSettings: NewClusterSettingsStore(cfg)})
	defer s.Close()
	var buf bytes.Buffer
	if err := png.Encode(&buf, halves(200, 100)); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "halves.png")
	if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetClusterIconCut(devCtx, "", DefaultIconCrop); err == nil {
		t.Error("a cut without an image accepted")
	}
	first, err := s.SetClusterIconCut(devCtx, file, DefaultIconCrop)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.SetClusterIconCut(devCtx, "", IconCrop{Zoom: 1, X: 0, Y: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if again.IconSource != first.IconSource || again.Icon == first.Icon || *again.IconCrop != (IconCrop{Zoom: 1, X: 0.25, Y: 0.5}) {
		t.Errorf("cut again: %+v (first %+v)", again, first)
	}
	if _, err := os.Stat(filepath.Join(cfg, ClusterIconsDir, first.Icon)); !os.IsNotExist(err) {
		t.Errorf("the first cut stays: %v", err)
	}
	src, crop, err := s.ClusterIconSource(devCtx)
	if err != nil || src == nil || crop != *again.IconCrop {
		t.Errorf("source %v, crop %+v, %v", src != nil, crop, err)
	}
	data, _, _ := s.ClusterIcon(devCtx)
	icon, err := png.Decode(bytes.NewReader(data))
	if err != nil || icon.Bounds().Dx() != iconSide || !mostly(icon, iconSide-4, iconSide/2, red) {
		t.Errorf("the icon is not the left square, %d px: %v", iconSide, err)
	}

	// An icon from before sources: it serves as the source.
	if _, err := s.ChangeClusterSettings(devCtx, func(c *ClusterSettings) error {
		c.Icon, c.IconSource, c.IconCrop = again.Icon, "", nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg, ClusterIconsDir, again.IconSource)); !os.IsNotExist(err) {
		t.Errorf("a source no context uses stays: %v", err)
	}
	if src, crop, err := s.ClusterIconSource(devCtx); err != nil || src == nil || crop != DefaultIconCrop {
		t.Errorf("an old icon as the source: %v, %+v, %v", src != nil, crop, err)
	}
	if cs, err := s.SetClusterIconCut(devCtx, "", IconCrop{Zoom: 2, X: 0.5, Y: 0.5}); err != nil || cs.IconSource == "" {
		t.Errorf("a cut from an old icon: %+v, %v", cs, err)
	}
}

// The tile's letters: at most five characters, no spaces or control characters.
func TestCheckIconLetters(t *testing.T) {
	for _, ok := range []string{"", "K", "PROD", "STAGE", "ÄÖÜß1", "k8s-1"} {
		if err := CheckIconLetters(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"TOOLONG", "A B", "A\tB", "A\x00"} {
		if err := CheckIconLetters(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
