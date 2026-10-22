package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A failed connect's page and the connect's stages (D-114, the owner's AW17 of 2026-10-22).

// certGoneKubeconfig is a kubeconfig whose context name logs in by a client certificate whose files
// are gone, as the owner's walk-cert-gone after its certificate was removed.
func certGoneKubeconfig(t *testing.T, name string) (path, cert string) {
	t.Helper()
	dir := t.TempDir()
	cert = filepath.Join(dir, "walk-cert", "client.crt")
	doc := "apiVersion: v1\nkind: Config\ncurrent-context: " + name + "\nclusters:\n- name: c\n  cluster: {server: \"https://127.0.0.1:1\"}\n" +
		"users:\n- name: u\n  user:\n    client-certificate: " + cert + "\n    client-key-data: Zm9v\n" +
		"contexts:\n- name: " + name + "\n  context: {cluster: c, user: u}\n"
	path = filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, cert
}

// stageResults sums up stages as "name=result" in their order.
func stageResults(stages []ConnectStage) string {
	parts := make([]string, len(stages))
	for i, st := range stages {
		parts[i] = st.Name + "=" + st.Result
	}
	return strings.Join(parts, " ")
}

// The restore of the last session whose shown cluster fails to connect (its client certificate
// file gone) leaves the window on that cluster with its error page, not the welcome page: the
// error is the cause in a sentence, the failure keeps the stages, the log the whole chain.
func TestARestoreThatFailsShowsTheErrorPage(t *testing.T) {
	alpha := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(alpha.Close)
	gone, cert := certGoneKubeconfig(t, "walk-cert-gone")
	dir := filepath.Join(t.TempDir(), "Cogpit")
	writeWorkspaceText(t, dir, `{"version": 1, "active": 1, "tabs": [
  {"context": "alpha", "view": "pods", "viewTitle": "Pods", "used": 1},
  {"context": "walk-cert-gone", "view": "pods", "viewTitle": "Pods", "used": 2}
], "connected": ["alpha", "walk-cert-gone"], "shown": "walk-cert-gone"}`)
	s := New(Options{
		Connector:        &ClusterConnector{KubeconfigPaths: []string{fakeClusterKubeconfig(t, alpha, "alpha"), gone}},
		SnapshotInterval: 10 * time.Millisecond, Workspace: NewWorkspace(dir).WithDelay(time.Millisecond),
		Preferences: restorePreferences(t, dir, true), ConnectLogDir: t.TempDir(),
	})
	t.Cleanup(s.Close)
	s.RestoreLastSession("")
	waitUntil(t, "the failed connect", func() bool { return s.UIState().Connection.State == ConnectError })
	u := s.UIState()
	if u.Welcome {
		t.Error("the welcome page shows over the failed connect of the shown cluster")
	}
	want := "the client certificate file " + cert + " does not exist"
	if u.Context != "walk-cert-gone" || u.Connection.Error != want {
		t.Errorf("shown %q, error %q, want %q", u.Context, u.Connection.Error, want)
	}
	f := u.ConnectFailure
	if f == nil || f.Context != "walk-cert-gone" || f.At.IsZero() {
		t.Fatalf("the failure %+v", f)
	}
	if got := stageResults(f.Stages); got != "kubeconfig=failed dns=notReached tcp=notReached tls=notReached api=notReached" || f.Stages[0].Text != want {
		t.Errorf("stages %s, the first %+v", got, f.Stages[0])
	}
	attempts, err := s.ConnectAttempts("walk-cert-gone", 1)
	if err != nil || len(attempts) != 1 || stageResults(attempts[0].Stages) != stageResults(f.Stages) ||
		!strings.Contains(lastLine(attempts[0]), "invalid kubeconfig: invalid configuration: unable to read client-cert") {
		t.Errorf("the log's attempt %+v (%v)", attempts, err)
	}
	waitUntil(t, "alpha connected in the background", func() bool {
		c, ok := s.UIState().ConnectionOf("alpha")
		return ok && c.Connection.State == Connected
	})
	if u := s.UIState(); u.Welcome || u.ConnectFailure == nil || u.Context != "walk-cert-gone" {
		t.Errorf("after alpha connected behind it: welcome %v, failure %+v, shown %q", u.Welcome, u.ConnectFailure, u.Context)
	}
}

// Every connect's attempt keeps its stages, a successful one's too, and a restart reads them back
// from the file.
func TestConnectLogKeepsTheStages(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: &testkit.FakeAccess{}})
	defer srv.Close()
	dir := t.TempDir()
	conn := &ClusterConnector{KubeconfigPaths: []string{insecureKubeconfig(t, srv)}}
	s := New(Options{Connector: conn, ConnectLogDir: dir})
	if err := s.Connect(context.Background(), "checked"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ConnectAttempts("checked", 1)
	const want = "kubeconfig=ok dns=skipped tcp=ok tls=ok api=ok"
	if len(got) != 1 || stageResults(got[0].Stages) != want || !strings.Contains(got[0].Stages[4].Text, "Kubernetes v") {
		t.Fatalf("stages %+v", got)
	}
	if u := s.UIState(); u.ConnectFailure != nil {
		t.Errorf("a failure after a connect that worked: %+v", u.ConnectFailure)
	}
	s.Close()
	again := New(Options{Connector: conn, ConnectLogDir: dir})
	t.Cleanup(again.Close)
	read, err := again.ConnectAttempts("checked", 1)
	if err != nil || len(read) != 1 || stageResults(read[0].Stages) != want || read[0].Stages[1].Text != got[0].Stages[1].Text {
		t.Errorf("read back %+v (%v)", read, err)
	}
}

// A connect that fails before the cluster layer runs (no source offers the context) has the
// kubeconfig stage failed with its problem, the network stages not reached.
func TestAFailureBeforeConnectHasItsStages(t *testing.T) {
	err := withConfigStages(&cluster.ConnectError{Context: "x", Problem: "no such context in the kubeconfig sources"})
	stages, cause := cluster.StagesOf(err)
	if got := stageResults(connectStages(stages)); got != "kubeconfig=failed dns=notReached tcp=notReached tls=notReached api=notReached" ||
		cause != "no such context in the kubeconfig sources" {
		t.Errorf("stages %s, cause %q", got, cause)
	}
	plain := withConfigStages(errors.New("the source cannot be read"))
	if _, cause := cluster.StagesOf(plain); cause != "the source cannot be read" || plain.Error() != "the source cannot be read" {
		t.Errorf("a plain error: %q, %q", cause, plain.Error())
	}
}

// Help › Welcome over a failed connect shows the welcome page: the failed cluster is left
// disconnected as Disconnect leaves it, its failure gone.
func TestWelcomeOverAFailedConnect(t *testing.T) {
	gone, _ := certGoneKubeconfig(t, "walk-cert-gone")
	s := New(Options{Connector: &ClusterConnector{KubeconfigPaths: []string{gone}}})
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "walk-cert-gone"); err == nil {
		t.Fatal("connected")
	}
	if u := s.UIState(); u.Welcome || u.ConnectFailure == nil {
		t.Fatalf("after the failure: welcome %v, failure %+v", u.Welcome, u.ConnectFailure)
	}
	u, err := s.OpenWelcome()
	if err != nil || !u.Welcome || u.ConnectFailure != nil || u.Connection.State != Disconnected || u.Context != "walk-cert-gone" {
		t.Errorf("Help › Welcome: %v, welcome %v, failure %+v, %s %q", err, u.Welcome, u.ConnectFailure, u.Connection.State, u.Context)
	}
}
