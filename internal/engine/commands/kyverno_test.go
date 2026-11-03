package commands_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var polrGVR = schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "policyreports"}

// TestKyvernoViolationsCommand drives the Kyverno integration as an agent does: kyverno_violations
// lists the violations with their objects' kind keys, resources_list carries the mark in a Warning
// cell, the open Deployments view sorts by the warning column the integration added, and with the
// integration off the command is disabled and says why.
func TestKyvernoViolationsCommand(t *testing.T) {
	src := seed()
	v := testkit.CRDVersion{Name: "v1alpha2", Served: true, Storage: true}
	src.Set(testkit.CRDsGVR, testkit.CRD(kinds.PolicyReportGroup, "policyreports", "PolicyReport", true, v))
	report := testkit.Object(kinds.PolicyReportGroup+"/v1alpha2", "PolicyReport", "shop", "uid-shop-web", map[string]any{
		"scope": map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "namespace": "shop", "name": "web", "uid": "uid-shop-web"},
		"results": []any{map[string]any{
			"policy": "require-team-label", "rule": "check-team", "result": "fail", "source": "kyverno",
			"message": "validation error: The label `team` is required.", "properties": map[string]any{"process": "background scan"},
		}},
	})
	report.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "kyverno"})
	src.Set(polrGVR, report)

	h := &harness{src: src, writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: crdConnector{src: src}, SnapshotInterval: 5 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	agent := engine.OriginExternalAgent
	var res commands.KyvernoViolationsResult
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		return h.run(t, agent, "kyverno.violations", map[string]any{}, &res) == nil && res.Total == 1
	}) {
		t.Fatalf("kyverno_violations: %+v", res)
	}
	got := res.Violations[0]
	if got.Object.Kind != "deployments" || got.Object.Namespace != "shop" || got.Object.Name != "web" || got.Object.Context != "test" ||
		got.Policy != "require-team-label" || got.Process != "background scan" || res.Objects != 1 {
		t.Errorf("violation %+v", got)
	}
	if h.must(t, agent, "kyverno.violations", map[string]any{"namespaces": []string{"infra"}}, &res); res.Total != 0 {
		t.Errorf("infra: %+v", res)
	}

	var list commands.ListResult
	h.must(t, agent, "resources.list", map[string]any{"kind": "deployments", "namespaces": []string{"shop"}}, &list)
	if len(list.Rows) != 1 || !strings.HasPrefix(list.Rows[0].Cells["Signals"], "Kyverno: 1 policy violation") {
		t.Errorf("resources_list %+v", list.Rows)
	}

	h.must(t, agent, "ui.openView", map[string]any{"view": "deployments"}, nil)
	h.must(t, agent, "ui.sort", map[string]any{"column": "signals", "desc": true}, nil)
	h.must(t, agent, "ui.setColumns", map[string]any{"hidden": []string{"signals"}}, nil)
	h.must(t, agent, "ui.setColumns", map[string]any{"reset": true}, nil)

	off := engine.IntKyverno
	h.s.UpdateUI(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{"test": {IntegrationsOff: &off}}
	})
	err := h.run(t, agent, "kyverno.violations", map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Errorf("with Kyverno off: %v", err)
	}
}
