package fyneui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A volume's usage in a persistent volume claim's or a persistent volume's drawer (D-139): above
// the chart a line that names whose usage it is, the name a link to its drawer ("Display metrics
// from PV: pvc-…" in a claim's, which holds no data itself; "Display metrics from PVC: ns/claim" in a
// volume's, the claim the kubelet reports it by), and under it the pods that mount the claim, each a
// link ("Mounted by pod: web-0", "Mounted by pods: web-0, web-1"); the chart shows the bytes in use, filled, and the
// filesystem's size as an outline over them, so a resize is a step and the fill's height against
// the outline reads as the fill level; under it a usage bar of the newest point in the disks' look
// (metricsdisk.go): "Volume", the bar in the table's colours and the share, the numbers in its
// tooltip. Without a newest point, quietly why ("Volume: the kubelet reports no usage …"). Under
// the bar, muted and wrapped, the engine's note when the storage reports a filesystem clearly
// larger than the volume's size (ObjectMetrics.SizeNote), at the size of the section's other notes
// with a small red mark before it (the owner's QS3: a volume that can fill its node's disk is
// easily missed in muted text).

// volumeTitle stands before a volume's usage bar.
const volumeTitle = "Volume"

// showIntro shows a volume's line above its chart; nothing for a pod or a node, or before the
// engine named the volume.
func (p *metricsPanel) showIntro(m *engine.ObjectMetrics) {
	var v *model.VolumeClaim
	if p.volume && m != nil {
		v = m.Volume
	}
	prefix, link := volumeIntro(v, p.ref.Kind)
	if prefix == "" {
		p.intro.Hide()
		p.pods.Hide()
		return
	}
	ctx, sh := p.ref.Context, p.d.sh
	p.intro.setSegs([]lineSeg{{text: prefix}, {text: link.Key(), onTap: func() { sh.followLink(link, ctx, false) }}})
	p.intro.Show()
	if len(v.Pods) == 0 {
		p.pods.Hide()
		return
	}
	p.pods.setSegs(podSegs(v, func(ref model.ObjectRef) { sh.followLink(ref, ctx, false) }))
	p.pods.Show()
}

// podSegs is the line of the pods that mount a claim: "Mounted by pod: " and its name, or "Mounted
// by pods: " and their names, comma-separated, each a link to the pod.
func podSegs(v *model.VolumeClaim, open func(model.ObjectRef)) []lineSeg {
	segs := []lineSeg{{text: "Mounted by pod: "}}
	if len(v.Pods) > 1 {
		segs[0].text = "Mounted by pods: "
	}
	for i, name := range v.Pods {
		if i > 0 {
			segs = append(segs, lineSeg{text: ", "})
		}
		ref := model.ObjectRef{Kind: kindPods, Namespace: v.Namespace, Name: name}
		segs = append(segs, lineSeg{text: name, onTap: func() { open(ref) }})
	}
	return segs
}

// volumeIntro is the line above a volume's chart: its words, and the object it names as a link. In
// a claim's drawer the volume, whose usage it is; in a volume's drawer the claim, by which the
// kubelet reports it. "" for none.
func volumeIntro(v *model.VolumeClaim, kind string) (string, model.ObjectRef) {
	switch {
	case v == nil:
		return "", model.ObjectRef{}
	case kind == "persistentvolumes":
		return "Display metrics from PVC: ", model.ObjectRef{Kind: "persistentvolumeclaims", Namespace: v.Namespace, Name: v.Claim}
	}
	return "Display metrics from PV: ", model.ObjectRef{Kind: "persistentvolumes", Name: v.Volume}
}

// showVolume shows a volume's usage bar of its newest point, or why there is none; nothing before
// the engine named the volume (not bound: the section's caption says so).
func (p *metricsPanel) showVolume(m *engine.ObjectMetrics) {
	b := p.disk
	b.title = volumeTitle
	note := ""
	switch {
	case m != nil && m.Volume != nil && m.Fill != nil:
		f, name := *m.Fill, m.Volume.Volume
		b.tip = func(model.NodeFilesystem) string { return volumeTip(name, f) }
		b.set([]model.NodeFilesystem{{UsedBytes: int64(f.UsageBytes), SizeBytes: int64(f.CapacityBytes)}}, "")
		note = m.SizeNote
	case m != nil && m.Volume != nil && m.VolumeNote != "":
		b.set(nil, m.VolumeNote)
	default:
		b.Hide()
		p.showSizeNote("")
		return
	}
	b.Show()
	p.showSizeNote(note)
}

// sizeMarkGap is the room between the size note's mark and its text.
const sizeMarkGap = 6

// sizeMarkSide is the side of the size note's mark: the height of a caption's line of text.
func sizeMarkSide() float32 {
	return fyne.MeasureText("Mg", fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameCaptionText), fyne.TextStyle{}).Height
}

// sizeMarkRoom is the width the mark takes from the note's text.
func sizeMarkRoom() float32 { return sizeMarkSide() + sizeMarkGap }

// newSizeNoteRow is the size note with its mark, the error icon in the status error colour, at the
// note's first line.
func newSizeNoteRow(note *paragraph) *fyne.Container {
	mark := canvas.NewImageFromResource(themedIcon(theme.ErrorIcon(), colorStatusError))
	mark.FillMode = canvas.ImageFillContain
	mark.SetMinSize(fyne.NewSquareSize(sizeMarkSide()))
	left := container.New(layout.NewCustomPaddedLayout(0, 0, 0, sizeMarkGap), container.NewVBox(mark))
	return container.NewBorder(nil, nil, left, nil, note)
}

// showSizeNote shows the note under a volume's usage bar, wrapped to the drawer's width less its
// mark, or hides it for none; its text is the note shown, "" while hidden.
func (p *metricsPanel) showSizeNote(text string) {
	n := p.sizeNote
	if text == "" {
		n.text = ""
		n.Hide()
		p.sizeRow.Hide()
		return
	}
	if w := p.d.contentWidth() - sizeMarkRoom(); n.text != text || n.width != w {
		n.text, n.width = text, w
		n.Refresh()
	}
	n.Show()
	p.sizeRow.Show()
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
