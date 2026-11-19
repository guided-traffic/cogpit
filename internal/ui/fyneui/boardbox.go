package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
)

// The alert board's and the silences page's state box (the owner): what the page says instead of
// cards (no Alertmanager found, not allowed, not answering; no alerts at all, none in the
// namespaces chosen, none the search keeps; no silences) stands in one box in the middle of the
// page, its text centred under an optional picture, its buttons (Set Its Address…, Remove Namespace
// Filter, Clear Search) at its bottom right. A board without alerts shows a steaming cup.

// boardBoxWidth is the box's width, and boardBoxIcon the picture's size.
const (
	boardBoxWidth = 520
	boardBoxIcon  = 72
)

// iconCoffee is a steaming cup, drawn for Cogpit as filled shapes (Fyne recolours fills only): the
// board while no alert fires.
var iconCoffee = fyne.NewStaticResource("cogpit-coffee.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
		`<path d="M7.2 9 Q5.8 7.5 7.2 6 Q8.6 4.5 7.2 3 L8.8 3 Q10.2 4.5 8.8 6 Q7.4 7.5 8.8 9 Z"/>`+
		`<path d="M11.2 9 Q9.8 7.5 11.2 6 Q12.6 4.5 11.2 3 L12.8 3 Q14.2 4.5 12.8 6 Q11.4 7.5 12.8 9 Z"/>`+
		`<path d="M3.5 11h13v5.5a4.5 4.5 0 0 1-4.5 4.5h-4a4.5 4.5 0 0 1-4.5-4.5z"/>`+
		`<path d="M16.5 12.5h1.5a3.25 3.25 0 0 1 0 6.5h-1.5v-2h1.5a1.25 1.25 0 0 0 0-2.5h-1.5z"/></svg>`))

// boardBox is the state box: icon (nil for none) over text, each line centred, in tone, then the
// buttons at the bottom right.
func boardBox(icon fyne.Resource, text string, tone fyne.ThemeColorName, buttons ...fyne.CanvasObject) fyne.CanvasObject {
	th := fyne.CurrentApp().Settings().Theme()
	pad := th.Size(theme.SizeNameInnerPadding)
	inner := float32(boardBoxWidth) - 4*pad
	var rows []fyne.CanvasObject
	if icon != nil {
		img := canvas.NewImageFromResource(theme.NewColoredResource(icon, colorMuted))
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(boardBoxIcon, boardBoxIcon))
		rows = append(rows, container.NewCenter(img))
	}
	for _, line := range wrapLines(text, inner, th.Size(theme.SizeNameText), fyne.TextStyle{}) {
		rows = append(rows, container.NewCenter(newTextLine(line, tone, theme.SizeNameText, fyne.TextStyle{})))
	}
	if len(buttons) > 0 { // a paragraph's space between the text and the buttons (the owner)
		rows = append(rows, gapOf(th.Size(theme.SizeNameText)), container.NewBorder(nil, nil, nil, container.NewHBox(buttons...)))
	}
	body := container.New(textWidth{w: inner}, box(layout.NewCustomPaddedVBoxLayout(6), rows...))
	bg := newSurface(colorSidebar)
	bg.radius = sizeChipRadius
	bg.stroke = colorBracket
	return container.NewStack(bg, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
		inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, body)))
}
