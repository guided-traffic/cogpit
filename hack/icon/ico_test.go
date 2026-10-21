package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"

	"github.com/guided-traffic/cogpit/internal/ui/logo"
)

// TestEncodeICO reads the icon back as Windows reads it: the directory, each entry's size, offset
// and length, a DIB for the small sizes with the colours bottom-up and a PNG for 256.
func TestEncodeICO(t *testing.T) {
	sizes := []int{16, 32, 256}
	var imgs []*image.NRGBA
	for _, n := range sizes {
		imgs = append(imgs, render(logo.Current, n, winMargin))
	}
	data, err := encodeICO(imgs)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if got := [3]uint16{le.Uint16(data), le.Uint16(data[2:]), le.Uint16(data[4:])}; got != [3]uint16{0, 1, 3} {
		t.Fatalf("ICONDIR %v, want reserved 0, type 1, count 3", got)
	}
	for i, n := range sizes {
		e := data[6+16*i:]
		width, height := int(e[0]), int(e[1])
		if width == 0 {
			width, height = 256, 256
		}
		if width != n || height != n {
			t.Errorf("entry %d: %d×%d, want %d", i, width, height, n)
		}
		if planes, bits := le.Uint16(e[4:]), le.Uint16(e[6:]); planes != 1 || bits != 32 {
			t.Errorf("entry %d: %d planes, %d bits", i, planes, bits)
		}
		length, offset := le.Uint32(e[8:]), le.Uint32(e[12:])
		img := data[offset : offset+length]
		if n == 256 {
			got, err := png.Decode(bytes.NewReader(img))
			if err != nil || got.Bounds().Dx() != 256 {
				t.Errorf("entry 256: not a 256-pixel PNG (%v)", err)
			}
			continue
		}
		if size, h := le.Uint32(img), int32(le.Uint32(img[8:])); size != 40 || h != int32(2*n) { // #nosec G115 -- test sizes
			t.Errorf("entry %d: header %d, height %d, want 40 and %d", i, size, h, 2*n)
		}
		mask := (n + 31) / 32 * 4
		if want := 40 + 4*n*n + mask*n; len(img) != want {
			t.Errorf("entry %d: %d bytes, want %d", i, len(img), want)
		}
		// The first row stored is the image's last: its middle pixel, as BGRA.
		c := imgs[i].NRGBAAt(n/2, n-1)
		if got := img[40+4*(n/2) : 40+4*(n/2)+4]; !bytes.Equal(got, []byte{c.B, c.G, c.R, c.A}) {
			t.Errorf("entry %d: bottom row's middle pixel %v, want %v", i, got, c)
		}
	}
}

func TestEncodeICORefuses(t *testing.T) {
	sq := func(n int) *image.NRGBA { return image.NewNRGBA(image.Rect(0, 0, n, n)) }
	for name, imgs := range map[string][]*image.NRGBA{
		"none":       nil,
		"not square": {image.NewNRGBA(image.Rect(0, 0, 16, 8))},
		"too large":  {sq(512)},
		"twice":      {sq(16), sq(16)},
	} {
		if _, err := encodeICO(imgs); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
