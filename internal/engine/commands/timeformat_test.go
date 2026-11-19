package commands_test

import (
	"context"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // Europe/Berlin on any test machine

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// builtinTimesConnector serves the built-in Events and Pods profiles in place of the test ones:
// the Events' Last Seen is a time column, a pod's drawer has the kind's container states.
type builtinTimesConnector struct{ src *testkit.MemorySource }

func (c *builtinTimesConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c *builtinTimesConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	profiles := testProfiles()
	for i, p := range profiles {
		if b, ok := kinds.ByKey(p.Key); ok && (p.Key == "events" || p.Key == "pods") {
			profiles[i] = b
		}
	}
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &engine.Backend{Context: name, Namespace: "default", Cache: ch, Profiles: profiles, Close: ch.Close}, nil
}

// TestAgentsReadTimesAsDrawn (D-31): what an agent reads as the window draws it writes absolute
// times in the user's format and the Preferences' time zone, summer time included: an event's Last
// Seen in view.rows and resources.list, a pod's Created and its container's start in object.get's
// drawer. ui.state names the format, so that an agent can tell 10.09. from 9/10; the object's own
// times in object.get's YAML stay RFC 3339.
func TestAgentsReadTimesAsDrawn(t *testing.T) {
	src := seed()
	pod := testkit.Pod("shop", "api-0")
	pod.Object["status"] = map[string]any{"containerStatuses": []any{map[string]any{
		"name": "app", "ready": true, "state": map[string]any{"running": map[string]any{"startedAt": "2026-10-01T12:00:05Z"}},
	}}}
	src.Set(testkit.PodsGVR, pod)
	de := model.TimeLayout{Date: "02.01.2006", Time: "15:04:05"}
	s := engine.New(engine.Options{Connector: &builtinTimesConnector{src: src}, SnapshotInterval: 10 * time.Millisecond,
		TimeLayout: de, Preferences: engine.NewPreferencesStore("")})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s, src: src}
	gui, agent := engine.OriginGUI, engine.OriginExternalAgent
	h.must(t, gui, "preferences.set", map[string]any{"timezone": "Europe/Berlin"}, nil)

	var st commands.StateResult
	h.must(t, agent, "ui.state", nil, &st)
	if st.TimeFormat != de {
		t.Errorf("ui.state's timeFormat %+v", st.TimeFormat)
	}

	// The seed's event: lastTimestamp 2026-10-11T09:00:00Z, 11:00 in Berlin's summer time.
	h.must(t, gui, "ui.openView", map[string]any{"view": "events"}, nil)
	var rows engine.ViewRows
	h.must(t, agent, "view.rows", struct{}{}, &rows)
	lastSeen := "" // the column's title, which names it in the rows' table
	for _, c := range rows.ViewColumns {
		if c.ID == "lastSeen" {
			lastSeen = c.Title
		}
	}
	if lastSeen == "" || len(rows.Rows) != 1 || rows.Cell(0, lastSeen) != "11.10.2026 11:00:00" {
		t.Errorf("view.rows: columns %+v, table %+v", rows.ViewColumns, rows.RowTable)
	}
	var list commands.ListResult
	h.must(t, agent, "resources.list", map[string]any{"kind": "events"}, &list)
	if len(list.Rows) != 1 || list.Cell(0, "Last Seen") != "11.10.2026 11:00:00" {
		t.Errorf("resources.list: %+v", list.RowTable)
	}

	// testkit's pods were created 2026-10-01T12:00:00Z: 14:00 in Berlin.
	var obj struct {
		Sections []model.DetailSection `json:"sections"`
		YAML     string                `json:"yaml"`
	}
	h.must(t, agent, "object.get", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "api-0"}}, &obj)
	var created, state string
	for _, sec := range obj.Sections {
		for _, it := range sec.Items {
			switch {
			case it.Name == "Created":
				created = it.Value
			case strings.HasPrefix(it.Value, "Running"):
				state = it.Value
			}
		}
	}
	if !strings.HasSuffix(created, " ago (01.10.2026 14:00:00)") {
		t.Errorf("object.get's Created %q", created)
	}
	if state != "Running, ready, started 01.10.2026 14:00:05" {
		t.Errorf("object.get's container state %q", state)
	}
	if model.HasMarks(created) || model.HasMarks(state) {
		t.Errorf("a mark reached an agent: %q, %q", created, state)
	}
	if !strings.Contains(obj.YAML, "startedAt: \"2026-10-01T12:00:05Z\"") {
		t.Errorf("the pod's YAML lost its RFC 3339 time:\n%s", obj.YAML)
	}
}
