package fyneui

import (
	"image"
	"image/color"
	"strconv"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/srwiley/rasterx"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Image grades as shields (D-113; the owner's request of 2026-10-22 and his pick in the Image Rating
// Lab, "Kandidat 10:27:57"): a grade (model.ImageGrade) is a crest, filled top to bottom from a
// lighter to a darker shade of its colour, its letter in the middle, in Docker Scout's colours, the
// same in both themes. S carries a gold rim and a mint letter; N/A is grey, at the same low contrast
// to the list in both themes (1.5:1, its text 4:1): the dark theme's grey is darker than the light
// theme's, not the same grey (the owner: "unaufdringlich", as quiet in dark as in light).
//
// The crest is rasterised with rasterx (Fyne's vector rasteriser, as the charts' lines,
// chartpath.go) at the screen's pixel size, and the letter is a canvas.Text in the theme's bold
// face over it. A crest's image depends only on its grade, its pixel size and its colours, so one
// image serves every badge of that grade and size (shieldImage): the Pods list's cells share them.

// The grades' colours. Docker Scout's scale, the owner's pick, the same in both themes; the letter
// takes the darker ink or white, whichever stands out more on the fill.
const (
	colorGradeS          fyne.ThemeColorName = "cogpit.gradeS"
	colorGradeA          fyne.ThemeColorName = "cogpit.gradeA"
	colorGradeB          fyne.ThemeColorName = "cogpit.gradeB"
	colorGradeC          fyne.ThemeColorName = "cogpit.gradeC"
	colorGradeD          fyne.ThemeColorName = "cogpit.gradeD"
	colorGradeE          fyne.ThemeColorName = "cogpit.gradeE"
	colorGradeNA         fyne.ThemeColorName = "cogpit.gradeNA"     // the N/A crest: 1.5:1 on the list
	colorGradeNAText     fyne.ThemeColorName = "cogpit.gradeNAText" // its "N/A": 4:1 on the list
	colorGradeRim        fyne.ThemeColorName = "cogpit.gradeRim"    // S's gold rim
	colorGradeSInk       fyne.ThemeColorName = "cogpit.gradeSInk"   // S's mint letter
	colorGradeInk        fyne.ThemeColorName = "cogpit.gradeInk"    // a letter on a light fill
	colorGradeInkOnDark  fyne.ThemeColorName = "cogpit.gradeInkOnDark"
	colorSevCritical     fyne.ThemeColorName = "cogpit.sevCritical" // the severity bar's segments, Docker Scout's
	colorSevHigh         fyne.ThemeColorName = "cogpit.sevHigh"
	colorSevMedium       fyne.ThemeColorName = "cogpit.sevMedium"
	colorSevLow          fyne.ThemeColorName = "cogpit.sevLow"
	colorSevUnknown      fyne.ThemeColorName = "cogpit.sevUnknown" // Unknown's hatching, its two stripes
	colorSevUnknownLight fyne.ThemeColorName = "cogpit.sevUnknownLight"
	colorSevInkDark      fyne.ThemeColorName = "cogpit.sevInkDark" // a count on Medium and Low
)

func init() {
	for _, p := range []*palette{&darkPalette, &lightPalette} {
		p.fyne[colorGradeS] = hex(0x1f5f55)
		p.fyne[colorGradeA] = hex(0x3e8e72)
		p.fyne[colorGradeB] = hex(0x8fd3b6)
		p.fyne[colorGradeC] = hex(0xe9ac4b)
		p.fyne[colorGradeD] = hex(0xc8691c)
		p.fyne[colorGradeE] = hex(0xc23b33)
		p.fyne[colorGradeRim] = hex(0xd4b25a)
		p.fyne[colorGradeSInk] = hex(0xa8f0d4)
		p.fyne[colorGradeInk] = hex(0x0b1220)
		p.fyne[colorGradeInkOnDark] = hex(0xffffff)
		p.fyne[colorSevCritical] = hex(0x7f1d1d)
		p.fyne[colorSevHigh] = hex(0xd9403a)
		p.fyne[colorSevMedium] = hex(0xe8a33d)
		p.fyne[colorSevLow] = hex(0xf5d68e)
		p.fyne[colorSevInkDark] = hex(0x1c1204)
	}
	// N/A between the list's background and its text, at 1.5:1 and 4:1 to the background (the lab's
	// rule, computed for each theme's stage).
	darkPalette.fyne[colorGradeNA] = hex(0x2f343d)
	darkPalette.fyne[colorGradeNAText] = hex(0x6e757e)
	lightPalette.fyne[colorGradeNA] = hex(0xced0d4)
	lightPalette.fyne[colorGradeNAText] = hex(0x787d86)
	// Unknown's hatching: the text colour at 28 % and 12 % over the stage.
	darkPalette.fyne[colorSevUnknown] = hex(0x45505c)
	darkPalette.fyne[colorSevUnknownLight] = hex(0x26303c)
	lightPalette.fyne[colorSevUnknown] = hex(0xb9bdc4)
	lightPalette.fyne[colorSevUnknownLight] = hex(0xe0e3e6)
}

// gradeFill is a grade's crest colour; N/A's is the grey of its own.
func gradeFill(g model.ImageGrade) fyne.ThemeColorName {
	switch g {
	case model.GradeS:
		return colorGradeS
	case model.GradeA:
		return colorGradeA
	case model.GradeB:
		return colorGradeB
	case model.GradeC:
		return colorGradeC
	case model.GradeD:
		return colorGradeD
	case model.GradeE:
		return colorGradeE
	}
	return colorGradeNA
}

// gradeInk is a grade's letter colour: S's mint, N/A's grey, else the ink that stands out more on
// the fill (the lab's "Auto": white on E, the dark ink on A to D).
func gradeInk(g model.ImageGrade) fyne.ThemeColorName {
	switch g {
	case model.GradeS:
		return colorGradeSInk
	case model.GradeE:
		return colorGradeInkOnDark
	case model.GradeNA, model.GradeNone:
		return colorGradeNAText
	}
	return colorGradeInk
}

// The crest's geometry in a box of side 100 (the lab's "Wappen" at 86 % width): the top's two
// shallow arcs meet in a point at its middle, the sides run straight down to half its height and
// curve to the point at the bottom.
const (
	crestLeft, crestRight = 11.3, 88.7
	crestTop, crestBottom = 4.0, 96.0
	crestLetterY          = 0.46 // the letter's centre, a share of the side: above the middle, as the point takes room below
	crestLetterSize       = 0.54 // the letter's size, a share of the side
	crestNASize           = 0.56 // "N/A" at this share of a letter's size
	crestShade            = 0.25 // the fill runs from 25 % lighter at the top to 25 % darker at the bottom
	crestRimWidth         = 6.0  // S's rim, in the box's units
)

// crestPath traces the crest at k pixels per unit.
func crestPath(p rasterx.Adder, k float64) {
	p.Start(rasterx.ToFixedP(crestLeft*k, 11*k))
	p.QuadBezier(rasterx.ToFixedP(32.585*k, 13*k), rasterx.ToFixedP(50*k, crestTop*k))
	p.QuadBezier(rasterx.ToFixedP(67.415*k, 13*k), rasterx.ToFixedP(crestRight*k, 11*k))
	p.Line(rasterx.ToFixedP(crestRight*k, 50*k))
	p.CubeBezier(rasterx.ToFixedP(crestRight*k, 71*k), rasterx.ToFixedP(71.285*k, 85*k), rasterx.ToFixedP(50*k, crestBottom*k))
	p.CubeBezier(rasterx.ToFixedP(28.715*k, 85*k), rasterx.ToFixedP(crestLeft*k, 71*k), rasterx.ToFixedP(crestLeft*k, 50*k))
	p.Stop(true)
}

// shieldKey names a crest's image: its grade, its pixel size and its colours.
type shieldKey struct {
	g         model.ImageGrade
	w, h      int
	fill, rim color.NRGBA
}

var shields = struct {
	sync.Mutex
	m map[shieldKey]*image.RGBA
}{m: map[shieldKey]*image.RGBA{}}

// shieldImage returns the crest of a grade at w × h pixels in the theme's colours, drawn once per
// grade, size and colours and shared after (it is never written again).
func shieldImage(th fyne.Theme, g model.ImageGrade, w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rectangle{})
	}
	key := shieldKey{g: g, w: w, h: h, fill: nrgba(themeColor(th, gradeFill(g)))}
	if g == model.GradeS {
		key.rim = nrgba(themeColor(th, colorGradeRim))
	}
	shields.Lock()
	defer shields.Unlock()
	if img, ok := shields.m[key]; ok {
		return img
	}
	img := drawShield(key)
	shields.m[key] = img
	return img
}

// drawShield rasterises a crest: the fill, a gradient for a grade and flat for N/A, then S's rim.
func drawShield(key shieldKey) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, key.w, key.h))
	scan := rasterx.NewScannerGV(key.w, key.h, img, img.Bounds())
	k := float64(min(key.w, key.h)) / 100
	if key.g == model.GradeNA || key.g == model.GradeNone {
		scan.SetColor(key.fill)
	} else {
		top, bottom := shade(key.fill, crestShade), shade(key.fill, -crestShade)
		scan.SetColor(rasterx.ColorFunc(func(_, y int) color.Color {
			return lerpNRGBA(top, bottom, (float64(y)/k-crestTop)/(crestBottom-crestTop))
		}))
	}
	f := rasterx.NewFiller(key.w, key.h, scan)
	crestPath(f, k)
	f.Draw()
	if key.g == model.GradeS {
		scan.Clear()
		light, dark := shade(key.rim, 0.35), shade(key.rim, -0.3)
		scan.SetColor(rasterx.ColorFunc(func(x, y int) color.Color {
			// diagonal across the crest's box, top left to bottom right: light, the gold, dark
			t := ((float64(x)/k-crestLeft)/(crestRight-crestLeft) + (float64(y)/k-crestTop)/(crestBottom-crestTop)) / 2
			if t < 0.5 {
				return lerpNRGBA(light, key.rim, t*2)
			}
			return lerpNRGBA(key.rim, dark, t*2-1)
		}))
		s := rasterx.NewStroker(key.w, key.h, scan)
		s.SetStroke(rasterx.ToFixedP(crestRimWidth*k, 0).X, 4<<6, rasterx.ButtCap, rasterx.ButtCap, rasterx.RoundGap, rasterx.Round)
		crestPath(s, k)
		s.Draw()
	}
	return img
}

func nrgba(c color.Color) color.NRGBA { return color.NRGBAModel.Convert(c).(color.NRGBA) }

// shade mixes a colour towards white (p > 0) or black (p < 0) by |p|.
func shade(c color.NRGBA, p float64) color.NRGBA {
	if p >= 0 {
		return lerpNRGBA(c, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: c.A}, p)
	}
	return lerpNRGBA(c, color.NRGBA{A: c.A}, -p)
}

// lerpNRGBA is the colour t of the way from a to b, t clamped to 0..1.
func lerpNRGBA(a, b color.NRGBA, t float64) color.NRGBA {
	t = max(0, min(1, t))
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: mix(a.A, b.A)}
}

// gradeBadge draws a grade as its crest and letter, side points square; GradeNone draws nothing.
type gradeBadge struct {
	widget.BaseWidget
	grade model.ImageGrade
	side  float32
}

func newGradeBadge(side float32, g model.ImageGrade) *gradeBadge {
	b := &gradeBadge{grade: g, side: side}
	b.ExtendBaseWidget(b)
	return b
}

// setGrade shows another grade.
func (b *gradeBadge) setGrade(g model.ImageGrade) {
	if g != b.grade {
		b.grade = g
		b.Refresh()
	}
}

func (b *gradeBadge) CreateRenderer() fyne.WidgetRenderer {
	r := &gradeBadgeRenderer{b: b, text: canvas.NewText("", color.Transparent)}
	r.text.TextStyle = fyne.TextStyle{Bold: true}
	r.text.Alignment = fyne.TextAlignCenter
	r.raster = canvas.NewRaster(func(w, h int) image.Image {
		if b.grade == model.GradeNone {
			return image.NewRGBA(image.Rectangle{})
		}
		return shieldImage(b.Theme(), b.grade, w, h)
	})
	r.objs = []fyne.CanvasObject{r.raster, r.text}
	r.Refresh()
	return r
}

type gradeBadgeRenderer struct {
	b      *gradeBadge
	raster *canvas.Raster
	text   *canvas.Text
	objs   []fyne.CanvasObject
}

func (r *gradeBadgeRenderer) MinSize() fyne.Size { return fyne.NewSize(r.b.side, r.b.side) }

// Layout puts the crest at the badge's side, centred, and the letter centred on the crest's
// optical middle (crestLetterY).
func (r *gradeBadgeRenderer) Layout(size fyne.Size) {
	side := r.b.side
	x, y := (size.Width-side)/2, (size.Height-side)/2
	r.raster.Move(fyne.NewPos(x, y))
	r.raster.Resize(fyne.NewSize(side, side))
	ts := r.text.MinSize()
	r.text.Move(fyne.NewPos(x+(side-ts.Width)/2, y+side*crestLetterY-ts.Height/2))
	r.text.Resize(ts)
}

func (r *gradeBadgeRenderer) Refresh() {
	b := r.b
	th := b.Theme()
	r.text.Text = b.grade.String()
	r.text.Color = themeColor(th, gradeInk(b.grade))
	r.text.TextSize = b.side * crestLetterSize
	if b.grade == model.GradeNA {
		r.text.TextSize *= crestNASize
	}
	r.Layout(b.Size())
	r.raster.Refresh()
	r.text.Refresh()
}

func (r *gradeBadgeRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *gradeBadgeRenderer) Destroy()                     {}

// severityBar draws an image's counts as five segments, Critical to Unknown, each as wide as its
// share of the vulnerabilities (at least a sliver, and room for its number), its count inside, in
// Docker Scout's colours; Unknown hatched; a severity without vulnerabilities faint (the owner's pick
// in the lab: "Anteilig", Scout's colours).
type severityBar struct {
	widget.BaseWidget
	counts model.VulnerabilityCounts
}

func newSeverityBar(c model.VulnerabilityCounts) *severityBar {
	b := &severityBar{counts: c}
	b.ExtendBaseWidget(b)
	return b
}

// The bar's measures: its height, the gap between segments, a segment's least share and the room
// round its number.
const (
	sevBarHeight   = 22
	sevBarGap      = 3
	sevBarMinShare = 0.07
	sevBarPad      = 6
	sevBarFaint    = 0x47 // about 28 %: a severity without vulnerabilities
	sevHatchStripe = 3    // Unknown's stripes, in points
)

func (b *severityBar) CreateRenderer() fyne.WidgetRenderer {
	r := &severityBarRenderer{b: b}
	for i := range r.segs {
		s := &r.segs[i]
		s.fill = canvas.NewRectangle(color.Transparent)
		s.fill.CornerRadius = 3
		s.text = canvas.NewText("", color.Transparent)
		s.text.TextStyle = fyne.TextStyle{Bold: true}
		r.objs = append(r.objs, s.fill)
		if i == sevUnknown {
			s.hatch = canvas.NewRasterWithPixels(s.stripe)
			r.objs = append(r.objs, s.hatch)
		}
		r.objs = append(r.objs, s.text)
	}
	r.Refresh()
	return r
}

// sevUnknown is the Unknown segment's index.
const sevUnknown = 4

type sevSegment struct {
	fill   *canvas.Rectangle
	hatch  *canvas.Raster // Unknown's stripes; nil for the others
	text   *canvas.Text
	n      int64
	width  float32 // the segment's width in points, for its stripes
	ca, cb color.Color
}

// stripe colours Unknown's segment in diagonal stripes sevHatchStripe points wide.
func (s *sevSegment) stripe(x, y, w, _ int) color.Color {
	scale := float32(1)
	if s.width > 0 {
		scale = float32(w) / s.width
	}
	if int(float32(x+y)/(sevHatchStripe*scale))%2 == 0 {
		return s.ca
	}
	return s.cb
}

type severityBarRenderer struct {
	b    *severityBar
	segs [5]sevSegment
	objs []fyne.CanvasObject
}

func (r *severityBarRenderer) counts() [5]int64 {
	c := r.b.counts
	return [5]int64{c.Critical, c.High, c.Medium, c.Low, c.Unknown}
}

// minWidths are the segments' least widths: their numbers and the room round them.
func (r *severityBarRenderer) minWidths() (out [5]float32) {
	for i, s := range r.segs {
		out[i] = s.text.MinSize().Width + 2*sevBarPad
	}
	return out
}

func (r *severityBarRenderer) MinSize() fyne.Size {
	var w float32
	for _, m := range r.minWidths() {
		w += m
	}
	return fyne.NewSize(w+4*sevBarGap, sevBarHeight)
}

// Layout gives every segment its least width, then shares out the rest by the segments' shares of
// the vulnerabilities (each at least sevBarMinShare).
func (r *severityBarRenderer) Layout(size fyne.Size) {
	n := r.counts()
	var total int64
	for _, v := range n {
		total += v
	}
	var shares [5]float64
	var sum float64
	for i, v := range n {
		share := sevBarMinShare
		if total > 0 {
			share = max(share, float64(v)/float64(total))
		}
		shares[i] = share
		sum += share
	}
	mins := r.minWidths()
	free := size.Width - 4*sevBarGap
	for _, m := range mins {
		free -= m
	}
	free = max(free, 0)
	x := float32(0)
	for i := range r.segs {
		s := &r.segs[i]
		w := mins[i] + free*float32(shares[i]/sum)
		s.width = w
		s.fill.Move(fyne.NewPos(x, 0))
		s.fill.Resize(fyne.NewSize(w, size.Height))
		if s.hatch != nil {
			s.hatch.Move(fyne.NewPos(x, 0))
			s.hatch.Resize(fyne.NewSize(w, size.Height))
		}
		ts := s.text.MinSize()
		s.text.Move(fyne.NewPos(x+(w-ts.Width)/2, (size.Height-ts.Height)/2))
		s.text.Resize(ts)
		x += w + sevBarGap
	}
}

func (r *severityBarRenderer) Refresh() {
	th := r.b.Theme()
	fills := [5]fyne.ThemeColorName{colorSevCritical, colorSevHigh, colorSevMedium, colorSevLow, colorSevUnknown}
	inks := [5]fyne.ThemeColorName{colorGradeInkOnDark, colorGradeInkOnDark, colorSevInkDark, colorSevInkDark, theme.ColorNameForeground}
	size := th.Size(theme.SizeNameCaptionText)
	for i, v := range r.counts() {
		s := &r.segs[i]
		s.n = v
		fill, ink := themeColor(th, fills[i]), themeColor(th, inks[i])
		if v == 0 {
			fill, ink = withAlpha(fill, sevBarFaint), withAlpha(ink, sevBarFaint)
		}
		s.fill.FillColor = fill
		if s.hatch != nil {
			s.fill.FillColor = color.Transparent
			s.ca, s.cb = fill, withAlpha(themeColor(th, colorSevUnknownLight), nrgba(fill).A)
			s.hatch.Refresh()
		}
		s.text.Text = strconv.FormatInt(v, 10)
		s.text.Color = ink
		s.text.TextSize = size
		s.fill.Refresh()
		s.text.Refresh()
	}
	r.Layout(r.b.Size())
}

func (r *severityBarRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *severityBarRenderer) Destroy()                     {}
