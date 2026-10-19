package fyneui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// The window's file choosers on the system's panels (filepicker.go, D-72), each falling back to
// its Fyne dialog where there is no system panel. What Cogpit reads again in a later run keeps a
// security-scoped bookmark (nativedialog.Store): a kubeconfig source, a folder, a CA file. What it
// reads or writes once does not: an icon is copied into Cogpit's own folder at once
// (cluster.settings.uploadIcon), and a save writes its file while the panel's grant holds.

// syncPanelMessage is the line above the Sync panel's list.
const syncPanelMessage = "Select kubeconfig files or folders of kubeconfigs and press Sync. " +
	"Cogpit reads them, follows their changes and never writes them."

// extYAML is a manifest's extension, the first of yamlExtensions: the one a name without one gets.
const extYAML = "yaml"

// Extensions the choosers accept, without the dot.
var (
	iconExtensions = []string{"png", "jpg", "jpeg", "svg"}
	caExtensions   = []string{"pem", "crt", "cer"}
	yamlExtensions = []string{extYAML, "yml"}
	csvExtensions  = []string{"csv"}
)

// syncPanel is the Sync Files and Folders panel, the system's dialog as Lens opens one: files and
// folders together, several at once, hidden files listed (~/.kube, a .kubeconfig), the button
// "Sync" (with nothing selected an open panel that takes folders returns the folder shown:
// AppKit's behaviour, not tried in a real window). It opens in start, the home folder.
func syncPanel(start string) nativedialog.OpenOptions {
	return nativedialog.OpenOptions{Title: "Sync Files and Folders", Message: syncPanelMessage, Prompt: "Sync",
		Files: true, Folders: true, Multiple: true, ShowHidden: true, Dir: start, Bookmark: nativedialog.ReadOnly}
}

// pickFiles asks for kubeconfig files and folders in the system's panel and calls done for each,
// in the order chosen; without one it shows Cogpit's own sync picker. A cancel calls nothing.
func (k *kubeconfigUI) pickFiles(done func(path string, folder bool)) {
	sh := k.sh
	if sh.files.open(syncPanel(k.startDir()), func(picks []nativedialog.Pick) {
		sh.keepAccess(nativedialog.UseKubeconfig, nativedialog.ReadOnly, picks)
		for _, p := range picks {
			done(p.Path, p.Folder)
		}
	}) {
		return
	}
	k.pickWithDialog(done)
}

// iconPanel asks for an image for a cluster's icon.
var iconPanel = nativedialog.OpenOptions{Title: "Upload Icon", Message: "Choose an image for the cluster's icon: PNG, JPEG or SVG.",
	Prompt: "Upload", Files: true, Types: iconExtensions}

// pickIconPanel asks for an image file in the system's panel, else in Fyne's dialog.
func (p *settingsPage) pickIconPanel(done func(path string)) {
	if p.sh.files.open(iconPanel, func(picks []nativedialog.Pick) { done(picks[0].Path) }) {
		return
	}
	p.pickIconWithDialog(done)
}

// folderPanel asks for a folder: a cluster's terminal working directory, or where kubectl's
// downloaded binaries go. Its bookmark is read-write: Cogpit writes kubectl there.
var folderPanel = nativedialog.OpenOptions{Title: "Choose a Folder", Prompt: "Choose", Folders: true,
	Bookmark: nativedialog.ReadWrite}

// pickFolderPanel asks for a folder in the system's panel, else in Fyne's dialog.
func (p *settingsPage) pickFolderPanel(done func(path string)) {
	sh := p.sh
	if sh.files.open(folderPanel, func(picks []nativedialog.Pick) {
		sh.keepAccess(nativedialog.UseFolder, nativedialog.ReadWrite, picks)
		done(picks[0].Path)
	}) {
		return
	}
	p.pickFolderWithDialog(done)
}

// caPanel asks for a chat connection's CA file.
var caPanel = nativedialog.OpenOptions{Title: "Choose a CA File",
	Message: "Choose the PEM file of the CA certificates this connection trusts.", Prompt: "Choose", Files: true,
	Types: caExtensions, Bookmark: nativedialog.ReadOnly}

// chooseCAFile picks the CA file in the system's panel, else in Fyne's dialog.
func (f *agentForm) chooseCAFile() {
	sh := f.a.p.sh
	if sh.files.open(caPanel, func(picks []nativedialog.Pick) {
		sh.keepAccess(nativedialog.UseCAFile, nativedialog.ReadOnly, picks)
		f.caFile.SetText(picks[0].Path)
	}) {
		return
	}
	f.chooseCAFileWithDialog()
}

// saveTable is the shell's saveFile: the table's CSV (table.exportCsv) into the file the user
// picks in the system's save panel, else in Fyne's dialog.
func (sh *shell) saveTable(name string, rows int, data []byte) {
	if sh.files.save(nativedialog.SaveOptions{Title: "Download CSV", Name: name, Types: csvExtensions}, func(path string) {
		sh.writePicked(path, data, func(err error) {
			if err != nil {
				sh.toast("error", "Could not save the table: "+err.Error())
				return
			}
			sh.toast("info", fmt.Sprintf("Saved %s to %s", itemCount(rows, rows), filepath.Base(path)))
		})
	}) {
		return
	}
	sh.saveWithDialog(name, rows, data, nil)
}

// saveManifest is the Create tab's save: its text into the YAML file the user picks in the
// system's save panel, else in Fyne's dialog.
func (c *createTab) saveManifest(name string, data []byte) {
	sh := c.sh
	if sh.files.save(nativedialog.SaveOptions{Title: "Save to a File", Name: name, Types: yamlExtensions}, func(path string) {
		sh.writePicked(path, data, func(err error) {
			if err != nil {
				c.setStatus("Not saved: "+err.Error(), colorStatusError)
				return
			}
			c.setStatus("Saved to "+path+".", colorStatusOK)
			sh.toast(engine.LevelInfo, "Saved the manifest to "+filepath.Base(path))
		})
	}) {
		return
	}
	c.showSaveDialog(name, data, nil)
}

// saveLines is a log tab's download: the lines it shows into the file the user picks in the
// system's save panel, else in Fyne's dialog (saveDialog).
func (lt *logTab) saveLines() {
	if lt.view == nil {
		return
	}
	sh := lt.sh
	if sh.files.save(nativedialog.SaveOptions{Title: "Download Logs", Name: lt.fileName()}, func(path string) {
		lines := lt.view.DownloadLines()
		go func() {
			defer sh.recoverTo("logs download")
			f, err := createPicked(path)
			if err == nil {
				err = saveLogs(f, lines)
			}
			sh.do(func() {
				if err != nil {
					sh.toast("error", "Could not save the logs: "+err.Error())
					return
				}
				sh.toast("info", "Saved "+formatCount(int64(len(lines)))+" lines to "+filepath.Base(path))
			})
		}()
	}) {
		return
	}
	lt.saveDialog()
}

// writePicked writes data to the file the user picked in a save panel, off the main goroutine,
// and calls done with the outcome on it.
func (sh *shell) writePicked(path string, data []byte, done func(error)) {
	go func() {
		defer sh.recoverTo("saving a file")
		f, err := createPicked(path)
		if err == nil {
			_, err = f.Write(data)
			err = errors.Join(err, f.Close())
		}
		sh.do(func() { done(err) })
	}()
}

// createPicked creates or truncates the file the user picked in a save panel, owner-only when it
// is new (a table, a manifest or logs of a cluster are the user's, not everyone's on the Mac); an
// existing file keeps its mode. The panel asked before replacing it.
func createPicked(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}
