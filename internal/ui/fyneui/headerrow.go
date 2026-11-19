package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// A view's header row: the heading (title, count, the selection's bar) at the left, the tools
// (namespace filter, status filter, search) at the right. A view beside an open drawer can be
// narrower than both at their widths; then the tools give way, the search first, then the status
// filter, then the namespace filter, each down to a width that still reads ("All states ▾"), and
// the heading keeps its room, instead of the tools running over the heading and out of the view's
// left edge (seen after the status filter came).

// headerRowLayout lays out [heading, tools]: the heading at its minimum width at the left, the
// tools right-aligned in what is left, between their shrunk and their full widths.
type headerRowLayout struct{}

func (headerRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) != 2 {
		return fyne.Size{}
	}
	hm, tm := objs[0].MinSize(), objs[1].MinSize()
	return fyne.NewSize(hm.Width+headerGap()+tm.Width, max(hm.Height, tm.Height))
}

func (headerRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) != 2 {
		return
	}
	heading, tools := objs[0], objs[1]
	hm := heading.MinSize()
	full := tools.MinSize().Width
	if t, ok := tools.(*fyne.Container); ok {
		if l, ok := t.Layout.(*shrinkRowLayout); ok {
			full = l.full(t.Objects)
		}
	}
	w := min(full, max(0, size.Width-hm.Width-headerGap()))
	w = max(w, tools.MinSize().Width)
	x := max(0, size.Width-w)
	tools.Move(fyne.NewPos(x, 0))
	tools.Resize(fyne.NewSize(min(w, size.Width), size.Height))
	heading.Move(fyne.NewPos(0, 0))
	heading.Resize(fyne.NewSize(min(hm.Width, max(0, x-headerGap())), size.Height))
}

func headerGap() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
}

// shrinkRowLayout lays its objects out in a row at their full widths (their MinSize), and when the
// row is given less, takes the difference from the last object first, each down to its floor in
// floors (an object without one keeps its width). Its MinSize is the row at the floors.
type shrinkRowLayout struct {
	floors map[fyne.CanvasObject]float32
}

func (l *shrinkRowLayout) gaps(objs []fyne.CanvasObject) float32 {
	n := 0
	for _, o := range objs {
		if o.Visible() {
			n++
		}
	}
	if n < 2 {
		return 0
	}
	return float32(n-1) * fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
}

// floor is how narrow o may become.
func (l *shrinkRowLayout) floor(o fyne.CanvasObject) float32 {
	if f, ok := l.floors[o]; ok {
		return min(f, o.MinSize().Width)
	}
	return o.MinSize().Width
}

// full is the row at its objects' full widths.
func (l *shrinkRowLayout) full(objs []fyne.CanvasObject) float32 {
	w := l.gaps(objs)
	for _, o := range objs {
		if o.Visible() {
			w += o.MinSize().Width
		}
	}
	return w
}

func (l *shrinkRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	w, h := l.gaps(objs), float32(0)
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		w += l.floor(o)
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(w, h)
}

func (l *shrinkRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	widths := make([]float32, len(objs))
	short := l.full(objs) - size.Width
	for i := len(objs) - 1; i >= 0; i-- {
		o := objs[i]
		if !o.Visible() {
			continue
		}
		widths[i] = o.MinSize().Width
		if short > 0 {
			cut := min(short, widths[i]-l.floor(o))
			widths[i] -= cut
			short -= cut
		}
	}
	gap := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
	x := float32(0)
	for i, o := range objs {
		if !o.Visible() {
			continue
		}
		h := o.MinSize().Height
		o.Move(fyne.NewPos(x, (size.Height-h)/2))
		o.Resize(fyne.NewSize(widths[i], h))
		x += widths[i] + gap
	}
}
