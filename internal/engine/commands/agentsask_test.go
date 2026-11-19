package commands_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A role binding an agent asks for waits for the user on every path that writes one, also where
// the user allowed agent writes for the context (H-27, owner): object.create of a text that holds a
// binding (among other documents, or in a v1 List), and object.apply of an Edit tab on a binding
// (from yaml or from the tab's text). Declined, nothing is written. The GUI's own write of the same
// text runs without a dialog, as before, and a text without a binding still runs unasked for the
// agent. (roleBinding.create: TestCreateDialogsAgentsNeedAcceptance.)
func TestAgentRoleBindingsAlwaysAsk(t *testing.T) {
	var asked []engine.ConfirmRequest
	answer := false
	h, _ := createHarness(t, engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return answer, nil
	})})
	h.s.SetAgentWrites("test", true)
	agent, gui := engine.OriginExternalAgent, engine.OriginGUI
	subjects := []any{map[string]any{"kind": "Group", "name": "devs"}}
	h.src.Set(roleBindingsGVR, testkit.Object("rbac.authorization.k8s.io/v1", "RoleBinding", "shop", "existing", map[string]any{
		"roleRef": map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "view"}, "subjects": subjects}))
	h.src.Set(clusterRoleBindingsGVR, testkit.Object("rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", "existing-crb", map[string]any{
		"roleRef": map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "view"}, "subjects": subjects}))

	// asks runs the agent's command, declined, and checks that it asked for this reason and wrote
	// nothing.
	asks := func(what, id string, params map[string]any) {
		t.Helper()
		n, writes := len(asked), len(h.writer.Calls())
		if err := h.run(t, agent, id, params, nil); !errors.Is(err, engine.ErrDeclined) {
			t.Fatalf("%s: %v, want ErrDeclined", what, err)
		}
		if len(asked) != n+1 || asked[n].Command != id || asked[n].Origin != agent || asked[n].Danger != engine.DangerMutating || !asked[n].AgentsAsk {
			t.Fatalf("%s: asked %+v", what, asked[n:])
		}
		if got := h.writer.Calls()[writes:]; len(got) != 0 {
			t.Fatalf("%s: a declined write sent %v", what, got)
		}
	}
	// runsUnasked runs the command for origin and checks that nobody was asked and it wrote.
	runsUnasked := func(what string, origin engine.Origin, id string, params map[string]any) {
		t.Helper()
		n, writes := len(asked), len(h.writer.Calls())
		h.must(t, origin, id, params, nil)
		if len(asked) != n {
			t.Fatalf("%s: asked %+v", what, asked[n:])
		}
		if len(h.writer.Calls()) == writes {
			t.Fatalf("%s: nothing was written", what)
		}
	}

	// object.create: a binding among other documents, and one inside a v1 List.
	mixed := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-1\n---\n" +
		"apiVersion: rbac.authorization.k8s.io/v1\nkind: RoleBinding\nmetadata:\n  name: ci-edit\n" +
		"roleRef:\n  apiGroup: rbac.authorization.k8s.io\n  kind: ClusterRole\n  name: edit\n" +
		"subjects:\n- kind: ServiceAccount\n  name: ci\n"
	asks("create with a role binding", "object.create", map[string]any{"yaml": mixed, "namespace": "shop"})
	list := "apiVersion: v1\nkind: List\nitems:\n- apiVersion: rbac.authorization.k8s.io/v1\n  kind: ClusterRoleBinding\n" +
		"  metadata:\n    name: ci-view\n  roleRef:\n    apiGroup: rbac.authorization.k8s.io\n    kind: ClusterRole\n    name: view\n" +
		"  subjects:\n  - kind: Group\n    name: ci\n"
	asks("create of a List with a cluster role binding", "object.create", map[string]any{"yaml": list})
	asks("create of a role (A1)", "object.create", map[string]any{"yaml": "apiVersion: rbac.authorization.k8s.io/v1\nkind: Role\n" +
		"metadata:\n  name: pod-reader\nrules:\n- apiGroups: [\"\"]\n  resources: [pods]\n  verbs: [get]\n", "namespace": "shop"})
	runsUnasked("the GUI's create with a role binding", gui, "object.create", map[string]any{"yaml": mixed, "namespace": "shop"})
	runsUnasked("the GUI's create of the List", gui, "object.create", map[string]any{"yaml": list})
	runsUnasked("an agent's create without a binding", agent, "object.create",
		map[string]any{"yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-2\n", "namespace": "shop"})

	// object.apply on an Edit tab of a binding: from yaml, and from the tab's text.
	rb := model.ObjectRef{Kind: "rolebindings", Namespace: "shop", Name: "existing"}
	crb := model.ObjectRef{Kind: "clusterrolebindings", Name: "existing-crb"}
	for _, ref := range []model.ObjectRef{rb, crb} {
		if _, err := h.s.Objects(context.Background(), ref.Kind, nil); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ok := testkit.WaitFor(ctx, func() bool { _, err := h.s.Lookup(ref); return err == nil })
		cancel()
		if !ok {
			t.Fatalf("the cache never held %s", ref.String())
		}
	}
	labelled := func(text, value string) string {
		t.Helper()
		obj, err := engine.ParseManifest(text)
		if err != nil {
			t.Fatal(err)
		}
		obj.SetLabels(map[string]string{"changed-by": value})
		out, err := engine.EditableYAML(obj)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var openedRB, openedCRB commands.EditResult
	h.must(t, agent, "object.edit", map[string]any{"ref": rb}, &openedRB)
	asks("apply of a role binding from yaml", "object.apply", map[string]any{"id": openedRB.Tab.ID, "yaml": labelled(openedRB.Text, "agent")})
	h.must(t, agent, "object.edit", map[string]any{"ref": crb}, &openedCRB)
	var set commands.TextResult
	h.must(t, agent, "editor.setText", map[string]any{"id": openedCRB.Tab.ID, "yaml": labelled(openedCRB.Text, "agent")}, &set)
	asks("apply of a cluster role binding from the tab's text", "object.apply", map[string]any{"id": openedCRB.Tab.ID, "textRevision": set.Revision})
	runsUnasked("the GUI's apply of the role binding", gui, "object.apply", map[string]any{"id": openedRB.Tab.ID, "yaml": labelled(openedRB.Text, "user")})

	// Accepted, the agent's binding is written; the confirmation named it.
	answer = true
	n := len(asked)
	h.must(t, agent, "object.apply", map[string]any{"id": openedCRB.Tab.ID, "textRevision": set.Revision}, nil)
	if len(asked) != n+1 || !slices.ContainsFunc(asked[n].Objects, func(o model.ObjectRef) bool { return o.Name == "existing-crb" }) {
		t.Errorf("the accepted apply: asked %+v", asked[n:])
	}

	// The registry and the MCP tools say which commands ask whatever the allowance.
	for id, want := range map[string]bool{"object.create": true, "object.apply": true, "roleBinding.create": true,
		"role.create": true, "clusterRole.create": true, "helm.chart.install": true, "helm.release.upgrade": true,
		"cluster.setPrometheus": true, "cluster.settings.set": true, "preferences.set": true,
		"namespace.create": false, "node.cordon": false, "clusterRoleBinding.create": false} {
		if c, ok := h.s.Registry().Get(id); !ok || c.AgentsAsk != want {
			t.Errorf("%s: agentsAsk %v, want %v", id, ok && c.AgentsAsk, want)
		}
	}
}

// TextHoldsRBAC judges by the kinds in the text: a Role, ClusterRole, RoleBinding or
// ClusterRoleBinding of the RBAC group in any document or List item, whatever the case of the kind
// (A1); a lookalike kind of another group is not one; text that does not parse counts as one (it is
// refused anyway).
func TestTextHoldsRBAC(t *testing.T) {
	for text, want := range map[string]bool{
		"":   false,
		"  ": false,
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n":                                                                                                false,
		"apiVersion: rbac.authorization.k8s.io/v1\nkind: Role\nmetadata:\n  name: a\n":                                                                           true,
		"apiVersion: rbac.authorization.k8s.io/v1\nkind: clusterrole\nmetadata:\n  name: a\n":                                                                    true,
		"apiVersion: example.io/v1\nkind: Role\nmetadata:\n  name: a\n":                                                                                          false,
		"apiVersion: rbac.authorization.k8s.io/v1\nkind: RoleBinding\nmetadata:\n  name: a\n":                                                                    true,
		"apiVersion: rbac.authorization.k8s.io/v1beta1\nkind: rolebinding\nmetadata:\n  name: a\n":                                                               true,
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\napiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRoleBinding\nmetadata:\n  name: b\n": true,
		"apiVersion: v1\nkind: List\nitems:\n- apiVersion: rbac.authorization.k8s.io/v1\n  kind: RoleBinding\n  metadata:\n    name: a\n":                        true,
		"apiVersion: example.io/v1\nkind: RoleBinding\nmetadata:\n  name: a\n":                                                                                   false,
		"kind: [x": true,
	} {
		if got := engine.TextHoldsRBAC(text); got != want {
			t.Errorf("TextHoldsRBAC(%q) = %v, want %v", text, got, want)
		}
	}
	if !engine.RBACKind("not a version///", "ClusterRoleBinding") || !engine.RBACKind("not a version///", "Role") ||
		engine.RBACKind("v1", "Binding") {
		t.Error("RBACKind: an unparseable apiVersion is judged by the kind; a core Binding is not an RBAC object")
	}
}

// An agent's cluster.setPrometheus always waits for the user, also where agent writes are allowed,
// the question naming the service (A16, owner; H-26); declined, nothing is set. The window's own
// setting is not asked.
func TestAgentSetPrometheusAsks(t *testing.T) {
	var asked []engine.ConfirmRequest
	h := newHarness(t, seed(), engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return false, nil
	})})
	h.s.SetAgentWrites("test", true)
	params := map[string]any{"address": "monitoring/lookalike:9090"}
	if err := h.run(t, engine.OriginExternalAgent, "cluster.setPrometheus", params, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an agent's setPrometheus: %v, want ErrDeclined", err)
	}
	if len(asked) != 1 || !asked[0].AgentsAsk || !strings.Contains(asked[0].Detail, "monitoring/lookalike:9090") {
		t.Errorf("asked %+v", asked)
	}
	_ = h.run(t, engine.OriginGUI, "cluster.setPrometheus", params, nil) // may fail without a settings store; never asks
	if len(asked) != 1 {
		t.Errorf("the window's setPrometheus asked: %+v", asked[1:])
	}
}
