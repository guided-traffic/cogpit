package fyneui

import "fyne.io/fyne/v2"

// pulseSamples is how many recent event rates the status bar's pulse draws.
const pulseSamples = 24

// pulse is the status bar's live line: a small cyan wave beside the event rate, drawn from the
// recent rates the bar has seen, or as a gentle sine until there are two. It is one raster that is
// redrawn when a sample arrives, not an animation, so it also stands still under Reduce motion.
type pulse struct {
	w       *wave
	samples []float64
}

func newPulse() *pulse {
	p := &pulse{w: newWave(colorAccent, 1.5, 0.45)}
	p.w.min = fyne.NewSize(80, 14)
	return p
}

// add records a sample and redraws the wave.
func (p *pulse) add(v float64) {
	p.samples = append(p.samples, v)
	if n := len(p.samples); n > pulseSamples {
		p.samples = append(p.samples[:0], p.samples[n-pulseSamples:]...)
	}
	lo, hi := p.samples[0], p.samples[0]
	for _, v := range p.samples {
		lo, hi = min(lo, v), max(hi, v)
	}
	if len(p.samples) < 2 || lo == hi { // nothing moved: a gentle sine, not a dead flat line
		p.w.SetValues(nil)
		return
	}
	p.w.SetValues(p.samples)
}
