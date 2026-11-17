package commands_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The silence commands as agents and the window use them (D-166): a card's draft, a silence an
// agent asks for always waits for the user even with agent writes allowed, the window's own is
// created at once and its alerts leave the board, the silences page lists it with the alerts it
// mutes, an expire always asks and brings them back; the page's groups fold; the pill opens the
// board without a namespace filter.
func TestSilenceCommands(t *testing.T) {
	var mu sync.Mutex
	var asked []engine.ConfirmRequest
	accept := false
	h, am := alertHarnessAM(t, engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, req)
		return accept, nil
	}))
	agent := engine.OriginExternalAgent
	board := func() engine.AlertBoardModel {
		var b engine.AlertBoardModel
		h.must(t, agent, "alerts.board", map[string]any{}, &b)
		return b
	}
	waitBoard := func(what string, cond func(engine.AlertBoardModel) bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for b := board(); !cond(b); b = board() {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %d alerts", what, b.Total)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// The pill opens the board without the namespace filter of the view before.
	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": "pods"}, nil)
	h.must(t, engine.OriginGUI, "ui.setNamespace", map[string]any{"namespaces": []string{"shop"}}, nil)
	var u engine.UIState
	h.must(t, engine.OriginGUI, "alerts.open", map[string]any{}, &u)
	if u.View != engine.AlertBoard || len(u.Namespaces) != 0 {
		t.Errorf("alerts.open: view %q, namespaces %v", u.View, u.Namespaces)
	}
	h.must(t, engine.OriginGUI, "ui.tab.back", map[string]any{}, &u)
	if u.View != "pods" || len(u.Namespaces) != 1 || u.Namespaces[0] != "shop" {
		t.Errorf("back: view %q, namespaces %v", u.View, u.Namespaces)
	}

	var draft engine.SilenceDraft
	h.must(t, agent, "alerts.silenceDraft", map[string]any{"card": "warning/KubePodCrashLooping"}, &draft)
	if len(draft.Matchers) < 2 || draft.Matchers[0].Name != "alertname" || draft.Matchers[0].Value != "KubePodCrashLooping" ||
		draft.Matches.Shown != 3 || draft.Duration != "2h" || draft.Where != "monitoring/kube-prometheus-alertmanager:9093" {
		t.Fatalf("draft %+v", draft)
	}
	for _, m := range draft.Matchers {
		if m.Name == "pod" {
			t.Errorf("a label the card's alerts do not share: %+v", m)
		}
	}
	// A workload's alert: its alertname, namespace and Deployment; not the exporter's instance, pod,
	// job, endpoint or service, which change when kube-state-metrics restarts.
	h.must(t, agent, "alerts.silenceDraft", map[string]any{"card": "warning/KubeDeploymentReplicasMismatch"}, &draft)
	var names []string
	for _, m := range draft.Matchers {
		names = append(names, m.Name+"="+m.Value)
	}
	if strings.Join(names, ",") != "alertname=KubeDeploymentReplicasMismatch,namespace=shop,deployment=checkout" {
		t.Errorf("a workload's draft: %v", names)
	}
	params := map[string]any{"matchers": []map[string]string{{"name": "alertname", "op": "=", "value": "KubePodCrashLooping"}},
		"duration": "1d", "comment": "the cart rollout", "createdBy": "hans"}

	// An agent's silence asks even with agent writes allowed.
	h.s.SetAgentWrites(h.s.UIState().Context, true)
	if err := h.run(t, agent, "alerts.silence", params, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an agent's silence: %v, want ErrDeclined", err)
	}
	if len(asked) != 1 || !asked[0].AgentsAsk || len(asked[0].Objects) != 1 || asked[0].Objects[0].Name != "kube-prometheus-alertmanager" ||
		!strings.Contains(asked[0].Detail, `alertname="KubePodCrashLooping"`) || !strings.Contains(asked[0].Detail, "3 alerts now") ||
		!strings.Contains(asked[0].Detail, "1d from now") {
		t.Errorf("asked %+v", asked)
	}
	if len(am.Silences()) != 0 {
		t.Fatal("a declined silence was created")
	}

	// The window's own: no question, created, its alerts leave the board.
	var created engine.SilenceCreated
	h.must(t, engine.OriginGUI, "alerts.silence", params, &created)
	if created.ID == "" || created.Mutes != 3 || created.EndsAt.Sub(created.StartsAt) != 24*time.Hour || len(asked) != 1 {
		t.Errorf("created %+v, asked %d", created, len(asked))
	}
	waitBoard("the muted alerts leave the board", func(b engine.AlertBoardModel) bool { return b.Total == 21 })

	var page engine.SilencesModel
	h.must(t, agent, "alerts.silences", map[string]any{}, &page)
	if page.Total != 1 || len(page.Groups) != 1 || page.Groups[0].Key != engine.SilenceGroupActive {
		t.Fatalf("silences %+v", page)
	}
	if x := page.Groups[0].Silences[0]; x.ID != created.ID || x.Mutes != 3 || x.CreatedBy != "hans" || x.Comment != "the cart rollout" ||
		len(x.Alerts) != 3 || x.Alerts[0].Name != "KubePodCrashLooping" {
		t.Errorf("silence %+v", x)
	}

	// An expire always asks; declined, nothing changes.
	if err := h.run(t, engine.OriginGUI, "alerts.expireSilence", map[string]any{"id": created.ID}, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an expire declined: %v", err)
	}
	if last := asked[len(asked)-1]; last.Danger != engine.DangerDestructive || !last.Undoable ||
		!strings.Contains(last.Detail, "the cart rollout") || !strings.Contains(last.Detail, `alertname="KubePodCrashLooping"`) {
		t.Errorf("expire asked %+v", last)
	}
	mu.Lock()
	accept = true
	mu.Unlock()
	h.must(t, engine.OriginGUI, "alerts.expireSilence", map[string]any{"id": created.ID}, nil)
	waitBoard("the alerts come back", func(b engine.AlertBoardModel) bool { return b.Total == 24 })

	// The silences page: opened, its groups fold; the expired one starts folded.
	h.must(t, agent, "alerts.openSilences", map[string]any{}, &u)
	if u.View != engine.AlertSilences {
		t.Errorf("alerts.openSilences shows %q", u.View)
	}
	deadline := time.Now().Add(10 * time.Second)
	for h.must(t, agent, "alerts.silences", map[string]any{}, &page); len(page.Groups) != 1 || page.Groups[0].Key != engine.SilenceGroupExpired; h.must(t, agent, "alerts.silences", map[string]any{}, &page) {
		if time.Now().After(deadline) {
			t.Fatalf("the expired silence: %+v", page.Groups)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !page.Groups[0].Folded {
		t.Error("the expired group starts unfolded")
	}
	h.must(t, agent, "alerts.fold", map[string]any{"key": engine.SilenceGroupExpired}, &u)
	var unfolded engine.SilencesModel // a fresh one: decoding into page keeps an omitted folded's true
	h.must(t, agent, "alerts.silences", map[string]any{}, &unfolded)
	if unfolded.Groups[0].Folded {
		t.Error("the expired group stays folded")
	}

	// Invalid silences are refused before anything is asked or sent.
	n := len(asked)
	for _, bad := range []map[string]any{
		{"matchers": []map[string]string{{"name": "alertname", "op": "=~", "value": ".*"}}, "comment": "c"},
		{"matchers": []map[string]string{{"name": "alertname", "op": "=", "value": "A"}}, "comment": " "},
		{"matchers": []map[string]string{{"name": "alertname", "op": "==", "value": "A"}}, "comment": "c"},
		{"matchers": []map[string]string{{"name": "alertname", "op": "=", "value": "A"}}, "comment": "c", "duration": "soon"},
	} {
		if err := h.run(t, engine.OriginGUI, "alerts.silence", bad, nil); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if len(asked) != n || len(am.Silences()) != 1 {
		t.Errorf("an invalid silence was asked for or sent: %d asked, %d silences", len(asked)-n, len(am.Silences()))
	}
}
