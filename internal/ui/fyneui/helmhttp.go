package fyneui

import (
	"slices"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// helmHTTPBanner asks for the Helm repositories whose user and password would go over plain http
// (credentials H-40, owner 2026-10-14): while the Charts view or a Helm Install tab is open, one
// row below the Helm banner for each such repository the user has not allowed, saying that Cogpit
// downloads nothing from it and why, with Allow…, which says plainly what the allowance means
// before helm.repo.allowHTTPCredentials (GUI-only) records it. The engine announces changes with
// EventHelmRepos; the banner reads helm.repo.list, the list an agent reads too. That Lens asks is
// not captured: Lens, as helm, sends the credentials without asking (inferred).
type helmHTTPBanner struct {
	sh     *shell
	root   *fyne.Container // the rows; hidden while there are none
	queued atomic.Bool     // a re-read is queued; any goroutine
	repos  []engine.HelmRepoInfo
	ask    *dialog.CustomDialog // the open question; main goroutine only
}

// helmHTTPItem and helmHTTPParams mirror helm.repo.allowHTTPCredentials' parameters.
type (
	helmHTTPItem struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	helmHTTPParams struct {
		Repositories []helmHTTPItem `json:"repositories"`
	}
)

func newHelmHTTPBanner(sh *shell) *helmHTTPBanner {
	b := &helmHTTPBanner{sh: sh, root: container.NewVBox()}
	b.root.Hide()
	return b
}

// helmChartsShown reports whether u shows the Charts view or holds a Helm Install tab: when the
// engine downloads the repositories' indexes by itself, and when the banner shows.
func helmChartsShown(u engine.UIState) bool {
	return u.Connection.State.Live() && (u.View == helmChartsKind ||
		slices.ContainsFunc(u.Dock, func(t engine.DockTab) bool { return t.Type == engine.DockHelmInstall }))
}

// request re-reads the repositories. Any goroutine may call it; calls while one is queued are
// covered by it.
func (b *helmHTTPBanner) request() {
	if !b.queued.CompareAndSwap(false, true) {
		return
	}
	b.sh.do(func() {
		b.queued.Store(false)
		if !helmChartsShown(b.sh.ui) {
			b.show(nil, nil)
			return
		}
		b.sh.dispatch("helm.repo.list", struct{}{}, b.show)
	})
}

// show renders the repositories of a helm.repo.list result that wait for the user's allowance. An
// error (a session without repositories) empties the banner.
func (b *helmHTTPBanner) show(res any, err error) {
	var l engine.HelmReposList
	if err == nil && res != nil {
		err = decode(res, &l)
	}
	var waiting []engine.HelmRepoInfo
	if err == nil {
		for _, r := range l.Repositories {
			if r.HTTPToken != "" && (r.HTTPCredentials == engine.HTTPCredentialsRequired || r.HTTPCredentials == engine.HTTPCredentialsChanged) {
				waiting = append(waiting, r)
			}
		}
	}
	b.repos = waiting
	rows := make([]fyne.CanvasObject, 0, len(waiting)+1)
	for _, r := range waiting {
		rows = append(rows, b.row(r))
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

// helmHTTPTitle and helmHTTPDetail are a row's two lines.
func helmHTTPTitle(r engine.HelmRepoInfo) string {
	if r.HTTPCredentials == engine.HTTPCredentialsChanged {
		return "Helm repository " + r.Name + " changed: allow its credentials over plain http again?"
	}
	return "Helm repository " + r.Name + " would send its user name and password over plain http"
}

func helmHTTPDetail(r engine.HelmRepoInfo) string {
	why := "unencrypted; Cogpit downloads nothing from it until you allow it"
	if r.HTTPCredentials == engine.HTTPCredentialsChanged {
		why = "its URL or user name changed since you allowed it; Cogpit downloads nothing from it until you allow it again"
	}
	return r.URL + " · " + why
}

// row is one repository: a tinted strip with an icon, two lines of text and Allow….
func (b *helmHTTPBanner) row(r engine.HelmRepoInfo) fyne.CanvasObject {
	title := newTextLine(helmHTTPTitle(r), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	title.truncate = true
	detail := newTextLine(helmHTTPDetail(r), colorMuted, theme.SizeNameText, fyne.TextStyle{})
	detail.truncate = true
	allow := newPlateButton("Allow…", theme.WarningIcon(), func() { b.confirm(r) })
	allow.Importance = widget.WarningImportance
	content := container.NewBorder(nil, nil,
		container.NewCenter(widget.NewIcon(themedIcon(theme.WarningIcon(), colorStatusWarning))),
		container.NewCenter(allow),
		container.NewVBox(title, detail))
	return container.NewStack(newBannerTint(colorStatusWarning), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content))
}

// helmHTTPAsk is the question's text, paragraph by paragraph: what crosses the network, who can
// read it, what the allowance binds, and that no agent can give it.
func helmHTTPAsk(r engine.HelmRepoInfo) []string {
	var out []string
	if r.HTTPCredentials == engine.HTTPCredentialsChanged {
		out = append(out, "You allowed this repository before, for another URL or user name.")
	}
	return append(out,
		"Helm's repositories file gives the repository "+r.Name+" a user name and password, and its URL is plain http, not https:",
		r.URL,
		"Cogpit would send them unencrypted with every download, the background refresh included: anyone on the network "+
			"path, a proxy included, can read them, and can change the index and the charts Cogpit downloads. "+
			"helm sends them anyway; Cogpit sends them only once you allow it.",
		"Your allowance is kept for this repository's name, URL and user name. If one of them changes in Helm's file, "+
			"Cogpit asks again; a new password does not. Agents cannot allow it.")
}

// confirm asks whether the repository's credentials may go over plain http and, on Allow, records
// it with the token the list showed: a repository that changed meanwhile is refused by the engine.
func (b *helmHTTPBanner) confirm(r engine.HelmRepoInfo) {
	if b.ask != nil {
		b.ask.Hide()
	}
	width := b.sh.app.Settings().Theme().Size(sizePickerWidth)
	paras := helmHTTPAsk(r)
	objs := make([]fyne.CanvasObject, 0, len(paras))
	for _, p := range paras {
		color := theme.ColorNameForeground
		if p == r.URL {
			color = colorMuted
		}
		objs = append(objs, newParagraph(p, color, width))
	}
	var d *dialog.CustomDialog
	allow := newPlateButton("Allow over http", theme.WarningIcon(), func() {
		d.Hide()
		b.sh.dispatch("helm.repo.allowHTTPCredentials", helmHTTPParams{Repositories: []helmHTTPItem{{Name: r.Name, Token: r.HTTPToken}}},
			func(_ any, err error) {
				if err != nil {
					b.sh.showError(err)
					b.request()
					return
				}
				b.sh.toast("info", "Helm repository "+r.Name+" allowed over plain http; its index downloads now")
			})
	})
	allow.Importance = widget.WarningImportance
	cancel := newPlateButton("Cancel", nil, func() { d.Hide() })
	d = dialog.NewCustomWithoutButtons("Send credentials over plain http?", container.NewVBox(objs...), b.sh.win)
	d.SetButtons([]fyne.CanvasObject{cancel, allow})
	d.SetOnClosed(func() {
		if b.ask == d {
			b.ask = nil
		}
	})
	b.ask = d
	b.sh.showDialog(d)
}
