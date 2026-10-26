package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// StateResult is ui.state's result: the UI state and what the open view shows.
type StateResult struct {
	engine.UIState
	// ViewTitle is the open view's title, "Pods".
	ViewTitle string `json:"viewTitle,omitempty"`
	// Rows is the number of rows the view shows after the namespace filter, the search and the status
	// filter.
	Rows int `json:"rows"`
	// Total is the number of objects of the view's kind before filtering.
	Total int `json:"total"`
	// Synced is false while the view's kind is still being listed.
	Synced bool `json:"synced"`
	// ViewError is what keeps the view from showing objects: a list the cluster refuses, or a
	// custom resource kind the cluster no longer serves.
	ViewError string `json:"viewError,omitempty"`
	// FilterError says why the search filters nothing out: its regular expression does not compile
	// or is too long (engine.FilterError); the view then shows every row.
	FilterError string `json:"filterError,omitempty"`
	// DrawerCharts are the charts of the drawer's Metrics section (a pod's or a node's) with their
	// time ranges and variants, which ui_setMetricsRange and ui_setMetricsVariant set.
	DrawerCharts []ChartRange `json:"drawerCharts,omitempty"`
	// DrawerMetricsProviders is the status line under those charts: each metrics provider (Prometheus,
	// the Metrics Server), whether it answers, its last good answer and last failure, what it serves.
	DrawerMetricsProviders []metrics.ProviderStatus `json:"drawerMetricsProviders,omitempty"`
}

// NavigateResult is ui.navigate's result.
type NavigateResult struct {
	View  string          `json:"view"`
	Ref   model.ObjectRef `json:"ref"`
	Found bool            `json:"found"` // false: the object is not in the cache; the view and drawer show it once it appears
}

type viewParams struct {
	View string `json:"view" jsonschema:"kind key of the view to open: pods, services, or <plural>.<group> for a custom resource kind (gateways.gateway.networking.k8s.io); kinds_list lists them. Or a page: cluster/overview (the cluster Overview), workloads/overview (the Workloads Overview)"`
	// States opens the view filtered, as a dashboard's state link does (statusfilter.go).
	States []string `json:"states,omitempty" jsonschema:"open the view with its status filter set to these states, as ui_setStatusFilter takes them (Not Ready, Health: Degraded); omitted: unfiltered"`
}

type namespaceParams struct {
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"namespaces to show; empty or omitted shows all namespaces"`
}

type filterParams struct {
	Filter    string `json:"filter,omitempty" jsonschema:"search text; every whitespace-separated term must occur in a cell, case-insensitively unless matchCase; with regex, one regular expression (RE2 syntax, at most 1000 bytes) that must match in a cell; empty clears the search"`
	MatchCase *bool  `json:"matchCase,omitempty" jsonschema:"the search field's Aa toggle: true keeps the search's case; omitted: unchanged"`
	Regex     *bool  `json:"regex,omitempty" jsonschema:"the search field's .* toggle: true takes the search text as one regular expression; omitted: unchanged"`
}

type selectParams struct {
	Refs []model.ObjectRef `json:"refs,omitempty" jsonschema:"objects of the open view to select; empty with mode replace clears the selection; exactly one with mode open"`
	Mode string            `json:"mode,omitempty" jsonschema:"how the objects change the selection: replace (the default: they become the selection), add, toggle (each is added, or removed when selected), remove, or open (the selection stays as it is and the detail drawer opens on the one object: a plain click on a row)"`
	All  bool              `json:"all,omitempty" jsonschema:"true: the objects are every row the open view shows (its namespace filter, search and status filter applied), in place of refs; not with mode open"`
	// NoDrawer applies to mode replace; open always opens the drawer, the other modes never change it.
	NoDrawer bool `json:"noDrawer,omitempty" jsonschema:"with mode replace: select without opening the detail drawer on the first object (or, with no objects, without closing it); mode open always opens it, the other modes never change it"`
}

type refParams struct {
	Ref model.ObjectRef `json:"ref" jsonschema:"the object"`
}

func registerUI(g *registrar) {
	add(g, engine.Spec[struct{}]{
		ID: "ui.state", Label: "Show UI State", Danger: engine.DangerRead,
		Description: "Returns what the user sees: the connected context and its connection state (tlsUnchecked when the connection does not check the server's certificate; " +
			"after a failed connect state error, error its cause in a sentence and connectFailure the page the window shows for it: " +
			"the context, when, and the connect's stages kubeconfig, login, dns, tcp, tls and api, each with its result and text; " +
			"welcome is set while the welcome page shows instead), " +
			"who Cogpit is logged in as there (user: username and groups with source apiServer when the API server confirmed them at the connect, " +
			"source kubeconfig with a reason when it did not and username is only the kubeconfig's user entry's name, source lastSeen with lastSeen " +
			"while the cluster is not connected; absent for the demo cluster and a context never confirmed), " +
			"its Kubernetes version (clusterVersion: the API server's version as its connect read it, or its heartbeat once it changed; its minor and the minor's upstream " +
			"support from the Kubernetes release table built into this Cogpit, upstream's dates only: released, maintenanceFrom, endOfLife, " +
			"estimated for a minor newer than the table (tableNewest, tableFetched), state supported, maintenance, endOfLife or unknown as of now, " +
			"daysLeft; view_rows' tips say each node's kubelet support and version skew), the open view (kind key), " +
			"the namespace filter, the search text and its toggles (filterOptions: matchCase, regex; filterError when its " +
			"regular expression is invalid), the status filter (states; view_rows lists the states offered), the sort order, the selected objects (selected: the shown view's; " +
			"every view keeps its own, which comes back when the view is shown again and is not listed meanwhile), the object in the detail drawer and " +
			"the order of its sortable tables by title (drawerSort: a node's or a workload's Pods, ui_sortDrawerTable), " +
			"the tab bar's tabs (tabs and activeTab: each tab's id, context, view, title, preview or fixed, its own " +
			"filter and selection, and its history), the dock tabs, how many rows the view shows of how many objects, for a pod's or node's drawer the time " +
			"range and variant of each metrics chart (drawerCharts) and the metrics providers' status line " +
			"(drawerMetricsProviders: Prometheus and the Metrics Server, answering or not, what each serves), " +
			"the hotbar of pinned clusters as hotbar_list returns it, one list of clusters top to bottom (each tile's displayName is the name the window shows: the display name set, else the part of the context's name after its last @; commands take the context), and timeFormat, the Go layouts of a date and a time " +
			"of day that the drawn texts (view_rows, resources_list, object_get's sections) write absolute times in, in the " +
			"Preferences' time zone; the window's layout (navigatorHidden, dockOpen, chat) and update, the newer release " +
			"the updater holds for the user (available, version). Use it to learn which object the user means.",
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			u := s.UIState()
			u.ClusterVersion = s.ClusterVersionNow(u.ClusterVersion) // its support as of now (k8sversion.go, D-91)
			res := StateResult{UIState: u, FilterError: engine.FilterError(u.Filter, u.FilterOptions)}
			if u.Hotbar != nil { // what each tile shows: label, connection, missing (hotbar.go)
				if v, err := s.HotbarView(); err == nil {
					res.Hotbar = &v
				}
			}
			if t := s.Table(u.View); t != nil {
				res.ViewTitle, res.Rows, res.Total, res.Synced, res.ViewError = t.Title, len(t.Rows), t.Total, t.Synced, t.Error
			} else if b := s.Backend(); b != nil {
				if p, ok := b.Profile(u.View); ok {
					res.ViewTitle = p.Title
				}
			}
			if u.Drawer != nil {
				if m, err := s.ObjectMetrics(*u.Drawer); err == nil && m != nil {
					res.DrawerCharts, res.DrawerMetricsProviders = chartRanges(m), m.Providers
				}
			}
			return res, nil
		},
	})

	add(g, engine.Spec[viewParams]{
		ID: "ui.openView", Label: "Open View", MenuPath: "View/Open View", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Opens the list view of a kind (pods, services, ...) or a page (cluster/overview, the cluster " +
			"Overview; workloads/overview, the Workloads Overview), as clicking it in the sidebar does: in the cluster's " +
			"preview tab, which the next view of the cluster opened this way replaces, or by activating a fixed tab of the cluster " +
			"that shows the view (ui_tab_open opens fixed tabs). The view's selection (the checked rows) comes back as " +
			"the tab last left it, without objects deleted since; every view keeps its own, so the selection is always " +
			"the shown view's. Clears the detail drawer, the search text and the status filter, or sets the status " +
			"filter to states; keeps the namespace filter. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ viewParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p viewParams) (any, error) {
			view, err := viewKey(s, p.View)
			if err != nil {
				return nil, err
			}
			if states := engine.CleanStates(p.States); len(states) > 0 {
				return s.OpenTabFiltered(view, states), nil
			}
			return s.OpenTab(view, false), nil
		},
	})

	add(g, engine.Spec[namespaceParams]{
		ID: "ui.setNamespace", Label: "Select Namespaces", MenuPath: "View/Namespaces", Danger: engine.DangerUI,
		Description: "Sets the namespace filter of the namespaced views; an empty list shows all namespaces. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p namespaceParams) (any, error) {
			var ns []string
			for _, n := range p.Namespaces {
				n = strings.TrimSpace(n)
				if n != "" && !slices.Contains(ns, n) {
					ns = append(ns, n)
				}
			}
			return s.UpdateUI(func(u *engine.UIState) { u.Namespaces = ns }), nil
		},
	})

	add(g, engine.Spec[filterParams]{
		ID: "ui.setFilter", Label: "Search", MenuPath: "View/Search", Shortcut: "Cmd+F", Danger: engine.DangerUI,
		Description: "Sets the search text of the open view, and its toggles as the search field's Aa and .* do: a row is " +
			"shown when every whitespace-separated term occurs in one of its cells, ignoring case unless matchCase; with " +
			"regex the text is one regular expression (RE2 syntax, at most 1000 bytes) that must match in one of its cells. " +
			"A toggle left out stays as it is; the toggles stay when the view changes. An empty text clears the search. A " +
			"regular expression that does not compile filters nothing out and ui_state reports it as filterError. " +
			"Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p filterParams) (any, error) {
			return s.UpdateUI(func(u *engine.UIState) {
				u.Filter = p.Filter
				if p.MatchCase != nil {
					u.FilterOptions.MatchCase = *p.MatchCase
				}
				if p.Regex != nil {
					u.FilterOptions.Regex = *p.Regex
				}
			}), nil
		},
	})

	add(g, engine.Spec[engine.SortSpec]{
		ID: "ui.sort", Label: "Sort", Danger: engine.DangerUI,
		Description: "Sorts the open view by a column id (name, namespace, age, ... as kinds_list reports them), " +
			"ascending unless desc is true. Empty cells sort last. The view remembers its sort order. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ engine.SortSpec) error { _, err := openView(s); return err },
		Run: func(_ context.Context, s *engine.Session, p engine.SortSpec) (any, error) {
			view, prof, err := openViewKey(s)
			if err != nil {
				return nil, err
			}
			prof = s.WithViewColumns(prof) // an integration's column too (D-79)
			if prof.ColumnIndex(p.Column) < 0 {
				return nil, fmt.Errorf("the %s view has no column %q; columns: %s", view, p.Column, columnIDs(prof))
			}
			applied := false
			st := s.UpdateUI(func(u *engine.UIState) {
				if u.View == view {
					u.Sort, applied = p, true
				}
			})
			if !applied {
				return nil, errors.New("the open view changed; sort again")
			}
			return st, nil
		},
	})

	add(g, engine.Spec[selectParams]{
		ID: "ui.select", Label: "Select", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Selects objects of the open view, as a row's checkbox, ⌘-click, ⇧-click or the header's checkbox " +
			"does, or opens one object's detail drawer as a plain click on its row does. mode replace (the default) makes " +
			"them the selection and, unless noDrawer is true, opens the detail drawer on the first one; an empty list " +
			"clears the selection (and closes the drawer unless noDrawer). mode add, toggle and remove change the " +
			"selection and leave the drawer as it is. mode open takes one object and opens the drawer on it, leaving the " +
			"selection (the checked rows) as it is: the click on a row. all: true takes " +
			"every row the view shows (namespace filter, search and status filter applied) in place of refs. Actions on the selection " +
			"take its objects (object_deleteMany). On the cluster Overview the objects are its issues' (any kind). To " +
			"select an object of another kind or one hidden by the filters, show it with ui_navigate first. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ selectParams) error { _, err := openView(s); return err },
		Run:     selectObjects,
	})

	add(g, engine.Spec[struct{}]{
		ID: "ui.closeDrawer", Label: "Close Details", MenuPath: "View/Close Details", Danger: engine.DangerUI,
		Description: "Closes the detail drawer. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.UpdateUI(func(u *engine.UIState) { u.Drawer = nil }), nil
		},
	})

	add(g, engine.Spec[refParams]{
		ID: "ui.navigate", Label: "Go to Object", MenuPath: "View/Go to Object", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Shows one object to the user: opens its kind's view, makes sure the namespace filter, the search " +
			"text and the status filter do not hide it (resetting them if they do) and opens its detail drawer. It leaves " +
			"the selection (the checked rows) as it is in the same view; a view it changes to shows its own selection " +
			"as the tab last left it, as ui_openView does; to check the object too, call ui_select afterwards. The place it " +
			"leaves (view, filters, drawer) goes into the active tab's history, which ui_tab_back brings back. Use it after " +
			"resources_find. An object of another connected cluster shows that cluster in the window first, as cluster_connect " +
			"of a connected context does (no request, nothing connected anew); one of a context that is not connected is an " +
			"error. Reads only the cache. Returns {view, ref, found}; found is false when the object is not in the cache.",
		Enabled: func(s *engine.Session, p refParams) error { _, err := connectedOn(s, p.Ref.Context); return err },
		Run:     navigate,
	})
}

// navigate implements ui.navigate. It decides from the cache, not from the view's snapshot, so it
// is correct before the view's first snapshot exists.
func navigate(ctx context.Context, s *engine.Session, p refParams) (any, error) {
	if c := p.Ref.Context; c != "" && c != s.UIState().Context {
		if err := s.Show(c); err != nil { // another connected cluster: shown first (D-69), never connected
			return nil, err
		}
	}
	ref, prof, err := s.ResolveRef(p.Ref)
	if err != nil {
		return nil, err
	}
	var row *model.Row
	obj, err := s.Object(ctx, ref)
	switch {
	case err == nil:
		r := model.BuildRow(prof, ref.Context, obj)
		if cur := s.UIState(); len(prof.Needs) > 0 && cur.View == prof.Key && cur.Filter != "" {
			// The view's rows show related objects (a claim's pods) and its filter may match only
			// those: check the row the view shows. The open view holds the related kinds, so this
			// waits for nothing.
			if b := s.Backend(); b != nil && b.Context == ref.Context {
				r = s.BuildRows(ctx, b, prof, []*unstructured.Unstructured{obj})[0]
			}
		}
		row = &r
	case !errors.Is(err, engine.ErrNotFound):
		return nil, err
	}
	s.NoteViewTitle(prof.Key, prof.Title) // the active tab's title when the view changes (engine/tabs.go)
	sort := s.ViewSort(prof.Key)
	st := s.UpdateUI(func(u *engine.UIState) {
		if u.View != prof.Key {
			// The checked rows are the open view's: the tab brings back the new view's own (N111,
			// engine/selectionmemory.go), as ui.openView does.
			u.View, u.Sort, u.Filter = prof.Key, sort, ""
		}
		if prof.Namespaced && len(u.Namespaces) > 0 && !slices.Contains(u.Namespaces, ref.Namespace) {
			u.Namespaces = nil
		}
		if u.Filter != "" && (row == nil || !engine.MatchesFilter(*row, u.Filter, u.FilterOptions)) {
			u.Filter = ""
		}
		if len(u.States) > 0 && (row == nil || !engine.MatchesStates(prof, *row, u.States)) {
			u.SetStates(nil) // the status filter hides it (statusfilter.go)
		}
		// Selecting and opening a drawer are separate actions (owner, 2026-10-15): the checked rows
		// change only through ui.select.
		d := ref
		u.Drawer = &d
		u.MarkStep() // back comes back here, another object of the same view too (engine/tabs.go)
	})
	st = s.DropGoneSelected(ctx, st) // a view shown again: its selection without the objects gone meanwhile
	return NavigateResult{View: st.View, Ref: ref, Found: row != nil}, nil
}

// openView returns the profile of the open view's rows, or why there is none.
func openView(s *engine.Session) (*model.KindProfile, error) {
	_, p, err := openViewKey(s)
	return p, err
}

// openViewKey returns the open view's key and the profile of its rows: the kind's, or a page's
// table profile (the Workloads Overview's events).
func openViewKey(s *engine.Session) (string, *model.KindProfile, error) {
	if _, err := connected(s); err != nil {
		return "", nil, err
	}
	view := s.UIState().View
	p, ok := s.ViewProfile(view)
	if !ok {
		return "", nil, errors.New("no list view is open")
	}
	return view, p, nil
}

func columnIDs(p *model.KindProfile) string {
	ids := make([]string, 0, len(p.Columns))
	for _, c := range p.Columns {
		ids = append(ids, c.ID)
	}
	return strings.Join(ids, ", ")
}
