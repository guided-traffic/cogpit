package fyneui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The connection log (G2, D-66): "Connection Updates: <context>", INFO, WARN and ERROR lines as Lens
// shows them behind its warning icon (captured 2026-10-12), extended by Cogpit to every attempt
// kept (7 days, 1 MiB per context): each connect, reconnect, login and disconnect with its kind,
// start, outcome and duration, and its steps with their times and durations, successful ones too.
// It opens from every status dot of a cluster (bars.go, the navigator's rows), from the welcome
// page's warning icon and from Show Connection Updates… in a navigator row's menu, for any context:
// one without a log says so.
//
// The attempts are shown newest first, each with its lines in the order they happened: the latest
// attempt is the first thing the dialog shows without scrolling, and each attempt reads top down
// like a log and ends in how it ended, which its header says in its colour. An interruption is an
// entry of its own under the reconnect after it (the owner's WF37 of 2026-10-19: a reconnect that
// succeeded read as ending in "Connection interrupted", its cause written into its top). While the dialog is open it follows the log: the engine announces each change
// (EventConnectLog, coalesced), and the dialog reads cluster.connectLog again (the engine's memory
// and the context's file; nothing is asked of the cluster). Copy puts every attempt shown on the
// clipboard as plain text, for a bug report. A line's time is its time of day in the user's zone
// and format, an attempt's its date and time (D-31). Reconnect (cluster.reconnect, D-81) connects a
// connected cluster anew from its kubeconfig file, where its log says why it needs it; it is
// greyed while the context is not connected or reconnects.

// connectLogLimit is how many attempts the dialog shows, newest first.
const connectLogLimit = 50

type connectLogLine struct {
	Time   time.Time `json:"time"`
	Level  string    `json:"level"`
	Text   string    `json:"text"`
	Step   string    `json:"step"`
	TookMS int64     `json:"tookMs"`
}

type connectLogAttempt struct {
	Kind    string           `json:"kind"`
	Start   time.Time        `json:"start"`
	End     time.Time        `json:"end"`
	TookMS  int64            `json:"tookMs"`
	Outcome string           `json:"outcome"`
	Running bool             `json:"running"`
	Lines   []connectLogLine `json:"lines"`
	Omitted int              `json:"omitted"`

	headerless bool // the lines of a result without attempts: shown as they are
}

type connectLogView struct {
	Context   string              `json:"context"`
	Lines     []connectLogLine    `json:"lines"`
	Attempts  []connectLogAttempt `json:"attempts"`
	FileError string              `json:"fileError"`
}

// attempts are the view's attempts, the newest connectLogLimit; a result without them (an older
// engine) is its lines as one.
func (v connectLogView) attempts() []connectLogAttempt {
	if len(v.Attempts) > 0 || len(v.Lines) == 0 {
		return v.Attempts[:min(len(v.Attempts), connectLogLimit)]
	}
	return []connectLogAttempt{{Kind: engine.AttemptConnect, Start: v.Lines[0].Time, Lines: v.Lines, headerless: true}}
}

// connectLogDialog is the open "Connection Updates" dialog.
type connectLogDialog struct {
	ctx       string
	d         *dialog.CustomDialog
	close     *widget.Button
	copy      *widget.Button
	reconnect *widget.Button
	rows      *fyne.Container
	scroll    *container.Scroll
	view      connectLogView
	reading   bool // a read of the log is under way
	stale     bool // the log changed while it was read: read again
}

// showConnectLog reads the log of contextID and shows it. It is safe for any context: connected or
// not, failed or not, and one without a log.
func (k *kubeconfigUI) showConnectLog(contextID string) {
	k.readConnectLog(contextID, func(v connectLogView) { k.openConnectLog(contextID, v) })
}

// readConnectLog dispatches cluster.connectLog for contextID and hands the result to done.
func (k *kubeconfigUI) readConnectLog(contextID string, done func(connectLogView)) {
	k.sh.dispatch("cluster.connectLog", map[string]string{contextParam: contextID}, func(res any, err error) {
		if err != nil {
			k.sh.showError(err)
			return
		}
		var v connectLogView
		if err := decode(res, &v); err != nil {
			k.sh.toast("error", "Unexpected connection log: "+err.Error())
			return
		}
		done(v)
	})
}

func (k *kubeconfigUI) openConnectLog(contextID string, v connectLogView) {
	if k.connectLog != nil {
		k.connectLog.d.Hide()
	}
	cl := &connectLogDialog{ctx: contextID, rows: container.NewVBox()}
	cl.scroll = container.NewVScroll(cl.rows)
	bg := newSurface(theme.ColorNameInputBackground)
	bg.radius, bg.stroke = theme.SizeNameInputRadius, colorHairline
	content := sized(sizePickerWidth, sizePickerHeight, container.NewStack(bg, inset(theme.SizeNamePadding, theme.SizeNamePadding, cl.scroll)))

	cl.d = dialog.NewCustomWithoutButtons("Connection Updates: "+kubeconfig.DisplayText(contextID, maxShownArg), content, k.sh.win)
	cl.close = widget.NewButton("Close", cl.d.Hide)
	cl.copy = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		k.sh.copyText(connectLogText(cl.ctx, cl.view, k.sh.timeFormat()), false)
		k.sh.toast("info", "Copied the connection log of "+kubeconfig.DisplayText(cl.ctx, maxShownArg))
	})
	cl.reconnect = widget.NewButtonWithIcon(reconnectLabel, reconnectIcon(), func() { k.sh.reconnect(cl.ctx) })
	cl.d.SetButtons([]fyne.CanvasObject{cl.reconnect, cl.copy, cl.close})
	cl.d.SetOnClosed(func() {
		if k.connectLog == cl {
			k.connectLog = nil
		}
	})
	k.connectLog = cl
	k.fillConnectLog(cl, v)
	k.sh.showDialog(cl.d) // Escape closes it (AW19)
}

// connectLogChanged follows the engine's EventConnectLog: the open dialog of that context reads
// the log again, one read at a time.
func (k *kubeconfigUI) connectLogChanged(contextID string) {
	cl := k.connectLog
	if cl == nil || cl.ctx != contextID {
		return
	}
	if cl.reading {
		cl.stale = true
		return
	}
	cl.reading = true
	k.readConnectLog(contextID, func(v connectLogView) {
		cl.reading = false
		if k.connectLog != cl {
			return
		}
		k.fillConnectLog(cl, v)
		if cl.stale {
			cl.stale = false
			k.connectLogChanged(contextID)
		}
	})
}

// followReconnect enables the dialog's Reconnect for a connected context that does not reconnect
// now. Main goroutine.
func (k *kubeconfigUI) followReconnect(cl *connectLogDialog) {
	if cl == nil || cl.reconnect == nil {
		return
	}
	ui := k.sh.ui
	switch {
	case !reconnectable(ui, cl.ctx):
		cl.reconnect.SetText(reconnectLabel)
		cl.reconnect.Disable()
	case reconnecting(ui, cl.ctx):
		cl.reconnect.SetText(reconnectingText)
		cl.reconnect.Disable()
	default:
		cl.reconnect.SetText(reconnectLabel)
		cl.reconnect.Enable()
	}
}

// fillConnectLog shows v in the dialog, keeping the scroll position.
func (k *kubeconfigUI) fillConnectLog(cl *connectLogDialog, v connectLogView) {
	cl.view = v
	k.followReconnect(cl)
	th := k.sh.app.Settings().Theme()
	width := th.Size(sizePickerWidth) - 8*th.Size(theme.SizeNamePadding)
	tf := k.sh.timeFormat()
	cl.rows.RemoveAll()
	if v.FileError != "" {
		cl.rows.Add(newParagraph("The log file could not be read ("+v.FileError+"); this session's attempts are shown.", colorStatusWarning, width))
	}
	attempts := v.attempts()
	if len(attempts) == 0 {
		cl.rows.Add(newParagraph("Nothing was recorded for "+kubeconfig.DisplayText(cl.ctx, maxShownArg)+
			" in the last 7 days: no connect, reconnect, login or disconnect. Its attempts appear here as they happen.", colorMuted, width))
	}
	for i, a := range attempts {
		if i > 0 {
			cl.rows.Add(newTextLine(" ", colorMuted, theme.SizeNameText, fyne.TextStyle{}))
		}
		if !a.headerless {
			label, fg := attemptHeader(a, tf)
			cl.rows.Add(newTextLine(label, fg, theme.SizeNameText, fyne.TextStyle{Bold: true}))
		}
		for _, l := range a.Lines {
			cl.rows.Add(connectLogRow(l, tf, width))
		}
		if a.Omitted > 0 {
			cl.rows.Add(newTextLine(fmt.Sprintf("(%d further lines left out)", a.Omitted), colorMuted, theme.SizeNameText, fyne.TextStyle{}))
		}
	}
	offset := cl.scroll.Offset
	cl.rows.Refresh()
	cl.scroll.Offset = offset
	cl.scroll.Refresh()
}

// connectLogRow is one line: its time and level in the level's colour, then its text and the
// step's duration.
func connectLogRow(l connectLogLine, tf model.TimeFormat, width float32) fyne.CanvasObject {
	fg := colorMuted
	switch l.Level {
	case engine.LogError:
		fg = colorStatusError
	case engine.LogWarn:
		fg = colorStatusWarning
	}
	prefix := newTextLine(tf.FormatTime(l.Time)+"  "+padRight(l.Level, 5)+"  ", fg,
		theme.SizeNameText, fyne.TextStyle{Monospace: true})
	text := l.Text
	if l.TookMS > 0 {
		text += "  (" + tookText(l.TookMS) + ")"
	}
	return container.NewBorder(nil, nil, container.NewVBox(prefix), nil,
		newParagraph(text, theme.ColorNameForeground, width-prefix.MinSize().Width))
}

// attemptLabels are the attempts' kinds as the dialog names them.
var attemptLabels = map[string]string{
	engine.AttemptConnect:      "Connect",
	engine.AttemptReconnect:    "Reconnect",
	engine.AttemptLogin:        "Login",
	engine.AttemptDisconnect:   "Disconnect",
	engine.AttemptInterruption: "Interruption",
}

// attemptHeader is an attempt's header line and its colour: kind, start, outcome and duration.
func attemptHeader(a connectLogAttempt, tf model.TimeFormat) (string, fyne.ThemeColorName) {
	kind := attemptLabels[a.Kind]
	if kind == "" {
		kind = kubeconfig.DisplayText(a.Kind, 40)
	}
	head := kind + " · " + tf.Format(a.Start) + " · "
	took := ""
	if a.TookMS > 0 {
		took = " after " + tookText(a.TookMS)
	}
	switch {
	case a.Running:
		return head + "running…", colorStatusPending
	case a.Outcome == engine.OutcomeOK && a.Kind == engine.AttemptDisconnect:
		return head + "disconnected", colorMuted
	case a.Outcome == engine.OutcomeOK:
		return head + "succeeded" + took, colorStatusOK
	case a.Outcome == engine.OutcomeFailed && a.Kind == engine.AttemptDisconnect:
		return head + "closed by Cogpit", colorStatusError
	case a.Outcome == engine.OutcomeFailed:
		return head + "failed" + took, colorStatusError
	case a.Outcome == engine.OutcomeAbandoned:
		return head + "ended by a newer attempt" + took, colorMuted
	case a.Outcome == engine.OutcomeCancelled: // Cancel Connecting (D-73)
		return head + "cancelled" + took, colorMuted
	case a.Outcome == engine.OutcomeInterrupted: // the reconnect after it is the attempt above (WF37)
		return head + "the connection was lost", colorStatusWarning
	}
	return head + "no outcome recorded (Cogpit stopped first)", colorMuted
}

// tookText writes a duration in milliseconds as the engine's lines do (engine.FormatSpan): "340 ms",
// "2.4 s", "10 min 2 s", so that a header and its lines say a duration the same way (WF37).
func tookText(ms int64) string { return engine.FormatSpan(time.Duration(ms) * time.Millisecond) }

// connectLogText is the log as Copy puts it on the clipboard: a heading, then each attempt's
// header and lines, newest first.
func connectLogText(ctx string, v connectLogView, tf model.TimeFormat) string {
	var b strings.Builder
	b.WriteString("Connection Updates: " + ctx + "\n")
	attempts := v.attempts()
	if len(attempts) == 0 {
		b.WriteString("Nothing was recorded in the last 7 days.\n")
	}
	for _, a := range attempts {
		if !a.headerless {
			label, _ := attemptHeader(a, tf)
			b.WriteString("\n== " + label + " ==\n")
		}
		for _, l := range a.Lines {
			b.WriteString(tf.FormatTime(l.Time) + "  " + padRight(l.Level, 5) + "  " + l.Text)
			if l.TookMS > 0 {
				b.WriteString("  (" + tookText(l.TookMS) + ")")
			}
			b.WriteString("\n")
		}
		if a.Omitted > 0 {
			fmt.Fprintf(&b, "(%d further lines left out)\n", a.Omitted)
		}
	}
	return b.String()
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
