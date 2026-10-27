package fyneui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Secrets view and drawer as the engine serves them since the full watch (D-13 revised,
// 2026-10-12): rows with their Keys and Type, a drawer whose Data lists the keys masked with Show
// beside each, and secret.reveal answering the GUI with one value. Every name and value is made up.

// fixtureSecretValues are the values of checkout-db, the Secret the drawer shows.
var fixtureSecretValues = map[string]string{
	"DB_HOST": "checkout-db.checkout.svc.cluster.local", "DB_PASSWORD": "correct-horse-battery-staple",
	"DB_USER": "checkout", "DATABASE_URL": "postgres://checkout:correct-horse-battery-staple@checkout-db:5432/orders?sslmode=verify-full&application_name=checkout-api",
}

// fixtureSecretBinary is checkout-db's ca.crt, its one binary value: a DER certificate's size,
// binary data, 1.2 KiB.
func fixtureSecretBinary() []byte {
	binary := make([]byte, 1229)
	for i := range binary {
		binary[i] = byte(i)
	}
	return binary
}

// fakeRevealed answers secret.reveal for key's value v as the engine does: with base64 the whole
// value encoded, the binary ca.crt's too (the b64 button, VW7); else v as it is.
func fakeRevealed(p engine.RevealParams, v string) engine.SecretValue {
	if !p.Base64 {
		return engine.SecretValue{Key: p.Key, Value: v, Size: len(v)}
	}
	raw := []byte(v)
	if p.Key == "ca.crt" {
		raw = fixtureSecretBinary()
	}
	return engine.SecretValue{Key: p.Key, Value: base64.StdEncoding.EncodeToString(raw), Size: len(raw), Base64: true}
}

// fixtureSecretObjects are Secrets as the cache holds them: in full.
func fixtureSecretObjects() []*unstructured.Unstructured {
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	dbData := map[string]any{"ca.crt": base64.StdEncoding.EncodeToString(fixtureSecretBinary())}
	for k, v := range fixtureSecretValues {
		dbData[k] = enc(v)
	}
	var out []*unstructured.Unstructured
	for i, s := range []struct {
		ns, name, typ string
		labels        map[string]any
		data          map[string]any
	}{
		{"checkout", "checkout-db", "Opaque", map[string]any{"app.kubernetes.io/name": "checkout-api", "app.kubernetes.io/component": "database"}, dbData},
		{"checkout", "checkout-api-token", "Opaque", map[string]any{"app.kubernetes.io/name": "checkout-api"}, map[string]any{"token": enc("not-a-real-token")}},
		{"payments", "stripe-webhook", "Opaque", map[string]any{"team": "payments"}, map[string]any{"signing-secret": enc("whsec_not-real"), "endpoint": enc("https://example.invalid/hook")}},
		{"storefront", "storefront-tls", "kubernetes.io/tls", map[string]any{"cert-manager.io/certificate-name": "storefront"}, map[string]any{"tls.crt": enc("not-a-cert"), "tls.key": enc("not-a-key"), "ca.crt": enc("not-a-ca")}},
		{"storefront", "sh.helm.release.v1.storefront.v7", "helm.sh/release.v1", map[string]any{"owner": "helm", "name": "storefront", "status": "deployed", "version": "7"}, map[string]any{"release": enc("H4sIAAAA-not-a-release")}},
		{"kube-system", "bootstrap-token-k8m2xq", "bootstrap.kubernetes.io/token", nil, map[string]any{"token-id": enc("k8m2xq"), "token-secret": enc("not-a-secret"), "usage-bootstrap-authentication": enc("true")}},
		{"monitoring", "grafana-admin", "Opaque", map[string]any{"app.kubernetes.io/name": "grafana"}, map[string]any{"admin-user": enc("admin"), "admin-password": enc("not-a-password")}},
	} {
		meta := map[string]any{"name": s.name, "namespace": s.ns, "uid": "uid-sec-" + s.name, "resourceVersion": "4821" + string(rune('0'+i)),
			"creationTimestamp": ts(time.Duration(3+i*5) * 24 * time.Hour)}
		if s.labels != nil {
			meta["labels"] = s.labels
		}
		out = append(out, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "Secret", "metadata": meta, "type": s.typ, "data": s.data,
		}})
	}
	return out
}

// secretsFake is a fake connected to a cluster with Secrets, the drawer open on checkout-db, and
// secret.reveal answering with the value of a key.
func secretsFake(t testing.TB) (*fakeEngine, model.ObjectRef) {
	f := kindsFake(t, "secrets")
	f.kinds = append(f.kinds, kindInfo{Kind: "secrets", Title: "Secrets", APIKind: "Secret", Version: "v1", Namespaced: true})
	objs := fixtureSecretObjects()
	f.tables["secrets"] = kindSnapshot(t, "secrets", objs)
	p, _ := kinds.ByKey("secrets")
	ref := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "checkout", Name: "checkout-db"}
	m, err := engine.BuildDetail(p, ref, objs[0], nil, fixedNow) // the Type and the Data section, values masked
	if err != nil {
		t.Fatal(err)
	}
	f.details[ref.String()] = m
	f.handlers[engine.RevealCommand] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p engine.RevealParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		if inv.Origin != engine.OriginGUI {
			t.Errorf("secret.reveal dispatched by %s", inv.Origin)
		}
		return fakeRevealed(p, fixtureSecretValues[p.Key]), nil
	}
	f.ui.View = "secrets"
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	return f, ref
}

// dataButtons returns the drawer's buttons in its Data section, by label.
func drawerButtons(d *drawer) map[string][]*widget.Button {
	out := map[string][]*widget.Button{}
	for _, b := range findAll[*widget.Button](d.details) {
		out[b.Text] = append(out[b.Text], b)
	}
	return out
}

// drawerHasText reports whether a label or text line of the drawer's details reads s.
func drawerHasText(d *drawer, s string) bool {
	for _, l := range findAll[*widget.Label](d.details) {
		if strings.Contains(l.Text, s) {
			return true
		}
	}
	for _, l := range findAll[*textLine](d.details) {
		if strings.Contains(l.text, s) {
			return true
		}
	}
	for _, l := range findAll[*sectionHead](d.details) { // a section's head (stage_header.go)
		if strings.Contains(l.title, s) {
			return true
		}
	}
	for _, l := range findAll[*secretText](d.details) { // a revealed value (D-25)
		if strings.Contains(l.text, s) {
			return true
		}
	}
	return false
}

// Show reveals one value in place, from the GUI; Hide masks it again; moving the drawer forgets
// it. The value never reaches a toast.
func TestSecretRevealInDrawer(t *testing.T) {
	f, ref := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerButtons(sh.drawer)["Show"]) == 4 })
	if drawerHasText(sh.drawer, "correct-horse") {
		t.Fatal("a value shows before Show")
	}
	test.Tap(drawerButtons(sh.drawer)["Show"][2]) // DB_PASSWORD
	q.until(t, "the value", func() bool { return drawerHasText(sh.drawer, "correct-horse-battery-staple") })
	if calls := f.callsOf(engine.RevealCommand); len(calls) != 1 || calls[0].Origin != engine.OriginGUI ||
		!strings.Contains(string(calls[0].Params), `"key":"DB_PASSWORD"`) {
		t.Errorf("reveal calls %+v", calls)
	}
	if b := drawerButtons(sh.drawer); len(b["Hide"]) != 1 || len(b["Show"]) != 3 {
		t.Errorf("buttons after Show: %v", b)
	}
	for _, o := range sh.toasts.Objects {
		for _, l := range findAll[*textLine](o) {
			if strings.Contains(l.text, "correct-horse") {
				t.Error("the value reached a toast")
			}
		}
	}

	test.Tap(drawerButtons(sh.drawer)["Hide"][0])
	q.until(t, "the value to hide", func() bool { return !drawerHasText(sh.drawer, "correct-horse") })

	test.Tap(drawerButtons(sh.drawer)["Show"][2])
	q.until(t, "the value again", func() bool { return drawerHasText(sh.drawer, "correct-horse") })
	other := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "payments", Name: "stripe-webhook"}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{other}, &other })
	q.until(t, "the other Secret", func() bool { return sh.drawer.shown != nil && *sh.drawer.shown == other })
	if len(sh.drawer.revealed) != 0 {
		t.Error("a revealed value survived the move to another object")
	}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{ref}, &ref })
	q.until(t, "the first Secret", func() bool { return sh.drawer.last != nil && sh.drawer.last.Ref == ref })
	if drawerHasText(sh.drawer, "correct-horse") {
		t.Error("the value shows again without Show")
	}
}

// A revealed value belongs to the Secret's version it was revealed at: when the Secret changes (a
// rotation) or is created again under its name, the drawer shows the key masked and forgets the
// old value, so nobody copies a stale credential (review of the fourth run, R-6).
func TestRevealedValueDroppedWhenSecretChanges(t *testing.T) {
	f, ref := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerButtons(sh.drawer)["Show"]) == 4 })
	test.Tap(drawerButtons(sh.drawer)["Show"][2]) // DB_PASSWORD
	q.until(t, "the value", func() bool { return drawerHasText(sh.drawer, "correct-horse-battery-staple") })
	if calls := f.callsOf(engine.RevealCommand); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"resourceVersion":`) {
		t.Fatalf("the reveal does not name the Secret's version: %+v", calls)
	}

	rotated := fixtureSecretObjects()[0].DeepCopy()
	rotated.SetResourceVersion(rotated.GetResourceVersion() + "1")
	p, _ := kinds.ByKey("secrets")
	m, err := engine.BuildDetail(p, ref, rotated, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.details[ref.String()] = m
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventDetail})
	q.until(t, "the value masked again", func() bool {
		return !drawerHasText(sh.drawer, "correct-horse") && len(drawerButtons(sh.drawer)["Show"]) == 4
	})
	if len(sh.drawer.revealed) != 0 {
		t.Error("the old value stays in the drawer's memory")
	}
}

// renderSecrets renders the Secrets list with the drawer open, one value revealed.
func renderSecrets(t *testing.T, mode ThemeMode, name string) {
	f, _ := secretsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.drawer.last != nil && len(drawerButtons(sh.drawer)["Show"]) == 4 && sh.diag.total > 0
	})
	test.Tap(drawerButtons(sh.drawer)["Show"][0]) // DATABASE_URL: long, wraps
	q.until(t, "the value", func() bool { return drawerHasText(sh.drawer, "sslmode") })
	settle(w)
	savePNG(t, w, name)
}

func TestRenderSecretsDark(t *testing.T) { renderSecrets(t, ThemeDark, "secrets_dark.png") }

// TestRenderSecretsListDark renders the Secrets list without the drawer: the Keys and Type columns
// the full watch fills (D-13 revised, 2026-10-12).
func TestRenderSecretsListDark(t *testing.T) {
	f, _ := secretsFake(t)
	f.ui.Selected, f.ui.Drawer = nil, nil
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the list", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	savePNG(t, w, "secrets_list_dark.png")
}

var _ fyne.CanvasObject = (*widget.Label)(nil)
