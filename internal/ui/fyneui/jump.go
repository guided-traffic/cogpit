package fyneui

import (
	"encoding/json"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The palette's jump (engine/jump.go, D-161): a text that starts with ":" lists jump entries in
// place of the commands, k9s's `:pod kube-system /web @prod`. The `:` key opens the palette with
// ":" typed (openJump). The entries are made on every keystroke from what the window holds, without
// a request: the shown cluster's kinds (kinds.list, with their singular and short names), its
// namespaces (read again from the cache when the text turns into a jump, followed while it is
// one) and the contexts of the navigator's section. A jump to a cluster that is not connected asks
// for its login program's approval first, as every connect in the window does.

// jumpHint is the hint line while the text is a jump the user typed.
const jumpHint = "View [namespace] [/search] [@cluster] · Enter to open · Escape to close"

// openJump opens the command palette with ":" typed, its field focused and the caret after it, so
// that the user goes on typing a jump: the `:` key of the table and the window.
func (sh *shell) openJump() {
	p := sh.palette
	if p.visible() {
		p.typeJump()
		return
	}
	p.jumpNext = true
	p.open()
}

// typeJump puts ":" into the filter as the user's typing, focused, the caret at its end.
func (p *cmdPalette) typeJump() {
	p.filter.SetText(":")
	p.filter.CursorColumn = 1
	p.filter.Refresh()
	p.sh.win.Canvas().Focus(p.filter)
}

// jumping reports whether the filter's text is a jump.
func (p *cmdPalette) jumping() bool { return strings.HasPrefix(p.filter.Text, ":") }

// jumpEntries are the entries of the jump the filter holds. Turning into a jump, the text reads the
// shown cluster's namespaces again and follows them, so that one created since completes and none
// is greyed for want of it; the following ends when the palette closes.
func (p *cmdPalette) jumpEntries() []engine.PaletteEntry {
	sh := p.sh
	if p.stopJumpNS == nil {
		p.stopJumpNS = sh.followNamespaces(func([]string) {
			if p.visible() && p.jumping() {
				p.apply()
			}
		})
		if sh.ui.Connection.State.Live() {
			sh.loadNamespaces()
		}
	}
	return sh.eng.Registry().JumpEntries(sh.jumpIndex(), p.filter.Text, func(id string) bool { return sh.paletteRunner(id) != nil })
}

// endJump stops following the namespaces for the jump.
func (p *cmdPalette) endJump() {
	if p.stopJumpNS != nil {
		p.stopJumpNS()
		p.stopJumpNS = nil
	}
}

// jumpIndex is what the window knows for the jump: the shown cluster's kinds and namespaces while
// it is connected and they are its, and every context of the navigator's section.
func (sh *shell) jumpIndex() engine.JumpIndex {
	var ix engine.JumpIndex
	if sh.ui.Connection.State.Live() && sh.kindsFor == sh.ui.Context && sh.kinds != nil {
		ix.Context = sh.ui.Context
		kinds := make([]engine.JumpKind, 0, len(sh.kinds))
		for _, k := range sh.kinds {
			kinds = append(kinds, engine.JumpKind{Key: k.Kind, Title: k.Title, Kind: k.APIKind, Namespaced: k.Namespaced,
				Names: model.KindNames{Singular: k.Singular, ShortNames: k.ShortNames}})
		}
		ix.Views = engine.JumpViewsFor(kinds)
		if sh.nsFor == sh.ui.Context {
			ix.Namespaces = sh.namespaces
		}
	}
	for _, c := range sh.side.sec.contexts {
		ix.Clusters = append(ix.Clusters, engine.JumpCluster{Context: c.Name, Name: engine.ClusterName(c.Name, sh.ui.ClusterSettings[c.Name])})
	}
	return ix
}

// runJump runs a jump entry: after its login program's approval when it goes to a cluster that is
// not connected (kubeconfigs_approval.go), as the hotbar and the navigator's section connect one.
func (sh *shell) runJump(e engine.PaletteEntry) {
	var p engine.JumpParams
	if err := json.Unmarshal(e.Params, &p); err != nil {
		sh.showError(err)
		return
	}
	run := func() {
		sh.dispatch(engine.JumpCommand, e.Params, func(_ any, err error) {
			if err != nil && !engine.ConnectNotFailed(err) {
				sh.showError(err)
			}
		})
	}
	sh.connectingThen(p.Context, run)
}

// runPaletteConnect runs one of :ctx's entries: a cluster's opens it, after its login program's
// approval when it is not connected; the one without a cluster shows the navigator's section, as ⌘P.
func (sh *shell) runPaletteConnect(e engine.PaletteEntry) {
	var p contextParams
	if err := json.Unmarshal(e.Params, &p); err != nil || p.Context == "" {
		sh.findCluster()
		return
	}
	sh.connectingThen(p.Context, func() {
		sh.dispatch(connectCommand, p, func(_ any, err error) {
			if err != nil && !engine.ConnectNotFailed(err) {
				sh.showError(err)
			}
		})
	})
}

// connectingThen runs then at once for no context or a connected one; for one that is not
// connected, once its login program is allowed (nothing is left to allow for a context without
// one), and not at all when the user refuses.
func (sh *shell) connectingThen(contextID string, then func()) {
	if contextID == "" || clusterConnected(sh.ui, contextID) {
		then()
		return
	}
	sh.kc.reviewPlugin(contextID, func(allowed bool) {
		if !allowed {
			sh.toast("warning", "Not connected: the login of "+sh.identity(contextID).name+" was not allowed.")
			return
		}
		then()
	})
}
