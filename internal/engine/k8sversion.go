package engine

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/k8seol"
)

// The Kubernetes version of the shown cluster and of its nodes against upstream's support (D-91,
// the owner's WF30 of 2026-10-18). The API server's version is the one its connect read
// (cluster.Conn's GET /version, Backend.ServerVersion), a kubelet's is its Node's
// status.nodeInfo.kubeletVersion in the cache: nothing here asks the cluster. The support is the
// Kubernetes release table built into this Cogpit (internal/model/k8seol): upstream's dates only,
// an estimate for a minor newer than the table, never a fetch.
//
//   - The shown cluster: UIState.ClusterVersion, which the navigator's root row shows.
//   - Its nodes: Cogpit's own overlay on the Nodes list's Version column (overlay.go): a kubelet
//     whose minor is past its end of life, or outside the version skew policy against the API
//     server, reads "<version> ⚠" in the warning colour; one in maintenance mode is drawn in the
//     pending colour; the cell's tooltip says the support and the skew. The node drawer's Kubelet
//     version takes the same colour, with "Upstream support" and, outside the policy, "Version
//     skew" after it (markNodeVersion).
//   - A day passing is seen when the row is built again: a Node's status changes at least every five
//     minutes (the kubelet's report), the UI state at every change, ui_state when read.

// ClusterVersion is the shown cluster's API server version and its minor's upstream support:
// UIState.ClusterVersion.
type ClusterVersion struct {
	// Context is the context id the version belongs to.
	Context string `json:"context"`
	// Version is the API server's version as its connect read it: "v1.36.1".
	Version string `json:"version"`
	// The minor, its days and the table's newest minor and fetch day.
	k8seol.Support
	// State is the support on the day of the UI state's last change: supported, maintenance (the
	// last two months), endOfLife or unknown.
	State k8seol.State `json:"state"`
	// DaysLeft counts the days to the end of life on that day: negative past it, 0 when unknown.
	DaysLeft int `json:"daysLeft"`
}

// releases is the release table the session reads: its options', else the one built in.
func (s *Session) releases() *k8seol.Table { return tableOr(s.opts.K8sReleases) }

// tableOr returns t, or the table built in for nil.
func tableOr(t *k8seol.Table) *k8seol.Table {
	if t == nil {
		return k8seol.Embedded()
	}
	return t
}

// newClusterVersion assesses an API server's version on now's day.
func newClusterVersion(t *k8seol.Table, contextID, version string, now time.Time) ClusterVersion {
	sup := t.SupportOf(version)
	return ClusterVersion{Context: contextID, Version: version, Support: sup, State: sup.StateAt(now), DaysLeft: sup.DaysLeft(now)}
}

// versionBook keeps each connected cluster's API server version, by context id, as its last
// connect read it.
type versionBook struct {
	mu sync.Mutex
	by map[string]string
}

func (k *versionBook) set(contextID, version string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.by == nil {
		k.by = map[string]string{}
	}
	k.by[contextID] = version
}

func (k *versionBook) get(contextID string) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.by[contextID]
}

// learnVersion records a new connection's API server version and puts the version check on its
// nodes, before the session publishes it. Every connect calls it: the window's, one in the
// background and a reconnect's.
func (s *Session) learnVersion(b *Backend) {
	if b == nil {
		return
	}
	v := b.ServerVersion()
	s.versions.set(b.Context, v)
	if v != "" {
		b.overlays.putOwn(&versionCheck{table: s.releases(), server: v, now: s.Now, format: s.TimeFormat})
	}
}

// syncClusterVersion keeps ClusterVersion the shown cluster's, after every change.
func (u *UIState) syncClusterVersion(k *versionBook, t *k8seol.Table, now time.Time) {
	u.ClusterVersion = nil
	if u.Context == "" || !u.Connection.State.Live() {
		return
	}
	if v := k.get(u.Context); v != "" {
		cv := newClusterVersion(t, u.Context, v, now)
		u.ClusterVersion = &cv
	}
}

// ClusterVersionNow returns a UI state's cluster version assessed as of now, in the Preferences'
// zone: ui_state's, which an agent may read days after the last change.
func (s *Session) ClusterVersionNow(cv *ClusterVersion) *ClusterVersion {
	if cv == nil {
		return nil
	}
	now, _ := VersionDates(s.TimeFormat(), s.Now())
	out := *cv
	out.State, out.DaysLeft = cv.StateAt(now), cv.Support.DaysLeft(now) // DaysLeft is also a field
	return &out
}

// VersionDates returns now in a time format's zone and the writer of the release table's days in
// its date layout: a day is written as it is, never moved into the zone (D-31).
func VersionDates(f model.TimeFormat, now time.Time) (time.Time, func(time.Time) string) {
	if f.Zone != nil {
		now = now.In(f.Zone)
	}
	layout := f.Layout.OrDefault().Date
	return now, func(d time.Time) string { return d.Format(layout) }
}

// kubeletVersionItem is the node drawer's item of the kubelet's version (kinds/nodes.go).
const kubeletVersionItem = "Kubelet version"

// The drawer's items after it.
const (
	upstreamSupportItem = "Upstream support"
	versionSkewItem     = "Version skew"
)

// nodeVersionColumn is the Nodes list's Version column.
const nodeVersionColumn = "version"

// kubeletNote is what the version check says of a kubelet.
type kubeletNote struct {
	support k8seol.Support
	state   k8seol.State
	skew    k8seol.Skew
	skewed  bool // outside the version skew policy
	text    string
	short   string
}

// noteKubelet assesses a kubelet's version against its API server's on now's day.
func noteKubelet(t *k8seol.Table, kubelet, server string, now time.Time, date func(time.Time) string) kubeletNote {
	n := kubeletNote{support: t.SupportOf(kubelet)}
	n.state = n.support.StateAt(now)
	n.text, n.short = n.support.Text(now, date), n.support.Short(now, date)
	if k, ok := k8seol.KubeletSkew(kubelet, server); ok && k.Outside() {
		n.skew, n.skewed = k, true
	}
	return n
}

// warns reports whether the kubelet gets the ⚠.
func (n kubeletNote) warns() bool { return n.state == k8seol.EndOfLife || n.skewed }

// status is the version's colour: warning with the ⚠, pending in maintenance mode.
func (n kubeletNote) status() model.Status {
	switch {
	case n.warns():
		return model.StatusWarning
	case n.state == k8seol.Maintenance:
		return model.StatusPending
	}
	return model.StatusNone
}

// tip is the version cell's tooltip: the support, then the skew outside the policy.
func (n kubeletNote) tip() string {
	if n.skewed {
		return n.text + "\n" + n.skew.Text()
	}
	return n.text
}

// cell is the overlay's Version cell: "<version> ⚠" in parts with the ⚠, else the colour alone;
// the tooltip always.
func (n kubeletNote) cell(kubelet string) model.Cell {
	if n.warns() {
		c := model.PartsCell(model.StatusWarning, model.CellPart{Text: kubelet, Status: model.StatusWarning},
			model.CellPart{Text: "⚠", Status: model.StatusWarning})
		c.Tip = n.tip()
		return c
	}
	return model.Cell{Status: n.status(), Tip: n.tip()}
}

// isNodes reports whether a profile is the built-in Nodes kind's.
func isNodes(p *model.KindProfile) bool { return p != nil && p.Key == "nodes" && p.Group == "" }

// versionCheck is Cogpit's own overlay on a cluster's Nodes list (overlay.go): the kubelets'
// support and skew in the Version column. It reads only the object it is given.
type versionCheck struct {
	table  *k8seol.Table           // the release table
	server string                  // the API server's version
	now    func() time.Time        // the session's clock
	format func() model.TimeFormat // the window's time format (D-31)
}

func (v *versionCheck) columns(p *model.KindProfile) []overlayColumn {
	if !isNodes(p) {
		return nil
	}
	i := p.ColumnIndex(nodeVersionColumn)
	if i < 0 {
		return nil
	}
	return []overlayColumn{{Column: p.Columns[i]}}
}

func (v *versionCheck) cell(p *model.KindProfile, column string, obj *unstructured.Unstructured) (model.Cell, bool) {
	if !isNodes(p) || column != nodeVersionColumn {
		return model.Cell{}, false
	}
	kubelet := model.NestedString(obj, "status", "nodeInfo", "kubeletVersion")
	if kubelet == "" {
		return model.Cell{}, false
	}
	now, date := VersionDates(v.format(), v.now())
	return noteKubelet(v.table, kubelet, v.server, now, date).cell(kubelet), true
}

func (v *versionCheck) sections(*model.KindProfile, *unstructured.Unstructured, model.TimeFormat) []model.DetailSection {
	return nil // markNodeVersion marks the drawer's own item
}

func (v *versionCheck) waitSynced(context.Context) {}

func (v *versionCheck) stop() {}

// markNodeVersion gives a node's drawer its kubelet's support and skew (MarkKubeletVersion);
// nothing for another kind or a backend without an API server version.
func (s *Session) markNodeVersion(b *Backend, p *model.KindProfile, obj *unstructured.Unstructured, m *DetailModel) {
	if !isNodes(p) {
		return
	}
	MarkKubeletVersion(s.releases(), m, model.NestedString(obj, "status", "nodeInfo", "kubeletVersion"), b.ServerVersion(), s.Now(), s.TimeFormat())
}

// KubeletVersionCell is a Nodes row's Version cell as the version check shows it: the kind's
// cell, the kubelet's version, with the check's colour, ⚠ and tooltip merged in (mergeCell); base
// itself without a kubelet or an API server version; t nil reads the table built in. The window's
// tests draw with it.
func KubeletVersionCell(t *k8seol.Table, base model.Cell, kubelet, server string, now time.Time, f model.TimeFormat) model.Cell {
	if kubelet == "" || server == "" {
		return base
	}
	now, date := VersionDates(f, now)
	return mergeCell(base, noteKubelet(tableOr(t), kubelet, server, now, date).cell(kubelet))
}

// MarkKubeletVersion gives a node's drawer its kubelet's support and skew, on now's day in f: the
// Kubelet version in the version's colour, then "Upstream support" and, outside the skew policy,
// "Version skew". Nothing without a kubelet or an API server version, or a Kubelet version item;
// t nil reads the table built in.
func MarkKubeletVersion(t *k8seol.Table, m *DetailModel, kubelet, server string, now time.Time, f model.TimeFormat) {
	if m == nil || kubelet == "" || server == "" {
		return
	}
	now, date := VersionDates(f, now)
	n := noteKubelet(tableOr(t), kubelet, server, now, date)
	for si := range m.Sections {
		sec := &m.Sections[si]
		if sec.Title != model.TitleProperties {
			continue
		}
		for ii, it := range sec.Items {
			if it.Name != kubeletVersionItem {
				continue
			}
			it.Status = n.status()
			sup := model.StatusNone
			switch n.state {
			case k8seol.EndOfLife:
				sup = model.StatusWarning
			case k8seol.Maintenance:
				sup = model.StatusPending
			}
			add := []model.DetailItem{it, {Name: upstreamSupportItem, Value: n.short, Status: sup}}
			if n.skewed {
				add = append(add, model.DetailItem{Name: versionSkewItem, Value: n.skew.Short(), Status: model.StatusWarning})
			}
			items := make([]model.DetailItem, 0, len(sec.Items)+len(add)-1)
			items = append(items, sec.Items[:ii]...)
			items = append(items, add...)
			sec.Items = append(items, sec.Items[ii+1:]...)
			return
		}
	}
}
