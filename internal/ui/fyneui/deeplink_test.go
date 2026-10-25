package fyneui

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// raisedWindow counts the window's raises.
type raisedWindow struct {
	fyne.Window
	raised atomic.Int32
}

func (w *raisedWindow) RequestFocus() { w.raised.Add(1); w.Window.RequestFocus() }

// The cogpit:// links that came before the window wait until it attaches; then each is opened by
// the engine, one after the other, with the window brought to the front for each: a link to the
// demo cluster's Deployments asks the user before it connects (A7), its question naming the
// context and who asked, then connects it and shows them with a toast saying so; a link to a
// context Cogpit does not know changes nothing, and a toast says why.
func TestDeepLinksOpenInTheWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{IncludeDemo: true}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 50, true, 5
	s := newTestEngine(engine.Options{Connector: conn})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newEngineShell(t, s, Options{})
	rw := &raisedWindow{Window: w}
	sh.win = rw
	sh.confirmer.armDelay = 0
	s.SetConfirmer(sh.confirmer)

	links := engine.NewDeepLinkQueue(nil)
	links.Deliver("cogpit://open?context=demo&view=deployments")
	links.Deliver("cogpit://open?context=prod&view=pods")
	if links.Pending() != 2 {
		t.Fatalf("%d links waiting", links.Pending())
	}
	toasts := len(sh.toasts.Objects)
	detach := sh.attachDeepLinks(links)
	defer detach()
	q.until(t, "the link's question", func() bool { return sh.confirmer.current != nil })
	texts := strings.Join(overlayTexts(w), "\n")
	if !strings.Contains(texts, "Opens") || !strings.Contains(texts, "the deployments view") || !strings.Contains(texts, "A cogpit:// link") {
		t.Errorf("the question:\n%s", texts)
	}
	test.Tap(sh.confirmer.current.confirm)
	q.until(t, "both links opened", func() bool {
		u := sh.ui
		return u.Context == engine.DemoContext && u.View == "deployments" && rw.raised.Load() == 2 &&
			len(sh.toasts.Objects) >= toasts+2
	})
	if links.Pending() != 0 {
		t.Errorf("%d links still waiting", links.Pending())
	}
	notes := s.Notifications(true)
	var opened, refused bool
	for _, n := range notes.Notifications {
		opened = opened || (n.Level == engine.LevelInfo && n.Message == "Opened from a link: Deployments on demo")
		refused = refused || (n.Level == engine.LevelWarning && n.Message == "Cogpit did not follow a link: the link names "+
			"no single context of Cogpit's kubeconfig sources: prod")
	}
	if !opened || !refused {
		t.Errorf("notifications %+v", notes.Notifications)
	}

	// A fake engine without OpenDeepLink leaves the links waiting.
	f := newFakeEngine(t)
	fsh, _, _ := newTestShell(t, f, Options{})
	later := engine.NewDeepLinkQueue(nil)
	later.Deliver("cogpit://open?view=pods")
	fsh.attachDeepLinks(later)()
	time.Sleep(10 * time.Millisecond)
	if later.Pending() != 1 {
		t.Errorf("a fake engine took the link")
	}
}
