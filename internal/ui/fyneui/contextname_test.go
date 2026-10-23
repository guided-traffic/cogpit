package fyneui

import (
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A source context's id carries its file ("<context> [<file>]", D-28); the bars show the name and
// the file apart, so the navigator's header shows the name, and the file muted, and the status bar
// the name with the full id (the file included) as its tooltip.
func TestContextNameAndFileShownApart(t *testing.T) {
	for id, want := range map[string][2]string{
		"acme-d-pinniped [.kube/kubeconfig-acme-d.yaml]": {"acme-d-pinniped", ".kube/kubeconfig-acme-d.yaml"},
		"demo":                   {"demo", ""},
		"prod [a.yaml]":          {"prod", "a.yaml"},
		"odd [name] [b/c.yaml]":  {"odd [name]", "b/c.yaml"},
		"no-file-but-bracket [x": {"no-file-but-bracket [x", ""},
	} {
		if n, f := splitContextID(id); n != want[0] || f != want[1] {
			t.Errorf("splitContextID(%q) = %q, %q", id, n, f)
		}
	}

	f := connectedFake(t, 10)
	f.ui.Context = "acme-d-pinniped [.kube/kubeconfig-acme-d.yaml]"
	f.ui.Connection = engine.Connection{State: engine.Connected} // not the demo cluster, whose subtitle says so
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the header", func() bool { return sh.side.name.text == "acme-d-pinniped" })
	if sh.side.badge == nil || sh.side.subtitle.text != ".kube/kubeconfig-acme-d.yaml" || sh.side.subtitle.color != colorMuted {
		t.Errorf("navigator header %q / %q (%s)", sh.side.name.text, sh.side.subtitle.text, sh.side.subtitle.color)
	}
	if sh.status.context.text != "acme-d-pinniped" || sh.status.context.tip != f.ui.Context {
		t.Errorf("status bar %q (tip %q)", sh.status.context.text, sh.status.context.tip)
	}
}

// The top bar names no cluster (the owner, 2026-10-21, D-54): neither its name, its kubeconfig file
// nor "Not connected", and it carries no connection dot; the status bar and the navigator name it,
// and the status bar's dot is where the shown cluster's connection log opens.
func TestTopBarNamesNoCluster(t *testing.T) {
	const id = "acme-d-pinniped [.kube/kubeconfig-acme-d.yaml]"
	f := connectedFake(t, 10)
	f.ui.Context = id
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the header", func() bool { return sh.side.name.text == "acme-d-pinniped" })
	settle(w)
	if got := lineTexts(sh.top.root); !slices.Contains(got, "Cogpit") || !slices.Contains(lineTexts(sh.status.root), "acme-d-pinniped") {
		t.Fatalf("the probe is blind: the top bar's texts %q, the status bar's %q", got, lineTexts(sh.status.root))
	}
	for _, text := range lineTexts(sh.top.root) {
		for _, named := range []string{"acme-d-pinniped", "kubeconfig-acme-d", notConnected} {
			if strings.Contains(text, named) {
				t.Errorf("the top bar says %q", text)
			}
		}
	}
	if dots := findAll[*connDot](sh.top.root); len(dots) != 0 {
		t.Errorf("the top bar has %d connection dots", len(dots))
	}
	if sh.status.context.text != "acme-d-pinniped" || sh.status.dot.context != id || sh.side.name.text != "acme-d-pinniped" {
		t.Errorf("status bar %q (dot of %q), navigator %q", sh.status.context.text, sh.status.dot.context, sh.side.name.text)
	}

	// Not connected: the top bar still says nothing of it; the status bar and the navigator do.
	f.update(func(u *engine.UIState) { u.Context, u.Connection = "", engine.Connection{State: engine.Disconnected} })
	q.until(t, "no cluster", func() bool { return sh.status.context.text == notConnected })
	for _, text := range lineTexts(sh.top.root) {
		if strings.Contains(text, notConnected) {
			t.Errorf("the top bar says %q without a cluster", text)
		}
	}
	if sh.side.name.text != notConnected {
		t.Errorf("the navigator says %q without a cluster", sh.side.name.text)
	}
}
