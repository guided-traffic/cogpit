package metrics

import (
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// AddSample runs a chart series' bounds on their own: a live point is added when it is newer than
// the last, then the points older than age before it and beyond maxPoints are dropped.
func AddSample(pts []model.UsageSample, s model.UsageSample, maxPoints int, age time.Duration) []model.UsageSample {
	c := newChart()
	c.usage[""] = pts
	c.addUsage("", s, 0)
	c.trim(age, maxPoints)
	return c.usage[""]
}

// Kick makes a drawer's poll run now, as a range change does.
func Kick(h *Hub, sc Scope) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.kickLocked(sc)
}

// ErrorClass is errorClass.
func ErrorClass(err error) string { return errorClass(err) }

// Splice is splice for usage samples.
func Splice(old, add []model.UsageSample, from, to time.Time) []model.UsageSample {
	return splice(old, add, from, to, usageAt)
}

// GroupQueryMax is groupQueryMax.
const GroupQueryMax = groupQueryMax
