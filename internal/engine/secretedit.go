package engine

import (
	"context"
	"encoding/base64"
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/helm"
)

// Editing and creating Secrets from YAML, in the GUI only (D-19, owner). An Edit or Create resource
// tab that holds a Secret shows the user its values (base64 in data, plain text in stringData) and
// works like any other editor tab. Agents and the CLI can neither open such a tab, nor read its
// text, nor change, check or apply it, nor create a Secret: the same pattern as secret.reveal. The
// check sits in the engine functions each of those commands calls, so every entry point has it, and
// its text never carries a value.

// ErrSecretGUIOnly refuses an agent or the CLI anything that opens, reads, changes, checks, applies
// or creates a Secret from YAML, and the same for a Helm release record kept in a ConfigMap
// (HELM_DRIVER=configmap), whose payload holds the release's values (RedactManifests).
var ErrSecretGUIOnly = errors.New("a Secret, or a Helm release record kept in a ConfigMap, is edited and created only by " +
	"the user, in Cogpit's own window: agents and the CLI cannot open, read, change, check, apply or create one from YAML")

// secretDataField is a Secret manifest's field of base64-encoded values.
const secretDataField = "data"

// secretValueFields are the fields of a Secret manifest that hold values.
var secretValueFields = []string{secretDataField, "stringData"}

// requireGUIForKind refuses every origin but the GUI for a kind whose YAML holds values.
func requireGUIForKind(ctx context.Context, apiVersion, kind string) error {
	if RedactedKind(apiVersion, kind) && OriginOf(ctx) != OriginGUI {
		return ErrSecretGUIOnly
	}
	return nil
}

// TextHoldsSecret reports whether manifest text holds a Secret (or a list with one): the text an
// agent must not read, set, check or apply.
func TextHoldsSecret(text string) bool {
	_, changed := RedactManifests(text, false)
	return changed
}

// GuardSecretText refuses every origin but the GUI a text that holds a Secret. text is what a
// command would put into a tab or send to the cluster.
func GuardSecretText(ctx context.Context, text string) error {
	if OriginOf(ctx) != OriginGUI && TextHoldsSecret(text) {
		return ErrSecretGUIOnly
	}
	return GuardAgentMarkers(ctx, text) // a placeholder an agent read in place of a value (agentview.go)
}

// EditorHoldsSecret reports whether the editor tab id edits a Secret or holds a Secret's manifest
// in its text. A tab that does not exist holds none.
func (s *Session) EditorHoldsSecret(id string) bool {
	e, err := s.editorOf(id)
	if err != nil {
		return false
	}
	if e.tab.Type == DockEdit && RedactedKind(e.target.APIVersion, e.target.Kind) {
		return true
	}
	return !HelmValuesTab(e.tab.Type) && TextHoldsSecret(e.text)
}

// GuardSecretEditor refuses every origin but the GUI a command on the editor tab id when the tab
// holds a Secret or text (the command's own yaml parameter, "" for none) holds one.
func (s *Session) GuardSecretEditor(ctx context.Context, id, text string) error {
	if OriginOf(ctx) == OriginGUI {
		return nil
	}
	if s.EditorHoldsSecret(id) || TextHoldsSecret(text) {
		return ErrSecretGUIOnly
	}
	return GuardAgentMarkers(ctx, text) // a placeholder an agent read in place of a value (agentview.go)
}

// secretValues returns the values of a Secret manifest as ScrubValues takes them: every data and
// stringData entry, and the decoded form of each data entry.
func secretValues(obj *unstructured.Unstructured) map[string]any {
	out := map[string]any{}
	for _, field := range secretValueFields {
		m, ok := obj.Object[field].(map[string]any)
		if !ok {
			continue
		}
		for k, v := range m {
			str, ok := v.(string)
			if !ok {
				continue
			}
			out[field+"/"+k] = str
			if field == secretDataField {
				if dec, err := base64.StdEncoding.DecodeString(str); err == nil {
					out["decoded/"+k] = string(dec)
				}
			}
		}
	}
	return out
}

// scrubSecretWrite returns err with the values of the Secret manifest obj removed from its text,
// so that an API server's message that quotes a value never reaches an error, a toast or the audit
// log. A ConflictError, whose text names versions only, and a nil error pass unchanged. Errors for
// other kinds pass unchanged.
func scrubSecretWrite(err error, obj *unstructured.Unstructured) error {
	if err == nil || obj == nil || !RedactedKind(obj.GetAPIVersion(), obj.GetKind()) {
		return err
	}
	var ce *ConflictError
	if errors.As(err, &ce) {
		return err
	}
	return errors.New(helm.ScrubValues(err.Error(), secretValues(obj)))
}
