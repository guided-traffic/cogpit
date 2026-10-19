package commands_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Keys added and removed in a Config Map's and a Secret's drawer (the owner's audit decision of
// 2026-10-16, N152 (b)): the saves take add and remove beside data, a key's name follows the API
// server's rule, a removal is destructive and confirmed for every origin with the keys named, the
// review's rebase counts an added key that appeared and a removed key that changed as a conflict,
// kept edits and their draft hold the keys too, and no value of a Secret reaches a confirmation,
// a result, the audit log or the log.

// TestConfigMapDataAddsAndRemovesKeys: the window reviews an edit that changes a value, adds a key
// and removes one: the diff, the keys of each kind, the Config Map's UID and the dry run. Its save
// removes a key, so it is destructive: the confirmation names the Config Map, its namespace, its
// context, its UID and the keys, and the update holds the new key and not the removed one. A save
// that only adds a key is mutating and the window's own runs unasked.
func TestConfigMapDataAddsAndRemovesKeys(t *testing.T) {
	h, srv := configMapHarness(t, engine.Options{})
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	gui := engine.OriginGUI
	var data engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	params := map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion, "data": map[string]any{"enable-ipv4": "false"},
		"add": map[string]any{"enable-ipv6": "true"}, "remove": []string{"bpf-lb-acceleration"}}

	var review engine.ConfigMapDataReview
	h.must(t, gui, "configMap.reviewData", params, &review)
	if !review.Accepted || review.Stale || !slices.Equal(review.Keys, []string{"enable-ipv4"}) ||
		!slices.Equal(review.KeysAdded, []string{"enable-ipv6"}) || !slices.Equal(review.KeysRemoved, []string{"bpf-lb-acceleration"}) ||
		review.UID != "uid-shop-cilium-config" {
		t.Fatalf("review %+v", review)
	}
	text := engine.DiffText(review.Lines)
	if !strings.Contains(text, "+   enable-ipv6: \"true\"") || !strings.Contains(text, "-   bpf-lb-acceleration: disabled") {
		t.Errorf("the diff:\n%s", text)
	}
	if len(c.all()) != 0 {
		t.Error("the review asked for a confirmation")
	}

	var saved engine.ConfigMapDataSaved
	h.must(t, gui, "configMap.saveData", params, &saved)
	req := c.last(t)
	if req.Danger != engine.DangerDestructive || req.Origin != gui || req.Context != "test" || !slices.Equal(req.Namespaces, []string{"shop"}) ||
		len(req.Objects) != 1 || req.Objects[0].Name != "cilium-config" || !slices.Equal(req.UIDs, []string{"uid-shop-cilium-config"}) ||
		req.Detail != "Removes: bpf-lb-acceleration. Adds: enable-ipv6." || req.Undoable ||
		!slices.Equal(req.Effects, []engine.Effect{{Name: "Removes", Value: "bpf-lb-acceleration", Tone: engine.EffectLost},
			{Name: "Adds", Value: "enable-ipv6", Tone: engine.EffectNames}}) {
		t.Errorf("the confirmation of a removal: %+v", req)
	}
	got, _, _ := unstructured.NestedStringMap(srv.lastUpdate().Object, "data")
	if _, ok := got["bpf-lb-acceleration"]; ok || got["enable-ipv6"] != "true" || got["enable-ipv4"] != "false" || got["huge"] != bigValue || len(got) != 5 {
		t.Errorf("the update's data %v", got)
	}
	if bin, _, _ := unstructured.NestedStringMap(srv.lastUpdate().Object, "binaryData"); len(bin) != 1 {
		t.Errorf("the binary data was not kept: %v", bin)
	}
	if !slices.Equal(saved.Keys, []string{"enable-ipv4"}) || !slices.Equal(saved.KeysAdded, []string{"enable-ipv6"}) ||
		!slices.Equal(saved.KeysRemoved, []string{"bpf-lb-acceleration"}) {
		t.Errorf("saved %+v", saved)
	}

	// Adding a key only: mutating, the window's own save is not asked.
	asked := len(c.all())
	rv := cachedRV(t, h, cmApp, data.ResourceVersion)
	h.must(t, gui, "configMap.saveData", map[string]any{"ref": cmApp, "resourceVersion": rv, "add": map[string]any{"log-level": "debug"}}, &saved)
	if len(c.all()) != asked || !slices.Equal(saved.KeysAdded, []string{"log-level"}) || len(saved.Keys) != 0 {
		t.Errorf("an added key: asked %d times, saved %+v", len(c.all())-asked, saved)
	}

	// Declined, a removal writes nothing.
	c.mu.Lock()
	c.answer = false
	c.mu.Unlock()
	writes := len(h.writer.Calls())
	rv = cachedRV(t, h, cmApp, rv)
	err := h.run(t, gui, "configMap.saveData", map[string]any{"ref": cmApp, "resourceVersion": rv, "remove": []string{"config.yaml"}}, nil)
	if !errors.Is(err, engine.ErrDeclined) || len(h.writer.Calls()) != writes {
		t.Errorf("a declined removal: %v, writes %v", err, h.writer.Calls()[writes:])
	}
}

// TestConfigMapDataKeyRefusals: an added key follows the API server's rule and is not one the data
// holds; a removed key is one it holds and not binary; a key is named once. Nothing is written.
func TestConfigMapDataKeyRefusals(t *testing.T) {
	h, _ := configMapHarness(t, engine.Options{})
	h.s.SetConfirmer(&confirmations{answer: true})
	gui := engine.OriginGUI
	var data engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	for _, c := range []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"an empty key", map[string]any{"add": map[string]any{"": "x"}}, "a key needs a name"},
		{"a space", map[string]any{"add": map[string]any{"two words": "x"}}, "only letters, digits"},
		{"a slash", map[string]any{"add": map[string]any{"a/b": "x"}}, "only letters, digits"},
		{"a dot", map[string]any{"add": map[string]any{".": "x"}}, "is not \".\" or \"..\""},
		{"two dots first", map[string]any{"add": map[string]any{"..cfg": "x"}}, "does not start with \"..\""},
		{"254 characters", map[string]any{"add": map[string]any{strings.Repeat("k", 254): "x"}}, "at most 253 characters"},
		{"a key the data holds", map[string]any{"add": map[string]any{"enable-ipv4": "x"}}, "holds the key \"enable-ipv4\" already"},
		{"a binary key added", map[string]any{"add": map[string]any{"logo.png": "x"}}, "binary data"},
		{"a key it does not hold removed", map[string]any{"remove": []string{"gone"}}, "has no key \"gone\" to remove"},
		{"a binary key removed", map[string]any{"remove": []string{"logo.png"}}, "binary data"},
		{"removed twice", map[string]any{"remove": []string{"huge", "huge"}}, "removed twice"},
		{"changed and removed", map[string]any{"data": map[string]any{"huge": "x"}, "remove": []string{"huge"}}, "both changed and removed"},
		{"changed and added", map[string]any{"data": map[string]any{"new": "x"}, "add": map[string]any{"new": "y"}}, "both changed and added"},
		{"nothing", map[string]any{}, "give data, add or remove"},
	} {
		c.params["ref"], c.params["resourceVersion"] = cmApp, data.ResourceVersion
		for _, id := range []string{"configMap.reviewData", "configMap.saveData"} {
			if err := h.run(t, gui, id, c.params, nil); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s, %s: %v, want %q", c.name, id, err, c.want)
			}
		}
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Errorf("a refused edit wrote: %v", calls)
	}
	// 253 characters of the allowed ones are a key.
	long := strings.Repeat("a.b-c_D9", 31) + "12345"
	var review engine.ConfigMapDataReview
	h.must(t, gui, "configMap.reviewData", map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion,
		"add": map[string]any{long: "v"}}, &review)
	if len(long) != 253 || !slices.Equal(review.KeysAdded, []string{long}) || !review.Accepted {
		t.Errorf("a key of 253 characters: %+v", review)
	}
}

// TestConfigMapDataKeysRebase (D-55's rebase with keys): a change of another key rebases an edit
// that adds and removes keys; an added key that appeared meanwhile, a removed key changed or gone
// meanwhile, and a removed key without its value at the base are conflicts, shown against the newer
// version as a save over it would write them; the save never rebases.
func TestConfigMapDataKeysRebase(t *testing.T) {
	h, srv := configMapHarness(t, engine.Options{})
	h.s.SetConfirmer(&confirmations{answer: true})
	gui := engine.OriginGUI
	var data engine.ConfigMapData
	h.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	base := data.ResourceVersion
	write := func(set map[string]string, drop ...string) string {
		t.Helper()
		obj, err := srv.current(configMapsGVR, "shop", "cilium-config")
		if err != nil {
			t.Fatal(err)
		}
		before := obj.GetResourceVersion()
		for k, v := range set {
			if err := unstructured.SetNestedField(obj.Object, v, "data", k); err != nil {
				t.Fatal(err)
			}
		}
		for _, k := range drop {
			unstructured.RemoveNestedField(obj.Object, "data", k)
		}
		h.src.Set(configMapsGVR, obj)
		return cachedRV(t, h, cmApp, before)
	}
	edit := func(rv string) map[string]any {
		return map[string]any{"ref": cmApp, "resourceVersion": rv, "add": map[string]any{"enable-ipv6": "true"},
			"remove": []string{"bpf-lb-acceleration"}, "from": map[string]any{"bpf-lb-acceleration": "disabled"}}
	}
	review := func(params map[string]any) engine.ConfigMapDataReview {
		t.Helper()
		var r engine.ConfigMapDataReview
		h.must(t, gui, "configMap.reviewData", params, &r)
		return r
	}

	newer := write(map[string]string{"enable-ipv4": "false"})
	if r := review(edit(base)); r.RebasedFrom != base || r.BaseResourceVersion != newer || r.Stale || !r.Accepted ||
		!slices.Equal(r.KeysAdded, []string{"enable-ipv6"}) || !slices.Equal(r.KeysRemoved, []string{"bpf-lb-acceleration"}) {
		t.Fatalf("an edit of keys over another key's change: %+v", r)
	}
	saveOf := func(params map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range params {
			if k != "from" {
				out[k] = v
			}
		}
		return out
	}
	err := h.run(t, gui, "configMap.saveData", saveOf(edit(base)), nil)
	var ce *engine.DataConflictError
	if !errors.As(err, &ce) || ce.CurrentResourceVersion != newer {
		t.Fatalf("a save based on the older version: %v", err)
	}
	noFrom := edit(base)
	delete(noFrom, "from")
	if r := review(noFrom); r.RebasedFrom != "" || !r.Stale {
		t.Errorf("a removal without its value at the base: %+v", r)
	}

	// The added key appeared on the cluster: a conflict, shown as a change of its value.
	appeared := write(map[string]string{"enable-ipv6": "false"})
	r := review(edit(newer))
	if r.RebasedFrom != "" || !r.Stale || r.CachedResourceVersion != appeared || !slices.Equal(r.Keys, []string{"enable-ipv6"}) ||
		len(r.KeysAdded) != 0 || !slices.Equal(r.KeysRemoved, []string{"bpf-lb-acceleration"}) {
		t.Errorf("an added key that appeared: %+v", r)
	}
	if err := h.run(t, gui, "configMap.saveData", saveOf(edit(newer)), nil); !errors.As(err, &ce) || len(ce.Diff) == 0 {
		t.Errorf("a save over an added key that appeared: %v", err)
	}
	// Saving over it, the window sends the appeared key as a change (what the review showed).
	var saved engine.ConfigMapDataSaved
	h.must(t, gui, "configMap.saveData", map[string]any{"ref": cmApp, "resourceVersion": appeared,
		"data": map[string]any{"enable-ipv6": "true"}, "remove": []string{"bpf-lb-acceleration"}}, &saved)
	got, _, _ := unstructured.NestedStringMap(srv.lastUpdate().Object, "data")
	if _, ok := got["bpf-lb-acceleration"]; ok || got["enable-ipv6"] != "true" || got["enable-ipv4"] != "false" {
		t.Errorf("the save over the appeared key: %v", got)
	}

	// A removed key changed on the cluster: a conflict; gone there: a conflict that removes nothing.
	at := cachedRV(t, h, cmApp, appeared)
	removal := func(rv string) map[string]any {
		return map[string]any{"ref": cmApp, "resourceVersion": rv, "remove": []string{"config.yaml"}, "from": map[string]any{"config.yaml": "a: 1\nb: 2\n"}}
	}
	changed := write(map[string]string{"config.yaml": "a: 2\n"})
	if r := review(removal(at)); r.RebasedFrom != "" || !r.Stale || r.CachedResourceVersion != changed || !slices.Equal(r.KeysRemoved, []string{"config.yaml"}) {
		t.Errorf("a removed key that changed: %+v", r)
	}
	gone := write(nil, "config.yaml")
	if r := review(removal(at)); r.RebasedFrom != "" || !r.Stale || r.CachedResourceVersion != gone || len(r.KeysRemoved) != 0 {
		t.Errorf("a removed key that is gone: %+v", r)
	}
}

// TestConfigMapDataAgentRemovalAsks (D-2): an agent's save that removes a key always waits for the
// user, even where agent writes are allowed; the confirmation names the keys and shows the diff. An
// agent's save that adds a key runs unasked there, as a change of values does.
func TestConfigMapDataAgentRemovalAsks(t *testing.T) {
	audit := &syncBuffer{}
	h, srv := configMapHarness(t, engine.Options{Audit: audit, ClusterSettings: engine.NewClusterSettingsStore(t.TempDir())})
	c := &confirmations{answer: false}
	h.s.SetConfirmer(c)
	agent := engine.OriginExternalAgent
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil)
	var data engine.ConfigMapData
	h.must(t, agent, "configMap.readData", map[string]any{"ref": cmApp}, &data)

	params := map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion, "remove": []string{"enable-ipv4"}}
	if err := h.run(t, agent, "configMap.saveData", params, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("an agent's removal where agent writes are allowed: %v", err)
	}
	req := c.last(t)
	if req.Danger != engine.DangerDestructive || req.Origin != agent || req.Detail != "Removes: enable-ipv4." ||
		!strings.Contains(req.Review, "-   enable-ipv4: \"true\"") || !strings.Contains(req.Message(), "An agent asks to run") {
		t.Errorf("the confirmation %+v\n%s", req, req.Review)
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Fatalf("a declined removal wrote: %v", calls)
	}
	asked := len(c.all())
	h.must(t, agent, "configMap.saveData", map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion,
		"add": map[string]any{"log-level": "info"}}, nil)
	if len(c.all()) != asked {
		t.Error("an agent's added key asked although agent writes are allowed")
	}
	if got, _, _ := unstructured.NestedString(srv.lastUpdate().Object, "data", "log-level"); got != "info" {
		t.Errorf("the added key: %q", got)
	}
	// An agent's added value that holds a placeholder for a Secret's value is refused unasked.
	if err := h.run(t, agent, "configMap.saveData", map[string]any{"ref": cmApp, "resourceVersion": data.ResourceVersion,
		"add": map[string]any{"dsn": "«secret shop/db/password»"}}, nil); !errors.Is(err, engine.ErrAgentMarkerWrite) {
		t.Errorf("an agent's placeholder in add: %v", err)
	}
	var requested bool
	for _, l := range audit.Lines() {
		requested = requested || (l.Command == "configMap.saveData" && strings.Contains(string(l.Params), `"remove":["enable-ipv4"]`))
	}
	if !requested {
		t.Errorf("the audit log does not name the removed key: %+v", audit.Lines())
	}
}

// TestConfigMapDrawerDraftsKeepKeys: the keys the window added and removed are kept with its
// values, in memory and in the draft, and come back with a restored draft; an agent's listing names
// them without a value.
func TestConfigMapDrawerDraftsKeepKeys(t *testing.T) {
	gui, agent := engine.OriginGUI, engine.OriginExternalAgent
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h1, _ := configMapHarness(t, engine.Options{Drafts: engine.NewDrafts(dir).WithDelay(20 * time.Millisecond)})
	var data engine.ConfigMapData
	h1.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	h1.must(t, gui, "configMap.keepEdits", map[string]any{"ref": cmAppIn, "resourceVersion": data.ResourceVersion, "revision": 1,
		"add": map[string]any{"log-level": "kept-value-7f"}, "remove": []string{"huge"}, "from": map[string]any{"huge": bigValue}}, nil)
	h1.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	if u := data.Unsaved; u == nil || u.Add["log-level"] != "kept-value-7f" || !slices.Equal(u.Remove, []string{"huge"}) || len(u.Values) != 0 {
		t.Fatalf("kept %+v", data.Unsaved)
	}
	if err := h1.run(t, gui, "configMap.keepEdits", map[string]any{"ref": cmAppIn, "resourceVersion": data.ResourceVersion, "revision": 2,
		"add": map[string]any{"bad key": "x"}}, nil); err == nil || !strings.Contains(err.Error(), "only letters") {
		t.Errorf("an invalid added key kept: %v", err)
	}
	waitDraftFiles(t, dir, 1)
	if files := draftFiles(t, dir); files[0].Add["log-level"] != "kept-value-7f" || !slices.Equal(files[0].Remove, []string{"huge"}) {
		t.Fatalf("the draft %+v", files[0])
	}
	h1.s.Close()

	h2, _ := configMapHarness(t, engine.Options{Drafts: engine.NewDrafts(dir).WithDelay(20 * time.Millisecond)})
	var list []engine.DraftInfo
	h2.must(t, agent, "editor.drafts", map[string]any{}, &list)
	if len(list) != 1 || !slices.Equal(list[0].Keys, []string{"huge", "log-level"}) || !slices.Equal(list[0].KeysRemoved, []string{"huge"}) {
		t.Fatalf("drafts %+v", list)
	}
	if raw, _ := json.Marshal(list); strings.Contains(string(raw), "kept-value-7f") {
		t.Errorf("an agent's listing holds a value: %s", raw)
	}
	h2.must(t, agent, "editor.restoreDraft", map[string]any{"id": list[0].ID}, nil)
	h2.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	if u := data.Unsaved; u == nil || !u.Restored || u.Add["log-level"] != "kept-value-7f" || !slices.Equal(u.Remove, []string{"huge"}) {
		t.Errorf("restored %+v", data.Unsaved)
	}
}

// TestSecretDrawerDataAddsAndRemovesKeys: the window reviews an edit of a Secret that adds a key
// (typed as text) and removes one: the masked diff names both with their sizes, never a value. The
// save removes a key, so the confirmation is destructive and names the keys only; the update holds
// the added value base64-encoded and not the removed key. No value reaches a result, the
// confirmation, a toast, the audit log or the log.
func TestSecretDrawerDataAddsAndRemovesKeys(t *testing.T) {
	h := newSecretDataHarness(t)
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	gui := engine.OriginGUI
	h.show(t, sdRef)
	obj, _ := h.s.Lookup(sdRef)
	base := obj.GetResourceVersion()
	params := map[string]any{"ref": sdRef, "resourceVersion": base, "add": map[string]any{"api-key": sdTyped}, "remove": []string{"token"},
		"from": map[string]any{"token": sdToken}}
	var review engine.SecretDataReview
	h.must(t, gui, engine.SecretReviewDataCommand, params, &review)
	want := []engine.SecretKeyChange{{Key: "api-key", Change: engine.SecretKeyAdded, NewBytes: len(sdTyped)},
		{Key: "token", Change: engine.SecretKeyRemoved, Bytes: len(sdToken)}}
	if !slices.Equal(review.Changes, want) || !review.Accepted || review.UID != "uid-shop-db-creds" {
		t.Fatalf("review %+v", review)
	}
	var saved engine.SecretDataSaved
	h.must(t, gui, engine.SecretSaveDataCommand, map[string]any{"ref": sdRef, "resourceVersion": base, "add": params["add"], "remove": params["remove"]}, &saved)
	req := c.last(t)
	if req.Danger != engine.DangerDestructive || req.Detail != "Removes: token. Adds: api-key. Values: not shown." ||
		req.Context != "test" || !slices.Equal(req.UIDs, []string{"uid-shop-db-creds"}) {
		t.Errorf("the confirmation %+v", req)
	}
	data, _, _ := unstructured.NestedStringMap(h.srv.lastUpdate().Object, "data")
	if _, ok := data["token"]; ok || data["api-key"] != base64.StdEncoding.EncodeToString([]byte(sdTyped)) || len(data) != 4 || data["ca.der"] == "" {
		t.Errorf("the update's data %v", data)
	}
	if !slices.Equal(saved.KeysAdded, []string{"api-key"}) || !slices.Equal(saved.KeysRemoved, []string{"token"}) || len(saved.Keys) != 0 {
		t.Errorf("saved %+v", saved)
	}
	for _, v := range []any{review, saved, req} {
		b, _ := json.Marshal(v)
		if l := sdLeak(string(b)); l != "" {
			t.Errorf("%q in %s", l, b)
		}
	}
	if l := sdLeak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}

	// A binary value can be removed; refusals name keys, never a value.
	rv := cachedRV(t, h.harness, sdRef, base)
	h.show(t, sdRef)
	for _, c := range []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"a key the data holds", map[string]any{"add": map[string]any{"password": sdTyped}}, "holds the key \"password\" already"},
		{"an invalid key", map[string]any{"add": map[string]any{"my key": sdTyped}}, "only letters"},
		{"a key it does not hold", map[string]any{"remove": []string{"token"}}, "has no key \"token\" to remove"},
	} {
		c.params["ref"], c.params["resourceVersion"] = sdRef, rv
		if err := h.run(t, gui, engine.SecretSaveDataCommand, c.params, nil); err == nil || !strings.Contains(err.Error(), c.want) || sdLeak(err.Error()) != "" {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	h.must(t, gui, engine.SecretSaveDataCommand, map[string]any{"ref": sdRef, "resourceVersion": rv, "remove": []string{"ca.der"}}, &saved)
	if !slices.Equal(saved.KeysRemoved, []string{"ca.der"}) {
		t.Errorf("a binary value removed: %+v", saved)
	}
}

// TestSecretDrawerDataKeysConflict: an added key that appeared on the cluster and a removed key that
// changed there are conflicts, never rebased; a change of another key rebases the edit. The stale
// review shows what a save over the newer version would write, masked.
func TestSecretDrawerDataKeysConflict(t *testing.T) {
	h := newSecretDataHarness(t)
	gui := engine.OriginGUI
	h.show(t, sdRef)
	obj, _ := h.s.Lookup(sdRef)
	base := obj.GetResourceVersion()
	change := func(key, value string) string {
		t.Helper()
		cur, err := h.srv.current(secretsGVR, "shop", "db-creds")
		if err != nil {
			t.Fatal(err)
		}
		if err := unstructured.SetNestedField(cur.Object, base64.StdEncoding.EncodeToString([]byte(value)), "data", key); err != nil {
			t.Fatal(err)
		}
		before, _ := h.s.Lookup(sdRef)
		h.src.Set(secretsGVR, cur)
		h.show(t, sdRef)
		return cachedRV(t, h.harness, sdRef, before.GetResourceVersion())
	}
	params := map[string]any{"ref": sdRef, "resourceVersion": base, "add": map[string]any{"api-key": sdTyped}, "remove": []string{"token"},
		"from": map[string]any{"token": sdToken}}
	review := func() engine.SecretDataReview {
		t.Helper()
		var r engine.SecretDataReview
		h.must(t, gui, engine.SecretReviewDataCommand, params, &r)
		return r
	}
	newer := change("password", sdOther)
	if r := review(); r.RebasedFrom != base || r.BaseResourceVersion != newer || r.Stale || len(r.Changes) != 2 {
		t.Fatalf("an edit of keys over another key's change: %+v", r)
	}
	appeared := change("api-key", sdOther)
	r := review()
	if r.RebasedFrom != "" || !r.Stale || r.CachedResourceVersion != appeared || len(r.Changes) != 2 ||
		r.Changes[0] != (engine.SecretKeyChange{Key: "api-key", Change: engine.SecretKeyChanged, Bytes: len(sdOther), NewBytes: len(sdTyped)}) {
		t.Errorf("an added key that appeared: %+v", r)
	}
	params["resourceVersion"], params["add"] = appeared, map[string]any{}
	params["data"] = map[string]any{"api-key": sdTyped}
	params["from"] = map[string]any{"token": sdToken, "api-key": sdOther}
	changed := change("token", sdPassword)
	if r := review(); r.RebasedFrom != "" || !r.Stale || r.CachedResourceVersion != changed {
		t.Errorf("a removed key that changed: %+v", r)
	}
	if b, _ := json.Marshal(r); sdLeak(string(b)) != "" {
		t.Errorf("a stale review carries a value: %s", b)
	}
}

// TestSecretDrawerDataKeysAudit: an agent's call with added and removed keys is refused before
// anything is read or sent (D-59), and its audit line names the keys only: the added keys' values
// redacted, the removed keys listed.
func TestSecretDrawerDataKeysAudit(t *testing.T) {
	h := newSecretDataHarness(t)
	h.show(t, sdRef)
	obj, _ := h.s.Lookup(sdRef)
	params := map[string]any{"ref": sdRef, "resourceVersion": obj.GetResourceVersion(), "add": map[string]any{"api-key": sdTyped},
		"remove": []string{"token"}}
	for _, id := range []string{engine.SecretReviewDataCommand, engine.SecretSaveDataCommand} {
		if err := h.run(t, engine.OriginExternalAgent, id, params, nil); !errors.Is(err, engine.ErrSecretDataGUIOnly) {
			t.Errorf("%s by an agent: %v", id, err)
		}
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Errorf("a refused call reached the cluster: %v", calls)
	}
	lines := 0
	for _, e := range h.audit.Lines() {
		if e.Command != engine.SecretSaveDataCommand && e.Command != engine.SecretReviewDataCommand {
			continue
		}
		lines++
		var p map[string]any
		if err := json.Unmarshal(e.Params, &p); err != nil {
			t.Fatal(err)
		}
		add, _ := p["add"].(map[string]any)
		remove, _ := p["remove"].([]any)
		if add["api-key"] != model.Redacted || len(remove) != 1 || remove[0] != "token" || p["otherParams"] != nil {
			t.Errorf("audit params %s", e.Params)
		}
	}
	if lines != 2 {
		t.Errorf("%d audit lines, want 2", lines)
	}
	if l := sdLeak(h.trail()); l != "" {
		t.Errorf("%q in the audit log, the events or the log", l)
	}
}
