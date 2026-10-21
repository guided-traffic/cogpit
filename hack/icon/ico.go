package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
)

// encodeICO writes the images as a Windows icon file (ICONDIR, one ICONDIRENTRY each, then the
// images): the 256-pixel one PNG-compressed as Windows Vista introduced, every smaller one as a
// 32-bit DIB with its AND mask, the form every reader of icons takes (the resource compiler,
// NSIS's installer icon, Explorer). Each image is square, 1 to 256 pixels, and its size unique.
func encodeICO(imgs []*image.NRGBA) ([]byte, error) {
	if len(imgs) == 0 {
		return nil, errors.New("an icon needs an image")
	}
	seen := map[int]bool{}
	payloads := make([][]byte, len(imgs))
	for i, img := range imgs {
		b := img.Bounds()
		n := b.Dx()
		switch {
		case n != b.Dy():
			return nil, errors.New("an icon's image is square")
		case n < 1 || n > 256:
			return nil, errors.New("an icon's image is 1 to 256 pixels")
		case seen[n]:
			return nil, errors.New("an icon's sizes are unique")
		}
		seen[n] = true
		if n == 256 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				return nil, err
			}
			payloads[i] = buf.Bytes()
		} else {
			payloads[i] = dib(img)
		}
	}
	var out bytes.Buffer
	le := binary.LittleEndian
	w := func(v any) { _ = binary.Write(&out, le, v) } // a bytes.Buffer does not fail
	w([3]uint16{0, 1, uint16(len(imgs))})              // reserved, type 1 (icon), count
	offset := 6 + 16*len(imgs)
	for i, img := range imgs {
		n := img.Bounds().Dx()
		side := uint8(n % 256) // 0 means 256
		w([4]uint8{side, side, 0, 0})
		w([2]uint16{1, 32})                                    // planes, bits per pixel
		w([2]uint32{uint32(len(payloads[i])), uint32(offset)}) // #nosec G115 -- an icon is far below 4 GiB
		offset += len(payloads[i])
	}
	for _, p := range payloads {
		out.Write(p)
	}
	return out.Bytes(), nil
}

// dib is a 32-bit BGRA bitmap of img, bottom-up, with the AND mask after it (all zero: the alpha
// channel masks), as an icon's ICONIMAGE holds it: a BITMAPINFOHEADER whose height counts both.
func dib(img *image.NRGBA) []byte {
	n := img.Bounds().Dx()
	maskRow := (n + 31) / 32 * 4 // one bit a pixel, rows padded to 32 bits
	var out bytes.Buffer
	le := binary.LittleEndian
	w := func(v any) { _ = binary.Write(&out, le, v) }
	w(uint32(40))                                          // biSize
	w([2]int32{int32(n), int32(2 * n)})                    // #nosec G115 -- at most 256; biWidth, biHeight (colour and mask)
	w([2]uint16{1, 32})                                    // biPlanes, biBitCount
	w([6]uint32{0, uint32(4*n*n + maskRow*n), 0, 0, 0, 0}) // #nosec G115 -- small; BI_RGB, biSizeImage, the rest 0
	for y := n - 1; y >= 0; y-- {
		for x := 0; x < n; x++ {
			c := img.NRGBAAt(img.Bounds().Min.X+x, img.Bounds().Min.Y+y)
			out.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	out.Write(make([]byte, maskRow*n))
	return out.Bytes()
}
