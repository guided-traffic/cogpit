package metrics

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestQuoteLabel(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"web", `"web"`},
		{`a"b`, `"a\"b"`},
		{`a\b`, `"a\\b"`},
		{"a\nb", `"a\nb"`},
		{"a\x00b", `"a\x00b"`},
		{`x"} or vector(1) #`, `"x\"} or vector(1) #"`},
		{"ünï", `"ünï"`},
	} {
		if got := quoteLabel(tc.in); got != tc.want {
			t.Errorf("quoteLabel(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// Whatever a name holds, the literal reads back as exactly that name: it cannot end early.
func FuzzQuoteLabel(f *testing.F) {
	for _, s := range []string{"", "web", `"`, `\`, `\"`, "\n", `a"} or up{x="`, "\x00\x7f"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip()
		}
		q := quoteLabel(s)
		got, err := strconv.Unquote(q)
		if err != nil || got != s {
			t.Fatalf("quoteLabel(%q) = %s reads back as %q (%v)", s, q, got, err)
		}
	})
}

func TestQueries(t *testing.T) {
	pod := podPlan("web", "a", rangeRate(30*time.Second), QuerySettings{})
	for name, tc := range map[string]struct{ got, want string }{
		"cpu":    {pod.cpu, `sum by (container) (rate(container_cpu_usage_seconds_total{namespace="web",pod="a",container!="",container!="POD"}[4m]))`},
		"mem":    {pod.mem, `sum by (container) (container_memory_working_set_bytes{namespace="web",pod="a",container!="",container!="POD"})`},
		"rx":     {pod.rx, `sum(rate(container_network_receive_bytes_total{namespace="web",pod="a"}[4m]))`},
		"tx":     {pod.tx, `sum(rate(container_network_transmit_bytes_total{namespace="web",pod="a"}[4m]))`},
		"fs":     {pod.fsUsage, `sum by (container) (container_fs_usage_bytes{namespace="web",pod="a",container!="",container!="POD"})`},
		"fsr":    {pod.fsReads, `sum by (container) (rate(container_fs_reads_bytes_total{namespace="web",pod="a",container!="",container!="POD"}[4m]))`},
		"list":   {podListRate(metricCPU, "", containerMatchers), `sum by (namespace, pod, container) (rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[4m]))`},
		"week":   {podPlan("web", "a", rangeRate(605*time.Second), QuerySettings{}).rx, `sum(rate(container_network_receive_bytes_total{namespace="web",pod="a"}[665s]))`},
		"listns": {podListGauge(metricMemory, "web", containerMatchers), `sum by (namespace, pod, container) (container_memory_working_set_bytes{namespace="web",container!="",container!="POD"})`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, tc.got, tc.want)
		}
	}

	evil := podPlan(`ns"} or up{a="`, `p\"`, rateWindow, QuerySettings{})
	if !strings.Contains(evil.cpu, `namespace="ns\"} or up{a=\""`) || !strings.Contains(evil.cpu, `pod="p\\\""`) {
		t.Errorf("names were not quoted: %s", evil.cpu)
	}

	node := nodePlan("n1.example", nil, rangeRate(30*time.Second), QueryAuto)
	want := `sum(rate(container_cpu_usage_seconds_total{id="/",node="n1.example"}[4m])) or ` +
		`sum(rate(container_cpu_usage_seconds_total{id="/",instance=~"n1\\.example(:[0-9]+)?"}[4m]))`
	if node.cpu != want {
		t.Errorf("node cpu:\n got %s\nwant %s", node.cpu, want)
	}

	// node-exporter's (A43): the node label, else the instance by the node's name or addresses
	// (kube-prometheus-stack's exporter, scraped under its host-network address), an IPv6 address
	// also in brackets; every exporter counted once.
	node = nodePlan("n1.example", []string{"10.0.0.7", "fd00::7"}, rangeRate(30*time.Second), QueryAuto)
	inst := `instance=~"(n1\\.example|10\\.0\\.0\\.7|fd00::7|\\[fd00::7\\])(:[0-9]+)?"`
	for name, tc := range map[string]struct{ got, want string }{
		"exporter cpu": {node.exporterCPU,
			`sum(max by (node, instance) (sum by (node, instance, job) (rate(node_cpu_seconds_total{node="n1.example",mode!~"idle|iowait|steal"}[4m])))) or ` +
				`sum(max by (node, instance) (sum by (node, instance, job) (rate(node_cpu_seconds_total{` + inst + `,mode!~"idle|iowait|steal"}[4m]))))`},
		"exporter mem": {node.exporterMem,
			`sum(max by (node, instance) (node_memory_MemTotal_bytes{node="n1.example"} - node_memory_MemFree_bytes{node="n1.example"} - node_memory_Inactive_file_bytes{node="n1.example"})) or ` +
				`sum(max by (node, instance) (node_memory_MemTotal_bytes{` + inst + `} - node_memory_MemFree_bytes{` + inst + `} - node_memory_Inactive_file_bytes{` + inst + `}))`},
		"cadvisor cpu with addresses": {node.cpu,
			`sum(rate(container_cpu_usage_seconds_total{id="/",node="n1.example"}[4m])) or sum(rate(container_cpu_usage_seconds_total{id="/",` + inst + `}[4m]))`},
		"every exporter": {exporterCPU("", rateWindow),
			`sum(max by (node, instance) (sum by (node, instance, job) (rate(node_cpu_seconds_total{mode!~"idle|iowait|steal"}[4m]))))`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, tc.got, tc.want)
		}
	}
	evilNode := nodePlan(`n"} or up{a="`, []string{`1.2.3.4"} or vector(1) #`}, rateWindow, QueryAuto)
	if !strings.Contains(evilNode.exporterMem, `{node="n\"} or up{a=\""}`) || !strings.Contains(evilNode.exporterMem, `1\\.2\\.3\\.4\"\\} or vector\\(1\\) #)(:[0-9]+)?"}`) {
		t.Errorf("a node's name or address was not quoted: %s", evilNode.exporterMem)
	}
}

func TestParseResult(t *testing.T) {
	t.Run("matrix", func(t *testing.T) {
		got, err := parseResult([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"container":"app"},"values":[[1000,"0.5"],[1030.5,"NaN"],[1060,"+Inf"],[1090,"1.25"]]}]}}`))
		if err != nil || len(got) != 1 {
			t.Fatalf("%v %v", got, err)
		}
		pts := got[0].points
		if len(pts) != 2 || pts[0] != (point{1000000, 0.5}) || pts[1] != (point{1090000, 1.25}) {
			t.Errorf("points = %+v (NaN and Inf must be dropped)", pts)
		}
	})
	t.Run("vector", func(t *testing.T) {
		got, err := parseResult([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"pod":"a"},"value":[1000.25,"3"]}]}}`))
		if err != nil || len(got) != 1 || got[0].points[0] != (point{1000250, 3}) {
			t.Errorf("%+v %v", got, err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		got, err := parseResult([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		if err != nil || len(got) != 0 {
			t.Errorf("%+v %v", got, err)
		}
	})
	for name, body := range map[string]string{
		"error status": `{"status":"error","errorType":"bad_data","error":"parse error"}`,
		"not json":     `<html>`,
		"bad sample":   `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1,"x"]]}]}}`,
		"odd type":     `{"status":"success","data":{"resultType":"streams","result":[]}}`,
	} {
		if _, err := parseResult([]byte(body)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A46: charts of one object whose ranges lie far apart get query sets of their own, each at its
// own step; the coarsest set is the one set there was before, so no chart gets fewer points.
func TestQuerySetsForFarApartRanges(t *testing.T) {
	h := NewHub(nil, Options{})
	defer h.Close()
	const day = 24 * time.Hour
	for _, tc := range []struct {
		name    string
		windows []time.Duration // nil: a subscription that set none
		want    []band
	}{
		{"the default", nil, []band{{time.Hour, 30 * time.Second}}},
		{"close ranges share a set", []time.Duration{5 * time.Minute, time.Hour}, []band{{time.Hour, 15 * time.Second}}},
		{"six hours and the hour", []time.Duration{time.Hour, 6 * time.Hour}, []band{{6 * time.Hour, 30 * time.Second}}},
		{"twelve hours: the hour keeps more than half its points", []time.Duration{time.Hour, 12 * time.Hour}, []band{{12 * time.Hour, 44 * time.Second}}},
		{"a day and the hour", []time.Duration{time.Hour, day}, []band{{time.Hour, 30 * time.Second}, {day, 87 * time.Second}}},
		{"a week and the hour", []time.Duration{7 * day, time.Hour}, []band{{time.Hour, 30 * time.Second}, {7 * day, 605 * time.Second}}},
		{"a week alone keeps its thousand points", []time.Duration{7 * day}, []band{{7 * day, 605 * time.Second}}},
		{"five minutes and the hour share the finer set", []time.Duration{5 * time.Minute, time.Hour, 7 * day}, []band{{time.Hour, 15 * time.Second}, {7 * day, 605 * time.Second}}},
		{"three far apart", []time.Duration{time.Hour, day, 30 * day}, []band{{time.Hour, 30 * time.Second}, {day, 720 * time.Second}, {30 * day, 2592 * time.Second}}},
	} {
		st := &scopeState{subs: map[*Subscription]struct{}{{windows: tc.windows}: {}}}
		got := h.bandsLocked(st)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
		for _, b := range got {
			if n := b.window / b.step; n > 1000 {
				t.Errorf("%s: %v gets %d points", tc.name, b, n)
			}
		}
	}
	// Two subscriptions: one that set none shows the default hour beside the other's week.
	st := &scopeState{subs: map[*Subscription]struct{}{{}: {}, {windows: []time.Duration{7 * day}}: {}}}
	if got := h.bandsLocked(st); len(got) != 2 || got[0] != (band{time.Hour, 30 * time.Second}) {
		t.Errorf("a subscription without windows: %+v", got)
	}
}
