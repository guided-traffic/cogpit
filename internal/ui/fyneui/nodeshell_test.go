package fyneui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
)

// The node shell in the window (G7, G8; D-37): its confirmation, which names the privileged pod,
// the namespace, the node and the image; its dock tab, a terminal on Session.OpenTerminal; and
// Cluster Settings › Node Shell. The row menus and the drawer's toolbar: TestRowMenuNodes,
// TestDrawerToolbarFromRegistry, TestRegistryMenusAsLens. PNGs:
// testdata/node_shell_confirm_{dark,light}.png, node_shell_{dark,light}.png and
// cluster_settings_node_shell_{dark,light}.png.

const nodeShellPod = "cogpit-node-shell-k3v9q2mx"

// nodeShellOutput is what the engine's node shell session shows: its notice, then the node's
// shell (the shell's lines are the node's, as on a kind node).
var nodeShellOutput = "[cogpit] Node shell on " + busyNode + ": privileged pod kube-system/" + nodeShellPod + ", image " +
	engine.DefaultNodeShellImage + "; the pod ends by itself after " + engine.NodeShellDeadlineText(engine.DefaultNodeShellDeadline) + ".\r\n" +
	"[cogpit] Waiting for the pod to run…\r\n" +
	"root@" + busyNode + ":/# hostname\r\n" + busyNode + "\r\nroot@" + busyNode + ":/# crictl ps --name kube-proxy -q\r\n" +
	"5f2c0e8a9d14\r\nroot@" + busyNode + ":/# "

// nodeShellEngine is the fake engine whose node shell tabs stream nodeShellOutput.
type nodeShellEngine struct {
	*fakeEngine
	last *atomic.Pointer[openStream]
}

func (n nodeShellEngine) OpenTerminal(ctx context.Context, tabID string) (terminal.Session, error) {
	if !strings.HasPrefix(tabID, string(engine.DockNodeShell)+"-") {
		return n.fakeEngine.OpenTerminal(ctx, tabID)
	}
	n.mu.Lock()
	n.opened[tabID]++
	n.mu.Unlock()
	s := newGatedStream(n.fakeEngine, tabID, nodeShellOutput)
	n.last.Store(s)
	return s, nil
}

// nodeShellScene is the Nodes view with a node shell tab open in the dock, its output shown.
func nodeShellScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window) {
	t.Helper()
	f := kindsFake(t, "nodes")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: busyNode}
	f.ui.Dock = []engine.DockTab{{ID: "nodeShell-1", Type: engine.DockNodeShell, Title: "Node shell: " + busyNode, Context: "demo",
		Ref: &ref, Container: engine.NodeShellContainer, NodeShell: &engine.NodeShellTab{Node: busyNode, Namespace: engine.NodeShellNamespace,
			Pod: nodeShellPod, Image: engine.DefaultNodeShellImage, Pinned: true, DeadlineSeconds: 7200}}}
	f.ui.ActiveDock = "nodeShell-1"
	eng := nodeShellEngine{fakeEngine: f, last: &atomic.Pointer[openStream]{}}
	sh, w, q := startShell(t, eng, f, Options{Theme: mode})
	// As attachScene: the output flows once the shell has settled and the widget waits in Read.
	q.until(t, "the terminal to read", func() bool {
		s := eng.last.Load()
		return len(sh.kinds) > 0 && sh.view.snap != nil && s != nil && s.reading.Load()
	})
	q.drain()
	s := eng.last.Load()
	close(s.gate)
	deadline := time.Now().Add(5 * time.Second)
	for !s.idle.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the terminal did not take the node shell's output")
		}
		time.Sleep(time.Millisecond)
	}
	term := sh.dock.contents["nodeShell-1"].term
	t.Cleanup(func() {
		_ = term.Close()
		select {
		case <-term.Done():
		case <-time.After(10 * time.Second):
			t.Error("the node shell's widget did not end")
		}
	})
	return sh, w
}

// TestNodeShellTabShowsATerminal: a node shell tab is a terminal on Session.OpenTerminal, titled
// "Node shell: <node>".
func TestNodeShellTabShowsATerminal(t *testing.T) {
	sh, _ := nodeShellScene(t, ThemeDark)
	term, ok := sh.dock.contents["nodeShell-1"].term, sh.dock.contents["nodeShell-1"].term != nil
	if !ok {
		t.Fatalf("the node shell tab shows %T", sh.dock.contents["nodeShell-1"].obj.Objects[0])
	}
	if text := term.Text(); !strings.Contains(text, "privileged pod kube-system/"+nodeShellPod) || !strings.Contains(text, "root@"+busyNode) {
		t.Errorf("terminal %q", text)
	}
	if b := sh.dock.tabRow.Objects[0].(*tabButton); b.title != "Node shell: "+busyNode {
		t.Errorf("tab %q", b.title)
	}
}

func TestRenderNodeShellDark(t *testing.T) {
	_, w := nodeShellScene(t, ThemeDark)
	savePNG(t, w, "node_shell_dark.png")
}

func TestRenderNodeShellLight(t *testing.T) {
	_, w := nodeShellScene(t, ThemeLight)
	savePNG(t, w, "node_shell_light.png")
}

// nodeShellRequest is the confirmation the engine asks for a node shell on busyNode (as Dispatch
// builds it from node.shell: its label, the node, the context, the effects and their text).
func nodeShellRequest(origin engine.Origin) engine.ConfirmRequest {
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: busyNode}
	cfg := engine.NodeShellConfig{Image: engine.DefaultNodeShellImage, Deadline: engine.DefaultNodeShellDeadline}
	return engine.ConfirmRequest{Command: engine.NodeShellCommand, Label: "Shell", Danger: engine.DangerDestructive, Origin: origin,
		Context: "demo", Objects: []model.ObjectRef{ref}, Kinds: []string{"Node"}, Undoable: true, // as the engine sets them (A101)
		Effects: engine.NodeShellEffects(busyNode, cfg), Detail: engine.NodeShellDetail(busyNode, cfg)}
}

// confirmNodeShell shows the node shell's confirmation and returns the dialog's texts; answer is
// tapped afterwards.
func confirmNodeShell(t *testing.T, mode ThemeMode, origin engine.Origin, png string) []string {
	t.Helper()
	f := kindsFake(t, "nodes")
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the nodes", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	done := make(chan bool, 1)
	go func() {
		ok, _ := sh.confirmer.Confirm(context.Background(), nodeShellRequest(origin))
		done <- ok
	}()
	q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
	q.drain()
	texts := overlayTexts(w)
	if png != "" {
		savePNG(t, w, png)
	}
	test.Tap(sh.confirmer.current.cancel)
	if <-done {
		t.Error("Cancel confirmed the node shell")
	}
	return texts
}

// TestNodeShellConfirmationNamesWhatItCreates: the dialog says it creates a privileged pod with
// root access, names the namespace, the node, the image, the maximum duration and the context, and
// who asked.
func TestNodeShellConfirmationNamesWhatItCreates(t *testing.T) {
	texts := strings.Join(confirmNodeShell(t, ThemeDark, engine.OriginExternalAgent, ""), "\n")
	at := strings.LastIndex(engine.DefaultNodeShellImage, "@") // Renovate moves the default: the test follows it
	for _, want := range []string{"asked by · agent", "Shell?", "Creates", "privileged pod in namespace kube-system on node " + busyNode,
		"Access", "root on the node", "Image\n" + engine.DefaultNodeShellImage[:at], "Digest\n" + engine.DefaultNodeShellImage[at+1:at+20],
		"by itself after 2 hours", "demo", "An external agent over MCP"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the confirmation lacks %q:\n%s", want, texts)
		}
	}
	if strings.Contains(texts, "This cannot be undone.") { // nothing is lost (A101)
		t.Errorf("the node shell's confirmation says it cannot be undone:\n%s", texts)
	}
}

func TestRenderNodeShellConfirmDark(t *testing.T) {
	confirmNodeShell(t, ThemeDark, engine.OriginGUI, "node_shell_confirm_dark.png")
}

func TestRenderNodeShellConfirmLight(t *testing.T) {
	confirmNodeShell(t, ThemeLight, engine.OriginGUI, "node_shell_confirm_light.png")
}

// nodeShellField is a shown Node Shell section's field.
func nodeShellField(sh *shell, setting string) *widget.Entry {
	if c, ok := sh.settings.controls[setting].(*fyne.Container); ok {
		return entryOf(c.Objects[0])
	}
	return entryOf(sh.settings.controls[setting])
}

// TestClusterSettingsNodeShellSection: the image field shows Cogpit's default by its short name as
// its placeholder (R30) and says it is pinned; an image typed is kept, and a tag says it floats; a
// pull secret typed is kept; what cannot be an image is refused under the field; the default's short
// name typed sets the default, pinned.
func TestClusterSettingsNodeShellSection(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": engine.SectionNodeShell})
	q.until(t, "the Node Shell section", func() bool {
		return sh.settings.visible() && nodeShellField(sh, nodeShellImageSetting) != nil && sh.settings.view != nil
	})
	image, note := nodeShellField(sh, nodeShellImageSetting), sh.settings.controls["nodeShellNote"].(*textLine)
	if image.PlaceHolder != engine.DefaultNodeShellImageName || note.text != nodeShellNotePinned {
		t.Errorf("placeholder %q, note %q", image.PlaceHolder, note.text)
	}

	w.Canvas().Focus(image)
	test.Type(image, "registry.example.com/tools/shell:1.2")
	q.until(t, "the image kept", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.NodeShellImage == "registry.example.com/tools/shell:1.2"
	})
	w.Canvas().Unfocus()
	q.until(t, "the floating note", func() bool { return note.text == nodeShellNoteFloating })

	secret := nodeShellField(sh, nodeShellPullSecretSetting)
	w.Canvas().Focus(secret)
	test.Type(secret, "registry-pull")
	q.until(t, "the pull secret kept", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.NodeShellPullSecret == "registry-pull"
	})

	w.Canvas().Focus(image)
	image.SetText("alpine 3.24")
	q.until(t, "the refusal", func() bool { return sh.settings.errs[nodeShellImageSetting].Visible() })
	if text := sh.settings.errs[nodeShellImageSetting].text; !strings.Contains(text, "without spaces") {
		t.Errorf("refusal %q", text)
	}

	image.SetText(engine.DefaultNodeShellImageName)
	q.until(t, "the default", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.NodeShellImage == "" && !sh.settings.errs[nodeShellImageSetting].Visible()
	})
	w.Canvas().Unfocus()
	q.until(t, "the pinned note", func() bool { return note.text == nodeShellNotePinned })
}

// TestClusterSettingsNodeShellMaxDuration: Maximum duration (owner, 2026-10-14, D-37) shows the
// default 2h as its placeholder and says it in words under the field; 90m typed is kept as 90
// minutes and said as 1 hour 30 minutes; 3m is refused under the field with the bounds; emptied, it
// is the default again; an agent's reset shows.
func TestClusterSettingsNodeShellMaxDuration(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": engine.SectionNodeShell})
	q.until(t, "the Node Shell section", func() bool {
		return sh.settings.visible() && nodeShellField(sh, nodeShellMaxDurationSetting) != nil && sh.settings.view != nil
	})
	field, note := nodeShellField(sh, nodeShellMaxDurationSetting), sh.settings.controls["nodeShellMaxDurationNote"].(*textLine)
	if field.PlaceHolder != "2h" || note.text != "Every node shell's pod ends by itself after 2 hours." {
		t.Errorf("placeholder %q, note %q", field.PlaceHolder, note.text)
	}

	w.Canvas().Focus(field)
	test.Type(field, "90m")
	q.until(t, "90 minutes kept", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.NodeShellMaxMinutes == 90
	})
	w.Canvas().Unfocus()
	q.until(t, "the note in words, the value as kept", func() bool {
		return note.text == "Every node shell's pod ends by itself after 1 hour 30 minutes." && field.Text == "1h30m"
	})

	w.Canvas().Focus(field)
	field.SetText("3m")
	q.until(t, "the refusal", func() bool { return sh.settings.errs[nodeShellMaxDurationSetting].Visible() })
	if text := sh.settings.errs[nodeShellMaxDurationSetting].text; !strings.Contains(text, "from 5m to 24h") {
		t.Errorf("refusal %q", text)
	}
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.NodeShellMaxMinutes != 90 {
		t.Errorf("a refused value changed the setting to %d minutes", cs.NodeShellMaxMinutes)
	}
	field.SetText("")
	q.until(t, "the default again", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.NodeShellMaxMinutes == 0 && !sh.settings.errs[nodeShellMaxDurationSetting].Visible()
	})
	w.Canvas().Unfocus()
	q.until(t, "the default's note", func() bool { return note.text == "Every node shell's pod ends by itself after 2 hours." })

	mustDispatch(t, s, "cluster.settings.setNodeShell", map[string]any{"context": "kind-dev", "maxDuration": "4h"})
	q.until(t, "the window's change shown", func() bool {
		return field.Text == "4h" && note.text == "Every node shell's pod ends by itself after 4 hours."
	})
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "cluster.settings.setNodeShell", Origin: engine.OriginExternalAgent,
		Params: mustJSON(t, map[string]any{"context": "kind-dev", "maxDuration": ""})}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "an agent's reset shown", func() bool {
		return field.Text == "" && note.text == "Every node shell's pod ends by itself after 2 hours."
	})
}

func TestRenderClusterSettingsNodeShellDark(t *testing.T) {
	renderClusterSettingsNodeShell(t, ThemeDark, "cluster_settings_node_shell_dark.png")
}

func TestRenderClusterSettingsNodeShellLight(t *testing.T) {
	renderClusterSettingsNodeShell(t, ThemeLight, "cluster_settings_node_shell_light.png")
}

func renderClusterSettingsNodeShell(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster", "iconColor": "violet"})
	mustDispatch(t, s, "cluster.settings.setNodeShell", map[string]any{"context": "kind-dev", "imagePullSecret": "registry-pull", "maxDuration": "4h"})
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": engine.SectionNodeShell})
	q.until(t, "the Node Shell section", func() bool {
		e, d := nodeShellField(sh, nodeShellPullSecretSetting), nodeShellField(sh, nodeShellMaxDurationSetting)
		return sh.settings.visible() && sh.settings.view != nil && e != nil && e.Text == "registry-pull" && d != nil && d.Text == "4h"
	})
	q.drain()
	savePNG(t, w, name)
}
