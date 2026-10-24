package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A cluster's light strip (D-new): static, breathing, scanner or flow in any case, static kept as
// none; anything else refused, and dropped from a file that holds it; kept across a restart, shown
// in the UI state and the settings' view (static when none is set).
func TestLightStripKept(t *testing.T) {
	for in, want := range map[string]string{"static": "", " Static ": "", "": "", "breathing": "breathing", "SCANNER": "scanner", " flow": "flow"} {
		if got, err := ParseLightStrip(in); err != nil || got != want {
			t.Errorf("ParseLightStrip(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"blink", "rainbow", "breathe", "scanner;"} {
		if _, err := ParseLightStrip(in); err == nil {
			t.Errorf("ParseLightStrip(%q) took it", in)
		}
	}
	if err := (ClusterSettings{AccentColor: "red", LightStrip: "blink"}).Check(); err == nil {
		t.Error("Check took the light strip blink")
	}
	if err := (ClusterSettings{AccentColor: "red", LightStrip: "static"}).Check(); err == nil {
		t.Error("Check took static, which the settings keep as none")
	}

	dir := filepath.Join(t.TempDir(), "Cogpit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	doc := `{"version":1,"clusters":{"prod":{"accentColor":"red","lightStrip":"scanner"},` +
		`"odd":{"accentColor":"green","lightStrip":"blink"},"plain":{"accentColor":"blue","lightStrip":"static"}}}`
	file := filepath.Join(dir, ClusterSettingsFile)
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(Options{ClusterSettings: NewClusterSettingsStore(dir)})
	defer s.Close()
	u := s.UIState()
	if u.ClusterSettings["prod"].LightStrip != "scanner" || u.ClusterSettings["odd"].LightStrip != "" ||
		u.ClusterSettings["odd"].AccentColor != "green" || u.ClusterSettings["plain"].LightStrip != "" {
		t.Errorf("read from the file: %+v", u.ClusterSettings)
	}
	for ctx, want := range map[string]string{"prod": "scanner", "odd": "static", "plain": "static", "none": "static"} {
		if v, _ := s.ClusterSettingsView(ctx); v.LightStrip != want {
			t.Errorf("%s's view: light strip %q, want %q", ctx, v.LightStrip, want)
		}
	}
	if _, err := s.ChangeClusterSettings("prod", func(c *ClusterSettings) error { c.LightStrip = LightStripFlow; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings("prod", func(c *ClusterSettings) error { c.LightStrip = "blink"; return nil }); err == nil {
		t.Error("a change to a light strip Check refuses was kept")
	}
	b, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(b), `"lightStrip": "flow"`) || strings.Contains(string(b), "blink") {
		t.Errorf("cluster-settings.json: %s, %v", b, err)
	}
	s2 := New(Options{ClusterSettings: NewClusterSettingsStore(dir)})
	defer s2.Close()
	if cs, _ := s2.ClusterSettingsOf("prod"); cs.LightStrip != LightStripFlow || cs.AccentColor != "red" {
		t.Errorf("after a restart: %+v", cs)
	}
	if !(ClusterSettings{LightStrip: LightStripFlow}).Equal(ClusterSettings{LightStrip: LightStripFlow}) ||
		(ClusterSettings{LightStrip: LightStripFlow}).Equal(ClusterSettings{}) {
		t.Error("Equal does not compare the light strip")
	}
}
