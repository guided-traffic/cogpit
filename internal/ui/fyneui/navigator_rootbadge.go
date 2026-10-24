package fyneui

import (
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// The navigator's root shows the cluster's icon as large as its room allows (the owner's LS19 of
// 2026-10-24: "Das Icon hier in der Leiste ist super klein. Wenn es eng wird, darf das Icon ruhig
// so klein werden, aber wenn so viel Platz ist, wie hier, hätte ich es gerne 50px x 50px"):
//
//   - as tall as the root's lines, at most sizeNavHeadBadge (50): the three lines (name, user,
//     version) are 55 points high, so the badge is 50 there; two lines (the version not known yet)
//     give it their height, one line ("Not connected") the smallest. The root is never taller for it.
//   - no wider than leaves the lines what they showed beside the smallest badge, sizeNavBadge (28,
//     the section's rows' badges and the root's size before LS19): the name and the version are
//     never cut further than there, and the user's line, when it showed whole there, stays whole.
//     The badge gives way first, down to 28. A user's line that was cut beside 28 too (an e-mail
//     address, a kubeconfig's path, with the groups' box) does not hold the badge back: such lines
//     are often wider than the navigator's default room, and would keep the badge small nearly
//     always.
//
// The lead's decision (D-74 option a; decisions.md, D-119): the lines keep what they showed over a
// larger icon. At the default width of 216 the owner's cluster, "awe-d-pinniped" (101 points in
// bold) as "dex:HFischer", has a 31-point badge, and 50 from a width of 235. The navigator's width
// alone (50 at 216 and wider, 28 at 160) was the other option, stable per width but cutting that
// name to "awe-d-pin…" at the default width.

// rootHeadLayout lays the root's badge, its lines and its right-hand column (the gear, the dot and
// the ›) out as the container.NewBorder before it did, the badge sized first (rootBadgeSide): the
// badge left and centred in the height, the lines from two paddings right of it to a padding
// before the column, the column at the right edge.
type rootHeadLayout struct {
	badge *clusterBadge
	text  fyne.CanvasObject // the lines, centred in the root's height by their spacers
	right fyne.CanvasObject
	keep  []*textLine       // never cut further than beside the smallest badge: the name and the version
	sub   fyne.CanvasObject // the user's line, kept whole when it is whole beside the smallest badge
	user  *userLineLayout   // sub's layout: the username and the groups' box
}

// MinSize implements fyne.Layout: with the smallest badge, so that a large one never asks the
// navigator for room.
func (l *rootHeadLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	pad := theme.Padding()
	lo := fyne.CurrentApp().Settings().Theme().Size(sizeNavBadge)
	t, r := l.text.MinSize(), l.rightSize()
	w := lo + 2*pad + t.Width
	if r.Width > 0 {
		w += pad + r.Width
	}
	return fyne.NewSize(w, max(lo, t.Height, r.Height))
}

// Layout implements fyne.Layout.
func (l *rootHeadLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	r := l.rightSize()
	side := l.side(size)
	l.badge.setSide(side)
	l.badge.Resize(fyne.NewSize(side, side))
	l.badge.Move(fyne.NewPos(0, (size.Height-side)/2))
	x := side + 2*pad
	end := size.Width
	if r.Width > 0 {
		end -= r.Width + pad
		l.right.Resize(fyne.NewSize(r.Width, size.Height))
		l.right.Move(fyne.NewPos(size.Width-r.Width, 0))
	}
	l.text.Resize(fyne.NewSize(max(end-x, 0), size.Height))
	l.text.Move(fyne.NewPos(x, 0))
}

// rightSize is the right-hand column's minimum, zero while it is hidden.
func (l *rootHeadLayout) rightSize() fyne.Size {
	if l.right == nil || !l.right.Visible() {
		return fyne.Size{}
	}
	return l.right.MinSize()
}

// side is the badge's side in a root of size: rootBadgeSide with the root's lines and room.
func (l *rootHeadLayout) side(size fyne.Size) float32 {
	th := fyne.CurrentApp().Settings().Theme()
	pad := theme.Padding()
	lo := th.Size(sizeNavBadge)
	room := size.Width - 2*pad // the badge and the lines
	if r := l.rightSize(); r.Width > 0 {
		room -= r.Width + pad
	}
	beside := room - lo // the lines' width beside the smallest badge
	var need float32
	for _, t := range l.keep {
		if t.Visible() && t.text != "" {
			need = max(need, min(t.natural(), beside))
		}
	}
	if l.sub != nil && l.sub.Visible() {
		if w := l.user.natural(); w <= beside {
			need = max(need, w)
		}
	}
	return rootBadgeSide(lo, th.Size(sizeNavHeadBadge), l.text.MinSize().Height, room, need)
}

// natural is the line's width uncut.
func (t *textLine) natural() float32 {
	return fyne.MeasureText(t.text, t.Theme().Size(t.size), t.style).Width
}

// natural is the user's line's width uncut: the username, and the groups' box after its gap.
func (l *userLineLayout) natural() float32 {
	w := l.text.natural()
	if l.box.Visible() {
		w += l.gap() + l.box.MinSize().Width
	}
	return w
}

// rootBadgeSide is the root's badge's side, from lo to hi points: as tall as the lines (linesHigh)
// and no wider than leaves need points of room for the lines whole, in whole points. room is the
// width the badge and the lines share, their gap not counted.
func rootBadgeSide(lo, hi, linesHigh, room, need float32) float32 {
	side := min(hi, max(lo, linesHigh))
	if room-side < need {
		side = max(lo, room-need)
	}
	return float32(math.Floor(float64(side)))
}
