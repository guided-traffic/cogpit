//nolint:goconst // Release values: Kubernetes field names and values read best as literals, as in a values file.
package demo

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The demo cluster's Helm releases: the records Helm keeps of three releases with a history, in
// Helm's own format, so that the Releases view, a release's drawer and its revisions show without
// a cluster. Two releases are kept in Secrets, Helm's default driver, one in ConfigMaps
// (HELM_DRIVER=configmap); the demo's Secrets and ConfigMaps hold nothing else. Each revision is a
// record of its own, named sh.helm.release.v1.<release>.v<revision> and labelled as Helm's driver
// labels it; its payload is Helm's encoding of the release, base64 of gzip of its JSON, which a
// Secret's data holds once more in base64, as every Secret value. A revision's manifest and notes
// are its chart rendered with its values (helmcharts.go).
//
// The records are static: the churn never changes them. Nothing in them comes from the random
// generator, so the rest of the cluster stays as it was, and the same Now gives byte-identical
// records. Their times lie days before Now. The objects the manifests describe are not in the
// demo cluster: only the records are.

// Helm's record format (helm.sh/helm/v4/pkg/storage/driver).
const (
	helmOwner      = "helm"
	helmSecretType = "helm.sh/release.v1"
)

// Helm's descriptions of a revision that succeeded (pkg/action).
const (
	descInstall = "Install complete"
	descUpgrade = "Upgrade complete"
)

// demoRelease is one release and its history, oldest revision first: revision i+1 is
// revisions[i].
type demoRelease struct {
	name, namespace string
	configMaps      bool // kept in ConfigMaps (HELM_DRIVER=configmap), else in Secrets
	revisions       []demoRevision
}

// demoRevision is one revision of a release.
type demoRevision struct {
	chart       *demoChart
	config      map[string]any // the user-supplied values
	status      common.Status
	description string
	deployed    time.Time // Info.LastDeployed, and when its record was created
	modified    time.Time // when Helm last updated its record (its status changed); zero: never
	// rollback is the revision a rollback restored: this revision has its chart, values, manifest
	// and notes, as Helm's rollback copies them. Zero for an install or an upgrade.
	rollback int
}

// buildHelmReleases adds the release records, each release only where its namespace exists and
// is not being deleted (a small demo cluster has fewer namespaces). Called with c.mu held.
func (c *Cluster) buildHelmReleases(now time.Time) {
	for _, rel := range demoReleases(now) {
		if ns := c.namespace(rel.namespace); ns == nil || ns.terminating {
			continue
		}
		records, err := rel.records()
		if err != nil { // a bug in a chart's template: the release is left out, the demo runs on
			slog.Error("demo: Helm release left out", "release", rel.name, "err", err)
			continue
		}
		k := kindSecrets
		if rel.configMaps {
			k = kindConfigMaps
		}
		for _, r := range records {
			c.put(k, r)
		}
	}
}

// demoReleases are the demo's releases, their times relative to now.
func demoReleases(now time.Time) []demoRelease {
	return []demoRelease{shopWebRelease(now), cacheRelease(now), searchAPIRelease(now)}
}

// releases returns a release's revisions as Helm's release objects, oldest first.
func (rel *demoRelease) releases() ([]*rspb.Release, error) {
	out := make([]*rspb.Release, 0, len(rel.revisions))
	first := rel.revisions[0].deployed
	for i, rev := range rel.revisions {
		number := i + 1
		rls := &rspb.Release{
			Name: rel.name, Namespace: rel.namespace, Version: number,
			Info: &rspb.Info{
				FirstDeployed: first, LastDeployed: rev.deployed,
				Description: rev.description, Status: rev.status, RollbackRevision: rev.rollback,
			},
		}
		if rev.rollback > 0 {
			if rev.rollback >= number {
				return nil, fmt.Errorf("revision %d rolls back to %d", number, rev.rollback)
			}
			prev := out[rev.rollback-1]
			rls.Chart, rls.Config, rls.Manifest, rls.Info.Notes = prev.Chart, prev.Config, prev.Manifest, prev.Info.Notes
		} else {
			manifest, notes, err := rev.chart.render(rel.name, rel.namespace, number, rev.config)
			if err != nil {
				return nil, fmt.Errorf("revision %d: %w", number, err)
			}
			meta := *rev.chart.meta
			rls.Chart = &chart.Chart{Metadata: &meta, Values: rev.chart.values}
			rls.Config, rls.Manifest, rls.Info.Notes = rev.config, manifest, notes
		}
		out = append(out, rls)
	}
	return out, nil
}

// records returns a release's records, oldest revision first.
func (rel *demoRelease) records() ([]*unstructured.Unstructured, error) {
	releases, err := rel.releases()
	if err != nil {
		return nil, err
	}
	out := make([]*unstructured.Unstructured, 0, len(releases))
	for i, rls := range releases {
		r, err := rel.record(rls, rel.revisions[i])
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// record is the Secret or ConfigMap Helm's driver writes for one revision.
func (rel *demoRelease) record(rls *rspb.Release, rev demoRevision) (*unstructured.Unstructured, error) {
	payload, err := helmPayload(rls)
	if err != nil {
		return nil, fmt.Errorf("revision %d: %w", rls.Version, err)
	}
	name := "sh.helm.release.v1." + rls.Name + ".v" + strconv.Itoa(rls.Version)
	labels := map[string]any{
		"owner": helmOwner, "name": rls.Name, "status": rls.Info.Status.String(), "version": strconv.Itoa(rls.Version),
		"createdAt": strconv.FormatInt(rev.deployed.Unix(), 10),
	}
	if !rev.modified.IsZero() {
		labels["modifiedAt"] = strconv.FormatInt(rev.modified.Unix(), 10)
	}
	kind := "Secret"
	if rel.configMaps {
		kind = "ConfigMap"
	}
	meta := map[string]any{
		"name": name, "namespace": rel.namespace, "uid": stableUID(kind, rel.namespace, name),
		"creationTimestamp": ts(rev.deployed), "labels": labels,
	}
	if rel.configMaps {
		return object("v1", kind, meta, map[string]any{"data": map[string]any{"release": payload}}), nil
	}
	return object("v1", kind, meta, map[string]any{
		"type": helmSecretType,
		"data": map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(payload))},
	}), nil
}

// helmPayload encodes a release as Helm's storage drivers do: base64 of the gzip (best
// compression) of its JSON.
func helmPayload(rls *rspb.Release) (string, error) {
	b, err := json.Marshal(rls)
	if err != nil {
		return "", fmt.Errorf("encode the release: %w", err)
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", fmt.Errorf("compress the release: %w", err)
	}
	if _, err := zw.Write(b); err != nil {
		return "", fmt.Errorf("compress the release: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("compress the release: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// stableUID is a version 4 UID made from the parts' hash rather than the random generator: the
// same for the same parts, and unique across kinds, namespaces and names.
func stableUID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// shop-web in demo-shop, kept in Secrets: six revisions. 1 is the install; 2 changes values
// (three replicas, a higher memory limit); 3 a new image tag; 4 a new chart version, 1.5.0,
// whose manifest has a ConfigMap more and the metrics Service less, and a new database password;
// 5 an upgrade that failed (five replicas, a second ingress host); 6 the rollback to 4, deployed.

// shopWebSettings are what the user set for one revision of shop-web.
type shopWebSettings struct {
	replicas    int
	tag, memory string
	hosts       []string
	password    string
	newCheckout bool // config.featureFlags.newCheckout, a value of chart 1.5.0
}

// values are the user-supplied values (helm upgrade --values) of the settings.
func (s shopWebSettings) values() map[string]any {
	hosts := make([]any, 0, len(s.hosts))
	for _, h := range s.hosts {
		hosts = append(hosts, map[string]any{
			"host": h, "paths": []any{map[string]any{"path": "/", "pathType": "Prefix"}},
		})
	}
	v := map[string]any{
		"replicaCount": s.replicas,
		"image":        map[string]any{"repository": shopWebImage, "tag": s.tag},
		"service":      map[string]any{"port": 8080},
		"ingress":      map[string]any{"enabled": true, "className": "nginx", "hosts": hosts},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "250m", "memory": "256Mi"},
			"limits":   map[string]any{"cpu": "1", "memory": s.memory},
		},
		"database": map[string]any{"password": s.password},
		"env":      map[string]any{"LOG_FORMAT": "json", "CART_TTL": "30m"},
	}
	if s.newCheckout {
		v["config"] = map[string]any{"featureFlags": map[string]any{"newCheckout": true}}
	}
	return v
}

func shopWebRelease(now time.Time) demoRelease {
	t := []time.Time{
		now.Add(-21*day + 3*time.Hour), now.Add(-14*day + 5*time.Hour), now.Add(-9*day + 2*time.Hour),
		now.Add(-4*day + 6*time.Hour), now.Add(-2 * day), now.Add(-2*day + 25*time.Minute),
	}
	hosts := []string{"shop.example.com"}
	s1 := shopWebSettings{replicas: 2, tag: "2.3.0", memory: "512Mi", hosts: hosts, password: "demo-shop-password-1"}
	s2 := s1
	s2.replicas, s2.memory = 3, "1Gi"
	s3 := s2
	s3.tag = "2.3.1"
	s4 := s3
	s4.tag, s4.password, s4.newCheckout = "2.4.0", "demo-shop-password-2", true
	s5 := s4
	s5.replicas, s5.hosts = 5, []string{"shop.example.com", "www.shop.example.com"}
	return demoRelease{
		name: "shop-web", namespace: "demo-shop",
		revisions: []demoRevision{
			{chart: shopWeb140, config: s1.values(), status: common.StatusSuperseded, description: descInstall, deployed: t[0], modified: t[1]},
			{chart: shopWeb140, config: s2.values(), status: common.StatusSuperseded, description: descUpgrade, deployed: t[1], modified: t[2]},
			{chart: shopWeb140, config: s3.values(), status: common.StatusSuperseded, description: descUpgrade, deployed: t[2], modified: t[3]},
			{chart: shopWeb150, config: s4.values(), status: common.StatusSuperseded, description: descUpgrade, deployed: t[3], modified: t[5]},
			{
				chart: shopWeb150, config: s5.values(), status: common.StatusFailed, deployed: t[4], modified: t[4].Add(5 * time.Minute),
				description: `Upgrade "shop-web" failed: client rate limiter Wait returned an error: context deadline exceeded`,
			},
			{status: common.StatusDeployed, description: "Rollback to 4", deployed: t[5], rollback: 4},
		},
	}
}

// cache in demo-shop, kept in Secrets: the redis chart, installed, then a new patch version of it
// with the same values (helm upgrade --reuse-values).
func cacheRelease(now time.Time) demoRelease {
	config := map[string]any{
		"architecture": "standalone",
		"auth":         map[string]any{"enabled": false},
		"master": map[string]any{
			"persistence": map[string]any{"size": "2Gi"},
			"resources": map[string]any{
				"requests": map[string]any{"cpu": "100m", "memory": "128Mi"},
				"limits":   map[string]any{"memory": "256Mi"},
			},
		},
		"commonConfiguration": "maxmemory 200mb\nmaxmemory-policy allkeys-lru",
	}
	t1, t2 := now.Add(-18*day+4*time.Hour), now.Add(-6*day+30*time.Minute)
	return demoRelease{
		name: "cache", namespace: "demo-shop",
		revisions: []demoRevision{
			{chart: redis2061, config: config, status: common.StatusSuperseded, description: descInstall, deployed: t1, modified: t2},
			{chart: redis2062, config: config, status: common.StatusDeployed, description: descUpgrade, deployed: t2},
		},
	}
}

// search-api in demo-search, kept in ConfigMaps: installed, scaled with a shorter index refresh,
// then upgraded to chart 0.10.0, which adds a PodDisruptionBudget.
func searchAPIRelease(now time.Time) demoRelease {
	values := func(replicas int, tag, refresh string) map[string]any {
		return map[string]any{
			"replicaCount": replicas,
			"image":        map[string]any{"tag": tag},
			"indexing":     map[string]any{"batchSize": 1000, "refreshInterval": refresh},
			"resources": map[string]any{
				"requests": map[string]any{"cpu": "500m", "memory": "1Gi"},
				"limits":   map[string]any{"memory": "2Gi"},
			},
		}
	}
	t := []time.Time{now.Add(-12*day + time.Hour), now.Add(-8*day + 3*time.Hour), now.Add(-3*day + 2*time.Hour)}
	return demoRelease{
		name: "search-api", namespace: "demo-search", configMaps: true,
		revisions: []demoRevision{
			{chart: searchAPI090, config: values(2, "1.8.0", "30s"), status: common.StatusSuperseded, description: descInstall, deployed: t[0], modified: t[1]},
			{chart: searchAPI090, config: values(3, "1.8.0", "10s"), status: common.StatusSuperseded, description: descUpgrade, deployed: t[1], modified: t[2]},
			{chart: searchAPI0100, config: values(3, "1.9.0", "10s"), status: common.StatusDeployed, description: descUpgrade, deployed: t[2]},
		},
	}
}
