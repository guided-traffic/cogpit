package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/terminal"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// sourceToken is the fake API server's token in these tests: no result, error, audit line or log
// may contain it.
const sourceToken = "tok-SOURCES-41a7-must-not-leak"

// sourcesSession is an engine with kubeconfig sources in a temporary configuration directory and
// no other source (tests never read the user's ~/.kube or $KUBECONFIG; with sources nothing is merged, D-28).
func sourcesSession(t *testing.T) (*engine.Session, *engine.KubeconfigSources, *syncBuffer) {
	t.Helper()
	k := engine.NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	audit := &syncBuffer{}
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{Sources: k},
		Audit:     audit,
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, k, audit
}

// sourcesDispatch runs a command from origin and decodes its result into out (if not nil).
func sourcesDispatch(t *testing.T, s *engine.Session, origin engine.Origin, id string, params any, out any) error {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: origin})
	if err != nil {
		return err
	}
	if out != nil {
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

// contextIDs lists cluster.contexts as an agent sees them, by id.
func contextIDs(t *testing.T, s *engine.Session) map[string]engine.ContextInfo {
	t.Helper()
	var list []engine.ContextInfo
	if err := sourcesDispatch(t, s, engine.OriginExternalAgent, "cluster.contexts", nil, &list); err != nil {
		t.Fatal(err)
	}
	out := map[string]engine.ContextInfo{}
	for _, c := range list {
		out[c.Name] = c
	}
	return out
}

// TestAddedKubeconfigConnects adds a fake API server's kubeconfig file in the GUI and connects to it
// as an agent would: the context is "<context> [<file>]", reads from its own file only, and the
// connection's UI state names that id. Neither results nor the audit log carry the token.
func TestAddedKubeconfigConnects(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: sourceToken})
	defer srv.Close()
	s, _, audit := sourcesSession(t)
	file := srv.WriteKubeconfig(t, "fake", "team-a")
	id := "fake [" + filepath.Base(file) + "]"

	var added commands.SourceAdded
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.addFile", map[string]string{"path": file}, &added); err != nil {
		t.Fatal(err)
	}
	if added.Source.ID != filepath.Base(file) || !slices.Equal(added.Contexts, []string{id}) {
		t.Fatalf("addFile: %+v", added)
	}
	ctxs := contextIDs(t, s)
	c, ok := ctxs[id]
	if !ok || c.KubeContext != "fake" || c.Source != filepath.Base(file) || c.File != file || c.Exec != "" {
		t.Fatalf("contexts: %+v", ctxs)
	}
	var res commands.ConnectResult
	if err := sourcesDispatch(t, s, engine.OriginExternalAgent, "cluster.connect", map[string]string{"context": id}, &res); err != nil {
		t.Fatal(err)
	}
	if res.State != engine.Connected || res.Context != id || s.UIState().Context != id {
		t.Fatalf("connect: %+v", res)
	}
	if b := s.Backend(); b == nil || b.Namespace != "team-a" || b.Conn.Context() != "fake" ||
		!slices.Equal(b.Conn.KubeconfigPaths(), []string{c.File}) {
		t.Fatalf("backend: %+v", b)
	}
	// A terminal binds kubectl to the context's name in the file, not to the id.
	var tab commands.DockResult
	if err := sourcesDispatch(t, s, engine.OriginGUI, "terminal.open", nil, &tab); err != nil {
		t.Fatal(err)
	}
	term, err := s.OpenTerminal(context.Background(), tab.Tab.ID)
	switch {
	case runtime.GOOS == "windows" && errors.Is(err, terminal.ErrNotImplemented):
		// No local terminal on Windows yet (ConPTY): the binding is not tried there.
	case err != nil:
		t.Fatalf("a terminal for a source context: %v", err)
	default:
		_ = term.Close()
	}

	var sources commands.SourcesResult
	if err := sourcesDispatch(t, s, engine.OriginExternalAgent, "kubeconfig.sources", nil, &sources); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(sources)
	if len(sources.Sources) != 1 || sources.Sources[0].Contexts != 1 || strings.Contains(string(out), sourceToken) {
		t.Errorf("sources: %s", out)
	}
	if strings.Contains(audit.String(), sourceToken) {
		t.Errorf("the audit log carries the token: %s", audit.String())
	}
}

// TestKubeconfigCommandsAreGUIOnly: adding, removing and allowing are refused for agents and the CLI,
// whatever they send; nothing is written, and the audit log never carries a token.
func TestKubeconfigCommandsAreGUIOnly(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: sourceToken})
	defer srv.Close()
	s, k, audit := sourcesSession(t)
	file := srv.WriteKubeconfig(t, "fake", "")
	calls := []struct {
		id     string
		params any
	}{
		{"kubeconfig.addFile", map[string]string{"path": file}},
		{"kubeconfig.addFolder", map[string]string{"path": filepath.Dir(file)}},
		{"kubeconfig.remove", map[string]string{"source": "dev.yaml"}},
		{"kubeconfig.approveExec", map[string]any{"approvals": []map[string]string{{"context": "x [y]", "fingerprint": "sha256:00"}}}},
	}
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		for _, c := range calls {
			if err := sourcesDispatch(t, s, origin, c.id, c.params, nil); !errors.Is(err, engine.ErrGUIOnly) {
				t.Errorf("%s from %s: %v, want ErrGUIOnly", c.id, origin, err)
			}
		}
	}
	if _, err := os.Stat(k.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused command wrote Cogpit's configuration directory: %v", err)
	}
	log := audit.String()
	if strings.Contains(log, sourceToken) || strings.Contains(log, "BEGIN") {
		t.Errorf("the audit log:\n%s", log)
	}
	for _, c := range calls {
		if !strings.Contains(log, `"command":"`+c.id+`"`) {
			t.Errorf("no audit line for %s", c.id)
		}
	}
	// The GUI may: the same file is added.
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.addFile", map[string]string{"path": file}, nil); err != nil {
		t.Fatal(err)
	}
}

// pluginScript writes an exec credential plugin that marks $COGPIT_TEST_MARKER and prints token.
func pluginScript(t *testing.T, dir, token string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the test plugin is a shell script")
	}
	script := "#!/bin/sh\necho ran >> \"$COGPIT_TEST_MARKER\"\n" +
		`echo '{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"` + token + `"}}'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "cred-plugin"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

// pluginKubeconfig is a kubeconfig for srv whose user runs ./cred-plugin (next to the file).
func pluginKubeconfig(srv *testkit.FakeAPIServer, marker string, args ...string) string {
	return `apiVersion: v1
kind: Config
clusters:
- name: fake
  cluster:
    server: ` + srv.URL + `
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(srv.CAData()) + `
users:
- name: plugin
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: ./cred-plugin
      args: ["` + strings.Join(args, `", "`) + `"]
      interactiveMode: Never
      env:
      - name: COGPIT_TEST_MARKER
        value: ` + marker + `
contexts:
- name: plugged
  context: {cluster: fake, user: plugin}
`
}

func pluginRuns(t *testing.T, marker string) int {
	t.Helper()
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "ran")
}

// TestExecPluginRunsOnlyOnceAllowed is the H-1 flow on a sync folder: a kubeconfig dropped into it
// names a credential plugin; connecting is refused with a reason for every origin and the plugin
// does not run; an agent may read the plugin's description (without its arguments) but not allow
// it; the GUI allows exactly what it showed (a stale fingerprint is refused); then the plugin runs
// and the connection uses its token; when the plugin's arguments change, connecting asks again.
func TestExecPluginRunsOnlyOnceAllowed(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: sourceToken})
	defer srv.Close()
	s, _, audit := sourcesSession(t)
	folder := t.TempDir()
	pluginScript(t, folder, sourceToken)
	marker := filepath.Join(t.TempDir(), "marker")
	kubeconfig := filepath.Join(folder, "team.yaml")
	if err := os.WriteFile(kubeconfig, []byte(pluginKubeconfig(srv, marker, "get-token", "--cluster", "prod")), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.addFolder", map[string]string{"path": folder}, nil); err != nil {
		t.Fatal(err)
	}
	id := "plugged [" + filepath.Base(folder) + "/team.yaml]"
	if c := contextIDs(t, s)[id]; c.Exec != engine.ExecRequired {
		t.Fatalf("context %q: %+v", id, contextIDs(t, s))
	}

	connect := func(origin engine.Origin) commands.ConnectResult {
		t.Helper()
		var res commands.ConnectResult
		if err := sourcesDispatch(t, s, origin, "cluster.connect", map[string]string{"context": id}, &res); err != nil {
			t.Fatalf("connect: %v", err)
		}
		return res
	}
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginGUI} {
		res := connect(origin)
		if res.State != engine.ConnectError || !strings.Contains(res.Error, "which you have not allowed yet") ||
			!strings.Contains(res.Error, filepath.Join(folder, "cred-plugin")) || strings.Contains(res.Error, marker) {
			t.Fatalf("connect from %s before approval: %+v", origin, res)
		}
	}
	if n := pluginRuns(t, marker); n != 0 {
		t.Fatalf("the plugin ran %d times before it was allowed", n)
	}

	var forAgent, forGUI commands.ExecPluginResult
	if err := sourcesDispatch(t, s, engine.OriginExternalAgent, "kubeconfig.execPlugin", map[string]string{"context": id}, &forAgent); err != nil {
		t.Fatal(err)
	}
	if forAgent.State != engine.ExecRequired || forAgent.Plugin == nil || forAgent.Plugin.Args != nil || forAgent.ArgsWithheld != 3 ||
		forAgent.Plugin.Command != filepath.Join(folder, "cred-plugin") || forAgent.Found != forAgent.Plugin.Command ||
		!slices.Equal(forAgent.Plugin.EnvNames, []string{"COGPIT_TEST_MARKER"}) {
		t.Fatalf("execPlugin for an agent: %+v", forAgent)
	}
	if out, _ := json.Marshal(forAgent); strings.Contains(string(out), marker) {
		t.Errorf("the plugin's environment value reached the agent: %s", out)
	}
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.execPlugin", map[string]string{"context": id}, &forGUI); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(forGUI.Plugin.Args, []string{"get-token", "--cluster", "prod"}) || forGUI.ArgsWithheld != 0 {
		t.Fatalf("execPlugin for the GUI: %+v", forGUI)
	}
	if err := sourcesDispatch(t, s, engine.OriginExternalAgent, "kubeconfig.approveExec",
		approvals(id, forGUI.Fingerprint), nil); !errors.Is(err, engine.ErrGUIOnly) {
		t.Fatalf("an agent allowed the plugin: %v", err)
	}
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.approveExec",
		approvals(id, "sha256:stale"), nil); !errors.Is(err, engine.ErrExecChangedSinceShown) {
		t.Fatalf("a stale fingerprint: %v", err)
	}
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.approveExec",
		approvals(id, forGUI.Fingerprint), nil); err != nil {
		t.Fatal(err)
	}
	if c := contextIDs(t, s)[id]; c.Exec != engine.ExecApproved {
		t.Fatalf("after approval: %+v", c)
	}
	if res := connect(engine.OriginExternalAgent); res.State != engine.Connected {
		t.Fatalf("connect after approval: %+v", res)
	}
	runs := pluginRuns(t, marker)
	if runs < 1 {
		t.Fatal("the allowed plugin did not run")
	}

	if after, _ := os.ReadFile(kubeconfig); !bytes.Equal(before, after) {
		t.Error("Cogpit wrote the synced kubeconfig while listing, allowing or connecting")
	}

	// The arguments change: the approval no longer applies, and nothing runs.
	if err := os.WriteFile(kubeconfig, []byte(pluginKubeconfig(srv, marker, "get-token", "--cluster", "other")), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ = os.ReadFile(kubeconfig)
	if c := contextIDs(t, s)[id]; c.Exec != engine.ExecChanged {
		t.Fatalf("after the change: %+v", c)
	}
	if err := s.Disconnect(); err != nil { // a connect again would only show the open connection (D-69)
		t.Fatal(err)
	}
	if res := connect(engine.OriginGUI); res.State != engine.ConnectError || !strings.Contains(res.Error, "changed since you allowed it") {
		t.Fatalf("connect after the change: %+v", res)
	}
	if n := pluginRuns(t, marker); n != runs {
		t.Fatalf("the changed plugin ran (%d → %d)", runs, n)
	}
	if after, _ := os.ReadFile(kubeconfig); !bytes.Equal(before, after) {
		t.Error("Cogpit wrote the synced kubeconfig")
	}
	if strings.Contains(audit.String(), sourceToken) || strings.Contains(audit.String(), marker) {
		t.Errorf("the audit log carries the token or the plugin's environment:\n%s", audit.String())
	}
}

// approvals is the parameter of kubeconfig.approveExec for one context.
func approvals(id, fingerprint string) map[string]any {
	return map[string]any{"approvals": []map[string]string{{"context": id, "fingerprint": fingerprint}}}
}
