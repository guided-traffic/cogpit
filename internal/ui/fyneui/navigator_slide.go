package fyneui

import (
	"image"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// The navigator's slides (the owner's requests, 2026-10-20). Drilling into a cluster moves its tree
// in from the right over the section's clusters, which stay where they are; ‹ All Clusters moves
// the tree out to the right again and uncovers them. Showing another cluster while a cluster's tree
// shows (a tab of another cluster with mixed environments on, the hotbar, an agent) slides
// vertically in the order All Clusters lists them: a cluster listed above the one shown comes in
// from above and pushes the old tree out downwards, one listed below comes in from below. Every
// change of the scope slides, an agent's ui.setNavigatorScope and ⌘P's included; the first scope
// the window shows does not, and nothing slides with Preferences › App › Reduce motion or macOS's
// Reduce motion on (shell.reduceMotion, preferences_motion.go). The header does not move: "All Clusters" is its title at either level
// (navigator_header.go).
//
// The tree is one widget, drawn from the shown cluster's state, so the cluster that leaves is a
// picture: taken from the window as it shows when the scope changes (Canvas.Capture reads the frame
// on screen, which the new cluster has not reached yet), cut to the navigator, and moved out beside
// the live tree.
//
// Cogpit's (inferred): a quarter second, easing out; a soft shadow on the tree's left edge while it
// moves sideways; a slide turned back halfway returns from where it is, in its share of the time,
// and a slide of the other direction first ends the one that runs.

const (
	navSlideDuration = 250 * time.Millisecond // a whole slide
	navSlideShade    = 12                     // the shadow's width left of the moving tree, in points
)

// navSlide lays the two levels over each other, the section's clusters below and the cluster's
// tree above, the tree moved right by off of the width and vertically by v of the height, and clips
// them to the navigator.
type navSlide struct {
	s     *sidebar
	box   *fyne.Container
	clip  *container.Clip
	shade *canvas.LinearGradient
	old   *canvas.Image // the cluster that leaves, while a vertical slide runs

	off  float32 // the tree's offset as a share of the width: 0 covers the clusters, 1 is out of sight
	v    float32 // the tree's vertical offset as a share of the height: 0 in place, 1 out of sight
	down float32 // where the vertical slide comes from: -1 above, 1 below

	anim     *fyne.Animation // the slide running, nil when settled
	vertical bool            // the slide running is a change of cluster
	end      func()          // puts the slide running where it goes and runs what follows it
	started  int             // the slides started, for tests
}

func newNavSlide(s *sidebar) *navSlide {
	n := &navSlide{s: s, shade: canvas.NewHorizontalGradient(color.Transparent, color.Transparent), old: &canvas.Image{}}
	n.shade.Hide()
	n.old.Hide()
	n.box = container.New(navSlideLayout{n}, s.sec, n.shade, n.old, s.tree)
	// NewClip leaves ExtendBaseWidget to its first MinSize, which the navigator's layout never asks:
	// without it Resize makes no renderer, and Fyne's focus walk, which enters rendered widgets only,
	// would not find the levels below (a Focus on the tree or the clusters did nothing).
	n.clip = container.NewClip(n.box)
	n.clip.ExtendBaseWidget(n.clip)
	return n
}

// still reports whether the user asked for less motion: everything switches at once.
func (n *navSlide) still() bool { return n.s.sh.reduceMotion() }

// to moves the tree to target (0 over the clusters, 1 out of sight), sliding unless animate is
// false, the user asked for less motion or the navigator has no width yet, and runs done once it
// is there. A sideways slide that runs is turned around from where it is, its own done dropped: the
// new one says what stays shown. A change of cluster that runs ends first.
func (n *navSlide) to(target float32, animate bool, done func()) {
	if n.vertical {
		n.settle()
	}
	n.stop()
	from := n.off
	dist := target - from
	if dist < 0 {
		dist = -dist
	}
	if !animate || n.still() || dist == 0 || n.box.Size().Width <= 0 {
		n.off = target
		n.place(n.box.Size())
		done()
		return
	}
	n.start(time.Duration(float32(navSlideDuration)*dist), false, func(p float32) {
		n.off = from + (target-from)*p
		n.place(n.box.Size())
	}, done)
}

// swap slides the shown cluster's tree in vertically, from above when fromAbove, else from below,
// and the picture of the cluster shown so far out the other way. Whatever slide runs ends first.
// Without a picture of the navigator nothing moves.
func (n *navSlide) swap(fromAbove bool) {
	n.settle()
	size := n.box.Size()
	if n.still() || size.Width <= 0 || size.Height <= 0 || !n.s.tree.Visible() {
		return
	}
	n.down = 1
	if fromAbove {
		n.down = -1
	}
	n.v = 1
	n.place(size) // before the first frame: the new tree out of sight
	n.start(navSlideDuration, true, func(p float32) {
		n.v = 1 - p
		n.place(n.box.Size())
	}, func() {
		n.old.Hide()
		n.old.Image = nil // the picture's pixels go
	})
	if n.anim == nil {
		return // the test driver ran the slide to its end at once: no picture needed
	}
	img := n.picture()
	if img == nil {
		n.settle()
		return
	}
	n.old.Image = img
	n.old.Show()
	n.old.Refresh()
	n.place(size)
}

// picture is the navigator as the window shows it now, in the window's pixels; nil when the window
// cannot be read.
func (n *navSlide) picture() image.Image {
	c := n.s.sh.win.Canvas()
	shot := c.Capture()
	if shot == nil {
		return nil
	}
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(n.box)
	x0, y0 := c.PixelCoordinateForPosition(pos)
	x1, y1 := c.PixelCoordinateForPosition(pos.Add(n.box.Size()))
	r := image.Rect(x0, y0, x1, y1).Intersect(shot.Bounds())
	if r.Empty() {
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			out.Set(x-r.Min.X, y-r.Min.Y, shot.At(x, y))
		}
	}
	return out
}

// start runs a slide: tick places it at p of the way, done follows its end.
func (n *navSlide) start(d time.Duration, vertical bool, tick func(p float32), done func()) {
	var a *fyne.Animation
	a = fyne.NewAnimation(d, func(p float32) {
		if n.anim != a {
			return // stopped in this frame
		}
		tick(p)
		if p >= 1 {
			n.anim, n.end, n.vertical = nil, nil, false
			done()
		}
	})
	a.Curve = fyne.AnimationEaseOut
	n.anim, n.vertical = a, vertical
	n.started++
	n.end = func() { tick(1); done() }
	a.Start() // last: the test driver ticks the whole animation inside it
}

// stop drops the slide that runs where it is, without what would follow it.
func (n *navSlide) stop() {
	if n.anim != nil {
		n.anim.Stop()
	}
	n.anim, n.end, n.vertical = nil, nil, false
}

// settle ends the slide that runs at once: where it goes, with what follows it.
func (n *navSlide) settle() {
	end := n.end
	n.stop()
	if end != nil {
		end()
	}
}

// place lays the levels out at the navigator's size: the clusters at the origin, the tree off to
// the right and up or down by its shares, the leaving cluster's picture beside it, the shadow
// beside its left edge while it moves sideways.
func (n *navSlide) place(size fyne.Size) {
	sec, tree := n.s.sec, n.s.tree
	sec.Move(fyne.Position{})
	sec.Resize(size)
	x, y := n.off*size.Width, n.down*n.v*size.Height
	tree.Move(fyne.NewPos(x, y))
	tree.Resize(size)
	if n.old.Visible() {
		n.old.Move(fyne.NewPos(x, y-n.down*size.Height))
		n.old.Resize(size)
	}
	if n.off <= 0 || n.off >= 1 {
		n.shade.Hide()
		return
	}
	n.shade.EndColor = themeColor(tree.Theme(), theme.ColorNameShadow)
	n.shade.Move(fyne.NewPos(x-navSlideShade, 0))
	n.shade.Resize(fyne.NewSize(navSlideShade, size.Height))
	n.shade.Show()
	n.shade.Refresh()
}

// navSlideLayout is navSlide's layout: its size is the larger level's.
type navSlideLayout struct{ n *navSlide }

func (l navSlideLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) { l.n.place(size) }

func (l navSlideLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return l.n.s.sec.MinSize().Max(l.n.s.tree.MinSize())
}
