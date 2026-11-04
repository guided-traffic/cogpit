package engine

import (
	"context"
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// selectingService is a Service in ns whose selector is app=<app>.
func selectingService(t *testing.T, ns, name, app string) *unstructured.Unstructured {
	t.Helper()
	svc := testkit.Service(ns, name, "10.0.0.1")
	if err := unstructured.SetNestedStringMap(svc.Object, map[string]string{"app": app}, "spec", "selector"); err != nil {
		t.Fatal(err)
	}
	return svc
}

// referencedBy returns the drawer's Referenced By section, nil without.
func referencedBy(m *DetailModel) *model.DetailSection {
	for i := range m.Sections {
		if m.Sections[i].Title == kinds.TitleReferencedBy {
			return &m.Sections[i]
		}
	}
	return nil
}

// TestDrawerBackReferences: with Preferences.BackReferences on, a pod's drawer ends with
// Referenced By, the Services that select it, read from the cache; off, the default, it has none.
// The open drawer follows a switch at once and a new Service that selects the pod; a kind the
// cluster does not serve is named as not read.
func TestDrawerBackReferences(t *testing.T) {
	src := testkit.NewMemorySource()
	pod := testkit.Pod("shop", "web-0")
	pod.SetLabels(map[string]string{"app": "web"})
	src.Set(testkit.PodsGVR, pod)
	src.Set(testkit.ServicesGVR, selectingService(t, "shop", "web", "web"))
	src.Set(testkit.ServicesGVR, selectingService(t, "shop", "blog", "blog"))
	fc := &fakeConnector{src: src, profiles: testProfiles()}
	s := New(Options{Connector: fc, SnapshotInterval: 10 * time.Millisecond, Preferences: NewPreferencesStore("")})
	t.Cleanup(s.Close)
	connectTest(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}

	m, err := s.DetailWait(ctx, ref)
	if err != nil || referencedBy(m) != nil {
		t.Fatalf("off: %+v, %v", m, err)
	}

	s.UpdateUI(func(u *UIState) { u.Drawer = &ref })
	if _, err := s.ChangePreferences(func(p *Preferences) error { p.BackReferences = true; return nil }); err != nil {
		t.Fatal(err)
	}
	rows := func(want [][]string) func() bool {
		return func() bool {
			m, err := s.Detail(ref)
			sec := referencedBy(m)
			return err == nil && sec != nil && sec.Table != nil && reflect.DeepEqual(sec.Table.Rows, want)
		}
	}
	waitFor(t, "the open drawer's Services", rows([][]string{{"Service", "web", "spec.selector"}}))
	m, _ = s.Detail(ref)
	if want := (model.DetailItem{Name: "Not read", Value: "Network Policies, Pod Disruption Budgets"}); !reflect.DeepEqual(referencedBy(m).Items, []model.DetailItem{want}) {
		t.Errorf("items %+v, want %+v", referencedBy(m).Items, want)
	}
	if last := m.Sections[len(m.Sections)-1]; last.Title != kinds.TitleReferencedBy {
		t.Errorf("last section %q", last.Title)
	}

	src.Set(testkit.ServicesGVR, selectingService(t, "shop", "web-canary", "web"))
	waitFor(t, "a new Service", rows([][]string{{"Service", "web", "spec.selector"}, {"Service", "web-canary", "spec.selector"}}))

	// An agent's read carries it too.
	m, err = s.DetailWait(ctx, ref)
	if err != nil || referencedBy(m) == nil || referencedBy(m).Table == nil || len(referencedBy(m).Table.Rows) != 2 {
		t.Fatalf("agent's read: %+v, %v", m, err)
	}

	if _, err := s.ChangePreferences(func(p *Preferences) error { p.BackReferences = false; return nil }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the section gone", func() bool {
		m, err := s.Detail(ref)
		return err == nil && referencedBy(m) == nil
	})
	s.state.mu.Lock()
	d := s.state.drawer
	s.state.mu.Unlock()
	if d == nil || d.backRefs {
		t.Errorf("the drawer after the switch off: %+v", d)
	}
}

// A kind without back references reads nothing more while the preference is on.
func TestDrawerNeedsBackReferences(t *testing.T) {
	b := &Backend{Profiles: testProfiles()}
	pods, _ := b.Profile("pods")
	deployments, _ := b.Profile("deployments")
	if got := drawerNeeds(b, pods, true); len(got) != len(pods.DrawerNeeds())+3 {
		t.Errorf("a pod's drawer reads %v", got)
	}
	if got := drawerNeeds(b, pods, false); len(got) != len(pods.DrawerNeeds()) {
		t.Errorf("off, a pod's drawer reads %v", got)
	}
	if got := drawerNeeds(b, deployments, true); len(got) != len(deployments.DrawerNeeds()) {
		t.Errorf("a deployment's drawer reads %v", got)
	}
}

// A drawer table its profile gave an order (Referenced By: by Kind) keeps it until the user sorts
// it by a column; another table nobody sorted goes by name.
func TestDrawerTableKeepsItsOwnOrder(t *testing.T) {
	cols := []model.Column{{ID: "kind", Title: "Kind", Type: model.ColumnText}, {ID: "name", Title: "Name", Type: model.ColumnText}}
	table := func(own bool) *model.DetailTable {
		tb := model.SortableCellTable(cols, [][]model.Cell{{{Text: "Service"}, {Text: "a"}}, {{Text: "NetworkPolicy"}, {Text: "b"}}})
		if own {
			tb.Sort = &model.TableSort{Column: "kind"}
		}
		return tb
	}
	m := &DetailModel{Sections: []model.DetailSection{{Title: kinds.TitleReferencedBy, Table: table(true)}, {Title: "Pods", Table: table(false)}}}
	SortDrawerTables(m, nil)
	if got := m.Sections[0].Table.Rows[0][0]; got != "NetworkPolicy" {
		t.Errorf("its own order: first %q", got)
	}
	if got := m.Sections[1].Table.Rows[0][1]; got != "a" {
		t.Errorf("by name: first %q", got)
	}
	m.Sections[0].Table = table(true)
	SortDrawerTables(m, map[string]SortSpec{kinds.TitleReferencedBy: {Column: "name"}})
	if got := m.Sections[0].Table.Rows[0][1]; got != "a" {
		t.Errorf("sorted by the user: first %q", got)
	}
}
