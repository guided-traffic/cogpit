package commands

import (
	"context"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// cluster.prometheus and cluster.setPrometheus: which Prometheus a connected cluster's metrics
// come from (G12; Lens: Cluster settings → Metrics, the Prometheus service address and a path
// prefix). The setting is kept in Cogpit's own preferences per context, never in the kubeconfig or
// the cluster.

type prometheusParams struct {
	engine.OnCluster
	Address string `json:"address,omitempty" jsonschema:"the Prometheus service as namespace/service:port, the port by number or name (monitoring/prometheus-server:80); omitted or auto: detect it by its well-known service names"`
	Prefix  string `json:"prefix,omitempty" jsonschema:"the path Prometheus serves under (its --web.route-prefix), such as /prometheus; omitted: none"`
	// Format, FilterEmptyContainers and HTTPS: how the Prometheus is asked (2026-10-16).
	Format                string `json:"format,omitempty" jsonschema:"the query format, how the cluster's Prometheus names a node's series: auto (default: node-exporter where it has them, else cAdvisor's root cgroup), prometheus-operator (kube-prometheus-stack: node-exporter alone), cadvisor (no node-exporter: the root cgroup alone)"`
	FilterEmptyContainers *bool  `json:"filterEmptyContainers,omitempty" jsonschema:"leave the series without a container name out of the pods' queries (default true, Lens's default); false keeps them, for a Prometheus whose cAdvisor series carry no container name"`
	HTTPS                 bool   `json:"https,omitempty" jsonschema:"ask the Prometheus service over https through the API server's service proxy (default false)"`
}

// querySettings are the parameters' query settings; an omitted field takes its default.
func (p prometheusParams) querySettings() (metrics.QuerySettings, error) {
	f, err := metrics.ParseQueryFormat(p.Format)
	if err != nil {
		return metrics.QuerySettings{}, err
	}
	return metrics.QuerySettings{Format: f, KeepEmptyContainers: p.FilterEmptyContainers != nil && !*p.FilterEmptyContainers, HTTPS: p.HTTPS}, nil
}

// autoAddress names detection in cluster.setPrometheus.
const autoAddress = "auto"

func registerPrometheusSetting(g *registrar) {
	add(g, engine.Spec[engine.OnCluster]{
		ID: "cluster.prometheus", Label: "Prometheus Setting", Danger: engine.DangerRead,
		Description: "Returns which Prometheus a connected cluster's metrics come from (the shown one unless context names another): auto (found among the " +
			"cluster's Services by its well-known names) or the service address namespace/service:port and path prefix " +
			"the user set, and whether the setting is kept in Cogpit's preferences. The metrics providers' state is in " +
			"metrics_get's providers.",
		Enabled: func(s *engine.Session, p engine.OnCluster) error { _, err := connectedOn(s, p.Context); return err },
		Run: func(_ context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			return s.PrometheusSetting(p.Context)
		},
	})
	add(g, engine.Spec[prometheusParams]{
		ID: "cluster.setPrometheus", Label: "Set Prometheus", Danger: engine.DangerUI,
		Description: "Sets which Prometheus a connected cluster's metrics come from (the shown one unless context names another), as Lens's cluster settings do: a " +
			"service address namespace/service:port and an optional path prefix, read through the API server's service " +
			"proxy, or auto to find it by its well-known service names again. It is kept in Cogpit's own preferences for " +
			"this context, never written to the kubeconfig or the cluster, and sends no request itself: the next chart " +
			"or list that needs Prometheus probes that service alone (one GET of its buildinfo). It also sets how the " +
			"Prometheus is asked, as Lens's Metrics settings do: the query format (how its node series are named), the " +
			"empty-container filter and HTTPS. It sets the whole setting: a field it omits takes its default (address: " +
			"auto; format: auto; filterEmptyContainers: true; https: false). An agent's request always " +
			"waits for the user's acceptance, which names the service. Returns the setting.",
		Enabled: func(s *engine.Session, p prometheusParams) error { _, err := connectedOn(s, p.Context); return err },
		// Where the cluster's metrics come from decides which service learns the names of the pods
		// and nodes the user opens (H-26): an agent's change always waits for the user (A16, owner
		// 2026-10-14 evening; always, as the lead's night decision N8 has UI commands ask).
		AgentsAsk: func(*engine.Session, prometheusParams) bool { return true },
		Effects:   prometheusEffects,
		Run: func(_ context.Context, s *engine.Session, p prometheusParams) (any, error) {
			q, err := p.querySettings()
			if err != nil {
				return nil, err
			}
			addr := strings.TrimSpace(p.Address)
			if addr == "" || strings.EqualFold(addr, autoAddress) {
				return s.SetPrometheusSetting(p.Context, nil, q)
			}
			ns, svc, port, err := engine.ParsePrometheusAddress(addr)
			if err != nil {
				return nil, err
			}
			return s.SetPrometheusSetting(p.Context, &metrics.Override{Namespace: ns, Service: svc, Port: port, Prefix: strings.TrimSpace(p.Prefix)}, q)
		},
	})
}

// prometheusEffects are the confirmation's rows: where the metrics come from, what that service
// learns, and how they are asked when that is not Lens's default.
func prometheusEffects(_ *engine.Session, p prometheusParams) []engine.Effect {
	addr := strings.TrimSpace(p.Address)
	out := []engine.Effect{
		{Name: "Metrics from", Value: addr + strings.TrimSpace(p.Prefix), Tone: engine.EffectNames},
		{Name: "Through", Value: "the API server's service proxy"},
		{Name: "Service sees", Value: "the names of the pods and nodes you open", Tone: engine.EffectWarning},
	}
	if addr == "" || strings.EqualFold(addr, autoAddress) {
		out = []engine.Effect{{Name: "Prometheus", Value: "found by its well-known service names again"}}
	}
	q, err := p.querySettings()
	if err != nil {
		return out
	}
	if q.Format != metrics.QueryAuto {
		out = append(out, engine.Effect{Name: "Query format", Value: q.Format.Label()})
	}
	if q.KeepEmptyContainers {
		out = append(out, engine.Effect{Name: "Pods' queries", Value: "keep the series without a container name"})
	}
	if q.HTTPS {
		out = append(out, engine.Effect{Name: "Scheme", Value: "https"})
	}
	return out
}
