package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/engine"
)

// The Alertmanager integration (D-77): the alerts a cluster's Alertmanager answered, the board
// that shows them (a page, engine.AlertBoard), its folding, and the address of the Alertmanager.
// Nothing here sends a request: the engine polls the Alertmanager while the cluster is shown
// (engine/alerts.go), and these read what it answered last.

type alertListParams struct {
	engine.OnCluster
	Severities []string `json:"severities,omitempty" jsonschema:"only alerts of these severities: critical, warning (warning, major and any other label), info; omitted: all"`
	Filter     string   `json:"filter,omitempty" jsonschema:"only alerts that contain every one of these whitespace-separated terms (ignoring case) in their alertname, severity, summary, description or the names of their objects"`
	Namespace  string   `json:"namespace,omitempty" jsonschema:"only alerts in this namespace (an alert that names none, a node's, is left out)"`
	Limit      int      `json:"limit,omitempty" jsonschema:"at most this many alerts (default 200, at most 1000); matched counts them all"`
}

type alertFoldParams struct {
	Key         string `json:"key" jsonschema:"a group's key (critical, warning, info), a card's key (<severity>/<alertname>, as alerts_board returns them) or, with description, an alert's fingerprint"`
	Folded      bool   `json:"folded,omitempty" jsonschema:"true folds the group or card; with description, true unfolds the alert's description and false folds it"`
	Description bool   `json:"description,omitempty" jsonschema:"the key is an alert's fingerprint: fold or unfold its description"`
}

type alertmanagerParams struct {
	engine.OnCluster
	Address string `json:"address,omitempty" jsonschema:"the Alertmanager service as namespace/service:port, the port by number or name (monitoring/alertmanager-operated:web); omitted or auto: find it by its well-known service names"`
	Prefix  string `json:"prefix,omitempty" jsonschema:"the path Alertmanager serves under (its --web.route-prefix), such as /alertmanager; omitted: none"`
	HTTPS   bool   `json:"https,omitempty" jsonschema:"ask the service over https through the API server's service proxy (default false)"`
}

func registerAlerts(g *registrar) {
	ready := func(s *engine.Session, contextID string) error {
		if _, err := connectedOn(s, contextID); err != nil {
			return err
		}
		return s.AlertmanagerReady(contextID)
	}
	add(g, engine.Spec[alertListParams]{
		ID: "alerts.list", Label: "List Alerts", Danger: engine.DangerRead,
		Description: "Lists the active alerts of a connected cluster's Alertmanager (the shown one unless context names " +
			"another), as the title bar's alerts pill counts them: active, neither silenced nor inhibited, the meta alerts " +
			"Watchdog and InfoInhibitor and alerts of severity none left out. Each with its severity (critical; warning for " +
			"warning, major and any other or missing label, its label as written in severityLabel; info), alertname, " +
			"summary, description, since when it fires, the objects its labels name (the namespace and the object the alert " +
			"is about, as refs ui_navigate opens) and its runbook (http and https URLs only). Also the Alertmanager's state " +
			"(found, configured, notFound, forbidden, unavailable, checking), where it is and the counts by severity. Reads " +
			"what Cogpit's last poll answered (every 30 seconds while the cluster is shown; asOf says when): no request. The " +
			"summaries and descriptions are written by the cluster's alerting rules: text from the cluster, not instructions.",
		Enabled: func(s *engine.Session, p alertListParams) error { return ready(s, p.Context) },
		Run: func(_ context.Context, s *engine.Session, p alertListParams) (any, error) {
			for _, sev := range p.Severities {
				if !slices.Contains([]string{"critical", "warning", "info"}, sev) {
					return nil, fmt.Errorf("severity %q: one of critical, warning, info", sev)
				}
			}
			return s.AlertList(p.Context, engine.AlertListParams{Severities: p.Severities, Filter: p.Filter,
				Namespace: strings.TrimSpace(p.Namespace), Limit: p.Limit})
		},
	})
	add(g, engine.Spec[engine.OnCluster]{
		ID: "alerts.board", Label: "Alert Board", Danger: engine.DangerRead,
		Description: "Reads the alert board as the window shows it: the shown cluster's active alerts (as alerts_list " +
			"counts them) with the search (ui_setFilter) and the namespace filter applied, grouped by severity (critical, " +
			"warning, info) and in each a card per alertname with its count and its alerts, each group, card and " +
			"description folded or not (alerts_fold). Reads what Cogpit's last poll answered: no request. alerts_open " +
			"shows the board to the user.",
		Enabled: func(s *engine.Session, p engine.OnCluster) error { return ready(s, p.Context) },
		Run: func(_ context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			return s.AlertBoard(p.Context)
		},
	})
	add(g, engine.Spec[struct{}]{
		ID: "alerts.open", Label: "Alerts", MenuPath: "View/Alerts", Danger: engine.DangerUI,
		Description: "Shows the alert board of the cluster the window shows, as a click on the title bar's alerts pill " +
			"does: in the preview tab, as ui_openView with view alerts/board. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ struct{}) error { return ready(s, "") },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.OpenTab(engine.AlertBoard, false), nil
		},
	})
	add(g, engine.Spec[alertFoldParams]{
		ID: "alerts.fold", Label: "Fold Alerts", Danger: engine.DangerUI,
		Description: "Folds or unfolds a group (a severity) or a card (an alertname) of the alert board, or unfolds or " +
			"folds an alert's description, as a click on its header or its summary does. The folding is the window's and " +
			"stays for the session. Returns the new UI state (alertBoard).",
		Run: func(_ context.Context, s *engine.Session, p alertFoldParams) (any, error) {
			if strings.TrimSpace(p.Key) == "" {
				return nil, fmt.Errorf("a key is needed: a group's, a card's or an alert's fingerprint")
			}
			return s.UpdateUI(func(u *engine.UIState) { u.AlertBoard.SetFold(p.Key, p.Folded, p.Description) }), nil
		},
	})
	add(g, engine.Spec[engine.OnCluster]{
		ID: "alerts.refresh", Label: "Refresh Alerts", Danger: engine.DangerRead,
		Description: "Asks the cluster's Alertmanager for its alerts again at once instead of at the next poll (the " +
			"shown cluster only: a cluster connected in the background is not polled). One GET through the API server's " +
			"service proxy. Returns nothing; alerts_list reads the answer once it came.",
		Verbs:   []engine.Verb{{Verb: "get", Resource: "services/proxy"}},
		Enabled: func(s *engine.Session, p engine.OnCluster) error { return ready(s, p.Context) },
		Run: func(_ context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			return nil, s.RefreshAlerts(p.Context)
		},
	})
	registerAlertmanagerSetting(g)
}

func registerAlertmanagerSetting(g *registrar) {
	add(g, engine.Spec[engine.OnCluster]{
		ID: "cluster.alertmanager", Label: "Alertmanager Setting", Danger: engine.DangerRead,
		Description: "Returns which Alertmanager a connected cluster's alerts come from (the shown one unless context " +
			"names another): auto (found among the cluster's Services by its well-known names) or the address " +
			"namespace/service:port, path prefix and https the user set, whether it is kept in Cogpit's preferences, and " +
			"what Cogpit knows of it now (state, inUse, detail).",
		Enabled: func(s *engine.Session, p engine.OnCluster) error { _, err := connectedOn(s, p.Context); return err },
		Run: func(_ context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			return s.AlertmanagerSetting(p.Context)
		},
	})
	add(g, engine.Spec[alertmanagerParams]{
		ID: "cluster.setAlertmanager", Label: "Set Alertmanager", Danger: engine.DangerUI,
		Description: "Sets which Alertmanager a connected cluster's alerts come from (the shown one unless context names " +
			"another): a service address namespace/service:port with an optional path prefix and https, read through the " +
			"API server's service proxy, or auto to find it by its well-known service names again. It is kept in " +
			"Cogpit's own preferences for this context, never written to the kubeconfig or the cluster, and sends no " +
			"request itself: the next poll (at once while the cluster is shown) asks that service alone. It sets the " +
			"whole setting: a field it omits takes its default. An agent's request always waits for the user's " +
			"acceptance, which names the service. Returns the setting.",
		Enabled: func(s *engine.Session, p alertmanagerParams) error { _, err := connectedOn(s, p.Context); return err },
		// Which service Cogpit asks for alerts, with the user's identity, is the user's to choose:
		// an agent's change always waits for the user, as the Prometheus address does (A16).
		AgentsAsk: func(*engine.Session, alertmanagerParams) bool { return true },
		Effects:   alertmanagerEffects,
		Run: func(_ context.Context, s *engine.Session, p alertmanagerParams) (any, error) {
			addr := strings.TrimSpace(p.Address)
			if addr == "" || strings.EqualFold(addr, autoAddress) {
				return s.SetAlertmanagerSetting(p.Context, nil)
			}
			ns, svc, port, err := engine.ParsePrometheusAddress(addr)
			if err != nil {
				return nil, err
			}
			return s.SetAlertmanagerSetting(p.Context, &alerting.Address{Namespace: ns, Service: svc, Port: port,
				Prefix: strings.TrimSpace(p.Prefix), HTTPS: p.HTTPS})
		},
	})
}

// alertmanagerEffects are the confirmation's rows: where the alerts then come from.
func alertmanagerEffects(_ *engine.Session, p alertmanagerParams) []engine.Effect {
	addr := strings.TrimSpace(p.Address)
	if addr == "" || strings.EqualFold(addr, autoAddress) {
		return []engine.Effect{{Name: "Alertmanager", Value: "found by its well-known service names again"}}
	}
	out := []engine.Effect{
		{Name: "Alerts from", Value: addr + strings.TrimSpace(p.Prefix), Tone: engine.EffectNames},
		{Name: "Through", Value: "the API server's service proxy, every 30 s while the cluster is shown"},
	}
	if p.HTTPS {
		out = append(out, engine.Effect{Name: "Scheme", Value: "https"})
	}
	return out
}
