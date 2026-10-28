package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// ui.setMetricsRange: the time range of one chart in the detail drawer's Metrics section (G12,
// architecture §9). The range is UI state the engine holds for the open drawer, so the window's
// range control and an agent set the same thing; ui.state lists every chart's range.

type metricsRangeParams struct {
	Ref       model.ObjectRef `json:"ref" jsonschema:"the pod, node, persistent volume claim or persistent volume the detail drawer shows (ui_state's drawer)"`
	Container string          `json:"container,omitempty" jsonschema:"a container's chart in a pod's drawer; omitted: the object's own chart"`
	Range     string          `json:"range,omitempty" jsonschema:"the time range, 1m to 30d, as 15m, 6h or 7d; omitted or default: the default 1h"`
}

// ChartRange is one chart of the drawer's Metrics section, its time range and its variant.
type ChartRange struct {
	// Container is the container's name; empty for the object's own chart.
	Container string `json:"container,omitempty"`
	Range     string `json:"range"`
	// Variant is what the chart shows: cpu, memory, network or filesystem (ui.setMetricsVariant).
	Variant string `json:"variant"`
}

// MetricsRangeResult is ui.setMetricsRange's and ui.setMetricsVariant's result: every chart of the
// drawer with its range and variant.
type MetricsRangeResult struct {
	Ref    model.ObjectRef `json:"ref"`
	Charts []ChartRange    `json:"charts"`
}

func registerMetricsRange(g *registrar) {
	registerMetricsVariant(g)
	add(g, engine.Spec[metricsRangeParams]{
		ID: "ui.setMetricsRange", Label: "Metrics Time Range", Danger: engine.DangerUI,
		Description: "Sets the time range of one chart in the detail drawer's Metrics section, as the chart's range " +
			"control does: the pod's, node's, persistent volume claim's or persistent volume's own chart, or with container " +
			"one container's chart. The drawer must show " +
			"the object (ui_navigate opens it). With Prometheus the chart then shows that much history; the Metrics " +
			"Server keeps none, so its charts show at most the time since the drawer opened. Every chart's range is the " +
			"drawer's: it ends when the drawer closes or shows another object. ui_state lists the charts' ranges. " +
			"Returns every chart's range.",
		Enabled: func(s *engine.Session, _ metricsRangeParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p metricsRangeParams) (any, error) {
			window, err := engine.ParseChartRange(p.Range)
			if err != nil {
				return nil, err
			}
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
			if err := s.SetMetricsRange(m.Ref, p.Container, window); err != nil {
				return nil, err
			}
			if m, err = s.ObjectMetrics(m.Ref); err != nil || m == nil {
				return nil, err
			}
			return MetricsRangeResult{Ref: m.Ref, Charts: chartRanges(m)}, nil
		},
	})
}

// chartNames are the charts' container names, "" for the object's own.
func chartNames(m *engine.ObjectMetrics) []string {
	out := make([]string, len(m.Targets))
	for i, t := range m.Targets {
		out[i] = t.Name
	}
	return out
}

// chartRanges lists each chart of a Metrics section with its time range.
func chartRanges(m *engine.ObjectMetrics) []ChartRange {
	out := make([]ChartRange, 0, len(m.Targets))
	for _, t := range m.Targets {
		out = append(out, ChartRange{Container: t.Name, Range: t.Range, Variant: t.Variant})
	}
	return out
}

func quoted(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = fmt.Sprintf("%q", n)
	}
	return out
}
