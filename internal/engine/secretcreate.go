package engine

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Create Secret (G9; D-19, owner): Lens's eighth create dialog, behind the Secrets view's "+". Its
// command (package commands, secretcreate.go) builds the Secret from the dialog's fields and
// creates it as the other create dialogs do (CreateManifest), for the GUI only: an agent or the CLI
// is refused before anyone is asked. What the engine adds here: the types and the keys the dialog
// starts a typed Secret with, the refusal, and the audit line of a refused call, which names the
// Secret, its type and its keys, never a value.

// SecretCreateCommand is the id of the Create Secret dialog's command.
const SecretCreateCommand = "secret.create"

// The Secret types the dialog offers, Kubernetes' built-in ones (inferred from Lens's
// documentation of the Secrets view; its dialog is not captured). Names, no credentials.
const (
	SecretTypeOpaque              = "Opaque"                              //nolint:gosec // a Secret type's name, no credential
	SecretTypeServiceAccountToken = "kubernetes.io/service-account-token" //nolint:gosec // a Secret type's name, no credential
	SecretTypeDockercfg           = "kubernetes.io/dockercfg"             //nolint:gosec // a Secret type's name, no credential
	SecretTypeDockerConfigJSON    = "kubernetes.io/dockerconfigjson"      //nolint:gosec // a Secret type's name, no credential
	SecretTypeBasicAuth           = "kubernetes.io/basic-auth"            //nolint:gosec // a Secret type's name, no credential
	SecretTypeSSHAuth             = "kubernetes.io/ssh-auth"              //nolint:gosec // a Secret type's name, no credential
	SecretTypeTLS                 = "kubernetes.io/tls"                   //nolint:gosec // a Secret type's name, no credential
	SecretTypeBootstrapToken      = "bootstrap.kubernetes.io/token"       //nolint:gosec // a Secret type's name, no credential

	// ServiceAccountNameAnnotation names the service account a service-account-token Secret is
	// for; the API server requires it.
	ServiceAccountNameAnnotation = "kubernetes.io/service-account.name"
)

// SecretTypes are the types the dialog offers, in its order; Opaque first, the default.
var SecretTypes = []string{
	SecretTypeOpaque, SecretTypeServiceAccountToken, SecretTypeDockercfg, SecretTypeDockerConfigJSON,
	SecretTypeBasicAuth, SecretTypeSSHAuth, SecretTypeTLS, SecretTypeBootstrapToken,
}

// secretTypeKeys are the data keys a typed Secret's dialog starts with (Cogpit's default): the
// keys the API server requires of the type, and a bootstrap token's two, which its signer needs.
var secretTypeKeys = map[string][]string{
	SecretTypeDockercfg:        {".dockercfg"},
	SecretTypeDockerConfigJSON: {".dockerconfigjson"},
	SecretTypeBasicAuth:        {"username", "password"},
	SecretTypeSSHAuth:          {"ssh-privatekey"},
	SecretTypeTLS:              {"tls.crt", "tls.key"},
	SecretTypeBootstrapToken:   {"token-id", "token-secret"},
}

// SecretTypeKeys returns the data keys the dialog starts a Secret of type t with; none for Opaque
// and the service account token, whose data the token controller fills.
func SecretTypeKeys(t string) []string { return slices.Clone(secretTypeKeys[t]) }

// SecretTypeAnnotations returns the annotation keys the dialog starts a Secret of type t with: a
// service account token's account name.
func SecretTypeAnnotations(t string) []string {
	if t == SecretTypeServiceAccountToken {
		return []string{ServiceAccountNameAnnotation}
	}
	return nil
}

// ErrSecretCreateGUIOnly refuses an agent or the CLI the Create Secret dialog's command (D-19). It
// matches ErrSecretGUIOnly with errors.Is.
var ErrSecretCreateGUIOnly error = secretCreateRefusal{}

type secretCreateRefusal struct{}

func (secretCreateRefusal) Error() string {
	return "a Secret is created only by the user, in Cogpit's own window (Create Secret, D-19): agents and the CLI cannot create one"
}

func (secretCreateRefusal) Is(target error) bool { return target == ErrSecretGUIOnly }

// The parameters of secret.create that the audit log keeps, as they are (the others go by name).
var auditedSecretCreateFields = []string{metadataName, columnNamespace, "type"}

// auditSecretCreateParams returns secret.create's parameters as the audit log keeps them: the
// Secret's name, namespace and type; the data entries' keys and the annotations' keys, each value
// replaced by model.Redacted; the labels, which the Secrets list shows anyway; and only the names of
// any other fields (an agent's stringData, say), never their contents. A data or annotations
// parameter that is not a list of entries is replaced by a note of its type.
func auditSecretCreateParams(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil {
		return json.RawMessage(fmt.Sprintf(`"<%d bytes that are not secret.create's parameters; not logged>"`, len(raw)))
	}
	out := map[string]any{}
	var others []string
	for _, k := range slices.Sorted(maps.Keys(params)) {
		v := params[k]
		switch {
		case slices.Contains(auditedSecretCreateFields, k):
			if s, ok := v.(string); ok {
				out[k] = s
			} else {
				out[k] = fmt.Sprintf("<a %T; not logged>", v)
			}
		case k == secretDataField || k == "annotations":
			out[k] = auditEntryKeys(v)
		case k == "labels":
			out[k] = v
		default:
			others = append(others, k)
		}
	}
	if len(others) > 0 {
		out["otherParams"] = others // their names only: an unknown field may carry a value
	}
	return marshalParams(out, json.RawMessage(`"<not logged>"`))
}

// auditEntryKeys keeps the keys of a list of {key, value} entries and replaces every value.
func auditEntryKeys(v any) any {
	list, ok := v.([]any)
	if !ok {
		return fmt.Sprintf("<a %T, not a list of entries; not logged>", v)
	}
	out := make([]any, 0, len(list))
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			out = append(out, "<not an entry; not logged>")
			continue
		}
		e := map[string]any{"value": model.Redacted}
		if k, ok := m["key"].(string); ok {
			e["key"] = k
		}
		out = append(out, e)
	}
	return out
}
