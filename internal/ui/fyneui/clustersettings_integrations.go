package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's Settings › Integrations (D-76; engine/integrations.go): each integration of the
// cluster switched on or off, and at once whether Cogpit found it automatically or it needs a
// manual configuration (the owner's requirement of 2026-10-17, I1): one row per integration,
// its switch with its label, a status line in the status colours ("Found automatically:
// monitoring/kube-prometheus-alertmanager:9093", "Needs manual configuration: none found by name",
// "Not in this cluster: …", "No permission: …", "Checking…", "Off", "Not connected"), the line of
// what it adds, and a link to the integration's own section (D-87: "Details…", or "Set its
// address…" while Prometheus or Alertmanager needs one). Lens has no such section: the wording and
// the rows are Cogpit's (the lead's decisions, 2026-10-17). A switch dispatches cluster.settings.set's
// integrations at once and makes the choice the cluster's own; the status follows
// EventIntegrations and EventMetrics. The window's question about a new cluster's integrations
// (integrationsask.go) shows the same rows, without the links.
//
// Since the owner's WF22 of 2026-10-18 (D-87) the section is the heading of the integrations in
// the page's list, under a divider below Agents, and each integration has a section of its own
// below it (clustersettings_integration.go) with its switch, its state and its details: Prometheus's
// address and query format (before on Metrics), Alertmanager's address (before under its row here).
// This section keeps what concerns them all: the note while the cluster has no choice, Ask again,
// and the rows to switch them at a glance.

const (
	integrationsSetting = "integrations"
	integrationsLabel   = "Integrations"
	integrationsHelp    = "Tools this cluster runs that Cogpit reads to show more. Off: Cogpit asks the cluster nothing for it and shows nothing of it."
	// integrationsUndecided is the note while the user made no choice for the cluster (D-82).
	integrationsUndecided = "Not chosen for this cluster yet: nothing is looked for until you tick one here or answer its question."
	// integrationsAskAgain is the button that asks the question again, and the line beside it (D-82, WF4).
	integrationsAskAgain     = "Ask again"
	integrationsAskAgainHint = "Shows the question again from its first choice; nothing is read until you answer it."
	// integrationDetailsLink leads from a row to the integration's own section (D-87).
	integrationDetailsLink = "Details…"
)

func init() { clusterSectionLabels[engine.SectionIntegrations] = integrationsLabel }

// integrationRow is one integration's row: its switch with its label, the status line, and the line
// of what it adds; on the settings page a link to the integration's own section.
type integrationRow struct {
	name   engine.Integration
	check  *widget.Check
	status *sourceLine     // plain text, or with the Service's namespace and name as links (placeLinks)
	slot   *fyne.Container // holds the status line, and the link beside it
	link   *linkText       // "Details…" or "Set its address…" to the integration's section; nil elsewhere
	adds   *textLine
	box    *fyne.Container
	state  engine.IntegrationState  // the status shown, for the link and tests
	last   engine.IntegrationStatus // the status as the engine gave it

	// placeLinks makes the place of a found Prometheus or Alertmanager links (serviceSegments); nil:
	// the status line is plain text.
	placeLinks func(text, where string) []lineSeg
}

// newIntegrationRow builds a row; onSwitch receives the user's switch (not the state's).
func newIntegrationRow(in engine.IntegrationInfo, onSwitch func(on bool)) *integrationRow {
	r := &integrationRow{name: in.Name}
	r.check = widget.NewCheck(in.Label, onSwitch)
	r.status = newSourceLine("", colorMuted, theme.SizeNameText)
	r.adds = newTextLine(in.Adds, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	r.adds.truncate = true
	r.slot = container.NewStack(r.status)
	r.box = box(layout.NewCustomPaddedVBoxLayout(0), r.check,
		box(checkIndent{}, box(layout.NewCustomPaddedVBoxLayout(2), r.slot, r.adds)))
	return r
}

// withLink adds the link to the integration's section, after the status line.
func (r *integrationRow) withLink(onTap func()) {
	r.link = newLinkText(integrationDetailsLink, onTap)
	r.link.size = theme.SizeNameCaptionText
	r.slot.Objects = []fyne.CanvasObject{box(textThenLink{}, r.status, r.link)}
	r.slot.Refresh()
}

// needsAddress reports whether a status asks for the address the user sets: Prometheus's and
// Alertmanager's while none is found by name, the one found does not answer, or the one set.
func needsAddress(st engine.IntegrationStatus) bool {
	if st.Name != engine.IntegrationPrometheus && st.Name != engine.IntegrationAlertmanager {
		return false
	}
	switch st.State {
	case engine.IntegrationManual, engine.IntegrationUnavailable, engine.IntegrationConfigured:
		return true
	}
	return false
}

// setStatus shows what Cogpit knows of the integration.
func (r *integrationRow) setStatus(st engine.IntegrationStatus) {
	text, c := integrationStatusLine(st)
	r.state, r.last = st.State, st
	switch {
	case r.placeLinks != nil && st.Where != "" && (st.State == engine.IntegrationFound || st.State == engine.IntegrationConfigured) &&
		(st.Name == engine.IntegrationPrometheus || st.Name == engine.IntegrationAlertmanager):
		r.status.setSegs(r.placeLinks(text, st.Where))
	default:
		r.status.SetText(text)
	}
	r.status.SetColor(c)
	if r.link != nil {
		want := integrationDetailsLink
		if needsAddress(st) {
			want = "Set its address…"
		}
		if r.link.text != want {
			r.link.text = want
			r.link.Refresh()
			r.slot.Refresh()
		}
	}
}

// integrationStatusLine is an integration's status line and its status colour.
func integrationStatusLine(st engine.IntegrationStatus) (string, fyne.ThemeColorName) {
	with := func(prefix, more string) string {
		if more == "" {
			return prefix
		}
		return prefix + ": " + more
	}
	where := func() string {
		if st.Where != "" && st.Detail != "" {
			return st.Where + " (" + st.Detail + ")"
		}
		return st.Where + st.Detail
	}
	switch st.State {
	case engine.IntegrationFound:
		return with("Found automatically", st.Where), colorStatusOK
	case engine.IntegrationConfigured:
		return with("Configured", st.Where), colorStatusOK
	case engine.IntegrationManual:
		return with("Needs manual configuration", st.Detail), colorStatusWarning
	case engine.IntegrationMissing:
		return with("Not in this cluster", st.Detail), colorMuted
	case engine.IntegrationForbidden:
		return with("No permission", where()), colorStatusError
	case engine.IntegrationUnavailable:
		return with("Not answering", where()), colorStatusError
	case engine.IntegrationOff:
		return "Off: nothing is asked of the cluster", colorMuted
	case engine.IntegrationNotConnected:
		return "Not connected: found when the cluster is connected", colorMuted
	}
	return "Checking…", colorStatusPending
}

// openIntegrationSection shows an integration's own section of a context's settings (D-87).
func (p *settingsPage) openIntegrationSection(ctx string, name engine.Integration) {
	p.sh.dispatch("cluster.settings.open", clusterSettingsOpenParams{Context: ctx, Section: engine.IntegrationSection(name)}, nil)
}

// integrationStatuses are the shown context's integrations by name, as the page's view has them;
// none while it loads.
func (p *settingsPage) integrationStatuses() map[engine.Integration]engine.IntegrationStatus {
	out := map[engine.Integration]engine.IntegrationStatus{}
	if p.view != nil {
		for _, st := range p.view.Integrations {
			out[st.Name] = st
		}
	}
	return out
}

// integrationsNote is the note while the shown cluster has no choice of integrations (D-82), shown
// by syncIntegrationsNote.
func (p *settingsPage) integrationsNote() *textLine {
	note := newTextLine(integrationsUndecided, colorStatusPending, theme.SizeNameCaptionText, fyne.TextStyle{})
	note.truncate = true
	p.controls["integrationsNote"] = note
	return note
}

// syncIntegrationsNote shows the note while the shown cluster has no choice of its own in a window
// that asks before it looks (D-82).
func (p *settingsPage) syncIntegrationsNote(note fyne.CanvasObject) {
	if u := p.sh.ui; u.IntegrationsAskFirst && !u.IntegrationsDecided(p.shown.Context) {
		note.Show()
	} else {
		note.Hide()
	}
}

// clusterIntegrationsSection builds a cluster's Settings › Integrations.
func (p *settingsPage) clusterIntegrationsSection() fyne.CanvasObject {
	ctx := p.shown.Context
	help := newTextLine(integrationsHelp, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	help.truncate = true
	note := p.integrationsNote()
	// Ask again (D-82, the owner's WF4): the question from its pre-choice, the cluster's choice
	// forgotten; not on the demo cluster, which is never asked.
	ask := widget.NewButton(integrationsAskAgain, func() {
		p.sh.dispatch("cluster.integrations.ask", map[string]any{contextParam: ctx}, nil)
	})
	askHint := newTextLine(integrationsAskAgainHint, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	askHint.truncate = true
	askRow := container.NewBorder(nil, nil, ask, nil, container.NewVBox(layout.NewSpacer(), askHint, layout.NewSpacer()))
	p.controls["integrationsAsk"], p.controls["integrationsAskRow"] = ask, askRow
	if engine.IsDemoContext(ctx) || (p.sh.ui.Context == ctx && p.sh.ui.Connection.Demo) {
		askRow.Hide()
	}
	rows := make([]*integrationRow, 0, len(engine.Integrations))
	objs := make([]fyne.CanvasObject, 0, 4+len(engine.Integrations))
	objs = append(objs, help, note, askRow, newSpace(theme.SizeNamePadding))
	for _, in := range engine.Integrations {
		name := in.Name
		r := newIntegrationRow(in, func(on bool) {
			p.setCluster(integrationsSetting, map[string]bool{string(name): on}) // nothing while syncing
		})
		r.withLink(func() { p.openIntegrationSection(ctx, name) })
		p.controls["integration:"+string(name)] = r.box
		rows = append(rows, r)
		objs = append(objs, r.box)
	}
	p.integrationRows = rows
	p.sync = func() {
		statuses := p.integrationStatuses()
		for _, r := range rows {
			r.check.SetChecked(p.sh.ui.IntegrationOn(ctx, r.name)) // the UI state's, so the switch follows at once
			if st, ok := statuses[r.name]; ok {
				r.setStatus(st)
			}
		}
		p.syncIntegrationsNote(note)
	}
	return container.NewVBox(p.setting(integrationsSetting, integrationsLabel, "", box(layout.NewCustomPaddedVBoxLayout(8), objs...)))
}

// showsIntegrations reports whether the shown section shows integrations' states: Integrations, an
// integration's own section, or Metrics, which names the two it reads (D-87).
func (p *settingsPage) showsIntegrations() bool {
	if !p.visible() || p.shown.Page != engine.SettingsCluster {
		return false
	}
	if _, ok := engine.IntegrationOfSection(p.shown.Section); ok {
		return true
	}
	return p.shown.Section == engine.SectionIntegrations || p.shown.Section == engine.SectionMetrics
}

// integrationsChanged follows a change of what Cogpit knows of a cluster's integrations
// (EventIntegrations for ctx; "" for any cluster, EventMetrics): the sections that show them and the
// question read them again. Main goroutine.
func (sh *shell) integrationsChanged(ctx string) {
	p := sh.settings
	if p.showsIntegrations() && (ctx == "" || ctx == p.shown.Context) {
		p.requestView()
	}
	sh.intq.statusesChanged(ctx)
}

// checkIndent lays its objects out from where a check's label starts, so that the lines under a
// switch line up with its label (widget.Check's layout: the focus square, the borders, the
// label's own padding).
type checkIndent struct{}

func (checkIndent) offset() float32 {
	th := fyne.CurrentApp().Settings().Theme()
	return th.Size(theme.SizeNameInlineIcon) + th.Size(theme.SizeNameInnerPadding) + 2*th.Size(theme.SizeNameInputBorder)
}

// textThenLink lays a text line out at its natural width (shortened when the room is short) with a
// link right after it, on one line.
type textThenLink struct{}

func (textThenLink) gap() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
}

// textWidthOf is the width a text line or a link takes unshortened.
func textWidthOf(o fyne.CanvasObject) float32 {
	switch t := o.(type) {
	case *textLine:
		return fyne.MeasureText(t.text, t.Theme().Size(t.size), t.style).Width + 1
	case *sourceLine:
		return fyne.MeasureText(t.text, t.Theme().Size(t.size), fyne.TextStyle{}).Width + 1
	case *linkText:
		return fyne.MeasureText(t.text, t.Theme().Size(t.size), fyne.TextStyle{}).Width + 1
	}
	return o.MinSize().Width
}

func (l textThenLink) MinSize(objs []fyne.CanvasObject) fyne.Size {
	ms := objs[0].MinSize()
	if objs[1].Visible() {
		lm := objs[1].MinSize()
		ms = fyne.NewSize(ms.Width+l.gap()+textWidthOf(objs[1]), max(ms.Height, lm.Height))
	}
	return ms
}

func (l textThenLink) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	text, link := objs[0], objs[1]
	lw := float32(0)
	if link.Visible() {
		lw = textWidthOf(link) + l.gap()
	}
	tw := min(size.Width-lw, textWidthOf(text))
	text.Move(fyne.NewPos(0, 0))
	text.Resize(fyne.NewSize(max(tw, 0), size.Height))
	link.Move(fyne.NewPos(max(tw, 0)+l.gap(), 0))
	link.Resize(fyne.NewSize(max(lw-l.gap(), 0), size.Height))
}

func (l checkIndent) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var ms fyne.Size
	for _, o := range objs {
		if o.Visible() {
			ms = ms.Max(o.MinSize())
		}
	}
	return ms.AddWidthHeight(l.offset(), 0)
}

func (l checkIndent) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	x := l.offset()
	for _, o := range objs {
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(max(size.Width-x, 0), size.Height))
	}
}
