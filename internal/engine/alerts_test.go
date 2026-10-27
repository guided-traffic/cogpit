package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// amService is a Service named and ported as kube-prometheus-stack names its Alertmanager's.
func amService(namespace, name string) *unstructured.Unstructured {
	return testkit.Object("v1", "Service", namespace, name, map[string]any{"spec": map[string]any{
		"type": "ClusterIP", "clusterIP": "10.96.0.93",
		"ports": []any{
			map[string]any{"name": "http-web", "port": int64(9093), "protocol": "TCP"},
			map[string]any{"name": "reloader-web", "port": int64(8080), "protocol": "TCP"},
		},
	}})
}

// alertStack is a fake API server whose monitoring/kube-prometheus-alertmanager serves
// testkit.SampleAlerts through the service proxy, and a session with fast polls.
type alertStack struct {
	srv *testkit.FakeAPIServer
	am  *testkit.FakeAlertmanager
	s   *Session
	dir string
}

func newAlertStack(t *testing.T, contexts ...string) *alertStack {
	t.Helper()
	if len(contexts) == 0 {
		contexts = []string{"fake"}
	}
	all := []testkit.FakeRule{{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: &testkit.FakeAccess{Rules: map[string][]testkit.FakeRule{"": all}}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.ServicesGVR, amService("monitoring", "kube-prometheus-alertmanager"))
	for _, ns := range []string{"monitoring", "shop", "flux-system"} {
		srv.Set(testkit.NamespacesGVR, testkit.Namespace(ns))
	}
	am := &testkit.FakeAlertmanager{}
	am.Set(testkit.SampleAlerts(time.Now())...)
	srv.SetProxy("monitoring", "kube-prometheus-alertmanager:http-web", am)
	var paths []string
	for _, c := range contexts {
		paths = append(paths, srv.WriteKubeconfig(t, c, "default"))
	}
	dir := t.TempDir()
	ms, err := NewMetricsSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{
		Connector:        &ClusterConnector{KubeconfigPaths: paths, MetricsSettings: ms},
		SnapshotInterval: 20 * time.Millisecond, ClusterSettings: NewClusterSettingsStore(""),
		Alerts: alerting.Options{Interval: 40 * time.Millisecond, RetryMax: 200 * time.Millisecond},
	})
	t.Cleanup(s.Close)
	for _, c := range contexts {
		decideIntegrations(t, s, c) // Alertmanager read (D-82)
	}
	return &alertStack{srv: srv, am: am, s: s, dir: dir}
}

// waitUI waits until the UI state satisfies cond.
func waitUI(t *testing.T, s *Session, what string, cond func(UIState) bool) UIState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		u := s.UIState()
		if cond(u) {
			return u
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; alerts %+v", what, u.Alerts)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func answered(u UIState) bool { return u.Alerts != nil && u.Alerts.Answered }

// The integration end to end: found by its Service's name among the cached Services, read through
// the service proxy, the pill's counts in the UI state (D-54 changed by D-77), the integration's
// status, alerts.list's items with their objects, and the board grouped, searched, filtered and
// folded.
func TestAlertmanagerIntegration(t *testing.T) {
	st := newAlertStack(t)
	s := st.s
	mustConnect(t, s, "fake")
	u := waitUI(t, s, "the first answer", answered)
	want := AlertsSummary{State: AlertsFound, Where: "monitoring/kube-prometheus-alertmanager:9093", Total: 24, Critical: 3,
		Warning: 18, Info: 3, Worst: "critical", Answered: true}
	if *u.Alerts != want {
		t.Errorf("pill\n got %+v\nwant %+v", *u.Alerts, want)
	}
	b, _ := json.Marshal(u)
	if !strings.Contains(string(b), `"alerts":{"state":"found"`) {
		t.Errorf("ui_state lacks the alerts: %s", b)
	}
	var found IntegrationStatus
	for _, is := range s.IntegrationStatuses(u.Context) {
		if is.Name == IntegrationAlertmanager {
			found = is
		}
	}
	if found.State != IntegrationFound || found.Where != want.Where || !found.On {
		t.Errorf("status %+v", found)
	}

	list, err := s.AlertList("", AlertListParams{})
	if err != nil || list.Total != 24 || list.Matched != 24 || len(list.Alerts) != 24 {
		t.Fatalf("alerts.list: %d of %d, %v", len(list.Alerts), list.Total, err)
	}
	byName := map[string]AlertItem{}
	for i, it := range list.Alerts {
		if i > 0 && alerting.Severity(it.Severity).Rank() < alerting.Severity(list.Alerts[i-1].Severity).Rank() {
			t.Errorf("%s before %s: not by severity", list.Alerts[i-1].Name, it.Name)
		}
		if _, ok := byName[it.Name]; !ok {
			byName[it.Name] = it
		}
	}
	objs := func(name string) string {
		var out []string
		for _, o := range byName[name].Objects {
			out = append(out, o.Label+":"+o.Ref.Kind+":"+o.Ref.Namespace+"/"+o.Ref.Name+":"+o.Title+":"+o.Container)
		}
		return strings.Join(out, " ")
	}
	for name, wantObjs := range map[string]string{
		// The crashing pod is the subject, its container named; kube-state-metrics' service is not.
		"KubePodCrashLooping": "namespace:namespaces:/shop:Namespace: pod:pods:shop/checkout-7d9f8c6b5-2xkqp:Pod:api",
		// A workload's alert names the Deployment, not the exporter's pod beside it.
		"KubeDeploymentReplicasMismatch": "namespace:namespaces:/shop:Namespace: deployment:deployments:shop/checkout:Deployment:",
		// The certificate's Secret in its own namespace, not the exporter's (Secrets not served here).
		"CertificateExpiration": "namespace:namespaces:/shop:Namespace: secret::shop/shop-tls:Secret:",
		// A Flux object by kube-state-metrics' custom resource labels (Kustomizations not served).
		"KustomizationNotReady": "namespace:namespaces:/flux-system:Namespace: customresource::flux-system/apps:Kustomization:",
		// Only the exporter's own pod: no subject.
		"CephPGImbalance": "namespace:namespaces:/rook-ceph:Namespace:",
		// A claim; the exporter's pod beside it is not.
		"KubePersistentVolumeFillingUp": "namespace:namespaces:/shop:Namespace: persistentvolumeclaim::shop/data-postgres-0:PersistentVolumeClaim:",
		// An alert without a severity, its namespace only.
		"BackupJobLate": "namespace:namespaces:/velero:Namespace:",
	} {
		if got := objs(name); got != wantObjs {
			t.Errorf("%s objects\n got %s\nwant %s", name, got, wantObjs)
		}
	}
	if it := byName["KustomizationNotReady"]; it.Severity != "warning" || it.SeverityLabel != "major" {
		t.Errorf("major: %+v", it)
	}
	if it := byName["BackupJobLate"]; it.Severity != "warning" || it.SeverityLabel != "" {
		t.Errorf("no severity: %+v", it)
	}
	if it := byName["KubePodCrashLooping"]; it.Runbook != "https://runbooks.example.com/kubernetes/kubepodcrashlooping" || it.Summary == "" || it.Description == "" {
		t.Errorf("crash looping %+v", it)
	}
	if l, _ := s.AlertList("", AlertListParams{Severities: []string{"critical"}}); l.Matched != 3 || l.Critical != 3 {
		t.Errorf("critical only: %d", l.Matched)
	}
	if l, _ := s.AlertList("", AlertListParams{Namespace: "shop", Filter: "crash", Limit: 2}); l.Matched != 3 || len(l.Alerts) != 2 {
		t.Errorf("shop crash: %d matched, %d returned", l.Matched, len(l.Alerts))
	}

	// The board: groups worst first, a card per alertname with its count.
	board, err := s.AlertBoard("")
	if err != nil || board.Total != 24 || board.Matched != 24 || len(board.Groups) != 3 {
		t.Fatalf("board %d/%d in %d groups, %v", board.Matched, board.Total, len(board.Groups), err)
	}
	var shape []string
	for _, g := range board.Groups {
		shape = append(shape, g.Key+"="+itoa(g.Count))
	}
	if !slices.Equal(shape, []string{"critical=3", "warning=18", "info=3"}) || board.Groups[0].Status != model.StatusError ||
		board.Groups[1].Status != model.StatusWarning || board.Groups[2].Status != model.StatusPending {
		t.Errorf("groups %v", shape)
	}
	var kust *AlertCard
	for i, c := range board.Groups[1].Cards {
		if c.Name == "KustomizationNotReady" {
			kust = &board.Groups[1].Cards[i]
		}
	}
	if kust == nil || kust.Count != 6 || kust.Key != "warning/KustomizationNotReady" || len(kust.Alerts) != 6 {
		t.Errorf("the Kustomizations' card %+v", kust)
	}
	if kust != nil && !kust.Alerts[0].Since.After(kust.Alerts[5].Since) {
		t.Error("a card's alerts are not newest first")
	}

	// The search, the namespace filter and the folding are the UI state's.
	s.UpdateUI(func(u *UIState) {
		u.Filter, u.Namespaces = "checkout", []string{"shop"}
		u.AlertBoard.SetFold("info", true, FoldCard)
		u.AlertBoard.SetFold(kust.Key, true, FoldDescription)
		u.AlertBoard.SetFold(kust.Key, true, FoldRows)
	})
	board, _ = s.AlertBoard("")
	if board.Matched != 4 || board.Total != 24 || len(board.Groups) != 1 || board.Groups[0].Key != "warning" {
		t.Errorf("checkout in shop: %d matched in %v", board.Matched, board.Groups)
	}
	s.UpdateUI(func(u *UIState) { u.Filter, u.Namespaces = "", nil })
	board, _ = s.AlertBoard("")
	for _, g := range board.Groups {
		if g.Folded != (g.Key == "info") {
			t.Errorf("group %s folded %v", g.Key, g.Folded)
		}
		for _, c := range g.Cards {
			if c.Expanded != (c.Key == kust.Key) || c.AllRows != (c.Key == kust.Key) {
				t.Errorf("%s expanded %v, all rows %v", c.Key, c.Expanded, c.AllRows)
			}
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// Switched off, nothing is asked and nothing shows: no pill, no alerts, the commands refused; on
// again, it polls again (D-76's contract).
func TestAlertmanagerOff(t *testing.T) {
	st := newAlertStack(t)
	s := st.s
	mustConnect(t, s, "fake")
	waitUI(t, s, "the first answer", answered)
	setAM := func(on bool) {
		s.UpdateUI(func(u *UIState) {
			if u.ClusterSettings == nil {
				u.ClusterSettings = map[string]ClusterSettings{}
			}
			cs := u.ClusterSettings[u.Context]
			if on {
				cs.SetIntegrations(0, IntAlertmanager, 0)
			} else {
				cs.SetIntegrations(0, 0, IntAlertmanager)
			}
			u.ClusterSettings[u.Context] = cs
		})
	}
	setAM(false)
	u := s.UIState()
	if u.Alerts != nil {
		t.Errorf("the pill with the integration off: %+v", u.Alerts)
	}
	if _, err := s.AlertList("", AlertListParams{}); !errors.Is(err, ErrAlertmanagerOff) {
		t.Errorf("alerts.list off: %v", err)
	}
	if b, _ := json.Marshal(u); strings.Contains(string(b), `"alerts":`) {
		t.Errorf("ui_state holds alerts with the integration off: %s", b)
	}
	time.Sleep(50 * time.Millisecond) // a poll under way when it went off ends
	n := len(st.am.Requests())
	time.Sleep(200 * time.Millisecond)
	if got := len(st.am.Requests()); got != n {
		t.Errorf("%d requests while off", got-n)
	}
	for _, is := range s.IntegrationStatuses(u.Context) {
		if is.Name == IntegrationAlertmanager && (is.On || is.State != IntegrationOff) {
			t.Errorf("status off: %+v", is)
		}
	}
	setAM(true)
	waitUI(t, s, "the answer after on", answered)
	if got := len(st.am.Requests()); got <= n {
		t.Error("no request after the integration went on")
	}
}

// The address the user sets is kept in metrics.json beside the Prometheus settings and asked alone;
// auto finds it by name again.
func TestAlertmanagerAddress(t *testing.T) {
	st := newAlertStack(t)
	s := st.s
	other := &testkit.FakeAlertmanager{}
	other.Set(testkit.SampleAlerts(time.Now())[:2]...)
	st.srv.SetProxy("alerts", "am:9093", other)
	mustConnect(t, s, "fake")
	waitUI(t, s, "the first answer", answered)

	set, err := s.SetAlertmanagerSetting("", &alerting.Address{Namespace: "alerts", Service: "am", Port: "9093", Prefix: "am/"})
	if err != nil || set.Auto || set.Address != "alerts/am:9093" || set.Prefix != "/am" || !set.Kept {
		t.Fatalf("set %+v, %v", set, err)
	}
	other.Prefix("/am")
	waitUI(t, s, "the configured answer", func(u UIState) bool { return u.Alerts != nil && u.Alerts.State == AlertsConfigured })
	if u := s.UIState(); u.Alerts.Total != 2 || u.Alerts.Where != "alerts/am:9093/am" {
		t.Errorf("configured %+v", u.Alerts)
	}
	raw, err := os.ReadFile(filepath.Join(st.dir, MetricsSettingsFile))
	if err != nil || !strings.Contains(string(raw), `"alertmanager"`) || !strings.Contains(string(raw), `"service": "am"`) {
		t.Errorf("metrics.json: %s, %v", raw, err)
	}
	if got, _ := s.AlertmanagerSetting(""); got.State != AlertsConfigured || got.InUse != "alerts/am:9093/am" {
		t.Errorf("setting %+v", got)
	}
	// Prometheus's setting written afterwards keeps the Alertmanager's.
	if _, err := s.SetPrometheusOverride("", nil); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(st.dir, MetricsSettingsFile))
	if !strings.Contains(string(raw), `"alertmanager"`) {
		t.Errorf("the Prometheus setting dropped the Alertmanager's: %s", raw)
	}
	for _, bad := range []alerting.Address{{Namespace: "Bad NS", Service: "am", Port: "9093"}, {Namespace: "a", Service: "am", Port: "99999"},
		{Namespace: "a", Service: "am", Port: "web", Prefix: "../x"}} {
		if _, err := s.SetAlertmanagerSetting("", &bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	if _, err := s.SetAlertmanagerSetting("", nil); err != nil {
		t.Fatal(err)
	}
	waitUI(t, s, "found by name again", func(u UIState) bool { return u.Alerts != nil && u.Alerts.State == AlertsFound && u.Alerts.Total == 24 })
}

// A cluster connected in the background is not polled; shown again, it polls at once (D-69).
func TestAlertmanagerOnlyTheShownCluster(t *testing.T) {
	st := newAlertStack(t, "alpha", "beta")
	s := st.s
	mustConnect(t, s, "alpha")
	waitUI(t, s, "alpha's answer", answered)
	mustConnect(t, s, "beta") // alpha stays connected, not shown
	waitUI(t, s, "beta's answer", func(u UIState) bool { return u.Context == "beta" && answered(u) })
	alpha := s.BackendOf("alpha")
	if alpha == nil {
		t.Fatal("alpha is not pooled")
	}
	p := alpha.alerts.current()
	if p == nil {
		t.Fatal("alpha has no poller")
	}
	asOf := p.Snapshot().AsOf
	time.Sleep(200 * time.Millisecond)
	if !p.Snapshot().AsOf.Equal(asOf) {
		t.Error("the background cluster was polled")
	}
	if err := s.Show("alpha"); err != nil {
		t.Fatal(err)
	}
	waitUI(t, s, "alpha shown", func(u UIState) bool { return u.Context == "alpha" && answered(u) })
	deadline := time.Now().Add(5 * time.Second)
	for !p.Snapshot().AsOf.After(asOf) {
		if time.Now().After(deadline) {
			t.Fatal("alpha not polled once shown")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNoAlertmanager(t *testing.T) {
	if why := noAlertmanager(&Backend{Demo: true}); !strings.Contains(why, "demo") {
		t.Errorf("demo: %q", why)
	}
	if why := noAlertmanager(&Backend{}); why == "" {
		t.Error("a backend without a connection reads an Alertmanager")
	}
	var a AlertBoardState
	a.SetFold("critical", true, FoldCard)
	a.SetFold("critical", true, FoldCard)
	a.SetFold("warning/X", true, FoldDescription)
	a.SetFold("warning/Y", true, FoldRows)
	if !slices.Equal(a.Folded, []string{"critical"}) || !slices.Equal(a.Expanded, []string{"warning/X"}) || !slices.Equal(a.Rows, []string{"warning/Y"}) {
		t.Errorf("%+v", a)
	}
	a.SetFold("critical", false, FoldCard)
	if len(a.Folded) != 0 || a.IsZero() {
		t.Errorf("%+v", a)
	}
	for u, want := range map[string]string{"https://r.example/x": "https://r.example/x", "http://r": "http://r",
		"javascript:alert(1)": "", "file:///etc/passwd": "", "/relative": "", "https://user:pw@r.example": ""} {
		if got := webURL(u); got != want {
			t.Errorf("webURL(%q) = %q", u, got)
		}
	}
}
