package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// kubectlSettings are Preferences › Kubernetes' settings for kubectl in terminals (D-3;
// engine/kubectl.go), as Lens's Preferences name them (its documentation: download binaries, the
// download mirror, the directory for binaries, the path to kubectl; the controls and texts are
// Cogpit's, inferred). The mirror is dl.k8s.io or an https URL typed in (Lens offers Google or
// Azure). The engine refuses the last three for agents; in this window they are set like any
// other preference, a text field after a typing pause, the refusal under the field.
func (p *settingsPage) kubectlSettings() []fyne.CanvasObject {
	download := widget.NewCheck("Offer the release that matches the cluster", func(on bool) { p.setPreference("kubectlDownload", on) })
	mirror := newFieldEntry() // Escape closes the card (D-116, D-118)
	mirror.SetPlaceHolder("https://dl.k8s.io/release")
	mirror.OnChanged = func(string) {
		p.typed("kubectlMirror", func() { p.setPreference("kubectlMirror", strings.TrimSpace(mirror.Text)) })
	}
	dir := newFieldEntry()
	dir.SetPlaceHolder("Cogpit's own folder")
	dir.OnChanged = func(string) {
		p.typed("kubectlDir", func() { p.setPreference("kubectlDir", strings.TrimSpace(dir.Text)) })
	}
	browse := newTipButton(p.sh, theme.FolderOpenIcon(), "Pick a folder", nil)
	browse.OnTapped = func() {
		p.pickFolder(func(path string) { dir.SetText(path) }) // OnChanged sets it after the pause
	}
	enforce := widget.NewCheck("Define kubectl as a shell function in terminals", func(on bool) { p.setPreference("kubectlEnforce", on) })
	path := newFieldEntry()
	path.SetPlaceHolder("kubectl on PATH")
	path.OnChanged = func(string) {
		p.typed("kubectlPath", func() { p.setPreference("kubectlPath", strings.TrimSpace(path.Text)) })
	}
	outer := p.sync
	p.sync = func() {
		if outer != nil {
			outer()
		}
		pr := p.prefs()
		download.SetChecked(pr.KubectlDownload)
		p.setEntry(mirror, &mirror.Entry, "kubectlMirror", pr.KubectlMirror)
		p.setEntry(dir, &dir.Entry, "kubectlDir", pr.KubectlDir)
		p.setEntry(path, &path.Entry, "kubectlPath", pr.KubectlPath)
		enforce.SetChecked(pr.KubectlEnforce)
	}
	return []fyne.CanvasObject{
		p.setting("kubectlDownload", "Download kubectl",
			"For a terminal whose kubectl is over one minor version off its cluster, or missing; downloads wait for your click.",
			download),
		p.setting("kubectlMirror", "Download mirror",
			"An https URL laid out as dl.k8s.io/release; each download is verified against the release's signature.",
			mirror),
		p.setting("kubectlDir", "Directory for binaries",
			"Where downloaded releases are kept, as <folder>/<version>/kubectl: an absolute path.",
			container.NewBorder(nil, nil, nil, browse, dir)),
		p.setting("kubectlPath", "Path to kubectl",
			"The kubectl your terminals run, an absolute path; Cogpit checks it against each cluster as it checks PATH's.",
			path),
		p.setting("kubectlEnforce", "Enforce kubectl",
			"A shell function kubectl (zsh, bash, fish; not yet PowerShell or cmd), so that an alias or a folder put first later cannot win.",
			enforce),
	}
}
