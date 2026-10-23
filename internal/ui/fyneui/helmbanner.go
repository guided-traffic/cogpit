package fyneui

import (
	"fmt"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// helmBanner shows the connected context's unfinished Helm release actions below the top bar, one
// row each, as drainBanner shows drains (D-11): what happened to the release, and Finish (the
// rollback or uninstall again, as recorded, confirmed as usual; an upgrade's rollback) and Dismiss
// (helm.dismissOperation). An install is offered nothing (owner, 2026-10-14 evening, A17): its row
// says the release's state, Show release opens it in the Helm Releases view, where the user cleans
// up, and Dismiss forgets the row. An action running in this window shows as running. The engine
// announces changes with EventHelm; the banner reads the actions with helm.pendingOperations, so
// that it shows what an agent would read.
type helmBanner struct {
	sh     *shell
	root   *fyne.Container // the rows; hidden while there are none
	queued atomic.Bool     // a re-read is queued; any goroutine
	ops    []engine.PendingHelmOpInfo
}

func newHelmBanner(sh *shell) *helmBanner {
	b := &helmBanner{sh: sh, root: container.NewVBox()}
	b.root.Hide()
	return b
}

// request re-reads the pending actions. Any goroutine may call it; calls while one is queued are
// covered by it.
func (b *helmBanner) request() {
	if !b.queued.CompareAndSwap(false, true) {
		return
	}
	b.sh.do(func() {
		b.queued.Store(false)
		b.sh.dispatch("helm.pendingOperations", struct{}{}, b.show)
	})
}

// show renders a helm.pendingOperations result. An error (no cluster connected, no journal, an
// unreadable journal, which the engine reports itself) empties the banner.
func (b *helmBanner) show(res any, err error) {
	var list []engine.PendingHelmOpInfo
	if err == nil {
		err = decode(res, &list)
	}
	if err != nil {
		list = nil
	}
	b.ops = list
	rows := make([]fyne.CanvasObject, 0, len(list)+1)
	for _, op := range list {
		rows = append(rows, b.row(op))
	}
	if len(rows) > 0 {
		rows = append(rows, newDivider(false))
	}
	b.root.Objects = rows
	if len(rows) == 0 {
		b.root.Hide()
	} else {
		b.root.Show()
	}
	b.root.Refresh()
}

// showReleaseLabel is an interrupted install's button: it opens the release in the Helm Releases
// view (ui.navigate), where the user cleans up; Cogpit offers nothing to finish an install (A17).
const showReleaseLabel = "Show release"

// helmRollbackParams mirror helm.release.rollback's parameters.
type helmRollbackParams struct {
	refParams
	Revision int `json:"revision"`
}

// row is one action: a tinted strip with an icon, two lines of text and the actions.
func (b *helmBanner) row(op engine.PendingHelmOpInfo) fyne.CanvasObject {
	ref := op.Ref()
	tint, icon := colorStatusWarning, theme.WarningIcon()
	if op.Status == engine.HelmOpStatusRunning {
		tint, icon = colorAccent, theme.InfoIcon()
	}
	title := newTextLine(helmOpTitle(op), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	title.truncate = true
	detail := newTextLine(helmOpDetail(op, b.sh.timeFormat()), colorMuted, theme.SizeNameText, fyne.TextStyle{})
	detail.truncate = true

	buttons := container.NewHBox()
	if op.Status != engine.HelmOpStatusRunning {
		dismiss := newTextButton("Dismiss", nil, func() { b.sh.dispatch("helm.dismissOperation", refParams{Ref: ref}, nil) })
		buttons.Add(dismiss)
		var finish *widget.Button
		switch op.Action {
		case engine.HelmRollback:
			finish = widget.NewButtonWithIcon("Finish rollback", theme.MediaPlayIcon(), func() {
				b.sh.dispatch("helm.release.rollback", helmRollbackParams{refParams: refParams{Ref: ref}, Revision: op.Revision}, nil)
			})
		case engine.HelmUpgrade: // the SDK refuses to upgrade a release left pending-upgrade: roll back (architecture §8)
			finish = widget.NewButtonWithIcon(fmt.Sprintf("Roll back to revision %d", op.FromRevision), theme.MediaPlayIcon(), func() {
				b.sh.dispatch("helm.release.rollback", helmRollbackParams{refParams: refParams{Ref: ref}, Revision: op.FromRevision}, nil)
			})
		case engine.HelmInstall:
			// Nothing is offered after an install (A17): the row shows the release's state, and the
			// user cleans up in the Helm Releases view, which this opens on the release.
			if op.ReleaseStatus != engine.HelmReleaseMissing {
				show := widget.NewButtonWithIcon(showReleaseLabel, theme.VisibilityIcon(), func() {
					b.sh.dispatch("ui.navigate", refParams{Ref: ref}, nil)
				})
				buttons.Add(show)
			}
		default:
			finish = widget.NewButtonWithIcon("Finish uninstall", theme.MediaPlayIcon(), func() {
				b.sh.dispatch("helm.release.uninstall", refParams{Ref: ref}, nil)
			})
		}
		if finish != nil {
			finish.Importance = widget.HighImportance
			buttons.Add(finish)
		}
	}
	content := container.NewBorder(nil, nil,
		container.NewCenter(widget.NewIcon(themedIcon(icon, tint))),
		container.NewCenter(buttons),
		container.NewVBox(title, detail))
	return container.NewStack(newBannerTint(tint), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content))
}

// helmOpTitle is a row's first line.
func helmOpTitle(op engine.PendingHelmOpInfo) string {
	what := "The uninstall of release " + op.Release
	switch op.Action {
	case engine.HelmRollback:
		what = fmt.Sprintf("The rollback of release %s to revision %d", op.Release, op.Revision)
	case engine.HelmUpgrade:
		what = fmt.Sprintf("The upgrade of release %s from revision %d to %s %s", op.Release, op.FromRevision, op.Chart, op.ChartVersion)
	case engine.HelmInstall:
		what = fmt.Sprintf("The install of release %s from %s %s", op.Release, op.Chart, op.ChartVersion)
	}
	switch op.Status {
	case engine.HelmOpStatusRunning:
		switch op.Action {
		case engine.HelmRollback:
			return fmt.Sprintf("Rolling back release %s to revision %d", op.Release, op.Revision)
		case engine.HelmUpgrade:
			return fmt.Sprintf("Upgrading release %s to %s %s", op.Release, op.Chart, op.ChartVersion)
		case engine.HelmInstall:
			return fmt.Sprintf("Installing release %s from %s %s", op.Release, op.Chart, op.ChartVersion)
		}
		return "Uninstalling release " + op.Release
	case engine.HelmOpStatusFailed:
		return what + " failed"
	}
	return what + " did not finish"
}

// helmOpDetail is a row's second line: where and when the action was started and by whom, the
// state the release is in, and why the action did not finish, the cause last because it can be
// long and is cut off first; the full text is in helm.pendingOperations. The time is written in
// tf, the user's zone and format (D-31).
func helmOpDetail(op engine.PendingHelmOpInfo, tf model.TimeFormat) string {
	parts := []string{fmt.Sprintf("Context %s, namespace %s, started %s by %s", op.Context, op.Namespace,
		tf.Format(op.Started), starterText(op.StartedBy))}
	if op.Status == engine.HelmOpStatusRunning {
		if op.Elsewhere && op.Owner != nil { // as drains: this window will not finish it (H-22, R-9)
			parts = append(parts, fmt.Sprintf("in another Cogpit window (process %d)", op.Owner.PID))
		}
		return strings.Join(parts, " · ")
	}
	switch op.ReleaseStatus {
	case engine.HelmReleaseMissing:
		parts = append(parts, "the release no longer exists")
	case engine.HelmReleaseUnknown, "":
	default:
		parts = append(parts, fmt.Sprintf("the release is %s at revision %d", op.ReleaseStatus, op.ReleaseRevision))
	}
	if op.Status == engine.HelmOpStatusFailed {
		parts = append(parts, op.Error)
	} else {
		parts = append(parts, "Cogpit ended before the action did")
	}
	return strings.Join(parts, " · ")
}
