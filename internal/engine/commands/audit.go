package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The audit log in the window (G17; D-2: every agent-initiated command is in an audit log the user
// can read; engine/auditlist.go): audit.open shows it in a dock tab, audit.list reads it. Agents may
// read it too (Cogpit's default, inferred: it holds no Secret value, Helm value or proxy password,
// and of what the user did in the window only the switches that are no command, AuditUser).

type auditListParams struct {
	Origin string `json:"origin,omitempty" jsonschema:"whose entries: agents (external agents and the built-in agent), externalAgent, builtInAgent, cli, gui (the user in the window) or deeplink (cogpit:// links); empty or omitted: all"`
	Offset int    `json:"offset,omitempty" jsonschema:"index of the first entry to return, from 0 (the newest); default 0"`
	Limit  int    `json:"limit,omitempty" jsonschema:"entries to return; default 100, at most 1000"`
}

type auditOpenParams struct {
	Origin string `json:"origin,omitempty" jsonschema:"whose entries the tab lists: agents, externalAgent, builtInAgent, cli, gui or deeplink; empty or omitted: all"`
}

func auditEnabled(s *engine.Session) error {
	if !s.HasAuditLog() {
		return engine.ErrNoAuditLog
	}
	return nil
}

func registerAudit(g *registrar) {
	add(g, engine.Spec[auditListParams]{
		ID: "audit.list", Label: "Read Audit Log", Danger: engine.DangerRead,
		Description: "Reads Cogpit's audit log, newest first: one entry per command an agent dispatched (external agents, " +
			"the built-in agent; and the few switches the user made in the window that are no command), with its time, " +
			"origin, command, the context it ran in, its phase (requested before a write runs, done with its outcome), ok " +
			"or the error, the objects its parameters name and the parameters as logged (Secret values, Helm values and " +
			"proxy passwords never are). Filter by origin; a page (offset, limit: default 100, at most 1000) of the log's " +
			"last 4 MiB (partial: older entries are not read). Reads Cogpit's own file, nothing of a cluster.",
		Enabled: func(s *engine.Session, _ auditListParams) error { return auditEnabled(s) },
		Run: func(_ context.Context, s *engine.Session, p auditListParams) (any, error) {
			return s.AuditList(p.Origin, p.Offset, p.Limit)
		},
	})

	add(g, engine.Spec[auditOpenParams]{
		ID: "audit.open", Label: "Audit Log", MenuPath: "Help/Audit Log", Danger: engine.DangerUI,
		Description: "Opens the Audit Log dock tab, or activates it: the audit log's entries newest first (time, origin, " +
			"command, objects, outcome), read-only, filtered by origin (origin; empty: all), as Help › Audit Log does. " +
			"Returns the tab.",
		Enabled: func(s *engine.Session, p auditOpenParams) error {
			if err := auditEnabled(s); err != nil {
				return err
			}
			return engine.CheckAuditOrigin(p.Origin)
		},
		Run: func(_ context.Context, s *engine.Session, p auditOpenParams) (any, error) {
			if err := engine.CheckAuditOrigin(p.Origin); err != nil {
				return nil, err
			}
			tab := engine.DockTab{ID: s.NewDockID(engine.DockAudit), Type: engine.DockAudit, Title: "Audit Log", Audit: engine.AuditTab{Origin: p.Origin}}
			created := true
			s.UpdateUI(func(u *engine.UIState) {
				for i, t := range u.Dock { // one Audit Log: activated, with the filter asked for
					if t.Type == engine.DockAudit {
						u.Dock[i].Audit = tab.Audit
						tab, created = u.Dock[i], false
						break
					}
				}
				if created {
					u.Dock = append(u.Dock, tab)
				}
				u.ShowDockTab(tab.ID) // the one Audit Log, maybe the active tab: the dock opens too (AW9)
			})
			return DockResult{Tab: tab, Created: created}, nil
		},
	})
}
