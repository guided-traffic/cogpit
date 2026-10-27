package fyneui

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/terminal"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/termview"
)

// kubectlEngine is the fake engine with a terminal whose shell shows a prompt and the old kubectl.
type kubectlEngine struct {
	*fakeEngine
	last *atomic.Pointer[openStream]
}

const kubectlTerminalOutput = "~ $ kubectl version --client\r\nClient Version: v1.30.2\r\nKustomize Version: v5.0.4-0.20230601165947-6ce0bf390ce3\r\n~ $ "

func (k kubectlEngine) OpenTerminal(ctx context.Context, tabID string) (terminal.Session, error) {
	if !strings.HasPrefix(tabID, "terminal-") {
		return k.fakeEngine.OpenTerminal(ctx, tabID)
	}
	k.mu.Lock()
	k.opened[tabID]++
	k.mu.Unlock()
	s := newGatedStream(k.fakeEngine, tabID, kubectlTerminalOutput)
	k.last.Store(s)
	return s, nil
}

// offerCheck is what the engine reports for a terminal of a 1.36.1 cluster whose PATH has kubectl
// 1.30.2.
func offerCheck() *engine.KubectlCheck {
	return &engine.KubectlCheck{Server: "1.36.1", Found: "/usr/local/bin/kubectl", FoundVersion: "1.30.2", Offer: "1.36.1",
		Message: "kubectl 1.36.1 matches this cluster; the kubectl on PATH is 1.30.2, more than one minor version away."}
}

// kubectlScene is the pods view of kind-cogpit with a terminal tab in the dock whose kubectl is
// check, the terminal's output shown.
func kubectlScene(t *testing.T, mode ThemeMode, check *engine.KubectlCheck) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := connectedFake(t, 200)
	f.quietTerminals = true // their output would race the next test under the test driver (quietReader)
	f.ui.Context, f.ui.Connection = "kind-cogpit", engine.Connection{State: engine.Connected}
	f.ui.Dock = []engine.DockTab{{ID: "terminal-1", Type: engine.DockTerminal, Title: "Terminal 1", Context: "kind-cogpit", Kubectl: check}}
	f.ui.ActiveDock = "terminal-1"
	eng := kubectlEngine{fakeEngine: f, last: &atomic.Pointer[openStream]{}}
	sh, w, q := startShell(t, eng, f, Options{Theme: mode})
	// As attachScene: the output flows only once the widget waits in Read, and the test goroutine
	// touches no widget until the widget has processed it all.
	q.until(t, "the terminal to read", func() bool {
		s := eng.last.Load()
		return len(sh.kinds) > 0 && s != nil && s.reading.Load()
	})
	q.drain()
	s := eng.last.Load()
	close(s.gate)
	deadline := time.Now().Add(5 * time.Second)
	for !s.idle.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the terminal did not take its output")
		}
		time.Sleep(time.Millisecond)
	}
	terms := findAll[*termview.Terminal](sh.dock.contents["terminal-1"].obj)
	if len(terms) != 1 {
		t.Fatalf("the terminal tab shows %d terminals", len(terms))
	}
	t.Cleanup(func() {
		_ = terms[0].Close()
		select {
		case <-terms[0].Done():
		case <-time.After(10 * time.Second):
			t.Error("the terminal's widget did not end")
		}
	})
	return sh, w, q, f
}

// TestKubectlOfferDownloadsThroughTheEngine: the line shows the engine's text with Download; a
// click dispatches kubectl.download for the tab from the GUI, the line follows the download's
// state, and New Terminal opens a terminal once it is in place.
func TestKubectlOfferDownloadsThroughTheEngine(t *testing.T) {
	skipTerminalUnderRace(t)
	sh, _, q, f := kubectlScene(t, ThemeDark, offerCheck())
	offer := sh.dock.contents["terminal-1"].kubectl
	if offer == nil || !offer.root.Visible() || offer.text.text != offerCheck().Message {
		t.Fatalf("the line: %+v", offer)
	}
	if !offerButtonShown(offer, offer.download) || offerButtonShown(offer, offer.newTerm) {
		t.Fatal("the offer has no Download button, or a New Terminal one")
	}

	release := make(chan struct{})
	f.mu.Lock()
	f.handlers["kubectl.download"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		set := func(d engine.KubectlDownload, msg string) {
			f.update(func(u *engine.UIState) {
				c := *offerCheck()
				c.Download, c.Message = &d, msg
				u.Dock[0].Kubectl = &c
			})
		}
		set(engine.KubectlDownload{State: engine.KubectlRunning, Done: 23_100_000, Total: 57_000_000}, "Downloading kubectl 1.36.1… 22.0 MiB of 54.4 MiB")
		<-release
		set(engine.KubectlDownload{State: engine.KubectlDone, Path: "/cfg/Cogpit/kubectl/1.36.1/kubectl"},
			"kubectl 1.36.1 is downloaded and verified: terminals opened from now on find it first.")
		return map[string]string{"path": "/cfg/Cogpit/kubectl/1.36.1/kubectl"}, nil
	}
	f.mu.Unlock()

	test.Tap(offer.download)
	q.until(t, "the download's progress", func() bool { return strings.HasPrefix(offer.text.text, "Downloading kubectl 1.36.1") })
	if !offer.download.Disabled() && offerButtonShown(offer, offer.download) {
		t.Error("Download can be clicked again while it runs")
	}
	calls := f.callsOf("kubectl.download")
	if len(calls) != 1 || calls[0].Origin != engine.OriginGUI {
		t.Fatalf("calls %+v", calls)
	}
	var p kubectlDownloadParams
	if err := json.Unmarshal(calls[0].Params, &p); err != nil || p.Tab != "terminal-1" {
		t.Errorf("params %s", calls[0].Params)
	}
	close(release)
	q.until(t, "the download to end", func() bool { return strings.HasPrefix(offer.text.text, "kubectl 1.36.1 is downloaded") })
	if !offerButtonShown(offer, offer.newTerm) || offerButtonShown(offer, offer.download) {
		t.Error("after the download: no New Terminal button, or still Download")
	}
	test.Tap(offer.newTerm)
	q.until(t, "a new terminal", func() bool { return len(f.callsOf("terminal.open")) == 1 })
}

// TestKubectlOfferStates: no line without a message, a note without a button when downloading is
// off, and the line of a release Cogpit put first.
func TestKubectlOfferStates(t *testing.T) {
	skipTerminalUnderRace(t)
	f := connectedFake(t, 3)
	f.quietTerminals = true // their output would race the next test under the test driver (quietReader)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	f.update(func(u *engine.UIState) {
		u.Dock = []engine.DockTab{{ID: "terminal-1", Type: engine.DockTerminal, Title: "Terminal 1"}}
		u.ActiveDock = "terminal-1"
	})
	q.until(t, "the tab", func() bool { c := sh.dock.contents["terminal-1"]; return c != nil && c.kubectl != nil })
	offer := sh.dock.contents["terminal-1"].kubectl
	if offer.root.Visible() {
		t.Error("a line without a check")
	}
	set := func(c *engine.KubectlCheck) {
		f.update(func(u *engine.UIState) { u.Dock[0].Kubectl = c })
		q.drain()
	}
	set(&engine.KubectlCheck{Server: "1.36.1", Supported: true})
	if offer.root.Visible() {
		t.Error("a line for a kubectl that suits the cluster")
	}
	off := offerCheck()
	off.DownloadOff = true
	off.Message = "kubectl 1.36.1 would match this cluster; the kubectl on PATH is 1.30.2, more than one minor version away. Downloading is off in Preferences › Kubernetes."
	set(off)
	if !offer.root.Visible() || offer.text.text != off.Message || offerButtonShown(offer, offer.download) {
		t.Errorf("downloading off: visible %v, %q, Download shown %v", offer.root.Visible(), offer.text.text, offerButtonShown(offer, offer.download))
	}
	first := &engine.KubectlCheck{Server: "1.36.1", FoundVersion: "1.30.2", First: "/cfg/kubectl/1.36.1/kubectl", FirstVersion: "1.36.1",
		Message: "kubectl 1.36.1, downloaded by Cogpit for this cluster's 1.36.1, comes first on this terminal's PATH; the kubectl on PATH is 1.30.2, more than one minor version away."}
	set(first)
	if offer.text.text != first.Message || offerButtonShown(offer, offer.download) || offerButtonShown(offer, offer.newTerm) {
		t.Errorf("a downloaded release first: %q", offer.text.text)
	}
	set(nil)
	if offer.root.Visible() {
		t.Error("the line stays without a check")
	}
}

// offerButtonShown reports whether b is part of the line as rendered.
func offerButtonShown(o *kubectlOffer, b fyne.CanvasObject) bool {
	for _, x := range findAll[fyne.CanvasObject](o.root) {
		if x == b {
			return true
		}
	}
	return false
}

func TestRenderKubectlOfferDark(t *testing.T) {
	_, w, _, _ := kubectlScene(t, ThemeDark, offerCheck())
	savePNG(t, w, "terminal_kubectl_offer_dark.png")
}
