package model

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// RelatedIndex declares an index over the cached objects of another kind, for cells and drawer
// sections that show objects of that kind: a PVC's pods (pods by the claims they mount), a node's
// pods (pods by node name). Indexes are declared once, in package kinds, and named by profiles in
// KindProfile.Needs; the engine builds them from the cache (architecture §4.4).
type RelatedIndex struct {
	// Kind is the kind key of the indexed objects: "pods".
	Kind string
	// Name names the index, unique per kind: "byNode", "byClaim".
	Name string
	// Keys returns the keys an object is found under: a pod's node name; a pod's
	// "<namespace>/<claim>" for each claim it mounts. It must tolerate any content without
	// panicking.
	Keys func(obj *unstructured.Unstructured) []string
	// Sum, when set, adds up a contribution of each object under its keys, kept current by the
	// kind's events (TotalsOf): what a namespace's pods request. It must tolerate any content
	// without panicking.
	Sum func(obj *unstructured.Unstructured) Totals
}

// Totals are the numbers an index adds up per key (RelatedIndex.Sum), by position; the index's
// declaration says what each position holds.
type Totals [8]int64

// Add returns t plus o, position by position.
func (t Totals) Add(o Totals) Totals {
	for i := range t {
		t[i] += o[i]
	}
	return t
}

// Sub returns t minus o, position by position.
func (t Totals) Sub(o Totals) Totals {
	for i := range t {
		t[i] -= o[i]
	}
	return t
}

// RelatedTotals is a Related that also serves the sums of the indexes that declare one.
type RelatedTotals interface {
	// Totals returns what the index's Sum adds up under key, and true once the index's kind is
	// listed: a key without objects then has zero totals. It never blocks on the API server.
	Totals(index RelatedIndex, key string) (Totals, bool)
}

// TotalsOf returns what rel's index adds up under key, and false while rel cannot tell (the
// index's kind not listed yet, not served, refused, or a Related without sums).
func TotalsOf(rel Related, index RelatedIndex, key string) (Totals, bool) {
	if t, ok := rel.(RelatedTotals); ok && index.Sum != nil {
		return t.Totals(index, key)
	}
	return Totals{}, false
}

// ID identifies the index: "<kind>/<name>".
func (ix RelatedIndex) ID() string { return ix.Kind + "/" + ix.Name }

// Related looks up the objects of other kinds through the indexes a profile declares in Needs. It
// never blocks and never calls the API server. Objects are shared with the cache and must not be
// modified.
type Related interface {
	// Objects returns the objects the index maps key to, in no particular order; nil while the
	// index's kind is still listing, when the cluster does not serve it, or when the profile did
	// not declare the index.
	Objects(index RelatedIndex, key string) []*unstructured.Unstructured
	// Listed reports whether the profile declared the index, the cluster serves its kind and the
	// kind finished its initial list: only then does an empty Objects mean that no object has the
	// key, rather than that the objects are not known yet (a binding to a cluster role that does
	// not exist warns; one whose roles are still listing does not).
	Listed(index RelatedIndex) bool
}

// noRelated answers every lookup with nothing, and knows nothing.
type noRelated struct{}

func (noRelated) Objects(RelatedIndex, string) []*unstructured.Unstructured { return nil }
func (noRelated) Listed(RelatedIndex) bool                                  { return false }

// NoRelated is a Related without objects, for rows and details built outside a view.
var NoRelated Related = noRelated{}

func relatedOrNone(rel Related) Related {
	if rel == nil {
		return NoRelated
	}
	return rel
}

// DrawerNeeds returns the indexes the kind's drawer reads: Needs, then each of DetailNeeds that
// Needs does not name.
func (p *KindProfile) DrawerNeeds() []RelatedIndex {
	if len(p.DetailNeeds) == 0 {
		return p.Needs
	}
	out := append([]RelatedIndex(nil), p.Needs...)
	for _, ix := range p.DetailNeeds {
		dup := false
		for _, n := range out {
			if n.ID() == ix.ID() {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, ix)
		}
	}
	return out
}

// DetailSections returns a profile's own drawer sections for obj: DetailWith with rel, else
// Detail. It may panic if the profile does; callers recover.
func (p *KindProfile) DetailSections(obj *unstructured.Unstructured, rel Related) []DetailSection {
	switch {
	case p.DetailWith != nil:
		return p.DetailWith(obj, relatedOrNone(rel))
	case p.Detail != nil:
		return p.Detail(obj)
	}
	return nil
}
