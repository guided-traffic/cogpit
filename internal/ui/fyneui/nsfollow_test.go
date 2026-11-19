package fyneui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2/test"
)

// TestNamespaceFilterOffersANamespaceCreatedSince is the owner's WF10: a namespace created after
// the cluster was shown is in the filter's list when it opens next.
func TestNamespaceFilterOffersANamespaceCreatedSince(t *testing.T) {
	sh, _, q, f := nsFilterShell(t, ThemeDark)
	f.mu.Lock()
	f.namespaces = append(slices.Clone(thirtyNamespaces), "walk-test")
	f.mu.Unlock()
	tv := sh.view
	test.Tap(tv.nsButton)
	q.until(t, "the new namespace", func() bool { return slices.Contains(sh.namespaces, "walk-test") })
	q.until(t, "its row", func() bool {
		for _, r := range tv.ns.rows {
			if r.name == "walk-test" {
				return true
			}
		}
		return false
	})
}

// TestCreateDialogFollowsTheNamespaces: a create dialog's namespace field reads the names again when
// it opens and follows them while it is open; closing it stops that (WF10).
func TestCreateDialogFollowsTheNamespaces(t *testing.T) {
	f := createFake(t, kindSecrets)
	sh, _, q := newCreateShell(t, f, ThemeDark)
	q.until(t, "the namespaces", func() bool { return len(sh.namespaces) == len(f.namespaces) })
	f.mu.Lock()
	f.namespaces = append(slices.Clone(f.namespaces), "walk-test")
	f.mu.Unlock()
	form := openForm(t, sh, q, kindSecrets)
	q.until(t, "the dialog to read the names again", func() bool { return slices.Contains(sh.namespaces, "walk-test") })
	if len(sh.nsFollow) != 1 || len(form.nsFields) != 1 {
		t.Fatalf("followers %d, fields %d", len(sh.nsFollow), len(form.nsFields))
	}
	var got []string
	stop := sh.followNamespaces(func(names []string) { got = names })
	f.mu.Lock()
	f.namespaces = append(slices.Clone(f.namespaces), "later")
	f.mu.Unlock()
	sh.refreshNamespaces()
	q.until(t, "the followers to hear of it", func() bool { return slices.Contains(got, "later") })
	stop()
	form.d.Hide()
	if len(sh.nsFollow) != 0 {
		t.Errorf("%d followers after the dialog closed", len(sh.nsFollow))
	}
}
