package fyneui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The views that show related objects (architecture §4.4), rendered from the real profiles: the
// Persistent Volume Claims list with its Pods column and a node's drawer with its Pods table.
// PNGs: testdata/pvc_list_*.png, node_pods_drawer_*.png (and node_drawer_*.png, whose cordoned
// node has one pod).

// fixtureRelated serves the related-objects indexes from the fixture's objects by kind key, as the
// engine's index does: an object is found under every key its index's Keys returns.
type fixtureRelated map[string][]*unstructured.Unstructured

func (r fixtureRelated) Listed(ix model.RelatedIndex) bool { _, ok := r[ix.Kind]; return ok }

func (r fixtureRelated) Objects(ix model.RelatedIndex, key string) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, o := range r[ix.Kind] {
		if slices.Contains(ix.Keys(o), key) {
			out = append(out, o)
		}
	}
	return out
}

// fixtureClaims returns claims in every phase and the pods that mount them: one pod (a link),
// three on a shared ReadWriteMany claim (text), none.
func fixtureClaims() (claims, pods []*unstructured.Unstructured) {
	claim := func(ns, name, class, size, phase string, modes []any, age time.Duration, deleting bool) {
		meta := map[string]any{"name": name, "namespace": ns, "uid": "uid-pvc-" + ns + "-" + name, "creationTimestamp": ts(age)}
		if deleting {
			meta["deletionTimestamp"] = ts(time.Minute)
		}
		status := map[string]any{"phase": phase}
		if phase == "Bound" {
			status["capacity"] = map[string]any{"storage": size}
		}
		claims = append(claims, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": meta,
			"spec": map[string]any{
				"storageClassName": class, "accessModes": modes, "volumeName": "pvc-" + name,
				"resources": map[string]any{"requests": map[string]any{"storage": size}},
			},
			"status": status,
		}})
	}
	mount := func(p podSpec, claimNames ...string) {
		o := fixturePodObject(p)
		vols, _, _ := unstructured.NestedSlice(o.Object, "spec", "volumes")
		for _, c := range claimNames {
			vols = append(vols, map[string]any{"name": c, "persistentVolumeClaim": map[string]any{"claimName": c}})
		}
		_ = unstructured.SetNestedSlice(o.Object, vols, "spec", "volumes")
		pods = append(pods, o)
	}
	rwo, rwx := []any{"ReadWriteOnce"}, []any{"ReadWriteMany"}
	for i := range 3 {
		name := fmt.Sprintf("postgres-%d", i)
		claim("payments", "data-"+name, "fast-ssd", "50Gi", "Bound", rwo, time.Duration(30+i)*24*time.Hour, false)
		mount(podSpec{ns: "payments", name: name, ownerKind: "StatefulSet", ownerName: "postgres", node: "worker-a-01", state: podOK,
			containers: []string{"postgres"}, age: 30 * 24 * time.Hour}, "data-"+name)
	}
	claim("media", "shared-uploads", "nfs-shared", "1Ti", "Bound", rwx, 90*24*time.Hour, false)
	for _, n := range []string{"thumbnailer-6f8b9c7d4-q9m2z", "uploader-7d9f8c6b5-bkmqx", "uploader-7d9f8c6b5-clnry"} {
		mount(podSpec{ns: "media", name: n, ownerKind: "ReplicaSet", ownerName: n[:len(n)-6], node: "worker-b-01", state: podOK,
			containers: []string{"app"}, age: 5 * 24 * time.Hour}, "shared-uploads")
	}
	claim("search", "data-indexer-0", "fast-ssd", "200Gi", "Bound", rwo, 12*24*time.Hour, false)
	mount(podSpec{ns: "search", name: "indexer-0", ownerKind: "StatefulSet", ownerName: "indexer", node: "worker-c-01", state: podCrashLoop,
		containers: []string{"app"}, age: 12 * 24 * time.Hour}, "data-indexer-0")
	claim("qa", "scratch", "standard", "10Gi", "Pending", rwo, 2*time.Hour, false)
	claim("analytics", "events-archive", "fast-ssd", "500Gi", "Lost", rwo, 200*24*time.Hour, false)
	claim("staging", "build-cache", "standard", "20Gi", "Bound", rwo, 3*24*time.Hour, true)
	return claims, pods
}

// claimsFake is kindsFake on the Persistent Volume Claims view, with the claim fixture and the
// Storage kinds its links lead to.
func claimsFake(t testing.TB) *fakeEngine {
	f := kindsFake(t, "persistentvolumeclaims")
	f.kinds = append(f.kinds,
		kindInfo{Kind: "persistentvolumeclaims", Title: "Persistent Volume Claims", APIKind: "PersistentVolumeClaim", Version: "v1", Namespaced: true},
		kindInfo{Kind: "persistentvolumes", Title: "Persistent Volumes", APIKind: "PersistentVolume", Version: "v1"},
		kindInfo{Kind: "storageclasses", Title: "Storage Classes", APIKind: "StorageClass", Group: "storage.k8s.io", Version: "v1"})
	claims, pods := fixtureClaims()
	f.tables["persistentvolumeclaims"] = kindSnapshotWith(t, "persistentvolumeclaims", claims, fixtureRelated{"pods": pods})
	return f
}

func renderClaims(t *testing.T, mode ThemeMode, name string) {
	f := claimsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the claims view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == "persistentvolumeclaims" && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderClaimsListDark(t *testing.T)  { renderClaims(t, ThemeDark, "pvc_list_dark.png") }
func TestRenderClaimsListLight(t *testing.T) { renderClaims(t, ThemeLight, "pvc_list_light.png") }

// TestClaimPodsCellLinks: a claim's single pod is a link that navigates to the pod; the claim of
// three pods names them as text; the class is a link.
func TestClaimPodsCellLinks(t *testing.T) {
	f := claimsFake(t)
	sh, _, q := settledShell(t, f, "persistentvolumeclaims")
	podsCol, classCol := columnOf(t, sh, "pods"), columnOf(t, sh, "storageClass")
	single := cellAt(t, sh, rowOf(t, f, "persistentvolumeclaims", "data-postgres-0"), podsCol)
	if single.content.link == nil || single.full != "postgres-0" {
		t.Fatalf("single pod cell %+v", single.content)
	}
	if shared := cellAt(t, sh, rowOf(t, f, "persistentvolumeclaims", "shared-uploads"), podsCol); shared.content.link != nil ||
		shared.full != "thumbnailer-6f8b9c7d4-q9m2z, uploader-7d9f8c6b5-bkmqx, uploader-7d9f8c6b5-clnry" {
		t.Errorf("shared claim's pods cell %+v", shared.content)
	}
	if class := cellAt(t, sh, rowOf(t, f, "persistentvolumeclaims", "data-postgres-0"), classCol); class.content.link == nil {
		t.Errorf("storage class cell is not a link: %+v", class.content)
	}
	test.TapAt(single, fyne.NewPos(single.text.Position().X+4, single.Size().Height/2))
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	if got := decodeParams[refParams](t, f.callsOf("ui.navigate")[0]).Ref; got != (model.ObjectRef{Context: "demo", Kind: "pods", Namespace: "payments", Name: "postgres-0"}) {
		t.Errorf("pod link %+v", got)
	}
}

// busyNode is the fixture node with the most pods: its drawer shows a full Pods table.
const busyNode = "worker-a-01"

// renderNodePods opens a busy node's drawer and scrolls it to the Pods table.
func renderNodePods(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "nodes")
	registerProbes(t, f)
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: busyNode}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(findAll[*tipIcon](sh.drawer.details)) > 0 && sh.diag.total > 0
	})
	settle(w)
	scrollDrawerTo(t, sh, "Pods")
	settle(w)
	savePNG(t, w, name)
}

// scrollDrawerTo scrolls the drawer's details so that the section titled title is at the top.
func scrollDrawerTo(t *testing.T, sh *shell, title string) {
	t.Helper()
	var scroll *container.Scroll
	for _, s := range findAll[*container.Scroll](sh.drawer.tabs) {
		if slices.Contains(findAll[*fyne.Container](s.Content), sh.drawer.details) {
			scroll = s
		}
	}
	if scroll == nil {
		t.Fatal("no scroll around the drawer's details")
	}
	drv := fyne.CurrentApp().Driver()
	for _, l := range findAll[*textLine](sh.drawer.details) {
		if l.text == title && l.style.Bold {
			scroll.ScrollToOffset(fyne.NewPos(0, drv.AbsolutePositionForObject(l).Y-drv.AbsolutePositionForObject(scroll.Content).Y))
			return
		}
	}
	for _, l := range findAll[*sectionHead](sh.drawer.details) { // a section's bracket label (stage_header.go)
		if l.title == title {
			scroll.ScrollToOffset(fyne.NewPos(0, drv.AbsolutePositionForObject(l).Y-drv.AbsolutePositionForObject(scroll.Content).Y))
			return
		}
	}
	t.Fatalf("no section %q in the drawer", title)
}

func TestRenderNodePodsDrawerDark(t *testing.T) {
	renderNodePods(t, ThemeDark, "node_pods_drawer_dark.png")
}

func TestRenderNodePodsDrawerLight(t *testing.T) {
	renderNodePods(t, ThemeLight, "node_pods_drawer_light.png")
}

// TestNodeDrawerPodsTable: a node's drawer ends with its Pods table as captured (Name, the
// warning icon, Node, Namespace, Ready, CPU, Memory, Status), one row per pod on the node, a
// warning icon on each pod with a problem, and pod names that navigate.
func TestNodeDrawerPodsTable(t *testing.T) {
	f := kindsFake(t, "nodes")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: busyNode}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(findAll[*tipIcon](sh.drawer.details)) > 0 })

	var onNode, warned []string
	for _, p := range fixturePodSpecs() {
		if p.node != busyNode {
			continue
		}
		onNode = append(onNode, p.name)
		if p.state == podCrashLoop || p.state == podNotReady || p.state == podPullBackOff {
			warned = append(warned, p.name)
		}
	}
	texts := drawerTexts(sh)
	idx := -1 // the section titled Pods; Capacity's and Allocatable's "Pods" are columns
	for i, s := range texts {
		if s == "Pods" && i+1 < len(texts) && texts[i+1] == "Name" {
			idx = i
		}
	}
	if idx < 0 || idx+7 > len(texts) || !slices.Equal(texts[idx+1:idx+7], []string{"Name", "", "Node", "Namespace", "Ready", "CPU"}) {
		t.Fatalf("no Pods table with the captured columns in %q", texts)
	}
	links := drawerLinks(sh)
	for _, n := range onNode {
		if links[n] == nil {
			t.Errorf("pod %s is not a link in the node's Pods table", n)
		}
	}
	if icons := findAll[*tipIcon](sh.drawer.details); len(icons) != len(warned) {
		t.Errorf("%d warning icons for %d pods with problems %q", len(icons), len(warned), warned)
	}
	test.Tap(links[onNode[0]])
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	if got := decodeParams[refParams](t, f.callsOf("ui.navigate")[0]).Ref; got.Kind != "pods" || got.Name != onNode[0] {
		t.Errorf("pod link %+v", got)
	}
}

// BenchmarkNodeDrawerPodsRedraw measures what a redraw of a node's drawer with 250 pods costs the
// main goroutine: the engine announces a churning node's drawer at most ten times a second
// (TestNodeDrawerUnderDemoChurn), and each announcement whose sections changed rebuilds them.
func BenchmarkNodeDrawerPodsRedraw(b *testing.B) {
	f := kindsFake(b, "nodes")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: busyNode}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(b, f, Options{})
	q.until(b, "the drawer", func() bool { return sh.drawer.last != nil })
	settle(w)
	var pods []*unstructured.Unstructured
	for i := range 250 {
		pods = append(pods, fixturePodObject(podSpec{ns: fixtureNamespaces[i%len(fixtureNamespaces)], name: fmt.Sprintf("app-%03d", i),
			node: busyNode, state: []podState{podOK, podOK, podOK, podNotReady}[i%4], containers: []string{"app"}, age: time.Hour}))
	}
	p, _ := kinds.ByKey("nodes")
	var node *unstructured.Unstructured
	for _, n := range fixtureNodeObjects() {
		if n.GetName() == busyNode {
			node = n
		}
	}
	models := [2]*engine.DetailModel{}
	for i := range models {
		churn := append([]*unstructured.Unstructured(nil), pods...)
		churn[i] = fixturePodObject(podSpec{ns: "qa", name: churn[i].GetName(), node: busyNode, state: podCrashLoop, containers: []string{"app"}, age: time.Hour})
		m, err := engine.BuildDetailWith(p, ref, node, nil, fixedNow, fixtureRelated{"pods": churn})
		if err != nil {
			b.Fatal(err)
		}
		models[i] = m
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sh.drawer.show(ref, models[i%2], nil, sh.drawer.acts)
		settle(w)
	}
}
