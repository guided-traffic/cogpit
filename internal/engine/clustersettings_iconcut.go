package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// A cluster's icon cut from an image (the owner's VW5 of 2026-10-23, D-119): the user picks an
// image, places it in the tile's square frame, zooms it to a section and moves it by dragging. The
// image is kept as the icon's source (IconSource: a PNG of at most iconSourceSide pixels a side; an
// SVG is drawn into one when it is picked), with where the icon was cut from it (IconCrop), so
// that the dialog opens again where it was left; the icon itself (Icon) is the square cut,
// iconSide pixels a side, which the window draws. The colour tile, with letters the user may set
// (IconLetters, at most MaxIconLetters), is the other kind of icon; the user chooses one
// (IconTile) and either is kept while the other shows. Every change is the user's alone, in
// Cogpit's window (H-125).

// Bounds of the icon's source, its cut and its letters.
const (
	iconSourceSide = 1024 // pixels a side of a kept source
	MaxIconZoom    = 8    // the closest a cut may zoom, times the zoom at which the source covers the square
	MaxIconLetters = 5    // characters on the colour tile (the owner: "vielleicht bis zu 5 Buchstaben")
)

// IconCrop is where a cluster's icon is cut from its source: the zoom, 1 when the source's shorter
// side just fills the square, and the source's point at the square's centre, as fractions of its
// width and height.
type IconCrop struct {
	Zoom float64 `json:"zoom"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// DefaultIconCrop is the cut of a source just picked: the square filled, its centre the source's.
var DefaultIconCrop = IconCrop{Zoom: 1, X: 0.5, Y: 0.5}

// Check says why a cut cannot be kept.
func (c IconCrop) Check() error {
	if !(c.Zoom >= 1 && c.Zoom <= MaxIconZoom) {
		return fmt.Errorf("the icon's zoom %v: from 1 to %d", c.Zoom, MaxIconZoom)
	}
	if !(c.X >= 0 && c.X <= 1 && c.Y >= 0 && c.Y <= 1) {
		return fmt.Errorf("the icon's centre %v, %v: fractions of the image, from 0 to 1", c.X, c.Y)
	}
	return nil
}

// Clamp is the cut as it can be made from a source of w×h pixels: its zoom within its bounds and
// its centre where the square stays inside the source (no empty part).
func (c IconCrop) Clamp(w, h int) IconCrop {
	if w < 1 || h < 1 {
		return DefaultIconCrop
	}
	if !(c.Zoom >= 1) { // NaN included
		c.Zoom = 1
	}
	c.Zoom = min(c.Zoom, MaxIconZoom)
	half := float64(min(w, h)) / c.Zoom / 2 // half the square's side, in source pixels
	clamp := func(v float64, size int) float64 {
		if math.IsNaN(v) {
			v = 0.5
		}
		lo, hi := half/float64(size), 1-half/float64(size)
		return min(max(v, lo), hi)
	}
	c.X, c.Y = clamp(c.X, w), clamp(c.Y, h)
	return c
}

// CutIcon cuts a square of side pixels from src as crop says (clamped to src). The window's
// dialog previews a cut with it, so the preview is the icon kept.
func CutIcon(src image.Image, crop IconCrop, side int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, side, side))
	if w < 1 || h < 1 || side < 1 {
		return out
	}
	crop = crop.Clamp(w, h)
	view := float64(min(w, h)) / crop.Zoom // the square's side in source pixels
	scale := float64(side) / view          // output pixels per source pixel
	left := crop.X*float64(w) - view/2
	top := crop.Y*float64(h) - view/2
	// Source to output: out = (src - min - corner) × scale.
	s2d := f64.Aff3{
		scale, 0, -(float64(b.Min.X) + left) * scale,
		0, scale, -(float64(b.Min.Y) + top) * scale,
	}
	xdraw.CatmullRom.Transform(out, s2d, src, b, draw.Src, nil)
	return out
}

// ReadIconSource reads the image file the user picked as an icon's source: a PNG or JPEG decoded
// under the upload's bounds and made at most iconSourceSide pixels a side, an SVG checked and drawn
// into a PNG of that size. It returns the source and its PNG as Cogpit keeps it. The window's
// dialog shows a picked file with it before anything is kept.
func ReadIconSource(path string) (image.Image, []byte, error) {
	raw, err := readIconUpload(path)
	if err != nil {
		return nil, nil, err
	}
	return iconSource(raw)
}

// iconSource is an image's bytes made a kept source: see ReadIconSource.
func iconSource(raw []byte) (image.Image, []byte, error) {
	var img image.Image
	switch {
	case bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")), bytes.HasPrefix(raw, []byte{0xff, 0xd8, 0xff}):
		decoded, err := decodeRaster(raw)
		if err != nil {
			return nil, nil, err
		}
		img = shrink(decoded, iconSourceSide)
	case looksLikeSVG(raw):
		if err := checkSVG(raw); err != nil {
			return nil, nil, err
		}
		drawn, err := rasterizeIconSVG(raw, iconSourceSide)
		if err != nil {
			return nil, nil, err
		}
		img = drawn
	default:
		return nil, nil, errors.New("the icon is not a PNG, JPEG or SVG image")
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, nil, err
	}
	return img, out.Bytes(), nil
}

// rasterizeIconSVG draws a checked SVG into an image whose longer side is side pixels, keeping its
// aspect. A panic of the renderer fails the image instead of the program.
func rasterizeIconSVG(data []byte, side int) (out image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("the SVG cannot be drawn: %v", r)
		}
	}()
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, errors.New("the SVG cannot be read")
	}
	vw, vh := icon.ViewBox.W, icon.ViewBox.H
	if !(vw > 0 && vh > 0) || math.IsInf(vw, 0) || math.IsInf(vh, 0) {
		return nil, errors.New("the SVG has no usable size")
	}
	w, h := side, side
	switch {
	case vw > vh:
		h = max(1, int(math.Round(float64(side)*vh/vw)))
	case vh > vw:
		w = max(1, int(math.Round(float64(side)*vw/vh)))
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	icon.Draw(rasterx.NewDasher(w, h, rasterx.NewScannerGV(w, h, img, img.Bounds())), 1)
	return img, nil
}

// decodeSource decodes a kept source or an icon kept before sources were: a PNG, or an SVG drawn.
func decodeSource(data []byte, format string) (image.Image, error) {
	if format == iconSVG {
		if err := checkSVG(data); err != nil {
			return nil, err
		}
		return rasterizeIconSVG(data, iconSourceSide)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width > iconSourceSide || cfg.Height > iconSourceSide {
		return nil, errors.New("not a PNG Cogpit wrote")
	}
	return png.Decode(bytes.NewReader(data))
}

// ClusterIconSource returns the image a context's icon is cut from and where it was cut, for the
// window's dialog: the kept source, else an icon kept before sources were (the whole icon, its
// cut the default); no image without either.
func (s *Session) ClusterIconSource(context string) (image.Image, IconCrop, error) {
	cs, err := s.ClusterSettingsOf(context)
	if err != nil {
		return nil, DefaultIconCrop, err
	}
	name, crop := cs.IconSource, DefaultIconCrop
	if name != "" && cs.IconCrop != nil {
		crop = *cs.IconCrop
	}
	if name == "" {
		name = cs.Icon
	}
	if name == "" {
		return nil, DefaultIconCrop, nil
	}
	data, err := s.opts.ClusterSettings.readIconFile(name)
	if err != nil {
		return nil, DefaultIconCrop, fmt.Errorf("the icon of %s: %w", context, err)
	}
	img, err := decodeSource(data, strings.TrimPrefix(filepath.Ext(name), "."))
	if err != nil {
		return nil, DefaultIconCrop, fmt.Errorf("the icon of %s is not Cogpit's: %w", context, err)
	}
	b := img.Bounds()
	return img, crop.Clamp(b.Dx(), b.Dy()), nil
}

// SetClusterIconCut makes a context's icon a square cut from an image: from the file at path, a
// new source, or with path empty from the source kept (an icon kept before sources were serving as
// one). It keeps the source, the cut and the icon, shows the icon instead of the colour tile and
// returns the context's settings. The colour and the letters stay for the tile.
func (s *Session) SetClusterIconCut(context, path string, crop IconCrop) (ClusterSettings, error) {
	c := s.opts.ClusterSettings
	if c == nil {
		return ClusterSettings{}, ErrNoClusterSettings
	}
	if c.dir == "" {
		return ClusterSettings{}, errors.New("this Cogpit has no configuration folder to keep an icon in")
	}
	if err := crop.Check(); err != nil {
		return ClusterSettings{}, err
	}
	var src image.Image
	var srcPNG []byte
	if path != "" {
		img, data, err := ReadIconSource(path)
		if err != nil {
			return ClusterSettings{}, err
		}
		src, srcPNG = img, data
	} else {
		img, _, err := s.ClusterIconSource(context)
		if err != nil {
			return ClusterSettings{}, err
		}
		if img == nil {
			return ClusterSettings{}, errors.New("no image to cut the icon from: pick one first")
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return ClusterSettings{}, err
		}
		src, srcPNG = img, buf.Bytes()
	}
	b := src.Bounds()
	crop = crop.Clamp(b.Dx(), b.Dy())
	var icon bytes.Buffer
	if err := png.Encode(&icon, CutIcon(src, crop, iconSide)); err != nil {
		return ClusterSettings{}, err
	}
	srcName, err := c.keepIconFile(srcPNG, iconPNG)
	if err != nil {
		return ClusterSettings{}, err
	}
	iconName, err := c.keepIconFile(icon.Bytes(), iconPNG)
	if err != nil {
		return ClusterSettings{}, err
	}
	cs, err := s.ChangeClusterSettings(context, func(cs *ClusterSettings) error {
		cs.Icon, cs.IconSource, cs.IconTile = iconName, srcName, false
		cut := crop
		cs.IconCrop = &cut
		return nil
	})
	if err != nil {
		c.mu.Lock()
		for _, name := range []string{srcName, iconName} {
			if !iconInUse(c.by, name) {
				c.removeIcon(name)
			}
		}
		c.mu.Unlock()
	}
	return cs, err
}

// keepIconFile writes an icon's or a source's bytes into ClusterIconsDir under their SHA-256 and
// returns the file's name.
func (c *ClusterSettingsStore) keepIconFile(data []byte, format string) (string, error) {
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:]) + "." + format
	if err := writePrivateFile(c.iconPath(name), data); err != nil {
		return "", fmt.Errorf("keeping the icon: %w", err)
	}
	return name, nil
}

// readIconFile reads a kept icon or source, a regular file of bounded size.
func (c *ClusterSettingsStore) readIconFile(name string) ([]byte, error) {
	path := c.iconPath(name)
	if path == "" {
		return nil, errors.New("no file")
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxIconUpload*4 {
		return nil, errors.New("missing or not Cogpit's")
	}
	return os.ReadFile(filepath.Clean(path)) // a name iconPath checked: 64 hex digits and its extension, in ClusterIconsDir
}

// CheckIconLetters says why letters cannot be a tile's: more than MaxIconLetters characters, or a
// space or a control character among them.
func CheckIconLetters(letters string) error {
	if utf8.RuneCountInString(letters) > MaxIconLetters {
		return fmt.Errorf("the icon's letters %q: at most %d", letters, MaxIconLetters)
	}
	for _, r := range letters {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return fmt.Errorf("the icon's letters %q: letters, digits and signs, no spaces", letters)
		}
	}
	return nil
}
