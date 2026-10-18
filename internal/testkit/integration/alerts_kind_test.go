//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// The Alertmanager integration against a real Alertmanager (D-77): the kind cluster's (make
// kind-alertmanager, namespace cogpit-alerts), found by its Service's name among the cached
// Services, read through the API server's service proxy. The test posts its own alerts to it
// (through the service proxy, as the cluster's admin: a test setup; Cogpit itself only GETs) and
// resolves them at its end. The counts are what the pill shows: severity none and Watchdog left out,
// major as warning.
func TestKindAlertmanager(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Kube().CoreV1().Services("cogpit-alerts").Get(ctx, "cogpit-alertmanager", metav1.GetOptions{}); err != nil {
		t.Skip("the kind cluster has no Alertmanager: run make kind-alertmanager")
	}
	post := func(ends time.Time) {
		t.Helper()
		now := time.Now()
		alert := func(name, severity string, extra map[string]string) map[string]any {
			labels := map[string]string{"alertname": name, "namespace": "cogpit-alerts", "source": "cogpit-kind-test"}
			if severity != "" {
				labels["severity"] = severity
			}
			for k, v := range extra {
				labels[k] = v
			}
			return map[string]any{"labels": labels, "startsAt": now.Add(-time.Hour).Format(time.RFC3339),
				"endsAt": ends.Format(time.RFC3339), "annotations": map[string]string{"summary": name + " fires",
					"runbook_url": "https://runbooks.example.com/" + name}}
		}
		body, _ := json.Marshal([]any{
			alert("CogpitKindCritical", "critical", map[string]string{"deployment": "cogpit-alertmanager"}),
			alert("CogpitKindCritical", "critical", map[string]string{"deployment": "other"}),
			alert("CogpitKindWarning", "warning", nil),
			alert("CogpitKindMajor", "major", nil),
			alert("CogpitKindUnlabelled", "", nil),
			alert("CogpitKindInfo", "info", nil),
			alert("Watchdog", "none", nil),
			alert("CogpitKindNone", "none", nil),
		})
		res := conn.Kube().CoreV1().RESTClient().Post().Namespace("cogpit-alerts").Resource("services").
			Name("cogpit-alertmanager:http").SubResource("proxy").Suffix("api/v2/alerts").SetHeader("Content-Type", "application/json").Body(body).Do(ctx)
		if err := res.Error(); err != nil {
			t.Fatalf("posting the test alerts: %v", err)
		}
	}
	post(time.Now().Add(10 * time.Minute))
	defer post(time.Now()) // resolved: they leave the active alerts

	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		SnapshotInterval: 20 * time.Millisecond, ClusterSettings: engine.NewClusterSettingsStore(""),
		Alerts: alerting.Options{Interval: time.Second},
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	decideIntegrations(t, s, contextName) // Alertmanager read (D-82)
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	var u engine.UIState
	for u = s.UIState(); u.Alerts == nil || !u.Alerts.Answered || u.Alerts.Total < 6; u = s.UIState() {
		if ctx.Err() != nil {
			t.Fatalf("no answer: %+v", u.Alerts)
		}
		time.Sleep(50 * time.Millisecond)
	}
	got := *u.Alerts
	if got.State != engine.AlertsFound || got.Where != "cogpit-alerts/cogpit-alertmanager:9093" || got.Critical != 2 ||
		got.Warning != 3 || got.Info != 1 || got.Total != 6 || got.Worst != "critical" {
		t.Errorf("the pill: %+v", got)
	}
	list, err := s.AlertList("", engine.AlertListParams{Severities: []string{"critical"}})
	if err != nil || len(list.Alerts) != 2 {
		t.Fatalf("critical: %+v, %v", list, err)
	}
	var linked bool
	for _, it := range list.Alerts {
		for _, o := range it.Objects {
			if o.Ref.Kind == "deployments" && o.Ref.Namespace == "cogpit-alerts" && o.Ref.Name == "cogpit-alertmanager" {
				linked = true
			}
		}
		if it.Runbook == "" || it.Summary != "CogpitKindCritical fires" {
			t.Errorf("critical alert %+v", it)
		}
	}
	if !linked {
		t.Errorf("no alert links its Deployment: %+v", list.Alerts)
	}

	// One GET a poll, nothing else.
	before := s.Backend().Conn.Counter().Snapshot().ByVerbResource["get services/proxy"]
	time.Sleep(3500 * time.Millisecond)
	sent := s.Backend().Conn.Counter().Snapshot().ByVerbResource["get services/proxy"] - before
	if sent < 2 || sent > 5 {
		t.Errorf("%d proxy requests in 3.5 s at a 1 s interval", sent)
	}
}
