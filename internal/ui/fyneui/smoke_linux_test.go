//go:build linux && cgo

package fyneui

import "testing"

// TestSmokeXChannel: a channel of an X pixel comes out as 8 bits whatever its mask's width: the
// 8-bit channels of a 24-bit TrueColor visual as they are, wider ones cut, narrower ones scaled.
func TestSmokeXChannel(t *testing.T) {
	for _, c := range []struct {
		name  string
		pixel uint64
		mask  uint64
		want  uint8
	}{
		{"red of 0x1e1f22", 0x1e1f22, 0xff0000, 0x1e},
		{"green of 0x1e1f22", 0x1e1f22, 0x00ff00, 0x1f},
		{"blue of 0x1e1f22", 0x1e1f22, 0x0000ff, 0x22},
		{"10-bit red at its top", 0x3ff << 20, 0x3ff << 20, 0xff},
		{"5-bit red at its top", 0xf800, 0xf800, 0xff},
		{"5-bit red at half", 0x8000, 0xf800, 0x83},
		{"no mask", 0xffffff, 0, 0},
	} {
		if got := channel(c.pixel, c.mask); got != c.want {
			t.Errorf("%s: %#02x, want %#02x", c.name, got, c.want)
		}
	}
}
