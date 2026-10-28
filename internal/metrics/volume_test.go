package metrics_test

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A claim's volume (D-139): the kubelet's volume stats through Prometheus, one range query per
// poll, never the Metrics Server.

func part(name string) map[string]string {
	return map[string]string{"namespace": "bezon-prod", "persistentvolumeclaim": "pg-1", "part": name}
}

func claimQuery(ns, name string) metrics.RangeQuery {
	to := time.Unix(100020, 0)
	return metrics.RangeQuery{Kind: metrics.KindClaims, Namespace: ns, Name: name, From: to.Add(-time.Hour), To: to, Step: 30 * time.Second, Want: metrics.WantFilesystem}
}

func TestObjectRangeOfAClaim(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.on("kubelet_volume_stats_used_bytes",
		s(part("used"), 1000, 120*gib, 1030, 121*gib),
		s(part("capacity"), 1000, 196*gib, 1030, 196*gib, 1060, 196*gib), // 1060 without the bytes in use: no point
		s(part("available"), 1000, 66*gib, 1030, 65*gib))
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	before := c.proxyCount()

	got, err := prov.ObjectRange(context.Background(), claimQuery("bezon-prod", "pg-1"))
	if err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 1 {
		t.Errorf("%d requests for a claim's chart, want 1", n)
	}
	whole := got.Targets[""]
	if got.FilesystemSource != metrics.FilesystemUsage || whole == nil || len(got.Targets) != 1 {
		t.Fatalf("answer = %+v", got)
	}
	want := []model.FilesystemSample{
		{At: time.Unix(1000, 0), UsageBytes: 120 * gib, CapacityBytes: 196 * gib, AvailableBytes: 66 * gib},
		{At: time.Unix(1030, 0), UsageBytes: 121 * gib, CapacityBytes: 196 * gib, AvailableBytes: 65 * gib},
	}
	if len(whole.Filesystem) != len(want) {
		t.Fatalf("filesystem = %+v, want %+v", whole.Filesystem, want)
	}
	for i := range want {
		if g := whole.Filesystem[i]; !g.At.Equal(want[i].At) || g.UsageBytes != want[i].UsageBytes || g.CapacityBytes != want[i].CapacityBytes || g.AvailableBytes != want[i].AvailableBytes {
			t.Errorf("point %d = %+v, want %+v", i, g, want[i])
		}
	}
	if sh := whole.Filesystem[1].Share(); sh < 0.617 || sh > 0.618 {
		t.Errorf("share = %v, want 121/196", sh)
	}

	var q promRequest
	for _, r := range c.prom.requests() {
		if strings.Contains(r.path, "query_range") {
			q = r
		}
	}
	for _, sub := range []string{
		`max by (namespace, persistentvolumeclaim) (kubelet_volume_stats_used_bytes{namespace="bezon-prod",persistentvolumeclaim="pg-1"})`,
		`kubelet_volume_stats_capacity_bytes{`, `kubelet_volume_stats_available_bytes{`, `"part", "used"`, `"part", "capacity"`, `"part", "available"`,
	} {
		if !strings.Contains(q.query, sub) {
			t.Errorf("the query lacks %s: %s", sub, q.query)
		}
	}
	if q.step != "30" || q.end != "100020" {
		t.Errorf("range params: %+v", q)
	}
}

func TestClaimWithoutVolumeStatsIsAnEmptyAnswer(t *testing.T) {
	c := newPromCluster(t, false)
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	got, err := prov.ObjectRange(context.Background(), claimQuery("bezon-prod", "pg-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Targets) != 0 || got.FilesystemSource != "" {
		t.Errorf("a claim the kubelet does not report = %+v, want no series", got)
	}
}

func TestClaimNamesAreQuotedOnTheWire(t *testing.T) {
	c := newPromCluster(t, false)
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	if _, err := prov.ObjectRange(context.Background(), claimQuery(`ns"}`, `a"} or vector(1) #`)); err != nil {
		t.Fatal(err)
	}
	for _, r := range c.prom.requests() {
		if r.query == "" {
			continue
		}
		if !strings.Contains(r.query, `persistentvolumeclaim="a\"} or vector(1) #"`) || !strings.Contains(r.query, `namespace="ns\"}"`) {
			t.Errorf("unquoted claim in %s", r.query)
		}
	}
}

var claimPG1 = metrics.ClaimScope("bezon-prod", "pg-1")

// A claim's chart asks Prometheus alone, for its volume's filesystem: never the Metrics Server,
// not even its detection, though it answers.
func TestClaimChartAsksPrometheusAlone(t *testing.T) {
	d := newDual(t)
	onChange, polls := pollCounter()
	sub := d.h.Subscribe(claimPG1, onChange)
	t.Cleanup(sub.Release)
	waitPoll(t, polls, 1)

	qs := d.hist.ranges()
	now := d.clk.Now()
	if len(qs) != 1 || qs[0].Kind != metrics.KindClaims || qs[0].Namespace != "bezon-prod" || qs[0].Name != "pg-1" ||
		qs[0].Want != metrics.WantFilesystem || !qs[0].From.Equal(now.Add(-time.Hour)) || qs[0].Step != 30*time.Second {
		t.Fatalf("Prometheus was asked %+v, want the claim's filesystem over the hour", qs)
	}
	if gets, lists := d.live.counts(); gets+lists != 0 || d.liveDet.n.Load() != 0 {
		t.Errorf("the Metrics Server was asked (%d gets, %d lists, %d detections)", gets, lists, d.liveDet.n.Load())
	}
	if st := d.h.State(claimPG1); !st.Usable() || st.Provider != metrics.PrometheusID {
		t.Errorf("state = %+v", st)
	}
	if hist := d.h.History(claimPG1); hist == nil || hist.FilesystemSource != metrics.FilesystemUsage {
		t.Errorf("history = %+v", hist)
	}
	// Its status line says the Metrics Server is not used; a pod's says nothing of the kind.
	for sc, want := range map[metrics.Scope]map[string]bool{
		claimPG1: {metrics.PrometheusID: false, metrics.ServerID: true},
		podA:     {metrics.PrometheusID: false, metrics.ServerID: false},
	} {
		got := map[string]bool{}
		for _, ps := range d.h.ProvidersFor(sc) {
			got[ps.Provider] = ps.Unused
		}
		if !maps.Equal(got, want) {
			t.Errorf("%s: unused %v, want %v", sc.Kind, got, want)
		}
	}

	// The next poll asks from the last point on.
	d.clk.Advance(30 * time.Second)
	metrics.Kick(d.h, claimPG1)
	waitPoll(t, polls, 2)
	if qs = d.hist.ranges(); len(qs) != 2 || qs[1].Want != metrics.WantFilesystem || !qs[1].To.Equal(d.clk.Now()) || !qs[1].From.After(qs[0].From) {
		t.Errorf("the second poll asked %+v, want the newest points", qs)
	}
	if gets, lists := d.live.counts(); gets+lists != 0 {
		t.Errorf("the Metrics Server was asked (%d gets, %d lists)", gets, lists)
	}
}

// Without Prometheus a claim's state says so, even while the Metrics Server answers.
func TestClaimWithoutPrometheus(t *testing.T) {
	clk := &testClock{t: time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)}
	live := &liveFake{clk: clk}
	none := &countingDetector{st: metrics.State{Status: metrics.StatusNotServed, Message: "no Prometheus service found"}}
	h := metrics.NewHubFor(metrics.Sources{Live: detectorFor(live), History: none}, metrics.Options{ObjectInterval: time.Hour, Now: clk.Now})
	t.Cleanup(h.Close)
	onChange, polls := pollCounter()
	sub := h.Subscribe(claimPG1, onChange)
	t.Cleanup(sub.Release)
	waitPoll(t, polls, 1)

	st := h.State(claimPG1)
	if st.Status != metrics.StatusNotServed || !strings.Contains(st.Message, "no Prometheus was found") {
		t.Errorf("state = %+v, want not served for want of Prometheus", st)
	}
	if gets, lists := live.counts(); gets+lists != 0 {
		t.Errorf("the Metrics Server was asked (%d gets, %d lists)", gets, lists)
	}
}

// P19: a claim's or a volume's drawer costs one range query over the chart's range when it
// opens, then one per step for the newest points; nothing reaches the Metrics API; nothing once it
// closes.
func TestVolumeBudget(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.onRel("kubelet_volume_stats_used_bytes", s(part("used"), -0.3, 120*gib, 0, 121*gib), s(part("capacity"), -0.3, 196*gib, 0, 196*gib))
	clk, opts := budgetClock()
	h := promHub(t, c, opts)
	before := c.conn.Counter().Snapshot().Total // the connection's own

	onChange, polls := pollCounter()
	sub := h.Subscribe(claimPG1, onChange)
	waitPoll(t, polls, 1)
	for n := 2; n <= 3; n++ {
		nextPoll(t, h, claimPG1, clk, 30*time.Second, polls, n)
	}
	// The detection probe, then one query per poll.
	if got := c.proxyCount(); got != 1+3 {
		t.Errorf("%d proxy requests after 3 polls, want 4 (1 probe + 3 queries)", got)
	}
	if n := c.prom.queryCount("kubelet_volume_stats_used_bytes"); n != 3 {
		t.Errorf("%d volume queries in 3 polls, want 3", n)
	}
	if snap := c.conn.Counter().Snapshot(); snap.Total-before != c.proxyCount() {
		t.Errorf("requests besides Prometheus's: %v", snap.ByVerbResource)
	}
	hist := h.History(claimPG1)
	if hist == nil || hist.Targets[""] == nil || len(hist.Targets[""].Filesystem) == 0 {
		t.Fatalf("history = %+v", hist)
	}
	fs := hist.Targets[""].Filesystem
	if last := fs[len(fs)-1]; last.UsageBytes != 121*gib || last.CapacityBytes != 196*gib {
		t.Errorf("newest point = %+v", last)
	}
	sub.Release()
	closedQuiet(t, h, c)
}
