package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The drawer's header after a move to another pod whose detail came before the next frame (the
// owner's finding of 2026-10-22, drawer.layoutHeader): the icons were drawn from the close button on
// past the drawer's edge and the status under the tabs, because no frame saw the header's minimum
// size change.

// twoHealthyPods are two running fixture pods with two containers each: the same actions, so that
// their headers are as wide and as tall.
func twoHealthyPods(t *testing.T) (a, b model.ObjectRef) {
	t.Helper()
	var refs []model.ObjectRef
	for _, p := range fixturePodSpecs() {
		if p.state == podOK && len(p.containers) == 2 {
			refs = append(refs, model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name})
		}
		if len(refs) == 2 {
			return refs[0], refs[1]
		}
	}
	t.Fatal("fewer than two healthy fixture pods with two containers")
	return a, b
}

// TestDrawerHeaderAfterAnotherPod: the drawer moved to another pod, its loading state and its
// detail handled before the next frame as a fast cache does, shows its icons left of the close
// button and inside the drawer and its status above the tabs, after that frame (glPass), which sees
// no minimum size change and lays nothing out again.
func TestDrawerHeaderAfterAnotherPod(t *testing.T) {
	f := kindsFake(t, "pods")
	registerProbes(t, f)
	withCopy(t, f)
	first, second := twoHealthyPods(t)
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{first}, &first
	sh, w, q := newTestShell(t, f, Options{})
	shown := func(ref model.ObjectRef) func() bool {
		return func() bool {
			d := sh.drawer
			return d.last != nil && d.last.Ref == ref && len(d.actions.Objects) > 0 && d.status.Visible()
		}
	}
	q.until(t, "the first pod's drawer", shown(first))
	settle(w)
	p := &glPass{}
	p.run(w)
	checkDrawerHeader(t, sh, "the first pod")
	icons := len(sh.drawer.actions.Objects)

	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{second}, &second })
	q.until(t, "the second pod's drawer", shown(second))
	p.run(w)
	if n := len(sh.drawer.actions.Objects); n != icons {
		t.Fatalf("the first pod has %d icons, the second %d: the test needs headers as wide", icons, n)
	}
	checkDrawerHeader(t, sh, "the second pod")
	lookPNG(t, w, "drawer_header_another_pod.png") // as the frame left it, not settled
}

// checkDrawerHeader fails where the header's icons reach past the close button or the close button
// past the drawer's edge, or the status reaches into the tabs below the header.
func checkDrawerHeader(t *testing.T, sh *shell, what string) {
	t.Helper()
	d := sh.drawer
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject
	var closeB *tipButton
	for _, b := range findAll[*tipButton](d.head) {
		if b.tip == "Close" {
			closeB = b
		}
	}
	if closeB == nil {
		t.Fatalf("%s: no close button in the header", what)
	}
	closeX := at(closeB).X
	for i, o := range d.actions.Objects {
		if end := at(o).X + o.Size().Width; end > closeX {
			t.Errorf("%s: icon %d ends at %v, past the close button at %v", what, i, end, closeX)
		}
	}
	if end, edge := closeX+closeB.Size().Width, at(d.root).X+d.root.Size().Width; end > edge {
		t.Errorf("%s: the close button ends at %v, past the drawer's edge at %v", what, end, edge)
	}
	if end, tabs := at(d.status).Y+d.status.Size().Height, at(d.body).Y; end > tabs {
		t.Errorf("%s: the status ends at %v, below the tabs' top at %v", what, end, tabs)
	}
}
