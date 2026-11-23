package commands

import (
	"context"
	"fmt"
	"slices"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// trivy.setTrendRange: the range of the Image Security page's findings curve, UI state the engine
// holds (UIState.ImageSecurity), set by the page's range control and by agents alike.

type imageSecTrendRangeParams struct {
	Days int `json:"days,omitempty" jsonschema:"the curve's range in days: 7, 30 or 90; omitted or 0: the default 30"`
}

func registerImageSecurityTrend(g *registrar) {
	add(g, engine.Spec[imageSecTrendRangeParams]{
		ID: "trivy.setTrendRange", Label: "Findings Trend Range", Danger: engine.DangerUI,
		Description: "Sets the range of the Image Security page's findings curve, as the page's range control does: 7, 30 " +
			"or 90 days. The curve shows the findings (a vulnerability of an image, each image counted once) by " +
			"severity over that range, a point a day (6 hours for 7 days), from a Prometheus that scrapes Trivy " +
			"Operator's metrics, in the namespaces of the namespace filter; it needs the Trivy and the Prometheus " +
			"integration on. The range stays for the session, whichever view is open. Returns the new UI state " +
			"(imageSecurity).",
		Enabled: func(s *engine.Session, _ imageSecTrendRangeParams) error { _, err := connected(s); return err },
		Run: func(_ context.Context, s *engine.Session, p imageSecTrendRangeParams) (any, error) {
			days := p.Days
			switch {
			case days == 0 || days == engine.DefaultImageSecurityTrendDays:
				days = 0
			case !slices.Contains(engine.ImageSecurityTrendDays, days):
				return nil, fmt.Errorf("the findings curve's range is 7, 30 or 90 days, not %d", p.Days)
			}
			return s.UpdateUI(func(u *engine.UIState) { u.ImageSecurity.TrendDays = days }), nil
		},
	})
}
