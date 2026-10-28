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
	return &engine.ObjectMetrics{
		Ref: ref, Status: metrics.StatusAvailable, Source: promSource, Providers: bothAnswering(),
		Capabilities: metrics.Capabilities{History: true, Network: true, Filesystem: true},
		Variants:     []string{engine.VariantFilesystem}, FilesystemSource: metrics.FilesystemUsage,
		Targets: []engine.MetricsTarget{{Variant: engine.VariantFilesystem, Filesystem: fs, Window: time.Hour, Range: "1h"}},
		Volume:  &vc, Fill: &last,
	}
}

// volumeDrawer opens the claim's drawer with the canned numbers, changed by mutate.
func volumeDrawer(t *testing.T, mode ThemeMode, mutate func(*engine.ObjectMetrics)) (*shell, *metricsPanel, func(string)) {
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
	return sh, sh.drawer.kept[metricsKey].(*metricsPanel), func(name string) { savePNG(t, w, name) }
}

func TestClaimDrawerShowsItsVolumesUsage(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		sh, p, png := volumeDrawer(t, mode, nil)
		texts := drawerTexts(sh)
		if texts[0] != "Metrics" || !p.intro.Visible() || !strings.HasPrefix(p.intro.text, "A claim holds no data itself: this is the usage of its volume pvc-f0327b26") {
			t.Fatalf("the section begins %q, intro %q (shown %v)", texts[:min(3, len(texts))], p.intro.text, p.intro.Visible())
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

func TestVolumeIntroInAVolumesDrawer(t *testing.T) {
	if got := volumeIntro(&pgClaim, "persistentvolumes"); got != "The usage of this volume, as the kubelet reports it for its claim bezon-prod/pg-bezon-timescale-1." {
		t.Errorf("intro = %q", got)
	}
}
