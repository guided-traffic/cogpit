package engine

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// heldPluginRuns counts the runs a test credential plugin noted in its file.
func heldPluginRuns(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "run\n")
}

// writeCredential makes the test plugin hand out token, without an expiry.
func writeCredential(t *testing.T, path, token string) {
	t.Helper()
	cred := `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"` + token + `"}}`
	if err := os.WriteFile(path, []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHeldPluginOnARealStack is D-69 item 4 end to end on client-go: cluster alpha logs in with a
// credential plugin (a script that notes each run), its setting off. Shown, it connects with one
// run; left for beta, it holds its login, and when the server stops taking the token (a 401, as for
// a revoked or expired login) no plugin process starts: the login lapses (not red). Shown again, the
// plugin runs once and the cluster works. A held credential's expiry is the cluster package's
// TestHeldPluginDoesNotRun.
func TestHeldPluginOnARealStack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test plugin is a shell script")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const token = "held-plugin-token-0042"
	alphaSrv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: token})
	t.Cleanup(alphaSrv.Close)
	betaSrv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(betaSrv.Close)

	dir := t.TempDir()
	runs, cred, plugin := filepath.Join(dir, "runs"), filepath.Join(dir, "cred.json"), filepath.Join(dir, "login-plugin")
	if err := os.WriteFile(plugin, []byte("#!/bin/sh\necho run >> \"$PLUGIN_RUNS\"\ncat \"$PLUGIN_CRED\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCredential(t, cred, token)
	alphaCfg := filepath.Join(dir, "alpha.yaml")
	if err := os.WriteFile(alphaCfg, []byte(`apiVersion: v1
kind: Config
clusters:
- name: alpha-cluster
  cluster:
    server: `+alphaSrv.URL+`
    certificate-authority-data: `+base64.StdEncoding.EncodeToString(alphaSrv.CAData())+`
users:
- name: alpha-plugin
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: `+plugin+`
      interactiveMode: Never
      env:
      - name: PLUGIN_RUNS
        value: `+runs+`
      - name: PLUGIN_CRED
        value: `+cred+`
contexts:
- name: alpha
  context: {cluster: alpha-cluster, user: alpha-plugin}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(Options{
		Connector:        &ClusterConnector{KubeconfigPaths: []string{alphaCfg, fakeClusterKubeconfig(t, betaSrv, "beta")}},
		SnapshotInterval: 20 * time.Millisecond, ClusterSettings: NewClusterSettingsStore(""),
		ConnWatch: ConnWatchOptions{Heartbeat: 100 * time.Millisecond, ProbeTimeout: 2 * time.Second, Confirm: 50 * time.Millisecond,
			LoginWait: 5 * time.Second, BackoffFirst: 100 * time.Millisecond, BackoffMax: 400 * time.Millisecond},
	})
	t.Cleanup(s.Close)
	if _, err := s.ChangeClusterSettings("alpha", func(cs *ClusterSettings) error { cs.HoldBackgroundLogin = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "beta"); err != nil {
		t.Fatal(err)
	}
	if n := heldPluginRuns(t, runs); n != 1 {
		t.Fatalf("alpha's plugin ran %d times before its token was revoked, want once (the connect)", n)
	}
	// What the plugin hands out next is the token the server takes: a renewal later is one run.
	const rotated = "held-plugin-token-0043"
	writeCredential(t, cred, rotated)
	alphaSrv.SetToken(rotated)
	if !testkit.WaitFor(ctx, func() bool { return entryOf(s, "alpha").LoginLapsed }) {
		t.Fatalf("alpha's login did not lapse: %+v", entryOf(s, "alpha"))
	}
	if e := entryOf(s, "alpha"); e.Connection.State != Connected {
		t.Errorf("alpha's lapsed login shows as %s", e.Connection.State)
	}
	if n := heldPluginRuns(t, runs); n != 1 {
		t.Fatalf("alpha's plugin ran %d times while held", n-1)
	}

	if err := s.Connect(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool {
		u := s.UIState()
		return u.Context == "alpha" && u.Connection.State == Connected && u.Connection.Reason == "" && !entryOf(s, "alpha").LoginLapsed
	}) {
		t.Fatalf("alpha was not renewed when shown: %+v", s.UIState().Connection)
	}
	if n := heldPluginRuns(t, runs); n != 2 {
		t.Errorf("showing alpha ran its plugin %d times, want once", n-1)
	}
	if err := s.writeBlocked(DangerMutating, "alpha"); err != nil {
		t.Errorf("a write to the renewed alpha: %v", err)
	}
}
