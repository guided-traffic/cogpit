package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// The question of a new cluster's integrations (D-76): asked for the shown, connected cluster
// without a choice; not for a decided one, the demo cluster, a session without cluster settings, a
// cluster not connected, or one put off this run.
func TestIntegrationsQuestionSync(t *testing.T) {
	none := IntegrationSet(0)
	live := func() UIState {
		return UIState{Context: "ctx", Connection: Connection{State: Connected}}
	}
	for _, c := range []struct {
		what  string
		u     UIState
		asks  bool
		later map[string]bool
		want  string
	}{
		{"undecided and connected", live(), true, nil, "ctx"},
		{"interrupted, still connected", UIState{Context: "ctx", Connection: Connection{State: Interrupted}}, true, nil, "ctx"},
		{"connecting", UIState{Context: "ctx", Connection: Connection{State: Connecting}}, true, nil, ""},
		{"disconnected", UIState{Context: "ctx", Connection: Connection{State: Disconnected}}, true, nil, ""},
		{"no cluster settings kept", live(), false, nil, ""},
		{"put off", live(), true, map[string]bool{"ctx": true}, ""},
		{"another put off", live(), true, map[string]bool{"other": true}, "ctx"},
		{"decided, all on", func() UIState {
			u := live()
			u.ClusterSettings = map[string]ClusterSettings{"ctx": {IntegrationsOff: &none}}
			return u
		}(), true, nil, ""},
		{"the demo cluster", UIState{Context: "ctx", Connection: Connection{State: Connected, Demo: true}}, true, nil, ""},
	} {
		u := c.u
		u.IntegrationsQuestion = "stale"
		u.syncIntegrationsQuestion(c.asks, c.later)
		if u.IntegrationsQuestion != c.want {
			t.Errorf("%s: asked %q, want %q", c.what, u.IntegrationsQuestion, c.want)
		}
	}

	on, off, err := ParseIntegrationSwitches(map[string]bool{"alertmanager": false, "Trivy": true})
	if err != nil || on != IntTrivy || off != IntAlertmanager {
		t.Errorf("switches: %v %v %v", on, off, err)
	}
	if _, _, err := ParseIntegrationSwitches(map[string]bool{"falco": true}); err == nil {
		t.Error("an unknown integration was accepted")
	}
	if _, _, err := ParseIntegrationSwitches(map[string]bool{"kyverno": true, "Kyverno": false}); err == nil {
		t.Error("one integration both on and off was accepted")
	}
}

// While the question or the cluster's Settings › Integrations shows a connected cluster, its
// metrics providers nothing looked for are detected once, and the window hears of it; nothing is
// detected for a cluster nothing shows, or one interrupted.
func TestIntegrationsShownDetectOnce(t *testing.T) {
	var detected atomic.Int64
	det := metrics.DetectorFunc(func(context.Context) (metrics.Provider, metrics.State) {
		detected.Add(1)
		return onePodProvider{}, metrics.State{Status: metrics.StatusAvailable}
	})
	hub := metrics.NewHub(det, metrics.Options{ListInterval: time.Hour})
	defer hub.Close()
	s := New(Options{})
	defer s.Close()
	heard := make(chan string, 4)
	defer s.Subscribe(func(ev Event) {
		if ev.Type == EventIntegrations {
			select { // never block the goroutine that emits
			case heard <- ev.View:
			default:
			}
		}
	})()
	st := &state{s: s}
	b := &Backend{Context: "ctx", Metrics: hub}
	shown := UIState{Context: "ctx", Connection: Connection{State: Connected}}

	detectShownIntegrations(st, b, shown) // neither the question nor the section
	interrupted := shown
	interrupted.Connection.State, interrupted.IntegrationsQuestion = Interrupted, "ctx"
	detectShownIntegrations(st, b, interrupted)
	if n := detected.Load(); n != 0 {
		t.Fatalf("detected %d times with nothing shown or the cluster interrupted", n)
	}

	section := shown
	section.Settings = &SettingsPage{Page: SettingsCluster, Context: "ctx", Section: SectionIntegrations}
	detectShownIntegrations(st, b, section)
	select {
	case ctx := <-heard:
		if ctx != "ctx" {
			t.Errorf("heard of %q", ctx)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the window never heard of the detection")
	}
	asked := shown
	asked.IntegrationsQuestion = "ctx"
	detectShownIntegrations(st, b, asked) // detected already: nothing again
	if n := detected.Load(); n != 1 {
		t.Errorf("detected %d times", n)
	}
}
