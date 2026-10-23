package fyneui

import (
	"errors"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster shown while its login is expired or its connection interrupted, whose objects were never
// read (the owner's finding of 2026-10-22, acme-p-pinniped): the cache's lists wait until the cluster
// is back, so the view loads nothing and the namespace filter has no namespaces of it.

// unreadPods is the Pods view's snapshot before its first list: no rows, not synced, no error.
func unreadPods(t *testing.T) *engine.TableSnapshot {
	snap := kindSnapshot(t, "pods", nil)
	snap.Synced = false
	return snap
}

// TestAViewNotReadWhileAway: the view says why it is empty and when it loads, with no item count, no
// "syncing…" and no download, for an expired login and an interruption; connected again it loads as
// before.
func TestAViewNotReadWhileAway(t *testing.T) {
	f := connectedFake(t, 0)
	f.tables["pods"] = unreadPods(t)
	f.ui.Connection = engine.Connection{State: engine.LoginExpired, Since: fixedNow, CredentialPlugin: true}
	sh, w, q := newTestShell(t, f, Options{})
	tv := sh.view
	shows := func(placeholder, count, syncing string) func() bool {
		return func() bool { // a download only of what was read
			return tv.placeholderText.text == placeholder && tv.count.text == count && tv.syncing.text == syncing &&
				tv.csvButton.Visible() == (count != "")
		}
	}
	q.untilShows(t, "the expired login's placeholder",
		shows("The pods were not read before the login expired; they load once it is renewed.", "", ""),
		func() string { return tv.placeholderText.text + " | " + tv.count.text + " | " + tv.syncing.text })
	settle(w)
	lookPNG(t, w, "view_unread_login_expired.png")

	f.update(func(u *engine.UIState) {
		u.Connection = engine.Connection{State: engine.Interrupted, Since: fixedNow, Reason: "no answer from the API server within 5s"}
	})
	q.untilShows(t, "the interruption's placeholder",
		shows("The pods were not read before the cluster stopped answering; they load once it answers again.", "", ""),
		func() string { return tv.placeholderText.text + " | " + tv.count.text + " | " + tv.syncing.text })

	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Connected} })
	q.untilShows(t, "loading once connected", shows("Loading pods…", "0 items", "syncing…"),
		func() string { return tv.placeholderText.text + " | " + tv.count.text + " | " + tv.syncing.text })
}

// TestAViewReadBeforeTheLoginExpired: a view read before the login expired keeps its rows and its
// count, as of then (D-58), and says nothing about not being read.
func TestAViewReadBeforeTheLoginExpired(t *testing.T) {
	f := connectedFake(t, 30)
	f.ui.Connection = engine.Connection{State: engine.LoginExpired, Since: fixedNow, CredentialPlugin: true}
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the pods as of the expiry", func() bool { return sh.view.snap != nil && len(sh.view.snap.Rows) == 30 })
	if tv := sh.view; tv.placeholder.Visible() || tv.count.text != "30 items" || tv.syncing.text != "" {
		t.Errorf("placeholder shown %v %q, count %q, syncing %q", tv.placeholder.Visible(), tv.placeholderText.text, tv.count.text, tv.syncing.text)
	}
}

// TestNamespacesStayWithTheirCluster: shown after another cluster, a cluster whose namespaces cannot
// be read offers none of the other cluster's in the namespace filter (it offered acme-main's).
func TestNamespacesStayWithTheirCluster(t *testing.T) {
	f := connectedFake(t, 10)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "demo's namespaces", func() bool { return sh.nsFor == "demo" && len(sh.namespaces) == len(fixtureNamespaces) })
	reads := len(f.callsOf("resources.list"))

	f.mu.Lock()
	f.nsErr = errors.New("the cluster's namespaces are not read yet")
	f.tables["pods"] = unreadPods(t)
	f.mu.Unlock()
	f.update(func(u *engine.UIState) {
		u.Context = "acme-p"
		u.Connection = engine.Connection{State: engine.LoginExpired, Since: fixedNow, CredentialPlugin: true}
	})
	q.until(t, "acme-p's namespaces read", func() bool { return sh.ui.Context == "acme-p" && len(f.callsOf("resources.list")) > reads })
	q.drain() // the read's answer
	if _, names, _ := sh.view.filterChoices(); len(names) != 0 {
		t.Errorf("acme-p's namespace filter offers %q", names)
	}
	if sh.namespaces != nil || sh.nsFor != "" {
		t.Errorf("namespaces %q read for %q kept", sh.namespaces, sh.nsFor)
	}
}
