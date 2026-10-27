package commands_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var checkout = model.ObjectRef{Kind: "services", Namespace: "shop", Name: "checkout"}

// editHarness is a harness whose fake writer acts on its source, with the services kind warm.
func editHarness(t *testing.T, opts engine.Options) (*harness, *objectServer) {
	t.Helper()
	h := newHarness(t, seed(), opts)
	srv := serveObjects(h)
	if _, err := h.s.Objects(context.Background(), "services", nil); err != nil {
		t.Fatal(err)
	}
	return h, srv
}

// cachedRV waits until the cache holds ref in a version other than not, and returns it.
func cachedRV(t *testing.T, h *harness, ref model.ObjectRef, not string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var rv string
	if !testkit.WaitFor(ctx, func() bool {
		obj, err := h.s.Lookup(ref)
		if err != nil {
			return false
		}
		rv = obj.GetResourceVersion()
		return rv != not
	}) {
		t.Fatalf("the cache never moved %s past version %s", ref.String(), not)
	}
	return rv
}

func TestEditDiffDryRunApply(t *testing.T) {
	h, srv := editHarness(t, engine.Options{})
	gui := engine.OriginGUI

	var opened, again commands.EditResult
	h.must(t, gui, "object.edit", map[string]any{"ref": checkout}, &opened)
	h.must(t, gui, "object.edit", map[string]any{"ref": checkout}, &again)
	if !opened.Created || again.Created || opened.Tab.ID != again.Tab.ID || opened.Tab.Type != engine.DockEdit {
		t.Fatalf("edit tabs %+v / %+v", opened.Tab, again.Tab)
	}
	if opened.Tab.Title != "Service checkout" || opened.Tab.Ref == nil || !opened.Tab.Ref.Same(checkout) || opened.Tab.Context != "test" {
		t.Errorf("tab %+v", opened.Tab)
	}
	if !strings.Contains(opened.Text, "clusterIP: 10.96.100.100") || strings.Contains(opened.Text, "managedFields") || opened.BaseResourceVersion == "" {
		t.Errorf("edit text (base %q):\n%s", opened.BaseResourceVersion, opened.Text)
	}
	if u := h.s.UIState(); u.ActiveDock != opened.Tab.ID || len(u.Dock) != 1 {
		t.Errorf("dock %+v active %s", u.Dock, u.ActiveDock)
	}

	// The user types; the engine keeps the text and its revision.
	edited := strings.Replace(opened.Text, "clusterIP: 10.96.100.100", "clusterIP: 10.96.100.100\n  sessionAffinity: ClientIP", 1)
	var tr commands.TextResult
	h.must(t, gui, "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": edited}, &tr)
	var st engine.EditorState
	h.must(t, gui, "editor.get", map[string]any{"id": opened.Tab.ID}, &st)
	if tr.Revision != 2 || st.Revision != 2 || st.Text != edited || st.Stale || st.BaseResourceVersion != opened.BaseResourceVersion {
		t.Errorf("editor state %+v after setText rev %d", st, tr.Revision)
	}

	var diff engine.EditDiff
	h.must(t, gui, "editor.diff", map[string]any{"id": opened.Tab.ID}, &diff)
	if !diff.Changed || diff.Added != 1 || diff.Removed != 0 || diff.Stale {
		t.Fatalf("diff %+v", diff)
	}
	added := slices.IndexFunc(diff.Lines, func(l engine.DiffLine) bool { return l.Op == engine.DiffAdded })
	if added < 0 || strings.TrimSpace(diff.Lines[added].Text) != "sessionAffinity: ClientIP" || len(diff.Lines) > 7 {
		t.Errorf("diff lines %+v", diff.Lines)
	}

	var dry engine.DryRunResult
	h.must(t, gui, "object.dryRun", map[string]any{"id": opened.Tab.ID}, &dry)
	if !dry.Accepted || dry.BaseResourceVersion != opened.BaseResourceVersion {
		t.Errorf("dry run %+v", dry)
	}
	if got := h.writer.Calls(); !slices.Equal(got, []string{"update(dry) shop/checkout"}) {
		t.Fatalf("writes after the dry run: %v", got)
	}

	// The text claims another resourceVersion and UID: the engine sends the base's.
	claimed := strings.Replace(edited, "resourceVersion: \""+opened.BaseResourceVersion+"\"", "resourceVersion: \"999\"", 1)
	claimed = strings.Replace(claimed, "uid: uid-shop-checkout", "uid: forged", 1)
	if strings.Count(claimed, "999") != 1 || !strings.Contains(claimed, "uid: forged") {
		t.Fatalf("the test text claims no other version or UID:\n%s", claimed)
	}
	var applied engine.ApplyResult
	h.must(t, gui, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": claimed}, &applied)
	if got := h.writer.Calls(); !slices.Equal(got, []string{"update(dry) shop/checkout", "update(dry) shop/checkout", "update shop/checkout"}) {
		t.Errorf("writes of the apply: %v", got)
	}
	sent := srv.lastUpdate()
	if sent.GetResourceVersion() != opened.BaseResourceVersion || string(sent.GetUID()) != "uid-shop-checkout" {
		t.Errorf("update carried resourceVersion %q uid %q, want the base %q", sent.GetResourceVersion(), sent.GetUID(), opened.BaseResourceVersion)
	}
	if v, _, _ := unstructured.NestedString(sent.Object, "spec", "sessionAffinity"); v != "ClientIP" {
		t.Errorf("the edit was not sent: %v", sent.Object["spec"])
	}
	if applied.ResourceVersion == "" || applied.ResourceVersion == opened.BaseResourceVersion || !strings.Contains(applied.Text, "sessionAffinity: ClientIP") {
		t.Errorf("apply result %+v", applied)
	}
	h.must(t, gui, "editor.get", map[string]any{"id": opened.Tab.ID}, &st)
	if st.BaseResourceVersion != applied.ResourceVersion || st.Text != applied.Text {
		t.Errorf("after apply the tab is based on %q, want %q", st.BaseResourceVersion, applied.ResourceVersion)
	}
	cachedRV(t, h, checkout, opened.BaseResourceVersion)

	// Closing the tab forgets its editor.
	h.must(t, gui, "dock.close", map[string]any{"id": opened.Tab.ID}, nil)
	if err := h.run(t, gui, "editor.get", map[string]any{"id": opened.Tab.ID}, nil); !errors.Is(err, engine.ErrNotEditorTab) {
		t.Errorf("editor.get on a closed tab: %v", err)
	}
}

// TestApplyConflictNeverOverwrites: another client changes the object after the edit's base
// version. The apply writes nothing, reports a typed conflict with the diff against the version
// the cache now holds, and only a second, explicit apply overwrites that version.
func TestApplyConflictNeverOverwrites(t *testing.T) {
	h, _ := editHarness(t, engine.Options{})
	gui := engine.OriginGUI
	var opened commands.EditResult
	h.must(t, gui, "object.edit", map[string]any{"ref": checkout}, &opened)
	edited := strings.Replace(opened.Text, "clusterIP: 10.96.100.100", "clusterIP: 10.96.100.100\n  sessionAffinity: ClientIP", 1)

	obj, err := h.s.Lookup(checkout)
	if err != nil {
		t.Fatal(err)
	}
	bump(h.src, testkit.ServicesGVR, obj, "changed-by", "someone-else")
	newer := cachedRV(t, h, checkout, opened.BaseResourceVersion)

	var diff engine.EditDiff
	h.must(t, gui, "editor.diff", map[string]any{"id": opened.Tab.ID, "yaml": edited}, &diff)
	if !diff.Stale || diff.CachedResourceVersion != newer || diff.Removed == 0 {
		t.Errorf("diff against a newer version: %+v", diff)
	}

	err = h.run(t, gui, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": edited}, nil)
	var ce *engine.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("stale apply: %v", err)
	}
	if ce.BaseResourceVersion != opened.BaseResourceVersion || ce.CurrentResourceVersion != newer || ce.Recreated {
		t.Errorf("conflict %+v", ce)
	}
	reverted := slices.IndexFunc(ce.Diff, func(l engine.DiffLine) bool {
		return l.Op == engine.DiffRemoved && strings.Contains(l.Text, "changed-by: someone-else")
	})
	if reverted < 0 {
		t.Errorf("the conflict's diff does not show the other change: %+v", ce.Diff)
	}
	if !strings.Contains(err.Error(), "nothing was applied") || !strings.Contains(err.Error(), "editor_diff") {
		t.Errorf("conflict message %q", err)
	}
	for _, c := range h.writer.Calls() {
		if c == "update shop/checkout" {
			t.Fatalf("a stale apply wrote: %v", h.writer.Calls())
		}
	}
	now, _ := h.s.Lookup(checkout)
	if now.GetAnnotations()["changed-by"] != "someone-else" || now.GetResourceVersion() != newer {
		t.Fatalf("the other change was overwritten: %v", now.GetAnnotations())
	}

	// The tab is now based on the version it was shown against; applying again is explicit.
	var st engine.EditorState
	h.must(t, gui, "editor.get", map[string]any{"id": opened.Tab.ID}, &st)
	if st.BaseResourceVersion != newer || st.Stale {
		t.Errorf("after the conflict: %+v", st)
	}
	var applied engine.ApplyResult
	h.must(t, gui, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": edited}, &applied)
	if applied.ResourceVersion == newer {
		t.Errorf("second apply %+v", applied)
	}
}

func TestEditRefusesWhatItMustNot(t *testing.T) {
	h, srv := editHarness(t, engine.Options{})
	gui := engine.OriginGUI
	h.src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "db", map[string]any{"data": map[string]any{"password": "aHVudGVyMg=="}}))

	var dis *engine.DisabledError
	if err := h.run(t, gui, "object.edit", map[string]any{"ref": model.ObjectRef{Kind: "services", Namespace: "shop", Name: "gone"}}, nil); !errors.As(err, &dis) || !strings.Contains(dis.Reason, "not found") {
		t.Errorf("edit a missing object: %v", err)
	}

	var opened commands.EditResult
	h.must(t, gui, "object.edit", map[string]any{"ref": checkout}, &opened)
	id := opened.Tab.ID
	for name, tc := range map[string]struct{ text, want string }{
		"rename":       {strings.Replace(opened.Text, "name: checkout", "name: checkout-2", 1), "cannot rename"},
		"move":         {strings.Replace(opened.Text, "namespace: shop", "namespace: infra", 1), "cannot move"},
		"retype":       {strings.Replace(opened.Text, "kind: Service", "kind: Pod", 1), "kind must stay"},
		"version":      {strings.Replace(opened.Text, "apiVersion: v1", "apiVersion: v2", 1), "apiVersion must stay"},
		"malformed":    {"apiVersion: v1\nkind: [Service\n", "invalid manifest"},
		"not a map":    {"- a\n- b\n", "not a YAML mapping"},
		"two objects":  {opened.Text + "---\n" + opened.Text, "holds 2 objects"},
		"a Secret":     {"apiVersion: v1\nkind: Secret\nmetadata:\n  name: checkout\n  namespace: shop\n", "kind must stay"},
		"no kind":      {"apiVersion: v1\nmetadata:\n  name: checkout\n", "kind is missing"},
		"bad metadata": {"apiVersion: v1\nkind: Service\nmetadata: oops\n", "metadata is not a mapping"},
	} {
		for _, cmd := range []string{"editor.diff", "object.dryRun", "object.apply"} {
			err := h.run(t, gui, cmd, map[string]any{"id": id, "yaml": tc.text}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s %s: %v, want %q", cmd, name, err, tc.want)
			}
		}
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Errorf("refused edits reached the cluster: %v", calls)
	}
	if srv.lastUpdate() != nil {
		t.Error("an update was sent")
	}

	// A Create resource tab has nothing to diff or apply against; ids must be editor tabs.
	var ct commands.EditResult
	h.must(t, gui, "resource.createTab", nil, &ct)
	for _, cmd := range []string{"editor.diff", "object.dryRun", "object.apply"} {
		if err := h.run(t, gui, cmd, map[string]any{"id": ct.Tab.ID}, nil); err == nil || !strings.Contains(err.Error(), "not an Edit tab") {
			t.Errorf("%s on a create tab: %v", cmd, err)
		}
		if err := h.run(t, gui, cmd, map[string]any{"id": "logs-9"}, nil); err == nil {
			t.Errorf("%s on an unknown tab succeeded", cmd)
		}
	}
}

func TestEditConfirmationPerOrigin(t *testing.T) {
	audit := &syncBuffer{}
	h, _ := editHarness(t, engine.Options{Audit: audit})
	agent := engine.OriginExternalAgent

	// Opening, reading, diffing and the dry run are free for agents.
	var opened commands.EditResult
	h.must(t, agent, "object.edit", map[string]any{"ref": checkout}, &opened)
	edited := strings.Replace(opened.Text, "clusterIP: 10.96.100.100", "clusterIP: 10.96.100.100\n  sessionAffinity: ClientIP", 1)
	h.must(t, agent, "editor.diff", map[string]any{"id": opened.Tab.ID, "yaml": edited}, nil)
	h.must(t, agent, "object.dryRun", map[string]any{"id": opened.Tab.ID, "yaml": edited}, nil)

	// Apply from an agent needs the user: refused headless, asked with a GUI.
	params := map[string]any{"id": opened.Tab.ID, "yaml": edited}
	if err := h.run(t, agent, "object.apply", params, nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("headless agent apply: %v", err)
	}
	var asked []engine.ConfirmRequest
	answer := false
	h.s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return answer, nil
	}))
	if err := h.run(t, agent, "object.apply", params, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Errorf("declined agent apply: %v", err)
	}
	if len(asked) != 1 || asked[0].Command != "object.apply" || asked[0].Origin != agent || asked[0].Context != "test" ||
		!slices.Equal(asked[0].Namespaces, []string{"shop"}) || len(asked[0].Objects) != 1 || !asked[0].Objects[0].Same(checkout) {
		t.Fatalf("confirmation %+v", asked)
	}
	if slices.Contains(h.writer.Calls(), "update shop/checkout") {
		t.Fatal("a declined apply wrote")
	}
	answer = true
	h.must(t, agent, "object.apply", params, nil)

	// The GUI applies without a dialog; an agent allowed to write in this context too.
	n := len(asked)
	h.must(t, engine.OriginGUI, "object.edit", map[string]any{"ref": checkout}, nil)
	var st engine.EditorState
	h.must(t, engine.OriginGUI, "editor.get", map[string]any{"id": opened.Tab.ID}, &st)
	h.must(t, engine.OriginGUI, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": strings.Replace(st.Text, "ClientIP", "None", 1)}, nil)
	h.s.SetAgentWrites("test", true)
	h.must(t, engine.OriginGUI, "editor.get", map[string]any{"id": opened.Tab.ID}, &st)
	h.must(t, agent, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": strings.Replace(st.Text, "None", "ClientIP", 1)}, nil)
	if len(asked) != n {
		t.Errorf("asked %d more times", len(asked)-n)
	}

	var applies []engine.AuditEntry
	for _, e := range audit.Lines() {
		if e.Command == "object.apply" {
			applies = append(applies, e)
		}
	}
	// headless refusal (done), declined (done), accepted (requested, done), allowed (requested, done)
	if len(applies) != 6 || applies[2].Phase != "requested" || !applies[3].OK || applies[0].OK || applies[1].OK {
		t.Errorf("audit of object.apply: %+v", applies)
	}
}

// An apply from a tab's text (no yaml) names the tab's text revision the user reviewed: an agent
// that changes the text while the confirmation is open has the apply refused, and an apply from a
// tab without a revision is refused (review of the fourth run, R-10).
func TestApplyFromTabBoundToTheReviewedText(t *testing.T) {
	h, srv := editHarness(t, engine.Options{})
	agent := engine.OriginExternalAgent
	var opened commands.EditResult
	h.must(t, agent, "object.edit", map[string]any{"ref": checkout}, &opened)
	reviewed := strings.Replace(opened.Text, "clusterIP: 10.96.100.100", "clusterIP: 10.96.100.100\n  sessionAffinity: ClientIP", 1)
	var set commands.TextResult
	h.must(t, agent, "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": reviewed}, &set)
	h.s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
		swapped := strings.Replace(reviewed, "ClientIP", "None", 1)
		if err := h.run(t, agent, "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": swapped}, nil); err != nil {
			t.Error(err)
		}
		return true, nil
	}))
	if err := h.run(t, agent, "object.apply", map[string]any{"id": opened.Tab.ID, "textRevision": set.Revision}, nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("apply with the text changed during its confirmation: %v", err)
	}
	h.s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil }))
	if err := h.run(t, agent, "object.apply", map[string]any{"id": opened.Tab.ID}, nil); err == nil || !strings.Contains(err.Error(), "textRevision") {
		t.Errorf("apply from a tab without a text revision: %v", err)
	}
	if srv.lastUpdate() != nil {
		t.Fatal("an update was sent")
	}
	var st engine.EditorState
	h.must(t, agent, "editor.get", map[string]any{"id": opened.Tab.ID}, &st)
	h.must(t, agent, "object.apply", map[string]any{"id": opened.Tab.ID, "textRevision": st.Revision}, nil)
	if u := srv.lastUpdate(); u == nil || model.NestedString(u.Object, "spec", "sessionAffinity") != "None" {
		t.Errorf("the reviewed text was not applied: %v", u)
	}
}

// TestEditTabOfAnotherCluster: an Edit tab works on its own cluster while the window shows another
// one (D-69); once its cluster is disconnected its commands are refused and nothing is written.
func TestEditTabOfAnotherCluster(t *testing.T) {
	h, _ := editHarness(t, engine.Options{})
	var opened commands.EditResult
	h.must(t, engine.OriginGUI, "object.edit", map[string]any{"ref": checkout}, &opened)
	if err := h.s.Connect(context.Background(), "other"); err != nil {
		t.Fatal(err)
	}
	var diff engine.EditDiff
	h.must(t, engine.OriginGUI, "editor.diff", map[string]any{"id": opened.Tab.ID}, &diff) // test stays connected
	if err := h.s.DisconnectContext("test"); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"editor.diff", "object.dryRun", "object.apply"} {
		if err := h.run(t, engine.OriginGUI, cmd, map[string]any{"id": opened.Tab.ID}, nil); !errors.Is(err, engine.ErrNotConnected) {
			t.Errorf("%s of a tab of a disconnected cluster: %v", cmd, err)
		}
	}
	if len(h.writer.Calls()) != 0 {
		t.Errorf("writes: %v", h.writer.Calls())
	}
	if err := h.run(t, engine.OriginGUI, "object.edit", map[string]any{"ref": model.ObjectRef{Context: "test", Kind: "services", Namespace: "shop", Name: "checkout"}}, nil); err == nil {
		t.Error("edit of an object of another cluster succeeded")
	}
}

func TestCreateObjects(t *testing.T) {
	h, srv := editHarness(t, engine.Options{})
	gui := engine.OriginGUI
	h.must(t, gui, "ui.setNamespace", map[string]any{"namespaces": []string{"shop"}}, nil)
	text := `apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
  resourceVersion: "77"
data:
  a: "1"
---
# a comment only
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: other
  namespace: infra
---
apiVersion: v1
kind: Gadget
metadata:
  name: creds
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: reader
  namespace: shop
---
apiVersion: example.io/v1
kind: Widget
metadata:
  name: w
`
	var res engine.CreateResult
	err := h.run(t, gui, "object.create", map[string]any{"yaml": text}, &res)
	if err == nil || !strings.Contains(err.Error(), "2 of 5 objects not created") {
		t.Fatalf("create: %v", err)
	}
	// The result reaches the GUI with the error; decode it from the dispatch directly.
	raw, _ := h.s.Dispatch(context.Background(), engine.Invocation{ID: "object.create", Params: []byte(`{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: third\n","namespace":"infra"}`), Origin: gui})
	third, ok := raw.(engine.CreateResult)
	if !ok || third.Created != 1 || third.Items[0].Ref.Namespace != "infra" || third.Items[0].Ref.Kind != "configmaps" {
		t.Errorf("create with a namespace parameter: %+v", raw)
	}
	want := []string{
		"create(dry) shop/settings", "create shop/settings",
		"create(dry) infra/other", "create infra/other",
		"create(dry) /reader", "create /reader",
		"create(dry) infra/third", "create infra/third",
	}
	if got := h.writer.Calls(); !slices.Equal(got, want) {
		t.Errorf("writes:\n got %v\nwant %v", got, want)
	}
	srv.mu.Lock()
	first, role := srv.creates[0], srv.creates[4]
	srv.mu.Unlock()
	if first.GetResourceVersion() != "" || first.GetNamespace() != "shop" {
		t.Errorf("a create carried resourceVersion %q namespace %q", first.GetResourceVersion(), first.GetNamespace())
	}
	if role.GetNamespace() != "" {
		t.Errorf("a cluster-scoped object kept namespace %q", role.GetNamespace())
	}
	if !strings.Contains(err.Error(), "Gadget") || !strings.Contains(err.Error(), "Widget") {
		t.Errorf("failures not named: %v", err)
	}

	// Agents: the confirmation names every object; nothing is created when declined.
	var asked engine.ConfirmRequest
	h.s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) { asked = req; return false, nil }))
	n := len(h.writer.Calls())
	two := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n  namespace: infra\n"
	if err := h.run(t, engine.OriginBuiltInAgent, "object.create", map[string]any{"yaml": two}, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Errorf("declined agent create: %v", err)
	}
	if len(asked.Objects) != 2 || asked.Objects[0].Namespace != "shop" || asked.Objects[1].Namespace != "infra" ||
		!slices.Equal(asked.Namespaces, []string{"shop", "infra"}) || len(h.writer.Calls()) != n {
		t.Errorf("confirmation %+v, writes %v", asked, h.writer.Calls()[n:])
	}
	if err := h.run(t, gui, "object.create", map[string]any{"yaml": "kind: [x"}, nil); !errors.Is(err, engine.ErrManifest) {
		t.Errorf("malformed create: %v", err)
	}
}

func TestCreateTabAndTemplates(t *testing.T) {
	h, _ := editHarness(t, engine.Options{})
	gui := engine.OriginGUI

	var tpls []engine.Template
	h.must(t, engine.OriginExternalAgent, "resource.templates", nil, &tpls)
	names := make([]string, 0, len(tpls))
	for _, tp := range tpls {
		names = append(names, tp.Name)
		if tp.Group != "Cogpit" || strings.Contains(tp.Text, "namespace:") && tp.Name != "ClusterRoleBinding" {
			t.Errorf("template %s: group %q, text names a namespace:\n%s", tp.Name, tp.Group, tp.Text)
		}
	}
	if !slices.IsSorted(names) || !slices.Contains(names, "Pod") || !slices.Contains(names, "Deployment") || slices.Contains(names, "Secret") || len(names) < 25 {
		t.Errorf("templates %v", names)
	}

	var tab commands.EditResult
	h.must(t, gui, "resource.createTab", nil, &tab)
	if tab.Tab.Type != engine.DockCreate || tab.Tab.Title != "Create resource" || !strings.Contains(tab.Text, "kind: Pod") {
		t.Errorf("create tab %+v", tab)
	}
	h.must(t, gui, "resource.createTab", map[string]any{"template": "deployment"}, &tab)
	if !strings.Contains(tab.Text, "kind: Deployment") || tab.Tab.ID != "create-2" {
		t.Errorf("deployment tab %+v", tab)
	}
	h.must(t, engine.OriginExternalAgent, "resource.createTab", map[string]any{"yaml": "apiVersion: v1\nkind: ConfigMap\n"}, &tab)
	if tab.Text != "apiVersion: v1\nkind: ConfigMap\n" {
		t.Errorf("handed-over text %q", tab.Text)
	}
	if err := h.run(t, gui, "resource.createTab", map[string]any{"template": "Widget"}, nil); err == nil || !strings.Contains(err.Error(), "Deployment") {
		t.Errorf("unknown template: %v", err)
	}
	if err := h.run(t, gui, "resource.createTab", map[string]any{"template": "Pod", "yaml": "x: 1"}, nil); err == nil {
		t.Error("yaml and template together accepted")
	}

	// The text round-trips; agents see what the user typed, and an event announces a change.
	got := make(chan engine.Event, 4)
	cancel := h.s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventEditor {
			select { // never block the engine's goroutine that emits
			case got <- ev:
			default:
			}
		}
	})
	defer cancel()
	var tr commands.TextResult
	h.must(t, gui, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "kind: X\n"}, &tr)
	select {
	case ev := <-got:
		if ev.View != tab.Tab.ID || ev.Revision != tr.Revision {
			t.Errorf("editor event %+v, want tab %s revision %d", ev, tab.Tab.ID, tr.Revision)
		}
	case <-time.After(5 * time.Second):
		t.Error("no editor event")
	}
	var st engine.EditorState
	h.must(t, engine.OriginExternalAgent, "editor.get", map[string]any{"id": tab.Tab.ID}, &st)
	if st.Text != "kind: X\n" || st.Revision != 2 {
		t.Errorf("editor.get %+v", st)
	}
	if err := h.run(t, gui, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": strings.Repeat("a", engine.MaxManifestBytes+1)}, nil); !errors.Is(err, engine.ErrManifest) {
		t.Errorf("oversized text: %v", err)
	}
}

func TestDeleteAnyKind(t *testing.T) {
	h, srv := editHarness(t, engine.Options{})
	h.src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "settings", nil))
	h.s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil }))

	var res commands.ActionResult
	h.must(t, engine.OriginGUI, "object.delete", map[string]any{"ref": checkout}, &res)
	if res.Action != "deleted" || !res.Ref.Same(checkout) {
		t.Errorf("delete a service: %+v", res)
	}
	cm := model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "settings"}
	h.must(t, engine.OriginGUI, "object.delete", map[string]any{"ref": cm}, &res)
	srv.mu.Lock()
	deletes := slices.Clone(srv.deletes)
	srv.mu.Unlock()
	if !slices.Equal(deletes, []string{"shop/checkout uid-shop-checkout", "shop/settings uid-shop-settings"}) {
		t.Errorf("deletes %v (each must carry the cached UID)", deletes)
	}
	// Cluster-scoped kinds lose a stray namespace; unknown kinds and keys are refused.
	if err := h.run(t, engine.OriginGUI, "object.delete", map[string]any{"ref": model.ObjectRef{Kind: "widgets", Namespace: "shop", Name: "w"}}, nil); err == nil {
		t.Error("delete of an unknown kind succeeded")
	}
	if err := h.run(t, engine.OriginGUI, "object.delete", map[string]any{"ref": model.ObjectRef{Kind: "configmaps", Name: "settings"}}, nil); err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Errorf("delete without a namespace: %v", err)
	}
}

func TestEditCommandsOnTheDemoCluster(t *testing.T) {
	src := seed()
	s := engine.New(engine.Options{Connector: &fakeConnector{src: src, writer: &fakeWriter{}, demo: true}, SnapshotInterval: 10 * time.Millisecond})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	for id, params := range map[string]string{
		"object.edit":        `{"ref":{"kind":"services","namespace":"shop","name":"checkout"}}`,
		"object.create":      `{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"}`,
		"resource.createTab": `{}`,
		"object.apply":       `{"id":"edit-1"}`,
		"object.dryRun":      `{"id":"edit-1"}`,
		"editor.diff":        `{"id":"edit-1"}`,
	} {
		_, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: []byte(params), Origin: engine.OriginGUI})
		var dis *engine.DisabledError
		if !errors.As(err, &dis) || !strings.Contains(dis.Reason, "demo") {
			t.Errorf("%s on the demo cluster: %v", id, err)
		}
	}
}

// TestEditorTabsUnderConcurrentCommands opens Edit tabs while other editor commands run: a new
// editor must survive the pruning another command does before its tab reaches the dock.
func TestEditorTabsUnderConcurrentCommands(t *testing.T) {
	h, _ := editHarness(t, engine.Options{})
	if _, err := h.s.Objects(context.Background(), "pods", nil); err != nil {
		t.Fatal(err)
	}
	var ct commands.EditResult
	h.must(t, engine.OriginGUI, "resource.createTab", nil, &ct)
	refs := []model.ObjectRef{
		checkout, {Kind: "services", Namespace: "shop", Name: "cart"}, {Kind: "services", Namespace: "infra", Name: "dns"},
		{Kind: "pods", Namespace: "shop", Name: "web-0"}, {Kind: "pods", Namespace: "infra", Name: "web-1"},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			_ = h.run(t, engine.OriginGUI, "editor.setText", map[string]any{"id": ct.Tab.ID, "yaml": strings.Repeat("x", i)}, nil)
		}
	}()
	results := make(chan commands.EditResult, len(refs))
	errs := make(chan error, len(refs))
	for _, r := range refs {
		go func() {
			var res commands.EditResult
			if err := h.run(t, engine.OriginExternalAgent, "object.edit", map[string]any{"ref": r}, &res); err != nil {
				errs <- err
				return
			}
			results <- res
		}()
	}
	for range refs {
		select {
		case err := <-errs:
			t.Errorf("object.edit: %v", err)
		case res := <-results:
			var st engine.EditorState
			h.must(t, engine.OriginGUI, "editor.get", map[string]any{"id": res.Tab.ID}, &st)
			if st.Text == "" || st.BaseResourceVersion == "" {
				t.Errorf("editor of %s lost: %+v", res.Tab.ID, st)
			}
		}
	}
	<-done
}
