//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
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

	// Silences (D-166) against the real Alertmanager: the window's silence is one POST ("create
	// services/proxy"), Alertmanager mutes the matching alerts and lists the silence with them;
	// expiring it is one DELETE ("delete services/proxy") and brings them back.
	counter := func(key string) int64 { return s.Backend().Conn.Counter().Snapshot().ByVerbResource[key] }
	dispatch := func(origin engine.Origin, id string, params any, out any) {
		t.Helper()
		raw, _ := json.Marshal(params)
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: origin})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if out != nil {
			b, _ := json.Marshal(res)
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatal(err)
			}
		}
	}
	creates := counter("create services/proxy")
	var created engine.SilenceCreated
	dispatch(engine.OriginGUI, "alerts.silence", map[string]any{
		"matchers": []map[string]string{{"name": "alertname", "op": "=", "value": "CogpitKindCritical"}, {"name": "source", "op": "=~", "value": "cogpit-kind-.*"}},
		"duration": "10m", "comment": "Cogpit's kind test", "createdBy": "cogpit-kind-test"}, &created)
	if n := counter("create services/proxy") - creates; n != 1 || created.ID == "" || created.Mutes != 2 {
		t.Errorf("created %+v with %d create services/proxy", created, n)
	}
	for u = s.UIState(); u.Alerts == nil || u.Alerts.Critical != 0; u = s.UIState() {
		if ctx.Err() != nil {
			t.Fatalf("the critical alerts are not muted: %+v", u.Alerts)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var page engine.SilencesModel
	dispatch(engine.OriginExternalAgent, "alerts.silences", map[string]any{}, &page)
	var found *engine.SilenceItem
	for _, g := range page.Groups {
		for i := range g.Silences {
			if g.Silences[i].ID == created.ID {
				found = &g.Silences[i]
			}
		}
	}
	if found == nil || found.State != "active" || found.Mutes != 2 || found.CreatedBy != "cogpit-kind-test" || len(found.Matchers) != 2 ||
		found.Matchers[1].Op != "=~" {
		t.Fatalf("the silence on the page: %+v (of %+v)", found, page.Groups)
	}
	deletes := counter("delete services/proxy")
	s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil }))
	dispatch(engine.OriginGUI, "alerts.expireSilence", map[string]any{"id": created.ID}, nil)
	if n := counter("delete services/proxy") - deletes; n != 1 {
		t.Errorf("%d delete services/proxy for one expire", n)
	}
	for u = s.UIState(); u.Alerts == nil || u.Alerts.Critical != 2; u = s.UIState() {
		if ctx.Err() != nil {
			t.Fatalf("the critical alerts do not come back: %+v", u.Alerts)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Alertmanager 0.28 answers an expired silence expired again with 200, an unknown id with 404:
	// the refusal names the request.
	dispatch(engine.OriginGUI, "alerts.expireSilence", map[string]any{"id": created.ID}, nil)
	const unknown = "00000000-0000-4000-8000-000000000999"
	raw, _ := json.Marshal(map[string]any{"id": unknown})
	_, err = s.Dispatch(ctx, engine.Invocation{ID: "alerts.expireSilence", Params: raw, Origin: engine.OriginGUI})
	if err == nil || !strings.Contains(err.Error(), "DELETE ") || !strings.Contains(err.Error(), "/proxy/api/v2/silence/"+unknown) ||
		!strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("an unknown silence: %v", err)
	}
}
