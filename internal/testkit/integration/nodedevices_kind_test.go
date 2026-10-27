//go:build kind

package integration_test

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// What TestKindNodeDevices creates on the shared kind cluster: one cluster-scoped resource slice of a
// driver that does not exist, published for the cluster's first node, so that nothing ever
// allocates or prepares its devices. It is deleted at the end.
const (
	nodeDevicesSlice  = "cogpit-node-devices"
	nodeDevicesDriver = "cogpit-node-devices.example.com"
)

// TestKindNodeDevices: Kubernetes 1.36 serves resource.k8s.io/v1, and a resource slice created by
// hand for a node (kind runs no DRA driver) appears in that node's drawer under Devices, from the
// cache through the related-objects index, with its pool, its link and its devices' attributes and
// capacity; it leaves the drawer when it is deleted.
func TestKindNodeDevices(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kube := conn.Kube()
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		t.Fatalf("the cluster's nodes: %v (%d)", err, len(nodes.Items))
	}
	node := nodes.Items[0].Name
	slicesAPI := kube.ResourceV1().ResourceSlices()
	cleanup := func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dcancel()
		if err := slicesAPI.Delete(dctx, nodeDevicesSlice, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete resource slice: %v", err)
		}
	}
	cleanup() // a leftover of an interrupted run
	t.Cleanup(cleanup)

	model1 := "X1"
	if _, err := slicesAPI.Create(ctx, &resourcev1.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: nodeDevicesSlice},
		Spec: resourcev1.ResourceSliceSpec{
			Driver:   nodeDevicesDriver,
			Pool:     resourcev1.ResourcePool{Name: node, Generation: 1, ResourceSliceCount: 1},
			NodeName: &node,
			Devices: []resourcev1.Device{{
				Name:       "accel-0",
				Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{"model": {StringValue: &model1}},
				Capacity:   map[resourcev1.QualifiedName]resourcev1.DeviceCapacity{"memory": {Value: resource.MustParse("16Gi")}},
			}},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	d := dispatcher{t: t, ctx: ctx, s: s}
	ref := model.ObjectRef{Context: contextName, Kind: "nodes", Name: node}
	d.must("ui.navigate", map[string]any{"ref": ref}, nil) // the Nodes view and the node's drawer
	devices := func() []model.DetailSection {
		m, err := s.Detail(ref)
		if err != nil {
			return nil
		}
		var out []model.DetailSection
		for _, sec := range m.Sections {
			if sec.Group == "Devices" && sec.Title == nodeDevicesDriver {
				out = append(out, sec)
			}
		}
		return out
	}
	// The section can come before its Claim column, which waits for the claims' index to be listed:
	// wait for the section as asserted.
	var sec []model.DetailSection
	if !testkit.WaitFor(ctx, func() bool {
		if sec = devices(); len(sec) != 1 {
			return false
		}
		var slice *model.ObjectRef
		for _, it := range sec[0].Items {
			if it.Name == "Slice" {
				slice = it.Link
			}
		}
		tbl := sec[0].Table
		return slice != nil && slice.Name == nodeDevicesSlice && tbl != nil && len(tbl.Rows) == 1 &&
			slices.Equal(tbl.Rows[0], []string{"accel-0", "model: X1", "memory: 16.0 GiB", ""})
	}) {
		t.Fatalf("the node's Devices: %+v", sec)
	}

	cleanup()
	waitFor(ctx, t, "the deleted slice to leave the drawer", func() bool { return len(devices()) == 0 })
}
