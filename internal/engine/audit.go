package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// AuditEntry is one line of the audit log: an agent-initiated command and its outcome.
type AuditEntry struct {
	Time    time.Time       `json:"time"`
	Origin  Origin          `json:"origin"`
	Command string          `json:"command"`
	Params  json.RawMessage `json:"params,omitempty"`
	// Context is the context the command was dispatched for, or the one the user confirmed.
	Context string `json:"context,omitempty"`
	// Phase is "requested" for the line a write gets before it runs, "done" for the outcome,
	// "ended" for the end of a background job the command started (jobs.go): ok when it is done,
	// else its outcome as the error.
	Phase string `json:"phase"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// ReadsLikeUser marks a call of the built-in agent over a connection the user marked private:
	// its reads returned what the window shows, Secret values included (D-10, H-94). The line
	// itself keeps values out as every line does.
	ReadsLikeUser bool `json:"readsLikeUser,omitempty"`
}

// Audit phases.
const (
	auditRequested = "requested"
	auditDone      = "done"
	auditEnded     = "ended" // a background job's end (jobs.go)
)

// audit writes one JSON line for an agent-initiated command. Results are not logged: they can be
// large and may carry cluster data; the parameters and the outcome are what the user reviews.
func (s *Session) audit(inv Invocation, context, phase string, err error) {
	if s.opts.Audit == nil {
		return
	}
	e := AuditEntry{
		Time: s.opts.Now().UTC(), Origin: inv.Origin, Command: inv.ID,
		Params: normalizeParams(inv.Params), Context: context, Phase: phase, OK: err == nil,
		ReadsLikeUser: inv.privateTurn(),
	}
	if !json.Valid(e.Params) {
		e.Params = nil
	}
	if inv.ID == SecretCreateCommand {
		e.Params = auditSecretCreateParams(e.Params) // its keys, never a value (secretcreate.go)
	} else if secretDataAudited(inv.ID) {
		e.Params = auditSecretDataParams(e.Params) // the Secret and its keys, never a value (secretdataedit.go)
	} else {
		e.Params = auditParams(e.Params)
	}
	if err != nil {
		e.Error = err.Error()
		var pe *ParamsError
		if errors.As(err, &pe) {
			// The schema validator quotes the values it rejects, Secret data included.
			e.Error = "command " + pe.Command + ": invalid params (the validator's message is not logged: it quotes the values)"
		}
		var uo *UserOnlyError
		if errors.As(err, &uo) {
			// A private turn read the error as the window does; it can quote what a chart's
			// lookup read (readslikeuser.go).
			e.Error = uo.Withheld
		}
	}
	line, mErr := json.Marshal(e)
	if mErr != nil {
		return
	}
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	if _, wErr := s.opts.Audit.Write(append(line, '\n')); wErr != nil {
		s.log.Warn("audit log write failed", "err", wErr)
	}
}

// AuditUser writes one line for something the user did in the window that is no command, such as
// switching the MCP server off: origin gui, the action's name as the command, its parameters and
// whether it worked. Nothing an agent does reaches it; the shell and the app's main call it.
func (s *Session) AuditUser(action string, params any, err error) {
	raw, mErr := json.Marshal(params)
	if mErr != nil {
		raw = nil
	}
	s.audit(Invocation{ID: action, Params: raw, Origin: OriginGUI}, "", auditDone, err)
}

// auditParams keeps Secret values out of the audit log. Commands that take manifests (object.apply,
// object.create, editor.setText, resource.createTab) carry them in a "yaml" parameter; an agent may
// send a Secret's manifest even though Cogpit refuses to write Secrets. The parameter is logged as
// RedactManifests returns it; a "yaml" parameter that is not a string is logged by its type only.
// Helm values, which can hold credentials, are never logged: helm.release.upgrade's "values", and
// the "yaml" an agent sets in a Helm Upgrade or Install tab (editor.setText with a helmUpgrade-… or
// helmInstall-… id). A proxy URL ("proxy", "httpProxy") is logged with its password as •••.
func auditParams(raw json.RawMessage) json.RawMessage {
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil {
		return raw
	}
	if v, ok := params["values"]; ok {
		params["values"] = withheldParam(v)
		raw = marshalParams(params, raw)
	}
	if redactProxyParams(params) { // a proxy's password (proxy.go)
		raw = marshalParams(params, raw)
	}
	v, ok := params["yaml"]
	if !ok {
		return raw
	}
	if id, _ := params["id"].(string); strings.HasPrefix(id, string(DockHelmUpgrade)+"-") || strings.HasPrefix(id, string(DockHelmInstall)+"-") {
		params["yaml"] = withheldParam(v)
		return marshalParams(params, raw)
	}
	text, isString := v.(string)
	if !isString {
		params["yaml"] = fmt.Sprintf("<a %T, not a string; not logged>", v)
		return marshalParams(params, raw)
	}
	out, changed := RedactManifests(text, true)
	if !changed {
		return raw
	}
	params["yaml"] = out
	return marshalParams(params, raw)
}

// RedactManifests returns manifest text with every Secret's values replaced by model.Redacted:
// Secret documents, and the items of any List (SecretList, v1 List, another group's List) that
// are Secrets or belong to a SecretList; and with the payload of every Helm release record kept in
// a ConfigMap (labelled owner=helm, HELM_DRIVER=configmap) replaced the same way, since it holds
// the release's values. Text that does not parse as manifests is replaced by a
// note with its size when strict is set (the audit log), or when it mentions a Secret kind;
// otherwise (an agent reading a half-typed editor tab) it is returned as it is. It reports
// whether it changed anything.
func RedactManifests(text string, strict bool) (string, bool) {
	if strings.TrimSpace(text) == "" {
		return text, false
	}
	objs, err := ParseManifests(text)
	if err != nil {
		if strict || mentionsSecret.MatchString(text) {
			return fmt.Sprintf("<%d bytes that are not manifests; not shown>", len(text)), true
		}
		return text, false
	}
	changed := false
	docs := make([]string, 0, len(objs))
	for _, o := range objs {
		kind := o.GetKind()
		switch {
		case kind == kindSecret:
			o, changed = redactSecret(o), true
		case helmRecordManifest(o.Object, ""): // a Helm release record in a ConfigMap (helmdriver.go)
			o, changed = redactHelmRecord(o), true
		case strings.HasSuffix(kind, "List"):
			if redactListItems(o, kind) {
				changed = true
			}
		}
		y, err := model.ObjectYAML(o.Object) // the Kubernetes order (D-86)
		if err != nil {
			return fmt.Sprintf("<%d bytes of manifests; not shown>", len(text)), true
		}
		docs = append(docs, string(y))
	}
	if !changed {
		return text, false
	}
	return strings.Join(docs, "---\n"), true
}

// mentionsSecret finds a kind: Secret (or SecretList) line in text that does not parse.
var mentionsSecret = regexp.MustCompile(`(?m)^\s*"?kind"?\s*:\s*["']?Secret(List)?["']?\s*,?\s*$`)

// redactListItems redacts the Secret items of a list object in place (o is a fresh copy from
// ParseManifests): all of them for a SecretList, whose items carry no kind; and the Helm release
// records kept in ConfigMaps among its items.
func redactListItems(o *unstructured.Unstructured, listKind string) bool {
	items, ok := o.Object["items"].([]any)
	if !ok {
		return false
	}
	changed := false
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		switch k, _ := m["kind"].(string); {
		case listKind == "SecretList" || k == kindSecret:
			items[i] = redactSecret(&unstructured.Unstructured{Object: m}).Object
			changed = true
		case helmRecordManifest(m, listKind):
			items[i] = redactHelmRecord(&unstructured.Unstructured{Object: m}).Object
			changed = true
		}
	}
	return changed
}

// helmRecordManifest reports whether a manifest (or an item of a list of listKind) is a Helm
// release record kept in a ConfigMap: kind ConfigMap (an item of a ConfigMapList carries none),
// labelled owner=helm, whatever its apiVersion says. Its data.release holds the release's values.
func helmRecordManifest(m map[string]any, listKind string) bool {
	k, _ := m["kind"].(string)
	if k != "ConfigMap" && (k != "" || listKind != "ConfigMapList") {
		return false
	}
	md, _ := m["metadata"].(map[string]any)
	labels, _ := md["labels"].(map[string]any)
	owner, _ := labels["owner"].(string)
	return owner == "helm"
}

// redactHelmRecord replaces a Helm release record's payload (data.release, or the whole data
// field if it is not a mapping) and the last-applied annotation, which repeats it.
func redactHelmRecord(o *unstructured.Unstructured) *unstructured.Unstructured {
	cp := o.DeepCopy()
	switch d := cp.Object["data"].(type) {
	case map[string]any:
		if _, ok := d[model.HelmRecordKey]; ok {
			d[model.HelmRecordKey] = model.Redacted
		}
	case nil:
	default:
		cp.Object["data"] = model.Redacted
	}
	if md, ok := cp.Object["metadata"].(map[string]any); ok {
		if ann, ok := md["annotations"].(map[string]any); ok {
			if _, found := ann["kubectl.kubernetes.io/last-applied-configuration"]; found {
				ann["kubectl.kubernetes.io/last-applied-configuration"] = model.Redacted
			}
		}
	}
	return cp
}

// redactSecret replaces a Secret manifest's values, whatever its apiVersion says and whatever
// shape its data has: data and stringData entries (or the whole field, if it is not a mapping)
// and the last-applied annotation, which repeats them.
func redactSecret(o *unstructured.Unstructured) *unstructured.Unstructured {
	cp := o.DeepCopy()
	for _, field := range secretValueFields {
		v, ok := cp.Object[field]
		if !ok {
			continue
		}
		m, isMap := v.(map[string]any)
		if !isMap {
			cp.Object[field] = model.Redacted
			continue
		}
		for k := range m {
			m[k] = model.Redacted
		}
	}
	if md, ok := cp.Object["metadata"].(map[string]any); ok {
		if ann, ok := md["annotations"].(map[string]any); ok {
			if _, found := ann["kubectl.kubernetes.io/last-applied-configuration"]; found {
				ann["kubectl.kubernetes.io/last-applied-configuration"] = model.Redacted
			}
		}
	}
	return cp
}

// withheldParam is what the audit log records of a parameter that holds Helm values: its size.
func withheldParam(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("<%d bytes of Helm values; not logged>", len(s))
	}
	return "<Helm values; not logged>"
}

func marshalParams(params map[string]any, fallback json.RawMessage) json.RawMessage {
	out, err := json.Marshal(params)
	if err != nil {
		return fallback
	}
	return out
}
