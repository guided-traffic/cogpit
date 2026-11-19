package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// proxyPassword is a proxy credential planted in the tests: only the settings file may hold it.
const proxyPassword = "pw-PROXY-secret-31"

// proxySession is settingsSession with an audit log.
func proxySession(t *testing.T) (*engine.Session, *syncBuffer, string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	kubeconfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kubeconfig, []byte(settingsKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	audit := &syncBuffer{}
	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}, IncludeDemo: true},
		Preferences:     engine.NewPreferencesStore(cfg),
		ClusterSettings: engine.NewClusterSettingsStore(cfg),
		Audit:           audit,
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, audit, cfg
}

// proxyConfirmer records what Cogpit asks and answers with *answer.
func proxyConfirmer(s *engine.Session, answer *bool) *[]engine.ConfirmRequest {
	var asked []engine.ConfirmRequest
	s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return *answer, nil
	}))
	return &asked
}

// The user sets a cluster's proxy in the window; an agent's proxy always waits for the user's
// acceptance, also where agent writes are allowed, and the question never shows the password
// (A20, owner); the CLI is refused before anything is kept. Agents and the CLI may clear it, and
// read it back with the password as •••. The audit log carries the attempts with the password
// redacted.
func TestClusterProxyCommands(t *testing.T) {
	s, audit, cfg := proxySession(t)
	answer := false
	asked := proxyConfirmer(s, &answer)
	s.SetAgentWrites("kind-dev", true)
	withPassword := "http://alice:" + proxyPassword + "@proxy.example.com:3128"
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		err := dispatch(t, s, origin, "cluster.settings.set", map[string]any{"context": "kind-dev", "proxy": withPassword}, nil)
		want := engine.ErrDeclined
		if origin == engine.OriginCLI {
			want = engine.ErrProxyGUIOnly
		}
		if !errors.Is(err, want) {
			t.Errorf("%s set a proxy: %v, want %v", origin, err, want)
		}
		if err != nil && strings.Contains(err.Error(), proxyPassword) {
			t.Errorf("the refusal quotes the password: %v", err)
		}
	}
	if len(*asked) != 2 || !(*asked)[0].AgentsAsk || (*asked)[0].Origin != engine.OriginExternalAgent ||
		!strings.Contains((*asked)[0].Detail, "http://alice:•••@proxy.example.com:3128") {
		t.Errorf("the agents' proxies: asked %+v", *asked)
	}
	if raw, _ := json.Marshal(*asked); strings.Contains(string(raw), proxyPassword) {
		t.Errorf("the question carries the password: %s", raw)
	}
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.Proxy != "" {
		t.Fatalf("a declined proxy was kept: %q", cs.Proxy)
	}
	answer = true
	if err := dispatch(t, s, engine.OriginBuiltInAgent, "cluster.settings.set", map[string]any{"context": "kind-dev", "proxy": withPassword}, nil); err != nil {
		t.Fatalf("an accepted agent's proxy: %v", err)
	}
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.Proxy != withPassword {
		t.Fatalf("the accepted proxy was not kept: %q", cs.Proxy)
	}
	answer = false

	var v engine.ClusterSettingsView
	if err := dispatch(t, s, engine.OriginGUI, "cluster.settings.set", map[string]any{"context": "kind-dev", "proxy": withPassword}, &v); err != nil {
		t.Fatal(err)
	}
	if v.Proxy != "http://alice:•••@proxy.example.com:3128" {
		t.Errorf("the user's set returns %q", v.Proxy)
	}
	var got engine.ClusterSettingsView
	if err := dispatch(t, s, engine.OriginExternalAgent, "cluster.settings.get", map[string]any{"context": "kind-dev"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Proxy != v.Proxy {
		t.Errorf("an agent reads %q", got.Proxy)
	}
	// The field's redacted form with another host keeps the password (the user edits the host).
	if err := dispatch(t, s, engine.OriginGUI, "cluster.settings.set", map[string]any{"context": "kind-dev", "proxy": "http://alice:•••@other:8080"}, nil); err != nil {
		t.Fatal(err)
	}
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.Proxy != "http://alice:"+proxyPassword+"@other:8080" {
		t.Errorf("after editing the host: %q", cs.Proxy)
	}
	if err := dispatch(t, s, engine.OriginGUI, "cluster.settings.set", map[string]any{"context": "kind-dev", "proxy": "ftp://alice:" + proxyPassword + "@x"}, nil); err == nil ||
		strings.Contains(err.Error(), proxyPassword) {
		t.Errorf("an ftp proxy: %v", err)
	}

	// An agent may clear it.
	if err := dispatch(t, s, engine.OriginExternalAgent, "cluster.settings.set", map[string]any{"context": "kind-dev", "proxy": ""}, nil); err != nil {
		t.Fatalf("an agent's clear: %v", err)
	}
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.Proxy != "" {
		t.Errorf("not cleared: %q", cs.Proxy)
	}

	for _, line := range audit.Lines() {
		raw, _ := json.Marshal(line)
		if strings.Contains(string(raw), proxyPassword) {
			t.Errorf("the audit log carries the password: %s", raw)
		}
	}
	if lines := audit.Lines(); len(lines) == 0 || !strings.Contains(string(lines[0].Params), `alice:•••@proxy.example.com`) {
		t.Errorf("the refused attempt's audit line: %+v", lines)
	}
	data, _ := os.ReadFile(filepath.Join(cfg, engine.ClusterSettingsFile))
	if strings.Contains(string(data), "•••") {
		t.Errorf("the file keeps the redaction mark: %s", data)
	}
}

// The Preferences' HTTP proxy follows the same rules: the GUI sets it, an agent's waits for the
// user (A20), the CLI is refused; agents clear it and read it as •••, the audit log redacts it.
func TestNonClusterProxyCommands(t *testing.T) {
	s, audit, _ := proxySession(t)
	answer := false
	asked := proxyConfirmer(s, &answer)
	withPassword := "https://bob:" + proxyPassword + "@proxy.example.com:8443"
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		want := engine.ErrDeclined
		if origin == engine.OriginCLI {
			want = engine.ErrProxyGUIOnly
		}
		if err := dispatch(t, s, origin, "preferences.set", map[string]any{"httpProxy": withPassword}, nil); !errors.Is(err, want) {
			t.Errorf("%s set the HTTP proxy: %v, want %v", origin, err, want)
		}
	}
	if len(*asked) != 2 || !(*asked)[1].AgentsAsk || !strings.Contains((*asked)[1].Detail, "https://bob:•••@proxy.example.com:8443") {
		t.Errorf("the agents' HTTP proxies: asked %+v", *asked)
	}
	var res commands.PreferencesResult
	if err := dispatch(t, s, engine.OriginGUI, "preferences.set", map[string]any{"httpProxy": withPassword}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Preferences.HTTPProxy != "https://bob:•••@proxy.example.com:8443" {
		t.Errorf("the user's set returns %q", res.Preferences.HTTPProxy)
	}
	if err := dispatch(t, s, engine.OriginExternalAgent, "preferences.get", struct{}{}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Preferences.HTTPProxy != "https://bob:•••@proxy.example.com:8443" {
		t.Errorf("an agent reads %q", res.Preferences.HTTPProxy)
	}
	if err := dispatch(t, s, engine.OriginExternalAgent, "preferences.set", map[string]any{"httpProxy": "", "theme": "light"}, &res); err != nil {
		t.Fatalf("an agent's clear: %v", err)
	}
	if p, _ := s.Preferences(); p.HTTPProxy != "" || p.Theme != "light" {
		t.Errorf("after the agent's clear: %+v", p)
	}
	for _, line := range audit.Lines() {
		if raw, _ := json.Marshal(line); strings.Contains(string(raw), proxyPassword) {
			t.Errorf("the audit log carries the password: %s", raw)
		}
	}
	if err := dispatch(t, s, engine.OriginExternalAgent, "preferences.open", map[string]any{"section": "proxy"}, nil); err != nil {
		t.Errorf("the Proxy section: %v", err)
	}
}
