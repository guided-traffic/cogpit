package metrics

import "github.com/guided-traffic/cogpit/internal/model"

// Namespace returns the usage of a namespace's pods summed (the Namespaces list's CPU and Memory,
// D-154), from the fresher of the pods lists that cover it: every pod's, or the namespace's own. A
// namespace without a measured pod is 0, measured; false while no such list is fresh or the
// namespace refused it.
func (h *Hub) Namespace(namespace string) (model.Usage, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best *dataSet
	for _, c := range []Scope{{Kind: KindPods}, {Kind: KindPods, Namespace: namespace}} {
		st := h.scopes[c]
		if st == nil || st.refused || !h.validLocked(st) {
			continue
		}
		if best == nil || st.data.at.After(best.at) {
			best = st.data
		}
	}
	if best == nil {
		return model.Usage{}, false
	}
	return best.namespaceUsage(namespace), true
}

// namespaceUsage is the summed usage of the set's pods in namespace; the sums are made once.
func (d *dataSet) namespaceUsage(namespace string) model.Usage {
	d.nsOnce.Do(func() {
		d.byNamespace = map[string]model.Usage{}
		for _, p := range d.pods {
			u := p.Total()
			if cur, ok := d.byNamespace[p.Namespace]; ok {
				u = cur.Add(u)
			}
			d.byNamespace[p.Namespace] = u
		}
	})
	return d.byNamespace[namespace]
}
