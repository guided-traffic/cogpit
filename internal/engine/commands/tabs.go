package commands

import (
	"context"
	"errors"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The tab bar's commands (engine/tabs.go, ui-parity › Layout and Keyboard shortcuts). Each is a UI
// command: it changes what the window shows, never a cluster, so agents run it freely (D-2);
// selecting a tab of another cluster connects to it as cluster.connect does.

type tabOpenParams struct {
	View  string `json:"view" jsonschema:"kind key of the view to open (pods, services, <plural>.<group> for a custom resource kind; kinds_list lists them) or a page key (workloads/overview)"`
	Fixed bool   `json:"fixed,omitempty" jsonschema:"open a fixed tab (the navigator's double click or Shift+Enter); without it the view opens in the preview tab, which the next view opened in preview replaces"`
}

type tabIDParams struct {
	ID string `json:"id,omitempty" jsonschema:"tab id as ui_state lists it in tabs (tab-1); empty means the active tab"`
}

type tabSelectParams struct {
	ID    string `json:"id,omitempty" jsonschema:"tab id as ui_state lists it in tabs (tab-1)"`
	Index int    `json:"index,omitempty" jsonschema:"the tab's place in the tab bar, from 1 (Cmd+1 is the first)"`
	Last  bool   `json:"last,omitempty" jsonschema:"the last tab (Cmd+9)"`
}

// tabsOpen is the Enabled check of the commands that need a tab.
func tabsOpen(s *engine.Session) error {
	if len(s.UIState().Tabs) == 0 {
		return engine.ErrNoTab
	}
	return nil
}

func registerTabs(g *registrar) {
	add(g, engine.Spec[tabOpenParams]{
		ID: "ui.tab.open", Label: "Open in Tab", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Opens a view of the shown cluster in a tab of the tab bar: in the preview tab (which the next " +
			"view opened in preview replaces; ui_openView does the same), or with fixed in a new fixed tab that stays until " +
			"closed. A fixed tab of the cluster already showing the view is activated instead; asked for fixed, a preview " +
			"tab showing the view becomes fixed. A new tab starts with the namespace filter of the tab before it. Returns " +
			"the new UI state; its tabs list every tab with its id, context, view, title and own state.",
		Enabled: func(s *engine.Session, _ tabOpenParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p tabOpenParams) (any, error) {
			view, err := viewKey(s, p.View)
			if err != nil {
				return nil, err
			}
			return s.OpenTab(view, p.Fixed), nil
		},
	})

	add(g, engine.Spec[tabIDParams]{
		ID: "ui.tab.pin", Label: "Keep Tab Open", MenuPath: "View/Keep Tab Open", Danger: engine.DangerUI,
		Description: "Makes the preview tab (or the tab named by id) a fixed tab, as a double click on its title does: " +
			"the next view opened in preview then opens in a new preview tab. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ tabIDParams) error { return tabsOpen(s) },
		Run: func(_ context.Context, s *engine.Session, p tabIDParams) (any, error) {
			return s.PinTab(p.ID)
		},
	})

	add(g, engine.Spec[tabIDParams]{
		ID: "ui.tab.close", Label: "Close Tab", MenuPath: "View/Close Tab", Shortcut: "Cmd+W", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Closes the active tab (or the tab named by id). Closing the active tab shows the nearest tab of the " +
			"same cluster, else an empty workspace. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ tabIDParams) error { return tabsOpen(s) },
		Run: func(_ context.Context, s *engine.Session, p tabIDParams) (any, error) {
			return s.CloseTab(p.ID)
		},
	})

	add(g, engine.Spec[tabSelectParams]{
		ID: "ui.tab.select", Label: "Go to Tab", Shortcut: "Cmd+1–9", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Activates a tab, named by id, by its place (index, from 1) or as the last one: its view comes back " +
			"with its own namespace filter, search, sort, selection and drawer. A tab of another cluster connects to that " +
			"cluster first (Cogpit holds one connection; connecting only reads), as cluster_connect does. Returns the new UI state, " +
			"also when that connect failed (connection says why) or goes on in the background because another cluster was shown " +
			"while it waited; a connect cancelled meanwhile (cluster_disconnect) answers a cancelled error.",
		Enabled: func(s *engine.Session, _ tabSelectParams) error { return tabsOpen(s) },
		Run: func(ctx context.Context, s *engine.Session, p tabSelectParams) (any, error) {
			switch {
			case p.ID != "":
				return s.SelectTab(ctx, p.ID)
			case p.Index > 0 || p.Last:
				return s.SelectTabAt(ctx, p.Index, p.Last)
			}
			return nil, errors.New("name the tab: id, index or last")
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "ui.tab.next", Label: "Next Tab", MenuPath: "View/Next Tab", Shortcut: "Cmd+Alt+Right", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Activates the tab right of the active one, the first after the last, as ui_tab_select does. Returns " +
			"the new UI state.",
		Enabled: func(s *engine.Session, _ struct{}) error { return tabsOpen(s) },
		Run: func(ctx context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.CycleTab(ctx, 1)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "ui.tab.previous", Label: "Previous Tab", MenuPath: "View/Previous Tab", Shortcut: "Cmd+Alt+Left", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Activates the tab left of the active one, the last before the first, as ui_tab_select does. Returns " +
			"the new UI state.",
		Enabled: func(s *engine.Session, _ struct{}) error { return tabsOpen(s) },
		Run: func(ctx context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.CycleTab(ctx, -1)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "ui.tab.back", Label: "Go Back", MenuPath: "View/Go Back", Shortcut: "Ctrl+-", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Goes back to the view the active tab showed before (its history: views opened in the preview tab, " +
			"ui_navigate to another kind). The view opens as ui_openView opens one. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ struct{}) error { return s.CheckTabHistory(true) },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.TabHistory(true)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "ui.tab.forward", Label: "Go Forward", MenuPath: "View/Go Forward", Shortcut: "Ctrl+Shift+-", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Goes forward again in the active tab's history, after ui_tab_back. Returns the new UI state.",
		Enabled:     func(s *engine.Session, _ struct{}) error { return s.CheckTabHistory(false) },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.TabHistory(false)
		},
	})
}

// viewKey checks a view key against the shown cluster and returns its canonical form: a page
// key, or the key of a kind the cluster serves.
func viewKey(s *engine.Session, view string) (string, error) {
	if _, page := engine.PageTitle(view); page {
		return view, nil
	}
	_, prof, err := s.Kind(view)
	if err != nil {
		return "", err
	}
	s.NoteViewTitle(prof.Key, prof.Title)
	return prof.Key, nil
}
