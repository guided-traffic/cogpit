package kinds_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzSecrets(f *testing.F) { fuzzProfile(f, "secrets") }

// Secrets come from a full watch (D-13 revised): Keys lists the names of the keys, sorted, and Type
// the type, as in Lens; no status token (inferred).
func TestSecretTexts(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"secrets", "healthy", "labels", "app.kubernetes.io/component=database, app.kubernetes.io/name=checkout-api", model.StatusNone, model.StatusNone},
		{"secrets", "healthy", "keys", "DB_HOST, DB_PASSWORD, DB_USER", model.StatusNone, model.StatusNone},
		{"secrets", "healthy", "type", "Opaque", model.StatusNone, model.StatusNone},
		{"secrets", "failing", "labels", "app=edge-proxy", model.StatusNone, model.StatusNone},
		{"secrets", "failing", "keys", "", model.StatusNone, model.StatusNone},
		{"secrets", "failing", "type", "", model.StatusNone, model.StatusNone},
		{"secrets", "terminating", "labels", "rotated=true", model.StatusNone, model.StatusTerminating},
		{"secrets", "terminating", "keys", "ca.crt, namespace, token", model.StatusNone, model.StatusTerminating},
		{"secrets", "minimal", "labels", "", model.StatusNone, model.StatusNone},
		{"secrets", "minimal", "keys", "", model.StatusNone, model.StatusNone},
		{"secrets", "full-object", "keys", "ca.crt, tls.crt, tls.key", model.StatusNone, model.StatusNone},
		{"secrets", "full-object", "type", "kubernetes.io/tls", model.StatusNone, model.StatusNone},
		{"secrets", "helm-release", "keys", "release", model.StatusNone, model.StatusNone},
		{"secrets", "helm-release", "type", "helm.sh/release.v1", model.StatusNone, model.StatusNone},
	})
	assertColumnOrder(t, "secrets", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "namespace", Title: "Namespace"},
		{ID: "labels", Title: "Labels"},
		{ID: "keys", Title: "Keys"},
		{ID: "type", Title: "Type"},
		{ID: "age", Title: "Age"},
	})
	p, _ := kinds.ByKey("secrets")
	if p.MetadataOnly || p.LabelSelector != "" || !p.APIObject() {
		t.Errorf("the Secrets view must watch every Secret in full: %+v", p)
	}
}

// The profile never shows a value, neither in its cells nor in its drawer, which has the Type only.
func TestSecretDetailShowsNoValue(t *testing.T) {
	p, _ := kinds.ByKey("secrets")
	for _, c := range loadCases(t, "secrets") {
		var values []string
		for _, field := range []string{"data", "stringData"} {
			for _, v := range model.NestedMap(c.obj.Object, field) {
				if s, ok := v.(string); ok && s != "" {
					values = append(values, s)
					if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) > 0 {
						values = append(values, string(raw))
					}
				}
			}
		}
		shows := func(text string) bool {
			for _, v := range values {
				if strings.Contains(text, v) {
					return true
				}
			}
			return false
		}
		for _, sec := range p.Detail(c.obj) {
			for _, it := range sec.Items {
				if it.Name != "Type" || shows(it.Value) {
					t.Errorf("%s: drawer item %+v", c.name, it)
				}
			}
		}
		for _, cell := range model.BuildRow(p, "test", c.obj).Cells {
			if shows(cell.Text) {
				t.Errorf("%s: a cell shows a value: %q", c.name, cell.Text)
			}
		}
	}
}
