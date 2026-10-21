// Command icon draws Cogpit's app icon into a PNG: the current logo (internal/ui/logo's Current,
// the glowing C helm of D-24) or, with -version, any earlier one (logo.Versions). No third-party
// assets.
//
// The PNG carries its own plate, with transparent corners and the margin macOS puts around an icon
// (100 of 1024 pixels, the plate's radius as `fyne package` would mask it), so `make app` passes
// `--use-raw-icon`: without it fyne package would shrink and mask the icon a second time.
// Regenerate it, or draw an earlier version's picture, with:
//
//	go run ./hack/icon -o packaging/icon.png
//	go run ./hack/icon -version helm -size 512 -o docs/developer/assets/logo/helm.png
//
// With -ico it writes a Windows icon instead (D-111): one image per size, each drawn at that size
// with the narrower margin Windows icons have, for the executables' resources and the setup
// (make dist-windows):
//
//	go run ./hack/icon -ico 16,20,24,32,40,48,64,256 -o .cache/windows/cogpit.ico
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"

	"github.com/guided-traffic/cogpit/internal/ui/logo"
)

const (
	margin    = 100.0 / 1024 // transparent margin around the plate, as macOS icons have
	winMargin = 1.0 / 32     // Windows' icons fill their square: one pixel of 32 (the lead's, D-111)
)

func main() {
	out := flag.String("o", "packaging/icon.png", "output PNG file, or .ico file with -ico")
	version := flag.String("version", logo.Current.Name, "the logo to draw: "+names())
	size := flag.Int("size", 1024, "the PNG's side in pixels")
	ico := flag.String("ico", "", "write a Windows icon with these sizes (comma-separated, 1 to 256) instead of a PNG")
	flag.Parse()
	v, ok := logo.Find(*version)
	if !ok {
		fmt.Fprintf(os.Stderr, "icon: no logo %q; there are %s\n", *version, names())
		os.Exit(2)
	}
	var err error
	if *ico != "" {
		err = writeICO(*out, v, *ico)
	} else {
		err = write(*out, render(v, *size, margin))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "icon:", err)
		os.Exit(1)
	}
}

func names() string {
	n := make([]string, 0, len(logo.Versions))
	for _, v := range logo.Versions {
		n = append(n, v.Name+" ("+v.Date+")")
	}
	return strings.Join(n, ", ")
}

func write(path string, img image.Image) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return png.Encode(f, img)
}

// writeICO draws the logo at each of the comma-separated sizes and writes them as one icon.
func writeICO(path string, v logo.Version, sizes string) error {
	var imgs []*image.NRGBA
	for _, s := range strings.Split(sizes, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 1 || n > 256 {
			return fmt.Errorf("-ico: size %q: 1 to 256 expected", s)
		}
		imgs = append(imgs, render(v, n, winMargin))
	}
	data, err := encodeICO(imgs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644) // #nosec G306 -- a build artefact, as the PNGs are
}

// render draws the plate (the box's 2…98 of its 100 units) into the square inside the margin, a
// fraction of the side.
func render(v logo.Version, size int, margin float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	border := margin * float64(size)
	scale := (float64(size) - 2*border) / (logo.Box - 4) // 96 box units are the plate's side
	off := border - 2*scale
	v.Draw(img, scale, off, off)
	return img
}
