package fyneui

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestWelcomeNoteNamesItsLevel (the owner's RW4 of 2026-10-23, his pick C): the welcome page's
// message about the connection stands in a banner of its own, between the tagline and the clusters:
// a title naming what happened in its level's colour, the level's icon, a plate tinted and outlined
// in that colour, the message under the title; nothing when there is nothing to say.
func TestWelcomeNoteNamesItsLevel(t *testing.T) {
	s := welcomeEngine(t)
	sh, _, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	w := welcomeShown(t, sh, q)
	q.drain()
	n := w.note
	if n.root.Visible() {
		t.Fatalf("a note without a connection: %q", n.title.text)
	}
	gone := "Disconnected from staging [team/staging.yaml]: its kubeconfig source was removed (the file ~/kube/team/staging.yaml is untouched)."
	for _, c := range []struct {
		ctx         string
		conn        engine.Connection
		title, text string
		color       string
	}{
		{"", engine.Connection{State: engine.Disconnected, Error: gone}, "Connection closed", gone, string(colorStatusWarning)},
		{"", engine.Connection{State: engine.ConnectError, Error: "dial tcp 10.0.0.5:6443: connect: connection refused"},
			"Could not connect", "dial tcp 10.0.0.5:6443: connect: connection refused", string(colorStatusError)},
		{"kind-dev", engine.Connection{State: engine.Connected}, "Connected to kind-dev", "Pick a view in the navigator, or a tab above.",
			string(colorAccent)},
	} {
		sh.ui.Context, sh.ui.Connection = c.ctx, c.conn
		w.renderStatus()
		if !n.root.Visible() || n.title.text != c.title || n.text.text != c.text {
			t.Errorf("%s: shown %v, title %q, text %q", c.conn.State, n.root.Visible(), n.title.text, n.text.text)
		}
		if string(n.title.color) != c.color || string(n.plate.color) != c.color || n.icon.Resource == nil {
			t.Errorf("%s: title in %q, plate in %q, icon %v; want %s", c.conn.State, n.title.color, n.plate.color, n.icon.Resource, c.color)
		}
	}
	sh.ui.Context, sh.ui.Connection = "", engine.Connection{State: engine.Disconnected}
	w.renderStatus()
	if n.root.Visible() {
		t.Error("the note stays after the connection has nothing to say")
	}
}
