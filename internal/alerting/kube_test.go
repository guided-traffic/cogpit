package alerting_test

import (
	"context"
	"errors"
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
	if len(reqs) == 0 || !strings.HasPrefix(reqs[0], "GET /api/v2/alerts?") || !strings.Contains(reqs[0], "active=true") ||
		!strings.Contains(reqs[0], "silenced=true") || !strings.Contains(reqs[0], "inhibited=false") {
		t.Errorf("requests %q", reqs)
	}
	// One request per poll, nothing else: wait for three polls rather than a fixed time, which a
	// loaded CI runner did not always fill with three (2026-10-22).
	for len(am.Requests()) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%d polls before the deadline", len(am.Requests()))
		}
		time.Sleep(5 * time.Millisecond)
	}
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
		t.Errorf("%d proxy requests for %d polls at 50ms", sent, polls)
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

// Silences through the fake API server's service proxy (D-166): listed while the silences page is
// shown, created with a POST ("create services/proxy"), expired with a DELETE ("delete
// services/proxy"); the alert a silence matches leaves the shown alerts for the muted ones at the
// next poll, which the write starts.
func TestSilencesThroughTheServiceProxy(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(srv.Close)
	conn, err := cluster.Connect(context.Background(), cluster.Options{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	am := &testkit.FakeAlertmanager{}
	am.Set(testkit.SampleAlerts(time.Now())...)
	srv.SetProxy("monitoring", "kube-prometheus-alertmanager:9093", am)
	p := alerting.NewPoller(conn.Kube(), alerting.ServiceSourceFunc(func(context.Context) ([]alerting.Service, error) {
		return []alerting.Service{{Namespace: "monitoring", Name: "kube-prometheus-alertmanager",
			Ports: []alerting.ServicePort{{Name: "http-web", Port: 9093}}}}, nil
	}), alerting.Options{Interval: time.Hour})
	defer p.Close()
	p.SetActive(true)
	p.SetSilences(true)
	deadline := time.Now().Add(5 * time.Second)
	wait := func(what string, cond func(alerting.Snapshot) bool) alerting.Snapshot {
		t.Helper()
		for {
			if s := p.Snapshot(); cond(s) {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %+v", what, p.Snapshot())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	snap := wait("the first answer", func(s alerting.Snapshot) bool { return s.Answered() && !s.SilencesAsOf.IsZero() })
	if snap.Total != 24 || snap.MutedTotal != 1 || len(snap.Silences) != 0 {
		t.Fatalf("total %d, muted %d, silences %d", snap.Total, snap.MutedTotal, len(snap.Silences))
	}

	m, _ := alerting.NewMatcher("alertname", "=", "KubePodCrashLooping")
	counts := func() map[string]int64 { return conn.Counter().Snapshot().ByVerbResource }
	created := counts()["create services/proxy"]
	id, tg, err := p.CreateSilence(context.Background(), alerting.NewSilence{Matchers: []alerting.Matcher{m}, StartsAt: time.Now(),
		EndsAt: time.Now().Add(2 * time.Hour), CreatedBy: "hans", Comment: "the cart rollout"})
	if err != nil || id == "" || tg.Service != "kube-prometheus-alertmanager" {
		t.Fatalf("create: %q %+v %v", id, tg, err)
	}
	if n := counts()["create services/proxy"] - created; n != 1 {
		t.Errorf("%d create services/proxy for one silence", n)
	}
	snap = wait("the silence mutes", func(s alerting.Snapshot) bool { return len(s.Silences) == 1 && s.MutedTotal == 4 })
	if s := snap.Silences[0]; s.ID != id || s.State != alerting.SilenceActive || s.CreatedBy != "hans" || s.Matchers[0].String() != `alertname="KubePodCrashLooping"` {
		t.Errorf("silence %+v", s)
	}
	if snap.Total != 21 {
		t.Errorf("shown %d after muting three", snap.Total)
	}

	deleted := counts()["delete services/proxy"]
	if _, err := p.ExpireSilence(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if n := counts()["delete services/proxy"] - deleted; n != 1 {
		t.Errorf("%d delete services/proxy for one expire", n)
	}
	wait("expired", func(s alerting.Snapshot) bool {
		return s.Total == 24 && len(s.Silences) == 1 && s.Silences[0].State == alerting.SilenceExpired
	})

	// Expiring it again changes nothing (Alertmanager answers 200); an unknown id is refused, and the
	// refusal names the request and quotes Alertmanager.
	if _, err := p.ExpireSilence(context.Background(), id); err != nil {
		t.Errorf("expired again: %v", err)
	}
	const unknown = "00000000-0000-4000-8000-000000000999"
	_, err = p.ExpireSilence(context.Background(), unknown)
	var re *alerting.RequestError
	if !errors.As(err, &re) || !strings.Contains(err.Error(), "DELETE ") || !strings.Contains(err.Error(), "/api/v1/namespaces/monitoring/services/kube-prometheus-alertmanager:9093/proxy/api/v2/silence/"+unknown) ||
		!strings.Contains(err.Error(), "not found (HTTP 404)") || !strings.Contains(err.Error(), "silence "+unknown+" not found") {
		t.Errorf("an unknown silence: %v", err)
	}
}
