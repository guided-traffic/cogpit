package fyneui

// The bracket's line, lifted. The first value (a step above the stage) drew the bracket under a
// group's label and the dialogs' edge as a hairline nobody could see: at 1.9:1 it was
// underline noise and not the reference's bracket. This one reaches about 2.8:1 on the dark stage
// and the light page, enough for a graphic that carries no information of its own, and is still
// quieter than text. It is filed over theme.go's value from here, so that the palette's table
// stays as the other tracks left it.
func init() {
	darkPalette.fyne[colorBracket] = hex(0x4a5d7c)
	lightPalette.fyne[colorBracket] = hex(0x8c98ab)
}
