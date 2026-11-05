package testkit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/guided-traffic/cogpit/internal/cache"
)

// A watch from an old resource version replays every change since, however many: the replay
// does not wait for a reader while the source holds its lock, so changes made meanwhile go on.
func TestMemorySourceWatchReplaysALongHistory(t *testing.T) {
	src := NewMemorySource()
	lw := toolscache.ToListerWatcherWithContext(src.ListerWatcher(cache.Scope{GVR: PodsGVR}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := lw.ListWithContext(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	acc, err := meta.ListAccessor(list)
	if err != nil {
		t.Fatal(err)
	}
	const changes = 5000 // more than a watcher's buffer for live events
	for i := range changes {
		src.Set(PodsGVR, Pod("apps", fmt.Sprintf("p-%04d", i%300)))
	}
	watched := make(chan watch.Interface, 1)
	go func() {
		w, err := lw.WatchWithContext(ctx, metav1.ListOptions{ResourceVersion: acc.GetResourceVersion()})
		if err != nil {
			t.Error(err)
			close(watched)
			return
		}
		watched <- w
	}()
	var w watch.Interface
	select {
	case w = <-watched:
	case <-ctx.Done():
		t.Fatal("Watch did not return: the replay blocked")
	}
	if w == nil {
		return
	}
	defer w.Stop()
	src.Set(PodsGVR, Pod("apps", "after")) // the source is not held
	for i := range changes + 1 {
		select {
		case <-w.ResultChan():
		case <-ctx.Done():
			t.Fatalf("%d of %d events", i, changes+1)
		}
	}
}
