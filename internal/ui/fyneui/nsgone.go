package fyneui

import (
	"image/color"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A namespace filter that names a namespace that is gone or going (the owner's gate walk,
// 2026-10-20, GW9): his Pods view was filtered to cogpit-gate when it was deleted, and showed "0 of
// 218 items" and "No pods match the filter." under a button that still read "cogpit-gate". The
// owner's choice, deliberately minimal ("Das wäre recht simpel und führt jetzt nicht zu weiteren
// Button und Logik"): a namespace still selected but deleted is marked red in the filter's list,
// like a crashing pod, and unticked it leaves the selection and the list; no notice, no button, no
// automatic removal. The lead's details, open to revision (D-106):
//   - a Terminating namespace is marked the same, selected or not: nothing can be created there
//     either;
//   - the row says which ("cogpit-gate · deleted", "· terminating") beside the colour, so that the
//     mark does not rest on the colour alone;
//   - the filter's button reads red (its label and its arrow) while a namespace it names is deleted
//     or terminating, one of them or several;
//   - what is gone or going comes from the cache through resources.list, as the list's names do:
//     read when a cluster is shown, when the filter opens, and on the engine's EventNamespaces,
//     which the namespaces' own changes send while the filter names namespaces (the owner's alpha
//     walk, AW11: until 2026-10-22 the view's changing rows read them again, and a namespace
//     without objects of the view's kind was noticed only at the filter's next opening;
//     engine/nswatch.go). Nothing is read while the user may not list namespaces: then nothing is
//     marked, for nothing is known.

// nsPageSize is how many namespaces one resources.list reads for the filter: the command's most.
// A cluster with more is read page by page (the side finding of GW9: one request without a limit
// answered the first 100).
const nsPageSize = 1000

// Why a namespace the filter offers is marked.
const (
	nsLive        = ""
	nsDeleted     = "deleted"
	nsTerminating = "terminating"
)

// namespaceState says whether a namespace is deleted (selected, and no longer in the cache's list),
// terminating, or neither; neither too while the names are not known.
func (sh *shell) namespaceState(name string) string {
	switch {
	case name == "" || !sh.namespacesKnown():
		return nsLive
	case sh.nsTerminating[name]:
		return nsTerminating
	case !slices.Contains(sh.namespaces, name):
		return nsDeleted
	}
	return nsLive
}

// namespacesKnown reports whether the cache's namespaces of the shown cluster are read: before,
// and while the user may not list them, nothing is known to be gone.
func (sh *shell) namespacesKnown() bool {
	return sh.namespaces != nil && sh.nsFor == sh.ui.Context && sh.namespacesListable()
}

// goneNamespaces are the filter's namespaces that are no longer in the cache's list, sorted; none
// while the names are not known.
func (sh *shell) goneNamespaces() []string {
	if !sh.namespacesKnown() {
		return nil
	}
	var out []string
	for _, n := range sh.ui.Namespaces {
		if !slices.Contains(sh.namespaces, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// filterNamesGone reports whether the namespace filter names a namespace that is deleted or
// terminating: its button reads red.
func (sh *shell) filterNamesGone() bool {
	for _, n := range sh.ui.Namespaces {
		if sh.namespaceState(n) != nsLive {
			return true
		}
	}
	return false
}

// namespacesChanged follows EventNamespaces: the shown cluster's namespaces are read again from
// the cache; another cluster's are read when it is shown.
func (sh *shell) namespacesChanged(contextID string) {
	if contextID == sh.ui.Context {
		sh.refreshNamespaces()
	}
}

// namespaceRow is one row of resources.list's result for the namespaces.
type namespaceRow struct {
	Ref    model.ObjectRef `json:"ref"`
	Status model.Status    `json:"status"`
}

// decodeNamespacePage reads one page of resources.list's namespaces: the rows and the total.
func decodeNamespacePage(res any) ([]namespaceRow, int, error) {
	var out struct {
		Total int            `json:"total"`
		Rows  []namespaceRow `json:"rows"`
	}
	if err := decode(res, &out); err != nil {
		return nil, 0, err
	}
	return out.Rows, out.Total, nil
}

// nsTintTheme is the theme of the namespace filter's button: the app's, the text and the arrow in
// the status error colour while on.
type nsTintTheme struct {
	app fyne.App
	on  bool
}

func (t *nsTintTheme) cur() fyne.Theme { return t.app.Settings().Theme() }

func (t *nsTintTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if t.on && n == theme.ColorNameForeground {
		return t.cur().Color(colorStatusError, v)
	}
	return t.cur().Color(n, v)
}
func (t *nsTintTheme) Font(s fyne.TextStyle) fyne.Resource     { return t.cur().Font(s) }
func (t *nsTintTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.cur().Icon(n) }
func (t *nsTintTheme) Size(n fyne.ThemeSizeName) float32       { return t.cur().Size(n) }
