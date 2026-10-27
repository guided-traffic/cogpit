package fyneui

import (
	"encoding/json"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// accessibleRows are the namespaces the shown Namespaces section lists.
func accessibleRows(sh *shell) []string {
	var out []string
	for k := range sh.settings.controls {
		if ns, ok := cutPrefix(k, "remove:"); ok {
			out = append(out, ns)
		}
	}
	slices.Sort(out)
	return out
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}

// A cluster's Settings › Namespaces, between Terminal and Metrics as captured: a name typed and
// submitted (Enter) or added with Add is kept, the list shows the namespaces sorted, a name that
// is no namespace's is refused under the field, a namespace is removed with its button, and an
// agent's change shows at once.
func TestClusterNamespacesField(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev"})
	q.until(t, "the page", sh.settings.visible)
	test.Tap(sh.settings.navItems[engine.SectionNamespaces])
	q.until(t, "the Namespaces section", func() bool { return sh.settings.controls["accessibleEntry"] != nil })
	entry := entryOf(sh.settings.controls["accessibleEntry"])
	kept := func() []string {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.AccessibleNamespaces
	}

	w.Canvas().Focus(entry)
	test.Type(entry, "team-b")
	entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "team-b kept", func() bool { return slices.Equal(kept(), []string{"team-b"}) && entry.Text == "" })
	test.Type(entry, "team-a")
	test.Tap(sh.settings.controls["accessibleAdd"].(*textButton))
	// The field is cleared in the dispatch's callback, which may come after the list followed
	// the engine: wait for both, or the next name is typed into a field about to be cleared.
	q.until(t, "team-a kept, sorted", func() bool {
		return slices.Equal(kept(), []string{"team-a", "team-b"}) && slices.Equal(accessibleRows(sh), []string{"team-a", "team-b"}) &&
			entry.Text == ""
	})

	test.Type(entry, "Team_C")
	test.Tap(sh.settings.controls["accessibleAdd"].(*textButton))
	q.until(t, "the refusal", func() bool { return sh.settings.errs[accessibleSetting].Visible() })
	if !slices.Equal(kept(), []string{"team-a", "team-b"}) || entry.Text != "Team_C" {
		t.Errorf("a refused name changed the list %q or cleared the field %q", kept(), entry.Text)
	}

	test.Tap(sh.settings.controls["remove:team-a"].(*tipButton))
	q.until(t, "team-a removed", func() bool {
		return slices.Equal(kept(), []string{"team-b"}) && slices.Equal(accessibleRows(sh), []string{"team-b"})
	})
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "accessibleNamespaces": []string{"shop", "team-b"}})
	q.until(t, "the agent's change shown", func() bool { return slices.Equal(accessibleRows(sh), []string{"shop", "team-b"}) })
}

// The namespace filter while Namespaces cannot be listed: it offers the cluster's accessible
// namespaces and those the open view reads or names as denied, and does not try to list the
// cluster's namespaces (the access reviews said no); a check narrows the view as before.
func TestNamespaceFilterOffersAccessibleNamespaces(t *testing.T) {
	f := connectedFake(t, 20)
	f.namespaces = nil
	setListable(f, map[string]bool{"namespaces": false, "pods": true})
	f.ui.ClusterSettings = map[string]engine.ClusterSettings{"demo": {AccessibleNamespaces: []string{"team-a", "team-b"}}}
	snap := *f.tables["pods"]
	snap.Watched, snap.Denied = []string{"team-a"}, []string{"team-b", "team-c"}
	f.tables["pods"] = &snap
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	tv := sh.view
	namespaceLists := func() int {
		n := 0
		for _, c := range f.callsOf("resources.list") {
			var p struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal(c.Params, &p) == nil && p.Kind == kindNamespaces {
				n++
			}
		}
		return n
	}
	before := namespaceLists() // the shell asks once when it connects, before the access reviews answer
	test.Tap(tv.nsButton)
	var labels []string
	for _, r := range tv.ns.rows {
		labels = append(labels, r.label.text)
	}
	if want := []string{"All namespaces", "team-a", "team-b", "team-c"}; !slices.Equal(labels, want) {
		t.Fatalf("filter offers %q, want %q", labels, want)
	}
	q.drain()
	if namespaceLists() != before {
		t.Error("the filter tried to list the namespaces the user may not list")
	}
	if want := "You may not list Pods in all namespaces: read in team-a only. Not permitted in team-b, team-c."; !tv.scopeLine.Visible() || tv.scopeLine.text != want {
		t.Errorf("scope line %q (visible %v), want %q", tv.scopeLine.text, tv.scopeLine.Visible(), want)
	}
	test.Tap(tv.ns.rows[1].check)
	q.until(t, "the filter set", func() bool {
		calls := f.callsOf("ui.setNamespace")
		return len(calls) > 0 && string(calls[len(calls)-1].Params) == `{"namespaces":["team-a"]}`
	})
}

// The Pods view of a user who may not list pods in all namespaces, its namespace filter open on
// the accessible namespaces, dark and light (AGENTS.md rule 7).
func TestRenderNamespaceFilterAccessibleDark(t *testing.T) {
	renderNamespaceFilterAccessible(t, ThemeDark, "namespace_filter_accessible_dark.png")
}

func renderNamespaceFilterAccessible(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 0)
	f.namespaces = nil
	setListable(f, map[string]bool{"namespaces": false, "pods": true})
	f.ui.ClusterSettings = map[string]engine.ClusterSettings{"demo": {AccessibleNamespaces: []string{"checkout", "orders", "payments"}}}
	full := fixturePods(200, 7)
	snap := *full
	snap.Rows = nil
	for _, r := range full.Rows {
		if r.Ref.Namespace == "checkout" || r.Ref.Namespace == "payments" {
			snap.Rows = append(snap.Rows, r)
		}
	}
	snap.Total = len(snap.Rows)
	snap.Watched, snap.Denied = []string{"checkout", "payments"}, []string{"orders"}
	f.tables["pods"] = &snap
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.scopeLine.Visible() })
	test.Tap(sh.view.nsButton)
	q.until(t, "the filter", func() bool { return sh.view.ns.visible() })
	savePNG(t, w, name)
}

// The Namespaces section, dark and light: the PNGs of the pull request (AGENTS.md rule 7).
func TestRenderClusterSettingsNamespacesDark(t *testing.T) {
	renderClusterSettingsNamespaces(t, ThemeDark, "cluster_settings_namespaces_dark.png")
}

func renderClusterSettingsNamespaces(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster", "iconColor": "violet",
		"accessibleNamespaces": []string{"team-a", "team-b", "payments"}})
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "namespaces"})
	q.until(t, "the Namespaces section", func() bool {
		return sh.settings.visible() && sh.settings.view != nil && len(accessibleRows(sh)) == 3
	})
	q.drain()
	savePNG(t, w, name)
}
