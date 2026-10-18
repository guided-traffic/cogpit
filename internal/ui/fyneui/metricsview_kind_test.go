//go:build kind

package fyneui

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestKindPrometheusDrawer is the window against a real Prometheus (make kind-prometheus) and the
// kind cluster's metrics-server: the real engine connected to the kind cluster, a running pod's
// drawer with the source line naming both, the status line (Prometheus serves the history, the
// Metrics Server the live values), the Network and Filesystem buttons enabled and each variant
// drawn once the engine asked Prometheus for it, the ranges beyond an hour offered. It renders
// each variant for a look, to COGPIT_PNG_DIR or a temporary directory: these are pictures of the
// kind cluster, never committed. It only reads. Skipped when the cluster has no Prometheus.
func TestKindPrometheusDrawer(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
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
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			q.drain()
			time.Sleep(10 * time.Millisecond)
		}
		q.drain()
	}
	var pod model.ObjectRef
	until("a running web pod", func() bool {
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := s.Object(ctx, model.ObjectRef{Kind: "services", Namespace: "monitoring", Name: "prometheus-server"}); errors.Is(err, engine.ErrNotFound) {
		t.Skip("the kind cluster has no Prometheus: run make kind-prometheus")
	} else if err != nil {
		t.Fatal(err)
	}
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &pod; u.Selected = []model.ObjectRef{pod} })
	var p *metricsPanel
	until("Prometheus's history in the drawer", func() bool {
		var ok bool
		p, ok = sh.drawer.kept[metricsKey].(*metricsPanel)
		if !ok || p.data == nil || p.data.Status != metrics.StatusAvailable || len(p.data.Targets) < 2 {
			return false
		}
		return len(p.data.Targets[0].Samples) > 0 && strings.Contains(p.data.Source, " and ")
	})
	settle(w)
	// Both providers on the source row (D-88): Prometheus's history at the left, the Metrics
	// Server's live values at the right, each answering; where Prometheus runs is in its tooltip.
	texts := drawerTexts(sh)
	if st := chipTexts(p.status); len(st) != 2 || !strings.HasPrefix(st[0], "Prometheus ● ") || !strings.HasPrefix(st[1], "Metrics Server ● ") ||
		strings.Contains(st[0], "·") || strings.Contains(st[1], "·") {
		t.Errorf("source row = %q", st)
	} else {
		t.Logf("source row: %q", st)
	}
	if tip := p.status.chips[0].tip; !strings.HasPrefix(tip, "Prometheus monitoring/prometheus-server:http") || !strings.Contains(tip, "Serves: history") {
		t.Errorf("Prometheus's tooltip %q", tip)
	}
	for _, s := range texts {
		if strings.Contains(s, "keeps no history") || strings.Contains(s, "has no history") {
			t.Errorf("the drawer says %q with Prometheus", s)
		}
	}
	for _, id := range []string{variantNetwork, variantFilesystem} {
		if p.variants[id].Disabled() {
			t.Errorf("%s is disabled with Prometheus: %q", id, p.variants[id].tip)
		}
	}
	if !slices.Contains(p.rangeSel.Options, "7d") {
		t.Errorf("ranges = %v", p.rangeSel.Options)
	}

	dir := os.Getenv("COGPIT_PNG_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	shot := func(name string) {
		t.Helper()
		settle(w)
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, w.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		t.Log("rendered", f.Name())
	}
	shot("kind_prom_status.png")
	for _, v := range []string{variantCPU, variantMemory, variantNetwork, variantFilesystem} {
		p.variants[v].OnTapped()
		// The network and the filesystem are asked of Prometheus only now that the chart shows them.
		until(v+" from the engine", func() bool {
			if p.variantPending || len(p.data.Targets) == 0 || p.data.Targets[0].Variant != v {
				return false
			}
			t0 := p.data.Targets[0]
			switch v {
			case variantNetwork:
				return len(t0.Network) > 0
			case variantFilesystem:
				return len(t0.Filesystem) > 0
			}
			return true
		})
		settle(w)
		d := p.plot.data
		points := 0
		for _, s := range d.Series {
			points += len(s.Points)
		}
		if points == 0 {
			t.Errorf("%s: no points", v)
		}
		names := make([]string, len(d.Series))
		for i, s := range d.Series {
			names[i] = s.Name
			if n := len(s.Points); n > 0 {
				t.Logf("%s %s: %d points, last %s", v, s.Name, n, d.Detail(s.Points[n-1].Value))
			}
		}
		t.Logf("%s: series %v, lines %d", v, names, len(d.Lines))
		shot("kind_prom_" + v + ".png")
	}
	p.variants[variantNetwork].OnTapped()
	until("the network again", func() bool {
		return !p.variantPending && len(p.data.Targets) > 0 && len(p.data.Targets[0].Network) > 0
	})
	settle(w)
	g := p.plot.geom
	p.plot.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(g.left+g.w*0.97, g.top+g.h*0.5)}})
	if h := p.plot.Hover(); h == nil || !strings.Contains(h.Text(), "Receive: ") || !strings.Contains(h.Text(), "/s") {
		t.Errorf("hover over the network chart: %v", h)
	} else {
		t.Logf("hover:\n%s", h.Text())
	}
	shot("kind_prom_network_hover.png")
}
