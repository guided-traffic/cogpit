package engine

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// makeCard makes a card of alerts given as labels and their summary and description, as the board
// groups them.
func makeCard(alerts ...[3]any) AlertCard {
	ix := alertKindIndex{byKey: map[string]*model.KindProfile{}, byGroup: map[string]*model.KindProfile{}}
	var c AlertCard
	for _, a := range alerts {
		labels := cardLabels(a[0].(map[string]string))
		c.Alerts = append(c.Alerts, AlertItem{Name: labels["alertname"], Summary: a[1].(string), Description: a[2].(string),
			Objects: alertObjects(labels, ix), labels: labels})
	}
	c.Count = len(c.Alerts)
	finishCard(&c, nil)
	return c
}

// text writes a card's text with its slots as ‹key›.
func text(parts []AlertTextPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Slot != "" {
			b.WriteString("‹" + p.Slot + "›")
		} else {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func columnKeys(c AlertCard) []string {
	var out []string
	for _, col := range c.Columns {
		out = append(out, col.Key)
	}
	return out
}

func alertLabels(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// The owner's example: CloudNativePG's PGReplication on two pods of one namespace, the same text,
// the same container. The text shows once, the namespace in the head, the pods as the one column.
func TestCardSharedText(t *testing.T) {
	pg := func(pod string) [3]any {
		return [3]any{alertLabels("alertname", "PGReplication", "severity", "warning", "namespace", "orders", "pod", pod,
			"container", "postgres", "job", "orders/orders-pg", "instance", "10.0.3.4:9187"),
			"The standby is lagging behind the primary", "Replica " + pod + " is lagging behind the primary"}
	}
	c := makeCard(pg("orders-pg-4"), pg("orders-pg-5"))
	if text(c.Summary) != "The standby is lagging behind the primary" || text(c.Description) != "Replica ‹object› is lagging behind the primary" {
		t.Errorf("texts %q, %q", text(c.Summary), text(c.Description))
	}
	if len(c.Context) != 1 || c.Context[0].Ref.Name != "orders" || !slices.Equal(columnKeys(c), []string{ColumnObject}) ||
		c.Columns[0].Title != "Pod" || c.Columns[0].Link != ColumnObject {
		t.Errorf("head %+v, columns %+v", c.Context, c.Columns)
	}
	for i, pod := range []string{"orders-pg-4", "orders-pg-5"} {
		it := c.Alerts[i]
		if it.Values[ColumnObject] != pod || it.Summary != "" || it.Description != "" || it.Message != "" || it.Detail != "" {
			t.Errorf("row %d: %+v", i, it)
		}
	}
}

// One alert of a host outside the cluster: no object, so the labels its text names are the row
// (the instance and the device), and the text shows their places; a label the text does not name
// (host) is not shown.
func TestCardOneAlertNamedLabels(t *testing.T) {
	c := makeCard([3]any{alertLabels("alertname", "CephNodeNetworkPacketErrors", "severity", "warning", "instance", "pve.example.net:9100",
		"device", "enp1s0", "host", "node-a", "os", "linux", "type", "ceph_default", "job", "pve"),
		"One or more NICs reports packet errors",
		"Node pve.example.net:9100 experiences packet errors > 0.01% or > 10 packets/s on interface enp1s0."})
	if !slices.Equal(columnKeys(c), []string{"instance", "device"}) || len(c.Context) != 0 {
		t.Errorf("columns %v, head %+v", columnKeys(c), c.Context)
	}
	if got := text(c.Description); got != "Node ‹instance› experiences packet errors > 0.01% or > 10 packets/s on interface ‹device›." {
		t.Errorf("description %q", got)
	}
	if v := c.Alerts[0].Values; v["instance"] != "pve.example.net:9100" || v["device"] != "enp1s0" {
		t.Errorf("values %v", v)
	}
}

// Texts that differ in a number besides the labels: the number becomes a value column; a label value
// inside a longer name ("shop" in "shop-db") stays text.
func TestCardNumbers(t *testing.T) {
	pvc := func(ns, claim, free string) [3]any {
		return [3]any{alertLabels("alertname", "KubePersistentVolumeFillingUp", "severity", "critical", "namespace", ns,
			"persistentvolumeclaim", claim, "job", "kubelet"),
			"PersistentVolume is filling up.",
			"The PersistentVolume claimed by " + claim + " in Namespace " + ns + " is only " + free + " free."}
	}
	c := makeCard(pvc("shop", "shop-db", "3.2%"), pvc("search", "index", "12.5%"))
	if !slices.Equal(columnKeys(c), []string{ColumnNamespace, ColumnObject, "value:1"}) {
		t.Fatalf("columns %v", columnKeys(c))
	}
	if got := text(c.Description); got != "The PersistentVolume claimed by ‹object› in Namespace ‹namespace› is only ‹value:1›% free." {
		t.Errorf("description %q", got)
	}
	if c.Alerts[0].Values["value:1"] != "3.2" || c.Alerts[1].Values["value:1"] != "12.5" || c.Alerts[0].Detail != "" {
		t.Errorf("rows %+v", c.Alerts)
	}
	for i, it := range c.Alerts {
		if fill(c.Description, it.Values) != []string{
			"The PersistentVolume claimed by shop-db in Namespace shop is only 3.2% free.",
			"The PersistentVolume claimed by index in Namespace search is only 12.5% free."}[i] {
			t.Errorf("row %d gives back %q", i, fill(c.Description, it.Values))
		}
	}
}

// A text the card's does not give back stays the row's own: the board never shows a text the cluster
// did not send.
func TestCardOwnText(t *testing.T) {
	pod := func(name, summary string) [3]any {
		return [3]any{alertLabels("alertname", "KubePodNotReady", "severity", "warning", "namespace", "shop", "pod", name), summary, ""}
	}
	c := makeCard(pod("a-1", "Pod a-1 is not ready."), pod("b-2", "Pod b-2 is not ready."), pod("c-3", "Something else entirely happened here."))
	if text(c.Summary) != "Pod ‹object› is not ready." || len(c.Description) != 0 {
		t.Errorf("summary %q, description %v", text(c.Summary), c.Description)
	}
	if c.Alerts[0].Message != "" || c.Alerts[1].Message != "" || c.Alerts[2].Message != "Something else entirely happened here." {
		t.Errorf("messages %q %q %q", c.Alerts[0].Message, c.Alerts[1].Message, c.Alerts[2].Message)
	}
}

// exported_namespace (honor_labels false) is the object's namespace; the scraped exporter's pod and
// container, the job and the instance of the scrape target are no columns.
func TestCardLabels(t *testing.T) {
	flux := func(name, ns, pod string) [3]any {
		return [3]any{alertLabels("alertname", "KustomizationNotReady", "severity", "warning", "namespace", "monitoring",
			"exported_namespace", ns, "customresource_group", "kustomize.toolkit.fluxcd.io", "customresource_kind", "Kustomization",
			"customresource_version", "v1", "name", name, "job", "kube-state-metrics", "pod", pod, "container", "kube-state-metrics",
			"instance", "10.0.0."+name[:1]+":8080", "reason", "ReconciliationFailed"),
			"Kustomization " + name + " is not ready", ""}
	}
	c := makeCard(flux("apps", "flux-system", "kube-state-metrics-1"), flux("tenants", "flux-system", "kube-state-metrics-2"))
	if len(c.Context) != 1 || c.Context[0].Ref.Name != "flux-system" || !slices.Equal(columnKeys(c), []string{ColumnObject}) ||
		c.Columns[0].Title != "Kustomization" {
		t.Errorf("head %+v, columns %+v", c.Context, c.Columns)
	}
	if text(c.Summary) != "Kustomization ‹object› is not ready" {
		t.Errorf("summary %q", text(c.Summary))
	}
}

func TestMarkSlots(t *testing.T) {
	for _, c := range []struct {
		text string
		vals map[string]string
		want string
	}{
		{"Pod orders/orders-pg-4 lags", map[string]string{"namespace": "orders", "object": "orders-pg-4"}, "Pod ‹namespace›/‹object› lags"},
		{"Filesystem on /dev/sda1, mounted on /, is full", map[string]string{"device": "/dev/sda1", "mountpoint": "/"}, "Filesystem on ‹device›, mounted on ‹mountpoint›, is full"},
		{"node n1 failed 1 times", map[string]string{"node": "n1", "count": "1"}, "node ‹node› failed 1 times"},
		{"ready is true", map[string]string{"ready": "true"}, "ready is true"},
	} {
		keys := []string{"namespace", "object", "device", "mountpoint", "node", "count", "ready"}
		got := strings.NewReplacer(slotMark+"namespace"+slotMark, "‹namespace›", slotMark+"object"+slotMark, "‹object›",
			slotMark+"device"+slotMark, "‹device›", slotMark+"mountpoint"+slotMark, "‹mountpoint›", slotMark+"node"+slotMark, "‹node›").
			Replace(markSlots(c.text, c.vals, keys))
		if got != c.want {
			t.Errorf("%q: %q, want %q", c.text, got, c.want)
		}
	}
	if pre, suf := commonEnds([]string{"12", "15"}); pre != "" || suf != "" {
		t.Errorf("commonEnds cut into a number: %q %q", pre, suf)
	}
	if pre, suf := commonEnds([]string{"(32s).", "(4s)."}); pre != "(" || suf != "s)." {
		t.Errorf("commonEnds: %q %q", pre, suf)
	}
}

// The object that controls the alerts' pods goes into the card's head when all share it (the owner's
// CloudNativePG Cluster), and nowhere when they differ.
func TestCardOwner(t *testing.T) {
	owners := map[string]string{"orders-pg-4": "orders-pg", "orders-pg-5": "orders-pg", "other-1": "other"}
	ownerOf := func(o AlertObject) *AlertObject {
		if o.Label != "pod" || owners[o.Ref.Name] == "" {
			return nil
		}
		return &AlertObject{Label: "owner", Ref: model.ObjectRef{Kind: "clusters.postgresql.cnpg.io", Namespace: o.Ref.Namespace, Name: owners[o.Ref.Name]}, Title: "Cluster"}
	}
	card := func(pods ...string) AlertCard {
		ix := alertKindIndex{byKey: map[string]*model.KindProfile{}, byGroup: map[string]*model.KindProfile{}}
		var c AlertCard
		for _, pod := range pods {
			labels := cardLabels(alertLabels("alertname", "PGReplication", "namespace", "orders", "pod", pod))
			c.Alerts = append(c.Alerts, AlertItem{Name: "PGReplication", Summary: "lag", Objects: alertObjects(labels, ix), labels: labels})
		}
		finishCard(&c, ownerOf)
		return c
	}
	c := card("orders-pg-4", "orders-pg-5")
	if len(c.Context) != 2 || c.Context[1].Title != "Cluster" || c.Context[1].Ref.Name != "orders-pg" {
		t.Errorf("head %+v", c.Context)
	}
	if c = card("orders-pg-4", "other-1"); len(c.Context) != 1 {
		t.Errorf("owners differ, head %+v", c.Context)
	}
}

// The pods' controllers come from what the cache holds (peeked: no list, no watch of their own), a
// ReplicaSet's own controller after it.
func TestPodOwnersFromTheCache(t *testing.T) {
	own := func(apiVersion, kind, name string) map[string]any {
		return map[string]any{"metadata": map[string]any{"ownerReferences": []any{map[string]any{
			"apiVersion": apiVersion, "kind": kind, "name": name, "uid": "u-" + name, "controller": true}}}}
	}
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Object("v1", "Pod", "orders", "orders-pg-4", own("postgresql.cnpg.io/v1", "Cluster", "orders-pg")))
	src.Set(testkit.PodsGVR, testkit.Object("v1", "Pod", "shop", "checkout-7d9f-x", own("apps/v1", "ReplicaSet", "checkout-7d9f")))
	rsGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	src.Set(rsGVR, testkit.Object("apps/v1", "ReplicaSet", "shop", "checkout-7d9f", own("apps/v1", "Deployment", "checkout")))
	c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
	defer c.Close()
	b := &Backend{Context: "test", Cache: c}
	if r := b.podOwners()("orders", "orders-pg-4"); r.kind != "" || src.Lists() != 0 {
		t.Fatalf("without a pods scope: %+v, %d lists", r, src.Lists())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, gvr := range []schema.GroupVersionResource{testkit.PodsGVR, rsGVR} {
		h := c.Acquire(cache.Scope{GVR: gvr})
		defer h.Release()
		if !h.WaitForSync(ctx) {
			t.Fatal("not synced")
		}
	}
	owners := b.podOwners()
	if r := owners("orders", "orders-pg-4"); r.kind != "Cluster" || r.name != "orders-pg" {
		t.Errorf("CNPG pod: %+v", r)
	}
	if r := owners("shop", "checkout-7d9f-x"); r.kind != "Deployment" || r.name != "checkout" {
		t.Errorf("a Deployment's pod: %+v", r)
	}
}
