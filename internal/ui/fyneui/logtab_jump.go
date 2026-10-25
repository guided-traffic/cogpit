package fyneui

import (
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Jumping to a time in a logs tab (ui-parity › Logs, "Jump to a time and date"): a text field
// beside the selectors (Fyne has no date picker; the field and its forms are Cogpit's, inferred)
// takes a time in the user's zone and format, as the tab's timestamps show (D-31: "13.10.2026
// 01:20"), or "10m ago"; Enter dispatches logs.jumpTo, which reads it in the engine, and the tab
// follows the jump in the engine's state, whoever asked: it scrolls to the first line written at
// or after that time among the lines it holds, and when it holds none that old for a container it
// shows, it loads them first (OlderLogs with since).

// The field is as wide as a search box, and wider when its placeholder, which names the forms it
// reads in the user's format, needs it (#169, a defect the owner reported on 2026-10-14 evening: at
// 1440 points the placeholder was cut after "10m"). The header row has room for it: it holds only
// the two selectors besides.

// jumpState is what the tab knows about jumps.
type jumpState struct {
	seen    int       // the engine's jump this tab has carried out
	pending bool      // a jump that came before the stream opened
	at      time.Time // its time
}

// newJumpField returns the field a time to jump to is typed into.
func (lt *logTab) newJumpField() *wheelEntry {
	e := newWheelEntry()
	// An example in the user's format, written as typed (no zone to convert from).
	example, tf := time.Date(2026, 10, 13, 1, 20, 0, 0, time.UTC), lt.sh.ui.TimeFormat.In(time.UTC)
	e.SetPlaceHolder("Jump to time: " + tf.FormatDate(example) + " " + tf.FormatClock(example) + ", 10m ago")
	e.SetIcon(theme.HistoryIcon())
	e.OnSubmitted = func(text string) {
		lt.sh.dispatch("logs.jumpTo", logJumpParams{ID: lt.tab.ID, Time: text}, func(_ any, err error) { lt.sh.showError(err) })
	}
	return e
}

// fitPlaceholderLayout sizes the time field: the theme's search width, or what its placeholder
// needs to show whole when that is more.
type fitPlaceholderLayout struct{}

func (fitPlaceholderLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	ms := sizedLayout{width: sizeSearchWidth}.MinSize(objs)
	for _, o := range objs {
		if e, ok := o.(*wheelEntry); ok && e.Visible() {
			ms.Width = max(ms.Width, entryWidthFor(&e.Entry, e.PlaceHolder))
		}
	}
	return ms
}

func (fitPlaceholderLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	sizedLayout{}.Layout(objs, size)
}

// entryWidthFor is the width a one-line entry with a leading icon needs to show text whole, as Fyne
// 2.8.1 lays it out (entryRenderer.Layout): the icon and the line spacing after it, the text with
// the inner padding on both sides, rounded up to a whole point.
func entryWidthFor(e *widget.Entry, text string) float32 {
	th := e.Theme()
	w := fyne.MeasureText(text, th.Size(theme.SizeNameText), e.TextStyle).Width + 2*th.Size(theme.SizeNameInnerPadding)
	if e.Icon != nil {
		w += th.Size(theme.SizeNameInlineIcon) + th.Size(theme.SizeNameLineSpacing)
	}
	return float32(math.Ceil(float64(w)))
}

// logJumpParams mirror logs.jumpTo's parameters.
type logJumpParams struct {
	ID   string `json:"id"`
	Time string `json:"time"`
}

// followJump carries out the jump the engine's state shows if the tab has not.
func (lt *logTab) followJump(t engine.DockTab) {
	if t.Logs.Jump.Seq == lt.jump.seen {
		return
	}
	lt.jump.seen = t.Logs.Jump.Seq
	at := t.Logs.Jump.Time
	lt.jumpField.SetText(lt.sh.timeFormat().Format(at)) // as the timestamps show it, and read back so
	lt.jumpTo(at)
}

// jumpTo scrolls to the first line written at or after at, loading older lines first when the
// tab holds none that old of a container it shows and does not hold whole.
func (lt *logTab) jumpTo(at time.Time) {
	if lt.failed {
		return
	}
	v := lt.view
	if v == nil {
		lt.jump.pending, lt.jump.at = true, at
		return
	}
	held := v.Held()
	older := len(held) == 0
	for _, h := range held {
		older = older || h.Oldest.After(at) && !lt.older.whole[h.Source]
	}
	if _, ok := lt.sh.eng.(olderLogsEngine); !ok || !older || lt.older.busy {
		lt.finishJump(at)
		return
	}
	lt.loadOlder(at, func() { lt.finishJump(at) })
}

// finishJump scrolls to the line, or says there is none.
func (lt *logTab) finishJump(at time.Time) {
	if lt.view.JumpTo(at) {
		return
	}
	lt.sh.toast("info", "No line written at or after "+lt.sh.timeFormat().Format(at)+" in this tab")
}
