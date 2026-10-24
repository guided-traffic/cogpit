package fyneui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
)

// copyPathOf is a menu's Copy Kubeconfig Path item; nil without one.
func copyPathOf(m *fyne.Menu) *fyne.MenuItem {
	if m == nil {
		return nil
	}
	for _, it := range m.Items {
		if it != nil && it.Label == copyPathLabel {
			return it
		}
	}
	return nil
}

// Copy Kubeconfig Path is in every menu of a cluster (the owner's VW13): the navigator's root (the
// shown cluster), its rows in All Clusters, connected or not, a hotbar tile's and a welcome page
// row's. It puts the path of the file the context comes from on the clipboard and says so in a
// toast; the demo cluster, from no file, has no such item, and neither has a context the window does
// not know (a hotbar tile whose context went).
func TestCopyKubeconfigPathInEveryClusterMenu(t *testing.T) {
	f := sectionFake(t)
	fh := withHotbar(f, secKind, secProd, "demo", "gone [old.yaml]")
	fh.missing["gone [old.yaml]"] = true
	sh, _, q := newTestShell(t, f, Options{})
	n := sh.side.sec
	q.until(t, "the clusters and the hotbar", func() bool {
		return n.loaded && len(tilesOf(sh.hot)) == 4 && len(sh.kinds) > 0
	})
	pos := fyne.NewPos(300, 300)
	copied := func(where string, m *fyne.Menu, want string) {
		t.Helper()
		item := copyPathOf(m)
		if item == nil || item.Disabled {
			t.Fatalf("%s: no Copy Kubeconfig Path in %v", where, menuLabels(m))
		}
		sh.app.Clipboard().SetContent("")
		item.Action()
		if got := sh.app.Clipboard().Content(); got != want {
			t.Errorf("%s copied %q, want %q", where, got, want)
		}
		q.until(t, where+"'s toast", func() bool { return strings.Contains(toastTexts(sh), "Copied the kubeconfig path "+want) })
	}

	// The navigator's rows: the shown cluster, one not connected, one whose connect failed.
	n.rowMenu(secKind, pos)
	// With the cluster's own items, right after the hotbar's, before the connection's (Reconnect,
	// Disconnect, Remove from Cogpit; the fake has none of them).
	if labels := menuLabels(n.menuModel); slices.Index(labels, copyPathLabel) != slices.Index(labels, "Remove from Hotbar")+1 {
		t.Errorf("the shown cluster's row menu %v", labels)
	}
	copied("the shown cluster's row", n.menuModel, "/Users/dev/.kube/config")
	n.rowMenu(secEKS, pos)
	copied("a row not connected", n.menuModel, "/Users/dev/eks.yaml")
	n.rowMenu(secProd, pos)
	copied("a failed cluster's row", n.menuModel, "/Users/dev/.kube/prod.yaml")
	n.rowMenu("demo", pos)
	if copyPathOf(n.menuModel) != nil {
		t.Errorf("the demo cluster's row: %v", menuLabels(n.menuModel))
	}

	// The navigator's root: the shown cluster.
	sh.side.rootRow.TappedSecondary(&fyne.PointEvent{AbsolutePosition: pos})
	copied("the navigator's root", sh.side.rootRow.menuModel, "/Users/dev/.kube/config")

	// A hotbar tile's menu: a cluster not connected; the demo and a context that went have none.
	sh.hot.tileMenu(2, pos)
	copied("a hotbar tile", sh.hot.menuModel, "/Users/dev/.kube/prod.yaml")
	for slot, what := range map[int]string{3: "the demo cluster's tile", 4: "a missing context's tile"} {
		sh.hot.tileMenu(slot, pos)
		if copyPathOf(sh.hot.menuModel) != nil {
			t.Errorf("%s: %v", what, menuLabels(sh.hot.menuModel))
		}
	}

	// A welcome page row's menu.
	sh.tabs.welcome.menu(secEKS, pos)
	copied("a welcome page row", sh.tabs.welcome.menuModel, "/Users/dev/eks.yaml")
}

// Over the engine, a context of a kubeconfig source copies its file's path: a file the user added
// and the default ~/.kube's config alike, the item with the cluster's own items before Remove from
// Cogpit.
func TestCopyKubeconfigPathOverTheEngine(t *testing.T) {
	s, added := sourcesWelcomeEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	n := sh.side.sec
	q.until(t, "the section", func() bool { return n.loaded && len(n.contexts) == 6 })
	n.rowMenu("team-a [team.yaml]", fyne.NewPos(300, 300))
	labels := menuLabels(n.menuModel)
	if want := []string{"Connect", "Settings…", copyPathLabel, "", "Remove from Cogpit…"}; !slices.Equal(labels, want) {
		t.Errorf("the row's menu %v, want %v", labels, want)
	}
	copyPathOf(n.menuModel).Action()
	if got := sh.app.Clipboard().Content(); got != added {
		t.Errorf("copied %q, want %q", got, added)
	}
	c, ok := sh.contextInfo("kind-dev [.kube/config]")
	n.rowMenu("kind-dev [.kube/config]", fyne.NewPos(300, 300))
	copyPathOf(n.menuModel).Action()
	if got := sh.app.Clipboard().Content(); !ok || got != c.File || !strings.HasSuffix(got, filepath.FromSlash(".kube/config")) {
		t.Errorf("copied %q, the context's file %q", got, c.File)
	}
}
