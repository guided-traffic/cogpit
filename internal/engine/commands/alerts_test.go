package commands_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// alertHarness is a session over a fake API server whose monitoring/kube-prometheus-alertmanager
// serves testkit.SampleAlerts through the service proxy, connected, its first poll answered.
func alertHarness(t *testing.T, confirm engine.Confirmer) *harness {
	t.Helper()
	all := []testkit.FakeRule{{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: &testkit.FakeAccess{Rules: map[string][]testkit.FakeRule{"": all}}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.ServicesGVR, testkit.Object("v1", "Service", "monitoring", "kube-prometheus-alertmanager", map[string]any{
		"spec": map[string]any{"ports": []any{map[string]any{"name": "http-web", "port": int64(9093)}}}}))
	am := &testkit.FakeAlertmanager{}
	am.Set(testkit.SampleAlerts(time.Now())...)
	srv.SetProxy("monitoring", "kube-prometheus-alertmanager:http-web", am)
	ms, err := engine.NewMetricsSettings(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")}, MetricsSettings: ms},
		SnapshotInterval: 20 * time.Millisecond, ClusterSettings: engine.NewClusterSettingsStore(""), Confirmer: confirm,
		Alerts: alerting.Options{Interval: 50 * time.Millisecond},
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	decideAll(t, s, "fake") // Alertmanager read (D-82)
	if err := s.Connect(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for u := s.UIState(); u.Alerts == nil || !u.Alerts.Answered; u = s.UIState() {
		if time.Now().After(deadline) {
			t.Fatalf("no answer: %+v", u.Alerts)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return &harness{s: s}
}

// The alert commands as agents and the window use them: alerts.list and alerts.board read the
// last answer, alerts.open shows the board, alerts.fold folds it, cluster.alertmanager reads the
// setting and an agent's cluster.setAlertmanager always waits for the user (as A16's Prometheus);
// with the integration off they are refused with the reason.
func TestAlertCommands(t *testing.T) {
	var asked []engine.ConfirmRequest
	h := alertHarness(t, engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return false, nil
	}))
	agent := engine.OriginExternalAgent

	var list engine.AlertList
	h.must(t, agent, "alerts.list", map[string]any{"severities": []string{"critical", "info"}}, &list)
	if list.Total != 24 || list.Matched != 6 || list.Critical != 3 || list.State != engine.AlertsFound {
		t.Errorf("alerts.list: %d of %d, %+v", list.Matched, list.Total, list.State)
	}
	if err := h.run(t, agent, "alerts.list", map[string]any{"severities": []string{"major"}}, nil); err == nil {
		t.Error("severity major accepted: it shows as warning")
	}
	var board engine.AlertBoardModel
	h.must(t, agent, "alerts.board", map[string]any{}, &board)
	if board.Matched != 24 || len(board.Groups) != 3 {
		t.Errorf("alerts.board: %d in %d groups", board.Matched, len(board.Groups))
	}

	var u engine.UIState
	h.must(t, agent, "alerts.open", map[string]any{}, &u)
	if u.View != engine.AlertBoard {
		t.Errorf("alerts.open shows %q", u.View)
	}
	h.must(t, agent, "alerts.fold", map[string]any{"key": "warning/KubePodCrashLooping", "folded": true}, &u)
	if len(u.AlertBoard.Folded) != 1 || u.AlertBoard.Folded[0] != "warning/KubePodCrashLooping" {
		t.Errorf("folded %+v", u.AlertBoard)
	}
	if err := h.run(t, agent, "alerts.fold", map[string]any{"key": " "}, nil); err == nil {
		t.Error("a fold without a key")
	}
	h.must(t, agent, "alerts.refresh", map[string]any{}, nil)

	var set engine.AlertmanagerSetting
	h.must(t, agent, "cluster.alertmanager", map[string]any{}, &set)
	if !set.Auto || set.State != engine.AlertsFound || set.InUse != "monitoring/kube-prometheus-alertmanager:9093" {
		t.Errorf("cluster.alertmanager %+v", set)
	}
	h.s.SetAgentWrites(h.s.UIState().Context, true)
	params := map[string]any{"address": "monitoring/lookalike:9093", "https": true}
	if err := h.run(t, agent, "cluster.setAlertmanager", params, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an agent's setAlertmanager: %v, want ErrDeclined", err)
	}
	if len(asked) != 1 || !asked[0].AgentsAsk || !strings.Contains(asked[0].Detail, "monitoring/lookalike:9093") ||
		!strings.Contains(asked[0].Detail, "https") {
		t.Errorf("asked %+v", asked)
	}
	h.must(t, agent, "cluster.alertmanager", map[string]any{}, &set)
	if !set.Auto {
		t.Errorf("a declined change was set: %+v", set)
	}
	h.must(t, engine.OriginGUI, "cluster.setAlertmanager", map[string]any{"address": "monitoring/kube-prometheus-alertmanager:http-web"}, &set)
	if set.Auto || set.Address != "monitoring/kube-prometheus-alertmanager:http-web" || len(asked) != 1 {
		t.Errorf("the window's setAlertmanager: %+v, asked %d", set, len(asked))
	}

	// Off: refused with the reason, and nothing in ui_state.
	h.s.UpdateUI(func(u *engine.UIState) {
		if u.ClusterSettings == nil {
			u.ClusterSettings = map[string]engine.ClusterSettings{}
		}
		cs := u.ClusterSettings[u.Context]
		cs.SetIntegrations(0, 0, engine.IntAlertmanager)
		u.ClusterSettings[u.Context] = cs
	})
	for _, id := range []string{"alerts.list", "alerts.board", "alerts.open", "alerts.refresh"} {
		if err := h.run(t, agent, id, map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "Alertmanager integration is off") {
			t.Errorf("%s with the integration off: %v", id, err)
		}
	}
	if h.s.UIState().Alerts != nil {
		t.Error("the pill's state with the integration off")
	}
}
