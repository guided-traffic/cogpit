package fyneui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The welcome page's cluster rows (welcome.go): the cluster's badge, its title with pills for what
// the user should know before connecting (the kubeconfig's current context, the demo, the source,
// a login program or an unchecked connection waiting for the user's approval, a certificate the
// kubeconfig does not check, the client certificate's time left: D-75, welcome.go), a subtitle,
// and a warning icon after a failed connect that opens the connect's log. The context picker had the same rows until it went (D-57, 2026-10-15); the
// navigator's section lists every cluster in rows of its own (navigator_sections.go).

// demoLabel is how the welcome page and the section's tooltip name the synthetic demo cluster.
const demoLabel = "Demo cluster (synthetic, 10,000 pods)"

// contextLabels returns a context's title and subtitle in a cluster row.
func contextLabels(c engine.ContextInfo) (title, sub string) {
	if c.Demo {
		return demoLabel, "context " + c.Name + " · generated in memory, no credentials needed"
	}
	title = c.Name
	if c.KubeContext != "" { // a kubeconfig source's context: its name in the file, the source as a badge
		title = c.KubeContext
	}
	var parts []string
	if c.Cluster != "" {
		parts = append(parts, c.Cluster)
	}
	if c.Server != "" {
		parts = append(parts, c.Server)
	}
	if c.Namespace != "" {
		parts = append(parts, "namespace "+c.Namespace)
	}
	return kubeconfig.DisplayText(title, maxShownPath), kubeconfig.DisplayText(strings.Join(parts, " · "), maxShownPath)
}

// loginState is what a context's pill says about its login while the user has not allowed it, ""
// once allowed: "login program not allowed yet", "token file changed", "unchecked connection not
// allowed yet" (A28; the "certificate not checked" pill says why).
func loginState(c engine.ContextInfo) string {
	what := "login program"
	switch c.ExecKind {
	case kubeconfig.LoginTokenFile:
		what = "token file"
	case kubeconfig.LoginInsecure:
		what = "unchecked connection"
	}
	switch c.Exec {
	case engine.ExecRequired:
		return what + " not allowed yet"
	case engine.ExecChanged:
		return what + " changed"
	}
	return ""
}

// clusterRow is one cluster on the welcome page: badge, title, pills and a subtitle. A click
// connects at its release, a right-click opens the cluster's menu. The row is no
// fyne.DoubleTappable: Fyne's driver holds every tap on one for the double-click interval, and a
// double click here did what one click does; the page takes a double click's second click as a
// click on a cluster whose connect is under way, which connects nothing more (welcomePage.connect).
type clusterRow struct {
	widget.BaseWidget
	hovered bool
	onTap   func()
	onMenu  func(pos fyne.Position) // the context's menu at pos (window coordinates)
	badge   *clusterBadge           // the cluster's icon (clusteridentity.go)

	bg     *canvas.Rectangle
	title  *textLine
	sub    *textLine
	badges *fyne.Container
	warn   *widget.Button // opens the connect log; present when the last connect failed
	// note is a warning line under the subtitle: a context whose kubeconfig skips the server's
	// certificate check (insecureNote); nil otherwise. A line, not a tooltip: a row's pills and lines
	// say what a tooltip would hide until the pointer rests on it.
	note *textLine
}

func newClusterRow(c engine.ContextInfo, title, sub string, onTap, onWarn func()) *clusterRow {
	r := &clusterRow{onTap: onTap, bg: canvas.NewRectangle(color.Transparent), badge: newClusterBadge()}
	r.title = newTextLine(title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	r.sub = newTextLine(sub, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	r.sub.truncate = true
	r.badges = container.NewHBox()
	if c.Current {
		r.badges.Add(newPill("current", colorAccent, true))
	}
	if c.Demo {
		r.badges.Add(newPill("demo", statusColorName(model.StatusSucceeded), true))
	}
	if c.Source != "" {
		r.badges.Add(newPill(kubeconfig.DisplayText(c.Source, maxShownArg), colorMuted, true))
	}
	if state := loginState(c); state != "" {
		r.badges.Add(newPill(state, colorStatusWarning, true))
	}
	if c.Insecure { // the kubeconfig skips the server's certificate check (engine/tlswarning.go)
		r.badges.Add(newPill("certificate not checked", colorStatusWarning, true))
		r.note = newTextLine(insecureNote(c), colorStatusWarning, theme.SizeNameCaptionText, fyne.TextStyle{})
		r.note.truncate = true
	}
	if c.ConnectProblem { // Lens: a warning icon beside the context opens the log of the connect
		r.warn = widget.NewButtonWithIcon("", theme.NewColoredResource(theme.WarningIcon(), colorStatusWarning), onWarn)
		r.warn.Importance = widget.LowImportance
		r.badges.Add(r.warn)
	}
	r.ExtendBaseWidget(r)
	return r
}

// insecureNote is the warning line of a context whose kubeconfig switches the check of the
// server's certificate off (Cogpit's own text and default, 2026-10-13; Lens documents nothing for
// it). The connect log says the same at length (engine/tlswarning.go).
func insecureNote(c engine.ContextInfo) string {
	server := "its server"
	if c.Server != "" {
		server = kubeconfig.DisplayText(c.Server, maxShownPath)
	}
	return "insecure-skip-tls-verify: the credential goes to whoever answers at " + server
}

// Tapped implements fyne.Tappable.
func (r *clusterRow) Tapped(*fyne.PointEvent) { r.onTap() }

// TappedSecondary implements fyne.SecondaryTappable: the context's menu.
func (r *clusterRow) TappedSecondary(ev *fyne.PointEvent) {
	if r.onMenu != nil {
		r.onMenu(ev.AbsolutePosition)
	}
}

// MouseIn implements desktop.Hoverable.
func (r *clusterRow) MouseIn(*desktop.MouseEvent) { r.hovered = true; r.Refresh() }

// MouseMoved implements desktop.Hoverable.
func (r *clusterRow) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (r *clusterRow) MouseOut() { r.hovered = false; r.Refresh() }

// CreateRenderer implements fyne.Widget.
func (r *clusterRow) CreateRenderer() fyne.WidgetRenderer {
	lines := container.NewVBox(container.NewHBox(r.title, r.badges), r.sub)
	if r.note != nil {
		lines.Add(r.note)
	}
	body := container.NewBorder(nil, nil, container.NewHBox(container.NewCenter(r.badge), newSpace(theme.SizeNamePadding)), nil, lines)
	content := inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, body)
	rr := &clusterRowRenderer{r: r, content: content}
	rr.Refresh()
	return rr
}

type clusterRowRenderer struct {
	r       *clusterRow
	content *fyne.Container
}

func (rr *clusterRowRenderer) Layout(size fyne.Size) {
	rr.r.bg.Resize(size)
	rr.content.Resize(size)
}

func (rr *clusterRowRenderer) MinSize() fyne.Size { return rr.content.MinSize() }

func (rr *clusterRowRenderer) Refresh() {
	th := rr.r.Theme()
	rr.r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	rr.r.bg.FillColor = color.Transparent
	if rr.r.hovered {
		rr.r.bg.FillColor = themeColor(th, theme.ColorNameHover)
	}
	rr.r.bg.Refresh()
}

func (rr *clusterRowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{rr.r.bg, rr.content}
}

func (rr *clusterRowRenderer) Destroy() {}
