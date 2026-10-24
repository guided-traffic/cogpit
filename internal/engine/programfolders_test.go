package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A connect whose credential plugin is not found (D-120): the error the window and agents read, the
// failed connect's login stage and its attempt in the connection log name the program and point to
// Preferences › Kubernetes › Program folders; the stage carries the program for the error page's
// button.
func TestAMissingPluginPointsToProgramFolders(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(srv.Close)
	const program = "cogpit-test-no-such-login-0815"
	doc := strings.Replace(string(srv.KubeconfigWithToken("plug", "", "")), "  user: {}",
		"  user:\n    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: "+program+"\n      interactiveMode: Never", 1)
	path := filepath.Join(t.TempDir(), "plug.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Connector: &ClusterConnector{KubeconfigPaths: []string{path}}, ConnectLogDir: t.TempDir()})
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "plug"); err == nil {
		t.Fatal("connected without the plugin")
	}
	const hint = "add the folder it is in under Preferences › Kubernetes › Program folders"
	u := s.UIState()
	if !strings.Contains(u.Connection.Error, "no program "+program+" on PATH") || !strings.Contains(u.Connection.Error, hint) {
		t.Errorf("the error %q", u.Connection.Error)
	}
	if u.ConnectFailure == nil {
		t.Fatal("no failure in the UI state")
	}
	login := stageOf(u.ConnectFailure.Stages, cluster.StageLogin)
	if login.Result != cluster.StageFailed || login.MissingProgram != program || !strings.Contains(login.Text, hint) {
		t.Errorf("the login stage %+v", login)
	}
	attempts, err := s.ConnectAttempts("plug", 1)
	if err != nil || len(attempts) != 1 || !strings.Contains(stageOf(attempts[0].Stages, cluster.StageLogin).Text, hint) {
		t.Errorf("the log's attempt %+v (%v)", attempts, err)
	}
}

// Another failure of a plugin says nothing of program folders.
func TestFailedConnectionWithoutAMissingProgram(t *testing.T) {
	err := &cluster.ConnectError{Problem: "x", Cause: "the credential plugin kubelogin failed: exec: executable kubelogin failed with exit code 1",
		Stages: []cluster.Stage{{Name: cluster.StageKubeconfig, Result: cluster.StageOK},
			{Name: cluster.StageLogin, Result: cluster.StageFailed, Text: "the credential plugin kubelogin failed: exec: executable kubelogin failed with exit code 1"}}}
	conn, stages := failedConnection(err, "x", false)
	if strings.Contains(conn.Error, "Program folders") || stageOf(stages, cluster.StageLogin).MissingProgram != "" {
		t.Errorf("%q %+v", conn.Error, stages)
	}
	err.Stages[1].Missing = "kubectl-oidc_login"
	conn, _ = failedConnection(err, "x", false)
	if !strings.Contains(conn.Error, "if kubectl-oidc_login is installed, add the folder it is in under Preferences › Kubernetes › Program folders") {
		t.Errorf("with the program missing: %q", conn.Error)
	}
	if conn, _ = failedConnection(err, "the caller's own end", true); conn.Error != "the caller's own end" {
		t.Errorf("the caller's own end: %q", conn.Error)
	}
}

func stageOf(stages []ConnectStage, name string) ConnectStage {
	for _, st := range stages {
		if st.Name == name {
			return st
		}
	}
	return ConnectStage{}
}

// A folder added is on PATH before anyone reads it in the preferences: a reader that sees the
// new folder (a connect, the window's settings page) finds it on PATH already (the Windows UI
// test that read the preferences first and PATH after, 2026-10-24).
func TestAProgramFolderIsOnPathBeforeThePreferencesSayIt(t *testing.T) {
	t.Setenv("PATH", filepath.FromSlash("/usr/bin"))
	s := New(Options{Preferences: NewPreferencesStore(t.TempDir())})
	defer s.Close()
	for i := range 40 {
		dir := filepath.Join(t.TempDir(), "bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := s.AddProgramFolder(dir); done <- err }()
		for {
			p, _ := s.Preferences()
			if slices.Contains(p.ProgramFolders, dir) {
				break
			}
			runtime.Gosched()
		}
		if path := os.Getenv("PATH"); !strings.Contains(path, dir) {
			t.Fatalf("round %d: the preferences hold %s, PATH %q does not", i, dir, path)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if _, err := s.RemoveProgramFolder(dir); err != nil {
			t.Fatal(err)
		}
	}
}
