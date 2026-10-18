package fyneui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A node's disks in its drawer (G12; WF26, D-90): under the node's chart, a row per real
// filesystem, the root first: "Disk" before the first, the mountpoint, a small usage bar in the
// table's colours (the accent, the warning colour from 80%, the error colour from 90%;
// tablebar.go) and the share. The numbers are in the row's tooltip: the mountpoint, the device
// and the type, the bytes used of the size, the bytes free. Without a disk, quietly why there is
// none ("Disk: … no Prometheus was found"). The filesystems are the Nodes list's Disk column's:
// node-exporter through Prometheus; which count, D-90.

// diskBars is the drawer's disks: the rows, or the note.
type diskBars struct {
	widget.BaseWidget
	sh   *shell
	fs   []model.NodeFilesystem
	note string // why there is no disk, while there are no filesystems

	rowsTop, rowH float32 // where the rows lie, from the last layout: the hover finds its row by them
}

var _ desktop.Hoverable = (*diskBars)(nil)

// diskTitle stands before the first row.
const diskTitle = "Disk"

// Sizes of a row, in units: the narrowest bar drawn, the widest mountpoint before it is cut.
const (
	diskBarMin       = 40
	diskMountMaxPart = 0.4 // of the row's width
)

func newDiskBars() *diskBars {
	b := &diskBars{}
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}

// set shows the filesystems, or the note when there are none.
func (b *diskBars) set(fs []model.NodeFilesystem, note string) {
	b.fs, b.note = fs, note
	b.Refresh()
}

// showDisk shows the node's disks, or why there are none; nothing for a pod or a hidden disk.
func (p *metricsPanel) showDisk(m *engine.ObjectMetrics) {
	b := p.disk
	b.sh = p.d.sh
	var fs []model.NodeFilesystem
	if m != nil && m.Disk != nil {
		fs = diskFilesystems(*m.Disk)
	}
	switch {
	case len(fs) > 0:
		b.set(fs, "")
	case m != nil && m.DiskNote != "":
		b.set(nil, m.DiskNote)
	default:
		b.Hide()
		return
	}
	b.Show()
}

// diskFilesystems are the rows of a node's disk: its filesystems, else the one its summary names
// (a disk read without them).
func diskFilesystems(d model.NodeDisk) []model.NodeFilesystem {
	if len(d.Filesystems) > 0 || d.SizeBytes <= 0 {
		return d.Filesystems
	}
	mp := d.Mountpoint
	if mp == "" {
		mp = "/"
	}
	return []model.NodeFilesystem{{Mountpoint: mp, UsedBytes: d.UsedBytes, SizeBytes: d.SizeBytes}}
}

// diskTip is a filesystem's tooltip: where it is, what it is, the numbers.
func diskTip(f model.NodeFilesystem) string {
	what := f.Mountpoint
	for _, s := range []string{f.Device, f.FSType} {
		if s != "" {
			what += " · " + s
		}
	}
	return what + "\n" + fmt.Sprintf("Used %s of %s (%s)", model.FormatBytes(f.UsedBytes), model.FormatBytes(f.SizeBytes), model.FormatPercent(f.Share())) +
		"\nFree " + model.FormatBytes(f.FreeBytes())
}

// rowAt is the filesystem under a position in the widget; false between and beyond the rows.
func (b *diskBars) rowAt(pos fyne.Position) (model.NodeFilesystem, bool) {
	if b.rowH <= 0 || pos.Y < b.rowsTop {
		return model.NodeFilesystem{}, false
	}
	i := int((pos.Y - b.rowsTop) / b.rowH)
	if i < 0 || i >= len(b.fs) {
		return model.NodeFilesystem{}, false
	}
	return b.fs[i], true
}

// MouseIn implements desktop.Hoverable.
func (b *diskBars) MouseIn(ev *desktop.MouseEvent) { b.MouseMoved(ev) }

// MouseMoved implements desktop.Hoverable: the tooltip of the row under the pointer.
func (b *diskBars) MouseMoved(ev *desktop.MouseEvent) {
	if b.sh == nil {
		return
	}
	f, ok := b.rowAt(ev.Position)
	if !ok {
		b.sh.tip.hideFor(b)
		return
	}
	b.sh.tip.hover(b, diskTip(f), ev.AbsolutePosition)
	b.sh.tip.moved(b, ev.AbsolutePosition)
}

// MouseOut implements desktop.Hoverable.
func (b *diskBars) MouseOut() {
	if b.sh != nil {
		b.sh.tip.hideFor(b)
	}
}

// Hide drops the tooltip with the widget: a hidden widget gets no MouseOut.
func (b *diskBars) Hide() {
	if b.sh != nil {
		b.sh.tip.hideFor(b)
	}
	b.BaseWidget.Hide()
}

// CreateRenderer implements fyne.Widget.
func (b *diskBars) CreateRenderer() fyne.WidgetRenderer {
	r := &diskBarsRenderer{b: b}
	r.Refresh()
	return r
}

// diskRowObjs are a row's objects.
type diskRowObjs struct {
	mount, share *canvas.Text
	track, fill  *canvas.Rectangle
}

type diskBarsRenderer struct {
	b     *diskBars
	title *canvas.Text // "Disk", or the note
	rows  []diskRowObjs
	objs  []fyne.CanvasObject
}

func (r *diskBarsRenderer) Destroy()                     {}
func (r *diskBarsRenderer) Objects() []fyne.CanvasObject { return r.objs }

// sizes are the text size, a row's height and the padding.
func (r *diskBarsRenderer) sizes() (text, rowH, pad float32) {
	th := r.b.Theme()
	text = th.Size(theme.SizeNameCaptionText)
	pad = th.Size(theme.SizeNamePadding)
	h := fyne.MeasureText("Mg", text, fyne.TextStyle{}).Height
	return text, max(h, barHeight(th)) + pad/2, pad
}

func (r *diskBarsRenderer) MinSize() fyne.Size {
	text, rowH, pad := r.sizes()
	if len(r.b.fs) == 0 {
		return fyne.NewSize(chartMinWidth, fyne.MeasureText("Mg", text, fyne.TextStyle{}).Height)
	}
	return fyne.NewSize(chartMinWidth, float32(len(r.b.fs))*rowH-pad/2)
}

// Refresh rebuilds the objects for the filesystems (a poll's, every 60 s) and colours them.
func (r *diskBarsRenderer) Refresh() {
	b := r.b
	th := b.Theme()
	text, _, _ := r.sizes()
	mk := func(s string, c fyne.ThemeColorName) *canvas.Text {
		t := canvas.NewText(s, themeColor(th, c))
		t.TextSize = text
		return t
	}
	r.objs, r.rows = r.objs[:0], r.rows[:0]
	switch {
	case len(b.fs) == 0:
		r.title = mk(diskTitle+": "+b.note, colorMuted)
	default:
		r.title = mk(diskTitle, theme.ColorNameForeground)
	}
	r.objs = append(r.objs, r.title)
	for _, f := range b.fs {
		row := diskRowObjs{
			mount: mk(f.Mountpoint, theme.ColorNameForeground),
			share: mk(model.FormatPercent(f.Share()), theme.ColorNameForeground),
			track: canvas.NewRectangle(themeColor(th, theme.ColorNameSeparator)),
			fill:  canvas.NewRectangle(themeColor(th, barColorName(model.UsageBarStatus(f.Share())))),
		}
		row.share.Alignment = fyne.TextAlignTrailing
		r.rows = append(r.rows, row)
		r.objs = append(r.objs, row.mount, row.track, row.fill, row.share)
	}
	r.Layout(b.Size())
	canvas.Refresh(b)
}

// Layout places the rows: "Disk", the mountpoints in a column as wide as the widest (cut at
// diskMountMaxPart of the width), the bars to the share's slot, the shares right-aligned in it.
func (r *diskBarsRenderer) Layout(size fyne.Size) {
	b := r.b
	text, rowH, pad := r.sizes()
	if r.title == nil {
		return
	}
	ts := r.title.MinSize()
	if len(r.rows) == 0 {
		r.title.Text = ellipsize(diskTitle+": "+b.note, size.Width, text, fyne.TextStyle{})
		r.title.Move(fyne.Position{})
		r.title.Resize(fyne.NewSize(size.Width, ts.Height))
		b.rowH = 0
		return
	}
	th := b.Theme()
	titleW := fyne.MeasureText(diskTitle, text, fyne.TextStyle{}).Width
	mountW := float32(0)
	for _, f := range b.fs {
		mountW = max(mountW, fyne.MeasureText(f.Mountpoint, text, fyne.TextStyle{}).Width)
	}
	mountW = min(mountW, size.Width*diskMountMaxPart)
	slot := fyne.MeasureText(barShareSlot, text, fyne.TextStyle{}).Width
	x := titleW + 2*pad
	barX := x + mountW + 2*pad
	barW := max(size.Width-slot-pad-barX, diskBarMin)
	barH := barHeight(th)
	b.rowsTop, b.rowH = 0, rowH
	r.title.Move(fyne.NewPos(0, (rowH-pad/2-ts.Height)/2))
	r.title.Resize(ts)
	for i, row := range r.rows {
		f := b.fs[i]
		y := float32(i) * rowH
		mid := y + (rowH-pad/2)/2
		row.mount.Text = ellipsize(f.Mountpoint, mountW, text, fyne.TextStyle{})
		ms := row.mount.MinSize()
		row.mount.Move(fyne.NewPos(x, mid-ms.Height/2))
		row.mount.Resize(fyne.NewSize(mountW, ms.Height))
		row.track.CornerRadius = barH / 2
		row.track.Move(fyne.NewPos(barX, mid-barH/2))
		row.track.Resize(fyne.NewSize(barW, barH))
		share := min(max(f.Share(), 0), 1)
		fw := barW * float32(share)
		if share > 0 {
			fw = max(fw, 2) // a share that rounds to nothing still shows it is not zero
		}
		row.fill.CornerRadius = min(barH, fw) / 2
		row.fill.Move(fyne.NewPos(barX, mid-barH/2))
		row.fill.Resize(fyne.NewSize(fw, barH))
		row.fill.Hidden = fw <= 0
		ss := row.share.MinSize()
		row.share.Move(fyne.NewPos(barX+barW+pad, mid-ss.Height/2))
		row.share.Resize(fyne.NewSize(slot, ss.Height))
	}
}
