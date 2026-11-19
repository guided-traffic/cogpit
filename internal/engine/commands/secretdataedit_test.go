package commands_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A Secret's values edited in the drawer, the window's only (the owner's audit answer, D-59): the
// window reveals a value whole, reviews a masked diff and a dry run, and saves with the version the
// values are based on; agents and the CLI reach no path that writes a value; no value reaches an
// error, a toast, the audit log, the log or a result's diff.

// The values of the drawer's Secret and the values typed into it, in every form they could leak in.
const (
	sdPassword = "s3cret-pass-41b7"
	sdToken    = "tok-9f2c-not-real"
	sdTyped    = "typed-new-pass-6d1e"
	sdOther    = "other-client-val-3a8"
)

func sdLeak(s string) string {
	for _, v := range []string{sdPassword, sdToken, sdTyped, sdOther} {
		for _, form := range []string{v, base64.StdEncoding.EncodeToString([]byte(v))} {
			if strings.Contains(s, form) {
				return form
			}
		}
	}
	return ""
}

// sdBig is a value beyond a reveal's cut (4 KiB): the editor gets it whole.
var sdBig = strings.Repeat("0123456789abcdef\n", 300)

var sdRef = model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "shop", Name: "db-creds"}

// secretDataConnector connects "test" with the real Secrets profile and a writer that acts on the
// source; demo: no writer.
type secretDataConnector struct {
	src    *testkit.MemorySource
	writer *fakeWriter
}

func (c secretDataConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c secretDataConnector) Connect(context.Context, string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	p, _ := kinds.ByKey("secrets")
	return &engine.Backend{Context: "test", Namespace: "default", Cache: ch, Close: ch.Close,
		Profiles: append([]*model.KindProfile{p}, testProfiles()...), Conn: &cluster.Conn{}, Actions: c.writer}, nil
}

// secretDataHarness is a session with Secrets in the cache, an audit log, a log and every event
// recorded: db-creds (two text values, a binary one and one beyond a reveal's cut), an immutable
// one and a Helm release record.
type secretDataHarness struct {
	*harness
	srv    *objectServer
	audit  *syncBuffer
	logs   *bytes.Buffer
	mu     sync.Mutex
	events []engine.Event
}

func newSecretDataHarness(t *testing.T) *secretDataHarness {
	t.Helper()
	src := seed()
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "db-creds", map[string]any{
		"type": "Opaque",
		"data": map[string]any{"password": enc(sdPassword), "token": enc(sdToken), "big": enc(sdBig),
			"ca.der": base64.StdEncoding.EncodeToString([]byte{0x30, 0x82, 0x00, 0x01, 0xff})},
	}))
	src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "frozen", map[string]any{
		"type": "Opaque", "immutable": true, "data": map[string]any{"a": enc("1")},
	}))
	src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "sh.helm.release.v1.web.v1", map[string]any{
		"type": "helm.sh/release.v1", "data": map[string]any{"release": enc("H4sIAAAA-not-a-release")},
	}))
	h := &secretDataHarness{harness: &harness{src: src, writer: &fakeWriter{}}, audit: &syncBuffer{}, logs: &bytes.Buffer{}}
	var logMu sync.Mutex
	h.s = engine.New(engine.Options{
		Connector: secretDataConnector{src: src, writer: h.writer}, SnapshotInterval: 10 * time.Millisecond, Audit: h.audit,
		Logger:    slog.New(slog.NewTextHandler(lockedWriter{&logMu, h.logs}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil }),
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
	h.srv = serveObjects(h.harness)
	if _, err := h.s.Objects(context.Background(), "secrets", nil); err != nil {
		t.Fatal(err)
	}
	return h
}

// show opens the drawer on a Secret, as the user's click on its row does.
func (h *secretDataHarness) show(t *testing.T, ref model.ObjectRef) {
	t.Helper()
	h.must(t, engine.OriginGUI, "ui.navigate", map[string]any{"ref": ref}, nil)
}

// trail is everything the session wrote outside the window's own widgets: the audit log, the
// events (toasts included) and the log.
func (h *secretDataHarness) trail() string {
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

func (h *secretDataHarness) toasts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, ev := range h.events {
		if ev.Type == engine.EventToast {
			out = append(out, ev.Message)
		}
	}
	return out
}

// TestSecretDrawerDataSave: the window reveals a value whole, reviews a change of one key (a masked
// diff: the key and its sizes, the dry run) and saves it with the version it is based on; the
// other keys and the binary value are sent as they were; nothing of a value reaches the review,
// the result, a toast, the audit log or the log.
func TestSecretDrawerDataSave(t *testing.T) {
	h := newSecretDataHarness(t)
	gui := engine.OriginGUI
	h.show(t, sdRef)
	obj, err := h.s.Lookup(sdRef)
	if err != nil {
		t.Fatal(err)
	}
	base := obj.GetResourceVersion()

	var big engine.SecretValue
	h.must(t, gui, engine.RevealCommand, map[string]any{"ref": sdRef, "key": "big", "resourceVersion": base, "whole": true}, &big)
	if big.Value != sdBig || big.Cut {
		t.Errorf("the whole value: %d bytes, cut %v; want %d", len(big.Value), big.Cut, len(sdBig))
	}
	var cut engine.SecretValue
	h.must(t, gui, engine.RevealCommand, map[string]any{"ref": sdRef, "key": "big", "resourceVersion": base}, &cut)
	if !cut.Cut || len(cut.Value) >= len(sdBig) {
		t.Errorf("a reveal without whole is no longer cut: %d bytes", len(cut.Value))
	}

	params := map[string]any{"ref": sdRef, "resourceVersion": base, "data": map[string]any{"password": sdTyped, "token": sdToken},
		"from": map[string]any{"password": sdPassword, "token": sdToken}}
	var review engine.SecretDataReview
	h.must(t, gui, engine.SecretReviewDataCommand, params, &review)
	want := []engine.SecretKeyChange{{Key: "password", Change: engine.SecretKeyChanged, Bytes: len(sdPassword), NewBytes: len(sdTyped)}}
	if !slices.Equal(review.Changes, want) || !review.Accepted || review.Stale || review.RebasedFrom != "" || review.BaseResourceVersion != base {
		t.Fatalf("review %+v", review)
	}
	if got := h.writer.Calls(); !slices.Equal(got, []string{"update(dry) shop/db-creds"}) {
		t.Fatalf("writes of the review: %v", got)
	}
	var saved engine.SecretDataSaved
	h.must(t, gui, engine.SecretSaveDataCommand, map[string]any{"ref": sdRef, "resourceVersion": base, "data": params["data"]}, &saved)
	sent := h.srv.lastUpdate()
	data, _, _ := unstructured.NestedStringMap(sent.Object, "data")
	if sent.GetResourceVersion() != base || data["password"] != base64.StdEncoding.EncodeToString([]byte(sdTyped)) ||
		data["token"] != base64.StdEncoding.EncodeToString([]byte(sdToken)) || len(data) != 4 || data["ca.der"] == "" {
		t.Errorf("the update: version %s, data %v", sent.GetResourceVersion(), data)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(sent.Object, "stringData"); found {
		t.Error("the update carried stringData")
	}
	if !slices.Equal(saved.Keys, []string{"password"}) || saved.ResourceVersion == "" || saved.ResourceVersion == base {
		t.Errorf("saved %+v", saved)
	}
	if toasts := h.toasts(); !slices.Contains(toasts, "Secret shop/db-creds saved") {
		t.Errorf("toasts %q", toasts)
	}
	for _, v := range []any{review, saved} {
		b, _ := json.Marshal(v)
		if l := sdLeak(string(b)); l != "" {
			t.Errorf("%q in a result: %s", l, b)
		}
	}
	if l := sdLeak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
}

// TestSecretDrawerDataIsGUIOnly: agents and the CLI are refused the review and the save before
// anything is read, asked or sent, whatever the parameters; the audit line of a refused call
// names the Secret and the keys, never a value.
func TestSecretDrawerDataIsGUIOnly(t *testing.T) {
	h := newSecretDataHarness(t)
	h.show(t, sdRef)
	obj, _ := h.s.Lookup(sdRef)
	params := map[string]any{"ref": sdRef, "resourceVersion": obj.GetResourceVersion(), "data": map[string]any{"password": sdTyped},
		"from": map[string]any{"password": sdPassword}}
	// A parameter the commands do not have fails the schema, refused as well; its audit line keeps
	// its name only.
	extra := map[string]any{"stringData": map[string]any{"x": sdOther}}
	for k, v := range params {
		extra[k] = v
	}
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		for _, id := range []string{engine.SecretReviewDataCommand, engine.SecretSaveDataCommand} {
			err := h.run(t, origin, id, params, nil)
			var ge *engine.GUIOnlyError
			if !errors.As(err, &ge) || !errors.Is(err, engine.ErrSecretGUIOnly) || !errors.Is(err, engine.ErrSecretDataGUIOnly) {
				t.Errorf("%s from %s: %v, want the GUI-only refusal", id, origin, err)
			} else if l := sdLeak(err.Error()); l != "" {
				t.Errorf("%s from %s: the refusal carries %q", id, origin, l)
			}
			if err := h.run(t, origin, id, extra, nil); err == nil || sdLeak(err.Error()) != "" {
				t.Errorf("%s from %s with stringData: %v", id, origin, err)
			}
		}
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Errorf("a refused call reached the cluster: %v", calls)
	}
	var lines, others int
	for _, e := range h.audit.Lines() {
		if e.Command != engine.SecretSaveDataCommand && e.Command != engine.SecretReviewDataCommand {
			continue
		}
		lines++
		var p map[string]any
		if err := json.Unmarshal(e.Params, &p); err != nil {
			t.Fatalf("audit params %s: %v", e.Params, err)
		}
		data, _ := p["data"].(map[string]any)
		from, _ := p["from"].(map[string]any)
		ref, _ := p["ref"].(map[string]any)
		if e.OK || data["password"] != model.Redacted || from["password"] != model.Redacted || ref["name"] != "db-creds" {
			t.Errorf("audit line %+v: %s", e, e.Params)
		}
		if p["otherParams"] != nil {
			others++
		}
	}
	if lines != 8 || others != 4 { // the agents' calls: the CLI's are not audited
		t.Errorf("%d audit lines of refused calls (%d with other parameters), want 8 (4)", lines, others)
	}
	if l := sdLeak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
}

// TestSecretDrawerDataConflictAndRebase: another client changes a key the user did not edit; the
// review rebases onto the newer version, a masked diff and a dry run against it. A change of the
// edited key is the conflict: the review stale, the save refused with the masked changes against
// the newer version, written over only by a save that names it. No conflict carries a value.
func TestSecretDrawerDataConflictAndRebase(t *testing.T) {
	h := newSecretDataHarness(t)
	gui := engine.OriginGUI
	h.show(t, sdRef)
	obj, _ := h.s.Lookup(sdRef)
	base := obj.GetResourceVersion()
	change := func(key, value string) string {
		t.Helper()
		cur, err := h.srv.current(secretsGVR, "shop", "db-creds")
		if err != nil {
			t.Fatal(err)
		}
		if err := unstructured.SetNestedField(cur.Object, base64.StdEncoding.EncodeToString([]byte(value)), "data", key); err != nil {
			t.Fatal(err)
		}
		before, _ := h.s.Lookup(sdRef)
		h.src.Set(secretsGVR, cur)
		return cachedRV(t, h.harness, sdRef, before.GetResourceVersion())
	}
	newer := change("token", sdOther)
	h.show(t, sdRef)
	params := map[string]any{"ref": sdRef, "resourceVersion": base, "data": map[string]any{"password": sdTyped},
		"from": map[string]any{"password": sdPassword}}
	var review engine.SecretDataReview
	h.must(t, gui, engine.SecretReviewDataCommand, params, &review)
	if review.RebasedFrom != base || review.BaseResourceVersion != newer || review.Stale || !review.Accepted || len(review.Changes) != 1 {
		t.Fatalf("a review over another key's change: %+v", review)
	}

	// The edited key changed: stale, and the save refused.
	newest := change("password", sdOther)
	h.show(t, sdRef)
	review = engine.SecretDataReview{}
	h.must(t, gui, engine.SecretReviewDataCommand, params, &review)
	if !review.Stale || review.RebasedFrom != "" || review.CachedResourceVersion != newest || review.Accepted {
		t.Errorf("a review over the edited key's change: %+v", review)
	}
	calls := len(h.writer.Calls())
	err := h.run(t, gui, engine.SecretSaveDataCommand, map[string]any{"ref": sdRef, "resourceVersion": newer, "data": params["data"]}, nil)
	var ce *engine.DataConflictError
	if !errors.As(err, &ce) || ce.CurrentResourceVersion != newest || len(ce.Changes) != 1 || ce.Changes[0].Key != "password" || len(ce.Diff) != 0 {
		t.Fatalf("a save over a newer version: %v (%+v)", err, ce)
	}
	if l := sdLeak(err.Error()); l != "" || !strings.Contains(err.Error(), "Save over version "+newest) {
		t.Errorf("the conflict says %q", err.Error())
	}
	if b, _ := json.Marshal(ce); sdLeak(string(b)) != "" {
		t.Errorf("the conflict carries a value: %s", b)
	}
	if len(h.writer.Calls()) != calls {
		t.Error("a stale save wrote")
	}
	h.must(t, gui, engine.SecretSaveDataCommand, map[string]any{"ref": sdRef, "resourceVersion": newest, "data": params["data"]}, nil)
	data, _, _ := unstructured.NestedStringMap(h.srv.lastUpdate().Object, "data")
	if data["password"] != base64.StdEncoding.EncodeToString([]byte(sdTyped)) || data["token"] != base64.StdEncoding.EncodeToString([]byte(sdOther)) {
		t.Errorf("the save over the newer version: %v", data)
	}
}

// TestSecretDrawerDataRefusals: values only, of text keys the data holds, of the Secret the drawer
// shows; no immutable Secret, no Helm release record; a server's message that quotes a value
// reaches the window without it.
func TestSecretDrawerDataRefusals(t *testing.T) {
	h := newSecretDataHarness(t)
	gui := engine.OriginGUI
	obj, _ := h.s.Lookup(sdRef)
	rv := obj.GetResourceVersion()
	run := func(ref model.ObjectRef, data map[string]any) error {
		return h.run(t, gui, engine.SecretSaveDataCommand, map[string]any{"ref": ref, "resourceVersion": rv, "data": data}, nil)
	}
	if err := run(sdRef, map[string]any{"password": sdTyped}); err == nil || !strings.Contains(err.Error(), "from the drawer that shows it") {
		t.Errorf("a save while the drawer shows another object: %v", err)
	}
	h.show(t, sdRef)
	frozen := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "shop", Name: "frozen"}
	helm := model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "shop", Name: "sh.helm.release.v1.web.v1"}
	for _, c := range []struct {
		name string
		ref  model.ObjectRef
		data map[string]any
		want string
	}{
		{"a new key", sdRef, map[string]any{"new-key": sdTyped}, "has no key"},
		{"a binary value", sdRef, map[string]any{"ca.der": sdTyped}, "binary data"},
		{"no values", sdRef, map[string]any{}, "give data"},
	} {
		if err := run(c.ref, c.data); err == nil || !strings.Contains(err.Error(), c.want) || sdLeak(err.Error()) != "" {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	for _, c := range []struct {
		ref  model.ObjectRef
		want string
	}{{frozen, "immutable"}, {helm, "Helm release record"}} {
		h.show(t, c.ref)
		var dis *engine.DisabledError
		if err := run(c.ref, map[string]any{"a": "2"}); !errors.As(err, &dis) || !strings.Contains(dis.Reason, c.want) {
			t.Errorf("%s: %v, want disabled: %q", c.ref.Name, err, c.want)
		}
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Fatalf("a refused save wrote: %v", calls)
	}

	h.show(t, sdRef)
	quote := func() error {
		return fmt.Errorf("Secret %q is invalid: data[password]: Invalid value: %q (was %q)", "db-creds", sdTyped, sdPassword)
	}
	h.srv.mu.Lock()
	h.srv.failNext = quote()
	h.srv.mu.Unlock()
	var review engine.SecretDataReview
	h.must(t, gui, engine.SecretReviewDataCommand, map[string]any{"ref": sdRef, "resourceVersion": rv, "data": map[string]any{"password": sdTyped}}, &review)
	if review.Accepted || !strings.Contains(review.Refused, "is invalid") || sdLeak(review.Refused) != "" {
		t.Errorf("a refused dry run: %+v", review)
	}
	h.srv.mu.Lock()
	h.srv.failNext = quote()
	h.srv.mu.Unlock()
	if err := run(sdRef, map[string]any{"password": sdTyped}); err == nil || sdLeak(err.Error()) != "" || !strings.Contains(err.Error(), "is invalid") {
		t.Errorf("a refused save: %v", err)
	}
	if l := sdLeak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
}

// TestSecretDrawerDataDisabledOnTheDemo: the demo cluster cannot be changed; the drawer's save is
// disabled with the reason the window shows.
func TestSecretDrawerDataDisabledOnTheDemo(t *testing.T) {
	h := newSecretDataHarness(t)
	h.s.Close()
	src := testkit.NewMemorySource()
	src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "app", map[string]any{"data": map[string]any{"a": "MQ=="}}))
	s := engine.New(engine.Options{Connector: demoSecrets{src: src}, SnapshotInterval: 10 * time.Millisecond})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	cmd, _ := s.Registry().Get(engine.SecretSaveDataCommand)
	raw, _ := json.Marshal(map[string]any{"ref": model.ObjectRef{Kind: "secrets", Namespace: "shop", Name: "app"}, "resourceVersion": "1", "data": map[string]any{"a": "2"}})
	if err := cmd.Enabled(s, raw); err == nil || !strings.Contains(err.Error(), "demo") {
		t.Errorf("the save on the demo cluster: %v", err)
	}
}

// demoSecrets connects "test" as the demo cluster: no writer.
type demoSecrets struct{ src *testkit.MemorySource }

func (c demoSecrets) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test", Current: true}}, nil
}

func (c demoSecrets) Connect(context.Context, string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	p, _ := kinds.ByKey("secrets")
	return &engine.Backend{Context: "test", Namespace: "default", Cache: ch, Close: ch.Close, Demo: true,
		Profiles: append([]*model.KindProfile{p}, testProfiles()...)}, nil
}
