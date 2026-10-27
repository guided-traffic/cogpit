package fyneui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Access reviews in the shell: kinds.list says per kind whether the user may list it (the sidebar
// leaves out the kinds they may not, and shows those not known yet), and a command the reviews
// deny is greyed in the row menu with the engine's reason ("not permitted: ...").

// setListable sets kinds.list's answers: a kind missing from listable stays unknown.
func setListable(f *fakeEngine, listable map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, k := range f.kinds {
		if l, ok := listable[k.Kind]; ok {
			f.kinds[i].Listable = &l
		} else {
			f.kinds[i].Listable = nil
		}
	}
}

// TestSidebarHidesKindsNotListable: Nodes and Deployments, which the user may not list, are left
// out; Services, not known yet, are shown; when the answers change, EventKinds brings a kind back.
func TestSidebarHidesKindsNotListable(t *testing.T) {
	f := connectedFake(t, 50)
	setListable(f, map[string]bool{"nodes": false, "deployments": false, "pods": true, "namespaces": true, "events": true})
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	s := sh.side
	if got := titlesOf(s, s.children[""]); !slices.Equal(got, []string{"Overview", "Workloads", "Network", "Namespaces", "Events"}) {
		t.Errorf("top level %v, want Nodes left out", got)
	}
	if got := s.children[groupPrefix+"workloads"]; !slices.Equal(got, []string{"workloads/overview", "pods"}) {
		t.Errorf("workloads %v, want Deployments left out", got)
	}
	if got := s.children[groupPrefix+"network"]; !slices.Equal(got, []string{"services"}) {
		t.Errorf("network %v, want Services shown while unknown", got)
	}

	setListable(f, map[string]bool{"nodes": false, "deployments": true, "pods": true})
	f.emit(engine.Event{Type: engine.EventKinds})
	q.until(t, "Deployments back", func() bool {
		return slices.Equal(s.children[groupPrefix+"workloads"], []string{"workloads/overview", "pods", "deployments"})
	})
}

// renderAccess: a user who may not list nodes and some workloads, and may not delete or shell
// into the pods of the namespace: the sidebar without those kinds, the row menu with Shell and
// Delete greyed and the reason after their labels.
func renderAccess(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "pods")
	registerProbes(t, f)
	setListable(f, map[string]bool{
		"nodes": false, "deployments": false, "daemonsets": false, "statefulsets": false, "configmaps": false,
		"pods": true, "replicasets": true, "jobs": true, "services": true, "namespaces": true, "events": true,
	})
	var pod podSpec
	for _, p := range fixturePodSpecs() {
		if len(p.containers) == 2 && len(p.init) == 1 {
			pod = p
			break
		}
	}
	f.disabled["pod.shell"] = "not permitted: you may not create pods/exec in " + pod.ns
	f.disabled["object.delete"] = "not permitted: you may not delete pods in " + pod.ns
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	c := cellAt(t, sh, rowOf(t, f, "pods", pod.name), 0)
	test.TapSecondaryAt(c, fyne.NewPos(120, c.Size().Height/2))
	q.until(t, "the row menu", func() bool { return sh.view.menu != nil && sh.view.menuModel != nil })
	m := sh.view.menuModel
	for _, label := range []string{"Shell", "Delete"} {
		it := item(t, m, label)
		if !it.Disabled || it.Label != label+" — not permitted: you may not "+map[string]string{"Shell": "create pods/exec", "Delete": "delete pods"}[label]+" in "+pod.ns {
			t.Errorf("%s: %q disabled %v", label, it.Label, it.Disabled)
		}
	}
	if item(t, m, "Logs").Disabled || item(t, m, "Evict").Disabled {
		t.Error("only Shell and Delete are denied")
	}
	if _, ok := sh.side.titles["deployments"]; ok {
		t.Error("Deployments in the sidebar")
	}
	savePNG(t, w, name)
}

func TestRenderAccessDark(t *testing.T) { renderAccess(t, ThemeDark, "access_dark.png") }
