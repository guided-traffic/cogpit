//go:build !nodemo

// A test over the demo cluster: a development build only, as the demo cluster is (D-67).

package commands_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestClusterOverviewDemoSize measures what an agent reads of the demo cluster's Overview: every
// issue an entry of its own against the fold, which stays under the bound and counts the same
// issues, and a subject's issues one by one.
func TestClusterOverviewDemoSize(t *testing.T) {
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{IncludeDemo: true, DemoOptions: demo.Options{Pods: 60}},
		SnapshotInterval: 20 * time.Millisecond})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), engine.DemoContext); err != nil {
		t.Fatal(err)
	}
	read := func(params map[string]any) (engine.ClusterOverviewModel, []byte) {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(context.Background(), engine.Invocation{ID: "cluster.overview", Params: raw, Origin: engine.OriginBuiltInAgent})
		if err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		var m engine.ClusterOverviewModel
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatal(err)
		}
		return m, out
	}
	// The demo's pods and events keep changing: the fold is compared with the unfolded list once two
	// reads of that list around it count the same issues.
	var every, folded engine.ClusterOverviewModel
	var before, after []byte
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		every, before = read(map[string]any{"everyIssue": true})
		folded, after = read(nil)
		later, _ := read(map[string]any{"everyIssue": true})
		return len(every.Issues) > 0 && len(every.Issues) == len(later.Issues) && folded.Fold != nil && folded.Fold.Issues == len(every.Issues)
	}) {
		t.Fatalf("the fold counts %+v, the unfolded list %d issues", folded.Fold, len(every.Issues))
	}
	if folded.Issues != nil || folded.Fold.Error+folded.Fold.Warning != folded.Fold.Issues {
		t.Fatalf("the fold %+v beside %d entries", folded.Fold, len(folded.Issues))
	}
	t.Logf("%d issues: %d bytes with every issue as an entry, %d bytes folded (%d subjects, %d causes)",
		len(every.Issues), len(before), len(after), folded.Fold.Subjects, folded.Fold.Causes.Total)
	if len(after) >= 24<<10 || len(after) >= len(before)/4 {
		t.Errorf("the fold is %d bytes of %d: want under 24 KiB and a quarter", len(after), len(before))
	}
	by := folded.Fold.BySubject
	if by.Total != folded.Fold.Subjects || len(by.Rows) != by.Total {
		t.Fatalf("%d subjects, %d rows of %d", folded.Fold.Subjects, len(by.Rows), by.Total)
	}
	// A subject's issues one by one: the node's, and the first pod's with its events.
	for i := range 2 {
		ref := by.Ref(i, "", "")
		members, raw := read(map[string]any{"subject": map[string]any{"kind": ref.Kind, "namespace": ref.Namespace, "name": ref.Name}})
		f := members.Fold
		if f.Members == nil || f.Issues == 0 || f.Members.Total == 0 || f.Members.Total > f.Issues || len(raw) >= 24<<10 {
			t.Fatalf("the issues of %s %s/%s: %s", ref.Kind, ref.Namespace, ref.Name, raw)
		}
		t.Logf("the issues of %s %s/%s: %d bytes, %d issues in %d rows", ref.Kind, ref.Namespace, ref.Name, len(raw), f.Issues, len(f.Members.Rows))
	}
}
