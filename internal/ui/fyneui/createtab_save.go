package fyneui

import (
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The Create resource tab's save and search buttons, left of the template picker, as Lens's tab
// has them (captured 2026-10-11; what they do is not captured). Cogpit's reading (inferred): save
// writes the tab's text to a YAML file the user picks in the system's save panel (D-72), a GUI
// action with no command, as the table's CSV download is; search opens the editor's find widget
// below the editor (findbar.go), as the Helm Upgrade tab's search icon does. A text that holds a
// Secret's manifest is not saved to a file, so that its values never land on disk in plain text
// (D-19; Cogpit's default).

// createSaveRefused is the status when the text holds a Secret.
const createSaveRefused = "A Secret's manifest is not saved to a file: its values would sit on disk in plain text."

func (c *createTab) newTools() {
	c.saveBtn = newTipButton(c.sh, theme.DocumentSaveIcon(), "Save to a file…", c.saveToFile)
	c.searchBtn = newTipButton(c.sh, theme.SearchIcon(), "Find", c.toggleSearch)
	c.find = newFindBar(c.sh, c.pane.view)
	c.saveDialog = c.saveManifest // the system's save panel, else Fyne's dialog (filepicker_choosers.go)
}

// toggleSearch opens the editor's find widget, or closes it.
func (c *createTab) toggleSearch() {
	if c.find.visible() {
		c.find.close()
		return
	}
	c.find.open()
}

// saveToFile asks where to save the tab's text and writes it there.
func (c *createTab) saveToFile() {
	text := c.pane.view.Text()
	switch {
	case strings.TrimSpace(text) == "":
		c.setStatus("Nothing to save: the editor is empty.", colorMuted)
	case engine.TextHoldsSecret(text):
		c.setStatus(createSaveRefused, colorStatusWarning)
	default:
		c.saveDialog(manifestFileName(text), []byte(text))
	}
}

// showSaveDialog shows Fyne's file save dialog for data, suggesting name, where the system has no
// save panel (saveManifest); start, if set, is the folder it opens in. It returns the dialog for
// tests.
func (c *createTab) showSaveDialog(name string, data []byte, start fyne.ListableURI) *dialog.FileDialog {
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err == nil && w == nil {
			return // cancelled
		}
		if err == nil {
			err = writeAndClose(w, data)
		}
		if err != nil {
			c.setStatus("Not saved: "+err.Error(), colorStatusError)
			return
		}
		c.setStatus("Saved to "+filepath.FromSlash(w.URI().Path())+".", colorStatusOK) // C:/Users/… on Windows otherwise
		c.sh.toast(engine.LevelInfo, "Saved the manifest to "+w.URI().Name())
	}, c.sh.win)
	d.SetFileName(name)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".yaml", ".yml"}))
	if start != nil {
		d.SetLocation(start)
	}
	c.sh.showDialog(d)
	return d
}

// manifestFileName is the file name a save proposes: the first object's kind and name,
// "configmap-settings.yaml", made safe for a file system; "resource.yaml" when the text names
// neither.
func manifestFileName(text string) string {
	objs, err := engine.ParseManifests(text)
	if err != nil || len(objs) == 0 || objs[0].GetKind() == "" || objs[0].GetName() == "" {
		return "resource.yaml"
	}
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return '-'
	}, objs[0].GetKind()+"-"+objs[0].GetName())
	if len(name) > 120 {
		name = name[:120]
	}
	return strings.Trim(name, ".-") + ".yaml"
}
