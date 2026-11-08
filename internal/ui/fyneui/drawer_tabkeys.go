package fyneui

import "github.com/guided-traffic/cogpit/internal/model"

// The drawer's tab a key asks for (k9s's y and d, rowkeys.go): YAML or Details. Which tab shows is
// the window's, as a click on a tab is: it changes what the window shows of the object, not which
// object.

// tabWant is the tab a key asked the drawer for, on its object.
type tabWant struct {
	ref  model.ObjectRef
	yaml bool
}

// openTab asks for an object's YAML or Details tab: at once when the drawer shows the object, else
// once it does (show). A release's flat drawer has no tabs.
func (d *drawer) openTab(ref model.ObjectRef, yaml bool) {
	d.tabWant = &tabWant{ref: ref, yaml: yaml}
	if d.shown != nil && d.shown.Same(ref) && d.last != nil {
		d.selectWantedTab()
	}
}

// selectWantedTab selects the asked-for tab while the drawer shows its object, and forgets the wish;
// another object's wish waits for it.
func (d *drawer) selectWantedTab() {
	w := d.tabWant
	if w == nil || d.shown == nil || !d.shown.Same(w.ref) || d.last == nil {
		return
	}
	d.tabWant = nil
	if d.plain {
		return
	}
	if w.yaml {
		d.tabs.Select(d.yamlTab)
	} else {
		d.tabs.SelectIndex(0)
	}
}
