package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
)

// buildLabel is the version right of the top bar's wordmark (F12, the owner), in the wordmark's
// colour and weight but about 30% smaller, on the wordmark's baseline (the owner: "nicht so wichtig
// … muss kleiner werden"): "v0.16.0" for a release; "dev" for a development build, its commit in
// the tooltip as internal/buildinfo has it ("dev 0691fb32abcd-dirty"). Cogpit's own: Lens's title
// bar shows no version. The README's showcase pictures hide it (showcase_test.go).
type buildLabel struct {
	textLine
	sh  *shell
	tip string            // "" for a release: the label says it all
	gap fyne.CanvasObject // the gap after it, before the history arrows; nil without them
}

var _ desktop.Hoverable = (*buildLabel)(nil)

func newBuildLabel(sh *shell, info buildinfo.Info) *buildLabel {
	b := &buildLabel{textLine: textLine{color: theme.ColorNameForeground, size: theme.SizeNameCaptionText,
		style: fyne.TextStyle{Bold: true}, baselineOf: theme.SizeNameSubHeadingText}, sh: sh}
	b.ExtendBaseWidget(b)
	b.set(info)
	return b
}

// set shows a build's version.
func (b *buildLabel) set(info buildinfo.Info) {
	text, tip := buildLabelText(info)
	b.tip = tip
	b.SetText(text)
}

// buildLabelText is the label and its tooltip for a build: "v" and a release's version (a v of
// its own not doubled), no tooltip; "dev" and the commit ("dev 0691fb32abcd", "-dirty" for a tree
// with changes), or "Development build" when Go recorded no commit (go run).
func buildLabelText(info buildinfo.Info) (text, tip string) {
	if info.Release {
		return "v" + strings.TrimPrefix(info.Version, "v"), ""
	}
	if info.CommitText() == "" {
		return buildinfo.Dev, devBuild
	}
	return buildinfo.Dev, info.String()
}

// conceal hides the label and its gap: the README's showcase pictures show none (showcase_test.go).
func (b *buildLabel) conceal() {
	b.Hide()
	if b.gap != nil {
		b.gap.Hide()
	}
}

// MouseIn implements desktop.Hoverable.
func (b *buildLabel) MouseIn(ev *desktop.MouseEvent) { b.sh.tip.hover(b, b.tip, ev.AbsolutePosition) }

// MouseMoved implements desktop.Hoverable.
func (b *buildLabel) MouseMoved(ev *desktop.MouseEvent) { b.sh.tip.moved(b, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (b *buildLabel) MouseOut() { b.sh.tip.hideFor(b) }
