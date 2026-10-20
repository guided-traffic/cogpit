package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
)

// The bracket's line, lifted. The first value (a step above the stage) drew the bracket under a
// group's label and the dialogs' edge as a hairline nobody could see: at 1.9:1 it was
// underline noise and not the reference's bracket. This one reaches about 2.8:1 on the dark stage
// and the light page, enough for a graphic that carries no information of its own, and is still
// quieter than text. It is filed over theme.go's value from here, so that the palette's table
// stays as the other tracks left it.
func init() {
	darkPalette.fyne[colorBracket] = hex(0x4a5d7c)
	lightPalette.fyne[colorBracket] = hex(0x8c98ab)
	darkPalette.fyne[colorClear] = alpha(hex(0x000000), 0)
	lightPalette.fyne[colorClear] = alpha(hex(0x000000), 0)
	for _, p := range []*palette{&darkPalette, &lightPalette} {
		p.fyne[colorAgentWash] = alpha(color.NRGBAModel.Convert(p.fyne[colorAgent]).(color.NRGBA), 0x1a) // 10 %
	}
	darkPalette.fyne[colorPlateEdge] = alpha(hex(0xffffff), 0x1c)  // about 11 % white on a plate
	lightPalette.fyne[colorPlateEdge] = alpha(hex(0x0f2a4d), 0x26) // about 15 % deep blue
}

// colorPlateEdge is the hairline round a raised plate (a cluster's row on the welcome page): the
// separator colour is a step above the plate's own and drew no edge there.
const colorPlateEdge fyne.ThemeColorName = "cogpit.plateEdge"

// colorClear is nothing: a surface that is not drawn at present (a quiet row's background), kept a
// theme colour so that a surface always has one. colorAgentWash is the agent's violet at about 10 %,
// behind what waits for the user's answer to the agent.
const (
	colorClear     fyne.ThemeColorName = "cogpit.clear"
	colorAgentWash fyne.ThemeColorName = "cogpit.agentWash"
)
