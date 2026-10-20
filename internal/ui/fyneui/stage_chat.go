package fyneui

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// The agent chat's violet identity (docs/developer/design-language.md): violet is the agent's light
// and only the agent's. The header names the agent with a bracket label, a slow wave runs under it
// while a reply is being written, and the agent's messages carry a violet hairline.

// agentHeadText is the header's bracket label: "agent · <model>", or the connection's name when it
// has no model, or "agent" when there is no connection yet.
func agentHeadText(cn string, model string, ok bool) string {
	switch {
	case !ok:
		return "agent"
	case model != "":
		return "agent · " + model
	case cn != "":
		return "agent · " + cn
	}
	return "agent"
}

// thinkingWave is the wave under the chat's header while the agent writes a reply: a violet sine
// whose phase moves slowly. Under Reduce motion it stands still, and in tests (whose driver ticks an
// animation to its end at once) it is a fixed phase.
type thinkingWave struct {
	w    *wave
	sh   *shell
	stop func()
}

// wavePeriod is how long the wave takes to move one period.
const wavePeriod = 3600 * time.Millisecond

func newThinkingWave(sh *shell) *thinkingWave {
	t := &thinkingWave{sh: sh, w: newWave(colorAgent, 4, 0.55)}
	t.w.min = fyne.NewSize(60, 16)
	t.w.phase = 0.6 // the still wave's phase: a crest near the left, as the reference's
	t.w.Hide()
	return t
}

// set shows the wave while running; it moves unless Reduce motion is on.
func (t *thinkingWave) set(running bool) {
	switch {
	case !running:
		if t.stop != nil {
			t.stop()
			t.stop = nil
		}
		if t.w.Visible() {
			t.w.Hide()
		}
	case !t.w.Visible():
		t.w.Show()
		fallthrough
	default:
		if t.stop == nil && !t.sh.reduceMotion() {
			a := fyne.NewAnimation(wavePeriod, func(p float32) { t.w.SetPhase(0.6 + 2*math.Pi*float64(p)) })
			a.RepeatCount = fyne.AnimationRepeatForever
			a.Curve = fyne.AnimationLinear
			a.Start()
			t.stop = a.Stop
		}
		if t.stop != nil && t.sh.reduceMotion() {
			t.stop()
			t.stop = nil
		}
	}
}

// answerIndent is the room left of an agent's message for its hairline.
const answerIndent = 12

// answerRenderer draws the agent's answer as the vstack does, indented, with a violet hairline at
// its left edge: the agent's messages are told from the user's cards and the tool rows by it.
type answerRenderer struct {
	v     *chatAnswerView
	inner *vstackRenderer
	bar   *canvas.Rectangle
}

func (v *chatAnswerView) CreateRenderer() fyne.WidgetRenderer {
	r := &answerRenderer{v: v, inner: &vstackRenderer{v: v.vstack}, bar: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

func (r *answerRenderer) MinSize() fyne.Size {
	ms := r.inner.MinSize()
	return fyne.NewSize(ms.Width+answerIndent, ms.Height)
}

func (r *answerRenderer) Layout(size fyne.Size) {
	r.inner.Layout(fyne.NewSize(max(size.Width-answerIndent, 0), size.Height))
	for _, o := range r.v.objs {
		if o.Visible() {
			o.Move(o.Position().AddXY(answerIndent, 0))
		}
	}
	r.bar.Move(fyne.NewPos(1, 2))
	r.bar.Resize(fyne.NewSize(2, max(size.Height-4, 0)))
}

func (r *answerRenderer) Refresh() {
	r.bar.FillColor = withAlpha(themeColor(r.v.Theme(), colorAgent), 0xa0)
	r.bar.CornerRadius = 1
	r.Layout(r.v.Size())
	r.bar.Refresh()
	canvas.Refresh(r.v)
}

func (r *answerRenderer) Objects() []fyne.CanvasObject {
	return append([]fyne.CanvasObject{r.bar}, r.v.objs...)
}
func (r *answerRenderer) Destroy() {}
