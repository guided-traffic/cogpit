package commands_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// secretsConnector connects "test" with the real Secrets profile, which watches Secrets in full.
type secretsConnector struct {
	src *testkit.MemorySource
}

func (c *secretsConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c *secretsConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	p, _ := kinds.ByKey("secrets")
	return &engine.Backend{
		Context: name, Namespace: "default", Cache: ch, Close: ch.Close,
		Profiles: append([]*model.KindProfile{p}, testProfiles()...),
		Conn:     &cluster.Conn{}, Actions: &fakeWriter{},
	}, nil
}

// secretsHarness is a session on the Secrets connector with an audit log and a log, recording
// every event.
type secretsHarness struct {
	s      *engine.Session
	src    *testkit.MemorySource
	audit  *syncBuffer
	logs   *bytes.Buffer
	mu     sync.Mutex
	events []engine.Event
}

// The values of the harness's Secret, in every form they could leak in.
// "db:5432", not the port alone: four digits turn up in a timestamp's nanoseconds in the audit log
// (CI run 38972598188).
var secretLeaks = []string{"hunter2", base64.StdEncoding.EncodeToString([]byte("hunter2")), "db:5432", "kubectl-applied-s3cr3t"}

func newSecretsHarness(t *testing.T) *secretsHarness {
	t.Helper()
	src := testkit.NewMemorySource()
	db := testkit.Secret("apps", "db", map[string]string{"app": "db"},
		map[string]string{"password": "hunter2", "dsn": "postgres://app:hunter2@db:5432/app"})
	db.SetAnnotations(map[string]string{cache.LastAppliedAnnotation: `{"stringData":{"token":"kubectl-applied-s3cr3t"}}`, "team": "db"})
	src.Set(testkit.SecretsGVR, db)
	h := &secretsHarness{src: src, audit: &syncBuffer{}, logs: &bytes.Buffer{}}
	var logMu sync.Mutex
	h.s = engine.New(engine.Options{
		Connector: &secretsConnector{src: src}, SnapshotInterval: 10 * time.Millisecond,
		Audit:  h.audit,
		Logger: slog.New(slog.NewTextHandler(lockedWriter{&logMu, h.logs}, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	h.s.Subscribe(func(ev engine.Event) {
		h.mu.Lock()
		h.events = append(h.events, ev)
		h.mu.Unlock()
	})
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	return h
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (h *secretsHarness) dispatch(origin engine.Origin, id string, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return h.s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: origin})
}

// leak returns the first value of the harness's Secret that v carries, or "".
func leak(v any) string {
	b, _ := json.Marshal(v)
	if s, ok := v.(string); ok {
		b = []byte(s)
	}
	for _, l := range secretLeaks {
		if bytes.Contains(b, []byte(l)) {
			return l
		}
	}
	return ""
}

// trail is everything a session wrote outside the GUI's own widgets: the audit log, the events
// and the log.
func (h *secretsHarness) trail() string {
	var out bytes.Buffer
	for _, e := range h.audit.Lines() {
		b, _ := json.Marshal(e)
		out.Write(b)
	}
	h.mu.Lock()
	for _, ev := range h.events {
		b, _ := json.Marshal(ev)
		out.Write(b)
	}
	h.mu.Unlock()
	out.WriteString(h.logs.String())
	return out.String()
}

// The drawer of a Secret lists its keys masked, and secret.reveal hands one value to the GUI and
// to nobody else: agents and the CLI are refused; no value reaches object_get, the audit log, a
// toast or the log; a reveal makes no request.
func TestRevealIsGUIOnly(t *testing.T) {
	h := newSecretsHarness(t)
	ref := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "apps", Name: "db"}
	reveal := engine.RevealParams{Ref: ref, Key: "password"}

	// Before the drawer opened, nothing is shown to reveal.
	if _, err := h.dispatch(engine.OriginGUI, "secret.reveal", reveal); err == nil || !strings.Contains(err.Error(), "open in the drawer") {
		t.Errorf("reveal without the drawer: %v", err)
	}

	// An agent opens the drawer (ui.navigate) and reads it: keys, masked.
	if _, err := h.dispatch(engine.OriginExternalAgent, "ui.navigate", map[string]any{"ref": ref}); err != nil {
		t.Fatal(err)
	}
	var m engine.DetailModel
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		res, err := h.dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": ref})
		if err != nil {
			return false
		}
		b, _ := json.Marshal(res)
		m = engine.DetailModel{}
		_ = json.Unmarshal(b, &m)
		return strings.Contains(string(b), engine.MaskedValue)
	}) {
		t.Fatalf("object.get never showed the masked data: %+v", m)
	}
	if l := leak(m); l != "" {
		t.Fatalf("object.get for an agent carries %q", l)
	}
	lists, watches := h.src.Lists(), h.src.Watches()

	asBase64 := reveal
	asBase64.Base64 = true // the drawer's b64 button's read (VW7): GUI-only as Show
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		for _, p := range []engine.RevealParams{reveal, asBase64} {
			res, err := h.dispatch(origin, "secret.reveal", p)
			if err == nil || res != nil || !strings.Contains(err.Error(), "never receive them") {
				t.Errorf("%s: reveal (base64 %v) gave %v, %v", origin, p.Base64, res, err)
			}
		}
	}
	res, err := h.dispatch(engine.OriginGUI, "secret.reveal", reveal)
	if err != nil {
		t.Fatal(err)
	}
	if v := res.(engine.SecretValue); v.Value != "hunter2" || v.Key != "password" || v.Size != 7 {
		t.Errorf("GUI reveal %+v", v)
	}
	res, err = h.dispatch(engine.OriginGUI, "secret.reveal", asBase64)
	if v, _ := res.(engine.SecretValue); err != nil || v.Value != base64.StdEncoding.EncodeToString([]byte("hunter2")) || !v.Base64 {
		t.Errorf("GUI reveal as base64 %+v, %v", v, err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "secret.reveal", engine.RevealParams{Ref: ref, Key: "nope"}); err == nil {
		t.Error("a key the Secret does not have was revealed")
	}
	if h.src.Lists() != lists || h.src.Watches() != watches {
		t.Error("a reveal read the cluster")
	}

	if l := leak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
	refused := 0
	for _, e := range h.audit.Lines() {
		if e.Command == "secret.reveal" && !e.OK {
			refused++
		}
	}
	if refused != 4 {
		t.Errorf("%d refused agent reveals audited, want 4 (two agents, with and without base64; the CLI is not audited)", refused)
	}

	// Closing the drawer: the GUI cannot reveal either.
	if _, err := h.dispatch(engine.OriginGUI, "ui.closeDrawer", struct{}{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.dispatch(engine.OriginGUI, "secret.reveal", reveal); err == nil {
		t.Error("revealed after the drawer closed")
	}
}

// Every command an agent reads with gets Secrets redacted: the names of the keys, never a value,
// and never the last-applied annotation that repeats them: kinds_list, resources_list,
// resources_find (by kind and across kinds), object_get, ui_state, ui_navigate, table_exportCsv,
// workloads_overview; nor do the audit log, the events or the log carry one.
func TestAgentsReadSecretsRedacted(t *testing.T) {
	h := newSecretsHarness(t)
	ref := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "apps", Name: "db"}
	agent := engine.OriginExternalAgent
	calls := []struct {
		id     string
		params any
		want   string // a text the result must carry, so that the call is known to have read the Secret
	}{
		{"kinds.list", struct{}{}, `"kind":"secrets"`},
		{"resources.list", map[string]any{"kind": "secrets"}, "dsn, password"},
		{"resources.list", map[string]any{"kind": "secrets", "filter": "hunter"}, `"total":0`}, // the search sees no value
		{"resources.find", map[string]any{"kind": "secrets", "field": "name", "value": "db"}, `"name":"db"`},
		{"resources.find", map[string]any{"kind": "secrets", "field": "label", "value": "app=db"}, `"name":"db"`},
		{"resources.find", map[string]any{"field": "name", "value": "db"}, `"count"`},
		{"object.get", map[string]any{"ref": ref}, engine.MaskedValue},
		{"object.get", map[string]any{"ref": ref, "related": true}, `"relatedTotal"`},
		{"ui.openView", map[string]any{"view": "secrets"}, `"view":"secrets"`},
		{"ui.navigate", map[string]any{"ref": ref}, `"found":true`},
		{"ui.state", struct{}{}, `"view":"secrets"`},
		{"table.exportCsv", struct{}{}, "dsn, password"},
		{"view.rows", struct{}{}, "dsn, password"}, // the rows the user sees: key names, never a value
		{"workloads.overview", struct{}{}, ""},
	}
	for _, c := range calls {
		var (
			res any
			err error
		)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		testkit.WaitFor(ctx, func() bool { // the view's first snapshot comes asynchronously
			res, err = h.dispatch(agent, c.id, c.params)
			b, _ := json.Marshal(res)
			return err == nil && strings.Contains(string(b), c.want)
		})
		cancel()
		if err != nil && c.id != "workloads.overview" {
			t.Errorf("%s: %v", c.id, err)
			continue
		}
		b, _ := json.Marshal(res)
		if !strings.Contains(string(b), c.want) {
			t.Errorf("%s %v: the result never carried %q: %s", c.id, c.params, c.want, b)
		}
		if l := leak(res); l != "" {
			t.Errorf("%s %v: carries %q: %s", c.id, c.params, l, b)
		}
		if strings.Contains(string(b), cache.LastAppliedAnnotation) {
			t.Errorf("%s: carries the last-applied annotation", c.id)
		}
	}
	if l := leak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
}

// Opening a Secret's Edit tab is the user's alone (D-19): agents and the CLI are refused before
// anything is read, and no value reaches the audit log, the events or the log. The GUI's own edit,
// apply and create are in TestSecretEditAndCreateAreGUIOnly.
func TestSecretEditIsRefusedToAgentsAndTheCLI(t *testing.T) {
	h := newSecretsHarness(t)
	ref := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "apps", Name: "db"}
	if _, err := h.s.Objects(context.Background(), "secrets", nil); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []engine.Origin{engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		_, err := h.dispatch(origin, "object.edit", map[string]any{"ref": ref})
		if !errors.Is(err, engine.ErrSecretGUIOnly) {
			t.Errorf("object.edit on a Secret from %s: %v", origin, err)
		} else if l := leak(err.Error()); l != "" {
			t.Errorf("the refusal carries %q", l)
		}
	}
	if l := leak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
}

// An agent's apply of a Secret tab the user opened, and an agent's create of a Secret, are refused
// before the user is asked: a confirmation for something the command then refuses would ask the
// user for nothing (D-19).
func TestSecretWritesFromAgentsRefusedBeforeAsking(t *testing.T) {
	h := newSecretsHarness(t)
	ref := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "apps", Name: "db"}
	if _, err := h.s.Objects(context.Background(), "secrets", nil); err != nil {
		t.Fatal(err)
	}
	asked := 0
	h.s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
		asked++
		return true, nil
	}))
	res, err := h.dispatch(engine.OriginGUI, "object.edit", map[string]any{"ref": ref})
	if err != nil {
		t.Fatal(err)
	}
	tab := res.(commands.EditResult)
	agent := engine.OriginExternalAgent
	if _, err := h.dispatch(agent, "object.apply", map[string]any{"id": tab.Tab.ID, "textRevision": tab.Revision}); !errors.Is(err, engine.ErrSecretGUIOnly) {
		t.Errorf("an agent's apply of a Secret tab: %v", err)
	}
	secret := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: agent-made\n  namespace: apps\nstringData:\n  password: kubectl-applied-s3cr3t\n"
	if _, err := h.dispatch(agent, "object.create", map[string]any{"yaml": secret}); !errors.Is(err, engine.ErrSecretGUIOnly) {
		t.Errorf("an agent's create of a Secret: %v", err)
	}
	if asked != 0 {
		t.Errorf("the user was asked %d times for writes that are refused anyway", asked)
	}
	if l := leak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
}
