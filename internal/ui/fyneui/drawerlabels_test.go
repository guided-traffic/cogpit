package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
)

// A name wider than the details' name column wraps within it instead of running into its value
// (the Stateful Set drawer's "Pod Management Policy" and "Volume Claim Templates"); a name that
// fits stays one line.
// PNGs: statefulset_pods_drawer_*.png.
func TestDrawerLongNamesWrapInTheirColumn(t *testing.T) {
	f, _ := workloadPodsFake(t, true, nil)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerHeaders(sh)) > 0 })
	settle(w)
	nameW := sh.app.Settings().Theme().Size(sizeDrawerLabel)
	wrapped := map[string]bool{}
	pairs := 0
	for _, c := range findAll[*fyne.Container](sh.drawer.details) {
		if _, ok := c.Layout.(kvLayout); !ok {
			continue
		}
		for i := 0; i+1 < len(c.Objects); i += 2 {
			name, value := c.Objects[i], c.Objects[i+1]
			pairs++
			text := ""
			switch x := name.(type) {
			case *paragraph:
				text = x.text
				wrapped[text] = true
			case *selLine:
				text = x.line.text
			}
			if right := name.Position().X + name.MinSize().Width; name.MinSize().Width > nameW || right > value.Position().X {
				t.Errorf("name %q is %.0f wide, ends at %.0f; its column is %.0f, its value starts at %.0f", text, name.MinSize().Width, right, nameW, value.Position().X)
			}
		}
	}
	if pairs == 0 {
		t.Fatal("no name and value pairs in the drawer")
	}
	for _, long := range []string{"Pod Management Policy", "Volume Claim Templates"} {
		if !wrapped[long] {
			t.Errorf("%q is not wrapped in its column (wrapped: %v)", long, wrapped)
		}
	}
	if wrapped["Replicas"] || wrapped["Current Revision"] {
		t.Errorf("a name that fits is wrapped: %v", wrapped)
	}
}
