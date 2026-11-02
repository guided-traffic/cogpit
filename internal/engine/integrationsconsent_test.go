package engine

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// podSource is a cluster with one pod.
func podSource() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("default", "web"))
	return src
}

// decideIntegrations makes a context's choice of integrations "all on", as a cluster the user
// answered for: tests that read an integration in a session that keeps cluster settings, where an
// undecided cluster reads none (D-82).
func decideIntegrations(t testing.TB, s *Session, context string) {
	t.Helper()
	if _, err := s.ChangeClusterSettings(context, func(cs *ClusterSettings) error {
		none := IntegrationSet(0)
		cs.IntegrationsOff = &none
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// What applies to a cluster without a choice (D-82): in a session that asks, nothing but what its
// question ticks while it asks; in one that does not (the CLI's), and for the demo cluster, the
// default for new clusters; a decided cluster its choice.
func TestIntegrationsOffUntilAnswered(t *testing.T) {
	trivyOff := IntTrivy
	base := func() UIState {
		return UIState{Context: "ctx", Connection: Connection{State: Connected}, IntegrationsAskFirst: true,
			Preferences: &Preferences{IntegrationsOff: IntKyverno}}
	}
	onOf := func(u UIState, ctx string) IntegrationSet {
		var on IntegrationSet
		for _, in := range Integrations {
			if u.IntegrationOn(ctx, in.Name) {
				on |= in.Set
			}
		}
		return on
	}
	asked := base()
	asked.IntegrationsQuestion, asked.IntegrationsLookFor = "ctx", IntPrometheus|IntTrivy
	cli := base()
	cli.IntegrationsAskFirst = false
	demo := base()
	demo.Connection.Demo = true
	pooledDemo := base()
	pooledDemo.Connections = []ConnectedCluster{{Context: "other", Connection: Connection{State: Connected, Demo: true}}}
	decided := base()
	decided.ClusterSettings = map[string]ClusterSettings{"ctx": {IntegrationsOff: &trivyOff}}
	for _, c := range []struct {
		what   string
		u      UIState
		ctx    string
		want   IntegrationSet
		source string
	}{
		{"undecided, not asked (Later, or not shown)", base(), "ctx", 0, "none"},
		{"asked, Prometheus and Trivy ticked", asked, "ctx", IntPrometheus | IntTrivy, "prometheus"},
		{"another cluster while one is asked", asked, "other", 0, "none"},
		{"a session that does not ask", cli, "ctx", IntAll &^ IntKyverno, "auto"},
		{"the demo cluster shown", demo, "ctx", IntAll &^ IntKyverno, "auto"},
		{"the demo cluster in the background", pooledDemo, "other", IntAll &^ IntKyverno, "auto"},
		{"decided", decided, "ctx", IntAll &^ IntTrivy, "auto"},
	} {
		if got := onOf(c.u, c.ctx); got != c.want {
			t.Errorf("%s: on %v, want %v", c.what, IntegrationNames(got), IntegrationNames(c.want))
		}
		if got := c.u.MetricsSourceOf(c.ctx); got != c.source {
			t.Errorf("%s: metrics source %q, want %q", c.what, got, c.source)
		}
	}
}

// countingConnector connects "test" with a metrics hub whose one provider counts its detections.
type countingConnector struct {
	*fakeConnector
	detected *atomic.Int64
}

func (c countingConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.fakeConnector.Connect(ctx, name)
	if err == nil {
		b.Metrics = metrics.NewHub(metrics.DetectorFunc(func(context.Context) (metrics.Provider, metrics.State) {
			c.detected.Add(1)
			return onePodProvider{}, metrics.State{Status: metrics.StatusAvailable}
		}), metrics.Options{ListInterval: 20 * time.Millisecond, ObjectInterval: 20 * time.Millisecond})
	}
	return b, err
}

// consentSession connects "test", new to a session that keeps cluster settings and preferences,
// whose default for new clusters switches Kyverno off; detected counts the metrics detections.
func consentSession(t *testing.T) (*Session, *atomic.Int64) {
	t.Helper()
	detected := &atomic.Int64{}
	dir := t.TempDir()
	s := New(Options{
		Connector: countingConnector{fakeConnector: &fakeConnector{src: podSource(),
			profiles: []*model.KindProfile{builtinProfile(t, "pods"), testEvents}}, detected: detected},
		ClusterSettings:  NewClusterSettingsStore(filepath.Join(dir, "settings")),
		Preferences:      NewPreferencesStore(filepath.Join(dir, "prefs")),
		SnapshotInterval: 5 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	if _, err := s.ChangePreferences(func(p *Preferences) error { p.IntegrationsOff = IntKyverno; return nil }); err != nil {
		t.Fatal(err)
	}
	connectTest(t, s)
	return s, detected
}

// The pre-choice and consent before looking (D-82): a new cluster's question opens on the
// pre-choice and nothing is looked for, a Pods view open behind it included; Choose looks for what
// is ticked at once, the pre-choice again for none, and ticks one at a time;
// Don't use's Yes answers with every one off; Ask again forgets the choice and asks from the
// pre-choice; Later reads nothing, and Ask again brings the question back.
func TestIntegrationsQuestionSteps(t *testing.T) {
	s, detected := consentSession(t)
	u := s.UIState()
	if u.IntegrationsQuestion != "test" || u.IntegrationsStep != IntegrationsPreChoice || u.IntegrationsLookFor != 0 {
		t.Fatalf("after the first connect: %q, step %q, %v", u.IntegrationsQuestion, u.IntegrationsStep, IntegrationNames(u.IntegrationsLookFor))
	}
	openView(s, "pods")
	time.Sleep(150 * time.Millisecond) // the view polls every 20 ms: none of it may reach the metrics
	if n := detected.Load(); n != 0 {
		t.Fatalf("metrics looked for %d times before the pre-choice was answered", n)
	}
	if err := s.LookForIntegrations("test", IntTrivy, 0); !errors.Is(err, ErrIntegrationsNotRows) {
		t.Errorf("a tick on the pre-choice: %v", err)
	}
	if err := s.StepIntegrationsQuestion("other", IntegrationsChoose); !errors.Is(err, ErrNoIntegrationsQuestion) {
		t.Errorf("a step for a cluster not asked: %v", err)
	}

	// Choose, every one but Kyverno ticked: looked for at once.
	if err := s.StepIntegrationsQuestion("test", IntegrationsChoose); err != nil {
		t.Fatal(err)
	}
	if err := s.LookForIntegrations("test", IntAll&^IntKyverno, 0); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.IntegrationsLookFor != IntAll&^IntKyverno || !u.IntegrationOn("test", IntegrationTrivy) || u.IntegrationOn("test", IntegrationKyverno) {
		t.Errorf("ticked: %v", IntegrationNames(u.IntegrationsLookFor))
	}
	waitUntil(t, "the metrics looked for", func() bool { return detected.Load() > 0 })

	// Back to the pre-choice: nothing looked for; then Choose, one tick.
	if err := s.StepIntegrationsQuestion("test", IntegrationsPreChoice); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.IntegrationsLookFor != 0 || u.MetricsSourceOf("test") != "none" {
		t.Errorf("back on the pre-choice: %v, source %q", IntegrationNames(u.IntegrationsLookFor), u.MetricsSourceOf("test"))
	}
	if err := s.StepIntegrationsQuestion("test", IntegrationsChoose); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.IntegrationsStep != IntegrationsChoose || u.IntegrationsLookFor != 0 {
		t.Errorf("Choose: %q, %v", u.IntegrationsStep, IntegrationNames(u.IntegrationsLookFor))
	}
	if err := s.LookForIntegrations("test", IntMetricsServer|IntAlertmanager, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.LookForIntegrations("test", 0, IntAlertmanager); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.IntegrationsLookFor != IntMetricsServer || u.MetricsSourceOf("test") != "metrics-server" {
		t.Errorf("ticked the Metrics Server: %v, source %q", IntegrationNames(u.IntegrationsLookFor), u.MetricsSourceOf("test"))
	}

	// Don't use: the warning looks for nothing; its Yes answers with every one off.
	if err := s.StepIntegrationsQuestion("test", IntegrationsDontUse); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.IntegrationsStep != IntegrationsDontUse || u.IntegrationsLookFor != 0 {
		t.Errorf("the warning: %q, %v", u.IntegrationsStep, IntegrationNames(u.IntegrationsLookFor))
	}
	if _, err := s.AnswerIntegrations("test", 0, IntAll, false); err != nil {
		t.Fatal(err)
	}
	u = s.UIState()
	if u.IntegrationsQuestion != "" || u.IntegrationsStep != "" || !u.IntegrationsDecided("test") || u.MetricsSourceOf("test") != "none" {
		t.Fatalf("answered without integrations: %q, %q, decided %v, source %q", u.IntegrationsQuestion, u.IntegrationsStep,
			u.IntegrationsDecided("test"), u.MetricsSourceOf("test"))
	}
	if p, _ := s.Preferences(); p.IntegrationsOff != IntKyverno {
		t.Errorf("the default changed: %v", IntegrationNames(p.IntegrationsOff))
	}

	// Ask again: the choice is forgotten, the question opens on the pre-choice.
	if err := s.AskIntegrationsAgain("test"); err != nil {
		t.Fatal(err)
	}
	u = s.UIState()
	if u.IntegrationsDecided("test") || u.IntegrationsQuestion != "test" || u.IntegrationsStep != IntegrationsPreChoice {
		t.Errorf("asked again: decided %v, %q, %q", u.IntegrationsDecided("test"), u.IntegrationsQuestion, u.IntegrationsStep)
	}
	if err := s.CanAskIntegrationsAgain("test"); !errors.Is(err, ErrIntegrationsAskedNow) {
		t.Errorf("Ask again while asked: %v", err)
	}

	// Later: nothing read; Ask again brings the question back this run.
	if err := s.StepIntegrationsQuestion("test", IntegrationsChoose); err != nil {
		t.Fatal(err)
	}
	if err := s.LookForIntegrations("test", IntAll, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOffIntegrationsQuestion("test"); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.IntegrationsQuestion != "" || u.IntegrationsLookFor != 0 || u.MetricsSourceOf("test") != "none" ||
		u.IntegrationOn("test", IntegrationTrivy) {
		t.Errorf("put off: %q, %v, source %q", u.IntegrationsQuestion, IntegrationNames(u.IntegrationsLookFor), u.MetricsSourceOf("test"))
	}
	if err := s.AskIntegrationsAgain("test"); err != nil {
		t.Fatalf("Ask again after Later: %v", err)
	}
	if u := s.UIState(); u.IntegrationsQuestion != "test" || u.IntegrationsStep != IntegrationsPreChoice {
		t.Errorf("asked again after Later: %q, %q", u.IntegrationsQuestion, u.IntegrationsStep)
	}
}

// Ask again is not for the demo cluster nor a session that keeps no cluster settings; a session
// that does not ask (the CLI's) reads an undecided cluster's integrations as the default says.
func TestIntegrationsAskAgainRefused(t *testing.T) {
	cli := New(Options{Connector: &fakeConnector{src: podSource(), profiles: testProfiles()}})
	t.Cleanup(cli.Close)
	connectTest(t, cli)
	if err := cli.CanAskIntegrationsAgain("test"); !errors.Is(err, ErrNoClusterSettings) {
		t.Errorf("without cluster settings: %v", err)
	}
	if u := cli.UIState(); u.IntegrationsQuestion != "" || !u.IntegrationOn("test", IntegrationTrivy) {
		t.Errorf("the CLI's session: asked %q, Trivy on %v", u.IntegrationsQuestion, u.IntegrationOn("test", IntegrationTrivy))
	}
	s := New(Options{ClusterSettings: NewClusterSettingsStore("")})
	t.Cleanup(s.Close)
	if DemoBuilt {
		if err := s.CanAskIntegrationsAgain(DemoContext); !errors.Is(err, ErrDemoNotAsked) {
			t.Errorf("the demo cluster: %v", err)
		}
	}
}
