package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The palette's jump (k9s's command line, D-new): a palette text that starts with ":" names a view
// the way k9s users type it, `:<name> [namespace] [/search] [@cluster]`, the words after the name
// in any order.
//
//   - <name> is a view: a kind by its plural, singular, Kind or short name (po, deploy, svc: the
//     cluster's discovery's, a custom kind's definition's; Backend.KindNames), its key
//     (gateways.gateway.networking.k8s.io) or its title, or a name k9s gives a page of its own that
//     Cogpit has in its place (jumpPages, jumpKindAliases); case-insensitive. A view whose name
//     begins with the word follows the exact matches, so the list completes as the user types.
//   - A further word is the namespace, one: the namespace filter is set to it; all or -A clears it;
//     without one the filter stays as it is. The shown cluster's namespaces complete it, as views
//     complete the name.
//   - /text is the search the view opens with; several /words are its terms.
//   - @word names the cluster by its context or the name the window shows; one other than the
//     shown cluster is shown, connected first when it is not connected (in the window its login
//     program is approved first).
//   - :ctx (:context, :contexts) lists the clusters to open, the word after it narrowing them;
//     k9s's other pages (xray, popeye, benchmarks, screen dumps, users, groups, aliases, help, quit)
//     have no counterpart and match no view.
//
// Every entry runs ui.jump (commands/jump.go), so an agent jumps alike; :ctx's run cluster.connect.
// An entry that cannot run stays in the list, greyed with the reason: no view by that name, a
// namespace the shown cluster does not have, a namespace on a cluster-scoped kind, a label
// selector (Cogpit's search takes text), a second namespace or cluster. The entries are made from
// what the caller knows (JumpIndex: the window's kinds.list, namespaces and contexts), without a
// request; ui.jump checks the view again on the cluster it runs on.

// JumpCommand is the command a jump entry runs.
const JumpCommand = "ui.jump"

// JumpAllNamespaces is the namespace word and ui.jump's namespace for every namespace.
const JumpAllNamespaces = "all"

// connectCommandID is the command :ctx's entries run.
const connectCommandID = "cluster.connect"

// jumpMaxEntries caps the entries of one text: a name's completions times a cluster's.
const jumpMaxEntries = 200

// JumpParams are ui.jump's parameters.
type JumpParams struct {
	View      string `json:"view" jsonschema:"the view to open: a kind key or a page as ui_openView takes it (pods, gateways.gateway.networking.k8s.io, cluster/overview), or a name k9s users type, case-insensitive: a kind's plural, singular, Kind or short name (po, deploy, svc; kinds_list lists singular and shortNames), pulses for the cluster Overview, workloads for the Workloads Overview, helm for Helm releases, pf for port forwards"`
	Namespace string `json:"namespace,omitempty" jsonschema:"set the namespace filter to this one namespace; all clears it (every namespace); omitted: the filter stays as it is. Not for a cluster-scoped kind"`
	Filter    string `json:"filter,omitempty" jsonschema:"the search text the view opens with, as ui_setFilter takes it; omitted: none"`
	Context   string `json:"context,omitempty" jsonschema:"the cluster, a context id as cluster_contexts lists it: shown first, connected first when it is not connected, as cluster_connect does; omitted: the shown cluster"`
}

// JumpKind is a kind as the jump names it.
type JumpKind struct {
	Key        string // kind key: "pods", "gateways.gateway.networking.k8s.io"
	Title      string // "Pods"
	Kind       string // "Pod"
	Names      model.KindNames
	Namespaced bool
}

// JumpView is a view the jump opens and the lower-case names that find it.
type JumpView struct {
	View  string   `json:"view"`  // the kind key or page ui.openView takes
	Title string   `json:"title"` // "Pods"
	Group string   `json:"group,omitempty"`
	Names []string `json:"names"`
	// Namespaced: the namespace filter applies (a namespaced kind, the Overviews).
	Namespaced bool `json:"namespaced,omitempty"`
}

// jumpPages are the pages the jump opens, by k9s's names for its own pages they stand in for: its
// pulses (the cluster at a glance) and its workloads.
var jumpPages = []JumpView{
	{View: ClusterOverview, Title: "Cluster Overview", Names: []string{"pulses", "pulse", "pu", "hz", "clusteroverview"}, Namespaced: true},
	{View: WorkloadsOverview, Title: "Workloads Overview", Names: []string{"workloads", "workload", "wk", "workloadsoverview"}, Namespaced: true},
}

// jumpKindAliases are k9s's names for its views of what Cogpit shows as a kind, by kind key: its
// helm view lists the Helm releases.
var jumpKindAliases = map[string][]string{
	helm.ReleaseKind: {"helm", "hm", "charts", "chart"},
	PortForwardKind:  {"pf"},
}

// jumpClusterNames are k9s's names of its contexts view: :ctx lists the clusters.
var jumpClusterNames = []string{"ctx", "context", "contexts"}

// JumpViewsFor are the jump's views of a cluster: the pages, then its kinds in their order.
func JumpViewsFor(kinds []JumpKind) []JumpView {
	out := make([]JumpView, 0, len(jumpPages)+len(kinds))
	out = append(out, jumpPages...)
	for _, k := range kinds {
		out = append(out, jumpView(k))
	}
	return out
}

// jumpView is a kind's view with every name that finds it.
func jumpView(k JumpKind) JumpView {
	plural, group, _ := strings.Cut(k.Key, ".")
	v := JumpView{View: k.Key, Title: k.Title, Group: group, Namespaced: k.Namespaced}
	add := func(n string) {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" && !slices.Contains(v.Names, n) {
			v.Names = append(v.Names, n)
		}
	}
	add(plural)
	add(k.Key)
	add(k.Names.Singular)
	add(k.Kind)
	for _, n := range k.Names.ShortNames {
		add(n)
	}
	add(strings.ReplaceAll(k.Title, " ", ""))
	for _, n := range jumpKindAliases[k.Key] {
		add(n)
	}
	return v
}

// JumpViews are the jump's views of a connected backend's cluster: its pages and every kind it
// serves, built-in, local, virtual and custom, with the names the cluster gives each.
func (b *Backend) JumpViews() []JumpView {
	all := b.AllProfiles()
	kinds := make([]JumpKind, 0, len(all))
	for _, p := range all {
		kinds = append(kinds, JumpKind{Key: p.Key, Title: p.Title, Kind: p.Kind, Names: b.KindNames(p), Namespaced: p.Namespaced})
	}
	return JumpViewsFor(kinds)
}

// ResolveJumpView returns the view a name names exactly (a key, or a name of JumpView.Names, case
// ignored): the first in the views' order when several share it (a custom kind's short name that a
// built-in kind has too).
func ResolveJumpView(views []JumpView, name string) (JumpView, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return JumpView{}, false
	}
	for _, v := range views {
		if v.View == name || slices.Contains(v.Names, name) {
			return v, true
		}
	}
	return JumpView{}, false
}

// matchJumpViews are the views a lower-case word names, best first: those with the word as a
// name, then those with a name it begins, each in the views' order; every view for no word.
func matchJumpViews(views []JumpView, word string) []JumpView {
	if word == "" {
		return views
	}
	var exact, prefix []JumpView
	for _, v := range views {
		switch {
		case v.View == word || slices.Contains(v.Names, word):
			exact = append(exact, v)
		case slices.ContainsFunc(v.Names, func(n string) bool { return strings.HasPrefix(n, word) }):
			prefix = append(prefix, v)
		}
	}
	return append(exact, prefix...)
}

// JumpQuery is a jump text read word by word (ParseJump).
type JumpQuery struct {
	// Name is the view's name, lower case; "" when the text names none (every view then).
	Name string
	// Namespace is the namespace word, lower case: JumpAllNamespaces for all and -A; "" none.
	Namespace string
	// Filter is the search: the /words' text, joined by a space.
	Filter string
	// Cluster is the word after @; AtCluster says an @ was typed, Cluster may be empty so far.
	Cluster   string
	AtCluster bool
	// Problem says why the text names no jump: a label selector, a second namespace or cluster.
	Problem string
}

// ParseJump reads a palette text that starts with ":"; false for any other text.
func ParseJump(text string) (JumpQuery, bool) {
	rest, ok := strings.CutPrefix(text, ":")
	if !ok {
		return JumpQuery{}, false
	}
	var q JumpQuery
	problem := func(p string) {
		if q.Problem == "" {
			q.Problem = p
		}
	}
	words := strings.Fields(rest)
	if len(words) > 0 && !strings.HasPrefix(words[0], "/") && !strings.HasPrefix(words[0], "@") {
		q.Name, words = strings.ToLower(words[0]), words[1:]
	}
	var terms []string
	for _, w := range words {
		switch {
		case strings.HasPrefix(w, "/"):
			if t := w[1:]; t != "" {
				terms = append(terms, t)
			}
		case strings.HasPrefix(w, "@"):
			if q.AtCluster {
				problem("one cluster at a time")
			}
			q.AtCluster, q.Cluster = true, w[1:]
		case strings.Contains(w, "=") || w == "-l":
			_, value, _ := strings.Cut(w, "=")
			if value == "" {
				value = "text"
			}
			problem("label selectors are not supported: search with /" + value)
		default:
			ns := strings.ToLower(w)
			if ns == "-a" {
				ns = JumpAllNamespaces
			}
			if strings.Trim(ns, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
				problem("“" + w + "” is not a namespace name")
			}
			if q.Namespace != "" && q.Namespace != ns {
				problem("one namespace at a time: the namespace filter takes several")
			}
			q.Namespace = ns
		}
	}
	q.Filter = strings.Join(terms, " ")
	return q, true
}

// JumpIndex is what the jump matches a text against: what the window knows of the shown cluster
// and the contexts.
type JumpIndex struct {
	// Context is the shown cluster's context while it is connected, else ""; Views and Namespaces
	// are its.
	Context string
	Views   []JumpView
	// Namespaces are the shown cluster's namespaces; nil when they are not known, and then a
	// namespace word is taken as it is.
	Namespaces []string
	// Clusters are the contexts @ and :ctx name.
	Clusters []JumpCluster
}

// JumpCluster is a context the jump can name.
type JumpCluster struct {
	Context string `json:"context"` // its id, as cluster.contexts lists it
	Name    string `json:"name"`    // the name the window shows (ClusterName, D-68)
}

// JumpEntries are the palette's entries for a jump text (one that starts with ":"; nil for
// another), best first; runs reports the commands the caller runs its own way (PaletteInput.Runs).
// Nil when the registry has no ui.jump.
func (r *Registry) JumpEntries(ix JumpIndex, text string, runs func(id string) bool) []PaletteEntry {
	q, ok := ParseJump(text)
	jump, has := r.Get(JumpCommand)
	if !ok || !has {
		return nil
	}
	if slices.Contains(jumpClusterNames, q.Name) {
		return r.clusterEntries(ix, q, runs)
	}
	if q.Problem != "" {
		return []PaletteEntry{greyJump(jump, text, q.Problem)}
	}
	targets := []JumpCluster{{}} // the shown cluster
	if q.AtCluster {
		if targets = matchClusters(ix.Clusters, q.Cluster); len(targets) == 0 {
			return []PaletteEntry{greyJump(jump, text, "no cluster “"+q.Cluster+"”")}
		}
	}
	var out []PaletteEntry
	for _, t := range targets {
		out = append(out, jumpEntriesOn(jump, ix, q, t, text)...)
		if len(out) >= jumpMaxEntries {
			return out[:jumpMaxEntries]
		}
	}
	return out
}

// jumpEntriesOn are a jump's entries on one cluster: t, or the shown one for a zero t.
func jumpEntriesOn(jump *Command, ix JumpIndex, q JumpQuery, t JumpCluster, text string) []PaletteEntry {
	other := t.Context != "" && t.Context != ix.Context
	var views []JumpView
	if ix.Context != "" {
		views = matchJumpViews(ix.Views, q.Name)
	}
	resolved := true
	if len(views) == 0 {
		switch {
		case other && q.Name != "":
			// The other cluster may serve what the shown one does not: ui.jump resolves the name there.
			views, resolved = []JumpView{{View: q.Name, Title: "“" + q.Name + "”", Namespaced: true}}, false
		case ix.Context == "" && !other:
			return []PaletteEntry{greyJump(jump, text, "no cluster is connected: name one with @")}
		case q.Name == "":
			return []PaletteEntry{greyJump(jump, text, "type a view's name")}
		default:
			return []PaletteEntry{greyJump(jump, text, "no view named “"+q.Name+"”")}
		}
	}
	namespaces, nsReason := []string{q.Namespace}, ""
	if !other && q.Namespace != "" && q.Namespace != JumpAllNamespaces && ix.Namespaces != nil {
		if namespaces = matchWords(ix.Namespaces, q.Namespace); len(namespaces) == 0 {
			namespaces, nsReason = []string{q.Namespace}, "no namespace “"+q.Namespace+"” in "+shownName(ix)
		}
	}
	cluster := ""
	if other {
		cluster = t.Name
	}
	out := make([]PaletteEntry, 0, len(views)*len(namespaces))
	for _, v := range views {
		for _, ns := range namespaces {
			reason := nsReason
			if ns != "" && ns != JumpAllNamespaces && !v.Namespaced && resolved {
				reason = v.Title + " are not namespaced"
			}
			p := JumpParams{View: v.View, Namespace: ns, Filter: q.Filter}
			if other {
				p.Context = t.Context
			}
			raw, err := json.Marshal(p)
			if err != nil {
				continue
			}
			out = append(out, PaletteEntry{Command: jump.ID, Label: jumpLabel(v.Title, ns, q.Filter, cluster), Category: v.Group,
				Danger: jump.Danger, Params: raw, Reason: reason})
			if len(out) >= jumpMaxEntries {
				return out
			}
		}
	}
	return out
}

// shownName names the shown cluster in a reason.
func shownName(ix JumpIndex) string {
	for _, c := range ix.Clusters {
		if c.Context == ix.Context && c.Name != "" {
			return c.Name
		}
	}
	return ShortContextName(ContextLabel(ix.Context))
}

// jumpLabel is a jump entry's label: "Switch to Pods in kube-system matching “web” on prod".
func jumpLabel(title, namespace, filter, cluster string) string {
	b := "Switch to " + title
	switch namespace {
	case "":
	case JumpAllNamespaces:
		b += " in all namespaces"
	default:
		b += " in " + namespace
	}
	if filter != "" {
		b += " matching “" + filter + "”"
	}
	if cluster != "" {
		b += " on " + cluster
	}
	return b
}

// greyJump is the one entry of a text that names no jump: the text itself, greyed with the reason.
func greyJump(jump *Command, text, reason string) PaletteEntry {
	return PaletteEntry{Command: jump.ID, Label: text, Danger: jump.Danger, Params: json.RawMessage(`{}`), Reason: reason}
}

// clusterEntries are :ctx's: the window's list of clusters (cluster.connect run the caller's way,
// as ⌘P) while no word follows, then every cluster the word names, each to open.
func (r *Registry) clusterEntries(ix JumpIndex, q JumpQuery, runs func(string) bool) []PaletteEntry {
	connect, ok := r.Get(connectCommandID)
	if !ok {
		return nil
	}
	word := q.Namespace // a bare word after :ctx names the cluster, as in k9s
	if word == "" {
		word = q.Cluster
	}
	var out []PaletteEntry
	if word == "" && runs != nil && runs(connect.ID) {
		category, _, _ := strings.Cut(connect.MenuPath, "/")
		out = append(out, PaletteEntry{Command: connect.ID, Label: connect.Label, Category: category, Shortcut: connect.Shortcut,
			Danger: connect.Danger, Params: json.RawMessage(`{}`), Own: true})
	}
	matches := matchClusters(ix.Clusters, word)
	if len(matches) == 0 && word != "" {
		return []PaletteEntry{{Command: connect.ID, Label: ":" + q.Name + " " + word, Danger: connect.Danger,
			Params: json.RawMessage(`{}`), Reason: "no cluster “" + word + "”"}}
	}
	for _, c := range matches {
		raw, err := json.Marshal(struct {
			Context string `json:"context"`
		}{c.Context})
		if err != nil {
			continue
		}
		out = append(out, PaletteEntry{Command: connect.ID, Label: "Open " + c.Name, Category: ContextLabel(c.Context),
			Danger: connect.Danger, Params: raw})
		if len(out) >= jumpMaxEntries {
			break
		}
	}
	return out
}

// matchClusters are the clusters a word names, best first: by their context, its name without the
// file, or the name the window shows, equal to the word, then beginning with it, then containing
// it, case ignored; every cluster for no word.
func matchClusters(cs []JumpCluster, word string) []JumpCluster {
	w := strings.ToLower(word)
	if w == "" {
		return cs
	}
	var ranks [3][]JumpCluster
	for _, c := range cs {
		names := []string{strings.ToLower(c.Context), strings.ToLower(ContextLabel(c.Context)), strings.ToLower(c.Name)}
		switch {
		case slices.Contains(names, w):
			ranks[0] = append(ranks[0], c)
		case slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, w) }):
			ranks[1] = append(ranks[1], c)
		case slices.ContainsFunc(names, func(n string) bool { return strings.Contains(n, w) }):
			ranks[2] = append(ranks[2], c)
		}
	}
	return slices.Concat(ranks[0], ranks[1], ranks[2])
}

// matchWords are the words equal to w, then beginning with it, then containing it.
func matchWords(words []string, w string) []string {
	var ranks [3][]string
	for _, s := range words {
		l := strings.ToLower(s)
		switch {
		case l == w:
			ranks[0] = append(ranks[0], s)
		case strings.HasPrefix(l, w):
			ranks[1] = append(ranks[1], s)
		case strings.Contains(l, w):
			ranks[2] = append(ranks[2], s)
		}
	}
	return slices.Concat(ranks[0], ranks[1], ranks[2])
}

// ResolveJump returns the view ui.jump opens on a backend's cluster, or why it cannot: no view by
// the name, or a namespace for a view the namespace filter does not apply to.
func (b *Backend) ResolveJump(p JumpParams) (JumpView, error) {
	v, ok := ResolveJumpView(b.JumpViews(), p.View)
	if !ok {
		return JumpView{}, fmt.Errorf("%w %q: no kind or page by that name on %s; kinds_list lists the kinds with their names", ErrUnknownKind, p.View, b.Context)
	}
	if p.Namespace != "" && p.Namespace != JumpAllNamespaces && !v.Namespaced {
		return v, fmt.Errorf("%s are not namespaced: no namespace applies", v.Title)
	}
	return v, nil
}
