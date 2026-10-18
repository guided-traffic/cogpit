package commands_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

const (
	editedSecretValue = "hunter2-edit-9f3a"
	newSecretValue    = "s3cr3t-new-77c1"
)

var dbSecret = model.ObjectRef{Kind: "secrets", Namespace: "shop", Name: "db"}

// secretEditHarness is the edit harness with one Secret in the cache and an audit log; every
// confirmation is answered yes, so only the engine's own refusals stop an agent.
func secretEditHarness(t *testing.T) (*harness, *objectServer, *bytes.Buffer) {
	t.Helper()
	var audit bytes.Buffer
	h, srv := editHarness(t, engine.Options{Audit: &audit, Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil })})
	h.src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "db", map[string]any{
		"data": map[string]any{"password": base64.StdEncoding.EncodeToString([]byte(editedSecretValue))},
	}))
	return h, srv, &audit
}

func secretLeak(s string) string {
	for _, l := range []string{editedSecretValue, base64.StdEncoding.EncodeToString([]byte(editedSecretValue)), newSecretValue, base64.StdEncoding.EncodeToString([]byte(newSecretValue))} {
		if strings.Contains(s, l) {
			return l
		}
	}
	return ""
}

// TestSecretEditAndCreateAreGUIOnly (D-19): the GUI opens a Secret's Edit tab with its values,
// diffs, checks and applies the edit, and creates a Secret; agents and the CLI can neither open the
// tab, read or change its text, check or apply it, nor create a Secret; no value reaches the audit
// log, an error or a toast.
func TestSecretEditAndCreateAreGUIOnly(t *testing.T) {
	h, srv, audit := secretEditHarness(t)
	gui := engine.OriginGUI
	var toasts []string
	h.s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventToast {
			toasts = append(toasts, ev.Message)
		}
	})

	// The user: edit, diff, dry run, apply.
	var opened commands.EditResult
	h.must(t, gui, "object.edit", map[string]any{"ref": dbSecret}, &opened)
	if !strings.Contains(opened.Text, base64.StdEncoding.EncodeToString([]byte(editedSecretValue))) {
		t.Fatalf("the Edit tab does not show the Secret's values:\n%s", opened.Text)
	}
	edited := strings.Replace(opened.Text, "\ndata:", "\nstringData:\n  token: "+newSecretValue+"\ndata:", 1)
	h.must(t, gui, "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": edited}, nil)
	var diff engine.EditDiff
	h.must(t, gui, "editor.diff", map[string]any{"id": opened.Tab.ID}, &diff)
	if !diff.Changed || diff.Added == 0 {
		t.Errorf("diff %+v", diff)
	}
	h.must(t, gui, "object.dryRun", map[string]any{"id": opened.Tab.ID}, nil)
	var applied engine.ApplyResult
	h.must(t, gui, "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": edited}, &applied)
	if v, _, _ := unstructured.NestedString(srv.lastUpdate().Object, "stringData", "token"); v != newSecretValue {
		t.Errorf("the edit was not sent: %v", srv.lastUpdate().Object)
	}
	var created engine.CreateResult
	h.must(t, gui, "object.create", map[string]any{"yaml": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: fresh\n  namespace: shop\nstringData:\n  k: " + newSecretValue + "\n"}, &created)
	if created.Created != 1 {
		t.Errorf("the GUI's create: %+v", created)
	}
	var tab engine.EditorState
	h.must(t, gui, "resource.createTab", map[string]any{"yaml": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: draft\nstringData:\n  k: " + newSecretValue + "\n"}, &tab)
	guiCalls := len(h.writer.Calls())

	// Agents and the CLI: every way in is closed.
	secretText := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  namespace: shop\nstringData:\n  k: " + newSecretValue + "\n"
	for _, origin := range []engine.Origin{engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		refuse := func(what, id string, params map[string]any) {
			t.Helper()
			err := h.run(t, origin, id, params, nil)
			if refused := errors.Is(err, engine.ErrSecretGUIOnly) || (id == "object.create" && err != nil && strings.Contains(err.Error(), "only by the user")); !refused {
				t.Errorf("%s from %s: %v, want the GUI-only refusal", what, origin, err)
			} else if l := secretLeak(err.Error()); l != "" {
				t.Errorf("%s from %s: the refusal carries %q", what, origin, l)
			}
		}
		refuse("open the Secret's Edit tab", "object.edit", map[string]any{"ref": dbSecret})
		refuse("set the Edit tab's text", "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": edited})
		refuse("diff the Edit tab", "editor.diff", map[string]any{"id": opened.Tab.ID})
		refuse("dry run the Edit tab", "object.dryRun", map[string]any{"id": opened.Tab.ID})
		refuse("apply the Edit tab", "object.apply", map[string]any{"id": opened.Tab.ID, "textRevision": 1})
		refuse("apply a Secret's yaml", "object.apply", map[string]any{"id": opened.Tab.ID, "yaml": secretText})
		refuse("set a Create tab's text to a Secret", "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": secretText})
		refuse("diff with a Secret's yaml", "editor.diff", map[string]any{"id": opened.Tab.ID, "yaml": secretText})
		refuse("open a Create tab with a Secret", "resource.createTab", map[string]any{"yaml": secretText})
		refuse("create a Secret", "object.create", map[string]any{"yaml": secretText})

		var got engine.EditorState
		h.must(t, origin, "editor.get", map[string]any{"id": opened.Tab.ID}, &got)
		var got2 engine.EditorState
		h.must(t, origin, "editor.get", map[string]any{"id": tab.Tab.ID}, &got2)
		if got.Text != model.Withheld || got2.Text != model.Withheld {
			t.Errorf("%s read a Secret tab: %q / %q", origin, got.Text, got2.Text)
		}
	}
	if calls := h.writer.Calls(); len(calls) != guiCalls {
		t.Errorf("a refused command reached the cluster: %v", calls[guiCalls:])
	}
	if l := secretLeak(audit.String()); l != "" {
		t.Errorf("%q in the audit log:\n%s", l, audit)
	}
	for _, msg := range toasts {
		if l := secretLeak(msg); l != "" {
			t.Errorf("%q in the toast %q", l, msg)
		}
	}
}

// TestSecretWriteErrorsNeverQuoteValues: an API server message that quotes a value of the Secret
// being written reaches the user without it, from Apply, the dry run and Create.
func TestSecretWriteErrorsNeverQuoteValues(t *testing.T) {
	h, srv, audit := secretEditHarness(t)
	gui := engine.OriginGUI
	var opened commands.EditResult
	h.must(t, gui, "object.edit", map[string]any{"ref": dbSecret}, &opened)
	edited := strings.Replace(opened.Text, "\ndata:", "\nstringData:\n  token: "+newSecretValue+"\ndata:", 1)
	quote := func() error {
		return fmt.Errorf("Secret %q is invalid: stringData[token]: Invalid value: %q", "db", newSecretValue)
	}
	for _, cmd := range []string{"object.dryRun", "object.apply"} {
		srv.mu.Lock()
		srv.failNext = quote()
		srv.mu.Unlock()
		err := h.run(t, gui, cmd, map[string]any{"id": opened.Tab.ID, "yaml": edited}, nil)
		if err == nil || secretLeak(err.Error()) != "" || !strings.Contains(err.Error(), "is invalid") {
			t.Errorf("%s: %v", cmd, err)
		}
	}
	srv.mu.Lock()
	srv.failNext = quote()
	srv.mu.Unlock()
	err := h.run(t, gui, "object.create", map[string]any{"yaml": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: fresh\n  namespace: shop\nstringData:\n  token: " + newSecretValue + "\n"}, nil)
	if err == nil || secretLeak(err.Error()) != "" {
		t.Errorf("object.create: %v", err)
	}
	if l := secretLeak(audit.String()); l != "" {
		t.Errorf("%q in the audit log", l)
	}
}

// TestSecretTabSurvivesForOtherKinds: the guards only look at Secrets; an agent still edits and
// creates everything else.
func TestSecretGuardsLeaveOtherKindsAlone(t *testing.T) {
	h, _, _ := secretEditHarness(t)
	var opened commands.EditResult
	h.must(t, engine.OriginExternalAgent, "object.edit", map[string]any{"ref": checkout}, &opened)
	if !strings.Contains(opened.Text, "clusterIP") {
		t.Errorf("an agent's Edit tab of a Service: %q", opened.Text)
	}
	h.must(t, engine.OriginExternalAgent, "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": opened.Text + "# note\n"}, nil)
	var st engine.EditorState
	h.must(t, engine.OriginExternalAgent, "editor.get", map[string]any{"id": opened.Tab.ID}, &st)
	if st.Text == model.Withheld || !slices.Contains(strings.Split(st.Text, "\n"), "# note") {
		t.Errorf("an agent cannot read its own Service tab: %q", st.Text)
	}
}
