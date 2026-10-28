package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// ui.setMetricsVariant: what one chart in the detail drawer's Metrics section shows (G12,
// architecture §9). Like the chart's range it is UI state the engine holds for the open drawer, so
// the window's variant buttons and an agent set the same thing, and it decides what Cogpit asks
// Prometheus for: the network and the filesystem only while a chart shows them.

type metricsVariantParams struct {
	Ref       model.ObjectRef `json:"ref" jsonschema:"the pod, node, persistent volume claim or persistent volume the detail drawer shows (ui_state's drawer)"`
	Container string          `json:"container,omitempty" jsonschema:"a container's chart in a pod's drawer; omitted: the object's own chart"`
	Variant   string          `json:"variant" jsonschema:"cpu, memory, network or filesystem; network and filesystem need Prometheus"`
}

func registerMetricsVariant(g *registrar) {
	add(g, engine.Spec[metricsVariantParams]{
		ID: "ui.setMetricsVariant", Label: "Metrics Chart Variant", Danger: engine.DangerUI,
		Description: "Sets what one chart in the detail drawer's Metrics section shows, as the chart's variant buttons " +
			"do: cpu, memory, network or filesystem, for the pod's or node's own chart or with container one container's " +
			"chart; a persistent volume claim's or persistent volume's chart shows the volume's filesystem alone. The " +
			"drawer must show the object (ui_navigate opens it). network and filesystem come from the cluster's " +
			"Prometheus only, which Cogpit asks for them only while a chart shows them; without Prometheus they are refused. " +
			"Every chart's variant is the drawer's: it ends when the drawer closes or shows another object. ui_state lists " +
			"the charts' variants and ranges. Returns every chart's variant and range.",
		Enabled: func(s *engine.Session, _ metricsVariantParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p metricsVariantParams) (any, error) {
			m, err := s.ObjectMetrics(p.Ref)
			if err != nil {
				return nil, err
			}
			if m == nil {
				return nil, fmt.Errorf("%w: %s has no metrics charts; pods, nodes, persistent volume claims and persistent volumes have", engine.ErrNotApplicable, p.Ref.Kind)
			}
			if names := chartNames(m); len(m.Targets) > 0 && !slices.Contains(names, p.Container) {
				return nil, fmt.Errorf("%s has no chart for container %q; its charts: %s", m.Ref.String(), p.Container, strings.Join(quoted(names), ", "))
			}
			if err := s.SetMetricsVariant(m.Ref, p.Container, strings.ToLower(strings.TrimSpace(p.Variant))); err != nil {
				return nil, err
			}
			if m, err = s.ObjectMetrics(m.Ref); err != nil || m == nil {
				return nil, err
			}
			return MetricsRangeResult{Ref: m.Ref, Charts: chartRanges(m)}, nil
		},
	})
}
