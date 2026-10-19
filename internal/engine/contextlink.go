package engine

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// A context's link (cluster.links, cluster.remove; the clusters' menus' "Remove from Cogpit…",
// ui-parity › Clusters). Cogpit reaches a context through Cogpit's link to a kubeconfig file or
// folder, a kubeconfig source (kubeconfigs.go); removing a cluster from Cogpit removes that link
// and never edits or deletes a kubeconfig file. A file the user added is unlinked that way, and
// with it every context the file defines. The rules (Cogpit's defaults, 2026-10-13; Lens's own rule
// is not captured):
//   - a context of a file in a sync folder, the default ~/.kube included, is unlinked with its
//     whole folder, and every context of the folder's files leaves Cogpit with it (the owner's A97,
//     2026-10-14 evening; built 2026-10-15). The window's confirmation names them all, and the
//     removal is bound to that list: a folder whose contexts changed since is refused, so that
//     nothing leaves Cogpit the user was not shown (the lead's night decision for the owner to
//     confirm);
//   - a context of a file named by $KUBECONFIG or --kubeconfig is refused: the file is offered
//     again at the next start;
//   - the demo cluster has no kubeconfig to unlink.

// ContextLink is where Cogpit reads a context from, and whether cluster.remove can unlink it.
type ContextLink struct {
	// Context is the context id ("dev [dev.yaml]").
	Context string `json:"context"`
	// Source is the kubeconfig source the context comes from: an added file, a sync folder (the
	// default ~/.kube or one the user added) or a file named for this run.
	Source KubeconfigSource `json:"source"`
	// File is the kubeconfig file's path; Contexts are the ids of every context that leaves Cogpit
	// with the link: the file's, or for a synced folder every context of the folder's files.
	File     string   `json:"file"`
	Contexts []string `json:"contexts"`
	// Folder is the synced folder's path when the link is a folder's (A97): removing the context
	// unlinks the whole folder.
	Folder string `json:"folder,omitempty"`
	// Removable says whether cluster.remove can unlink it; Reason says why not.
	Removable bool   `json:"removable"`
	Reason    string `json:"reason,omitempty"`
}

// errNoSources refuses the link commands in a session without kubeconfig sources.
var errNoSources = errors.New("this Cogpit keeps no kubeconfig sources")

// ContextLinks returns the link of every context of the kubeconfig sources, sorted by context id.
// It reads the sources' files once (no cluster is asked).
func (s *Session) ContextLinks() ([]ContextLink, error) {
	k := s.KubeconfigSources()
	if k == nil {
		return nil, errNoSources
	}
	files, _, err := k.Files()
	if err != nil {
		return nil, err
	}
	out := []ContextLink{}
	for _, f := range files {
		if f.Err != nil {
			continue
		}
		ids := linkedContexts(files, f)
		for _, c := range f.Contexts {
			out = append(out, linkOf(SourceContextID(c.Name, f.Label), f, ids))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Context < out[j].Context })
	return out, nil
}

// ContextLink returns one context's link. The demo cluster's says why it has none; a context no
// source offers is an error.
func (s *Session) ContextLink(id string) (ContextLink, error) {
	if IsDemoContext(id) {
		return ContextLink{Context: id, Contexts: []string{id},
			Reason: "the demo cluster is Cogpit's own (--demo): it comes from no kubeconfig file"}, nil
	}
	k := s.KubeconfigSources()
	if k == nil {
		return ContextLink{}, errNoSources
	}
	files, _, err := k.Files()
	if err != nil {
		return ContextLink{}, err
	}
	for _, f := range files {
		for _, c := range f.Contexts {
			if SourceContextID(c.Name, f.Label) == id {
				return linkOf(id, f, linkedContexts(files, f)), nil
			}
		}
	}
	return ContextLink{}, fmt.Errorf("%s is not a context of a kubeconfig source (cluster_contexts lists them)", kubeconfig.DisplayText(id, 200))
}

// linkedContexts are the ids of every context that leaves Cogpit with f's link, sorted: f's own, or
// for a file of a synced folder those of every readable file of the folder.
func linkedContexts(files []KubeconfigFile, f KubeconfigFile) []string {
	ids := []string{}
	for _, g := range files {
		if g.Err != nil || (g.Path != f.Path && (f.Source.Kind != SourceFolder || g.Source.ID != f.Source.ID)) {
			continue
		}
		for _, c := range g.Contexts {
			ids = append(ids, SourceContextID(c.Name, g.Label))
		}
	}
	sort.Strings(ids)
	return ids
}

// linkOf is a context's link through the file f, whose contexts are ids.
func linkOf(id string, f KubeconfigFile, ids []string) ContextLink {
	l := ContextLink{Context: id, Source: f.Source, File: f.Path, Contexts: ids, Removable: true}
	file := kubeconfig.DisplayText(f.Path, 400)
	switch {
	case f.Source.Session:
		l.Removable, l.Reason = false, "named by $KUBECONFIG or --kubeconfig for this run: "+file+" is offered again at the "+
			"next start (take it out of $KUBECONFIG, or remove it for this run in Kubeconfig Sources)"
	case f.Source.Kind == SourceFolder:
		l.Folder = f.Source.Path
	}
	return l
}

// CheckRemoveContext returns why cluster.remove cannot unlink a context, or nil.
func (s *Session) CheckRemoveContext(id string) error {
	l, err := s.ContextLink(id)
	if err != nil {
		return err
	}
	if !l.Removable {
		return errors.New(l.Reason)
	}
	return nil
}

// ErrLinkChanged refuses a removal whose contexts are no longer those the confirmation named.
var ErrLinkChanged = errors.New("the clusters that would leave Cogpit changed since the confirmation")

// RemoveContext unlinks a context's kubeconfig file, or its synced folder, from Cogpit
// (cluster.remove): the source is removed as kubeconfig.remove removes it
// (RemoveKubeconfigSource), so every context of the file or folder leaves Cogpit with its tabs,
// the credential plugins allowed for them are forgotten, and a connection made from one is closed.
// The files stay on disk as they are. shown are the contexts the user's confirmation named: when
// given, and always for a folder (A97), the removal is refused with ErrLinkChanged unless they are
// exactly the link's contexts now. It returns the link that was removed.
func (s *Session) RemoveContext(id string, shown []string) (ContextLink, error) {
	l, err := s.ContextLink(id)
	if err != nil {
		return ContextLink{}, err
	}
	if !l.Removable {
		return l, errors.New(l.Reason)
	}
	if shown != nil || l.Folder != "" {
		named := slices.Clone(shown)
		sort.Strings(named)
		if !slices.Equal(named, l.Contexts) {
			return l, fmt.Errorf("%w: %s now takes %d (%s); ask again", ErrLinkChanged,
				kubeconfig.DisplayText(cmp.Or(l.Folder, l.File), 400), len(l.Contexts), kubeconfig.DisplayText(strings.Join(l.Contexts, ", "), 400))
		}
	}
	if _, err := s.RemoveKubeconfigSource(l.Source.ID); err != nil {
		return l, err
	}
	return l, nil
}

// RemoveKubeconfigSource removes a kubeconfig source (kubeconfig.remove, and cluster.remove through
// RemoveContext): Cogpit's link to a file or folder, never the file or folder itself. A connection
// made from one of its files is closed, and the tabs of the contexts that leave Cogpit with it are
// closed, the active one too (UI state is engine state: the tab bar and an agent's ui_state lose
// them alike). A context the sources still offer afterwards keeps its tabs. Cogpit's default
// (2026-10-13, not the owner's decision; Lens's behaviour is not captured): a cluster Cogpit no
// longer offers keeps no tab, which could only fail to connect.
func (s *Session) RemoveKubeconfigSource(id string) (KubeconfigSource, error) {
	k := s.KubeconfigSources()
	if k == nil {
		return KubeconfigSource{}, errNoSources
	}
	src, err := k.Remove(id)
	if src.ID != "" {
		s.CloseGoneSourceConnection() // a connection made from the source's files ends with the link
		s.closeTabsOfSource(k, src)
		s.forgetSettingsOfSource(k, src)
		s.dropHotbarTilesOfSource(k, src)
		s.emit(Event{Type: EventContexts})
	}
	return src, err
}

// dropHotbarTilesOfSource takes the removed source's contexts that no source offers any more off
// the hotbar (the owner's WF45 of 2026-10-19: "Ein entfernter Cluster wird automatisch auch aus der
// Hotbar entfernt."): removing a source and Remove from Cogpit are the user's own acts. A file that
// went away by itself keeps its tile, marked missing (HotbarTile.Missing): it may come back, and
// its tile keeps its place. When the sources cannot be read, nothing is dropped on a guess.
func (s *Session) dropHotbarTilesOfSource(k *KubeconfigSources, src KubeconfigSource) {
	if s.opts.Hotbar == nil {
		return
	}
	offered, err := offeredContexts(k)
	if err != nil {
		return
	}
	gone := func(t HotbarTile) bool { return ofSource(src, t.Context) && !offered[t.Context] }
	st, err := s.HotbarState()
	if err != nil || !slices.ContainsFunc(st.Clusters, gone) {
		return
	}
	if _, err := s.ChangeHotbar(func(h *HotbarState) error {
		h.Clusters = slices.DeleteFunc(h.Clusters, gone)
		return nil
	}); err != nil {
		s.log.Warn("hotbar: the tiles of a removed source stay", "source", src.ID, "err", err)
	}
}

// closeTabsOfSource closes the tabs of the removed source's contexts that no source offers any
// more, and forgets UIState.Context when it names one of them and no connection holds it (the
// connection made from the source is closed already), so that nothing points at a context that is
// gone (UIState.dropContexts). When the sources cannot be read, nothing changes on a guess. Closing
// the active tab leaves no tab active, as closing a cluster's last tab does (UIState.closeTab). No
// notice: the user removed the source, after a confirmation naming its contexts; but one when an
// editor's unsaved text of those contexts was kept as a draft (dock_gone.go).
func (s *Session) closeTabsOfSource(k *KubeconfigSources, src KubeconfigSource) {
	offered, err := offeredContexts(k)
	if err != nil {
		return
	}
	gone := func(id string) bool { return ofSource(src, id) && !offered[id] }
	u := s.UIState()
	if !slices.ContainsFunc(u.Tabs, func(t ViewTab) bool { return gone(t.Context) }) && !u.dockOfGone(gone) &&
		(!gone(u.Context) || activeConnection(u.Connection.State)) {
		return
	}
	kept := s.keepDraftsOf(gone) // the dock's editors close too: their unsaved text stays (dock_gone.go)
	s.UpdateUI(func(u *UIState) { u.dropContexts(gone) })
	if notice := keptDraftsText(kept, true); notice != "" {
		s.emit(Event{Type: EventToast, Level: LevelInfo, Message: notice})
	}
}

// offeredContexts returns the ids of every context the sources offer now.
func offeredContexts(k *KubeconfigSources) (map[string]bool, error) {
	files, _, err := k.Files()
	if err != nil {
		return nil, err
	}
	offered := map[string]bool{}
	for _, f := range files {
		for _, c := range f.Contexts {
			offered[SourceContextID(c.Name, f.Label)] = true
		}
	}
	return offered, nil
}

// forgetSettingsOfSource forgets the cluster settings (cluster-settings.json: name, icon, accent
// colour, proxy, integrations, agent allowance, …) of the removed source's contexts that no source
// offers any more: a cluster removed from Cogpit and added again is a new cluster, asked its
// integrations again (the lead's decision, 2026-10-17 evening, after the owner's walk, WF4: he
// removed and re-added a cluster to see the integrations question again, and its kept choice
// stopped it). A session file ($KUBECONFIG) keeps them: it is offered again at the next start. When
// the sources cannot be read, nothing is forgotten on a guess.
func (s *Session) forgetSettingsOfSource(k *KubeconfigSources, src KubeconfigSource) {
	c := s.opts.ClusterSettings
	if c == nil || src.Session {
		return
	}
	offered, err := offeredContexts(k)
	if err != nil {
		return
	}
	c.mu.Lock()
	if err := c.load(); err != nil {
		s.log.Warn("cluster settings", "err", err)
	}
	var gone []string
	for id := range c.by {
		if ofSource(src, id) && !offered[id] {
			gone = append(gone, id)
		}
	}
	c.mu.Unlock()
	sort.Strings(gone)
	for _, id := range gone {
		if _, err := s.ChangeClusterSettings(id, func(cs *ClusterSettings) error { *cs = ClusterSettings{}; return nil }); err != nil {
			s.log.Warn("cluster settings of a removed cluster", "context", id, "err", err)
		}
	}
}

// ofSource reports whether a context id ("<context> [<label>]", SourceContextID) names a context of
// the source's files: the label is the source's id (a file), or the source's id followed by the
// name of a file in it (a folder, whose id ends in a slash).
func ofSource(src KubeconfigSource, id string) bool {
	rest, ok := strings.CutSuffix(id, "]")
	if !ok || src.ID == "" {
		return false
	}
	if src.Kind != SourceFolder {
		return strings.HasSuffix(rest, " ["+src.ID)
	}
	i := strings.LastIndex(rest, " ["+src.ID)
	if i < 0 {
		return false
	}
	name := rest[i+len(" [")+len(src.ID):]
	return name != "" && !strings.ContainsAny(name, `/\`)
}
