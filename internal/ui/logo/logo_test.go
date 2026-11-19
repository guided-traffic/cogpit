package logo

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"regexp"
	"strconv"
	"testing"
)

const assets = "../../../docs/developer/assets/logo/"

func readPNG(t *testing.T, path string) *image.NRGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			img.Set(x, y, src.At(x, y))
		}
	}
	return img
}

// appIcon draws a version as hack/icon does: 1024 pixels, the plate inside a 100-pixel margin.
func appIcon(v Version) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	scale := (1024 - 200.0) / plateSide
	v.Draw(img, scale, 100-plateInset*scale, 100-plateInset*scale)
	return img
}

// differ counts the pixels of got and want, compared where inside reports true, whose channels
// differ by more than tol.
func differ(got, want *image.NRGBA, tol int, inside func(x, y int) bool) (n int, first string) {
	d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
	for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
		for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
			if !inside(x, y) {
				continue
			}
			g, w := got.NRGBAAt(x, y), want.NRGBAAt(x, y)
			if d(g.R, w.R) > tol || d(g.G, w.G) > tol || d(g.B, w.B) > tol || d(g.A, w.A) > tol {
				if n == 0 {
					first = fmt.Sprintf("(%d, %d): %v, want %v", x, y, g, w)
				}
				n++
			}
		}
	}
	return n, first
}

func everywhere(int, int) bool { return true }

// TestEveryVersionDrawsItsPicture: each kept version draws the picture of it in
// docs/developer/assets/logo. The gauge's picture is the MVP's full-bleed square, which fyne
// package masked: inside the plate it is the tile; the others are the app icon as hack/icon draws it.
func TestEveryVersionDrawsItsPicture(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range Versions {
		if seen[v.Name] {
			t.Fatalf("two versions are called %q", v.Name)
		}
		seen[v.Name] = true
		t.Run(v.Name, func(t *testing.T) {
			t.Parallel()
			want := readPNG(t, assets+v.Name+".png")
			if v.Name == "gauge" {
				scale := 1024 / plateSide
				onPlate := func(x, y int) bool {
					return roundRect(float64(x)/scale+plateInset, float64(y)/scale+plateInset, plateInset, plateSide, plateRadius) < -1
				}
				if n, first := differ(v.Tile(1024), want, 1, onPlate); n > 0 {
					t.Errorf("%d pixels differ, the first %s", n, first)
				}
				return
			}
			if n, first := differ(appIcon(v), want, 2, everywhere); n > 0 {
				t.Errorf("%d pixels differ, the first %s", n, first)
			}
		})
	}
	if _, ok := Find(Current.Name); !ok {
		t.Errorf("Current %q is not among the versions", Current.Name)
	}
}

// TestPackagedIconIsCurrent: packaging/icon.png is the current version's picture (make icon).
func TestPackagedIconIsCurrent(t *testing.T) {
	got, want := readPNG(t, "../../../packaging/icon.png"), readPNG(t, assets+Current.Name+".png")
	if n, first := differ(got, want, 0, everywhere); n > 0 {
		t.Errorf("packaging/icon.png is not %s.png, run make icon: %d pixels differ, the first %s", Current.Name, n, first)
	}
}

// labLooks are the C helms designed in the icon lab, by their files' name in
// docs/developer/assets/logo: each is held to the lab's export (<name>.json) and SVG (<name>.svg).
var labLooks = map[string]Look{"helm-glow": HelmGlow}

// TestLabLooksAreTheLabsSettings: each look is the icon lab's export the owner chose.
func TestLabLooksAreTheLabsSettings(t *testing.T) {
	hexOf := func(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }
	for name, l := range labLooks {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(assets + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var lab map[string]any
			if err := json.Unmarshal(b, &lab); err != nil {
				t.Fatal(err)
			}
			for key, got := range map[string]any{
				"cColor": hexOf(l.C), "cWeight": l.CWeight, "handleColor": hexOf(l.Handle), "glow": l.Glow,
				"glowRadius": l.GlowRadius, "core": l.Core, "halo": l.Halo, "accent": hexOf(l.Accent),
				"spokeWeight": l.SpokeWeight, "accentGlow": l.AccentGlow, "outer": l.Outer,
				"plateTop": hexOf(l.PlateTop), "plateBottom": hexOf(l.PlateBottom), "rim": l.Rim,
			} {
				if fmt.Sprint(lab[key]) != fmt.Sprint(got) {
					t.Errorf("%s: %v, the lab's %v", key, got, lab[key])
				}
				delete(lab, key)
			}
			delete(lab, "concept")
			delete(lab, "base")
			for key := range lab {
				t.Errorf("the lab's %s is not in Look", key)
			}
		})
	}
}

// TestLabLooksMatchTheirReferenceSVGs: the lab's SVG of each look has the strokes, blurs and
// colours Look derives from its settings, so the Go drawing and the lab's agree.
func TestLabLooksMatchTheirReferenceSVGs(t *testing.T) {
	r2 := func(out map[string]bool, vs ...float64) map[string]bool {
		for _, v := range vs {
			out[strconv.FormatFloat(math.Round(v*100)/100, 'f', 2, 64)] = true
		}
		return out
	}
	for name, l := range labLooks {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(assets + name + ".svg")
			if err != nil {
				t.Fatal(err)
			}
			svg := string(b)
			set := func(re string) map[string]bool {
				out := map[string]bool{}
				for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(svg, -1) {
					v, _ := strconv.ParseFloat(m[1], 64)
					out[strconv.FormatFloat(v, 'f', 2, 64)] = true
				}
				return out
			}
			st := l.strokes()
			widths := r2(map[string]bool{}, st.outer, st.inner, st.spoke, st.needle, st.handle, l.CWeight)
			blurs := map[string]bool{}
			colours := []color.NRGBA{l.C, l.Handle, l.Accent, l.PlateTop, l.PlateBottom}
			if l.Glow > 0 {
				r2(widths, l.CWeight+2)
				r2(blurs, l.GlowRadius, math.Max(.3, l.GlowRadius*.4))
				colours = append(colours, mix(l.C, white, .18))
			}
			if l.AccentGlow > 0 {
				r2(blurs, l.GlowRadius, math.Max(.3, l.GlowRadius*.4))
			}
			if l.Core > 0 {
				r2(widths, l.CWeight*.34)
				colours = append(colours, mix(l.C, white, .72))
			}
			if l.Rim > 0 {
				r2(widths, 1)
			}
			for what, c := range map[string]struct{ got, want map[string]bool }{
				"stroke widths": {set(`stroke-width="([0-9.]+)"`), widths},
				"blurs":         {set(`stdDeviation="([0-9.]+)"`), blurs},
			} {
				for v := range c.want {
					if !c.got[v] {
						t.Errorf("%s: %s is not in the SVG", what, v)
					}
				}
				for v := range c.got {
					if !c.want[v] {
						t.Errorf("%s: the SVG's %s is not Look's", what, v)
					}
				}
			}
			for _, c := range colours {
				if hex := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B); !regexp.MustCompile(`"` + hex + `"`).MatchString(svg) {
					t.Errorf("the colour %s is not in the SVG", hex)
				}
			}
		})
	}
}

// at is the pixel at a box point of a tile drawn at 4 pixels to a unit.
func at(img *image.NRGBA, x, y float64) color.NRGBA {
	return img.NRGBAAt(int((x-plateInset)*4), int((y-plateInset)*4))
}

func near(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) int { return max(int(x)-int(y), int(y)-int(x)) }
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol && d(a.A, b.A) <= tol
}

// onC is the point at an angle (degrees, screen direction) on the C's centre line, out units further.
func onC(deg, out float64) (float64, float64) {
	a := deg * math.Pi / 180
	return 50 + (cR+out)*math.Cos(a), 50 + (cR+out)*math.Sin(a)
}

// TestLookGeometry: a C helm without light, its shapes where D-24's geometry puts them, in the
// look's inks.
func TestLookGeometry(t *testing.T) {
	l := HelmGlow
	l.Glow, l.Core, l.Halo, l.Rim, l.PlateBottom = 0, 0, 0, 0, l.PlateTop
	img := Version{layers: l.layers}.Tile(4 * int(plateSide))
	plate := l.PlateTop
	for _, c := range []struct {
		what string
		x, y float64
		want color.NRGBA
	}{
		{"outside the plate's corner", 3, 3, color.NRGBA{}},
		{"the plate", 12, 12, plate},
		{"the C at 112.5 degrees", 0, 0, l.C}, // between two spokes
		{"the C at 247.5 degrees", 0, 0, l.C},
		{"inside the C", 56, 33, plate}, // between the C and the inner ring, off every spoke
		{"the inner ring", 50, 38, l.Accent},
		{"a spoke", 50, 30, l.Accent}, // up
		{"a handle", 50, 13, l.Handle},
		{"the gap between the C and a handle", 50, 17.8, plate},
		{"the needle in the opening", 80, 50, l.Accent},
		{"the opening above the needle", 80, 45, plate},
		{"the hub", 50, 50, l.Accent},
		{"no spoke to the right but the needle", 70, 56, plate},
	} {
		switch c.what {
		case "the C at 112.5 degrees":
			c.x, c.y = onC(112.5, 0)
		case "the C at 247.5 degrees":
			c.x, c.y = onC(247.5, 0)
		}
		if got := at(img, c.x, c.y); !near(got, c.want, 3) {
			t.Errorf("%s at (%.1f, %.1f): %v, want %v", c.what, c.x, c.y, got, c.want)
		}
	}
}

// TestGlowLiesOverTheWheel: the C's glow is painted over the wheel (owner), so a spoke near the C
// takes the glow's blue; the C and its core stay on top of the glow.
func TestGlowLiesOverTheWheel(t *testing.T) {
	dark := HelmGlow
	dark.Glow = 0
	lit, unlit := Version{layers: HelmGlow.layers}.Tile(4*int(plateSide)), Version{layers: dark.layers}.Tile(4*int(plateSide))
	spoke := func(img *image.NRGBA) color.NRGBA { return at(img, 50, 27) } // the upper spoke, just inside the C
	if s := spoke(unlit); !near(s, HelmGlow.Accent, 3) {
		t.Fatalf("the spoke without glow is %v, want the accent %v", s, HelmGlow.Accent)
	}
	if s := spoke(lit); int(s.R) > int(HelmGlow.Accent.R)-20 {
		t.Errorf("the spoke under the glow is %v: not bluer than the accent %v, so the glow is not over it", s, HelmGlow.Accent)
	}
	core := mix(HelmGlow.C, white, .72)
	x, y := onC(157.5, 0)
	if got := at(lit, x, y); !near(got, core, 3) {
		t.Errorf("the C's centre line under the glow is %v, want its core %v", got, core)
	}
}

func TestTileFillsTheImageWithThePlate(t *testing.T) {
	img := Current.Tile(40)
	if got := img.NRGBAAt(0, 0); got.A != 0 {
		t.Errorf("the corner is %v, want transparent", got)
	}
	if got := img.NRGBAAt(0, 20); got.A < 0xc0 {
		t.Errorf("the left edge's middle is %v, want the plate", got)
	}
	if got := img.NRGBAAt(20, 20); got.A != 0xff {
		t.Errorf("the centre is %v, want opaque", got)
	}
}

func TestBlurKeepsWhatItSpreads(t *testing.T) {
	v := make([]float64, 41*41)
	v[20*41+20] = 1
	blur(v, 41, 41, 3)
	var sum float64
	for _, x := range v {
		sum += x
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Errorf("the blurred sum is %v, want 1", sum)
	}
	if v[20*41+17] != v[20*41+23] || v[17*41+20] != v[20*41+17] || v[20*41+20] <= v[20*41+21] {
		t.Errorf("the blur is not symmetric round its peak: %v %v %v", v[20*41+17], v[20*41+23], v[17*41+20])
	}
}
