package termview

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Follow by scrolling (the owner's alpha walk of 2026-10-22, AW14, verbatim): "Ich vergesse immer
// das Follow-Flag raus zu nehmen wenn ich hoch scrolle. Bekommen wir das automatisiert, wenn der
// Nutzer 5-6 Zeilen nach oben scrollt, wird das Follow-Flag entfernt und wenn er wieder runter
// scrollt, bis zur aktuellen Zeile im Log, wird Follow wieder aktiviert? Ist er auf der aktuellen
// Zeile des Logs und deaktiviert follow über das Flag, bleibt das Log dort einfach stehen (wie
// heute) und wenn er irgendwo im Log ist und Follow aktiviert, springt er zur aktuellen Zeile und
// folgt (wie heute)". The lead's details, open to revision (D-99):
//   - a scroll of the user's is one the list's scroller reports outside the view's own scrolling
//     and layout (logList.quietly): the wheel and the trackpad over the lines or the scroll bar,
//     the bar's drag, a click on its track; the stream's flushes, a jump, a selection's
//     auto-scroll, the rows the scrollback drops, a resize, a theme change are the view's;
//   - Follow goes off when such a scroll up leaves the view followOffRows rows (five, the owner's
//     "5-6 Zeilen") or more above the newest line, rows as the list draws them (a wrapped line's
//     rows count each);
//   - a scroll up by less keeps following, that far behind the newest line: the view still moves
//     with every new line, so that a stream of hundreds of lines a second does not snap the view
//     back under a wheel whose notch is half a row, and Follow ticked always means a moving view;
//   - "at the newest line" is the last row at least half in view (half a row below the view's end
//     or less); a scroll down that ends there turns Follow on and the view goes to the very end;
//   - new lines while not following never turn it on, however close they leave the end: only the
//     user's scroll down does; the checkbox as before both ways, ticked it goes to the newest line;
//   - the log view has no scrolling keys of its own (its keys go on to the window, D-99), so none
//     switches Follow.

// followOffRows is how many rows above the newest line a scroll up of the user's switches Follow
// off.
const followOffRows = 5

// userScrolled follows a scroll of the user's.
func (l *Logs) userScrolled(dy float32) {
	below, stride := l.list.below(), l.stride()
	atEnd := below <= stride/2
	switch {
	case l.follow && dy < 0 && below >= followOffRows*stride:
		l.SetFollow(false) // the view stays where the user scrolled
	case l.follow && atEnd:
		l.under = 0
	case l.follow:
		l.under = below // following that far behind the newest line
	case dy > 0 && atEnd:
		l.SetFollow(true) // to the very end, following
	}
}

// toEnd puts a followed view at the newest line, or as far behind it as the user scrolled.
func (l *Logs) toEnd() {
	l.list.ScrollToBottom()
	if l.under > 0 {
		l.list.ScrollToOffset(l.list.GetScrollOffset() - l.under)
	}
}

// CreateRenderer implements fyne.Widget: the list's, with its scroller's offset changes followed.
func (l *logList) CreateRenderer() fyne.WidgetRenderer {
	r := l.List.CreateRenderer()
	if objs := r.Objects(); len(objs) > 0 {
		if sc, ok := objs[0].(*container.Scroll); ok {
			l.scroll, l.lastY = sc, sc.Offset.Y
			inner := sc.OnScrolled // the list's own: it lays the visible rows out
			sc.OnScrolled = func(p fyne.Position) {
				if inner != nil {
					inner(p)
				}
				l.scrolled(p.Y)
			}
		}
	}
	return r
}

// scrolled is told of every change of the scroller's offset: one outside the view's own scrolling
// is the user's.
func (l *logList) scrolled(y float32) {
	dy := y - l.lastY
	l.lastY = y
	if l.quiet == 0 && dy != 0 && l.onUserScroll != nil {
		l.onUserScroll(dy)
	}
}

// quietly runs the view's own scrolling or layout: the offset changes it makes are not the user's.
func (l *logList) quietly(fn func()) {
	l.quiet++
	defer func() {
		l.quiet--
		if l.scroll != nil {
			l.lastY = l.scroll.Offset.Y
		}
	}()
	fn()
}

// below is how far the view is above the end of the rows.
func (l *logList) below() float32 {
	if l.scroll == nil {
		return 0
	}
	return max(l.scroll.Content.MinSize().Height-l.scroll.Size().Height-l.scroll.Offset.Y, 0)
}

// The view's own scrolling and layout, quietly.

func (l *logList) Refresh()                      { l.quietly(l.List.Refresh) }
func (l *logList) Resize(s fyne.Size)            { l.quietly(func() { l.List.Resize(s) }) }
func (l *logList) ScrollToBottom()               { l.quietly(l.List.ScrollToBottom) }
func (l *logList) ScrollToOffset(o float32)      { l.quietly(func() { l.List.ScrollToOffset(o) }) }
func (l *logList) ScrollTo(id widget.ListItemID) { l.quietly(func() { l.List.ScrollTo(id) }) }
func (l *logList) Select(id widget.ListItemID)   { l.quietly(func() { l.List.Select(id) }) }
func (l *logList) UnselectAll()                  { l.quietly(l.List.UnselectAll) }
