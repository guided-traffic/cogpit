package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestCreateRefusesWhenTheConfirmedNamespaceMoved: a manifest without a namespace takes the
// filter's; when the filter changes while the dialog is open, the create is refused instead of
// landing in a namespace the user did not confirm.
func TestCreateRefusesWhenTheConfirmedNamespaceMoved(t *testing.T) {
	var h *harness
	var confirmed engine.ConfirmRequest
	opts := engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		confirmed = req
		raw, _ := json.Marshal(map[string]any{"namespaces": []string{"infra"}})
		if _, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: "ui.setNamespace", Params: raw, Origin: engine.OriginExternalAgent}); err != nil {
			t.Errorf("setNamespace: %v", err)
		}
		return true, nil
	})}
	h = newHarness(t, seed(), opts)
	srv := serveObjects(h)
	h.must(t, engine.OriginGUI, "ui.setNamespace", map[string]any{"namespaces": []string{"shop"}}, nil)

	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: review\ndata:\n  a: b\n"
	err := h.run(t, engine.OriginExternalAgent, "object.create", map[string]any{"yaml": manifest}, nil)
	if !errors.Is(err, engine.ErrTargetsChanged) {
		t.Fatalf("object.create after the namespace moved: %v, want ErrTargetsChanged", err)
	}
	if len(confirmed.Namespaces) != 1 || confirmed.Namespaces[0] != "shop" {
		t.Errorf("the confirmation named %v, want shop", confirmed.Namespaces)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.creates) != 0 {
		t.Errorf("created %v; nothing should be created", srv.creates)
	}
}

// TestAgentDryRunKeepsTheUsersBase: an agent's dry run (a read) after a concurrent change reports
// the conflict but does not move the Edit tab's base, so the user's review still says stale and
// the user's apply meets the conflict instead of overwriting the other change.
func TestAgentDryRunKeepsTheUsersBase(t *testing.T) {
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
	cachedRV(t, h, checkout, opened.BaseResourceVersion)

	var ce *engine.ConflictError
	if err := h.run(t, engine.OriginExternalAgent, "object.dryRun", map[string]any{"id": opened.Tab.ID}, nil); !errors.As(err, &ce) || ce.Rebased {
		t.Fatalf("agent dry run: %v; want a conflict that does not rebase", err)
	}
	var diff engine.EditDiff
	h.must(t, gui, "editor.diff", map[string]any{"id": opened.Tab.ID, "yaml": edited}, &diff)
	if !diff.Stale || diff.BaseResourceVersion != opened.BaseResourceVersion {
		t.Errorf("the user's review after the agent's dry run: stale %v, base %s; want stale on base %s", diff.Stale, diff.BaseResourceVersion, opened.BaseResourceVersion)
	}
	err = h.run(t, gui, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": edited}, nil)
	if !errors.As(err, &ce) || !ce.Rebased {
		t.Fatalf("the user's first apply: %v; want a conflict that rebases", err)
	}
	if cur, _ := h.s.Lookup(checkout); cur.GetAnnotations()["changed-by"] != "someone-else" {
		t.Errorf("the other client's change was overwritten without a conflict: %v", cur.GetAnnotations())
	}
}

// TestDeleteKeepsTheConfirmedUID: an object deleted and created again under its name while the
// dialog is open is not deleted: the delete carries the UID the user confirmed.
func TestDeleteKeepsTheConfirmedUID(t *testing.T) {
	var h *harness
	cart := model.ObjectRef{Kind: "services", Namespace: "shop", Name: "cart"}
	var asked engine.ConfirmRequest
	opts := engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = req
		h.src.Delete(testkit.ServicesGVR, "shop", "cart")
		repl := testkit.Service("shop", "cart", "10.96.100.101")
		repl.SetUID(types.UID("uid-replacement"))
		h.src.Set(testkit.ServicesGVR, repl)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !testkit.WaitFor(ctx, func() bool {
			o, err := h.s.Lookup(cart)
			return err == nil && string(o.GetUID()) == "uid-replacement"
		}) {
			t.Error("the cache never showed the replacement") // the delete would carry the original's UID untested
		}
		return true, nil
	})}
	h = newHarness(t, seed(), opts)
	srv := serveObjects(h)
	if _, err := h.s.Objects(context.Background(), "services", nil); err != nil {
		t.Fatal(err)
	}
	_ = h.run(t, engine.OriginGUI, "object.delete", map[string]any{"ref": cart}, nil)
	if len(asked.UIDs) != 1 || asked.UIDs[0] == "" || asked.UIDs[0] == "uid-replacement" {
		t.Fatalf("the confirmation recorded UIDs %v, want the original's", asked.UIDs)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, d := range srv.deletes {
		if strings.HasSuffix(d, "uid-replacement") {
			t.Errorf("the replacement was deleted with its own UID as the precondition: %v", srv.deletes)
		}
	}
}

// TestAuditRedactsSecretLists: a SecretList's items are Secrets too.
func TestAuditRedactsSecretLists(t *testing.T) {
	var buf bytes.Buffer
	h := newHarness(t, seed(), engine.Options{Audit: &buf, Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil })})
	serveObjects(h)
	manifest := "apiVersion: v1\nkind: SecretList\nmetadata: {}\nitems:\n- apiVersion: v1\n  kind: Secret\n  metadata:\n    name: db\n  stringData:\n    password: hunter2-review\n"
	_ = h.run(t, engine.OriginExternalAgent, "object.create", map[string]any{"yaml": manifest, "namespace": "shop"}, nil)
	_ = h.run(t, engine.OriginExternalAgent, "editor.setText", map[string]any{"id": "create-1", "yaml": manifest}, nil)
	_ = h.run(t, engine.OriginExternalAgent, "object.create", map[string]any{"yaml": map[string]any{"password": "hunter2-review"}}, nil)
	if strings.Contains(buf.String(), "hunter2-review") {
		t.Errorf("Secret value in the audit log:\n%s", buf.String())
	}
}

// TestEditorGetRedactsSecretsForAgents: a Secret the user pasted into a Create tab is shown to
// the user (GUI origin) and withheld whole from agents (D-19).
func TestEditorGetRedactsSecretsForAgents(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	serveObjects(h)
	manifest := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\nstringData:\n  password: hunter2-paste\n"
	var tab engine.EditorState
	h.must(t, engine.OriginGUI, "resource.createTab", map[string]any{"yaml": manifest}, &tab)
	var gui, agent engine.EditorState
	h.must(t, engine.OriginGUI, "editor.get", map[string]any{"id": tab.Tab.ID}, &gui)
	h.must(t, engine.OriginExternalAgent, "editor.get", map[string]any{"id": tab.Tab.ID}, &agent)
	if !strings.Contains(gui.Text, "hunter2-paste") {
		t.Errorf("the user's own tab lost its text: %q", gui.Text)
	}
	if strings.Contains(agent.Text, "hunter2-paste") || agent.Text != model.Withheld {
		t.Errorf("an agent read the Secret: %q", agent.Text)
	}
}

// TestApplyToZeroReplicasIsDestructive: an edit that sets spec.replicas to 0 escalates to
// destructive (confirmed for every origin, the GUI included); one that leaves the count alone does
// not ask the GUI.
func TestApplyToZeroReplicasIsDestructive(t *testing.T) {
	conf := &confirmations{answer: false}
	h, _ := editHarness(t, engine.Options{Confirmer: conf})
	var opened commands.EditResult
	h.must(t, engine.OriginGUI, "object.edit", map[string]any{"ref": checkout}, &opened)
	if !h.s.EditScalesToZero(opened.Tab.ID, strings.Replace(opened.Text, "spec:\n", "spec:\n  replicas: 0\n", 1)) {
		t.Fatal("an edit to zero replicas does not escalate")
	}
	if h.s.EditScalesToZero(opened.Tab.ID, opened.Text) {
		t.Fatal("an edit without replicas escalates")
	}
	toZero := strings.Replace(opened.Text, "spec:\n", "spec:\n  replicas: 0\n", 1)
	if err := h.run(t, engine.OriginGUI, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": toZero}, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("apply to zero from the GUI: %v, want the user's refusal of a confirmation", err)
	}
	conf.mu.Lock()
	defer conf.mu.Unlock()
	if len(conf.reqs) != 1 || conf.reqs[0].Danger != engine.DangerDestructive {
		t.Fatalf("confirmations %+v, want one destructive", conf.reqs)
	}
}
