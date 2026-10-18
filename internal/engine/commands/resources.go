package commands

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Result limits of resources.list and resources.find.
const (
	defaultLimit = 100
	maxLimit     = 1000
)

// Match modes of resources.find.
const (
	matchExact    = "exact"
	matchContains = "contains"
	matchPrefix   = "prefix"
)

// KindInfo is one entry of kinds.list.
type KindInfo struct {
	Kind       string         `json:"kind"`    // kind key, "pods"
	Title      string         `json:"title"`   // "Pods"
	APIKind    string         `json:"apiKind"` // "Pod"
	Group      string         `json:"group,omitempty"`
	Version    string         `json:"version"`
	Namespaced bool           `json:"namespaced"`
	Columns    []model.Column `json:"columns"`
	Fields     []string       `json:"fields"` // what resources_find searches by
	// Listable says whether the user may list the kind (access reviews); absent while unknown,
	// and then the sidebar shows the kind.
	Listable *bool `json:"listable,omitempty"`
	// Local marks a kind whose objects Cogpit makes itself (port forwards), Virtual one made from
	// other cached objects (Helm releases): neither is an API resource that object_create,
	// object_edit or object_delete can write; their own commands change them.
	Local   bool `json:"local,omitempty"`
	Virtual bool `json:"virtual,omitempty"`
	// Actions are the tools of an object's row menu and drawer, in Lens's order (an object's state
	// can hide or grey one: Uncordon on a node that is not cordoned); SelectionActions those of a
	// selection's menu; Commands the kind's other tools (its create dialog). kindslist.go.
	Actions          []string `json:"actions,omitempty"`
	SelectionActions []string `json:"selectionActions,omitempty"`
	Commands         []string `json:"commands,omitempty"`
	// Cached is set while Cogpit's cache holds the kind: reading it costs no request, and
	// resources_find without a kind searches it.
	Cached bool `json:"cached,omitempty"`
	// NotServed marks a built-in kind the cluster does not serve (kinds_list with all).
	NotServed bool `json:"notServed,omitempty"`
}

// ListResult is resources.list's result.
type ListResult struct {
	Kind     string    `json:"kind"`
	Total    int       `json:"total"`    // objects that pass the namespace, search and status filters
	Returned int       `json:"returned"` // rows in this result, at most the limit
	Rows     []ListRow `json:"rows"`
}

// ListRow is one object of resources.list: its reference, status and cells by column title.
type ListRow struct {
	Ref    model.ObjectRef   `json:"ref"`
	Status model.Status      `json:"status,omitempty"`
	Cells  map[string]string `json:"cells"`
	// Tips are the tooltips of the cells that have one, by column title: a kubelet's upstream
	// support and version skew in the Nodes' Version (D-91).
	Tips map[string]string `json:"tips,omitempty"`
}

// FindResult is resources.find's result.
type FindResult struct {
	Count     int               `json:"count"` // every match, also those beyond the limit
	Matches   []FindMatch       `json:"matches"`
	Truncated bool              `json:"truncated,omitempty"`
	Skipped   map[string]string `json:"skipped,omitempty"` // kinds that could not be searched, with the reason
}

// FindMatch is one object found by resources.find, with the field's values that matched.
type FindMatch struct {
	Ref    model.ObjectRef `json:"ref"`
	Field  string          `json:"field"`
	Values []string        `json:"values"`
}

type listParams struct {
	engine.OnCluster
	Kind       string   `json:"kind" jsonschema:"kind key: the plural for built-in kinds (pods), <plural>.<group> for custom resources (orders.acme.cert-manager.io); kinds_list lists them"`
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"namespaces to list; empty lists all"`
	Filter     string   `json:"filter,omitempty" jsonschema:"search text as in the view's search box: every term must occur in a cell, case-insensitively"`
	States     []string `json:"states,omitempty" jsonschema:"status filter as in the view's states menu (ui_setStatusFilter): only objects whose row carries one of these states per state column named (Running, Not Ready, Health: Degraded; view_rows lists a view's in stateChoices)"`
	Limit      int      `json:"limit,omitempty" jsonschema:"maximum rows to return; default 100, at most 1000"`
}

type findParams struct {
	engine.OnCluster
	Kind  string `json:"kind,omitempty" jsonschema:"kind key to search (pods, or <plural>.<group> for a custom resource); empty searches every built-in kind that has the field and the custom resource kinds already in Cogpit's cache"`
	Field string `json:"field" jsonschema:"field to match: name, namespace, label (key=value) or a kind's own field such as clusterIP, podIP or node; kinds_list lists them"`
	Value string `json:"value" jsonschema:"value to look for"`
	Match string `json:"match,omitempty" jsonschema:"exact (the default), contains or prefix; all case-insensitive"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum matches to return; default 100, at most 1000"`
}

type objectParams struct {
	Ref     model.ObjectRef `json:"ref" jsonschema:"the object"`
	NoYAML  bool            `json:"noYaml,omitempty" jsonschema:"leave out the YAML"`
	Related bool            `json:"related,omitempty" jsonschema:"also list the related objects as refs (related, relatedTotal): what the drawer links to (owner, node, a node's pods, a claim's pods, a Service's endpoint slices, ...) and the objects a workload controls (replica sets, jobs, pods); at most 200"`
}

// objectWithRelated is object.get's result with related: the drawer model and its related objects
// (engine.Related, relatedrefs.go).
type objectWithRelated struct {
	*engine.DetailModel
	engine.RelatedRefs
}

func registerResources(g *registrar) {
	add(g, engine.Spec[kindsListParams]{
		ID: "kinds.list", Label: "List Kinds", Danger: engine.DangerRead,
		Description: "Lists the kinds a connected cluster serves (the shown one unless context names another), as Cogpit shows them: kind key (what other tools take as " +
			"kind), title, Kubernetes kind, group, version, whether it is namespaced, its table columns, and the fields " +
			"resources_find can search by. Built-in kinds come first and keep their plural as the key (pods); then the kinds " +
			"of the cluster's custom resource definitions, keyed <plural>.<group> (gateways.gateway.networking.k8s.io), " +
			"titled by their Kind, with their printer columns. Custom kinds follow the definitions while connected: they " +
			"appear shortly after connecting and change when a definition does. listable says whether the user may list " +
			"the kind (in every namespace or one they can see), from Cogpit's access reviews; it is absent until they answer. " +
			"local marks Cogpit's own kinds (port forwards), virtual the kinds made from other objects (Helm releases): " +
			"no API resource to create, edit or delete; their own commands change them. actions are the tools of an object's " +
			"row menu and drawer in Lens's order (an object's state can hide or grey one), selectionActions those of a " +
			"selection's menu, commands the kind's other tools (its create dialog); cached says whether Cogpit's cache holds " +
			"the kind now (no request to read it; resources_find without kind searches it). With all, the built-in kinds the " +
			"cluster does not serve follow, marked notServed.",
		Enabled: func(s *engine.Session, p kindsListParams) error { _, err := connectedOn(s, p.Context); return err },
		Run: func(_ context.Context, s *engine.Session, p kindsListParams) (any, error) {
			b, err := connectedOn(s, p.Context)
			if err != nil {
				return nil, err
			}
			return listKinds(s, b, p), nil // kindslist.go
		},
	})

	add(g, engine.Spec[listParams]{
		ID: "resources.list", Label: "List Resources", Danger: engine.DangerRead, Verbs: cacheReads,
		Description: "Lists objects of one kind from Cogpit's cache, as table rows: reference, status and the cells by column " +
			"title, and tips, the tooltips of the cells that have one (a node's kubelet: its upstream support and version skew). Optional namespaces, search text and states filter as in the view; rows are sorted by name. total counts every " +
			"row that passes the filters, returned the rows in the result (limit, default 100, at most 1000). Makes no API request once the kind is cached.",
		Enabled: func(s *engine.Session, p listParams) error { _, err := connectedOn(s, p.Context); return err },
		Run:     listResources,
	})

	add(g, engine.Spec[findParams]{
		ID: "resources.find", Label: "Find Resources", Danger: engine.DangerRead, Verbs: cacheReads,
		Description: "Finds objects by a field value in Cogpit's cache, e.g. the Service with clusterIP 10.96.0.1 " +
			"({kind: services, field: clusterIP, value: 10.96.0.1}) or the pods on a node. Without kind it searches every " +
			"built-in kind that has the field, and the custom resource kinds, Secrets and Helm releases only if already cached (a view or a " +
			"lookup used them); name such a kind to search it. Returns {count, matches: [{ref, field, values}]}; pass a match's ref to " +
			"ui_navigate to show it to the user.",
		Enabled: func(s *engine.Session, p findParams) error { _, err := connectedOn(s, p.Context); return err },
		Run:     findResources,
	})

	add(g, engine.Spec[objectParams]{
		ID: "object.get", Label: "Get Object", Danger: engine.DangerRead, Verbs: cacheReads,
		Description: "Returns what the detail drawer shows for one object, from Cogpit's cache: title, status, the sections " +
			"(Properties first: the metadata and the kind's state; then the kind's own sections; values that name objects " +
			"carry a link ref), its events (newest first, at most 50), its container names for pods, and its YAML " +
			"(managed fields dropped, Secret values redacted) unless noYaml. missing is true when the object is not in the cache. " +
			"For a Secret, certificates holds the decoded fields of every certificate its keys hold (subject, issuer, serial, " +
			"validity in UTC with expiresInDays and expired, names, key, usages, fingerprint), never a value, the PEM text or a private key. " +
			"With related, also the related objects as refs with their relation, from the cache: what the drawer links to " +
			"(Controlled By, a pod's node, a node's pods and devices, a claim's pods, a Service's endpoint slices, ...) and the " +
			"objects a workload controls (a Deployment's replica sets and pods, a Cron Job's jobs and pods, the pods of the " +
			"other workloads); at most 200, relatedTotal counts them all." + privateReads + " For it a Secret's Data section " +
			"holds each value as the drawer's Show reveals it, and its YAML the values as an Edit tab holds them.",
		Enabled: func(s *engine.Session, p objectParams) error { _, err := connectedOn(s, p.Ref.Context); return err },
		Run: func(ctx context.Context, s *engine.Session, p objectParams) (any, error) {
			m, err := s.DetailWait(ctx, p.Ref)
			if err != nil {
				return nil, err
			}
			if p.NoYAML {
				m.YAML = ""
			}
			if p.Related {
				return objectWithRelated{DetailModel: m, RelatedRefs: s.Related(ctx, m)}, nil
			}
			return m, nil
		},
	})
}

// fieldNames returns the searchable fields of a kind: the generic ones, then the kind's own,
// sorted.
func fieldNames(p *model.KindProfile) []string {
	names := p.FieldNames()
	slices.Sort(names[len(names)-len(p.Fields):]) // the generic ones come first, the kind's own in map order
	return names
}

func limitOf(n int) (int, error) {
	switch {
	case n < 0:
		return 0, fmt.Errorf("limit %d is negative", n)
	case n == 0:
		return defaultLimit, nil
	case n > maxLimit:
		return maxLimit, nil
	}
	return n, nil
}

func listResources(ctx context.Context, s *engine.Session, p listParams) (any, error) {
	limit, err := limitOf(p.Limit)
	if err != nil {
		return nil, err
	}
	b, prof, err := s.KindIn(p.Context, p.Kind) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return nil, err
	}
	objs, err := s.ObjectsIn(ctx, p.Context, prof.Key, p.Namespaces)
	if err != nil {
		return nil, err
	}
	rows := make([]model.Row, 0, len(objs))
	cols, built := s.BuildRowsShown(ctx, b, prof, objs) // an integration's cells too (D-79)
	for _, r := range built {
		if model.MatchesFilter(r, p.Filter) {
			rows = append(rows, r)
		}
	}
	rows = engine.FilterRowsByStates(prof, rows, engine.CleanStates(p.States))
	engine.SortRows(rows, cols, engine.DefaultSort)
	res := ListResult{Kind: prof.Key, Total: len(rows), Rows: make([]ListRow, 0, min(limit, len(rows)))}
	now, tf := s.Now(), s.TimeFormat() // times as the window writes them (D-31)
	for _, r := range rows[:min(limit, len(rows))] {
		cells := make(map[string]string, len(cols))
		var tips map[string]string
		for i, c := range cols {
			if i < len(r.Cells) {
				cells[c.Title] = tf.CellText(c, r.Cells[i], now)
				if tip := r.Cells[i].Tip; tip != "" {
					if tips == nil {
						tips = map[string]string{}
					}
					tips[c.Title] = tip
				}
			}
		}
		res.Rows = append(res.Rows, ListRow{Ref: r.Ref, Status: r.Status, Cells: cells, Tips: tips})
	}
	res.Returned = len(res.Rows)
	return res, nil
}

func matcher(mode, value string) (func(string) bool, error) {
	want := strings.ToLower(value)
	switch mode {
	case "", matchExact:
		return func(v string) bool { return strings.EqualFold(v, value) }, nil
	case matchContains:
		return func(v string) bool { return strings.Contains(strings.ToLower(v), want) }, nil
	case matchPrefix:
		return func(v string) bool { return strings.HasPrefix(strings.ToLower(v), want) }, nil
	}
	return nil, fmt.Errorf("match %q: want exact, contains or prefix", mode)
}

// hasField reports whether a kind can be searched by a field.
func hasField(p *model.KindProfile, field string) bool {
	return field == "labels" || slices.Contains(p.FieldNames(), field)
}

func findResources(ctx context.Context, s *engine.Session, p findParams) (any, error) {
	limit, err := limitOf(p.Limit)
	if err != nil {
		return nil, err
	}
	match, err := matcher(p.Match, p.Value)
	if err != nil {
		return nil, err
	}
	b, err := connectedOn(s, p.Context)
	if err != nil {
		return nil, err
	}
	var profs []*model.KindProfile
	if p.Kind != "" {
		_, prof, err := s.KindIn(p.Context, p.Kind)
		if err != nil {
			return nil, err
		}
		if !hasField(prof, p.Field) {
			return nil, fmt.Errorf("%s have no field %q; fields: %s", prof.Key, p.Field, strings.Join(fieldNames(prof), ", "))
		}
		profs = []*model.KindProfile{prof}
	} else {
		for _, prof := range b.Profiles {
			// A virtual kind, and a kind that holds Secret values, only when cached already: Helm's
			// releases and the Secrets would start a cluster-wide watch of every value in their
			// scope for a search by name.
			if hasField(prof, p.Field) && ((prof.Derive == nil && !engine.HoldsSecretValues(prof)) || b.Cached(prof)) {
				profs = append(profs, prof)
			}
		}
		// Custom kinds only when cached already: a cluster may define hundreds, and searching all of
		// them would start a watch for each.
		for _, prof := range b.CustomProfiles() {
			if hasField(prof, p.Field) && b.Cached(prof) {
				profs = append(profs, prof)
			}
		}
		if len(profs) == 0 {
			return nil, fmt.Errorf("no kind has a field %q", p.Field)
		}
	}

	// Read every kind at once: a kind not cached yet starts its informer, and they list in parallel.
	objs := make([][]*unstructured.Unstructured, len(profs))
	errs := make([]error, len(profs))
	var wg sync.WaitGroup
	for i, prof := range profs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					s.Logger().Error("commands: reading the cache panicked", "kind", prof.Key, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
					errs[i] = fmt.Errorf("internal error: %v", r)
				}
			}()
			objs[i], errs[i] = s.ObjectsIn(ctx, p.Context, prof.Key, nil)
		}()
	}
	wg.Wait()

	res := FindResult{Matches: []FindMatch{}}
	for i, prof := range profs {
		if errs[i] != nil {
			if len(profs) == 1 {
				return nil, errs[i]
			}
			if res.Skipped == nil {
				res.Skipped = map[string]string{}
			}
			res.Skipped[prof.Key] = errs[i].Error()
			continue
		}
		for _, o := range objs[i] {
			var hits []string
			for _, v := range model.FieldValues(prof, p.Field, o) {
				if match(v) {
					hits = append(hits, v)
				}
			}
			if len(hits) == 0 {
				continue
			}
			res.Count++
			ref := model.ObjectRef{Context: b.Context, Kind: prof.Key, Namespace: o.GetNamespace(), Name: o.GetName()}
			res.Matches = append(res.Matches, FindMatch{Ref: ref, Field: p.Field, Values: hits})
		}
	}
	slices.SortFunc(res.Matches, func(a, c FindMatch) int {
		return strings.Compare(a.Ref.String(), c.Ref.String())
	})
	if len(res.Matches) > limit {
		res.Matches, res.Truncated = res.Matches[:limit], true
	}
	return res, nil
}
