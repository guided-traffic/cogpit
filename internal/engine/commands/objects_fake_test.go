package commands_test

import (
	"context"
	"errors"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The fake writer's side of the generic object commands (edit, apply, create, delete). It acts
// like an API server on the harness's MemorySource when a test attaches one with serveObjects:
// updates check the resourceVersion precondition and answer 409 Conflict, dry runs store nothing,
// writes reach the cache through the source's watch. Without a source the writes are recorded
// only, which is what the fuzz target needs.

var _ engine.ObjectWriter = (*fakeWriter)(nil)

// objectServer is the API-server state behind one fake writer.
type objectServer struct {
	src *testkit.MemorySource

	mu       sync.Mutex
	updates  []*unstructured.Unstructured // every update request's object, dry runs included
	creates  []*unstructured.Unstructured
	deletes  []string // "namespace/name uid"
	failNext error    // returned by the next write, then cleared
	// afterFail, when set, runs once with the write that returns failNext, outside the lock: a test
	// makes there the change of the other client whose write the server's 409 stands for.
	afterFail func()
	// beforeDelete, when set, runs before each delete request is served, outside the lock: a test
	// holds a delete there (a job's step under way).
	beforeDelete func(namespace, name string)
}

var objectServers sync.Map // *fakeWriter → *objectServer

// serveObjects makes the harness's fake writer act on its source.
func serveObjects(h *harness) *objectServer {
	srv := &objectServer{src: h.src}
	objectServers.Store(h.writer, srv)
	return srv
}

func (w *fakeWriter) server() *objectServer {
	if v, ok := objectServers.Load(w); ok {
		return v.(*objectServer)
	}
	return nil
}

// fakeKinds is what the fake discovery serves.
var fakeKinds = []cluster.Mapping{
	{GVR: testkit.PodsGVR, GVK: schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, Namespaced: true},
	{GVR: testkit.ServicesGVR, GVK: schema.GroupVersionKind{Version: "v1", Kind: "Service"}, Namespaced: true},
	{GVR: testkit.NamespacesGVR, GVK: schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}},
	{GVR: testkit.EventsGVR, GVK: schema.GroupVersionKind{Version: "v1", Kind: "Event"}, Namespaced: true},
	{GVR: testkit.DeploymentsGVR, GVK: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, Namespaced: true},
	{GVR: configMapsGVR, GVK: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Namespaced: true},
	{GVR: secretsGVR, GVK: schema.GroupVersionKind{Version: "v1", Kind: "Secret"}, Namespaced: true},
	{GVR: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"},
		GVK: schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}},
}

var (
	configMapsGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	secretsGVR    = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
)

func (w *fakeWriter) MapKind(_ context.Context, apiVersion, kind string) (cluster.Mapping, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return cluster.Mapping{}, err
	}
	for _, m := range fakeKinds {
		if m.GVK.Kind == kind && m.GVK.Group == gv.Group && m.GVK.Version == gv.Version {
			return m, nil
		}
	}
	return cluster.Mapping{}, fmt.Errorf("no matches for kind %q in version %q", kind, apiVersion)
}

func (w *fakeWriter) MapResource(_ context.Context, group, resource string) (cluster.Mapping, error) {
	for _, m := range fakeKinds {
		if m.GVR.Resource == resource && (group == "" || m.GVR.Group == group) {
			return m, nil
		}
	}
	return cluster.Mapping{}, fmt.Errorf("the server does not serve %s.%s", resource, group)
}

// current returns the object the source holds, through its list, as a server would read it.
func (srv *objectServer) current(gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	// testkit's MemorySource lists only through the plain List; the engine may not import
	// client-go's tools/cache for the context adapter.
	list, err := srv.src.ListerWatcher(cache.Scope{GVR: gvr}).List(metav1.ListOptions{}) //nolint:staticcheck // see above
	if err != nil {
		return nil, err
	}
	for _, it := range list.(*unstructured.UnstructuredList).Items {
		if it.GetNamespace() == namespace && it.GetName() == name {
			return it.DeepCopy(), nil
		}
	}
	return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
}

func (srv *objectServer) takeFailure() error {
	srv.mu.Lock()
	err, after := srv.failNext, srv.afterFail
	srv.failNext = nil
	if err != nil {
		srv.afterFail = nil
	}
	srv.mu.Unlock()
	if err != nil && after != nil {
		after()
	}
	return err
}

func dryTag(dryRun bool) string {
	if dryRun {
		return "(dry)"
	}
	return ""
}

func (w *fakeWriter) UpdateObject(_ context.Context, gvr schema.GroupVersionResource, obj *unstructured.Unstructured, dryRun bool) (*unstructured.Unstructured, error) {
	w.record("update" + dryTag(dryRun) + " " + obj.GetNamespace() + "/" + obj.GetName())
	srv := w.server()
	if srv == nil {
		return obj, nil
	}
	srv.mu.Lock()
	srv.updates = append(srv.updates, obj.DeepCopy())
	srv.mu.Unlock()
	if err := srv.takeFailure(); err != nil {
		return nil, err
	}
	if obj.GetResourceVersion() == "" {
		return nil, cluster.ErrNoPrecondition
	}
	cur, err := srv.current(gvr, obj.GetNamespace(), obj.GetName())
	if err != nil {
		return nil, err
	}
	if cur.GetResourceVersion() != obj.GetResourceVersion() || (obj.GetUID() != "" && obj.GetUID() != cur.GetUID()) {
		return nil, apierrors.NewConflict(gvr.GroupResource(), obj.GetName(),
			errors.New("the object has been modified; please apply your changes to the latest version and try again"))
	}
	if dryRun {
		return obj.DeepCopy(), nil
	}
	srv.src.Set(gvr, obj)
	return srv.current(gvr, obj.GetNamespace(), obj.GetName())
}

func (w *fakeWriter) CreateObject(_ context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured, dryRun bool) (*unstructured.Unstructured, error) {
	w.record("create" + dryTag(dryRun) + " " + namespace + "/" + obj.GetName())
	srv := w.server()
	if srv == nil {
		return obj, nil
	}
	srv.mu.Lock()
	srv.creates = append(srv.creates, obj.DeepCopy())
	srv.mu.Unlock()
	if err := srv.takeFailure(); err != nil {
		return nil, err
	}
	if _, err := srv.current(gvr, namespace, obj.GetName()); err == nil {
		return nil, apierrors.NewAlreadyExists(gvr.GroupResource(), obj.GetName())
	}
	out := obj.DeepCopy()
	out.SetNamespace(namespace)
	out.SetUID(types.UID("uid-" + namespace + "-" + obj.GetName()))
	if dryRun {
		return out, nil
	}
	srv.src.Set(gvr, out)
	return srv.current(gvr, namespace, out.GetName())
}

func (w *fakeWriter) DeleteObject(_ context.Context, gvr schema.GroupVersionResource, namespace, name, uid string) error {
	w.record("delete " + namespace + "/" + name)
	srv := w.server()
	if srv == nil {
		return nil
	}
	srv.mu.Lock()
	before := srv.beforeDelete
	srv.mu.Unlock()
	if before != nil {
		before(namespace, name)
	}
	srv.mu.Lock()
	srv.deletes = append(srv.deletes, namespace+"/"+name+" "+uid)
	srv.mu.Unlock()
	cur, err := srv.current(gvr, namespace, name)
	if err != nil {
		return err
	}
	if uid != "" && string(cur.GetUID()) != uid {
		return apierrors.NewConflict(gvr.GroupResource(), name, errors.New("Precondition failed: UID in precondition: "+uid))
	}
	srv.src.Delete(gvr, namespace, name)
	return nil
}

// lastUpdate returns the object of the last update request.
func (srv *objectServer) lastUpdate() *unstructured.Unstructured {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.updates) == 0 {
		return nil
	}
	return srv.updates[len(srv.updates)-1]
}

// bump changes an object the way another client would: a new annotation, a new resourceVersion.
func bump(src *testkit.MemorySource, gvr schema.GroupVersionResource, obj *unstructured.Unstructured, key, value string) {
	c := obj.DeepCopy()
	ann := c.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[key] = value
	c.SetAnnotations(ann)
	src.Set(gvr, c)
}
