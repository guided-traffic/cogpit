package fyneui

import (
	"errors"

	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// newCreateButton is the "+" that creates an object in the open list (Lens's was a round button
// over the rows, captured 2026-10-11; here it is a chip at the right end of the dock's strip,
// stripchip.go, so that it never covers a cell): it opens a Create resource tab in the dock,
// starting from the template of the view's kind when Cogpit has one (Cogpit's choice; what Lens
// prefills is not captured), else the example Pod. On the views whose kind has a create dialog
// (Namespaces, Service Accounts, the four RBAC kinds, Resource Quotas: createdialog.go) it opens
// that dialog instead, as Lens does (inferred).
//
// It shows while a cluster is connected and a view is open, except on the views of local and
// virtual kinds (Port Forwarding, Helm Releases): no manifest creates their objects, so a Create
// resource tab would only offer to create something else (owner, 2026-10-12).
func newCreateButton(sh *shell) *stripChip {
	return newStripChip(theme.ContentAddIcon,
		func() bool {
			ui := sh.ui
			return ui.Connection.State.Live() && ui.View != "" && !sh.cogpitKind(ui.View)
		},
		func() {
			if !sh.openCreateDialog(sh.ui.View) {
				sh.openCreateTab()
			}
		})
}

// cogpitKind reports whether kinds.list marks a kind key local or virtual.
func (sh *shell) cogpitKind(key string) bool {
	for _, k := range sh.kinds {
		if k.Kind == key {
			return k.Local || k.Virtual
		}
	}
	return false
}

// openCreateTab opens a Create resource tab, from the template of the open view's kind when
// Cogpit has one.
func (sh *shell) openCreateTab() {
	template := ""
	for _, k := range sh.kinds {
		if k.Kind == sh.ui.View {
			template = k.APIKind
		}
	}
	sh.dispatch("resource.createTab", createTabParams{Template: template}, func(_ any, err error) {
		var dis *engine.DisabledError
		switch {
		case err == nil:
		case template != "" && !errors.As(err, &dis):
			sh.dispatch("resource.createTab", createTabParams{}, nil) // no template for this kind
		default:
			sh.showError(err)
		}
	})
}
