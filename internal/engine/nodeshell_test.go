package engine

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// defaultImageForm is what the default image must stay when Renovate moves it (D-37): Alpine from
// Docker Hub, a release tag, pinned by the digest of its index.
var defaultImageForm = regexp.MustCompile(`^docker\.io/library/alpine:3\.\d+@sha256:[0-9a-f]{64}$`)

// TestNodeShellImageRules: what counts as pinned, what cannot be an image or a pull secret, and the
// default being pinned (privilege-footprint.md › Node shell: "a pinned default, by digest").
func TestNodeShellImageRules(t *testing.T) {
	if !ImagePinned(DefaultNodeShellImage) || !defaultImageForm.MatchString(DefaultNodeShellImage) {
		t.Errorf("the default %s is not an Alpine release pinned by digest", DefaultNodeShellImage)
	}
	if want := strings.TrimPrefix(DefaultNodeShellImage[:strings.Index(DefaultNodeShellImage, "@")], "docker.io/library/"); DefaultNodeShellImageName != want {
		t.Errorf("the default's name %q, want %q", DefaultNodeShellImageName, want)
	}
	for image, pinned := range map[string]bool{
		"alpine:3.24": false, "docker.io/alpine": false, "alpine@sha256:abc": false,
		"registry.example.com:5000/a/b@sha256:" + strings.Repeat("0", 64): true,
	} {
		if ImagePinned(image) != pinned {
			t.Errorf("ImagePinned(%q) = %v", image, !pinned)
		}
	}
	for _, bad := range []string{"alpine 3.24", "https://example.com/alpine", "a\nb", strings.Repeat("a", 513)} {
		if CheckNodeShellImage(bad) == nil {
			t.Errorf("image %q accepted", bad)
		}
	}
	for _, good := range []string{"", "alpine:3.24", "registry.example.com:5000/tools/shell:1.2"} {
		if err := CheckNodeShellImage(good); err != nil {
			t.Errorf("image %q: %v", good, err)
		}
	}
	if CheckNodeShellPullSecret("Not_A_Name") == nil || CheckNodeShellPullSecret("") != nil || CheckNodeShellPullSecret("registry-pull") != nil {
		t.Error("pull secret names")
	}
	cfg := NodeShellConfig{Image: "alpine:3.24", Deadline: DefaultNodeShellDeadline}
	if !slices.Contains(NodeShellEffects("n1", cfg), Effect{Name: "Digest", Value: "none: a floating tag, what it runs can change without Cogpit knowing", Tone: EffectWarning}) ||
		!strings.Contains(NodeShellDetail("n1", cfg), "Digest: none: a floating tag") {
		t.Error("a tag's effects do not say it floats")
	}
	pod := NodeShellPod("cogpit-node-shell-x", "n1", NodeShellConfig{Image: "alpine:3.24", PullSecret: "pull", Deadline: 90 * time.Minute})
	if len(pod.Spec.ImagePullSecrets) != 1 || pod.Spec.ImagePullSecrets[0].Name != "pull" || pod.Labels["app.kubernetes.io/name"] != "cogpit-node-shell" {
		t.Errorf("pod %+v", pod)
	}
	if d := pod.Spec.ActiveDeadlineSeconds; d == nil || *d != 5400 || !slices.Equal(pod.Spec.Containers[0].Command, []string{"sleep", "5400"}) {
		t.Errorf("a 90-minute pod: deadline %v, command %v", d, pod.Spec.Containers[0].Command)
	}
	pod.Labels["changed"] = "x"
	if _, shared := NodeShellLabels["changed"]; shared {
		t.Error("a pod's labels are NodeShellLabels themselves")
	}
	if name := nodeShellName(); len(name) != len(nodeShellNamePrefix)+8 || strings.ToLower(name) != name {
		t.Errorf("pod name %q", name)
	}
}

// TestNodeShellMaxDuration: the maximum duration as typed (90m, 2h, 1h30m), in whole minutes from 5
// minutes to 24 hours, empty the default; how the field and a sentence write it; a kept value out
// of the bounds read from a file is the default (owner, 2026-10-14, D-37).
func TestNodeShellMaxDuration(t *testing.T) {
	for text, want := range map[string]int{"": 0, " ": 0, "5m": 5, "90m": 90, "2h": 120, "1h30m": 90, " 4h ": 240, "24h": 1440, "300s": 5} {
		got, err := ParseNodeShellDeadline(text)
		if err != nil || got != want {
			t.Errorf("ParseNodeShellDeadline(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
	for text, why := range map[string]string{"4m": "from 5m to 24h", "24h1m": "from 5m to 24h", "0m": "from 5m to 24h", "-1h": "from 5m to 24h",
		"90": "such as 90m", "2 hours": "such as 90m", "10m30s": "whole minutes", "9999999h": "such as 90m"} {
		if _, err := ParseNodeShellDeadline(text); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("ParseNodeShellDeadline(%q): %v, want %q", text, err, why)
		}
	}
	for _, m := range []int{4, 1441, -5} {
		if CheckNodeShellMaxMinutes(m) == nil {
			t.Errorf("%d minutes kept", m)
		}
	}
	if CheckNodeShellMaxMinutes(0) != nil || CheckNodeShellMaxMinutes(5) != nil || CheckNodeShellMaxMinutes(1440) != nil {
		t.Error("the bounds refused")
	}
	for d, want := range map[time.Duration][2]string{
		2 * time.Hour: {"2h", "2 hours"}, 90 * time.Minute: {"1h30m", "1 hour 30 minutes"}, 45 * time.Minute: {"45m", "45 minutes"},
		time.Hour: {"1h", "1 hour"}, 61 * time.Minute: {"1h1m", "1 hour 1 minute"}, 24 * time.Hour: {"24h", "24 hours"},
	} {
		if got := [2]string{FormatNodeShellDeadline(d), NodeShellDeadlineText(d)}; got != want {
			t.Errorf("%s written %q, want %q", d, got, want)
		}
	}
	for minutes, want := range map[int]time.Duration{0: 2 * time.Hour, 90: 90 * time.Minute, 2000: 2 * time.Hour} {
		if got := nodeShellDeadline(minutes); got != want {
			t.Errorf("nodeShellDeadline(%d) = %s", minutes, got)
		}
	}
	cs := ClusterSettings{NodeShellMaxMinutes: 3000}
	cs.normalize()
	if cs.NodeShellMaxMinutes != 0 || (ClusterSettings{NodeShellMaxMinutes: 4}).Check() == nil {
		t.Errorf("a maximum duration out of bounds kept: %+v", cs)
	}
	if (ClusterSettings{NodeShellMaxMinutes: 90}).Equal(ClusterSettings{}) {
		t.Error("a set maximum duration equals the defaults")
	}
}
