package engine

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A cluster's uploaded icon (Lens's General › Upload Icon: .jpeg, .png, .svg). The image the user
// picks is read once, bounded and checked, and a copy is kept in ClusterIconsDir; the GUI only
// ever draws that copy. A PNG or JPEG is decoded under bounds (its header first: at most
// maxIconPixels pixels, so a small file cannot expand into a huge image) and stored again as a PNG
// of at most iconSide pixels a side, so that what the GUI decodes later is small and written by
// Cogpit; an SVG is kept as text after a check that it is one <svg> document without a DOCTYPE,
// scripts, event handlers or references outside itself (Fyne's renderer loads none, but the copy
// holds none either). Uploading reads a file on this computer, so it is the user's own action in
// Cogpit's window: agents and the CLI are refused (a refusal that names no file's content, and
// a read that never happens for them). Since H-125 (2026-10-19) every change of the icon is the
// user's alone (clusteridentity.go).

// ClusterIconsDir is the directory of the uploaded icons under Cogpit's configuration directory.
const ClusterIconsDir = "cluster-icons"

// Bounds of an uploaded icon.
const (
	maxIconUpload  = 1 << 20   // bytes read from the picked file
	maxIconSVG     = 256 << 10 // bytes of an SVG kept
	maxIconPixels  = 16 << 20  // width × height of a PNG or JPEG decoded
	maxIconEdge    = 8192      // pixels a side of a PNG or JPEG decoded
	iconSide       = 256       // pixels a side of the PNG kept
	maxSVGTokens   = 20000
	maxSVGDepth    = 64
	iconFileDigits = 64 // hex digits of the SHA-256 that names a kept icon
)

// The formats of a kept icon, its file's extension.
const (
	iconPNG = "png"
	iconSVG = "svg"
)

// ErrIconGUIOnly refuses an icon upload from anywhere but the user's own action in Cogpit's window.
var ErrIconGUIOnly = errors.New("only the user, in Cogpit's own window, can upload a cluster's icon; agents and the " +
	"CLI are refused (it reads a file on this computer, and a cluster's icon is the user's mark of which cluster it is)")

// validIconFile reports whether name is a kept icon's file name: "<sha256 hex>.png" or ".svg", so
// that a tampered settings file cannot point outside ClusterIconsDir.
func validIconFile(name string) bool {
	base, ext, ok := strings.Cut(name, ".")
	if !ok || (ext != iconPNG && ext != iconSVG) || len(base) != iconFileDigits {
		return false
	}
	for _, r := range base {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// iconPath is a kept icon's path; empty without a directory.
func (c *ClusterSettingsStore) iconPath(name string) string {
	if c.dir == "" || !validIconFile(name) {
		return ""
	}
	return filepath.Join(c.dir, ClusterIconsDir, name)
}

// removeIcon removes a kept icon no context uses any more; failures are left for the next change.
func (c *ClusterSettingsStore) removeIcon(name string) {
	if p := c.iconPath(name); p != "" {
		_ = os.Remove(p)
	}
}

// ReadIconFile reads the image file the user picked and returns it as Cogpit keeps it: a PNG of at
// most iconSide pixels a side, or the checked SVG, and its format ("png", "svg").
func ReadIconFile(path string) (data []byte, format string, err error) {
	raw, err := readIconUpload(path)
	if err != nil {
		return nil, "", err
	}
	return CheckIcon(raw)
}

// readIconUpload reads a picked image file's bytes: a regular file of at most maxIconUpload bytes.
func readIconUpload(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("the icon %s cannot be read", filepath.Base(path))
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("the icon %s is not a file", filepath.Base(path))
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxIconUpload+1))
	if err != nil {
		return nil, fmt.Errorf("the icon %s cannot be read", filepath.Base(path))
	}
	if len(raw) > maxIconUpload {
		return nil, fmt.Errorf("the icon %s is larger than %s", filepath.Base(path), model.FormatBytes(maxIconUpload))
	}
	return raw, nil
}

// CheckIcon checks an image's bytes and returns them as Cogpit keeps them: a PNG or JPEG decoded
// under bounds and stored again as a small PNG, an SVG checked and kept as it is.
func CheckIcon(raw []byte) (data []byte, format string, err error) {
	switch {
	case bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")), bytes.HasPrefix(raw, []byte{0xff, 0xd8, 0xff}):
		data, err := rasterIcon(raw)
		return data, iconPNG, err
	case looksLikeSVG(raw):
		if err := checkSVG(raw); err != nil {
			return nil, "", err
		}
		return raw, iconSVG, nil
	}
	return nil, "", errors.New("the icon is not a PNG, JPEG or SVG image")
}

// rasterIcon decodes a PNG or JPEG, its size checked from its header first, and encodes it again
// as a PNG of at most iconSide pixels a side.
func rasterIcon(raw []byte) ([]byte, error) {
	img, err := decodeRaster(raw)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, shrink(img, iconSide)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// decodeRaster decodes a PNG or JPEG under the upload's bounds, its size checked from its header
// first, so that a small file cannot expand into a huge image.
func decodeRaster(raw []byte) (image.Image, error) {
	decode, decodeConfig := png.Decode, png.DecodeConfig
	if raw[0] == 0xff {
		decode, decodeConfig = jpeg.Decode, jpeg.DecodeConfig
	}
	cfg, err := decodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("the icon is not a readable PNG or JPEG image")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxIconEdge || cfg.Height > maxIconEdge || cfg.Width*cfg.Height > maxIconPixels {
		return nil, fmt.Errorf("the icon is %d×%d pixels; Cogpit takes at most %d pixels a side and %d megapixels", cfg.Width, cfg.Height, maxIconEdge, maxIconPixels>>20)
	}
	img, err := decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("the icon is not a readable PNG or JPEG image")
	}
	return img, nil
}

// shrink scales img down so that neither side exceeds side pixels, averaging the source pixels
// each target pixel covers; a smaller image is only copied.
func shrink(img image.Image, side int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
		return out
	}
	tw, th := side, side
	if w > h {
		th = max(1, h*side/w)
	} else {
		tw = max(1, w*side/h)
	}
	out := image.NewNRGBA(image.Rect(0, 0, tw, th))
	for ty := range th {
		y0, y1 := b.Min.Y+ty*h/th, b.Min.Y+(ty+1)*h/th
		for tx := range tw {
			x0, x1 := b.Min.X+tx*w/tw, b.Min.X+(tx+1)*w/tw
			var r, g, bl, a, n uint64
			for y := y0; y < max(y1, y0+1); y++ {
				for x := x0; x < max(x1, x0+1); x++ {
					c := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
					r, g, bl, a, n = r+uint64(c.R), g+uint64(c.G), bl+uint64(c.B), a+uint64(c.A), n+1
				}
			}
			out.SetNRGBA(tx, ty, color.NRGBA{R: uint8(r / n >> 8), G: uint8(g / n >> 8), B: uint8(bl / n >> 8), A: uint8(a / n >> 8)})
		}
	}
	return out
}

// looksLikeSVG reports whether raw starts, after a byte order mark and white space, with an XML
// declaration, a comment or an <svg element.
func looksLikeSVG(raw []byte) bool {
	s := bytes.TrimLeft(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), " \t\r\n")
	return bytes.HasPrefix(s, []byte("<?xml")) || bytes.HasPrefix(s, []byte("<svg")) || bytes.HasPrefix(s, []byte("<!--"))
}

// svgForbidden are the elements an icon may not hold: scripts, embedded HTML, frames, and
// anything that loads or animates content.
var svgForbidden = map[string]bool{"script": true, "foreignobject": true, "iframe": true, "object": true, "embed": true,
	"audio": true, "video": true, "animate": true, "set": true, "animatemotion": true, "animatetransform": true, "handler": true}

// checkSVG checks that raw is one <svg> document: well-formed XML of bounded size, depth and token
// count, no DOCTYPE or other directive (no entities), no processing instruction but the XML
// declaration, none of svgForbidden's elements, no event handler attribute (on…) and no reference
// to anything but a fragment of itself (href="#…"; url(#…) in styles).
func checkSVG(raw []byte) error {
	if len(raw) > maxIconSVG {
		return fmt.Errorf("an SVG icon has at most %s", model.FormatBytes(maxIconSVG))
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = true
	depth, tokens, root := 0, 0, false
	for {
		tok, err := dec.Token() // checks that elements nest and close
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("the SVG icon is not well-formed XML")
		}
		if tokens++; tokens > maxSVGTokens {
			return errors.New("the SVG icon is too complex")
		}
		switch t := tok.(type) {
		case xml.Directive:
			return errors.New("the SVG icon has a DOCTYPE or another directive; Cogpit takes none")
		case xml.ProcInst:
			if t.Target != "xml" {
				return fmt.Errorf("the SVG icon has a processing instruction <?%s?>", t.Target)
			}
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if depth == 0 {
				if root || name != "svg" {
					return errors.New("the icon is not one <svg> document")
				}
				root = true
			}
			if depth++; depth > maxSVGDepth {
				return errors.New("the SVG icon is nested too deeply")
			}
			if svgForbidden[name] {
				return fmt.Errorf("the SVG icon has a <%s> element; Cogpit takes none", t.Name.Local)
			}
			for _, a := range t.Attr {
				if err := checkSVGAttr(a); err != nil {
					return err
				}
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(t)) > 0 {
				return errors.New("the icon is not one <svg> document")
			}
			if bytes.Contains(bytes.ToLower(t), []byte("url(")) && !onlyFragmentURLs(string(t)) {
				return errors.New("the SVG icon refers to something outside itself")
			}
		}
	}
	if !root {
		return errors.New("the icon is not an <svg> document")
	}
	return nil
}

func checkSVGAttr(a xml.Attr) error {
	name, v := strings.ToLower(a.Name.Local), strings.TrimSpace(a.Value)
	switch {
	case strings.HasPrefix(name, "on"):
		return fmt.Errorf("the SVG icon has an event handler (%s); Cogpit takes none", a.Name.Local)
	case name == "href" || name == "src":
		if v != "" && !strings.HasPrefix(v, "#") {
			return errors.New("the SVG icon refers to something outside itself")
		}
	case strings.Contains(strings.ToLower(v), "url(") && !onlyFragmentURLs(v):
		return errors.New("the SVG icon refers to something outside itself")
	}
	return nil
}

// onlyFragmentURLs reports whether every url(…) in s names a fragment of the document, url(#id).
func onlyFragmentURLs(s string) bool {
	low := strings.ToLower(s)
	for {
		i := strings.Index(low, "url(")
		if i < 0 {
			return true
		}
		rest := strings.TrimLeft(low[i+4:], " \t\r\n'\"")
		if !strings.HasPrefix(rest, "#") {
			return false
		}
		low = rest
	}
}

// SetClusterIcon makes the image at path a context's icon, cut from its middle as the square it
// just fills (SetClusterIconCut with DefaultIconCrop), and returns the context's settings.
func (s *Session) SetClusterIcon(context, path string) (ClusterSettings, error) {
	return s.SetClusterIconCut(context, path, DefaultIconCrop)
}

// ClusterIcon returns a context's uploaded icon as Cogpit keeps it and its format ("png", "svg");
// no data without one. The GUI draws it; agents read the format from cluster_settings_get.
func (s *Session) ClusterIcon(context string) (data []byte, format string, err error) {
	cs, err := s.ClusterSettingsOf(context)
	if err != nil || cs.Icon == "" {
		return nil, "", err
	}
	path := s.opts.ClusterSettings.iconPath(cs.Icon)
	if path == "" {
		return nil, "", nil
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxIconUpload {
		return nil, "", fmt.Errorf("the icon of %s is missing or not Cogpit's", context)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	// The kept copy is checked again before the GUI decodes it: a file someone else put there is
	// no reason to decode a huge image or render a foreign document.
	format = strings.TrimPrefix(filepath.Ext(cs.Icon), ".")
	if format == iconSVG {
		err = checkSVG(data)
	} else if cfg, cerr := png.DecodeConfig(bytes.NewReader(data)); cerr != nil || cfg.Width > iconSide || cfg.Height > iconSide {
		err = errors.New("not a PNG Cogpit wrote")
	}
	if err != nil {
		return nil, "", fmt.Errorf("the icon of %s is not Cogpit's: %w", context, err)
	}
	return data, format, nil
}
