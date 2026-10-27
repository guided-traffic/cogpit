package commands_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Create Secret (G9, D-19) against the fake writer: the GUI's Secret is checked, dry-run and created
// with its values base64-encoded in data; agents and the CLI are refused before anyone is asked;
// no value reaches the audit log, an error or a toast.

func init() {
	wantCommands[engine.SecretCreateCommand] = engine.DangerMutating
}

const (
	createdSecretValue = "pa55-w0rd-31c9"
	createdSecretKey   = "-----BEGIN PRIVATE KEY-----\nMIIEvQ-not-a-key-77ab\n-----END PRIVATE KEY-----\n"
)

// createLeak returns the first of the test's values (raw or base64) found in s.
func createLeak(s string) string {
	for _, v := range []string{createdSecretValue, createdSecretKey, "MIIEvQ-not-a-key-77ab"} {
		for _, form := range []string{v, base64.StdEncoding.EncodeToString([]byte(v))} {
			if strings.Contains(s, form) {
				return form
			}
		}
	}
	return ""
}

// tlsSecretParams are a TLS Secret's parameters, a label and an annotation with it.
func tlsSecretParams(ns string) map[string]any {
	p := map[string]any{"name": "web-tls", "type": engine.SecretTypeTLS,
		"data": []any{
			map[string]any{"key": "tls.crt", "value": createdSecretValue},
			map[string]any{"key": " tls.key ", "value": createdSecretKey},
		},
		"labels":      []any{map[string]any{"key": "app.kubernetes.io/name", "value": " web "}},
		"annotations": []any{map[string]any{"key": "example.com/owner", "value": "team web"}},
	}
	if ns != "" {
		p["namespace"] = ns
	}
	return p
}

// TestSecretCreateFromTheGUI: the user's Secret is dry-run, then created in the filter's namespace
// with its type, its values base64-encoded in data (multi-line values as typed), its labels and
// annotations; the toast names it and nothing else.
func TestSecretCreateFromTheGUI(t *testing.T) {
	h, srv := createHarness(t, engine.Options{})
	var toasts []string
	h.s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventToast {
			toasts = append(toasts, ev.Message)
		}
	})
	h.must(t, engine.OriginGUI, "ui.setNamespace", map[string]any{"namespaces": []string{"shop"}}, nil)
	cmd, _ := h.s.Registry().Get(engine.SecretCreateCommand)
	pre, err := cmd.Prefill(h.s, []byte(`{"name":""}`))
	if err != nil || !strings.Contains(string(pre), `"namespace":"shop"`) {
		t.Fatalf("prefill %s, %v", pre, err)
	}
	var res engine.CreateItem
	h.must(t, engine.OriginGUI, engine.SecretCreateCommand, tlsSecretParams(""), &res)
	if !res.Created || res.Ref.Kind != "secrets" || res.Ref.Namespace != "shop" || res.Ref.Name != "web-tls" {
		t.Fatalf("result %+v", res)
	}
	if n := len(srv.creates); n != 2 {
		t.Fatalf("%d creates, want the dry run and the create", n)
	}
	got := srv.created()["Secret shop/web-tls"]
	if got == nil {
		t.Fatalf("created %v", srv.created())
	}
	want := `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"web-tls","namespace":"shop",` +
		`"labels":{"app.kubernetes.io/name":"web"},"annotations":{"example.com/owner":"team web"}},` +
		`"type":"kubernetes.io/tls","data":{"tls.crt":"` + base64.StdEncoding.EncodeToString([]byte(createdSecretValue)) +
		`","tls.key":"` + base64.StdEncoding.EncodeToString([]byte(createdSecretKey)) + `"}}`
	if !equalJSON(t, got.Object, want) {
		t.Errorf("sent %v\nwant %s", got.Object, want)
	}
	if _, ok := got.Object["stringData"]; ok {
		t.Error("the values went out in stringData")
	}
	if len(toasts) != 1 || toasts[0] != "Created secrets/shop/web-tls" {
		t.Errorf("toasts %q", toasts)
	}
	if dock := h.s.UIState().Dock; len(dock) != 0 {
		t.Errorf("the dialog's create opened dock tabs %+v: a tab could keep the text", dock)
	}
}

// TestSecretCreateIsGUIOnly (D-19): an agent or the CLI is refused before the user is asked and
// before anything is sent, whatever it sends; the audit line names the Secret, its type and keys,
// never a value, not even one in a parameter the command does not have (the CLI is not audited:
// only agents are).
func TestSecretCreateIsGUIOnly(t *testing.T) {
	var audit bytes.Buffer
	asked := 0
	h, srv := createHarness(t, engine.Options{Audit: &audit, ClusterSettings: engine.NewClusterSettingsStore(""), Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
		asked++
		return true, nil
	})})
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil) // even allowed writes
	for _, origin := range []engine.Origin{engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		for _, params := range []map[string]any{
			tlsSecretParams("shop"),
			{"name": "x", "namespace": "shop", "stringData": map[string]any{"k": createdSecretValue}}, // not a parameter
		} {
			err := h.run(t, origin, engine.SecretCreateCommand, params, nil)
			var perr *engine.ParamsError
			if _, unknown := params["stringData"]; unknown {
				if !errors.As(err, &perr) {
					t.Errorf("%s with stringData: %v, want a parameters error", origin, err)
				}
			} else if !errors.Is(err, engine.ErrSecretGUIOnly) || !errors.Is(err, engine.ErrSecretCreateGUIOnly) {
				t.Errorf("%s: %v, want the GUI-only refusal", origin, err)
			}
			if err != nil {
				if l := createLeak(err.Error()); l != "" {
					t.Errorf("%s: the error carries %q", origin, l)
				}
			}
		}
	}
	if asked != 0 {
		t.Errorf("the user was asked %d times for a refused create", asked)
	}
	if n := len(srv.creates); n != 0 {
		t.Errorf("%d creates reached the server", n)
	}
	log := audit.String()
	if l := createLeak(log); l != "" {
		t.Errorf("%q in the audit log:\n%s", l, log)
	}
	for _, want := range []string{`"command":"secret.create"`, `"name":"web-tls"`, `"type":"kubernetes.io/tls"`, `"key":"tls.crt"`,
		`"key":" tls.key "`, `"otherParams":["stringData"]`, `"origin":"externalAgent"`, `"origin":"builtInAgent"`} {
		if !strings.Contains(log, want) {
			t.Errorf("the audit log lacks %s:\n%s", want, log)
		}
	}
}

// TestSecretCreateChecksBeforeSending: what the API server would refuse is refused before anything
// is sent, and no message quotes a value.
func TestSecretCreateChecksBeforeSending(t *testing.T) {
	h, srv := createHarness(t, engine.Options{})
	entry := func(k, v string) map[string]any { return map[string]any{"key": k, "value": v} }
	for _, tt := range []struct {
		what   string
		params map[string]any
		want   string
	}{
		{"no name", map[string]any{"name": ""}, "the secret needs a name"},
		{"bad name", map[string]any{"name": "Web_TLS"}, `secret name "Web_TLS" is not valid`},
		{"unknown type", map[string]any{"name": "a", "type": "example.com/mine"}, `secret type "example.com/mine": one of Opaque`},
		{"no key", map[string]any{"name": "a", "data": []any{entry("", createdSecretValue)}}, "data entry 1 has no key"},
		{"bad key", map[string]any{"name": "a", "data": []any{entry("a/b", createdSecretValue)}}, `data key "a/b" is not valid`},
		{"key twice", map[string]any{"name": "a", "data": []any{entry("k", createdSecretValue), entry(" k", "x")}}, `data key "k" is given twice`},
		{"too large", map[string]any{"name": "a", "data": []any{entry("k", strings.Repeat("x", 1<<20+1))}}, "a Secret holds at most 1.0 MiB"},
		{"tls without key", map[string]any{"name": "a", "type": engine.SecretTypeTLS, "data": []any{entry("tls.crt", createdSecretValue)}},
			"needs the keys tls.crt and tls.key"},
		{"docker config not JSON", map[string]any{"name": "a", "type": engine.SecretTypeDockerConfigJSON,
			"data": []any{entry(".dockerconfigjson", createdSecretValue)}}, "the value of .dockerconfigjson is not JSON"},
		{"docker config missing", map[string]any{"name": "a", "type": engine.SecretTypeDockercfg}, "needs the key .dockercfg"},
		{"basic auth without either", map[string]any{"name": "a", "type": engine.SecretTypeBasicAuth, "data": []any{entry("user", "x")}},
			"needs the key username or password"},
		{"ssh without key", map[string]any{"name": "a", "type": engine.SecretTypeSSHAuth, "data": []any{entry("ssh-privatekey", "")}},
			"needs the key ssh-privatekey"},
		{"token without account", map[string]any{"name": "a", "type": engine.SecretTypeServiceAccountToken,
			"annotations": []any{entry(engine.ServiceAccountNameAnnotation, " ")}}, "names its service account"},
		{"bad label", map[string]any{"name": "a", "labels": []any{entry("app", "a b")}}, "label app: the value is not valid"},
		{"label twice", map[string]any{"name": "a", "labels": []any{entry("app", "x"), entry("app", "y")}}, `label key "app" is given twice`},
		{"bad annotation", map[string]any{"name": "a", "annotations": []any{entry("a b", "x")}}, `annotation key "a b" is not valid`},
	} {
		tt.params["namespace"] = "shop"
		err := h.run(t, engine.OriginGUI, engine.SecretCreateCommand, tt.params, nil)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.what, err, tt.want)
			continue
		}
		if l := createLeak(err.Error()); l != "" {
			t.Errorf("%s: the error carries %q", tt.what, l)
		}
	}
	if n := len(srv.creates); n != 0 {
		t.Errorf("%d creates reached the server", n)
	}
	// What the API server accepts passes: basic auth with a password only, a docker config of JSON,
	// a token Secret naming its account, an empty value.
	for _, params := range []map[string]any{
		{"name": "b", "type": engine.SecretTypeBasicAuth, "data": []any{entry("password", createdSecretValue)}},
		{"name": "d", "type": engine.SecretTypeDockerConfigJSON, "data": []any{entry(".dockerconfigjson", `{"auths":{}}`)}},
		{"name": "t", "type": engine.SecretTypeServiceAccountToken, "annotations": []any{entry(engine.ServiceAccountNameAnnotation, "ci")}},
		{"name": "e", "data": []any{entry("empty", "")}},
	} {
		params["namespace"] = "shop"
		h.must(t, engine.OriginGUI, engine.SecretCreateCommand, params, nil)
	}
}

// TestSecretCreateErrorsNeverQuoteValues: an API server message that quotes a value, raw or
// base64, reaches the dialog without it.
func TestSecretCreateErrorsNeverQuoteValues(t *testing.T) {
	var audit bytes.Buffer
	h, srv := createHarness(t, engine.Options{Audit: &audit})
	for _, quoted := range []string{createdSecretValue, base64.StdEncoding.EncodeToString([]byte(createdSecretValue))} {
		srv.mu.Lock()
		srv.failNext = errors.New(`Secret "web-tls" is invalid: data[tls.crt]: Invalid value: "` + quoted + `"`)
		srv.mu.Unlock()
		err := h.run(t, engine.OriginGUI, engine.SecretCreateCommand, tlsSecretParams("shop"), nil)
		if err == nil || !strings.Contains(err.Error(), "is invalid") {
			t.Fatalf("the failure did not come back: %v", err)
		}
		if l := createLeak(err.Error()); l != "" {
			t.Errorf("the error carries %q: %v", l, err)
		}
	}
	if audit.Len() != 0 {
		t.Errorf("the user's own creates were audited:\n%s", audit.String())
	}
}

// TestSecretCreateFollowsAccessReviews: a denied create of secrets in the namespace greys the
// dialog's Create with the reason.
func TestSecretCreateFollowsAccessReviews(t *testing.T) {
	h, srv := createHarness(t, engine.Options{})
	h.access.set(denyAccess{"create secrets shop": true})
	err := h.run(t, engine.OriginGUI, engine.SecretCreateCommand, tlsSecretParams("shop"), nil)
	var dis *engine.DisabledError
	if !errors.As(err, &dis) || dis.Reason != "not permitted: you may not create secrets in shop" {
		t.Errorf("%v, want disabled: not permitted", err)
	}
	h.must(t, engine.OriginGUI, engine.SecretCreateCommand, tlsSecretParams("infra"), nil)
	if len(srv.creates) != 2 {
		t.Errorf("creates %d", len(srv.creates))
	}
}
