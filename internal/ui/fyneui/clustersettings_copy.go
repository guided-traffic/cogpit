package fyneui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2/widget"
)

// Copy icons on Cluster Settings › General (the owner's F27: "Clustername und Pfad zur kube-config
// muss kopierbar sein"): the cluster name's field and the kubeconfig's path have the copy icon the
// drawer and the lists have (D-25: the same icon, its checkmark after a copy, the same toast),
// always shown beside the field, as a Secret editor's is. A GUI behaviour, no command: an agent
// reads both in cluster_settings_get (name, contextName, kubeconfig).

// settingCopyIcon returns a copy icon, kept among the section's controls as name, that copies what
// text returns; nothing when it returns "".
func (p *settingsPage) settingCopyIcon(name, tip string, text func() string) *copyButton {
	var done *time.Timer // the checkmark's timer
	var b *copyButton
	b = newPinnedCopyIcon(p.sh, tip, func() {
		t := text()
		if t == "" {
			return
		}
		p.sh.copyText(t, false)
		b.confirm(&done)
		p.sh.copyToast("info", copiedToast)
	})
	p.controls[name] = b
	return b
}

// shownClusterName is what the cluster name's field shows: the name typed, else its placeholder,
// the context's name (the lead's decision: the copy takes what the user reads there).
func (p *settingsPage) shownClusterName(e *widget.Entry) string {
	if t := strings.TrimSpace(e.Text); t != "" {
		return t
	}
	if e.PlaceHolder != "" {
		return e.PlaceHolder
	}
	return p.clusterView().Name
}
