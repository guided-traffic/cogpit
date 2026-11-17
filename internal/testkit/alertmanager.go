package testkit

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// FakeAlertmanager is Alertmanager's API v2 alerts endpoint (GET /api/v2/alerts with its active,
// silenced and inhibited filters) and its silences (GET and POST /api/v2/silences, DELETE
// /api/v2/silence/<id>: D-166), to put behind the fake API server's service proxy (SetProxy) or an
// httptest server (D-77). It answers what Set gave it, in Alertmanager's gettableAlert shape, an
// alert silenced by FakeAlert.Silenced or by an active silence it holds, and records the requests
// it served.
type FakeAlertmanager struct {
	mu       sync.Mutex
	alerts   []FakeAlert
	silences []FakeSilence
	seq      int // the last silence id handed out
	// Clock is the time silences are active, pending or expired at; nil: time.Now. A render's fixed
	// clock (the window's) makes their states and ends the window's.
	Clock    func() time.Time
	status   int    // when set, every request gets this status
	body     []byte // when set, every request gets this body
	prefix   string // the route prefix it serves under
	requests []string
}

// FakeAlert is one alert of a FakeAlertmanager.
type FakeAlert struct {
	Labels      map[string]string
	Annotations map[string]string
	StartsAt    time.Time
	Silenced    bool // state suppressed, silencedBy set: left out with silenced=false
	Inhibited   bool // state suppressed, inhibitedBy set: left out with inhibited=false
}

// FakeSilence is one silence of a FakeAlertmanager, in Alertmanager's gettableSilence shape.
type FakeSilence struct {
	ID        string           `json:"id"`
	Matchers  []map[string]any `json:"matchers"` // name, value, isRegex, isEqual
	StartsAt  time.Time        `json:"startsAt"`
	EndsAt    time.Time        `json:"endsAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	CreatedBy string           `json:"createdBy"`
	Comment   string           `json:"comment"`
}

func (f *FakeAlertmanager) now() time.Time {
	if f.Clock != nil {
		return f.Clock()
	}
	return time.Now()
}

// state is the silence's state at now: pending, active or expired.
func (s FakeSilence) state(now time.Time) string {
	switch {
	case now.Before(s.StartsAt):
		return "pending"
	case now.Before(s.EndsAt):
		return "active"
	}
	return "expired"
}

// matches reports whether every matcher matches labels, Alertmanager's way: the regular
// expressions anchored, a missing label empty.
func (s FakeSilence) matches(labels map[string]string) bool {
	if len(s.Matchers) == 0 {
		return false
	}
	for _, m := range s.Matchers {
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		isRegex, _ := m["isRegex"].(bool)
		isEqual := true
		if b, ok := m["isEqual"].(bool); ok {
			isEqual = b
		}
		hit := labels[name] == value
		if isRegex {
			re, err := regexp.Compile("^(?:" + value + ")$")
			hit = err == nil && re.MatchString(labels[name])
		}
		if hit != isEqual {
			return false
		}
	}
	return true
}

// SetSilences replaces the silences.
func (f *FakeAlertmanager) SetSilences(ss ...FakeSilence) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.silences = slices.Clone(ss)
}

// Silences returns the silences it holds.
func (f *FakeAlertmanager) Silences() []FakeSilence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.silences)
}

// Set replaces the alerts.
func (f *FakeAlertmanager) Set(alerts ...FakeAlert) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = slices.Clone(alerts)
}

// Fail makes every request answer status (0: answer again).
func (f *FakeAlertmanager) Fail(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

// Answer makes every request answer body as is (nil: the alerts again).
func (f *FakeAlertmanager) Answer(body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body = body
}

// Prefix makes it serve under a route prefix ("/alertmanager").
func (f *FakeAlertmanager) Prefix(p string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefix = p
}

// Requests returns the requests served, as "path?query".
func (f *FakeAlertmanager) Requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// ServeHTTP implements http.Handler.
func (f *FakeAlertmanager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	status, body, prefix, alerts, silences := f.status, f.body, f.prefix, slices.Clone(f.alerts), slices.Clone(f.silences)
	f.mu.Unlock()
	if status != 0 {
		http.Error(w, "refused", status)
		return
	}
	path := r.URL.Path
	if prefix != "" {
		if !strings.HasPrefix(path, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		path = strings.TrimPrefix(path, prefix)
	}
	switch {
	case path == "/api/v2/silences" && r.Method == http.MethodGet:
		f.listSilences(w, silences)
		return
	case path == "/api/v2/silences" && r.Method == http.MethodPost:
		f.createSilence(w, r)
		return
	case strings.HasPrefix(path, "/api/v2/silence/") && r.Method == http.MethodDelete:
		f.expireSilence(w, strings.TrimPrefix(path, "/api/v2/silence/"))
		return
	case path != "/api/v2/alerts":
		http.NotFound(w, r)
		return
	case r.Method != http.MethodGet:
		http.Error(w, "read only", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if body != nil {
		_, _ = w.Write(body)
		return
	}
	q := r.URL.Query()
	now := f.now()
	out := []map[string]any{}
	for _, a := range alerts {
		state, silencedBy, inhibitedBy := "active", []string{}, []string{}
		if a.Silenced {
			silencedBy = append(silencedBy, "silence-1")
		}
		for _, s := range silences {
			if s.state(now) == "active" && s.matches(a.Labels) {
				silencedBy = append(silencedBy, s.ID)
			}
		}
		if a.Inhibited {
			inhibitedBy = []string{"inhibitor-1"}
		}
		if len(silencedBy) > 0 || len(inhibitedBy) > 0 {
			state = "suppressed"
		}
		if q.Get("silenced") == "false" && len(silencedBy) > 0 || q.Get("inhibited") == "false" && len(inhibitedBy) > 0 {
			continue
		}
		ann := a.Annotations
		if ann == nil {
			ann = map[string]string{}
		}
		out = append(out, map[string]any{
			"labels": a.Labels, "annotations": ann, "fingerprint": fingerprint(a.Labels),
			"startsAt": a.StartsAt.UTC().Format(time.RFC3339Nano), "endsAt": a.StartsAt.Add(24 * time.Hour).UTC().Format(time.RFC3339Nano),
			"updatedAt": a.StartsAt.UTC().Format(time.RFC3339Nano), "generatorURL": "http://prometheus.example/graph",
			"receivers": []map[string]string{{"name": "null"}},
			"status":    map[string]any{"state": state, "silencedBy": silencedBy, "inhibitedBy": inhibitedBy, "mutedBy": []string{}},
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

// listSilences answers GET /api/v2/silences.
func (f *FakeAlertmanager) listSilences(w http.ResponseWriter, silences []FakeSilence) {
	now := f.now()
	out := []map[string]any{}
	for _, s := range silences {
		out = append(out, map[string]any{"id": s.ID, "status": map[string]any{"state": s.state(now)}, "matchers": s.Matchers,
			"startsAt": s.StartsAt.UTC().Format(time.RFC3339Nano), "endsAt": s.EndsAt.UTC().Format(time.RFC3339Nano),
			"updatedAt": s.UpdatedAt.UTC().Format(time.RFC3339Nano), "createdBy": s.CreatedBy, "comment": s.Comment})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// createSilence answers POST /api/v2/silences: a silence of at least one matcher that does not
// match the empty value, an end after its start and in the future, an author and a comment, as
// Alertmanager validates it; its answer the new id.
func (f *FakeAlertmanager) createSilence(w http.ResponseWriter, r *http.Request) {
	var s FakeSilence
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, `"silence invalid: `+err.Error()+`"`, http.StatusBadRequest)
		return
	}
	now := f.now()
	switch {
	case len(s.Matchers) == 0:
		http.Error(w, `"silence invalid: at least one matcher required"`, http.StatusBadRequest)
		return
	case s.matches(map[string]string{}):
		http.Error(w, `"silence invalid: at least one matcher must not match the empty string"`, http.StatusBadRequest)
		return
	case !s.EndsAt.After(s.StartsAt) || !s.EndsAt.After(now):
		http.Error(w, `"silence invalid: end time must not be before start time or in the past"`, http.StatusBadRequest)
		return
	case s.CreatedBy == "" || s.Comment == "":
		http.Error(w, `"silence invalid: createdBy and comment are required"`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.seq++
	s.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", f.seq)
	s.UpdatedAt = now
	if s.StartsAt.Before(now) {
		s.StartsAt = now
	}
	f.silences = append(f.silences, s)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"silenceID": s.ID})
}

// expireSilence answers DELETE /api/v2/silence/<id>: it ends now; an expired one stays as it is
// and an unknown id is 404, as Alertmanager 0.28 answers them.
func (f *FakeAlertmanager) expireSilence(w http.ResponseWriter, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	for i, s := range f.silences {
		if s.ID != id {
			continue
		}
		if s.state(now) == "expired" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if s.StartsAt.After(now) {
			f.silences[i].StartsAt = now
		}
		f.silences[i].EndsAt, f.silences[i].UpdatedAt = now, now
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Error(w, `"silence `+id+` not found"`, http.StatusNotFound)
}

// fingerprint is a hash of the labels, as Alertmanager's (its own hash differs; only stability
// matters here).
func fingerprint(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	h := fnv.New64a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k + "\x00" + labels[k] + "\x00"))
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

// SampleAlerts is a synthetic set of alerts shaped like a real cluster's Alertmanager answer (D-77,
// the owner's test cluster read on 2026-10-17; no name or text of it): kube-prometheus' rules over
// kube-state-metrics (the exporter's own pod and service on a workload's alert), kube-state-metrics'
// custom resource state of Flux objects at severity major, an x509 exporter's certificate alerts,
// a pod crash looping in three pods, runbook links, one alert without a severity, an info alert,
// and the meta alerts Watchdog and InfoInhibitor and a silenced alert, which are left out: 24 shown
// (critical 3, warning 18 with major and the unlabelled one, info 3). The times are before now.
func SampleAlerts(now time.Time) []FakeAlert {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	runbook := func(name string) map[string]string {
		return map[string]string{"runbook_url": "https://runbooks.example.com/kubernetes/" + strings.ToLower(name)}
	}
	ksm := func(l map[string]string) map[string]string {
		l["job"], l["service"], l["container"] = "kube-state-metrics", "kube-state-metrics", "kube-state-metrics"
		l["pod"], l["instance"], l["endpoint"] = "kube-state-metrics-6d8f7c9b5-x2kqp", "10.0.4.17:8080", "http"
		l["cluster"], l["environment"] = "example", "staging"
		return l
	}
	with := func(base map[string]string, extra map[string]string) map[string]string {
		for k, v := range extra {
			base[k] = v
		}
		return base
	}
	var out []FakeAlert
	add := func(labels, ann map[string]string, at time.Time) {
		out = append(out, FakeAlert{Labels: labels, Annotations: ann, StartsAt: at})
	}

	// critical
	for i, cert := range []string{"shop-tls", "payments-api-tls"} {
		ns := []string{"shop", "payments"}[i]
		add(map[string]string{"alertname": "CertificateExpiration", "severity": "critical", "namespace": "monitoring",
			"pod": "x509-certificate-exporter-5c9d8b7f6-mq2lt", "job": "x509-certificate-exporter", "service": "x509-certificate-exporter",
			"container": "x509-certificate-exporter", "secret_name": cert, "secret_namespace": ns, "cluster": "example", "environment": "staging"},
			map[string]string{"summary": "Certificate is expiring or expired",
				"description": "The certificate in secret " + ns + "/" + cert + " expires within 7 days."}, ago(time.Duration(26+i)*time.Hour))
	}
	add(ksm(map[string]string{"alertname": "KubePersistentVolumeFillingUp", "severity": "critical", "namespace": "shop",
		"persistentvolumeclaim": "data-postgres-0"}),
		with(runbook("KubePersistentVolumeFillingUp"), map[string]string{"summary": "PersistentVolume is filling up.",
			"description": "The PersistentVolume claimed by data-postgres-0 in namespace shop is only 2.1% free."}), ago(35*time.Minute))

	// major: Flux objects through kube-state-metrics' custom resource state
	for i, name := range []string{"apps", "infrastructure", "monitoring", "ingress", "policies", "tenants"} {
		add(ksm(map[string]string{"alertname": "KustomizationNotReady", "severity": "major", "namespace": "flux-system",
			"customresource_group": "kustomize.toolkit.fluxcd.io", "customresource_kind": "Kustomization", "customresource_version": "v1",
			"name": name, "service": "fluxcd", "status": "False", "type": "Ready"}),
			map[string]string{"summary": "Kustomization " + name + " in namespace flux-system is not ready",
				"description": "Kustomization " + name + " has been failing to reconcile for more than 10 minutes."}, ago(time.Duration(3+i)*time.Hour))
	}
	for i, name := range []string{"oauth2-proxy", "checkout"} {
		ns := []string{"iam", "shop"}[i]
		add(ksm(map[string]string{"alertname": "HelmReleaseNotReady", "severity": "major", "namespace": ns,
			"customresource_group": "helm.toolkit.fluxcd.io", "customresource_kind": "HelmRelease", "customresource_version": "v2",
			"name": name, "status": "False", "type": "Ready"}),
			map[string]string{"summary": "HelmRelease " + name + " in namespace " + ns + " is not ready"}, ago(time.Duration(50+10*i)*time.Minute))
	}

	// warning
	for i, pod := range []string{"checkout-7d9f8c6b5-2xkqp", "checkout-7d9f8c6b5-9wzrt", "cart-5b6c7d8e9-l4mnp"} {
		l := ksm(map[string]string{"alertname": "KubePodCrashLooping", "severity": "warning", "namespace": "shop", "reason": "CrashLoopBackOff"})
		l["pod"], l["container"] = pod, "api"
		add(l, with(runbook("KubePodCrashLooping"), map[string]string{"summary": "Pod is crash looping.",
			"description": "Pod shop/" + pod + " (api) is in waiting state (reason: \"CrashLoopBackOff\")."}), ago(time.Duration(12+i*7)*time.Minute))
	}
	for i, pod := range []string{"search-indexer-6f7d8c9b-q8wxz", "reports-84c9d7f6b-tt5ks"} {
		add(map[string]string{"alertname": "KubePodNotReady", "severity": "warning", "namespace": []string{"search", "reports"}[i],
			"pod": pod, "job": "kube-state-metrics", "cluster": "example", "environment": "staging"},
			with(runbook("KubePodNotReady"), map[string]string{"summary": "Pod has been in a non-ready state for more than 15 minutes."}),
			ago(time.Duration(2+i)*time.Hour))
	}
	add(ksm(map[string]string{"alertname": "KubeDeploymentReplicasMismatch", "severity": "warning", "namespace": "shop", "deployment": "checkout"}),
		with(runbook("KubeDeploymentReplicasMismatch"), map[string]string{"summary": "Deployment has not matched the expected number of replicas."}),
		ago(40*time.Minute))
	add(ksm(map[string]string{"alertname": "KubeDaemonSetRolloutStuck", "severity": "warning", "namespace": "ingress", "daemonset": "edge-proxy"}),
		with(runbook("KubeDaemonSetRolloutStuck"), map[string]string{"summary": "DaemonSet rollout is stuck."}), ago(5*time.Hour))
	add(map[string]string{"alertname": "TargetDown", "severity": "warning", "namespace": "ingress", "job": "edge-proxy",
		"service": "edge-proxy-metrics", "cluster": "example"},
		map[string]string{"summary": "One or more targets are unreachable.", "description": "100% of the edge-proxy/edge-proxy targets in ingress namespace are down."},
		ago(5*time.Hour))
	add(map[string]string{"alertname": "CephPGImbalance", "severity": "warning", "namespace": "rook-ceph",
		"pod": "rook-ceph-exporter-node-a-7c8d9e6f5-b2v4n", "job": "rook-ceph-exporter", "service": "rook-ceph-exporter",
		"container": "ceph-exporter", "ceph_daemon": "osd.2", "cluster": "example"},
		map[string]string{"summary": "PGs are not balanced across OSDs"}, ago(4*24*time.Hour))
	add(map[string]string{"alertname": "BackupJobLate", "namespace": "velero", "cluster": "example"},
		map[string]string{"summary": "The nightly backup has not finished."}, ago(9*time.Hour))

	// info
	for i, pod := range []string{"search-indexer-6f7d8c9b-q8wxz", "reports-84c9d7f6b-tt5ks"} {
		add(map[string]string{"alertname": "CPUThrottlingHigh", "severity": "info", "namespace": []string{"search", "reports"}[i],
			"pod": pod, "container": "worker", "job": "kubelet", "cluster": "example"},
			with(runbook("CPUThrottlingHigh"), map[string]string{"summary": "Processes experience elevated CPU throttling."}),
			ago(time.Duration(1+i)*24*time.Hour))
	}
	add(map[string]string{"alertname": "KubeQuotaAlmostFull", "severity": "info", "namespace": "shop", "resource": "requests.cpu", "cluster": "example"},
		map[string]string{"summary": "Namespace quota is going to be full."}, ago(3*24*time.Hour))

	// left out: the meta alerts, severity none, a silenced and an inhibited alert
	add(map[string]string{"alertname": "Watchdog", "severity": "none"}, map[string]string{"summary": "An alert that should always be firing."}, ago(10*24*time.Hour))
	add(map[string]string{"alertname": "InfoInhibitor", "severity": "none"}, nil, ago(10*24*time.Hour))
	out = append(out, FakeAlert{Labels: map[string]string{"alertname": "KubeJobFailed", "severity": "warning", "namespace": "batch", "job_name": "cleanup"},
		Annotations: map[string]string{"summary": "Job failed to complete."}, StartsAt: ago(time.Hour), Silenced: true})
	out = append(out, FakeAlert{Labels: map[string]string{"alertname": "KubeCPUOvercommit", "severity": "info"},
		Annotations: map[string]string{"summary": "Cluster has overcommitted CPU resource requests."}, StartsAt: ago(time.Hour), Inhibited: true})
	return out
}
