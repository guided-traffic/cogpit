package fyneui

import (
	"fmt"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Helm chart repositories in the GUI (G11, D-29): the Helm menu's "Repositories…" opens a
// dialog that lists the repositories of Helm's own repositories.yaml, read-only: each with its URL
// (never a user or password), marks for credentials, plain http, an unchecked certificate and an
// entry Cogpit does not use, its charts' count and the age of its index or its last error; above
// them Helm's file and its status; and Update All, which downloads the indexes again. Repositories
// are added and removed with the helm CLI (owner, D-29): Cogpit differs from Lens's Preferences ›
// Kubernetes › Helm Charts on purpose and has no Add or Remove. While open the dialog follows the
// engine's EventHelmRepos, so a repository helm adds appears without reopening it. The Charts view
// (helmcharts, a kind view) lists their charts.

// helmReposDialog is the open repositories dialog.
type helmReposDialog struct {
	sh      *shell
	d       *dialog.CustomDialog
	list    *fyne.Container
	file    *textLine
	fileMsg *fyne.Container // the file's status when it is not ok
	updateB *textButton
	status  *textLine
	src     engine.HelmReposList
	rows    []engine.HelmRepoInfo
	queued  atomic.Bool // a reload after EventHelmRepos is pending
	cancel  func()      // stops following EventHelmRepos
}

// helmMenu is the window menu's Helm menu.
func (sh *shell) helmMenu() *fyne.Menu {
	return fyne.NewMenu("Helm",
		fyne.NewMenuItem("Repositories…", sh.showHelmRepos),
		fyne.NewMenuItem("Update Repositories", func() { sh.dispatch("helm.repo.update", struct{}{}, nil) }),
		fyne.NewMenuItem("Charts", func() { sh.dispatch("ui.openView", openViewParams{View: helmChartsKind}, nil) }),
	)
}

// helmChartsKind is the Charts view's kind key.
const helmChartsKind = "helmcharts"

// helmReposIntro says where the repositories come from and how they are added.
const helmReposIntro = "The chart repositories of Helm's own repositories file. Add and remove them with the helm CLI " +
	"(helm repo add, helm repo remove): Cogpit takes a change over within seconds and writes nothing into Helm's files. " +
	"Credentials stay in Helm's file; Cogpit shows only whether a repository has them."

// showHelmRepos opens the repositories dialog, or reloads the open one.
func (sh *shell) showHelmRepos() {
	if sh.helmRepos != nil {
		sh.helmRepos.load()
		return
	}
	rd := &helmReposDialog{sh: sh, list: container.NewVBox(), fileMsg: container.NewVBox()}
	th := sh.app.Settings().Theme()
	rd.status = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	rd.status.truncate = true
	intro := newParagraph(helmReposIntro, colorMuted, th.Size(sizePickerWidth))
	rd.file = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	rd.file.truncate = true
	listBg := newSurface(theme.ColorNameInputBackground)
	listBg.radius, listBg.stroke = theme.SizeNameInputRadius, colorHairline
	list := container.NewStack(listBg, inset(theme.SizeNamePadding, theme.SizeNamePadding, newVScroll(rd.list)))
	top := container.NewVBox(intro, newSpace(theme.SizeNamePadding), rd.file, rd.fileMsg, newSpace(theme.SizeNamePadding))
	content := sized(sizePickerWidth, sizePickerHeight, container.NewBorder(top, rd.status, nil, nil, list))
	rd.d = dialog.NewCustomWithoutButtons("Helm Repositories", content, sh.win)
	rd.updateB = newPlateButton("Update All", theme.ViewRefreshIcon(), rd.update)
	rd.updateB.Importance = widget.HighImportance
	closeB := newPlateButton("Close", nil, nil)
	rd.d.SetButtons([]fyne.CanvasObject{rd.updateB, closeB})
	closeB.OnTapped = rd.d.Hide
	rd.cancel = sh.eng.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventHelmRepos && rd.queued.CompareAndSwap(false, true) {
			sh.do(func() {
				rd.queued.Store(false)
				if sh.helmRepos == rd {
					rd.load()
				}
			})
		}
	})
	rd.d.SetOnClosed(func() {
		rd.cancel()
		if sh.helmRepos == rd {
			sh.helmRepos = nil
		}
	})
	sh.helmRepos = rd
	sh.showDialog(rd.d)
	rd.load()
}

func (rd *helmReposDialog) setStatus(text string, c fyne.ThemeColorName) {
	rd.status.SetText(text)
	rd.status.SetColor(c)
}

// load reads the repositories and shows them.
func (rd *helmReposDialog) load() {
	rd.sh.dispatch("helm.repo.list", struct{}{}, func(res any, err error) {
		var l engine.HelmReposList
		if err == nil {
			err = decode(res, &l)
		}
		if err != nil {
			rd.setStatus(errText(err), colorStatusError)
			return
		}
		rd.src, rd.rows = l, l.Repositories
		rd.render()
	})
}

func (rd *helmReposDialog) render() {
	th := rd.sh.app.Settings().Theme()
	width := th.Size(sizePickerWidth) - 8*th.Size(theme.SizeNamePadding)
	rd.file.SetText("Helm's file: " + rd.src.File)
	var msg []fyne.CanvasObject
	switch rd.src.Status {
	case engine.HelmFileMissing:
		msg = append(msg, newTextLine("The file does not exist yet.", colorStatusWarning, theme.SizeNameCaptionText, fyne.TextStyle{}))
	case engine.HelmFileError:
		msg = append(msg, newParagraph("Not read: "+rd.src.Error+". The list below is from the last time it was read.", colorStatusError, width))
	}
	rd.fileMsg.Objects = msg
	rd.fileMsg.Refresh()

	objs := make([]fyne.CanvasObject, 0, len(rd.rows))
	for _, r := range rd.rows {
		title := newTextLine(r.Name, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
		head := []fyne.CanvasObject{title}
		for _, m := range repoMarks(r) {
			head = append(head, newPill(m.text, m.color, m.tinted))
		}
		u := newTextLine(r.URL, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
		u.truncate = true
		var info fyne.CanvasObject
		if r.Error != "" {
			info = newParagraph(r.Error, colorStatusError, width)
		} else {
			info = newTextLine(repoSummary(r, rd.sh.now()), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
		}
		objs = append(objs, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
			container.NewVBox(container.NewHBox(head...), u, info)))
	}
	if len(objs) == 0 {
		text := rd.src.Hint
		if text == "" {
			text = "No repositories."
		}
		objs = append(objs, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, newParagraph(text, colorMuted, width)))
	}
	rd.list.Objects = objs
	rd.list.Refresh()
	if len(rd.rows) == 0 {
		rd.updateB.Disable()
	} else {
		rd.updateB.Enable()
	}
}

// repoMark is a mark beside a repository's name.
type repoMark struct {
	text   string
	color  fyne.ThemeColorName
	tinted bool
}

// repoMarks are a repository's marks: whether it sends credentials (never which), plain http and
// an unchecked certificate (both warned about), and an entry Cogpit does not use.
func repoMarks(r engine.HelmRepoInfo) []repoMark {
	var out []repoMark
	if r.Unused {
		out = append(out, repoMark{"not used", colorStatusError, true})
	}
	if r.HasCredentials {
		text := "credentials"
		if r.ClientCertificate {
			text = "client certificate"
		}
		out = append(out, repoMark{text, colorMuted, false})
	}
	if r.PlainHTTP {
		out = append(out, repoMark{kinds.HelmOriginMark(kinds.HelmOriginHTTP), colorStatusWarning, true})
	}
	if r.InsecureSkipTLSVerify {
		// The Charts view's mark (A14): "insecure TLS".
		out = append(out, repoMark{kinds.HelmOriginMark(kinds.HelmOriginInsecureTLS), colorStatusWarning, true})
	}
	return out
}

// repoSummary is a repository's last line: its charts and the age of its index.
func repoSummary(r engine.HelmRepoInfo, now time.Time) string {
	charts := fmt.Sprintf("%d charts", r.Charts)
	if r.Charts == 1 {
		charts = "1 chart"
	}
	switch {
	case r.Updating:
		return charts + " · updating…"
	case r.Updated.IsZero():
		return charts + " · no index downloaded"
	}
	return charts + " · index downloaded " + model.FormatAge(now.Sub(r.Updated)) + " ago"
}

// update downloads every index again.
func (rd *helmReposDialog) update() {
	rd.updateB.Disable()
	rd.setStatus("Updating the repositories…", colorMuted)
	rd.sh.dispatch("helm.repo.update", struct{}{}, func(res any, err error) {
		rd.updateB.Enable()
		var infos []engine.HelmRepoInfo
		if err == nil {
			_ = decode(res, &infos)
		}
		failed := 0
		for _, r := range infos {
			if r.Error != "" {
				failed++
			}
		}
		switch {
		case err != nil:
			rd.setStatus("Not updated: "+errText(err), colorStatusError)
		case failed > 0:
			rd.setStatus(fmt.Sprintf("Updated; %d of %d repositories failed (below).", failed, len(infos)), colorStatusWarning)
		default:
			rd.setStatus("Updated.", colorStatusOK)
		}
		rd.load()
	})
}
