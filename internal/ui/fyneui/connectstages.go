package fyneui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A connect's stages as the window shows them (D-114): on a failed connect's error page
// (errorpage.go) and under each connect of Connection Updates (connectlog.go). One row per stage in
// the order the connect ran them: a glyph in its result's status colour (a tick for passed, a cross
// for failed, a ring for not reached, a dash for skipped), the stage's name, and what it found or
// why it failed, wrapped to the row's width, with how long it took. Glow is never the only signal
// (design-language.md, principle 7): every result has its glyph and its words.

// stageLabels name the stages, in the owner's words of AW17 (kubeconfig valid and complete, the
// API's name resolved by DNS, TCP to the address and port, then the API server's connect).
var stageLabels = map[string]string{
	"kubeconfig": "Kubeconfig valid and complete",
	"login":      "Login by the credential plugin",
	"dns":        "DNS: the API server's name resolved",
	"tcp":        "TCP: the address and port reached",
	"tls":        "TLS handshake",
	"api":        "API server: the login and the first request",
}

// Stage results (engine.ConnectStage.Result, cluster.Stage).
const (
	stageOK         = "ok"
	stageFailed     = "failed"
	stageNotReached = "notReached"
	stageSkipped    = "skipped"
	stageCancelled  = "cancelled"
)

// stageLabel is a stage's name as the window writes it.
func stageLabel(name string) string {
	if l := stageLabels[name]; l != "" {
		return l
	}
	return name
}

// stageMark is a result's glyph and status colour, and its word for a plain-text copy.
func stageMark(result string) (fyne.Resource, fyne.ThemeColorName, string) {
	switch result {
	case stageOK:
		return theme.ConfirmIcon(), colorStatusOK, "passed"
	case stageFailed:
		return theme.CancelIcon(), colorStatusError, "FAILED"
	case stageSkipped:
		return theme.ContentRemoveIcon(), colorMuted, "skipped"
	case stageCancelled:
		return theme.CancelIcon(), colorMuted, "cancelled"
	}
	return theme.RadioButtonIcon(), colorMuted, "not reached"
}

// stageDetail is what a stage's row says under its name: its text and duration, or "not reached".
func stageDetail(st engine.ConnectStage) string {
	text := st.Text
	switch {
	case st.Result == stageNotReached:
		return "not reached"
	case text == "" && st.Result == stageOK:
		text = "passed"
	}
	if st.TookMS > 0 {
		text += "  (" + tookText(st.TookMS) + ")"
	}
	return text
}

// stageRows are the rows of stages, each wrapped to width.
func stageRows(stages []engine.ConnectStage, width float32) []fyne.CanvasObject {
	th := fyne.CurrentApp().Settings().Theme()
	iconW := th.Size(theme.SizeNameInlineIcon) + th.Size(theme.SizeNamePadding)
	out := make([]fyne.CanvasObject, 0, len(stages))
	for _, st := range stages {
		res, c, _ := stageMark(st.Result)
		icon := widget.NewIcon(theme.NewColoredResource(res, c))
		name := newTextLine(stageLabel(st.Name), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
		fg := colorMuted
		if st.Result == stageFailed {
			fg = colorStatusError
		}
		detail := newParagraph(stageDetail(st), fg, max(width-iconW, 80))
		r := &stageRow{stage: st, obj: container.NewBorder(nil, nil, container.NewVBox(icon), nil, container.NewVBox(name, detail))}
		r.ExtendBaseWidget(r)
		out = append(out, r)
	}
	return out
}

// connectLogStages is a connect's stages in Connection Updates: their rows under the attempt's
// header, above its lines, on a plate.
func connectLogStages(stages []engine.ConnectStage, width float32) fyne.CanvasObject {
	th := fyne.CurrentApp().Settings().Theme()
	rows := stageRows(stages, width-2*th.Size(theme.SizeNameInnerPadding))
	return container.NewVBox(newSpace(theme.SizeNamePadding), plateOf(container.NewVBox(rows...)), newSpace(theme.SizeNamePadding))
}

// stageRow is one stage's row; it keeps its stage for tests.
type stageRow struct {
	widget.BaseWidget
	stage engine.ConnectStage
	obj   fyne.CanvasObject
}

// CreateRenderer implements fyne.Widget.
func (r *stageRow) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(r.obj) }

// stagesText is the stages as plain text, one line each, for Copy: "  FAILED  Kubeconfig valid and
// complete: the client certificate file … does not exist".
func stagesText(stages []engine.ConnectStage) string {
	var out string
	for _, st := range stages {
		_, _, word := stageMark(st.Result)
		out += "  " + padRight(word, 11) + " " + stageLabel(st.Name) + ": " + stageDetail(st) + "\n"
	}
	return out
}

// failedStage is the stage that failed, or nil.
func failedStage(stages []engine.ConnectStage) *engine.ConnectStage {
	for i := range stages {
		if stages[i].Result == stageFailed || stages[i].Result == stageCancelled {
			return &stages[i]
		}
	}
	return nil
}

// stagesAt is when a failure's stages were measured, in the user's date and time format.
func (sh *shell) stagesAt(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return sh.timeFormat().Format(at)
}
