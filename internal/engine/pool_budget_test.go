package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// fakeClusterKubeconfig writes a kubeconfig for one fake API server whose cluster and user entries
// carry the context's name, so that two such files merge into two clusters.
func fakeClusterKubeconfig(t *testing.T, srv *testkit.FakeAPIServer, contextName string) string {
	t.Helper()
	doc := strings.ReplaceAll(string(srv.Kubeconfig(contextName, "")), " fake", " fake-"+contextName)
	p := filepath.Join(t.TempDir(), contextName+".yaml")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// podReads counts the pod lists a fake API server served: plain lists and streaming ones.
func podReads(srv *testkit.FakeAPIServer) int64 {
	r := srv.Requests()
	return r["list pods"] + r["watchlist pods"]
}

// TestSwitchingClustersOnARealStack is D-69's budget on client-go against two fake API servers:
// showing a connected cluster again within the cache's warm period sends it no list (P10 for a
// cluster switch), and the other cluster's connection stays open meanwhile; once the warm period
// ended, the cluster is cold and showing it again lists its pods anew. The two halves have
// sessions of their own: within a warm period of a minute, which no connect under the race
// detector outlasts, and past one of 200 ms.
func TestSwitchingClustersOnARealStack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	alpha := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(alpha.Close)
	beta := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(beta.Close)
	alpha.Set(testkit.PodsGVR, testkit.Pod("default", "alpha-web"))
	beta.Set(testkit.PodsGVR, testkit.Pod("default", "beta-web"))
	session := func(warm time.Duration) *Session {
		s := New(Options{
			Connector: &ClusterConnector{KubeconfigPaths: []string{fakeClusterKubeconfig(t, alpha, "alpha"), fakeClusterKubeconfig(t, beta, "beta")},
				WarmPeriod: warm},
			SnapshotInterval: 10 * time.Millisecond,
		})
		t.Cleanup(s.Close)
		return s
	}
	show := func(s *Session, context string) {
		t.Helper()
		if err := s.Connect(ctx, context); err != nil {
			t.Fatal(err)
		}
		waitTable(t, s, "pods", func(tb *TableSnapshot) bool {
			return tb.Synced && tb.Context == context && len(tb.Rows) == 1
		})
	}

	s := session(time.Minute)
	show(s, "alpha")
	listed := podReads(alpha)
	if listed == 0 {
		t.Fatalf("alpha's pods were not listed: %v", alpha.Requests())
	}
	show(s, "beta")
	if alpha.OpenWatches() == 0 {
		t.Error("alpha's watches closed when beta was shown, within the warm period")
	}
	show(s, "alpha")
	if got := podReads(alpha); got != listed {
		t.Errorf("showing alpha again within the warm period listed its pods %d more times: %v", got-listed, alpha.Requests())
	}
	s.Close()
	if !testkit.WaitFor(ctx, func() bool { return alpha.OpenWatches() == 0 && beta.OpenWatches() == 0 }) {
		t.Fatalf("the first session's watches stay open: %v, %v", alpha.OpenWatchesBy(), beta.OpenWatchesBy())
	}

	s = session(200 * time.Millisecond)
	show(s, "alpha")
	listed = podReads(alpha)
	show(s, "beta")
	waitUntil(t, "alpha to turn cold", func() bool {
		c, ok := s.UIState().ConnectionOf("alpha")
		return ok && c.Cold
	})
	if !testkit.WaitFor(ctx, func() bool { return alpha.OpenWatchesBy()["pods"] == 0 }) {
		t.Errorf("alpha's pods are still watched while it is cold: %v", alpha.OpenWatchesBy())
	}
	show(s, "alpha")
	if podReads(alpha) == listed {
		t.Error("showing the cold alpha listed nothing")
	}
	if u := s.UIState(); len(u.Connections) != 2 {
		t.Errorf("connections %+v", u.Connections)
	}
}
