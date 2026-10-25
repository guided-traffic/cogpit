package fyneui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestRenderDemoShowcase renders the shell over the real engine and the real demo cluster (10,000
// pods, 1,000 watch events per second) after an agent's ui.navigate to the Service with ClusterIP
// 10.96.100.100: Services open, the row selected, its drawer open on the YAML. It is the offscreen
// counterpart of the MVP's agent demo; the PNG is testdata/demo_showcase.png with -update.
func TestRenderDemoShowcase(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the 10,000-pod demo cluster")
	}
	s := newTestEngine(engine.Options{Connector: &engine.ClusterConnector{IncludeDemo: true}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: engine.DemoContext, MCPCommand: "claude mcp add …"}, q.do)
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	defer func() {
		sh.stop()
		q.drain()
	}()

	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	until("the Pods view", func() bool {
		tbl := s.Table("pods")
		return tbl != nil && tbl.Synced && tbl.Total > 9000 && len(sh.kinds) > 0
	})
	until("the status bar's diagnostics", func() bool { return sh.diag.informers > 0 && sh.diag.demoObjects > 0 })
	savePNG(t, w, "demo_pods.png")

	ref := model.ObjectRef{Kind: "services", Namespace: "demo-mcp", Name: "web"}
	raw, _ := json.Marshal(map[string]any{"ref": ref})
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "ui.navigate", Params: raw, Origin: engine.OriginExternalAgent}); err != nil {
		t.Fatal(err)
	}
	until("the drawer on the Service", func() bool {
		tbl := s.Table("services")
		return tbl != nil && tbl.IndexOf(ref) >= 0 && sh.drawer.last != nil && sh.drawer.last.Ref.Same(ref)
	})
	time.Sleep(300 * time.Millisecond) // let a snapshot and the drawer refresh land
	q.drain()
	savePNG(t, w, "demo_showcase.png")
}
