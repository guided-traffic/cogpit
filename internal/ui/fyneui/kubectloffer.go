package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// kubectlOffer is the line above a terminal tab's terminal that shows what the engine decided
// about the tab's kubectl (D-3; engine/kubectl.go, UIState: DockTab.Kubectl): the release that
// matches the cluster with a Download button when the kubectl the terminal finds is more than one
// minor version away or missing, the download's progress and outcome (a New Terminal button once
// it is in place), or the release Cogpit put first on the terminal's PATH. The text is the
// engine's (KubectlCheck.Message), the same an agent reads; the line hides without one. The
// download runs only on the button's click, through kubectl.download, which the engine refuses for
// every origin but this window. Cogpit's own line and wording (inferred: Lens downloads without
// asking).
type kubectlOffer struct {
	sh    *shell
	tabID string
	root  *fyne.Container // hidden while there is nothing to say

	check *engine.KubectlCheck // what the line shows
	text  *textLine
	// download fetches the offered release; newTerm opens a terminal once it is in place.
	download, newTerm *textButton
	asked             bool // the download was asked for in this window and has not answered yet
}

func newKubectlOffer(sh *shell, t engine.DockTab) *kubectlOffer {
	o := &kubectlOffer{sh: sh, tabID: t.ID, root: container.NewStack()}
	o.text = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	o.text.truncate = true
	o.download = newPlateButton("Download", theme.DownloadIcon(), o.startDownload)
	o.download.Importance = widget.HighImportance
	o.newTerm = newPlateButton("New Terminal", theme.ContentAddIcon(), func() {
		sh.dispatch("terminal.open", struct{}{}, nil)
	})
	o.update(t)
	return o
}

// startDownload asks the engine for the download; the tab's state shows its progress.
func (o *kubectlOffer) startDownload() {
	if o.asked {
		return
	}
	o.asked = true
	o.render()
	o.sh.dispatch("kubectl.download", kubectlDownloadParams{Tab: o.tabID}, func(_ any, err error) {
		o.asked = false
		o.render()
		if err != nil && (o.check == nil || o.check.Download == nil) {
			o.sh.showError(err) // refused before it started: the line cannot say it
		}
	})
}

// kubectlDownloadParams mirror kubectl.download's parameters.
type kubectlDownloadParams struct {
	Tab string `json:"tab"`
}

// update follows the tab's state. Main goroutine.
func (o *kubectlOffer) update(t engine.DockTab) {
	if sameCheck(t.Kubectl, o.check) && (o.check != nil || !o.root.Visible()) {
		return
	}
	o.check = t.Kubectl
	o.render()
}

// sameCheck compares two checks by value, their downloads included.
func sameCheck(a, b *engine.KubectlCheck) bool {
	if a == nil || b == nil {
		return a == b
	}
	if (a.Download == nil) != (b.Download == nil) || (a.Download != nil && *a.Download != *b.Download) {
		return false
	}
	x, y := *a, *b
	x.Download, y.Download = nil, nil
	return x == y
}

// render lays the line out for the check: tint and icon by state, the text, the buttons.
func (o *kubectlOffer) render() {
	c := o.check
	if c == nil || c.Message == "" {
		o.root.Objects = nil
		o.root.Hide()
		o.root.Refresh()
		return
	}
	tint, icon := colorStatusWarning, theme.WarningIcon()
	var buttons []fyne.CanvasObject
	switch {
	case c.Download != nil && c.Download.State == engine.KubectlRunning:
		tint, icon = colorAccent, theme.DownloadIcon()
	case c.Download != nil && c.Download.State == engine.KubectlDone:
		tint, icon = colorStatusOK, theme.ConfirmIcon()
		buttons = append(buttons, o.newTerm)
	case c.Download != nil && c.Download.State == engine.KubectlFailed:
		tint, icon = colorStatusError, theme.ErrorIcon()
		buttons = append(buttons, o.download)
	case c.Offer == "":
		tint, icon = colorAccent, theme.InfoIcon() // a release Cogpit downloaded comes first
	case !c.DownloadOff:
		buttons = append(buttons, o.download)
	}
	if o.asked {
		o.download.Disable()
	} else {
		o.download.Enable()
	}
	o.text.SetText(c.Message)
	content := container.NewBorder(nil, nil,
		container.NewHBox(container.NewCenter(widget.NewIcon(themedIcon(icon, tint))), newHGap(theme.SizeNameInnerPadding)),
		container.NewCenter(container.NewHBox(buttons...)),
		o.text)
	o.root.Objects = []fyne.CanvasObject{container.NewBorder(nil, newDivider(false), nil, nil,
		container.NewStack(newBannerTint(tint), inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, content)))}
	o.root.Show()
	o.root.Refresh()
}

// withKubectlOffer puts a terminal tab's kubectl line above its terminal.
func (c *dockContent) withKubectlOffer(term fyne.CanvasObject) fyne.CanvasObject {
	if c.kubectl == nil {
		return term
	}
	return container.NewBorder(c.kubectl.root, nil, nil, nil, term)
}
