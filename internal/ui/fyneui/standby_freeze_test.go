//go:build standby

package fyneui

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"k8s.io/client-go/tools/clientcmd"                  //nolint:depguard // the hand-run reproduction (tag standby) writes its own kind cluster's kubeconfig
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api" //nolint:depguard // as above

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestStandbyFreeze is D-58's S1: the window against a kind cluster of its own whose control plane
// is frozen (docker pause), as a peer gone silent after the computer slept. It drives the window
// offscreen, measures how long the main goroutine's work takes and how long the engine's UIState
// takes while frozen, and writes goroutine dumps (COGPIT_DUMP_DIR, else a temporary directory) when
// one of them blocks longer than a second and at the end of the freeze.
//
// It pauses a docker container: never run it against a cluster others use. Run it alone:
//
//	COGPIT_STANDBY_KUBECONFIG=.cache/standby-kubeconfig COGPIT_STANDBY_CONTEXT=kind-cogpit-standby \
//	COGPIT_STANDBY_NODE=cogpit-standby-control-plane go test -tags standby -run TestStandbyFreeze ./internal/ui/fyneui
func TestStandbyFreeze(t *testing.T) {
	kubeconfig, contextName, node := os.Getenv("COGPIT_STANDBY_KUBECONFIG"), os.Getenv("COGPIT_STANDBY_CONTEXT"), os.Getenv("COGPIT_STANDBY_NODE")
	if kubeconfig == "" || contextName == "" || node == "" {
		t.Skip("COGPIT_STANDBY_KUBECONFIG, COGPIT_STANDBY_CONTEXT and COGPIT_STANDBY_NODE are not set")
	}
	freeze := 120 * time.Second
	if d, err := time.ParseDuration(os.Getenv("COGPIT_STANDBY_FREEZE")); err == nil {
		freeze = d
	}
	dir := os.Getenv("COGPIT_DUMP_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	s := newTestEngine(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: contextName}, q.do)
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	defer func() {
		sh.stop()
		q.drain()
	}()

	dumps := 0
	dump := func(why string) {
		dumps++
		f, err := os.Create(filepath.Join(dir, "goroutines-"+why+".txt"))
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		_ = pprof.Lookup("goroutine").WriteTo(f, 2)
		t.Log("goroutine dump:", f.Name())
	}

	// The main goroutine's watchdog: the start of the work it runs now, zero while idle.
	var busySince atomic.Int64
	var worst atomic.Int64
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		dumped := false
		for {
			select {
			case <-stopWatch:
				return
			case <-time.After(50 * time.Millisecond):
			}
			if since := busySince.Load(); since != 0 {
				d := time.Since(time.Unix(0, since))
				if int64(d) > worst.Load() {
					worst.Store(int64(d))
				}
				if d > time.Second && !dumped {
					dumped = true
					dump("main-blocked")
				}
			}
		}
	}()
	step := func() {
		busySince.Store(time.Now().UnixNano())
		q.drain()
		settle(w)
		_ = w.Canvas().Capture() // the painter's work
		busySince.Store(0)
	}
	until := func(what string, limit time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(limit)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			step()
			time.Sleep(10 * time.Millisecond)
		}
		step()
	}
	var pod model.ObjectRef
	until("a running pod", 90*time.Second, func() bool {
		tbl := s.Table("pods")
		if tbl == nil || !tbl.Synced {
			return false
		}
		for _, r := range tbl.Rows {
			if r.Ref.Namespace == "cogpit-demo" && strings.HasPrefix(r.Ref.Name, "web-") && r.Status == model.StatusOK {
				pod = r.Ref
				return true
			}
		}
		return false
	})
	// What a user has open: a pod's drawer, its logs, the dock.
	sh.openDetails(pod)
	sh.dispatch("pod.logs", map[string]any{"ref": pod}, nil)
	until("the logs tab", 30*time.Second, func() bool { return len(s.UIState().Dock) > 0 })
	for range 100 {
		step()
		time.Sleep(10 * time.Millisecond)
	}

	if out, err := exec.Command("docker", "pause", node).CombinedOutput(); err != nil {
		t.Fatalf("docker pause: %v: %s", err, out)
	}
	paused := true
	unpause := func() {
		if paused {
			if out, err := exec.Command("docker", "unpause", node).CombinedOutput(); err != nil {
				t.Errorf("docker unpause: %v: %s", err, out)
			}
			paused = false
		}
	}
	defer unpause()
	t.Logf("paused %s at %s", node, time.Now().Format(time.TimeOnly))

	// The engine's lock: UIState from another goroutine, as the MCP server and the agent read it.
	var uiWorst atomic.Int64
	stopUI := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopUI:
				return
			case <-time.After(200 * time.Millisecond):
			}
			t0 := time.Now()
			_ = s.UIState()
			if d := time.Since(t0); int64(d) > uiWorst.Load() {
				uiWorst.Store(int64(d))
			}
		}
	}()

	// While frozen, the user clicks around: other views, the drawer again, the row menu's actions,
	// a write (Delete pod, declined by the test's confirmer being absent: it waits), a shell.
	views := []string{"deployments", "services", "nodes", "pods", "namespaces", "events"}
	start := time.Now()
	next := start
	i := 0
	for time.Since(start) < freeze {
		if time.Now().After(next) {
			next = time.Now().Add(5 * time.Second)
			v := views[i%len(views)]
			i++
			raw, _ := json.Marshal(map[string]any{"view": v})
			go func() {
				_, _ = s.Dispatch(context.Background(), engine.Invocation{ID: "ui.openView", Params: raw, Origin: engine.OriginGUI})
			}()
			busySince.Store(time.Now().UnixNano())
			sh.openDetails(pod)
			busySince.Store(0)
			if i == 3 {
				sh.dispatch("pod.shell", map[string]any{"ref": pod}, nil)
			}
			t.Logf("%5.1fs: view %s, state %s, main worst %v, UIState worst %v", time.Since(start).Seconds(), v,
				s.UIState().Connection.State, time.Duration(worst.Load()), time.Duration(uiWorst.Load()))
		}
		step()
		time.Sleep(10 * time.Millisecond)
	}
	close(stopUI)
	dump("frozen-end")
	unpause()
	t.Logf("unpaused at %s", time.Now().Format(time.TimeOnly))
	recovered := time.Now()
	until("the pods again", 3*time.Minute, func() bool {
		tbl := s.Table(s.UIState().View)
		return tbl != nil && tbl.Synced && tbl.Error == ""
	})
	t.Logf("after the unfreeze: view %s synced after %v; main worst %v, UIState worst %v, dumps %d",
		s.UIState().View, time.Since(recovered), time.Duration(worst.Load()), time.Duration(uiWorst.Load()), dumps)
}

// TestStandbyPluginWaits is S1's second case: a credential plugin that waits (a browser login
// nobody answers) once the credential it gave expired, as one may at wake. COGPIT_STANDBY_PLUGIN is
// a plugin that prints the kind cluster's client certificate valid for 20 seconds and waits while a
// file "block" lies beside it.
func TestStandbyPluginWaits(t *testing.T) {
	kubeconfig, plugin := os.Getenv("COGPIT_STANDBY_KUBECONFIG"), os.Getenv("COGPIT_STANDBY_PLUGIN")
	if kubeconfig == "" || plugin == "" {
		t.Skip("COGPIT_STANDBY_KUBECONFIG and COGPIT_STANDBY_PLUGIN are not set")
	}
	dir := os.Getenv("COGPIT_DUMP_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	cfg, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range cfg.AuthInfos {
		*u = clientcmdapi.AuthInfo{Exec: &clientcmdapi.ExecConfig{APIVersion: "client.authentication.k8s.io/v1", Command: plugin,
			InteractiveMode: clientcmdapi.NeverExecInteractiveMode}}
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		t.Fatal(err)
	}
	block := filepath.Join(filepath.Dir(plugin), "block")
	_ = os.Remove(block)
	defer os.Remove(block)

	s := newTestEngine(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{path}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: cfg.CurrentContext}, q.do)
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	defer func() {
		sh.stop()
		q.drain()
	}()
	var worst time.Duration
	step := func() {
		t0 := time.Now()
		q.drain()
		settle(w)
		_ = w.Canvas().Capture()
		worst = max(worst, time.Since(t0))
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		tbl := s.Table("pods")
		if tbl != nil && tbl.Synced {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no pods")
		}
		step()
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(block, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("blocking the plugin at %s", time.Now().Format(time.TimeOnly))
	// A kind not read yet each time: a list request after the credential expired.
	views := []string{"deployments", "services", "nodes", "configmaps", "secrets", "serviceaccounts", "jobs", "cronjobs",
		"daemonsets", "statefulsets", "replicasets", "ingresses", "persistentvolumeclaims", "endpoints"}
	var uiWorst time.Duration
	start := time.Now()
	next := start
	i := 0
	for time.Since(start) < 70*time.Second {
		if time.Now().After(next) {
			next = time.Now().Add(5 * time.Second)
			raw, _ := json.Marshal(map[string]any{"view": views[i%len(views)]})
			i++
			go func() {
				_, _ = s.Dispatch(context.Background(), engine.Invocation{ID: "ui.openView", Params: raw, Origin: engine.OriginGUI})
			}()
			t0 := time.Now()
			st := s.UIState()
			uiWorst = max(uiWorst, time.Since(t0))
			var errText string
			if tbl := s.Table(st.View); tbl != nil {
				errText = tbl.Error
			}
			t.Logf("%5.1fs: state %s, view %s, error %q, main worst %v", time.Since(start).Seconds(), st.Connection.State, st.View, errText, worst)
		}
		step()
		time.Sleep(10 * time.Millisecond)
	}
	if f, err := os.Create(filepath.Join(dir, "goroutines-plugin-waits.txt")); err == nil {
		_ = pprof.Lookup("goroutine").WriteTo(f, 2)
		_ = f.Close()
		t.Log("goroutine dump:", f.Name())
	}
	_ = os.Remove(block)
	t.Logf("released the plugin; main worst %v, UIState worst %v", worst, uiWorst)
	deadline = time.Now().Add(60 * time.Second)
	for {
		tbl := s.Table(s.UIState().View)
		if tbl != nil && tbl.Synced && tbl.Error == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not recovered")
		}
		step()
		time.Sleep(10 * time.Millisecond)
	}
	t.Log("recovered")
}
