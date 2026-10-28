package fyneui

import (
	"fmt"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A volume's usage in a persistent volume claim's or a persistent volume's drawer (D-new): above
// the chart a line that says whose usage it is (a claim holds no data itself; its volume does, and
// the claim's drawer shows it for completeness); the chart shows the bytes in use, filled, and the
// filesystem's size as an outline over them, so a resize is a step and the fill's height against
// the outline reads as the fill level; under it a usage bar of the newest point in the disks' look
// (metricsdisk.go): "Volume", the bar in the table's colours and the share, the numbers in its
// tooltip. Without a newest point, quietly why ("Volume: the kubelet reports no usage …").

// volumeTitle stands before a volume's usage bar.
const volumeTitle = "Volume"

// showIntro shows a volume's line above its chart; nothing for a pod or a node, or before the
// engine named the volume.
func (p *metricsPanel) showIntro(m *engine.ObjectMetrics) {
	text := ""
	if p.volume && m != nil {
		text = volumeIntro(m.Volume, p.ref.Kind)
	}
	if w := p.d.contentWidth(); text != p.intro.text || w != p.intro.width {
		p.intro.text, p.intro.width = text, w
		p.intro.Refresh()
	}
	if text == "" {
		p.intro.Hide()
	} else {
		p.intro.Show()
	}
}

// volumeIntro says whose usage a volume's chart shows: in a claim's drawer its volume's, in a
// volume's drawer its own, found by its claim.
func volumeIntro(v *model.VolumeClaim, kind string) string {
	switch {
	case v == nil:
		return ""
	case kind == "persistentvolumes":
		return "The usage of this volume, as the kubelet reports it for its claim " + v.Namespace + "/" + v.Claim + "."
	}
	return "A claim holds no data itself: this is the usage of its volume " + v.Volume + ", as the kubelet reports it."
}

// showVolume shows a volume's usage bar of its newest point, or why there is none; nothing before
// the engine named the volume (not bound: the section's caption says so).
func (p *metricsPanel) showVolume(m *engine.ObjectMetrics) {
	b := p.disk
	b.title = volumeTitle
	switch {
	case m != nil && m.Volume != nil && m.Fill != nil:
		f, name := *m.Fill, m.Volume.Volume
		b.tip = func(model.NodeFilesystem) string { return volumeTip(name, f) }
		b.set([]model.NodeFilesystem{{UsedBytes: int64(f.UsageBytes), SizeBytes: int64(f.CapacityBytes)}}, "")
	case m != nil && m.Volume != nil && m.VolumeNote != "":
		b.set(nil, m.VolumeNote)
	default:
		b.Hide()
		return
	}
	b.Show()
}

// volumeTip is a volume's usage bar's tooltip: the volume, the bytes in use of its size, and what is
// still free for writing as the kubelet reports it (a filesystem's reserved blocks are neither).
func volumeTip(name string, f model.FilesystemSample) string {
	return name + "\n" +
		fmt.Sprintf("Used %s of %s (%s)", model.FormatBytes(int64(f.UsageBytes)), model.FormatBytes(int64(f.CapacityBytes)), model.FormatPercent(f.Share())) +
		"\nAvailable " + model.FormatBytes(int64(f.AvailableBytes))
}

// volumeSeries is a volume's chart: the bytes in use, and the filesystem's size as an outline.
func volumeSeries(samples []model.FilesystemSample) ([]chartSeries, func(float64) string, func(float64) string) {
	used := chartSeries{Name: seriesUsage, Color: colorAccent}
	size := chartSeries{Name: seriesCapacity, Color: colorMuted, Outline: true}
	for _, sm := range samples {
		used.Points = append(used.Points, chartPoint{At: sm.At, Value: sm.UsageBytes})
		if sm.CapacityBytes > 0 {
			size.Points = append(size.Points, chartPoint{At: sm.At, Value: sm.CapacityBytes})
		}
	}
	axis, detail := variantFormats(variantFilesystem, "")
	return []chartSeries{used, size}, axis, detail
}
