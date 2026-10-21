package alerting_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The whole path against a fake API server: the alerts request through client-go's service proxy on
// the connection's counting transport ("get services/proxy"), the filters as query parameters, the
// synthetic set shaped like a real cluster's (testkit.SampleAlerts) counted as the pill counts it,
// and one request per poll: the budget (testing.md, P-AM).
func TestPollerThroughTheServiceProxy(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(srv.Close)
	conn, err := cluster.Connect(context.Background(), cluster.Options{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	am := &testkit.FakeAlertmanager{}
	now := time.Now()
	am.Set(testkit.SampleAlerts(now)...)
	srv.SetProxy("monitoring", "kube-prometheus-alertmanager:9093", am)

	var changes atomic.Int32
	p := alerting.NewPoller(conn.Kube(), alerting.ServiceSourceFunc(func(context.Context) ([]alerting.Service, error) {
		return []alerting.Service{{Namespace: "monitoring", Name: "kube-prometheus-alertmanager",
			Ports: []alerting.ServicePort{{Name: "reloader-web", Port: 8080}, {Name: "http-web", Port: 9093}}}}, nil
	}), alerting.Options{Interval: 50 * time.Millisecond, OnChange: func() { changes.Add(1) }})
	defer p.Close()
	before := conn.Counter().Snapshot().ByVerbResource["get services/proxy"]
	p.SetActive(true)
	deadline := time.Now().Add(5 * time.Second)
	for !p.Snapshot().Answered() {
		if time.Now().After(deadline) {
			t.Fatalf("no answer: %+v", p.Snapshot())
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap := p.Snapshot()
	counts := map[alerting.Severity]int{}
	for _, a := range snap.Alerts {
		counts[a.Severity]++
		if a.Name == "Watchdog" || a.Name == "InfoInhibitor" || a.Name == "KubeJobFailed" || a.Name == "KubeCPUOvercommit" {
			t.Errorf("%s is shown", a.Name)
		}
	}
	if snap.State != alerting.StateFound || snap.Total != 24 || counts[alerting.SeverityCritical] != 3 ||
		counts[alerting.SeverityWarning] != 18 || counts[alerting.SeverityInfo] != 3 {
		t.Errorf("state %s, total %d, by severity %v", snap.State, snap.Total, counts)
	}
	reqs := am.Requests()
	if len(reqs) == 0 || !strings.HasPrefix(reqs[0], "/api/v2/alerts?") || !strings.Contains(reqs[0], "active=true") ||
		!strings.Contains(reqs[0], "silenced=false") || !strings.Contains(reqs[0], "inhibited=false") {
		t.Errorf("requests %q", reqs)
	}
	// One request per poll, nothing else.
	time.Sleep(260 * time.Millisecond)
	p.SetActive(false)
	// A poll under way when the poller stopped is counted as sent before the fake Alertmanager has
	// it: read both again until they agree or a second passed (a loaded CI runner, 2026-10-21).
	var sent, polls int64
	for settleBy := time.Now().Add(time.Second); ; time.Sleep(10 * time.Millisecond) {
		sent = conn.Counter().Snapshot().ByVerbResource["get services/proxy"] - before
		polls = int64(len(am.Requests()))
		if sent == polls || time.Now().After(settleBy) {
			break
		}
	}
	if sent != polls || polls < 3 || polls > 8 {
		t.Errorf("%d proxy requests for %d polls in ~300ms at 50ms", sent, polls)
	}
	if c := changes.Load(); c != 1 {
		t.Errorf("OnChange ran %d times for unchanged answers", c)
	}

	// The service without endpoints: unavailable, the answer kept.
	srv.SetProxy("monitoring", "kube-prometheus-alertmanager:9093", nil)
	p.Refresh()
	p.SetActive(true)
	for p.Snapshot().State != alerting.StateUnavailable {
		if time.Now().After(deadline.Add(5 * time.Second)) {
			t.Fatalf("not unavailable: %+v", p.Snapshot())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s := p.Snapshot(); s.Total != 24 || !strings.Contains(s.Error, "no endpoints available") {
		t.Errorf("unavailable %d, %q", s.Total, s.Error)
	}
}
