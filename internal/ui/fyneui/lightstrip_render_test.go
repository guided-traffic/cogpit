package fyneui

import (
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The light strip's pictures (D-122) for the owner's walk, dark and light: off (frosted glass),
// static, each animation at a fixed moment of its clock, a change through glass half-way through its
// fade-in, and one static colour spreading over another from the tab half way.
// The clusters' colours are the owner's lab examples (blue static, green breathing, red scanner,
// orange flow). The strip's clock is the test's, never the wall's.
func TestRenderLightStrip(t *testing.T) {
	cases := []struct {
		name, color, anim string
		at                time.Duration
		then              func(t *testing.T, sh *shell, f *fakeEngine, q *mainQueue, c *stripClock)
	}{
		{name: "off", at: stripFade},
		{name: "static", color: "blue", anim: engine.LightStripStatic, at: 2 * time.Second},
		// A quarter of a breath in: half way up from its lowest point.
		{name: "breathing", color: "green", anim: engine.LightStripBreathing, at: time.Duration(stripBreathePeriod / 4 * float64(time.Second))},
		// Three quarters of a period: the light in the middle on its way back.
		{name: "scanner", color: "red", anim: engine.LightStripScanner, at: time.Duration(stripScanPeriod * 3 / 4 * float64(time.Second))},
		// 40 % into the second pass: the new light in the left half, the last one's tail running out.
		{name: "flow", color: "orange", anim: engine.LightStripFlow, at: time.Duration(stripFlowPeriod * 1.4 * float64(time.Second))},
		// Blue changed to a red scanner in Cluster Settings at 2 s: through glass, blue has faded
		// out, red is half-way in.
		{name: "transition", color: "blue", anim: engine.LightStripStatic, at: 2 * time.Second,
			then: func(t *testing.T, sh *shell, f *fakeEngine, q *mainQueue, c *stripClock) {
				f.update(func(u *engine.UIState) {
					u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {AccentColor: "red", LightStrip: engine.LightStripScanner}}
				})
				q.drain()
				c.at(sh, 2*time.Second+stripHalf+stripHalf/2)
			}},
		// Blue changed to red, both static, at 2 s: red spreads from the active tab's place (LS22),
		// half way.
		{name: "spread", color: "blue", anim: engine.LightStripStatic, at: 2 * time.Second,
			then: func(t *testing.T, sh *shell, f *fakeEngine, q *mainQueue, c *stripClock) {
				f.update(func(u *engine.UIState) {
					u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {AccentColor: "red"}}
				})
				q.drain()
				c.at(sh, 2*time.Second+stripFade/2)
			}},
	}
	for _, mode := range []ThemeMode{ThemeDark} {
		for _, tc := range cases {
			t.Run(tc.name+"_"+string(mode), func(t *testing.T) {
				sh, w, q, f, c := stripShell(t, mode, tc.color, tc.anim)
				c.at(sh, tc.at)
				if tc.then != nil {
					tc.then(t, sh, f, q, c)
				}
				settle(w)
				sh.strip.paint(c.now) // the lights' places at the width the settle gave
				savePNG(t, w, "light_strip_"+tc.name+"_"+string(mode)+".png")
			})
		}
	}
}
