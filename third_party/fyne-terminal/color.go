package terminal

import (
	"image/color"
	"log"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	basicColors = []color.Color{
		color.Black,
		&color.RGBA{170, 0, 0, 255},
		&color.RGBA{0, 170, 0, 255},
		&color.RGBA{170, 170, 0, 255},
		&color.RGBA{0, 0, 170, 255},
		&color.RGBA{170, 0, 170, 255},
		&color.RGBA{0, 255, 255, 255},
		&color.RGBA{170, 170, 170, 255},
	}
	brightColors = []color.Color{
		&color.RGBA{85, 85, 85, 255},
		&color.RGBA{255, 85, 85, 255},
		&color.RGBA{85, 255, 85, 255},
		&color.RGBA{255, 255, 85, 255},
		&color.RGBA{85, 85, 255, 255},
		&color.RGBA{255, 85, 255, 255},
		&color.RGBA{85, 255, 255, 255},
		&color.RGBA{255, 255, 255, 255},
	}
	colourBands = []uint8{
		0x00,
		0x5f,
		0x87,
		0xaf,
		0xd7,
		0xff,
	}
)

func (t *Terminal) handleColorEscape(message string) {
	if message == "" || message == "0" {
		t.currentBG = nil
		t.currentFG = nil
		t.bold = false
		t.italic = false
		t.underline = false
		t.strikethrough = false
		t.blinking = false
		t.reverse = false
		return
	}
	if message[0] == '>' || message[0] == '?' {
		if t.debug {
			log.Println("Strange colour mode", message)
		}
		return
	}
	modes := strings.Split(message, ";")
	for i := 0; i < len(modes); i++ {
		mode := modes[i]
		if mode == "" {
			continue
		}

		if (mode == "38" || mode == "48") && i+1 < len(modes) {
			nextMode := modes[i+1]
			if nextMode == "5" && i+2 < len(modes) {
				t.handleColorModeMap(mode, modes[i+2])
				i += 2
			} else if nextMode == "2" && i+4 < len(modes) {
				t.handleColorModeRGB(mode, modes[i+2], modes[i+3], modes[i+4])
				i += 4
			}
		} else {
			t.handleColorMode(mode)
		}
	}
}

func (t *Terminal) handleColorMode(modeStr string) {
	mode, err := strconv.Atoi(modeStr)
	if err != nil {
		fyne.LogError("Failed to parse color mode: "+modeStr, err)
		return
	}
	switch mode {
	case 0:
		t.currentBG, t.currentFG = nil, nil
		t.bold = false
		t.blinking = false
		t.italic = false
		t.underline = false
		t.strikethrough = false
		t.reverse = false
	case 1:
		t.bold = true
	case 3:
		t.italic = true
	case 4:
		t.underline = true
	case 5:
		t.blinking = true
	case 7: // reverse; Cogpit patch: a state, see cellColors
		t.reverse = true
	case 9:
		t.strikethrough = true
	case 22:
		t.bold = false
	case 23:
		t.italic = false
	case 24:
		t.underline = false
	case 27: // reverse off
		t.reverse = false
	case 30, 31, 32, 33, 34, 35, 36, 37:
		t.currentFG = basicColors[mode-30]
	case 39:
		t.currentFG = nil
	case 40, 41, 42, 43, 44, 45, 46, 47:
		t.currentBG = basicColors[mode-40]
	case 49:
		t.currentBG = nil
	case 90, 91, 92, 93, 94, 95, 96, 97:
		t.currentFG = brightColors[mode-90]
	case 100, 101, 102, 103, 104, 105, 106, 107:
		t.currentBG = brightColors[mode-100]
	default:
		if t.debug {
			log.Println("Unsupported graphics mode", mode)
		}
	}
}

// cellColors returns the colours of a character written now: the current ones, swapped while reverse
// video is on, with the default text colour as the background and the default background's stand-in
// as the text where a colour is the default (nil). An erase does not use it: erased cells take the
// current colours unswapped, as in xterm.
//
// Cogpit patch: SGR 7 swapped the current colours each time it came and SGR 27 cleared both, so a
// second SGR 7 swapped them back and an SGR 27 dropped colours set before the SGR 7. zsh highlights a
// paste with SGR 7 until Enter, and with oh-my-zsh sends it again part way, or before every character
// when zsh-syntax-highlighting is loaded: every other character showed without the highlight
// (TestReverseVideo).
func (t *Terminal) cellColors() (fg, bg color.Color) {
	if !t.reverse {
		return t.currentFG, t.currentBG
	}
	fg, bg = t.currentBG, t.currentFG
	if fg == nil {
		fg = theme.Color(theme.ColorNameDisabledButton) // upstream's choice for the reversed text
	}
	if bg == nil {
		bg = theme.Color(theme.ColorNameForeground)
	}
	return fg, bg
}

func (t *Terminal) handleColorModeMap(mode, ids string) {
	var c color.Color
	id, err := strconv.Atoi(ids)
	if err != nil {
		if t.debug {
			log.Println("Invalid color map ID", ids)
		}
		return
	}
	if id <= 7 {
		c = basicColors[id]
	} else if id <= 15 {
		c = brightColors[id-8]
	} else if id <= 231 {
		id -= 16
		b := id % 6
		id = (id - b) / 6
		g := id % 6
		r := (id - g) / 6
		c = &color.RGBA{colourBands[r], colourBands[g], colourBands[b], 255}
	} else if id <= 255 {
		id -= 232
		inc := 256 / 24
		y := id * inc
		c = &color.Gray{uint8(y)}
	} else if t.debug {
		log.Println("Invalid colour map ID", id)
	}

	switch mode {
	case "38":
		t.currentFG = c
	case "48":
		t.currentBG = c
	}
}

func (t *Terminal) handleColorModeRGB(mode, rs, gs, bs string) {
	r, _ := strconv.Atoi(rs)
	g, _ := strconv.Atoi(gs)
	b, _ := strconv.Atoi(bs)
	c := &color.RGBA{uint8(r), uint8(g), uint8(b), 255}

	switch mode {
	case "38":
		t.currentFG = c
	case "48":
		t.currentBG = c
	}
}

// SetANSIColors replaces the sixteen colours of SGR 30-37, 40-47, 90-97 and 100-107 (and of the
// 256-colour palette's first sixteen): the eight normal colours, black to white, then the eight
// bright ones. It is not safe to call while a Terminal exists; call it once at start-up.
//
// Cogpit patch: upstream's colours are fixed. Cogpit sets Solarized's (D-22).
func SetANSIColors(c [16]color.Color) {
	basicColors = append([]color.Color(nil), c[:8]...)
	brightColors = append([]color.Color(nil), c[8:]...)
}
