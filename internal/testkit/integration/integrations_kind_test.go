//go:build kind

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// TestKindIntegrationsQuestion checks the question of a new cluster's integrations (D-76, D-82)
// against the kind cluster: its first connect asks (ui_state's integrationsQuestion) on the
// pre-choice, where nothing is looked for: every integration off, the metrics hub with no provider
// on. Choose integrations with both ticked looks for them: the Metrics Server and the Prometheus `make
// kind-prometheus` installs are detected once without a drawer or a list that shows metrics, so
// that the question says "found" with where; an answer decides the cluster and ends the question.
// Cluster settings and preferences in a temporary folder: the cluster is new to this Cogpit.
func TestKindIntegrationsQuestion(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		ClusterSettings: engine.NewClusterSettingsStore(dir), Preferences: engine.NewPreferencesStore(dir)})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	heard := make(chan struct{}, 8)
	defer s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventIntegrations && ev.View == contextName {
			select {
			case heard <- struct{}{}:
			default:
			}
		}
	})()
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.IntegrationsQuestion != contextName || u.IntegrationsStep != engine.IntegrationsPreChoice {
		t.Fatalf("the first connect asks nothing: %q, %q", u.IntegrationsQuestion, u.IntegrationsStep)
	}
	statuses := func() map[engine.Integration]engine.IntegrationStatus {
		out := map[engine.Integration]engine.IntegrationStatus{}
		for _, st := range s.IntegrationStatuses(contextName) {
			out[st.Name] = st
		}
		return out
	}
	// The pre-choice: nothing on, no metrics provider looked for.
	for name, st := range statuses() {
		if st.On {
			t.Errorf("%s on before the pre-choice was answered: %+v", name, st)
		}
	}
	if b := s.BackendOf(contextName); b != nil && b.Metrics != nil && len(b.Metrics.Providers()) != 0 {
		t.Errorf("metrics providers on before the pre-choice was answered: %+v", b.Metrics.Providers())
	}
	for _, inv := range []engine.Invocation{
		{ID: "cluster.integrations.step", Params: []byte(`{"step":"choose"}`), Origin: engine.OriginGUI},
		{ID: "cluster.integrations.lookFor", Params: []byte(`{"integrations":{"metrics-server":true,"prometheus":true}}`), Origin: engine.OriginGUI},
	} {
		if _, err := s.Dispatch(ctx, inv); err != nil {
			t.Fatal(err)
		}
	}
	for {
		st := statuses()
		ms, prom := st[engine.IntegrationMetricsServer], st[engine.IntegrationPrometheus]
		if ms.State != engine.IntegrationChecking && prom.State != engine.IntegrationChecking {
			if ms.State != engine.IntegrationFound {
				t.Errorf("Metrics Server: %+v", ms)
			}
			// make kind-prometheus installs monitoring/prometheus-server; without it (CI's cluster comes
			// from make kind-up alone) none is found by name, and the user is asked for its address.
			switch {
			case prom.State == engine.IntegrationFound && !strings.HasPrefix(prom.Where, "monitoring/prometheus-server:"):
				t.Errorf("Prometheus found elsewhere: %+v", prom)
			case prom.State != engine.IntegrationFound && prom.State != engine.IntegrationManual:
				t.Errorf("Prometheus: %+v, want found (with make kind-prometheus) or manual", prom)
			}
			t.Logf("Metrics Server %s %q; Prometheus %s %q", ms.State, ms.Where, prom.State, prom.Where)
			break
		}
		select {
		case <-heard:
		case <-ctx.Done():
			t.Fatalf("still checking: %+v", st)
		}
	}
	if b := s.BackendOf(contextName); b != nil && b.Metrics != nil {
		for _, p := range b.Metrics.Providers() {
			if len(p.Serves) > 0 && p.Status != metrics.StatusAvailable {
				t.Errorf("provider %s serves %v while %s", p.Provider, p.Serves, p.Status)
			}
		}
	}

	raw := []byte(`{"integrations":{"alertmanager":false}}`)
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.integrations.answer", Params: raw, Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	u := s.UIState()
	if u.IntegrationsQuestion != "" || !u.IntegrationsDecided(contextName) || u.IntegrationOn(contextName, engine.IntegrationAlertmanager) {
		t.Errorf("after the answer: question %q, decided %v", u.IntegrationsQuestion, u.IntegrationsDecided(contextName))
	}
}
