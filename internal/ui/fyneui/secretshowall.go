package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Show all (H-8, D-59, D-160): above a Secret's keys stands the hint "To show-all secrets at once
// press x.", right-aligned over the keys' Show buttons, muted, its "show-all" a link; while every
// value is shown it reads "hide-all". The link and the key x (revealAll, on a Secret's row or its
// drawer) do what a click on every Show in the data does, with no confirmation: a masked key reads
// its value through secret.reveal from the GUI, one read per key, kept in the drawer's revealed map
// or, on a Secret whose values the user may save, read whole into its editor; an edited value Hide
// masked comes back from the window's memory without a read. hide-all does what a click on every
// Hide does: an unedited value leaves the window's memory, an edited or added one stays masked. A
// value Show cuts stays cut. The reads' answers build the drawer once, when the last arrives, and a
// failed read shows one toast for the batch. No value goes to an agent, the CLI, the log or a
// toast: the reads are a single Show's. The hint is hidden where no key has a Show (binary values
// only, no data).

// The hint's words around its link.
const (
	revealAllBefore = "To"
	revealAllAfter  = "secrets at once press x."
)

// newRevealAllHint is the hint above a Secret's keys and its link, which runs toggle; hidden until
// render finds a key with a Show.
func newRevealAllHint(toggle func()) (*linkText, fyne.CanvasObject) {
	link := newLinkText(showAllWord, toggle)
	link.size = theme.SizeNameCaptionText
	line := box(wordsLayout{size: theme.SizeNameCaptionText},
		newTextLine(revealAllBefore, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}),
		fitLink(link), // its whole word: a link's own minimum lets it shorten
		newTextLine(revealAllAfter, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
	hint := container.NewBorder(nil, nil, nil, line)
	hint.Hide()
	return link, hint
}

// revealAllText is the hint as it reads now: the link's word in its place.
func (sd *secData) revealAllText() string {
	return revealAllBefore + " " + sd.allLink.text + " " + revealAllAfter
}

// wordsLayout sets its objects side by side as the words of one line: at their minimum widths, a
// space of the text size apart, at the line's full height.
type wordsLayout struct{ size fyne.ThemeSizeName }

func (l wordsLayout) gap() float32 {
	return fyne.MeasureText(" ", fyne.CurrentApp().Settings().Theme().Size(l.size), fyne.TextStyle{}).Width
}

func (l wordsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for i, o := range objs {
		m := o.MinSize()
		if i > 0 {
			s.Width += l.gap()
		}
		s.Width += m.Width
		s.Height = max(s.Height, m.Height)
	}
	return s
}

func (l wordsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for i, o := range objs {
		if i > 0 {
			x += l.gap()
		}
		w := o.MinSize().Width
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w
	}
}

// revealEntry is a key with a Show or Hide in the data, as render last drew it: a value in an
// editor (row, masked or not) or one in no editor with its reveal action.
type revealEntry struct {
	row    *secRow
	action model.ItemAction
	shown  bool
}

// anyHidden reports whether a key's value is masked: show-all shows it, else the link is hide-all.
func (sd *secData) anyHidden() bool {
	for _, e := range sd.entries {
		if !e.shown {
			return true
		}
	}
	return false
}

// setAllLook makes the hint's link show-all while a value is masked, hide-all while every value
// is shown, and hides the hint where no key has a Show.
func (sd *secData) setAllLook() {
	if len(sd.entries) == 0 {
		setVisible(sd.allHint, false)
		return
	}
	word := hideAllWord
	if sd.anyHidden() {
		word = showAllWord
	}
	if sd.allLink.text != word {
		sd.allLink.text = word
		sd.allLink.Refresh()
		sd.allHint.Refresh() // the line's width follows the word's
	}
	setVisible(sd.allHint, true)
}

// toggleAll is the hint's link: show-all while a value is masked, else hide-all.
func (sd *secData) toggleAll() {
	if sd.anyHidden() {
		sd.showAll()
		return
	}
	sd.hideAll()
}

// showAll shows every masked value as its Show would: an edited value Hide masked comes back at
// once, the others are read, the drawer built again when the last read answers.
func (sd *secData) showAll() {
	var reads []model.ItemAction
	unmasked := false
	for _, e := range sd.entries {
		switch {
		case e.shown:
		case e.row != nil:
			e.row.masked = false
			unmasked = true
		default:
			reads = append(reads, e.action)
		}
	}
	if unmasked {
		sd.d.rebuild()
	}
	if len(reads) == 0 {
		return
	}
	b := &revealBatch{left: len(reads)}
	for _, a := range reads {
		revealOne(sd.d.sh, sd.ref, a, b)
	}
}

// hideAll masks every shown value as its Hide would, then builds the drawer once.
func (sd *secData) hideAll() {
	changed := false
	for _, e := range sd.entries {
		switch {
		case !e.shown:
			continue
		case e.row != nil:
			sd.mask(e.row)
		default:
			k := revealKey(e.action)
			delete(sd.d.revealed, k)
			delete(sd.d.revealedLen, k)
		}
		changed = true
	}
	if changed {
		sd.d.rebuild()
	}
}

// revealBatch is Show all's reads: the drawer is built again once, when the last answers, and the
// first error is shown once.
type revealBatch struct {
	left int
	kept bool // an answer was kept: the drawer is to be built again
	err  error
}

// answered follows one read's answer for ref: err its error, kept whether its value was kept. A
// nil batch is a single Show: its error shows, a kept value builds the drawer again.
func (b *revealBatch) answered(d *drawer, ref model.ObjectRef, err error, kept bool) {
	if b == nil {
		if err != nil {
			d.sh.showError(err)
		} else if kept {
			d.rebuild()
		}
		return
	}
	if err != nil && b.err == nil {
		b.err = err
	}
	b.kept = b.kept || kept
	if b.left--; b.left > 0 {
		return
	}
	if b.err != nil {
		d.sh.showError(b.err)
	}
	if b.kept && d.shown != nil && *d.shown == ref {
		d.rebuild()
	}
}

// revealAll is the key x on a Secret (its row in a list or its drawer): while the drawer shows
// ref's data it toggles as the hint's link does (show-all while a value is masked, else
// hide-all); before the drawer has built ref's data (the drawer was just asked for ref) it remembers the
// request and shows every value once that data is built, and forgets it when the drawer moves to
// another object or closes. Any object but a Secret is ignored.
func (d *drawer) revealAll(ref model.ObjectRef) {
	if ref.Kind != kindSecrets {
		return
	}
	if sd, ok := d.kept[secDataKept].(*secData); ok && !sd.closed && sd.ref.Same(ref) {
		d.revealAllWant = nil
		sd.toggleAll()
		return
	}
	r := ref
	d.revealAllWant = &r
}

// revealWanted shows every value of the Secret revealAll asked for once the drawer has built its
// data, once.
func (d *drawer) revealWanted() {
	w := d.revealAllWant
	if w == nil || d.shown == nil || !d.shown.Same(*w) {
		return
	}
	sd := d.secretDataOf(*d.shown)
	if sd == nil {
		return // not built yet
	}
	d.revealAllWant = nil
	sd.showAll()
}

// dropRevealWant forgets revealAll's request unless it is for ref, the object the drawer goes to;
// a nil ref (the drawer closes) forgets it always.
func (d *drawer) dropRevealWant(ref *model.ObjectRef) {
	if w := d.revealAllWant; w != nil && (ref == nil || !w.Same(*ref)) {
		d.revealAllWant = nil
	}
}
