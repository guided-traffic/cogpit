package fyneui

import (
	"testing"
)

// The list renders show the Pods and Nodes views as the real kind profiles fill them: the warning
// column after Name, links in the Namespace, Controlled By and Node cells, and a cordoned node's
// Conditions ("SchedulingDisabled" plain, "Ready" green). PNGs: testdata/{pods,nodes}_list_*.png.

func renderList(t *testing.T, view string, mode ThemeMode, name string) {
	f := kindsFake(t, view)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the "+view+" view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == view && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderPodsListDark(t *testing.T)  { renderList(t, "pods", ThemeDark, "pods_list_dark.png") }
func TestRenderPodsListLight(t *testing.T) { renderList(t, "pods", ThemeLight, "pods_list_light.png") }
func TestRenderNodesListDark(t *testing.T) { renderList(t, "nodes", ThemeDark, "nodes_list_dark.png") }
