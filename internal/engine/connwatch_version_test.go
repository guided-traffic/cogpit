package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// versionedSession connects a session whose API server reads version at the connect, its heartbeats
// answered by the fake prober.
func versionedSession(t *testing.T, version string) (*Session, *fakeProber) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("default", "web-1"))
	p := &fakeProber{versions: []string{version}}
	fc := &fakeConnector{src: src, profiles: testProfiles(), health: p, version: version}
	s := New(Options{Connector: fc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: fastWatch})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s, p
}

// TestTheServerVersionFollowsTheHeartbeat (the owner's colleague's cluster upgraded while Cogpit ran,
// the owner's "Option A" of 2026-10-26): a version two heartbeats read becomes the cluster's; a patch
// sets nothing else, another minor sets Connection.Upgraded (in the shown cluster's entry of
// Connections too) and a line in the connection log, and a return to the connect's minor clears it.
func TestTheServerVersionFollowsTheHeartbeat(t *testing.T) {
	s, p := versionedSession(t, "v1.35.2")
	version := func(u UIState) string {
		if u.ClusterVersion == nil {
			return ""
		}
		return u.ClusterVersion.Version
	}
	waitUI(t, s, "the connect's version", func(u UIState) bool { return version(u) == "v1.35.2" })

	p.set(func(f *fakeProber) { f.versions = []string{"v1.35.3"} })
	u := waitUI(t, s, "the patch", func(u UIState) bool { return version(u) == "v1.35.3" })
	if u.Connection.Upgraded != nil || u.Connection.State != Connected {
		t.Errorf("a patch: %+v", u.Connection)
	}
	if v := s.Backend().ServerVersion(); v != "v1.35.3" {
		t.Errorf("the backend's version %q", v)
	}
	if v := s.Backend().connectVersion(); v != "v1.35.2" {
		t.Errorf("the connect's version %q", v)
	}

	p.set(func(f *fakeProber) { f.versions = []string{"v1.36.1"} })
	u = waitUI(t, s, "the minor", func(u UIState) bool { return u.Connection.Upgraded != nil })
	if up := u.Connection.Upgraded; up.From != "v1.35.2" || up.To != "v1.36.1" || up.Since.IsZero() || version(u) != "v1.36.1" {
		t.Errorf("another minor: %+v, version %q", up, version(u))
	}
	if c, ok := u.ConnectionOf(u.Context); !ok || c.Connection.Upgraded == nil || c.Connection.Upgraded.To != "v1.36.1" {
		t.Errorf("the entry of Connections: %+v", c)
	}
	var log []string
	for _, l := range s.ConnectLog(u.Context) {
		log = append(log, l.Text)
	}
	if all := strings.Join(log, "\n"); !strings.Contains(all, "The API server's version changed from v1.35.3 to v1.36.1 (two heartbeats in a row); "+
		"the connection knows the kinds and API versions of v1.35.2 until a reconnect") ||
		!strings.Contains(all, "changed from v1.35.2 to v1.35.3 (two heartbeats in a row)\n") {
		t.Errorf("the connection log:\n%s", all)
	}

	p.set(func(f *fakeProber) { f.versions = []string{"v1.35.4"} })
	u = waitUI(t, s, "the rollback", func(u UIState) bool { return version(u) == "v1.35.4" })
	if u.Connection.Upgraded != nil {
		t.Errorf("back to the connect's minor: %+v", u.Connection.Upgraded)
	}
}

// TestAVersionThatAlternatesIsNotTaken: during a rolling upgrade the API servers behind one address
// answer with the old and the new version in turn; a version read once is not taken.
func TestAVersionThatAlternatesIsNotTaken(t *testing.T) {
	s, p := versionedSession(t, "v1.35.2")
	p.set(func(f *fakeProber) { f.versions = []string{"v1.36.0", "v1.35.2"} })
	start := p.get().beats
	waitUntil(t, "ten heartbeats", func() bool { return p.get().beats >= start+10 })
	u := s.UIState()
	if u.ClusterVersion == nil || u.ClusterVersion.Version != "v1.35.2" || u.Connection.Upgraded != nil || s.Backend().ServerVersion() != "v1.35.2" {
		t.Errorf("an alternating version was taken: %+v %+v", u.ClusterVersion, u.Connection.Upgraded)
	}
}

func TestSameMinor(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"v1.35.2", "v1.35.3", true},
		{"v1.35.2", "v1.36.0", false},
		{"v1.34.2-gke.1234", "v1.34.9", true},
		{"v1.31.4+k3s1", "v1.32.0+k3s1", false},
		{"", "v1.36.0", true},
		{"v1.35.2", "unknown", true},
	} {
		if got := sameMinor(tc.a, tc.b); got != tc.want {
			t.Errorf("sameMinor(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}
