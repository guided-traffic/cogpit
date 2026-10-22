package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// placeholderLayout centres a view's placeholder (its line, the login it waits for, its buttons) at
// the width its line asks for, at most the view's less a margin each side: a longer line shortens
// with an ellipsis (textLine.truncate) instead of drawing beyond the view. The owner's AW18 of
// 2026-10-22: the line "Could not connect to <cluster>: <error>" was centred at its full width, so
// it reached over the navigator, which is drawn before the view, and a clipped "Could not" stood at
// the navigator's right edge beside the welcome page.
type placeholderLayout struct {
	text *textLine // the line whose full width the placeholder asks for
}

func (l placeholderLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var ms fyne.Size
	for _, o := range objs {
		ms = ms.Max(o.MinSize())
	}
	return ms
}

func (l placeholderLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
	for _, o := range objs {
		ms := o.MinSize()
		w := max(ms.Width, min(l.natural(), size.Width-2*pad))
		o.Move(fyne.NewPos((size.Width-w)/2, (size.Height-ms.Height)/2))
		o.Resize(fyne.NewSize(w, ms.Height))
	}
}

// natural is the line's full width.
func (l placeholderLayout) natural() float32 {
	if l.text == nil || l.text.text == "" {
		return 0
	}
	th := l.text.Theme()
	return fyne.MeasureText(l.text.text, th.Size(l.text.size), l.text.style).Width + 2
}
