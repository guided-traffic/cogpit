package fyneui

import (
	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A click on a signal's icon in the Signals column (tablesignals.go, D-150) selects its row, which
// opens the drawer, and asks the drawer for the section that tells more (model.Signal.Section:
// Kyverno's first policy, the container whose image has the worst grade): once the drawer shows the
// object with that section, it scrolls the section's title to the top, once. Where the drawer
// scrolls is the window's, not engine state: it changes what the window shows of the object, not
// which object.

// drawerWant is the section a click asked the drawer for.
type drawerWant struct {
	ref     model.ObjectRef
	section string
}

// openAt asks for an object's section; the drawer scrolls there when it shows the object, at once
// when it does already. An empty section asks for nothing.
func (d *drawer) openAt(ref model.ObjectRef, section string) {
	if section == "" {
		d.want = nil
		return
	}
	d.want = &drawerWant{ref: ref, section: section}
	d.scrollToWanted()
}

// noteTitle keeps a section's title object for scrollToWanted; the first of a title wins.
func (d *drawer) noteTitle(title string, obj fyne.CanvasObject) {
	if _, ok := d.titleObj[title]; !ok {
		d.titleObj[title] = obj
	}
}

// scrollToWanted scrolls the asked-for section's title to the top while the drawer shows its
// object, and forgets the wish; another object's wish waits for it.
func (d *drawer) scrollToWanted() {
	w := d.want
	if w == nil || d.shown == nil || !d.shown.Same(w.ref) || d.last == nil {
		return
	}
	obj := d.titleObj[w.section]
	if obj == nil {
		return // not built yet, or no such section: the next show may have it
	}
	d.want = nil
	drv := fyne.CurrentApp().Driver()
	y := drv.AbsolutePositionForObject(obj).Y - drv.AbsolutePositionForObject(d.scroll.Content).Y
	d.scroll.ScrollToOffset(fyne.NewPos(0, max(y, 0)))
}
