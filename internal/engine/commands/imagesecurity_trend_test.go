package commands_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestImageSecurityTrendRange: trivy.setTrendRange sets the findings curve's range as UI state: 7,
// 30 or 90 days, 30 and omitted the default (none in the state), anything else refused.
func TestImageSecurityTrendRange(t *testing.T) {
	h := newImageSecHarness(t)
	agent := engine.OriginExternalAgent
	var st engine.UIState
	h.must(t, agent, "trivy.setTrendRange", map[string]any{"days": 7}, &st)
	if st.ImageSecurity.TrendDays != 7 || h.s.UIState().ImageSecurity.Days() != 7 {
		t.Fatalf("7 days: %+v", st.ImageSecurity)
	}
	h.must(t, agent, "trivy.setTrendRange", map[string]any{"days": 90}, nil)
	if d := h.s.UIState().ImageSecurity.Days(); d != 90 {
		t.Fatalf("90 days: %d", d)
	}
	for _, bad := range []int{1, 14, -7, 365} {
		if err := h.run(t, agent, "trivy.setTrendRange", map[string]any{"days": bad}, nil); err == nil {
			t.Errorf("%d days was taken", bad)
		}
	}
	if d := h.s.UIState().ImageSecurity.Days(); d != 90 {
		t.Fatalf("a refused range changed it: %d", d)
	}
	h.must(t, agent, "trivy.setTrendRange", map[string]any{"days": 30}, nil)
	if is := h.s.UIState().ImageSecurity; is != (engine.ImageSecurityState{}) || is.Days() != engine.DefaultImageSecurityTrendDays {
		t.Fatalf("30 days is the default: %+v", is)
	}
	h.must(t, agent, "trivy.setTrendRange", map[string]any{"days": 7}, nil)
	h.must(t, agent, "trivy.setTrendRange", nil, nil)
	if is := h.s.UIState().ImageSecurity; is != (engine.ImageSecurityState{}) {
		t.Fatalf("omitted: %+v", is)
	}
}
