package fyneui

import (
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The kubeconfig sources in the GUI (G2): the Cluster menu's, the welcome page's, the navigator
// header's "+" and the section root's "Add Kubeconfigs" (one dialog for files and folders,
// syncpicker.go), the sources dialog with its Remove confirmation, the log of a connect (the
// welcome page's warning icon, a section row's Show Connection Updates…), and the dialog that shows
// a credential plugin before its first run (trust-boundaries H-1). Every change goes through the
// engine's kubeconfig commands, which only the GUI may run; the dialogs here are where the user
// does it. Lens's own dialogs are captured only from the outside (the system file dialog, the
// "Connection Updates" log): the layout is Cogpit's, the labels follow the captures.

// Result types of the kubeconfig commands, mirroring the engine's JSON.
type (
	sourceStatus struct {
		ID       string   `json:"id"`
		Kind     string   `json:"kind"`
		Path     string   `json:"path"`
		Files    []string `json:"files"`
		Contexts int      `json:"contexts"`
		Skipped  int      `json:"skipped"`
		Default  bool     `json:"default"`
		Session  bool     `json:"session"`
		Error    string   `json:"error"`
	}
	sourceAdded struct {
		Source       sourceStatus `json:"source"`
		Contexts     []string     `json:"contexts"`
		Skipped      int          `json:"skipped"`
		SkippedFiles []string     `json:"skippedFiles"`
	}
	pathParams struct {
		Path string `json:"path"`
	}
	sourceParams struct {
		Source string `json:"source"`
	}
)

// Display limits for text from a kubeconfig: one line each, cut with an ellipsis.
const (
	maxShownPath = 400
	maxShownArg  = 300
	maxShownArgs = 40
)

// kubeconfigUI holds the kubeconfig dialogs of one shell.
type kubeconfigUI struct {
	sh *shell
	// pick asks for files and folders, one dialog for both, and calls done with each path and
	// whether it is a folder: the system's panel on macOS, else the sync picker (pickFiles,
	// filepicker_choosers.go); a recorder in tests.
	pick func(done func(path string, folder bool))
	// startDir is the folder the Sync panel and the sync picker open in: the home directory in
	// the app, a temporary directory in tests.
	startDir func() string

	approval   *approvalDialog // the open approval dialog; main goroutine only
	sources    *sourcesDialog  // the open sources dialog; main goroutine only
	syncing    *syncPicker     // the open sync picker; main goroutine only
	connectLog *connectLogDialog
	// noClustersDialog says that a source just synced offers no cluster; main goroutine only.
	noClustersDialog *dialog.ConfirmDialog
}

func newKubeconfigUI(sh *shell) *kubeconfigUI {
	k := &kubeconfigUI{sh: sh, startDir: defaultStartFolder}
	k.pick = k.pickFiles
	return k
}

// menuItems are the Cluster menu's kubeconfig items.
func (k *kubeconfigUI) menuItems() []*fyne.MenuItem {
	return []*fyne.MenuItem{
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Sync Files and Folders…", k.addKubeconfigs),
		fyne.NewMenuItem(sourcesLabel, k.showSources),
	}
}

// addKubeconfigs asks for kubeconfig files and folders of them, in one dialog, and syncs each.
func (k *kubeconfigUI) addKubeconfigs() {
	k.pick(func(path string, folder bool) {
		id := "kubeconfig.addFile"
		if folder {
			id = "kubeconfig.addFolder"
		}
		k.addPath(id, path)
	})
}

// addPath adds a source; several picked at once are added in the order chosen, so that their ids
// follow it (a second "dev.yaml" becomes "dev.yaml (2)").
func (k *kubeconfigUI) addPath(id, path string) {
	k.sh.dispatchInOrder(id, pathParams{Path: path}, func(res any, err error) {
		if err != nil {
			k.sh.showError(err)
			return
		}
		k.announce(res)
	})
}

// announce tells the user what a new source offers and refreshes what lists contexts.
func (k *kubeconfigUI) announce(res any) {
	var a sourceAdded
	if err := decode(res, &a); err != nil {
		return
	}
	n := len(a.Contexts)
	if n == 0 {
		k.noClusters(a)
		k.contextsChanged()
		return
	}
	k.sh.toast("info", fmt.Sprintf("Synced %s: %d %s", kubeconfig.DisplayText(a.Source.ID, maxShownPath), n, nounFor(n, "context", "contexts")))
	k.contextsChanged()
}

// noClusters tells the user that a source they just synced offers no cluster, which files it
// found and why each was skipped, and offers to remove it again: a toast saying "0 contexts" was
// easy to miss (owner, 2026-10-12).
func (k *kubeconfigUI) noClusters(a sourceAdded) {
	width := k.sh.app.Settings().Theme().Size(sizePickerWidth)
	path := kubeconfig.DisplayText(a.Source.Path, maxShownPath)
	var msg string
	switch {
	case a.Source.Kind != "folder":
		msg = path + " defines no cluster context. Cogpit keeps syncing it and offers its contexts once it has some."
	case a.Skipped == 0:
		msg = "The folder " + path + " holds no file Cogpit could read as a kubeconfig. Cogpit keeps syncing it and offers " +
			"the contexts of kubeconfig files that appear in it (it does not look into its subfolders)."
	default:
		msg = fmt.Sprintf("None of the %d %s in the folder %s defines a cluster context, so no cluster was added. Cogpit keeps "+
			"syncing it (not its subfolders) and offers the contexts of kubeconfig files that appear in it.",
			a.Skipped, nounFor(a.Skipped, "file", "files"), path)
	}
	content := container.NewVBox(newParagraph(msg, theme.ColorNameForeground, width))
	if len(a.SkippedFiles) > 0 {
		lines := make([]string, 0, len(a.SkippedFiles)+1)
		for _, f := range a.SkippedFiles {
			lines = append(lines, kubeconfig.DisplayText(f, maxShownPath))
		}
		if more := a.Skipped - len(a.SkippedFiles); more > 0 {
			lines = append(lines, fmt.Sprintf("and %d more", more))
		}
		content.Add(newSpace(theme.SizeNamePadding))
		content.Add(newParagraph(strings.Join(lines, "\n"), colorMuted, width))
	}
	id := a.Source.ID
	d := dialog.NewCustomConfirm("No clusters found", "Remove", "Keep Syncing", content, func(remove bool) {
		if !remove {
			return
		}
		k.sh.dispatch("kubeconfig.remove", sourceParams{Source: id}, func(_ any, err error) {
			if err != nil {
				k.sh.showError(err)
			}
			k.contextsChanged()
		})
	}, k.sh.win)
	k.noClustersDialog = d
	d.Show()
}

// contextsChanged refreshes the sources dialog if it is open; the navigator's section and the
// welcome page follow the contexts themselves.
func (k *kubeconfigUI) contextsChanged() {
	if k.sources != nil {
		k.sources.load()
	}
}

func nounFor(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// errorText is the user-facing text of a dispatch error.
func errorText(err error) string {
	var dis *engine.DisabledError
	if errors.As(err, &dis) {
		return dis.Reason
	}
	return err.Error()
}

// sourcesDialog lists the kubeconfig sources with a Remove button each.
type sourcesDialog struct {
	k       *kubeconfigUI
	d       *dialog.CustomDialog
	list    *fyne.Container
	status  *textLine
	removeB map[string]*widget.Button // by source id, for tests
	rows    []sourceStatus
	// removing is the open Remove confirmation, for tests.
	removing *dialog.ConfirmDialog
}

// showSources opens the sources dialog.
func (k *kubeconfigUI) showSources() {
	if k.sources != nil {
		k.sources.load()
		return
	}
	sd := &sourcesDialog{k: k, list: container.NewVBox(), removeB: map[string]*widget.Button{}}
	sd.status = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	sd.status.truncate = true
	intro := newParagraph("Kubeconfig files and folders Cogpit syncs: it reads them, follows their changes and never "+
		"writes them. Removing one removes only Cogpit's link to it. Your ~/.kube folder is synced like any other; "+
		"the files $KUBECONFIG names are offered for this run and are removed for this run only.", colorMuted, k.sh.app.Settings().Theme().Size(sizePickerWidth))
	listBg := newSurface(theme.ColorNameInputBackground)
	listBg.radius, listBg.stroke = theme.SizeNameInputRadius, theme.ColorNameInputBorder
	list := container.NewStack(listBg, inset(theme.SizeNamePadding, theme.SizeNamePadding, container.NewVScroll(sd.list)))
	content := sized(sizePickerWidth, sizePickerHeight, container.NewBorder(
		container.NewVBox(intro, newSpace(theme.SizeNamePadding)), sd.status, nil, nil, list))
	sd.d = dialog.NewCustomWithoutButtons("Kubeconfig Sources", content, k.sh.win)
	addB := widget.NewButtonWithIcon("Sync Files and Folders…", theme.ContentAddIcon(), k.addKubeconfigs)
	closeB := widget.NewButton("Close", nil)
	sd.d.SetButtons([]fyne.CanvasObject{addB, closeB})
	closeB.OnTapped = sd.d.Hide
	sd.d.SetOnClosed(func() {
		if k.sources == sd {
			k.sources = nil
		}
	})
	k.sources = sd
	sd.d.Show()
	sd.load()
}

// load reads the sources off the main goroutine and shows them.
func (sd *sourcesDialog) load() {
	sd.k.sh.dispatch("kubeconfig.sources", struct{}{}, func(res any, err error) {
		if err != nil {
			sd.status.SetColor(colorStatusError)
			sd.status.SetText(errorText(err))
			return
		}
		var out struct {
			Sources []sourceStatus `json:"sources"`
		}
		if err := decode(res, &out); err != nil {
			sd.status.SetColor(colorStatusError)
			sd.status.SetText("Unexpected result: " + err.Error())
			return
		}
		sd.rows = out.Sources
		sd.render()
	})
}

func (sd *sourcesDialog) render() {
	th := sd.k.sh.app.Settings().Theme()
	width := th.Size(sizePickerWidth) - 8*th.Size(theme.SizeNamePadding)
	sd.removeB = map[string]*widget.Button{}
	objs := make([]fyne.CanvasObject, 0, len(sd.rows))
	for _, s := range sd.rows {
		src := s
		title := newTextLine(kubeconfig.DisplayText(src.ID, maxShownPath), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
		kind := newPill(sourceKindLabel(src.Kind), colorAccent, true)
		badges := container.NewHBox(title, kind)
		if src.Default {
			badges.Add(newPill("default", colorMuted, true))
		}
		if src.Session {
			badges.Add(newPill("this run", colorMuted, true))
		}
		path := newTextLine(kubeconfig.DisplayText(src.Path, maxShownPath), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
		path.truncate = true
		remove := widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() { sd.confirmRemove(src) })
		remove.Importance = widget.LowImportance
		sd.removeB[src.ID] = remove
		var info fyne.CanvasObject
		if src.Error != "" { // wrapped beside the button, never wider than the list
			info = newParagraph(src.Error, colorStatusError, width-remove.MinSize().Width-2*th.Size(theme.SizeNamePadding))
		} else {
			info = newTextLine(sourceSummary(src), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
		}
		row := inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, container.NewBorder(nil, nil, nil,
			container.NewCenter(remove), container.NewVBox(badges, path, info)))
		objs = append(objs, row)
	}
	if len(objs) == 0 {
		objs = append(objs, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
			newTextLine("No sources added yet.", colorMuted, theme.SizeNameText, fyne.TextStyle{})))
	}
	sd.list.Objects = objs
	sd.list.Refresh()
	sd.status.SetColor(colorMuted)
	sd.status.SetText(fmt.Sprintf("%d %s", len(sd.rows), nounFor(len(sd.rows), "source", "sources")))
}

func sourceKindLabel(kind string) string {
	switch kind {
	case string(engine.SourceFolder):
		return "sync folder"
	}
	return "file"
}

func sourceSummary(s sourceStatus) string {
	parts := []string{fmt.Sprintf("%d %s", s.Contexts, nounFor(s.Contexts, "context", "contexts"))}
	if s.Kind == string(engine.SourceFolder) {
		parts = append([]string{fmt.Sprintf("%d %s", len(s.Files), nounFor(len(s.Files), "kubeconfig", "kubeconfigs"))}, parts...)
		if s.Skipped > 0 {
			parts = append(parts, fmt.Sprintf("%d other %s skipped", s.Skipped, nounFor(s.Skipped, "file", "files")))
		}
	}
	return strings.Join(parts, " · ")
}

// removeText is what the Remove confirmation says: the path, that only Cogpit's link goes and
// the files stay on disk, and what else follows.
func removeText(src sourceStatus) (title, msg string) {
	path := kubeconfig.DisplayText(src.Path, maxShownPath)
	title = "Remove " + kubeconfig.DisplayText(src.ID, maxShownPath) + "?"
	if src.Kind == string(engine.SourceFolder) {
		msg = "Cogpit stops syncing the folder\n\n" + path + "\n\nOnly Cogpit's link to the folder is removed. The folder and " +
			"its files stay on your disk, unchanged. Their contexts are no longer offered, the login programs you allowed " +
			"for them are forgotten, and a connection made from one of them is closed."
	} else {
		msg = "Cogpit stops syncing the file\n\n" + path + "\n\nOnly Cogpit's link to the file is removed. The file stays on " +
			"your disk, unchanged. Its contexts are no longer offered, the login programs you allowed for it are forgotten, " +
			"and a connection made from it is closed."
	}
	if src.Default {
		msg += " Sync Files and Folders adds it again."
	}
	if src.Session {
		msg = "Cogpit stops offering the file\n\n" + path + "\n\nOnly for this run: the file is named by --kubeconfig or $KUBECONFIG " +
			"and is offered again at the next start. The file stays on your disk, unchanged, and the login programs you allowed " +
			"for it are remembered. Its contexts are no longer offered, and a connection made from it is closed."
	}
	return title, msg
}

// confirmRemove asks before removing a source.
func (sd *sourcesDialog) confirmRemove(src sourceStatus) {
	title, msg := removeText(src)
	width := sd.k.sh.app.Settings().Theme().Size(sizePickerWidth)
	d := dialog.NewCustomConfirm(title, "Remove", "Cancel", newParagraph(msg, theme.ColorNameForeground, width), func(ok bool) {
		sd.removing = nil
		if ok {
			sd.remove(src.ID)
		}
	}, sd.k.sh.win)
	sd.removing = d
	d.Show()
}

func (sd *sourcesDialog) remove(id string) {
	sd.k.sh.dispatch("kubeconfig.remove", sourceParams{Source: id}, func(_ any, err error) {
		if err != nil {
			sd.k.sh.showError(err)
		}
		sd.k.contextsChanged()
	})
}
