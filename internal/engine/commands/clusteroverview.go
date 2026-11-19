package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The cluster Overview (ui-parity › Cluster overview and charts, G12): a page, not a kind. The
// engine builds it from the cache and the metrics hub; ui.openView "cluster/overview" shows it, and
// agents read it here. Its charts' time range and node group are UI state the engine holds
// (UIState.Overview), set by the page's controls and by agents alike.

type clusterOverviewParams struct {
	engine.OnCluster
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"namespaces whose issues to read (a node's issues always are); omitted: the namespace filter the user has set (every namespace for a cluster the window does not show)"`
	Series     bool     `json:"series,omitempty" jsonschema:"true: include each chart's points over its time range (while the page is open; otherwise there are none)"`
	NoIssues   bool     `json:"noIssues,omitempty" jsonschema:"true: leave the issues out (the charts only)"`
	EveryIssue bool     `json:"everyIssue,omitempty" jsonschema:"true: every issue as an entry of its own in issues, newest first, in place of issueFold: not bounded in size, and not with the parameters below"`

	Severity string               `json:"severity,omitempty" jsonschema:"fold only the issues of this severity: error or warning"`
	Sources  []string             `json:"sources,omitempty" jsonschema:"fold only the issues of these sources: status (an object's own state), event (a warning event)"`
	Reason   string               `json:"reason,omitempty" jsonschema:"fold only the issues of this cause: a reason of causes"`
	Subject  *engine.IssueSubject `json:"subject,omitempty" jsonschema:"a row of bySubject by its kind, namespace and name: returns that subject's issues (members), their messages whole"`
	Limit    int                  `json:"limit,omitempty" jsonschema:"rows of bySubject or members to return; default 20, at most 200"`
	Offset   int                  `json:"offset,omitempty" jsonschema:"rows to skip before the first one returned, for the next page"`
}

// narrows reports whether a parameter of the fold is set.
func (p clusterOverviewParams) narrows() bool {
	return p.Severity != "" || len(p.Sources) > 0 || p.Reason != "" || p.Subject != nil || p.Limit != 0 || p.Offset != 0
}

type overviewRangeParams struct {
	Range string `json:"range,omitempty" jsonschema:"the charts' time range, 1m to 30d, as 15m, 6h or 7d; omitted or default: the default 1h"`
}

type overviewNodesParams struct {
	Nodes string `json:"nodes,omitempty" jsonschema:"the node group the charts show: master (the control-plane nodes) or worker (every other node); omitted or default: the workers when the cluster has any, else the masters"`
}

func registerClusterOverview(g *registrar) {
	add(g, engine.Spec[clusterOverviewParams]{
		ID: "cluster.overview", Label: "Cluster Overview", MenuPath: "View/Cluster Overview", Danger: engine.DangerRead,
		Verbs: slices.Concat(cacheReads, metricsReads),
		Description: "Reads the cluster Overview: the node groups (master: control-plane nodes; worker: the others), the " +
			"group and time range the charts show (ui_setOverviewNodes, ui_setOverviewRange), the charts CPU (cores), " +
			"Memory (bytes) and Pods for that group with their current usage, requests and limits (sums over the pods " +
			"that have not finished on those nodes), allocatable capacity and capacity, the metrics providers' state, and " +
			"the issues (pods with a warning, nodes not Ready or under pressure, Deployments, Stateful Sets and Daemon " +
			"Sets short of ready pods, warning events of the last hour) folded in issueFold: exact counts (issues, " +
			"error, warning, subjects); bySubject, a row per subject (the workload that controls the affected pods, else " +
			"the object: kind, namespace and name are its ref) with its level, affected objects, state and most common " +
			"event; causes, each distinct state or event with the subjects, objects and issues that share it. Both are " +
			"tables: columns names a row's cells, same holds the cells all rows share. {name}, {image}, {uid}, {digest} " +
			"and {hash} in a message stand for what differs between objects. more says how to read on; subject returns " +
			"one subject's issues with whole messages; ownersUnread names kinds that could not be read, whose pods are " +
			"then subjects themselves. The issues and lines are read from the cache; while the page is " +
			"open the charts carry the metrics history (with series: true, their points), otherwise the current CPU and " +
			"memory come from one read of the nodes' metrics. ui_openView with view cluster/overview shows the page to " +
			"the user; ui_select with a ref opens its details there.",
		Enabled: func(s *engine.Session, p clusterOverviewParams) error {
			_, err := connectedOn(s, p.Context)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p clusterOverviewParams) (any, error) {
			if !p.NoIssues && !p.EveryIssue {
				return s.ClusterOverviewFolded(ctx, p.Context, p.Namespaces, p.Series, engine.IssueQuery{
					Severity: p.Severity, Sources: p.Sources, Reason: p.Reason, Subject: p.Subject, Limit: p.Limit, Offset: p.Offset,
				})
			}
			if p.narrows() {
				return nil, errors.New("severity, sources, reason, subject, limit and offset read the fold: leave out everyIssue and noIssues")
			}
			m, err := s.ClusterOverview(ctx, p.Context, p.Namespaces, p.Series)
			if err == nil && p.NoIssues {
				m.Issues = nil
			}
			return m, err
		},
	})

	add(g, engine.Spec[overviewRangeParams]{
		ID: "ui.setOverviewRange", Label: "Overview Time Range", Danger: engine.DangerUI,
		Description: "Sets the time range of the cluster Overview's charts, as the page's range control does. With " +
			"Prometheus the charts show that much history while the page is open; the Metrics Server keeps none. The " +
			"range stays for the session, whichever view is open. Returns the new UI state (overview).",
		Enabled: func(s *engine.Session, _ overviewRangeParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p overviewRangeParams) (any, error) {
			window, err := engine.ParseChartRange(p.Range)
			if err != nil {
				return nil, err
			}
			if window != 0 && (window < engine.MinChartWindow || window > engine.MaxChartWindow) {
				return nil, fmt.Errorf("the Overview's time range is between %s and %s", engine.FormatChartRange(engine.MinChartWindow), engine.FormatChartRange(engine.MaxChartWindow))
			}
			return s.UpdateUI(func(u *engine.UIState) { u.Overview.Range = engine.FormatChartRange(window) }), nil
		},
	})

	add(g, engine.Spec[overviewNodesParams]{
		ID: "ui.setOverviewNodes", Label: "Overview Nodes", Danger: engine.DangerUI,
		Description: "Sets the node group the cluster Overview's charts show, as the page's Master and Worker buttons " +
			"do: master (the control-plane nodes) or worker (every other node). Returns the new UI state (overview).",
		Enabled: func(s *engine.Session, _ overviewNodesParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p overviewNodesParams) (any, error) {
			nodes := strings.TrimSpace(p.Nodes)
			switch nodes {
			case engine.NodesMaster, engine.NodesWorker:
			case "", engine.DefaultChartRange:
				nodes = ""
			default:
				return nil, fmt.Errorf("the Overview shows the master or the worker nodes, not %q", p.Nodes)
			}
			return s.UpdateUI(func(u *engine.UIState) { u.Overview.Nodes = nodes }), nil
		},
	})
}
