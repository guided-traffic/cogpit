package kinds

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func argoWindowApp(name, ns, server string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": name, "namespace": "argocd"},
		"spec":     map[string]any{"project": "team", "destination": map[string]any{"namespace": ns, "server": server}},
	}}
}

func argoWindowProject(windows ...map[string]any) *unstructured.Unstructured {
	l := make([]any, 0, len(windows))
	for _, w := range windows {
		l = append(l, w)
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "AppProject",
		"metadata": map[string]any{"name": "team", "namespace": "argocd"},
		"spec":     map[string]any{"syncWindows": l},
	}}
}

// TestArgoSyncBlocked (A33): a manual sync against an AppProject's windows, as Argo CD checks it.
func TestArgoSyncBlocked(t *testing.T) {
	// A Wednesday, 10:30 UTC.
	now := time.Date(2026, 10, 14, 10, 30, 0, 0, time.UTC)
	app := argoWindowApp("shop-web", "shop", "https://kubernetes.default.svc")
	denyNow := map[string]any{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h", "applications": []any{"shop-*"}}
	denyLater := map[string]any{"kind": "deny", "schedule": "0 22 * * *", "duration": "1h", "applications": []any{"*"}}
	allowNow := map[string]any{"kind": "allow", "schedule": "0 9 * * 3", "duration": "4h", "namespaces": []any{"shop"}}
	allowLater := map[string]any{"kind": "allow", "schedule": "0 22 * * *", "duration": "2h", "clusters": []any{"https://kubernetes.default.svc"}}
	otherApp := map[string]any{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h", "applications": []any{"billing-*"}}
	berlin := map[string]any{"kind": "deny", "schedule": "0 12 * * *", "duration": "1h", "applications": []any{"*"}, "timeZone": "Europe/Berlin"}
	for _, c := range []struct {
		name    string
		windows []map[string]any
		blocked string // a part of the reason, "" for not blocked
	}{
		{"no windows", nil, ""},
		{"an active deny", []map[string]any{denyNow}, "deny window \"0 10 * * *\""},
		{"an active deny that allows manual syncs", []map[string]any{withManual(denyNow)}, ""},
		{"a deny not active", []map[string]any{denyLater}, ""},
		{"a deny for another app", []map[string]any{otherApp}, ""},
		{"an active allow", []map[string]any{allowNow}, ""},
		{"allow windows, none active", []map[string]any{allowLater}, "no allow window"},
		{"allow windows, none active, manual allowed", []map[string]any{withManual(allowLater)}, ""},
		{"deny beats allow", []map[string]any{allowNow, denyNow}, "deny window"},
		{"a deny in Berlin's noon (10:00 UTC)", []map[string]any{berlin}, "Europe/Berlin"},
		{"andOperator: name matches, namespace not", []map[string]any{{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h",
			"applications": []any{"shop-*"}, "namespaces": []any{"billing"}, "andOperator": true}}, ""},
		{"andOperator: both match", []map[string]any{{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h",
			"applications": []any{"shop-*"}, "namespaces": []any{"shop"}, "andOperator": true}}, "deny window"},
	} {
		got, err := ArgoSyncBlocked(app, argoWindowProject(c.windows...), now)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if (c.blocked == "") != (got == "") || !strings.Contains(got, c.blocked) {
			t.Errorf("%s: %q, want %q", c.name, got, c.blocked)
		}
	}
	if got, _ := ArgoSyncBlocked(app, argoWindowProject(denyNow), now); !strings.Contains(got, "until 2026-10-14T11:00:00Z") {
		t.Errorf("the deny's end: %q", got)
	}
	for _, bad := range []map[string]any{
		{"kind": "maybe", "schedule": "0 10 * * *", "duration": "1h"},
		{"kind": "deny", "schedule": "not cron", "duration": "1h"},
		{"kind": "deny", "schedule": "0 10 * * *", "duration": "soon"},
		{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h", "timeZone": "Mars/Olympus"},
		{"kind": "deny", "schedule": "@daily", "duration": "1h"}, // Argo CD's parser takes no descriptor
	} {
		if _, err := ArgoSyncBlocked(app, argoWindowProject(bad), now); err == nil {
			t.Errorf("a window that cannot be read passed: %v", bad)
		}
	}
}

// TestArgoSyncBlockedAsArgoCD35 holds the check to Argo CD 3.5.5's CanSync where Cogpit's first
// reading of it differed (read in its source and checked against a live controller on a real
// cluster): a manual sync passes a deny window only when every active deny window allows manual
// syncs, and passes closed allow windows only when every one of them does; a cluster pattern's *
// matches a server URL's slashes.
func TestArgoSyncBlockedAsArgoCD35(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 30, 0, 0, time.UTC)
	app := argoWindowApp("shop-web", "shop", "https://kubernetes.default.svc")
	deny := map[string]any{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h", "applications": []any{"*"}}
	denyToo := map[string]any{"kind": "deny", "schedule": "15 10 * * *", "duration": "1h", "namespaces": []any{"shop"}}
	allowLater := map[string]any{"kind": "allow", "schedule": "0 22 * * *", "duration": "2h", "applications": []any{"*"}}
	allowNight := map[string]any{"kind": "allow", "schedule": "0 2 * * *", "duration": "2h", "namespaces": []any{"sh?p"}}
	for _, c := range []struct {
		name    string
		windows []map[string]any
		blocked string
	}{
		{"two active denies, one allows manual syncs", []map[string]any{withManual(deny), denyToo}, "deny window \"15 10 * * *\""},
		{"two active denies, both allow manual syncs", []map[string]any{withManual(deny), withManual(denyToo)}, ""},
		{"two closed allows, one allows manual syncs", []map[string]any{withManual(allowLater), allowNight}, "no allow window"},
		{"two closed allows, both allow manual syncs", []map[string]any{withManual(allowLater), withManual(allowNight)}, ""},
		{"a cluster pattern * over the server URL", []map[string]any{{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h",
			"clusters": []any{"*"}}}, "deny window"},
		{"a cluster pattern https://*.svc", []map[string]any{{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h",
			"clusters": []any{"https://*.svc"}}}, "deny window"},
		{"alternatives in a name pattern", []map[string]any{{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h",
			"applications": []any{"{billing,shop}-*"}}}, "deny window"},
		{"a negated class", []map[string]any{{"kind": "deny", "schedule": "0 10 * * *", "duration": "1h",
			"applications": []any{"[!s]hop-*"}}}, ""},
	} {
		got, err := ArgoSyncBlocked(app, argoWindowProject(c.windows...), now)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if (c.blocked == "") != (got == "") || !strings.Contains(got, c.blocked) {
			t.Errorf("%s: %q, want %q", c.name, got, c.blocked)
		}
	}
}

// TestArgoWindowOpenUntil: a deny window's end follows the openings that start before it closes;
// one that keeps reopening (every minute for an hour, as on the live check) says so instead of a
// time a minute away; a schedule that never fires keeps its window open, as Argo CD's check does.
func TestArgoWindowOpenUntil(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 30, 0, 0, time.UTC)
	app := argoWindowApp("shop-web", "shop", "https://kubernetes.default.svc")
	chained := map[string]any{"kind": "deny", "schedule": "0 10,11 * * *", "duration": "90m", "applications": []any{"*"}}
	if got, _ := ArgoSyncBlocked(app, argoWindowProject(chained), now); !strings.Contains(got, "until 2026-10-14T12:30:00Z") {
		t.Errorf("a chained window: %q", got)
	}
	always := map[string]any{"kind": "deny", "schedule": "* * * * *", "duration": "1h", "applications": []any{"*"}}
	if got, _ := ArgoSyncBlocked(app, argoWindowProject(always), now); !strings.Contains(got, "its schedule keeps it open") {
		t.Errorf("a window that keeps reopening: %q", got)
	}
	never := map[string]any{"kind": "deny", "schedule": "0 0 30 2 *", "duration": "1h", "applications": []any{"*"}}
	if got, _ := ArgoSyncBlocked(app, argoWindowProject(never), now); !strings.Contains(got, "its schedule keeps it open") {
		t.Errorf("a window whose schedule never fires: %q", got)
	}
	// Open from the minute after its start, as Argo CD's check (the first firing after now minus
	// the duration must be before now).
	exact := map[string]any{"kind": "deny", "schedule": "30 10 * * *", "duration": "1h", "applications": []any{"*"}}
	if got, _ := ArgoSyncBlocked(app, argoWindowProject(exact), now); got != "" {
		t.Errorf("a window at its very start: %q", got)
	}
}

func TestArgoGlobMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, value string
		want           bool
	}{
		{"*", "https://kubernetes.default.svc", true},
		{"https://*", "https://10.0.0.1:6443", true},
		{"shop-*", "shop-web", true},
		{"shop-*", "billing-web", false},
		{"sh?p", "shop", true},
		{"sh?p", "shoop", false},
		{"[a-c]*", "billing", true},
		{"[!a-c]*", "billing", false},
		{"{prod,stage}-*", "stage-eu", true},
		{"{prod,stage}-*", "dev-eu", false},
		{`a\*b`, "a*b", true},
		{`a\*b`, "axb", false},
		{"a.b", "axb", false},
		{"[unclosed", "[unclosed", false},
		{"{open", "{open", false},
		{"", "", true},
	} {
		if got := ArgoGlobMatch(c.pattern, c.value); got != c.want {
			t.Errorf("%q ~ %q: %v, want %v", c.pattern, c.value, got, c.want)
		}
	}
}

func withManual(w map[string]any) map[string]any {
	out := map[string]any{"manualSync": true}
	for k, v := range w {
		out[k] = v
	}
	return out
}
