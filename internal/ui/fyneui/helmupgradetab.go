package fyneui

import (
	"errors"
	"fmt"
	"reflect"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// helmUpgradeTab is Lens's "Helm Upgrade: <name>" dock tab (captured 2026-10-12): save and search
// icons, the chart and its version as text ("/<chart>  version: 0.1.0 (no repository)") beside a
// separate version picker, the line "Release <name> values.yaml, version …, on <namespace>
// namespace." with the release and the namespace as links, and the release's values in a YAML
// editor whose text lives in the engine (editor.get, editor.setText, as an Edit tab's). The search
// icon opens the editor's find widget at its bottom (findbar.go). Save sends the text, then
// upgrades the release with it (helm.release.upgrade with the tab id, the text revision the send
// made and the chart version the tab shows). The status line at the bottom is Cogpit's: what the
// values are based on, what Save does, progress and errors.
//
// Without a chart repository that lists the release's chart the picker offers the release's own
// version only, with Lens's "Select…" and "(no repository)" (captured). With one (2026-10-13;
// inferred, Lens with a repository is not captured) the chart reads "<repository>/<chart>", the
// version text loses "(no repository)", the picker offers the repository's versions with the
// release's own marked "(current)" and starts on the engine's choice, the newest when it is newer,
// and a second picker chooses the repository when several list the chart. A pick goes to the
// engine (helm.release.upgradeTab with the repository and version), whose tab is what the pickers
// show (update). A repository whose content nothing authenticates (plain http, or https without
// the certificate check) is marked beside the chart, and while Save downloads from it a line
// under the chart says what that means (H-41, A14; the engine's HelmUpgradeTab.Origin).
type helmUpgradeTab struct {
	sh   *shell
	tab  engine.DockTab
	pane *editorPane

	saveBtn     *tipButton
	searchBtn   *tipButton
	find        *findBar
	chartText   *textLine
	origin      *pill           // the repository's origin when nothing authenticates it (H-41, A14)
	originBox   *fyne.Container // the pill's place on the chart's line
	originNote  *textLine
	originRow   *fyne.Container // what the origin means, while Save downloads from it
	version     *selectField
	versionBox  *fyne.Container
	repo        *selectField
	repoBox     *fyne.Container
	versionText *textLine
	releaseLink *linkText
	nsLink      *linkText
	infoVersion *textLine
	spinner     *widget.Activity
	status      *textLine
	root        fyne.CanvasObject

	busy bool
	// labels maps the picker's options to the versions they name ("0.1.0 (current)" → "0.1.0").
	labels map[string]string
	// syncing is set while the pickers follow the engine, whose answer must not be sent back.
	syncing bool
	// loaded is the state the values were read at; showingBase says the status line shows it (not
	// a result), so that a new choice may rewrite it.
	loaded      engine.EditorState
	showingBase bool
}

// noRepository marks a version that comes from the release itself, not from a chart repository,
// as Lens writes it (captured 2026-10-12).
const noRepository = " (no repository)"

// currentVersion marks the release's own version among a repository's (inferred).
const currentVersion = " (current)"

// helmUpgradeTabParams mirror helm.release.upgradeTab's parameters.
type helmUpgradeTabParams struct {
	refParams
	Repository string `json:"repository,omitempty"`
	Version    string `json:"version,omitempty"`
}

func newHelmUpgradeTab(sh *shell, t engine.DockTab) *helmUpgradeTab {
	u := &helmUpgradeTab{sh: sh, tab: t, pane: newEditorPane(sh, t.ID)}
	name, namespace, ctx := "", "", t.Context
	if t.Ref != nil {
		name, namespace = t.Ref.Name, t.Ref.Namespace
		if t.Ref.Context != "" {
			ctx = t.Ref.Context
		}
	}

	u.saveBtn = newTipButton(sh, theme.DocumentSaveIcon(), "Save: upgrade the release with these values", u.save)
	u.searchBtn = newTipButton(sh, theme.SearchIcon(), "Find", u.toggleSearch)
	u.find = newFindBar(sh, u.pane.view)
	u.chartText = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	u.origin = newPill("", colorStatusWarning, true)
	u.originBox = container.NewCenter(u.origin) // hidden whole: a hidden pill in it would still take the line's gap
	u.originBox.Hide()
	u.originNote = newTextLine("", colorStatusWarning, theme.SizeNameText, fyne.TextStyle{})
	u.originNote.truncate = true
	u.originRow = container.NewBorder(nil, nil,
		container.NewCenter(widget.NewIcon(themedIcon(theme.WarningIcon(), colorStatusWarning))), nil, u.originNote)
	u.originRow.Hide()
	u.version = newSelectField(nil, nil)
	u.version.PlaceHolder = "Select…"
	u.version.OnChanged = u.pickVersion
	u.versionBox = box(fitSelect{}, u.version)
	u.repo = newSelectField(nil, nil)
	u.repo.OnChanged = u.pickRepository
	u.repoBox = box(fitSelect{}, u.repo)
	u.versionText = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})

	u.releaseLink = newLinkText(name, func() {
		sh.followLink(model.ObjectRef{Context: ctx, Kind: helm.ReleaseKind, Namespace: namespace, Name: name}, ctx, false)
	})
	u.nsLink = newLinkText(namespace, func() {
		sh.followLink(model.ObjectRef{Context: ctx, Kind: kindNamespaces, Name: namespace}, ctx, false)
	})
	u.infoVersion = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	u.infoVersion.Hide()
	u.spinner = widget.NewActivity()
	u.spinner.Start()
	plain := func(s string) fyne.CanvasObject {
		return newTextLine(s, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	}
	info := box(inlineLayout{}, plain("Release "), u.releaseLink, plain(" values.yaml, version "), u.infoVersion, u.spinner,
		plain(", on "), u.nsLink, plain(" namespace."))

	u.status = newTextLine("Loading…", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	u.status.truncate = true
	u.showChoice()

	chartLine := container.NewHBox(u.saveBtn, u.searchBtn, newDivider(true),
		container.NewCenter(u.chartText), u.originBox, container.NewCenter(u.versionText), u.repoBox, u.versionBox)
	header := container.NewStack(newSurface(colorDock), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewVBox(chartLine, u.originRow, info)))
	footer := container.NewStack(newSurface(colorDock), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, u.status))
	editor := container.NewBorder(nil, u.find.root, nil, nil,
		container.NewStack(newSurface(theme.ColorNameInputBackground), u.pane.view))
	u.root = container.NewBorder(container.NewVBox(header, newDivider(false)), container.NewVBox(newDivider(false), footer), nil, nil, editor)
	u.setBusy(true)
	u.pane.load(func(st engine.EditorState, err error) {
		u.setBusy(false)
		u.spinner.Stop()
		u.spinner.Hide()
		u.infoVersion.Show()
		if err != nil {
			u.setStatus("Could not open the values: "+errText(err), colorStatusError)
			return
		}
		u.showBase(st)
	})
	return u
}

// choice is the engine's choice of chart and version for the tab.
func (u *helmUpgradeTab) choice() engine.HelmUpgradeTab {
	if u.tab.Helm == nil {
		return engine.HelmUpgradeTab{}
	}
	return *u.tab.Helm
}

// showChoice shows the engine's choice: the chart (with its repository), the release's own
// version, the repository picker when several list the chart, and the version picker on the
// version the upgrade installs.
func (u *helmUpgradeTab) showChoice() {
	h := u.choice()
	own := h.ReleaseVersion
	if own == "" {
		own = h.Version
	}
	u.syncing = true
	defer func() { u.syncing = false }()
	if h.Repository == "" {
		u.chartText.SetText("/" + h.Chart)
		u.versionText.SetText("version: " + own + noRepository)
	} else {
		u.chartText.SetText(h.Repository + "/" + h.Chart)
		u.versionText.SetText("version: " + own)
	}
	// A repository nothing authenticates is marked beside the chart; when Save downloads from it,
	// the line under the chart says what that means: the upgrade's confirmation in the window
	// (H-41, A14).
	if mark := kinds.HelmOriginMark(h.Origin); mark != "" && h.Repository != "" {
		u.origin.Set(mark, colorStatusWarning)
		u.originBox.Show()
	} else {
		u.originBox.Hide()
	}
	if w := kinds.HelmOriginWarning(h.Repository, h.Origin); w != "" && h.Repository != "" && h.Version != own {
		u.originNote.SetText(w)
		u.originRow.Show()
	} else {
		u.originRow.Hide()
	}

	if len(h.Repositories) > 1 {
		u.repo.Options = h.Repositories
		u.repoBox.Layout = fitSelect{options: h.Repositories}
		u.repo.SetSelected(h.Repository)
		u.repoBox.Show()
	} else {
		u.repoBox.Hide()
	}
	u.repoBox.Refresh()

	u.labels = make(map[string]string, len(h.Versions))
	options := make([]string, 0, len(h.Versions))
	selected := ""
	for _, v := range h.Versions {
		label := v
		if h.Repository != "" && v == own {
			label += currentVersion
		}
		u.labels[label] = v
		options = append(options, label)
		if v == h.Version {
			selected = label
		}
	}
	u.version.Options = options
	u.versionBox.Layout = fitSelect{options: append([]string{u.version.PlaceHolder}, options...)}
	switch {
	case h.Repository != "" && selected != "":
		u.version.SetSelected(selected) // the engine's choice: the newest, or the user's pick
	case u.labels[u.version.Selected] == "":
		u.version.ClearSelected() // without a repository "Select…", as Lens shows it (captured)
	}
	u.version.Refresh()
	u.versionBox.Refresh()
	u.infoVersion.SetText(h.Version)
}

// update follows the engine's tab: a new choice of repository or version.
func (u *helmUpgradeTab) update(t engine.DockTab) {
	old := u.tab.Helm
	u.tab = t
	if t.Helm == nil || (old != nil && reflect.DeepEqual(*old, *t.Helm)) {
		return
	}
	u.showChoice()
	if u.showingBase {
		u.showBase(u.loaded)
	}
}

// pickVersion sends the user's pick to the engine; the pickers follow its answer (update).
func (u *helmUpgradeTab) pickVersion(label string) {
	h := u.choice()
	v := u.labels[label]
	if u.syncing || u.tab.Ref == nil || v == "" || v == h.Version {
		return
	}
	u.choose(helmUpgradeTabParams{refParams: refParams{Ref: *u.tab.Ref}, Repository: h.Repository, Version: v}, "Version "+v)
}

// pickRepository sends the user's pick of a repository; the engine chooses its version.
func (u *helmUpgradeTab) pickRepository(name string) {
	if u.syncing || u.tab.Ref == nil || name == "" || name == u.choice().Repository {
		return
	}
	u.choose(helmUpgradeTabParams{refParams: refParams{Ref: *u.tab.Ref}, Repository: name}, "Repository "+name)
}

func (u *helmUpgradeTab) choose(p helmUpgradeTabParams, what string) {
	u.sh.dispatch(upgradeTabCommand, p, func(_ any, err error) {
		if u.pane.closed {
			return
		}
		if err == nil {
			u.follow()
			return
		}
		u.showChoice() // back to the engine's choice
		u.setStatus(what+" was not chosen: "+errText(err), colorStatusError)
	})
}

// follow reads the tab from the window's UI state. The dock syncs its tabs when they come, go or
// move, not when a tab's choice changes (shell.go's dockEqual), so a choice the engine changed
// arrives here: after the user's pick, and with the tab's EventEditor for an agent's (remote).
func (u *helmUpgradeTab) follow() {
	if t, ok := u.sh.ui.DockTab(u.tab.ID); ok {
		u.update(t)
	}
}

// showBase says which revision the values are based on, whether the release has moved on, and
// what Save does: a change of the chart version is named.
func (u *helmUpgradeTab) showBase(st engine.EditorState) {
	u.loaded = st
	switch {
	case st.Missing:
		u.setStatus("The release no longer exists.", colorStatusWarning)
	case st.Stale:
		u.setStatus(fmt.Sprintf("The release moved to revision %d since these values (revision %d) were read: Save is refused.", st.CachedRevision, st.BaseRevision), colorStatusWarning)
	default:
		text := fmt.Sprintf("The user-supplied values of revision %d. Save upgrades the release with them.", st.BaseRevision)
		if h := u.choice(); h.Repository != "" && h.ReleaseVersion != "" && h.Version != h.ReleaseVersion {
			to := "to"
			if c, ok := helm.CompareVersions(h.Version, h.ReleaseVersion); ok && c < 0 {
				to = "back to the older"
			}
			text = fmt.Sprintf("The user-supplied values of revision %d. Save upgrades the release with them from chart version %s %s %s, downloaded from %s.",
				st.BaseRevision, h.ReleaseVersion, to, h.Version, h.Repository)
		}
		u.setStatus(text, colorMuted)
	}
	u.showingBase = true
}

func (u *helmUpgradeTab) setStatus(text string, c fyne.ThemeColorName) {
	u.showingBase = false
	u.status.SetText(text)
	u.status.SetColor(c)
}

func (u *helmUpgradeTab) setBusy(b bool) {
	u.busy = b
	if b {
		u.saveBtn.Disable()
	} else {
		u.saveBtn.Enable()
	}
}

// saveParams are the upgrade's parameters: the tab at the text revision the send made, and the
// chart version the tab shows (with the repository it is downloaded from when it is not the
// release's own), so that the upgrade installs what the user sees.
func (u *helmUpgradeTab) saveParams(textRevision uint64) helmUpgradeParams {
	p := helmUpgradeParams{refParams: refParams{Ref: *u.tab.Ref}, Tab: u.tab.ID, TextRevision: textRevision}
	if h := u.choice(); h.Version != "" {
		p.Version = h.Version
		if h.Repository != "" && h.ReleaseVersion != "" && h.Version != h.ReleaseVersion {
			p.Repository = h.Repository
		}
	}
	return p
}

// save hands the text to the engine, then upgrades the release with the tab's values: in that
// order, so that the upgrade never runs on text older than what the user sees.
func (u *helmUpgradeTab) save() {
	if u.busy || u.tab.Ref == nil {
		return
	}
	text := u.pane.view.Text()
	u.setBusy(true)
	u.setStatus("Upgrading…", colorMuted)
	u.sh.dispatch("editor.setText", editorTextParams{ID: u.tab.ID, YAML: text}, func(res any, err error) {
		if u.pane.closed {
			return
		}
		if err != nil {
			u.setBusy(false)
			u.setStatus("Not upgraded: "+errText(err), colorStatusError)
			return
		}
		var tr struct {
			Revision uint64 `json:"revision"`
		}
		if decode(res, &tr) == nil && tr.Revision > u.pane.pushed {
			u.pane.pushed = tr.Revision
		}
		u.pane.dirty = u.pane.view.Text() != text
		u.sh.dispatch("helm.release.upgrade", u.saveParams(tr.Revision), func(res any, err error) {
			if u.pane.closed {
				return
			}
			u.setBusy(false)
			switch {
			case errors.Is(err, engine.ErrDeclined):
				u.setStatus("Not upgraded.", colorMuted)
			case err != nil:
				u.setStatus("Not upgraded: "+errText(err), colorStatusError)
			default:
				var r struct {
					Revision int `json:"revision"`
				}
				_ = decode(res, &r)
				u.setStatus(fmt.Sprintf("Upgraded to revision %d. Save upgrades it again with these values.", r.Revision), colorStatusOK)
			}
		})
	})
}

// toggleSearch opens the editor's find widget, or closes it.
func (u *helmUpgradeTab) toggleSearch() {
	if u.find.visible() {
		u.find.close()
		return
	}
	u.find.open()
}

// fitSelect sizes a select as wide as its longest option, which widget.Select would cut short.
type fitSelect struct{ options []string }

func (l fitSelect) MinSize(objs []fyne.CanvasObject) fyne.Size {
	th := fyne.CurrentApp().Settings().Theme()
	var w, h float32
	for _, o := range objs {
		ms := o.MinSize()
		w, h = fyne.Max(w, ms.Width), fyne.Max(h, ms.Height)
	}
	for _, s := range l.options {
		text := fyne.MeasureText(s, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width
		w = fyne.Max(w, text+th.Size(theme.SizeNameInlineIcon)+4*th.Size(theme.SizeNameInnerPadding))
	}
	return fyne.NewSize(w, h)
}

func (l fitSelect) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

// remote follows the tab's EventEditor: text from elsewhere (an agent's editor.setText), or a new
// choice of chart version (helm.release.upgradeTab, an upgrade's rebase).
func (u *helmUpgradeTab) remote(rev uint64) {
	u.pane.remote(rev)
	u.follow()
}

func (u *helmUpgradeTab) close() {
	u.spinner.Stop()
	u.pane.close()
}

// inlineLayout lays its visible objects out on one line, each as wide as its text or content: a
// sentence of text, links and a spinner. Links ask for no width of their own (they shorten to fit),
// so their text is measured here.
type inlineLayout struct{}

func (inlineLayout) natural(o fyne.CanvasObject) fyne.Size {
	if l, ok := o.(*linkText); ok {
		th := fyne.CurrentApp().Settings().Theme()
		return fyne.MeasureText(l.text, th.Size(l.size), fyne.TextStyle{})
	}
	return o.MinSize()
}

func (l inlineLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objs {
		if o.Visible() {
			ms := l.natural(o)
			w += ms.Width
			h = fyne.Max(h, ms.Height)
		}
	}
	return fyne.NewSize(w, h)
}

func (l inlineLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	var x float32
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		ms := l.natural(o)
		o.Move(fyne.NewPos(x, (size.Height-ms.Height)/2))
		o.Resize(ms)
		x += ms.Width
	}
}
