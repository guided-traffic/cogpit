package fyneui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A volume's usage in a claim's drawer (D-139): the line above the chart, the chart of the bytes in
// use under the filesystem's size, no variant buttons, the usage bar. The numbers are canned, as
// the engine reports them from the claim's range queries. The PNGs, dark and light, are the pull
// request's (AGENTS.md rule 7): to testdata with -update.

const fixGiB = float64(1 << 30)

var pgClaim = model.VolumeClaim{Namespace: "bezon-prod", Claim: "pg-bezon-timescale-1", Volume: "pvc-f0327b26-9407-4238-ad1b-45a2d7a09dcd"}

// volumeMetricsFixture is a claim's volume over the last hour: filling up to 61 % of a 196.7 GiB
// filesystem, resized from 150 GiB twenty minutes ago.
func volumeMetricsFixture(ref model.ObjectRef) *engine.ObjectMetrics {
	n := 120
	var fs []model.FilesystemSample
	for i := range n {
		size := 196.7 * fixGiB
		if i < 80 {
			size = 150 * fixGiB
		}
		used := (108 + 12*float64(i+1)/float64(n)) * fixGiB
		fs = append(fs, model.FilesystemSample{At: fixedNow.Add(-time.Hour + time.Duration(i+1)*30*time.Second), UsageBytes: used, CapacityBytes: size, AvailableBytes: size*0.95 - used})
	}
	last := fs[len(fs)-1]
	vc := pgClaim
	vc.Pods = []string{"pg-bezon-timescale-1-0"}
	providers := bothAnswering()
	providers[1].Unused = true // a claim's chart never asks the Metrics Server
	return &engine.ObjectMetrics{
		Ref: ref, Status: metrics.StatusAvailable, Source: promSource, Providers: providers,
		Capabilities: metrics.Capabilities{History: true, Network: true, Filesystem: true},
		Variants:     []string{engine.VariantFilesystem}, FilesystemSource: metrics.FilesystemUsage,
		Targets: []engine.MetricsTarget{{Variant: engine.VariantFilesystem, Filesystem: fs, Window: time.Hour, Range: "1h"}},
		Volume:  &vc, Fill: &last,
	}
}

// volumeDrawer opens the claim's drawer with the canned numbers, changed by mutate.
func volumeDrawer(t *testing.T, mode ThemeMode, mutate func(*engine.ObjectMetrics)) (*shell, *metricsPanel, func(string)) {
	sh, p, png, _, _ := volumeDrawerOf(t, mode, mutate)
	return sh, p, png
}

// volumeDrawerOf is volumeDrawer with the fake engine and the main queue.
func volumeDrawerOf(t *testing.T, mode ThemeMode, mutate func(*engine.ObjectMetrics)) (*shell, *metricsPanel, func(string), *fakeEngine, *mainQueue) {
	t.Helper()
	f := kindsFake(t, "pods")
	f.kinds = append(f.kinds, kindInfo{Kind: "persistentvolumeclaims", Title: "Persistent Volume Claims", APIKind: "PersistentVolumeClaim", Version: "v1", Namespaced: true},
		kindInfo{Kind: "persistentvolumes", Title: "Persistent Volumes", APIKind: "PersistentVolume", Version: "v1"})
	ref := model.ObjectRef{Context: "demo", Kind: "persistentvolumeclaims", Namespace: pgClaim.Namespace, Name: pgClaim.Claim}
	obj := testkit.Object("v1", "PersistentVolumeClaim", ref.Namespace, ref.Name, map[string]any{
		"spec": map[string]any{
			"volumeName": pgClaim.Volume, "storageClassName": "default", "accessModes": []any{"ReadWriteOnce"}, "volumeMode": "Filesystem",
			"resources": map[string]any{"requests": map[string]any{"storage": "200Gi"}},
		},
		"status": map[string]any{"phase": "Bound", "capacity": map[string]any{"storage": "200Gi"}},
	})
	p, _ := kinds.ByKey("persistentvolumeclaims")
	dm, err := engine.BuildDetailWith(p, ref, obj, nil, fixedNow, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !dm.Metrics || !dm.VolumeMetrics {
		t.Fatalf("the claim's model: metrics %v, volume %v", dm.Metrics, dm.VolumeMetrics)
	}
	f.details[ref.String()] = dm
	m := volumeMetricsFixture(ref)
	if mutate != nil {
		mutate(m)
	}
	f.metrics = map[string]*engine.ObjectMetrics{ref.String(): m}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the metrics", func() bool {
		pm, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		return ok && pm.data != nil && sh.drawer.last != nil
	})
	settle(w)
	return sh, sh.drawer.kept[metricsKey].(*metricsPanel), func(name string) { savePNG(t, w, name) }, f, q
}

func TestClaimDrawerShowsItsVolumesUsage(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		sh, p, png := volumeDrawer(t, mode, nil)
		texts := drawerTexts(sh)
		if texts[0] != "Metrics" || !p.intro.Visible() || p.intro.text != "Display metrics from PV: "+pgClaim.Volume || p.intro.links()[pgClaim.Volume] == nil {
			t.Fatalf("the section begins %q, intro %q (shown %v)", texts[:min(3, len(texts))], p.intro.text, p.intro.Visible())
		}
		if !p.pods.Visible() || p.pods.text != "Mounted by pod: pg-bezon-timescale-1-0" || p.pods.links()["pg-bezon-timescale-1-0"] == nil {
			t.Errorf("pods line %q (shown %v)", p.pods.text, p.pods.Visible())
		}
		if got := chipTexts(p.status); len(got) != 2 || got[1] != "Metrics Server ● not used" {
			t.Errorf("source row %q", got)
		}
		for id, b := range p.variants {
			if b.Visible() {
				t.Errorf("the %s button shows on a volume's chart", id)
			}
		}
		if !p.controls.Visible() || !p.rangeSel.Visible() || p.variant != variantFilesystem {
			t.Errorf("controls %v, range %v, variant %q", p.controls.Visible(), p.rangeSel.Visible(), p.variant)
		}
		d := p.plot.data
		if len(d.Series) != 2 || d.Series[0].Name != "Usage" || d.Series[0].Outline || d.Series[1].Name != "Capacity" || !d.Series[1].Outline || !d.Binary {
			t.Fatalf("chart = %+v", d.Series)
		}
		if got := d.Series[1].Points; got[0].Value != 150*fixGiB || got[len(got)-1].Value != 196.7*fixGiB {
			t.Errorf("capacity from %v to %v, want the resize from 150 GiB to 196.7 GiB", got[0].Value, got[len(got)-1].Value)
		}
		if !p.disk.Visible() || p.disk.title != volumeTitle || len(p.disk.fs) != 1 || model.FormatPercent(p.disk.fs[0].Share()) != "61%" {
			t.Fatalf("usage bar: shown %v, %q, %+v", p.disk.Visible(), p.disk.title, p.disk.fs)
		}
		if tip := p.disk.tip(p.disk.fs[0]); !strings.HasPrefix(tip, pgClaim.Volume+"\nUsed 120.0 GiB of 196.7 GiB (61%)\nAvailable ") {
			t.Errorf("tooltip = %q", tip)
		}
		png("volume_claim_drawer_" + string(mode) + ".png")
	}
}

func TestVolumeWithoutUsageSaysWhy(t *testing.T) {
	_, p, _ := volumeDrawer(t, ThemeDark, func(m *engine.ObjectMetrics) {
		m.Fill, m.VolumeNote = nil, "the kubelet reports no usage of this volume now: no pod mounts it, or its driver reports none"
		m.Targets[0].Filesystem = nil
	})
	if !p.disk.Visible() || len(p.disk.fs) != 0 || p.disk.note == "" || p.disk.title != volumeTitle {
		t.Errorf("usage bar: shown %v, rows %d, note %q", p.disk.Visible(), len(p.disk.fs), p.disk.note)
	}
}

func TestUnboundClaimSaysSo(t *testing.T) {
	sh, p, _ := volumeDrawer(t, ThemeDark, func(m *engine.ObjectMetrics) {
		*m = engine.ObjectMetrics{Ref: m.Ref, Status: metrics.StatusNotServed, Message: "the claim is not bound to a volume", Variants: []string{engine.VariantFilesystem}}
	})
	if texts := drawerTexts(sh); !slices.Contains(texts, "No metrics: the claim is not bound to a volume") {
		t.Errorf("texts %q", texts)
	}
	if p.intro.Visible() || p.disk.Visible() || p.plot.Visible() {
		t.Errorf("intro %v, bar %v, chart %v shown for an unbound claim", p.intro.Visible(), p.disk.Visible(), p.plot.Visible())
	}
}

// The volume's name above the chart is a link to the volume's drawer, each pod's to the pod's.
func TestClaimIntroLinksTheVolume(t *testing.T) {
	_, p, _, f, q := volumeDrawerOf(t, ThemeDark, nil)
	p.intro.links()[pgClaim.Volume]()
	p.pods.links()["pg-bezon-timescale-1-0"]()
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 2 })
	var got []model.ObjectRef
	for _, c := range f.callsOf("ui.navigate") {
		got = append(got, decodeParams[refParams](t, c).Ref)
	}
	for _, want := range []model.ObjectRef{
		{Context: "demo", Kind: "persistentvolumes", Name: pgClaim.Volume},
		{Context: "demo", Kind: "pods", Namespace: pgClaim.Namespace, Name: "pg-bezon-timescale-1-0"},
	} {
		if !slices.Contains(got, want) {
			t.Errorf("links went to %+v, not %+v", got, want)
		}
	}
}

// Several pods are a list, each a link; none, no line.
func TestVolumePodsLine(t *testing.T) {
	v := pgClaim
	v.Pods = []string{"web-0", "web-1", "web-2"}
	var opened []string
	segs := podSegs(&v, func(ref model.ObjectRef) { opened = append(opened, ref.Key()) })
	var text strings.Builder
	for _, s := range segs {
		text.WriteString(s.text)
		if s.onTap != nil {
			s.onTap()
		}
	}
	if text.String() != "Mounted by pods: web-0, web-1, web-2" || !slices.Equal(opened, []string{"bezon-prod/web-0", "bezon-prod/web-1", "bezon-prod/web-2"}) {
		t.Errorf("line %q, links %q", text.String(), opened)
	}
	_, p, _ := volumeDrawer(t, ThemeDark, func(m *engine.ObjectMetrics) { m.Volume.Pods = nil })
	if !p.intro.Visible() || p.pods.Visible() {
		t.Errorf("without pods: intro %v, pods line %v", p.intro.Visible(), p.pods.Visible())
	}
}

func TestVolumeIntroInAVolumesDrawer(t *testing.T) {
	prefix, link := volumeIntro(&pgClaim, "persistentvolumes")
	if prefix != "Display metrics from PVC: " || link != (model.ObjectRef{Kind: "persistentvolumeclaims", Namespace: pgClaim.Namespace, Name: pgClaim.Claim}) {
		t.Errorf("intro = %q %+v", prefix, link)
	}
}
