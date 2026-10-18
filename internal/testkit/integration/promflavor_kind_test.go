//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Thanos and VictoriaMetrics, read as Prometheus is (G12, D-71, night run 2026-10-16/17): found by
// their Services' names under their own names, and drawn from their answers. The kind cluster's
// VictoriaMetrics (make kind-vm: single-node and a cluster, both fed from the kind Prometheus
// through /federate) and, with COGPIT_REAL_THANOS, a real cluster's Thanos Querier and query
// frontend. They only read; the Prometheus setting they make lives in the session (no
// metrics.json).

// flavorService builds detection's view of a Service from the cache.
func flavorService(ctx context.Context, t *testing.T, s *engine.Session, ref model.ObjectRef) (metrics.Service, bool) {
	t.Helper()
	obj, err := s.Object(ctx, ref)
	if errors.Is(err, engine.ErrNotFound) {
		return metrics.Service{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	out := metrics.Service{Namespace: ref.Namespace, Name: ref.Name}
	ports, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
	for _, p := range ports {
		m, _ := p.(map[string]any)
		name, _ := m["name"].(string)
		var port int64
		switch v := m["port"].(type) {
		case int64:
			port = v
		case float64:
			port = int64(v)
		}
		out.Ports = append(out.Ports, metrics.ServicePort{Name: name, Port: int32(port)})
	}
	return out, true
}

// setPrometheusAs sets the cluster's Prometheus as the window's user does (an agent's would wait
// for a confirmation, A16).
func setPrometheusAs(ctx context.Context, t *testing.T, s *engine.Session, address, prefix string) engine.PrometheusSetting {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"address": address, "prefix": prefix})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.setPrometheus", Params: raw, Origin: engine.OriginGUI})
	if err != nil {
		t.Fatalf("cluster.setPrometheus %s%s: %v", address, prefix, err)
	}
	return res.(engine.PrometheusSetting)
}

// checkFlavorDrawers opens a node's and a running pod's drawer and checks that their history and
// network come from the flavor's server, named in the source line.
func checkFlavorDrawers(ctx context.Context, t *testing.T, s *engine.Session, dispatch func(string, any) any, source string, wantPast bool) {
	t.Helper()
	dispatch("ui.openView", map[string]any{"view": "nodes"})
	var node model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("nodes")
		if tbl == nil || !tbl.Synced || len(tbl.Rows) == 0 {
			return false
		}
		node = tbl.Rows[0].Ref
		return true
	}) {
		t.Fatal("no node")
	}
	opened := time.Now()
	dispatch("ui.navigate", map[string]any{"ref": node})
	m := waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool {
		return strings.HasPrefix(m.Source, source) && len(m.Targets[0].Samples) > 0 && m.HistorySource != ""
	})
	t.Logf("%s: node source %q, history from %q", source, m.Source, m.HistorySource)
	logProviders(t, source+" node", m.Providers)
	if m.HistorySource != metrics.UsageNodeExporter {
		t.Errorf("%s: a node's history from %q, want node-exporter", source, m.HistorySource)
	}
	if wantPast {
		checkPast(t, source+" node", m.Targets[0].Samples, opened)
	}
	dispatch("ui.setMetricsVariant", map[string]any{"ref": node, "variant": "network"})
	m = waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool { return len(m.Targets[0].Network) > 0 })
	logTarget(t, source+" node "+node.Name, m.FilesystemSource, m.Targets[0])
	dispatch("ui.setMetricsVariant", map[string]any{"ref": node, "variant": "cpu"})

	dispatch("ui.openView", map[string]any{"view": "pods"})
	var pod model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("pods")
		if tbl == nil || !tbl.Synced {
			return false
		}
		for _, r := range tbl.Rows { // the API server's static pod (kubeadm, kind), else any of kube-system's
			if r.Ref.Namespace == "kube-system" && (pod.Name == "" || strings.HasPrefix(r.Ref.Name, "kube-apiserver")) {
				pod = r.Ref
			}
		}
		return pod.Name != ""
	}) {
		t.Fatal("no API server pod")
	}
	opened = time.Now()
	dispatch("ui.navigate", map[string]any{"ref": pod})
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		return strings.HasPrefix(m.Source, source) && len(m.Targets[0].Samples) > 0
	})
	if wantPast {
		checkPast(t, source+" pod", m.Targets[0].Samples, opened)
	}
	logTarget(t, source+" pod "+pod.Name, m.FilesystemSource, m.Targets[0])
}

// TestKindVictoriaMetrics: the kind cluster's VictoriaMetrics, single-node and the cluster's
// vmselect, are detection's candidates under their own names (vmselect under the default
// tenant's path); named in the Prometheus setting, the drawers' history and network come from
// each, the source line, the status line and the setting say "VictoriaMetrics". Skipped without
// make kind-vm.
func TestKindVictoriaMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	s, dispatch := kindMetricsSession(ctx, t, metrics.Options{})
	single, ok := flavorService(ctx, t, s, model.ObjectRef{Kind: "services", Namespace: "cogpit-vm", Name: "vmsingle-cogpit"})
	if !ok {
		t.Skip("the kind cluster has no VictoriaMetrics: run make kind-vm")
	}
	sel, _ := flavorService(ctx, t, s, model.ObjectRef{Kind: "services", Namespace: "cogpit-vm", Name: "vmselect-cogpit"})
	ins, _ := flavorService(ctx, t, s, model.ObjectRef{Kind: "services", Namespace: "cogpit-vm", Name: "vminsert-cogpit"})
	stor, _ := flavorService(ctx, t, s, model.ObjectRef{Kind: "services", Namespace: "cogpit-vm", Name: "vmstorage-cogpit"})
	cands := metrics.Candidates([]metrics.Service{single, sel, ins, stor})
	if len(cands) != 2 || cands[0].Target.Service != "vmsingle-cogpit" || !slices.Contains(cands[0].Ports, "http") ||
		cands[0].Target.Flavor != metrics.FlavorVictoriaMetrics || cands[1].Target.Service != "vmselect-cogpit" ||
		cands[1].Target.Prefix != metrics.VMSelectPrefix {
		t.Fatalf("candidates %+v", cands)
	}

	for _, c := range []struct{ address, prefix, source string }{
		{"cogpit-vm/vmsingle-cogpit:http", "", "VictoriaMetrics (cogpit-vm/vmsingle-cogpit)"},
		{"cogpit-vm/vmselect-cogpit:http", metrics.VMSelectPrefix, "VictoriaMetrics (cogpit-vm/vmselect-cogpit)"},
	} {
		setPrometheusAs(ctx, t, s, c.address, c.prefix)
		checkFlavorDrawers(ctx, t, s, dispatch, c.source, true) // make kind-vm a few minutes before: its history starts with it
		set, err := s.PrometheusSetting("")
		if err != nil || set.InUseStatus != metrics.StatusAvailable || !strings.HasPrefix(set.InUse, "VictoriaMetrics "+strings.TrimSuffix(c.address, ":http")) {
			t.Errorf("cluster.prometheus %+v (%v)", set, err)
		}
	}
	setPrometheusAs(ctx, t, s, "auto", "")
}

// TestRealThanos: a real cluster's Thanos, each Service COGPIT_REAL_THANOS names
// (namespace/service:port, comma-separated: a Querier, a query frontend), is a candidate under
// its own name, and named in the Prometheus setting the drawers' history and network come from it,
// "Thanos" in the source line. Never in CI; it only reads.
func TestRealThanos(t *testing.T) {
	named := os.Getenv("COGPIT_REAL_THANOS")
	if named == "" {
		t.Skip("COGPIT_REAL_THANOS is not set: namespace/service:port of a real cluster's Thanos Querier or query frontend")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	s, dispatch := realMetricsSession(ctx, t, metrics.Options{})
	for _, address := range strings.Split(named, ",") {
		ns, svc, port, err := engine.ParsePrometheusAddress(address)
		if err != nil {
			t.Fatal(err)
		}
		found, ok := flavorService(ctx, t, s, model.ObjectRef{Kind: "services", Namespace: ns, Name: svc})
		if !ok {
			t.Fatalf("%s: no such Service", address)
		}
		cands := metrics.Candidates([]metrics.Service{found})
		if len(cands) != 1 || cands[0].Target.Flavor != metrics.FlavorThanos || cands[0].Target.Port != port {
			t.Errorf("%s: candidates %+v", address, cands)
		} else {
			t.Logf("%s: a candidate, score %d", address, cands[0].Score)
		}
		before := s.Diagnostics().Requests.ByVerbResource["get services/proxy"]
		setPrometheusAs(ctx, t, s, address, "")
		checkFlavorDrawers(ctx, t, s, dispatch, "Thanos ("+ns+"/"+svc+")", true)
		t.Logf("%s: %s Prometheus API requests", address, strconv.FormatInt(s.Diagnostics().Requests.ByVerbResource["get services/proxy"]-before, 10))
	}
	setPrometheusAs(ctx, t, s, "auto", "")
}
