package commands

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Create Secret (G9, D-19): the eighth create dialog, behind the Secrets view's "+". Its fields are
// inferred from Lens's documentation of the Secrets view (the dialog is not captured): name,
// namespace, type, the data as key and value rows, labels and annotations. The command checks them
// as the API server would (names, keys, the type's required keys, the sizes) before anything is
// sent, then creates the Secret as every create dialog does: a server-side dry run, then the
// create (engine.CreateManifest).
//
// D-19: a Secret is created only by the user in Cogpit's window. The command is in the registry,
// an MCP tool like every command, but it refuses every origin but the GUI before anyone is asked
// (Refuse) and again in Run. No value reaches the audit log (engine.auditSecretCreateParams keeps
// the keys), an error (the commands name keys only; an API message that quotes a value is scrubbed
// by CreateObjects), a toast, a log, or a draft (a dialog keeps none).
//
// The values go out base64-encoded in data, not in stringData (Cogpit's default): data is what the
// API server stores and returns, so what was sent is exactly what is kept, with no merge of the
// two fields to reason about; kubectl create secret sends data too; and the scrubbing of an API
// message knows each value in both forms (engine.secretValues).

// Bounds of a Secret, as the API server keeps them.
const (
	maxSecretBytes     = 1 << 20   // the data's values together (corev1.MaxSecretSize)
	maxAnnotationBytes = 256 << 10 // the annotations' keys and values together
)

// The basic-auth keys, of which the API server requires at least one.
const (
	basicAuthUsername = "username"
	basicAuthPassword = "password"
)

var createSecretKind = createKind{gvr: schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
	kind: "Secret", noun: "secret", namespaced: true, nameRule: validation.IsDNS1123Subdomain}

// secretEntryParams is one data entry of a Secret.
type secretEntryParams struct {
	Key   string `json:"key" jsonschema:"the data key: letters, digits, '-', '_' and '.', at most 253 characters"`
	Value string `json:"value" jsonschema:"the value as text, sent base64-encoded in the Secret's data; it may span several lines (a PEM certificate or key)"`
}

// metaEntryParams is one label or annotation.
type metaEntryParams struct {
	Key   string `json:"key" jsonschema:"the key: a name, optionally with a DNS subdomain prefix and '/' (app.kubernetes.io/name)"`
	Value string `json:"value" jsonschema:"the value"`
}

type secretCreateParams struct {
	engine.OnCluster
	Name        string              `json:"name" jsonschema:"the Secret's name: a DNS-1123 subdomain (lowercase letters, digits, '-' and '.', at most 253 characters)"`
	Namespace   string              `json:"namespace,omitempty" jsonschema:"the namespace to create it in; empty uses the namespace filter's single namespace, else the context's default"`
	Type        string              `json:"type,omitempty" jsonschema:"the Secret's type: Opaque (the default), kubernetes.io/service-account-token, kubernetes.io/dockercfg, kubernetes.io/dockerconfigjson, kubernetes.io/basic-auth, kubernetes.io/ssh-auth, kubernetes.io/tls or bootstrap.kubernetes.io/token"`
	Data        []secretEntryParams `json:"data,omitempty" jsonschema:"the data entries, at most 100, each key once; a typed Secret needs its type's keys (tls.crt and tls.key, ...)"`
	Labels      []metaEntryParams   `json:"labels,omitempty" jsonschema:"labels, at most 100, each key once"`
	Annotations []metaEntryParams   `json:"annotations,omitempty" jsonschema:"annotations, at most 100, each key once; a service-account-token Secret names its account in kubernetes.io/service-account.name"`
}

// refuseSecretCreate refuses every origin but the GUI (D-19).
func refuseSecretCreate(ctx context.Context) error {
	if engine.OriginOf(ctx) != engine.OriginGUI {
		return engine.ErrSecretCreateGUIOnly
	}
	return nil
}

func registerSecretCreate(g *registrar) {
	spec := createSpec(engine.SecretCreateCommand, "Create Secret", "Secret/Create",
		"Creates a Secret (name, namespace, type, data entries, labels, annotations) after a server-side dry run, "+
			"for the user in Cogpit's window only (D-19): an agent or the CLI is refused before anything is asked or "+
			"sent, whatever the parameters, and no value is ever logged. To create a Secret, ask the user to use "+
			"Create Secret behind the Secrets view's +.",
		createSecretKind, engine.DangerMutating,
		func(p secretCreateParams) string { return p.Namespace }, buildSecret)
	spec.Prefill = func(s *engine.Session, p secretCreateParams) (secretCreateParams, error) {
		ns, err := s.CreateNamespace(p.Context, p.Namespace)
		p.Namespace = ns
		return p, err
	}
	spec.GUIOnly = engine.ErrSecretCreateGUIOnly // Dispatch refuses every other origin first
	spec.Refuse = func(ctx context.Context, _ *engine.Session, _ secretCreateParams) error {
		return refuseSecretCreate(ctx)
	}
	spec.Effects = func(_ *engine.Session, p secretCreateParams) []engine.Effect { return secretEffects(p) }
	create := spec.Run
	spec.Run = func(ctx context.Context, s *engine.Session, p secretCreateParams) (any, error) {
		if err := refuseSecretCreate(ctx); err != nil { // Refuse ran already; this keeps Run safe on its own
			return nil, err
		}
		return create(ctx, s, p)
	}
	add(g, spec)
}

// buildSecret builds the Secret's manifest from the dialog's fields, checked as the API server
// would check them. No error names a value.
func buildSecret(s *engine.Session, p secretCreateParams) (map[string]any, model.ObjectRef, error) {
	obj, ref, err := createSecretKind.object(s, p.Context, p.Name, p.Namespace)
	if err != nil {
		return nil, ref, err
	}
	typ, err := secretType(p.Type)
	if err != nil {
		return nil, ref, err
	}
	data, err := secretData(p.Data)
	if err != nil {
		return nil, ref, err
	}
	labels, err := secretLabels(p.Labels)
	if err != nil {
		return nil, ref, err
	}
	annotations, err := secretAnnotations(p.Annotations)
	if err != nil {
		return nil, ref, err
	}
	if err := checkSecretType(typ, p.Data, annotations); err != nil {
		return nil, ref, err
	}
	meta := obj["metadata"].(map[string]any)
	if len(labels) > 0 {
		meta["labels"] = labels
	}
	if len(annotations) > 0 {
		meta["annotations"] = annotations
	}
	obj["type"] = typ
	if len(data) > 0 {
		obj["data"] = data
	}
	return obj, ref, nil
}

// secretType returns the type, Opaque when empty; one of engine.SecretTypes.
func secretType(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" {
		return engine.SecretTypeOpaque, nil
	}
	if !slices.Contains(engine.SecretTypes, t) {
		return "", fmt.Errorf("secret type %q: one of %s", t, strings.Join(engine.SecretTypes, ", "))
	}
	return t, nil
}

// secretData checks the data entries (each key once and valid, the values together at most 1 MiB)
// and returns them base64-encoded. Errors name keys and positions, never values.
func secretData(entries []secretEntryParams) (map[string]any, error) {
	if len(entries) > maxCreateItems {
		return nil, fmt.Errorf("%d data entries, at most %d", len(entries), maxCreateItems)
	}
	out := make(map[string]any, len(entries))
	size := 0
	for i, e := range entries {
		key := strings.TrimSpace(e.Key)
		if key == "" {
			return nil, fmt.Errorf("data entry %d has no key", i+1)
		}
		if errs := validation.IsConfigMapKey(key); len(errs) > 0 {
			return nil, fmt.Errorf("data key %q is not valid: %s", key, strings.Join(errs, "; "))
		}
		if _, twice := out[key]; twice {
			return nil, fmt.Errorf("data key %q is given twice", key)
		}
		size += len(e.Value)
		out[key] = base64.StdEncoding.EncodeToString([]byte(e.Value))
	}
	if size > maxSecretBytes {
		return nil, fmt.Errorf("the values come to %s, a Secret holds at most %s", model.FormatBytes(int64(size)), model.FormatBytes(maxSecretBytes))
	}
	return out, nil
}

// metaEntries checks labels or annotations: each key a qualified name, given once, at most
// maxCreateItems of them.
func metaEntries(what string, entries []metaEntryParams, trimValues bool) (map[string]any, error) {
	if len(entries) > maxCreateItems {
		return nil, fmt.Errorf("%d %ss, at most %d", len(entries), what, maxCreateItems)
	}
	out := make(map[string]any, len(entries))
	for i, e := range entries {
		key, value := strings.TrimSpace(e.Key), e.Value
		if trimValues {
			value = strings.TrimSpace(value)
		}
		if key == "" {
			return nil, fmt.Errorf("%s %d has no key", what, i+1)
		}
		if errs := validation.IsQualifiedName(key); len(errs) > 0 {
			return nil, fmt.Errorf("%s key %q is not valid: %s", what, key, strings.Join(errs, "; "))
		}
		if _, twice := out[key]; twice {
			return nil, fmt.Errorf("%s key %q is given twice", what, key)
		}
		out[key] = value
	}
	return out, nil
}

// secretLabels checks the labels: qualified names, values as label values.
func secretLabels(entries []metaEntryParams) (map[string]any, error) {
	labels, err := metaEntries("label", entries, true)
	if err != nil {
		return nil, err
	}
	for k, v := range labels {
		if errs := validation.IsValidLabelValue(v.(string)); len(errs) > 0 {
			return nil, fmt.Errorf("label %s: the value is not valid: %s", k, strings.Join(errs, "; "))
		}
	}
	return labels, nil
}

// secretAnnotations checks the annotations: qualified names, together at most 256 KiB.
func secretAnnotations(entries []metaEntryParams) (map[string]any, error) {
	annotations, err := metaEntries("annotation", entries, false)
	if err != nil {
		return nil, err
	}
	size := 0
	for k, v := range annotations {
		size += len(k) + len(v.(string))
	}
	if size > maxAnnotationBytes {
		return nil, fmt.Errorf("the annotations come to %s, at most %s", model.FormatBytes(int64(size)), model.FormatBytes(maxAnnotationBytes))
	}
	return annotations, nil
}

// checkSecretType checks what the API server requires of a typed Secret: a service account
// token's account, a docker config's key holding JSON, basic auth's username or password, the SSH
// key, the TLS pair. A bootstrap token's keys are its signer's business, not the API server's.
func checkSecretType(typ string, entries []secretEntryParams, annotations map[string]any) error {
	values := map[string]string{}
	for _, e := range entries {
		values[strings.TrimSpace(e.Key)] = e.Value
	}
	has := func(k string) bool { _, ok := values[k]; return ok }
	switch typ {
	case engine.SecretTypeServiceAccountToken:
		if name, _ := annotations[engine.ServiceAccountNameAnnotation].(string); strings.TrimSpace(name) == "" {
			return fmt.Errorf("a %s Secret names its service account in the annotation %s", typ, engine.ServiceAccountNameAnnotation)
		}
	case engine.SecretTypeDockercfg, engine.SecretTypeDockerConfigJSON:
		key := engine.SecretTypeKeys(typ)[0]
		if !has(key) {
			return fmt.Errorf("a %s Secret needs the key %s", typ, key)
		}
		if !json.Valid([]byte(values[key])) {
			return fmt.Errorf("the value of %s is not JSON", key) // json.Valid quotes nothing
		}
	case engine.SecretTypeBasicAuth:
		if !has(basicAuthUsername) && !has(basicAuthPassword) {
			return fmt.Errorf("a %s Secret needs the key %s or %s", typ, basicAuthUsername, basicAuthPassword)
		}
	case engine.SecretTypeSSHAuth:
		if values["ssh-privatekey"] == "" {
			return fmt.Errorf("a %s Secret needs the key ssh-privatekey with the private key", typ)
		}
	case engine.SecretTypeTLS:
		for _, k := range engine.SecretTypeKeys(typ) {
			if !has(k) {
				return fmt.Errorf("a %s Secret needs the keys tls.crt and tls.key", typ)
			}
		}
	}
	return nil
}

// secretEffects are the Secret's rows in a confirmation: its type and keys, never a value. Only
// the GUI runs the command, which confirms nothing mutating; the rows are there for a session
// whose agents' writes are confirmed all the same.
func secretEffects(p secretCreateParams) []engine.Effect {
	typ, err := secretType(p.Type)
	if err != nil {
		typ = strings.TrimSpace(p.Type)
	}
	keys := make([]string, 0, len(p.Data))
	for _, e := range p.Data {
		keys = append(keys, strings.TrimSpace(e.Key))
	}
	out := []engine.Effect{{Name: "Type", Value: typ, Tone: engine.EffectNames}}
	if len(keys) == 0 {
		return append(out, engine.Effect{Name: "Keys", Value: "none"})
	}
	return append(out, engine.Effect{Name: "Keys", Value: cappedWith(keys, ", "), Tone: engine.EffectNames},
		engine.Effect{Name: effectValues, Value: "not shown"})
}
