package fyneui

import (
	"runtime"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
)

// TestKubeconfigPathSelectable (the owner's WF28, D-87): the kubeconfig's path on Cluster Settings ›
// General is the whole path and selectable: ⌘A selects all of it, ⌘C copies the selection as it is
// (not cleared later, as a Secret value is), a drag selects a part; a long path wraps rather than
// being shortened, so that a selection copies the real path.
func TestKubeconfigPathSelectable(t *testing.T) {
	s, ctx, file := settingsEngineWithFile(t)
	sh, w, q := openGeneral(t, s, ctx, ThemeDark)
	var path *secretText
	q.until(t, "the path", func() bool {
		p, ok := sh.settings.controls["kubeconfigPath"].(*secretText)
		path = p
		return ok && p.Visible() && p.text == file
	})
	settle(w)
	path.TypedShortcut(&fyne.ShortcutSelectAll{})
	if got := path.SelectedText(); got != file {
		t.Fatalf("select all: %q, want %q", got, file)
	}
	sh.app.Clipboard().SetContent("")
	path.TypedShortcut(&fyne.ShortcutCopy{Clipboard: sh.app.Clipboard()})
	if got := sh.app.Clipboard().Content(); got != file {
		t.Errorf("⌘C copied %q, want %q", got, file)
	}
	if sh.clipTimer != nil {
		t.Error("the path's copy is cleared later, as a Secret value's")
	}

	// A drag over the first five characters selects them.
	path.FocusLost()
	charW, lineH := path.metrics()
	from, to := fyne.NewPos(0, lineH/2), fyne.NewPos(5*charW, lineH/2)
	path.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: to}, Dragged: fyne.NewDelta(to.X-from.X, 0)})
	path.DragEnd()
	if got := path.SelectedText(); got != file[:5] {
		t.Errorf("a drag over five characters selected %q", got)
	}

	// A long path wraps, whole.
	long := "/Users/someone/" + strings.Repeat("a-rather-long-folder-name/", 8) + "config"
	path.setText(long)
	path.Resize(fyne.NewSize(40*charW, path.MinSize().Height))
	if lines := path.lines(); len(lines) < 2 || strings.Join(lines, "") != long {
		t.Errorf("the long path in %d lines: %q", len(lines), lines)
	}
}

// The General section with a long kubeconfig path, wrapped and all of it selected (⌘A), dark and
// light: the PNGs of the pull request (WF28).
func TestRenderKubeconfigPathSelected(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		t.Run(string(mode), func(t *testing.T) {
			s, ctx, _ := settingsEngineWithFile(t)
			sh, w, q := openGeneral(t, s, ctx, mode)
			var path *secretText
			q.until(t, "the path", func() bool {
				p, ok := sh.settings.controls["kubeconfigPath"].(*secretText)
				path = p
				return ok && p.Visible() && p.text != ""
			})
			path.setText("/Users/someone/Library/Mobile Documents/com~apple~CloudDocs/clusters/production/eu-central-1/" +
				"team-platform/kubeconfig-prod-eu-central-1.yaml")
			for range 2 { // the second layout asks the path's height for the columns of the first
				sh.settings.body.Refresh()
				settle(w)
			}
			path.TypedShortcut(&fyne.ShortcutSelectAll{})
			savePNG(t, w, "cluster_settings_kubeconfig_selected_"+string(mode)+".png")
		})
	}
}

// TestFolderURL: the folder icon opens the folder by a file URL the system's file manager takes:
// slashes and a leading one on every system, so a Windows folder is file:///C:/…, not a URL whose
// host is the drive.
func TestFolderURL(t *testing.T) {
	dir, want := "/Users/a b/.kube", "file:///Users/a%20b/.kube"
	if runtime.GOOS == "windows" {
		dir, want = `C:\Users\a b\.kube`, "file:///C:/Users/a%20b/.kube"
	}
	if got := folderURL(dir).String(); got != want {
		t.Errorf("folderURL(%q) = %s, want %s", dir, got, want)
	}
}
