package fyneui

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// Preferences › Kubernetes › Program folders (D-120, the owner's decision of 2026-10-23;
// engine/programfolders.go): the folders Cogpit looks in for a credential plugin's programs after
// PATH and its built-in folders. Each folder is a row with its path and a remove button; Add
// Folder… opens the system's folder panel (D-72), hidden folders listed so that ~/.krew/bin can be
// reached, and adds the folder chosen (preferences.addProgramFolder, which only this window may
// run). A refusal (a folder every user may write to, one kept already) shows under the setting.

const programFoldersSetting = "programFolders"

// programFoldersHelp is the setting's help line.
const programFoldersHelp = "Where Cogpit also looks for the programs a kubeconfig's login runs, such as kubelogin or krew's " +
	"kubectl-oidc_login, after PATH and its own folders (Homebrew's, krew's ~/.krew/bin, go install's and others): " +
	"a program found earlier is never replaced. Used from the next login on."

// programFoldersEmpty is the list's line while it has no folder.
const programFoldersEmpty = "None: Cogpit looks in PATH and its own folders."

// programFolderPanel asks for a program folder. The bookmark is read-only: Cogpit reads and runs
// what is in the folder, never writes there.
var programFolderPanel = nativedialog.OpenOptions{Title: "Add a Program Folder",
	Message: "Choose a folder that holds a login program or a kubectl plugin, such as ~/.krew/bin (⌘⇧. shows hidden folders).",
	Prompt:  "Add", Folders: true, ShowHidden: true, Bookmark: nativedialog.ReadOnly}

// pickProgramFolder asks for a folder in the system's panel, else in Fyne's dialog.
func (p *settingsPage) pickProgramFolder(done func(path string)) {
	sh := p.sh
	if sh.files.open(programFolderPanel, func(picks []nativedialog.Pick) {
		sh.keepAccess(nativedialog.UseFolder, nativedialog.ReadOnly, picks)
		done(picks[0].Path)
	}) {
		return
	}
	p.pickFolderWithDialog(done)
}

// programFoldersSetting returns the Program folders setting; it chains p.sync, which shows the
// preference's folders in it.
func (p *settingsPage) programFoldersSetting() fyne.CanvasObject {
	list := container.NewVBox()
	p.controls["programFolderList"] = list
	add := newPlateButton("Add Folder…", theme.FolderOpenIcon(), func() {
		p.pickProgramFolder(func(path string) {
			p.sh.dispatchInOrder("preferences.addProgramFolder", map[string]any{"path": path}, p.settled(programFoldersSetting))
		})
	})
	var shown []string
	built := false
	outer := p.sync
	p.sync = func() {
		if outer != nil {
			outer()
		}
		dirs := p.prefs().ProgramFolders
		if built && slices.Equal(dirs, shown) {
			return
		}
		built, shown = true, slices.Clone(dirs)
		list.Objects = p.programFolderRows(dirs)
		list.Refresh()
	}
	return p.setting(programFoldersSetting, "Program folders", programFoldersHelp,
		container.NewVBox(list, container.NewHBox(add)))
}

// programFolderRows are the list's rows: each folder with its remove button, or the line that says
// there is none.
func (p *settingsPage) programFolderRows(dirs []string) []fyne.CanvasObject {
	if len(dirs) == 0 {
		return []fyne.CanvasObject{newTextLine(programFoldersEmpty, colorMuted, theme.SizeNameText, fyne.TextStyle{})}
	}
	rows := make([]fyne.CanvasObject, 0, len(dirs))
	for _, dir := range dirs {
		name := newTextLine(dir, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
		name.truncate = true
		remove := newTipButton(p.sh, theme.DeleteIcon(), "Remove this folder", func() {
			p.sh.dispatchInOrder("preferences.removeProgramFolder", map[string]any{"path": dir}, p.settled(programFoldersSetting))
		})
		rows = append(rows, container.NewBorder(nil, nil, nil, remove, container.NewVBox(layout.NewSpacer(), name, layout.NewSpacer())))
	}
	return rows
}
