package fyneui

import (
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The one dialog that adds kubeconfigs (G2): "Sync Files and Folders". Lens opens the system's file
// dialog titled "Sync file(s)" or "Sync Files and Folders" with the button "Sync", which takes
// files and folders together (captured 2026-10-12). Fyne's own file dialogs choose either a file
// or a folder, so this is a small browser of Cogpit's own with the same semantic: select a file or
// a folder in the list and press Sync, or press Sync with nothing selected to sync the folder
// shown. Hidden files are listed (kubeconfigs may be named .kubeconfig), and the status line names
// the path that will be synced. The dialog reads directory listings only, never file contents.
// Since 2026-10-16 (D-72) macOS shows its own open panel instead (pickFiles,
// filepicker_choosers.go): this browser is the fallback where the system has no panel (Windows and
// Linux until M11, Fyne's test driver).

// maxSyncPickerEntries bounds the rows of one folder: a folder with more is not a folder of
// kubeconfigs, and building thousands of rows would stall the window.
const maxSyncPickerEntries = 2000

// syncEntry is one row of the sync picker.
type syncEntry struct {
	name string
	path string
	dir  bool
}

// syncPicker is the open "Sync Files and Folders" dialog.
type syncPicker struct {
	k    *kubeconfigUI
	d    *dialog.CustomDialog
	done func(path string, folder bool)

	dir      string
	entries  []syncEntry
	total    int // entries in the folder, before the cap
	selected string
	seq      int // the newest listing; older answers are dropped

	pathEntry *fieldEntry
	upB       *textButton
	syncB     *textButton
	cancelB   *textButton
	list      *fyne.Container
	status    *textLine
	rows      map[string]*fsRow // by entry name, for tests
}

// startFolder is the folder the dialog opens in: the user's home directory.
func defaultStartFolder() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return string(filepath.Separator)
}

// pickWithDialog opens the sync picker and calls done with the chosen path and whether it is a
// folder. Cancelling calls nothing.
func (k *kubeconfigUI) pickWithDialog(done func(path string, folder bool)) {
	if k.syncing != nil {
		k.syncing.d.Hide()
	}
	th := k.sh.app.Settings().Theme()
	sp := &syncPicker{k: k, done: done, rows: map[string]*fsRow{}}
	sp.pathEntry = newFieldEntry() // Escape in it cancels the dialog (escape.go)
	sp.pathEntry.SetPlaceHolder("Folder or file path…")
	sp.pathEntry.OnSubmitted = sp.goToTyped
	sp.pathEntry.OnChanged = func(string) { sp.renderStatus() }
	sp.upB = newPlateButton("", theme.MoveUpIcon(), sp.up)
	sp.list = container.NewVBox()
	sp.status = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	sp.status.truncate = true
	intro := newParagraph("Select a kubeconfig file or a folder of kubeconfigs and press Sync. Cogpit reads them, "+
		"follows their changes and never writes them.", colorMuted, th.Size(sizePickerWidth))

	listBg := newSurface(theme.ColorNameInputBackground)
	listBg.radius, listBg.stroke = theme.SizeNameInputRadius, colorHairline
	list := container.NewStack(listBg, inset(theme.SizeNamePadding, theme.SizeNamePadding, container.NewVScroll(sp.list)))
	top := container.NewVBox(intro, newSpace(theme.SizeNamePadding), container.NewBorder(nil, nil, sp.upB, nil, sp.pathEntry),
		newSpace(theme.SizeNamePadding))
	content := sized(sizePickerWidth, sizePickerHeight, container.NewBorder(top, sp.status, nil, nil, list))

	sp.d = dialog.NewCustomWithoutButtons("Sync Files and Folders", content, k.sh.win)
	sp.cancelB = newPlateButton("Cancel", nil, sp.d.Hide)
	sp.syncB = newPlateButton("Sync", theme.ConfirmIcon(), sp.sync)
	sp.syncB.Importance = widget.HighImportance
	sp.d.SetButtons([]fyne.CanvasObject{sp.cancelB, sp.syncB})
	sp.d.SetOnClosed(func() {
		if k.syncing == sp {
			k.syncing = nil
		}
	})
	k.syncing = sp
	k.sh.showDialog(sp.d)
	sp.goTo(k.startDir(), "")
}

// goTo lists dir off the main goroutine and shows it, selecting the entry called sel if there is one.
func (sp *syncPicker) goTo(dir, sel string) {
	dir = filepath.Clean(dir)
	sp.seq++
	seq := sp.seq
	sp.dir, sp.selected = dir, ""
	sp.pathEntry.SetText(dir)
	sp.status.SetColor(colorMuted)
	sp.status.SetText("Reading " + kubeconfig.DisplayText(dir, maxShownPath) + "…")
	sh := sp.k.sh
	go func() {
		defer sh.recoverTo("listing a folder")
		entries, total, err := listSyncFolder(dir)
		sh.do(func() {
			if seq != sp.seq || sp.k.syncing != sp {
				return
			}
			if err != nil {
				sp.entries, sp.total = nil, 0
				sp.render()
				sp.status.SetColor(colorStatusError)
				sp.status.SetText(errorText(err))
				return
			}
			sp.entries, sp.total = entries, total
			if sel != "" {
				for _, e := range entries {
					if e.name == sel {
						sp.selected = e.path
					}
				}
			}
			sp.render()
		})
	}()
}

// listSyncFolder lists dir: folders first, then files, each by name; at most maxSyncPickerEntries.
func listSyncFolder(dir string) (entries []syncEntry, total int, err error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	total = len(des)
	for _, de := range des {
		e := syncEntry{name: de.Name(), path: filepath.Join(dir, de.Name()), dir: de.IsDir()}
		if de.Type()&os.ModeSymlink != 0 { // a link counts as what it points to
			if fi, err := os.Stat(e.path); err == nil {
				e.dir = fi.IsDir()
			}
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].dir != entries[j].dir {
			return entries[i].dir
		}
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
	if len(entries) > maxSyncPickerEntries {
		entries = entries[:maxSyncPickerEntries]
	}
	return entries, total, nil
}

// expandPath is a typed path as the file system knows it: trimmed, "~" for the home directory,
// cleaned; "" for nothing typed.
func expandPath(text string) string {
	p := strings.TrimSpace(text)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") && (len(p) == 1 || p[1] == '/' || p[1] == filepath.Separator) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	return filepath.Clean(p)
}

// typed returns the path typed into the entry when it is not the folder shown: one the user typed
// or pasted and has not opened with Enter.
func (sp *syncPicker) typed() string {
	if p := expandPath(sp.pathEntry.Text); p != "" && p != sp.dir {
		return p
	}
	return ""
}

// goToTyped follows the path typed in the entry: a folder is opened, a file opens its folder with
// the file selected.
func (sp *syncPicker) goToTyped(text string) {
	p := expandPath(text)
	if p == "" {
		return
	}
	sh := sp.k.sh
	go func() {
		defer sh.recoverTo("following a typed path")
		fi, err := os.Stat(p)
		sh.do(func() {
			switch {
			case sp.k.syncing != sp:
			case err != nil:
				sp.status.SetColor(colorStatusError)
				sp.status.SetText(errorText(err))
			case fi.IsDir():
				sp.goTo(p, "")
			default:
				sp.goTo(filepath.Dir(p), filepath.Base(p))
			}
		})
	}()
}

// up opens the parent folder.
func (sp *syncPicker) up() {
	if parent := filepath.Dir(sp.dir); parent != sp.dir {
		sp.goTo(parent, filepath.Base(sp.dir))
	}
}

// target is what Sync adds: the selected entry, else the folder shown. A typed path wins over both
// (sync looks it up first).
func (sp *syncPicker) target() (path string, folder bool) {
	for _, e := range sp.entries {
		if e.path == sp.selected {
			return e.path, e.dir
		}
	}
	return sp.dir, true
}

// sync adds the target and closes the dialog. A path typed or pasted into the entry and not opened
// with Enter is what the user means: it wins over the folder shown (Sync used to add the folder
// the dialog opened in, the home directory, owner 2026-10-12). One that does not exist is said in
// the status line, and the dialog stays.
func (sp *syncPicker) sync() {
	typed := sp.typed()
	if typed == "" {
		path, folder := sp.target()
		sp.d.Hide()
		sp.done(path, folder)
		return
	}
	sh := sp.k.sh
	go func() {
		defer sh.recoverTo("syncing a typed path")
		fi, err := os.Stat(typed)
		sh.do(func() {
			switch {
			case sp.k.syncing != sp:
			case err != nil:
				sp.status.SetColor(colorStatusError)
				sp.status.SetText(errorText(err))
			default:
				sp.d.Hide()
				sp.done(typed, fi.IsDir())
			}
		})
	}()
}

func (sp *syncPicker) render() {
	rows := make([]fyne.CanvasObject, 0, len(sp.entries)+1)
	sp.rows = map[string]*fsRow{}
	for _, e := range sp.entries {
		r := newFSRow(e, e.path == sp.selected,
			func() {
				sp.selected = e.path
				sp.pathEntry.SetText(sp.dir) // a tap chooses from the list: a typed path no longer counts
				sp.render()
			},
			func() {
				if e.dir {
					sp.goTo(e.path, "")
					return
				}
				sp.selected = e.path
				sp.sync()
			})
		sp.rows[e.name] = r
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		rows = append(rows, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
			newTextLine("This folder is empty.", colorMuted, theme.SizeNameText, fyne.TextStyle{})))
	}
	sp.list.Objects = rows
	sp.list.Refresh()
	sp.renderStatus()
}

// renderStatus says what Sync adds: a typed path, else the selected entry or the folder shown.
func (sp *syncPicker) renderStatus() {
	if sp.status == nil {
		return
	}
	if typed := sp.typed(); typed != "" {
		sp.status.SetColor(colorMuted)
		sp.status.SetText("Sync: " + kubeconfig.DisplayText(typed, maxShownPath) + " (Enter opens it)")
		return
	}
	path, folder := sp.target()
	what := "file"
	if folder {
		what = "folder"
	}
	text := "Sync " + what + ": " + kubeconfig.DisplayText(path, maxShownPath)
	if sp.total > len(sp.entries) {
		text = "Showing the first " + strconv.Itoa(len(sp.entries)) + " of " + strconv.Itoa(sp.total) + " entries. " + text
	}
	sp.status.SetColor(colorMuted)
	sp.status.SetText(text)
}

// fsRow is one file or folder in the sync picker: a tap selects it, a double tap opens a folder or
// syncs a file.
type fsRow struct {
	widget.BaseWidget
	selected, hovered bool
	onTap, onDouble   func()
	entry             syncEntry

	bg      *canvas.Rectangle
	content *fyne.Container
}

func newFSRow(e syncEntry, selected bool, onTap, onDouble func()) *fsRow {
	r := &fsRow{entry: e, selected: selected, onTap: onTap, onDouble: onDouble, bg: canvas.NewRectangle(color.Transparent)}
	icon := theme.FileIcon()
	if e.dir {
		icon = theme.FolderIcon()
	}
	name := newTextLine(kubeconfig.DisplayText(e.name, maxShownArg), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: e.dir})
	name.truncate = true
	r.content = inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewBorder(nil, nil, widget.NewIcon(icon), nil, name))
	r.ExtendBaseWidget(r)
	return r
}

// Tapped implements fyne.Tappable.
func (r *fsRow) Tapped(*fyne.PointEvent) { r.onTap() }

// DoubleTapped implements fyne.DoubleTappable.
func (r *fsRow) DoubleTapped(*fyne.PointEvent) { r.onDouble() }

// MouseIn implements desktop.Hoverable.
func (r *fsRow) MouseIn(*desktop.MouseEvent) { r.hovered = true; r.Refresh() }

// MouseMoved implements desktop.Hoverable.
func (r *fsRow) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (r *fsRow) MouseOut() { r.hovered = false; r.Refresh() }

// CreateRenderer implements fyne.Widget.
func (r *fsRow) CreateRenderer() fyne.WidgetRenderer {
	rr := &fsRowRenderer{r: r}
	rr.Refresh()
	return rr
}

type fsRowRenderer struct{ r *fsRow }

func (rr *fsRowRenderer) Layout(size fyne.Size) {
	rr.r.bg.Resize(size)
	rr.r.content.Resize(size)
}

func (rr *fsRowRenderer) MinSize() fyne.Size { return rr.r.content.MinSize() }

func (rr *fsRowRenderer) Refresh() {
	th := rr.r.Theme()
	rr.r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	switch {
	case rr.r.selected:
		rr.r.bg.FillColor = themeColor(th, theme.ColorNameSelection)
	case rr.r.hovered:
		rr.r.bg.FillColor = themeColor(th, theme.ColorNameHover)
	default:
		rr.r.bg.FillColor = color.Transparent
	}
	rr.r.bg.Refresh()
}

func (rr *fsRowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{rr.r.bg, rr.r.content}
}

func (rr *fsRowRenderer) Destroy() {}
