package commands_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A data key removed through the Edit tab (the owner, 2026-10-16 evening; D-55): object.apply is
// destructive for every origin when the text drops a key the cached Config Map or Secret holds.

// appConfig is the Config Map the tests edit; dbSecret (secretedit_test.go) the Secret.
var appConfig = model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "app"}

// The Secret's values, which no confirmation may carry: base64 and decoded.
const (
	dbPassword    = "aHVudGVyMi1lZGl0LWtleQ==" // hunter2-edit-key
	dbPasswordRaw = "hunter2-edit-key"
	dbUser        = "YXBwLXVzZXI=" // app-user
	dbUserRaw     = "app-user"
)

// dataKeysHarness serves a Config Map with two data keys and a binary key, and a Secret with two
// keys, under a confirmer that records every request and declines it.
func dataKeysHarness(t *testing.T) (*harness, *objectServer, *confirmations) {
	t.Helper()
	conf := &confirmations{answer: false}
	h, srv := editHarness(t, engine.Options{Confirmer: conf})
	h.src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "app", map[string]any{
		"data":       map[string]any{"mode": "alpha-mode", "level": "bravo-level"},
		"binaryData": map[string]any{"blob": "AAEC"},
	}))
	h.src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "db", map[string]any{
		"data": map[string]any{"password": dbPassword, "user": dbUser},
	}))
	return h, srv, conf
}

// openEdit opens an object's Edit tab in the window.
func openEdit(t *testing.T, h *harness, ref model.ObjectRef) commands.EditResult {
	t.Helper()
	var opened commands.EditResult
	h.must(t, engine.OriginGUI, "object.edit", map[string]any{"ref": ref}, &opened)
	return opened
}

// edited is an Edit tab's text with change applied to the object it parses into.
func edited(t *testing.T, text string, change func(obj map[string]any)) string {
	t.Helper()
	obj, err := engine.ParseManifest(text)
	if err != nil {
		t.Fatal(err)
	}
	change(obj.Object)
	out, err := yaml.Marshal(obj.Object)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// dataField is a map field of an object, for changing it in place.
func dataField(obj map[string]any, name string) map[string]any {
	m, _ := obj[name].(map[string]any)
	return m
}

// updates counts the updates (dry runs included) the fake server received.
func updates(srv *objectServer) int {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return len(srv.updates)
}

// TestApplyRemovingADataKeyIsDestructive: in the window, an apply that drops a key of a Config
// Map's data or binaryData asks as destructive, naming the object, its namespace, the context and
// the removed keys; declined, nothing is sent. A changed value that keeps every key, an added key,
// and a key moved between data and binaryData do not escalate, and the window's apply writes
// without asking.
func TestApplyRemovingADataKeyIsDestructive(t *testing.T) {
	h, srv, conf := dataKeysHarness(t)
	gui := engine.OriginGUI
	opened := openEdit(t, h, appConfig)
	id := opened.Tab.ID

	dropLevel := edited(t, opened.Text, func(o map[string]any) { delete(dataField(o, "data"), "level") })
	dropBlob := edited(t, opened.Text, func(o map[string]any) { delete(o, "binaryData") })
	dropData := edited(t, opened.Text, func(o map[string]any) { delete(o, "data") })
	for name, tc := range map[string]struct {
		text string
		want []string
	}{
		"a data key":      {dropLevel, []string{"level"}},
		"the binary key":  {dropBlob, []string{"blob"}},
		"the whole data":  {dropData, []string{"level", "mode"}},
		"a value changed": {edited(t, opened.Text, func(o map[string]any) { dataField(o, "data")["mode"] = "charlie-mode" }), nil},
		"a key added":     {edited(t, opened.Text, func(o map[string]any) { dataField(o, "data")["extra"] = "delta" }), nil},
		"moved to binary": {edited(t, opened.Text, func(o map[string]any) {
			delete(dataField(o, "data"), "mode")
			dataField(o, "binaryData")["mode"] = "YWxwaGEtbW9kZQ=="
		}), nil},
		"the text unchanged": {opened.Text, nil},
	} {
		if got := h.s.EditRemovesDataKeys(id, tc.text); !slices.Equal(got, tc.want) {
			t.Errorf("%s: removed %q, want %q", name, got, tc.want)
		}
	}

	if err := h.run(t, gui, "object.apply", map[string]any{"id": id, "yaml": dropLevel}, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("the window's apply without a key: %v, want the user's refusal of a confirmation", err)
	}
	req := conf.last(t)
	if req.Command != "object.apply" || req.Danger != engine.DangerDestructive || req.Origin != gui || req.Context != "test" ||
		!slices.Equal(req.Namespaces, []string{"shop"}) || len(req.Objects) != 1 || !req.Objects[0].Same(appConfig) ||
		req.Detail != "Removes: level." || !slices.Equal(req.Effects, []engine.Effect{{Name: "Removes", Value: "level", Tone: engine.EffectLost}}) {
		t.Fatalf("the confirmation %+v", req)
	}
	if n := updates(srv); n != 0 {
		t.Fatalf("a declined apply sent %d updates", n)
	}

	// A changed value keeps every key: the window's apply writes at once.
	asked := len(conf.all())
	h.must(t, gui, "object.apply", map[string]any{"id": id, "yaml": edited(t, opened.Text, func(o map[string]any) {
		dataField(o, "data")["mode"] = "charlie-mode"
		dataField(o, "data")["extra"] = "delta"
	})}, nil)
	if len(conf.all()) != asked {
		t.Fatalf("a changed value and an added key asked: %+v", conf.last(t))
	}
	if n := updates(srv); n != 2 {
		t.Errorf("the apply sent %d updates, want the dry run and the update", n)
	}

	// The version the edit is now based on counts beside the cache, which may still hold the one
	// before the apply: dropping the key just added asks.
	var st engine.EditorState
	h.must(t, gui, "editor.get", map[string]any{"id": id}, &st)
	dropExtra := edited(t, st.Text, func(o map[string]any) { delete(dataField(o, "data"), "extra") })
	if got := h.s.EditRemovesDataKeys(id, dropExtra); !slices.Equal(got, []string{"extra"}) {
		t.Errorf("dropping the key just added: removed %q, want extra", got)
	}
}

// TestAgentApplyRemovingADataKeyAlwaysAsks: an agent (as the MCP server dispatches it, and the
// built-in agent) that drops a Config Map key through object.apply always asks, also where the user
// allowed agent writes for the context; with the allowance, a changed value is written without
// asking.
func TestAgentApplyRemovingADataKeyAlwaysAsks(t *testing.T) {
	h, srv, conf := dataKeysHarness(t)
	opened := openEdit(t, h, appConfig)
	id := opened.Tab.ID
	drop := edited(t, opened.Text, func(o map[string]any) { delete(dataField(o, "data"), "mode") })
	h.s.SetAgentWrites("test", true)

	for _, o := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		asked := len(conf.all())
		if err := h.run(t, o, "object.apply", map[string]any{"id": id, "yaml": drop}, nil); !errors.Is(err, engine.ErrDeclined) {
			t.Fatalf("%s's apply without a key: %v, want the user's refusal", o, err)
		}
		if len(conf.all()) != asked+1 {
			t.Fatalf("%s's apply without a key did not ask", o)
		}
		if req := conf.last(t); req.Danger != engine.DangerDestructive || req.Origin != o || req.Detail != "Removes: mode." ||
			req.Context != "test" || !slices.Equal(req.Namespaces, []string{"shop"}) {
			t.Fatalf("%s's confirmation %+v", o, req)
		}
	}
	if n := updates(srv); n != 0 {
		t.Fatalf("declined applies sent %d updates", n)
	}

	asked := len(conf.all())
	h.must(t, engine.OriginExternalAgent, "object.apply", map[string]any{"id": id, "yaml": edited(t, opened.Text, func(o map[string]any) {
		dataField(o, "data")["level"] = "echo-level"
	})}, nil)
	if len(conf.all()) != asked {
		t.Fatalf("an allowed agent's changed value asked: %+v", conf.last(t))
	}

	// Writes not allowed: a changed value asks as a mutating command, without a key named.
	h.s.SetAgentWrites("test", false)
	opened = openEdit(t, h, appConfig)
	_ = h.run(t, engine.OriginExternalAgent, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": edited(t, opened.Text, func(o map[string]any) {
		dataField(o, "data")["level"] = "foxtrot-level"
	})}, nil)
	if req := conf.last(t); len(conf.all()) != asked+1 || req.Danger != engine.DangerMutating || req.Detail != "" {
		t.Fatalf("a not allowed agent's changed value: %d asked, %+v", len(conf.all())-asked, req)
	}
}

// TestApplyRemovingASecretKeyIsDestructive: in the window, an apply that drops a Secret's key asks
// as destructive and names the key, never a value (neither base64 nor decoded, anywhere in the
// request); a key moved from data to stringData stays. An agent's apply of the Secret's tab is
// refused before anything is asked (D-19).
func TestApplyRemovingASecretKeyIsDestructive(t *testing.T) {
	h, srv, conf := dataKeysHarness(t)
	opened := openEdit(t, h, dbSecret)
	id := opened.Tab.ID
	drop := edited(t, opened.Text, func(o map[string]any) { delete(dataField(o, "data"), "user") })
	moved := edited(t, opened.Text, func(o map[string]any) {
		delete(dataField(o, "data"), "user")
		o["stringData"] = map[string]any{"user": dbUserRaw}
	})
	if got := h.s.EditRemovesDataKeys(id, drop); !slices.Equal(got, []string{"user"}) {
		t.Errorf("removed %q, want user", got)
	}
	if got := h.s.EditRemovesDataKeys(id, moved); got != nil {
		t.Errorf("a key moved to stringData counts as removed: %q", got)
	}

	if err := h.run(t, engine.OriginGUI, "object.apply", map[string]any{"id": id, "yaml": drop}, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("the window's apply without a Secret key: %v, want the user's refusal", err)
	}
	req := conf.last(t)
	if req.Danger != engine.DangerDestructive || req.Detail != "Removes: user." || len(req.Objects) != 1 || !req.Objects[0].Same(dbSecret) {
		t.Fatalf("the confirmation %+v", req)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{dbPassword, dbPasswordRaw, dbUser, dbUserRaw} {
		if strings.Contains(string(raw), v) {
			t.Errorf("the confirmation carries a Secret value: %s", raw)
		}
	}

	asked := len(conf.all())
	if err := h.run(t, engine.OriginExternalAgent, "object.apply", map[string]any{"id": id, "yaml": drop}, nil); err == nil || errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an agent's apply of a Secret: %v, want a refusal before asking", err)
	}
	if len(conf.all()) != asked {
		t.Errorf("an agent's apply of a Secret asked: %+v", conf.last(t))
	}
	if n := updates(srv); n != 0 {
		t.Errorf("%d updates sent", n)
	}
}
