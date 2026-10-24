package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// secretsSession connects a session whose Secrets are the real profile, watched in full.
func secretsSession(t *testing.T, src *testkit.MemorySource) *Session {
	t.Helper()
	p, _ := kinds.ByKey(secretsKind)
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = append([]*model.KindProfile{p}, testPods, testEvents, testNamespaces)
	connectTest(t, s)
	return s
}

// waitData waits until the drawer model of ref has a Data section that satisfies cond.
func waitData(t *testing.T, s *Session, ref model.ObjectRef, cond func(*DetailModel) bool) *DetailModel {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var m *DetailModel
	if !testkit.WaitFor(ctx, func() bool {
		var err error
		m, err = s.Detail(ref)
		return err == nil && !m.Loading && !m.Missing && dataItems(m) != nil && cond(m)
	}) {
		t.Fatalf("no data for %s: %+v", ref, m)
	}
	return m
}

func dataItems(m *DetailModel) []model.DetailItem {
	for _, sec := range m.Sections {
		if sec.Title == "Data" {
			return sec.Items
		}
	}
	return nil
}

// The Secrets view and drawer read the full Secrets from the cache: Keys and Type in the list, a
// drawer whose Data section lists the keys with their values masked (for the GUI and agents
// alike), a binary value by its size only, the Type once, and no request beyond the view's one
// list and watch; secret.reveal's value comes from the cache and only for the Secret the drawer
// shows, and follows the Secret when it changes.
func TestSecretDrawerReadsTheCacheAndMasks(t *testing.T) {
	src := testkit.NewMemorySource()
	db := testkit.Secret("apps", "db", map[string]string{"app": "db"}, map[string]string{"password": "hunter2", "url": "postgres://db:5432"})
	_ = unstructured.SetNestedField(db.Object, base64Of("\xff\xfe\x00\x01"), "data", "blob")
	db.SetAnnotations(map[string]string{cache.LastAppliedAnnotation: `{"stringData":{"password":"hunter2"}}`, "team": "db"})
	src.Set(testkit.SecretsGVR, db)
	src.Set(testkit.SecretsGVR, testkit.Secret("apps", "tls", nil, map[string]string{"tls.key": "-----BEGIN KEY-----"}))
	s := secretsSession(t, src)

	s.UpdateUI(func(u *UIState) { u.View = secretsKind })
	snap := waitTable(t, s, secretsKind, func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 2 })
	if got := snap.Rows[0].Cells[3].Text + " | " + snap.Rows[0].Cells[4].Text; got != "blob, password, url | Opaque" {
		t.Errorf("Keys | Type of db: %q", got)
	}
	values := []string{"hunter2", base64Of("hunter2"), "db:5432", "BEGIN KEY"} // not the port alone: four digits turn up in a timestamp
	carries := func(v any) string {
		b, _ := json.Marshal(v)
		for _, s := range values {
			if strings.Contains(string(b), s) {
				return s
			}
		}
		return ""
	}
	if v := carries(snap); v != "" {
		t.Fatalf("a value reached the table: %s", v)
	}

	db0 := model.ObjectRef{Context: "test", Kind: secretsKind, Namespace: "apps", Name: "db"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &db0 })
	m := waitData(t, s, db0, func(*DetailModel) bool { return true })
	items := dataItems(m)
	if len(items) != 3 || items[0].Name != "blob" || items[1].Name != "password" || items[2].Name != "url" {
		t.Fatalf("data items %+v", items)
	}
	if items[0].Value != "binary data, 4 B" || len(items[0].Actions) != 1 || items[0].Actions[0].Command != RevealCommand ||
		items[0].Actions[0].Label != Base64ActionLabel {
		t.Errorf("binary item %+v", items[0])
	}
	var bp RevealParams
	if err := json.Unmarshal(items[0].Actions[0].Params, &bp); err != nil || bp.Key != "blob" || !bp.Base64 || bp.ResourceVersion == "" {
		t.Errorf("a binary value's base64 params %s", items[0].Actions[0].Params)
	}
	if items[1].Value != MaskedValue+" (7 B)" || len(items[1].Actions) != 1 || items[1].Actions[0].Command != RevealCommand {
		t.Errorf("masked item %+v", items[1])
	}
	var rp RevealParams
	if err := json.Unmarshal(items[1].Actions[0].Params, &rp); err != nil || rp.Key != "password" || !rp.Ref.Same(db0) {
		t.Errorf("reveal params %s", items[1].Actions[0].Params)
	}
	types := 0
	for _, it := range m.Sections[0].Items {
		if it.Name == "Type" {
			types++
		}
	}
	if got := propertyValue(m, "Type"); got != "Opaque" || types != 1 {
		t.Errorf("Type %q, %d times", got, types)
	}
	for _, origin := range []Origin{OriginGUI, OriginExternalAgent, OriginBuiltInAgent} {
		mm, err := s.DetailWait(context.WithValue(context.Background(), originKey{}, origin), db0)
		if err != nil {
			t.Fatal(err)
		}
		if v := carries(mm); v != "" {
			t.Errorf("%s: a value reached the drawer model: %s", origin, v)
		}
		if !strings.Contains(mm.YAML, "password: "+model.Redacted) || !strings.Contains(mm.YAML, "type: Opaque") {
			t.Errorf("%s: YAML %s", origin, mm.YAML)
		}
	}

	v, err := s.SecretValue(RevealParams{Ref: db0, Key: "password"})
	if err != nil || v.Value != "hunter2" || v.Size != 7 || v.Cut {
		t.Errorf("SecretValue %+v, %v", v, err)
	}
	if _, err := s.SecretValue(RevealParams{Ref: db0, Key: "blob"}); err == nil {
		t.Error("a binary value was revealed")
	}
	if _, err := s.SecretValue(RevealParams{Ref: db0, Key: "blob", Whole: true}); err == nil {
		t.Error("a binary value was revealed whole")
	}
	// Base64 (the drawer's b64 button, VW7): the whole value encoded, a binary value's too.
	if v, err := s.SecretValue(bp); err != nil || v.Value != base64Of("\xff\xfe\x00\x01") || !v.Base64 || v.Size != 4 || v.Cut {
		t.Errorf("a binary value as base64: %+v, %v", v, err)
	}
	if err := s.SecretKeyShown(bp); err != nil {
		t.Errorf("a binary value's base64 read is not enabled: %v", err)
	}
	if v, err := s.SecretValue(RevealParams{Ref: db0, Key: "password", Base64: true}); err != nil || v.Value != base64Of("hunter2") || !v.Base64 || v.Size != 7 {
		t.Errorf("a text value as base64: %+v, %v", v, err)
	}
	if err := s.SecretKeyShown(RevealParams{Ref: db0, Key: "nope"}); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("a missing key: %v", err)
	}

	// The drawer follows the Secret in the cache: a new value shows its new size, and a reveal
	// reads the new value.
	db = testkit.Secret("apps", "db", map[string]string{"app": "db"}, map[string]string{"password": "correct-horse", "url": "postgres://db:5432"})
	src.Set(testkit.SecretsGVR, db)
	waitData(t, s, db0, func(m *DetailModel) bool {
		its := dataItems(m)
		return len(its) == 2 && its[0].Value == MaskedValue+" (13 B)"
	})
	if v, err := s.SecretValue(RevealParams{Ref: db0, Key: "password"}); err != nil || v.Value != "correct-horse" {
		t.Errorf("after the change: %+v, %v", v, err)
	}
	// A reveal names the version the drawer listed the key at (its action's params): the current
	// one reveals, an older one is refused without a value (R-6).
	listed, err := s.DetailWait(context.WithValue(context.Background(), originKey{}, OriginGUI), db0)
	if err != nil {
		t.Fatal(err)
	}
	var shown RevealParams
	if its := dataItems(listed); len(its) == 0 || len(its[0].Actions) != 1 || json.Unmarshal(its[0].Actions[0].Params, &shown) != nil {
		t.Fatalf("the Data section's actions: %+v", its)
	}
	if v, err := s.SecretValue(shown); err != nil || v.Value != "correct-horse" {
		t.Errorf("reveal at the drawer's version %q: %+v, %v", shown.ResourceVersion, v, err)
	}
	shown.ResourceVersion += "-old"
	if _, err := s.SecretValue(shown); !errors.Is(err, ErrTargetsChanged) || strings.Contains(err.Error(), "correct-horse") {
		t.Errorf("reveal at another version: %v", err)
	}

	// Moving the drawer: no reveal for the first Secret; closing it: none at all.
	tls := model.ObjectRef{Context: "test", Kind: secretsKind, Namespace: "apps", Name: "tls"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &tls })
	waitData(t, s, tls, func(*DetailModel) bool { return true })
	if _, err := s.SecretValue(RevealParams{Ref: db0, Key: "password"}); !errors.Is(err, ErrNoSecretData) {
		t.Errorf("a Secret the drawer moved away from: %v", err)
	}
	s.UpdateUI(func(u *UIState) { u.Drawer = nil })
	if _, err := s.SecretValue(RevealParams{Ref: tls, Key: "tls.key"}); !errors.Is(err, ErrNoSecretData) {
		t.Errorf("after closing the drawer: %v", err)
	}
	// A Secret the drawer does not show: object_get lists its keys masked, without a request.
	mm, err := s.DetailWait(context.WithValue(context.Background(), originKey{}, OriginExternalAgent), db0)
	if err != nil || len(dataItems(mm)) != 2 || carries(mm) != "" {
		t.Errorf("object_get of a Secret the drawer does not show: %+v, %v", dataItems(mm), err)
	}
	for _, st := range s.Backend().Cache.Stats() {
		if st.Scope.GVR == testkit.SecretsGVR && (st.Scope.MetadataOnly || st.Scope.LabelSelector != "") {
			t.Errorf("another Secrets scope: %s", st.Scope)
		}
	}
}

// The cache drops a Secret's last-applied annotation, which repeats its values; a Secret that
// arrives with it anyway shows it redacted in the drawer's Annotations and YAML, never its value.
func TestSecretDrawerRedactsTheLastAppliedAnnotation(t *testing.T) {
	p, _ := kinds.ByKey(secretsKind)
	obj := testkit.Secret("apps", "db", nil, map[string]string{"password": "hunter2"})
	obj.SetAnnotations(map[string]string{cache.LastAppliedAnnotation: `{"stringData":{"password":"hunter2"}}`, "team": "db"})
	m, err := BuildDetail(p, model.ObjectRef{Kind: secretsKind, Namespace: "apps", Name: "db"}, obj, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "hunter2") || strings.Contains(string(b), base64Of("hunter2")) {
		t.Fatalf("a value reached the drawer: %s", b)
	}
	if !strings.Contains(string(b), "team") {
		t.Error("the other annotations went too")
	}
	if obj.GetAnnotations()[cache.LastAppliedAnnotation] == "" {
		t.Error("BuildDetail changed the cached object")
	}
}

// A long value is revealed cut to the caps, a multi-byte text at a character boundary; a value
// with control characters counts as binary.
func TestSecretValueCaps(t *testing.T) {
	long := strings.Repeat("ä", 3000) // 6000 bytes
	if size, text := measure(base64Of(long)); size != 6000 || !text {
		t.Errorf("long value: %d bytes, text %v", size, text)
	}
	if shown := clip([]byte(long)); len(shown) > secretValueBytes || !strings.HasPrefix(long, string(shown)) {
		t.Errorf("kept %d bytes", len(shown))
	}
	if shown := clip([]byte(strings.Repeat("line\n", 100))); strings.Count(string(shown), "\n") >= secretValueLines {
		t.Errorf("kept %d lines", strings.Count(string(shown), "\n"))
	}
	if _, text := measure(base64Of("ring\a")); text {
		t.Error("control characters count as text")
	}
	if size, text := measure("not base64!"); text || size == 0 {
		t.Errorf("a value that is not base64: %d bytes, text %v", size, text)
	}
	if _, text := measure(42); text {
		t.Error("a value that is not a string counts as text")
	}
}

// TestSecretValueBase64IsWhole (VW7): a base64 read hands the whole value encoded, never the first
// 4 KiB a Show reveals, whether or not whole is asked for: a value cut short on the clipboard would
// be wrong; a binary value over the caps too.
func TestSecretValueBase64IsWhole(t *testing.T) {
	src := testkit.NewMemorySource()
	long := strings.Repeat("correct-horse-battery-staple\n", 300) // 8.7 KiB, 300 lines
	blob := strings.Repeat("\x00\xff", 5000)
	sec := testkit.Secret("apps", "big", nil, map[string]string{"pem": long})
	_ = unstructured.SetNestedField(sec.Object, base64Of(blob), "data", "blob")
	src.Set(testkit.SecretsGVR, sec)
	s := secretsSession(t, src)
	s.UpdateUI(func(u *UIState) { u.View = secretsKind })
	waitTable(t, s, secretsKind, func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 1 })
	ref := model.ObjectRef{Context: "test", Kind: secretsKind, Namespace: "apps", Name: "big"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &ref })
	waitData(t, s, ref, func(*DetailModel) bool { return true })

	if v, err := s.SecretValue(RevealParams{Ref: ref, Key: "pem"}); err != nil || !v.Cut {
		t.Fatalf("Show did not cut the long value: %+v, %v", v.Cut, err)
	}
	for _, key := range []string{"pem", "blob"} {
		want := map[string]string{"pem": long, "blob": blob}[key]
		v, err := s.SecretValue(RevealParams{Ref: ref, Key: key, Base64: true})
		if err != nil || v.Cut || !v.Base64 || v.Size != len(want) {
			t.Fatalf("%s as base64: cut %v, base64 %v, size %d, %v", key, v.Cut, v.Base64, v.Size, err)
		}
		if got, err := base64.StdEncoding.DecodeString(v.Value); err != nil || string(got) != want {
			t.Errorf("%s as base64 does not decode to the whole value (%d bytes, %v)", key, len(got), err)
		}
	}
}

func base64Of(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// propertyValue returns the value of a Properties item.
func propertyValue(m *DetailModel, name string) string {
	if len(m.Sections) == 0 {
		return ""
	}
	for _, it := range m.Sections[0].Items {
		if it.Name == name {
			return it.Value
		}
	}
	return ""
}
