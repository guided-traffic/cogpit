package fyneui

import (
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's Settings page (G13, ⌘⇧,; engine/clustersettings.go), as Lens's (captured
// 2026-10-11): General (the cluster's name, the icon with its "…" menu of Upload Icon, Pick Icon
// Color and Reset Icon, the kubeconfig's path, whole and selectable since the owner's WF28 of
// 2026-10-18, with a button to its folder), Proxy (the proxy of the cluster's API traffic,
// clustersettings_proxy.go), Terminal (the working directory, "$HOME" when unset, and kubectl's
// namespace, "default" when unset), Namespaces (the accessible namespaces,
// clustersettings_namespaces.go), Metrics (the two metrics integrations it reads and the hidden
// metrics, clustersettings_metrics.go).
// Node Shell (the node shells' image and pull secret, clustersettings_nodeshell.go, 2026-10-13).
// Under a divider, Cogpit's own integrations (D-76, D-87): Integrations, every integration's switch
// and what Cogpit found of it (clustersettings_integrations.go), then one section per integration
// with its switch and its details (clustersettings_integration.go, the list clustersettings_nav.go).
// Lens's Lens Metrics page, which installs a metrics stack into the cluster, has no counterpart:
// Cogpit installs nothing for its own features and reads what the cluster runs (D-61, owner
// 2026-10-16); its Cost page is not built and not listed. Cogpit's own: the row of
// colour swatches under the icon, which picks a colour in one click (the menu's Pick Icon Color
// does the same); since 2026-10-17 the cluster's accent colour below the icon (D-80,
// clustersettings_accent.go) and copy icons by the name and the kubeconfig's path (F27,
// clustersettings_copy.go).

// clusterSectionLabels are the page's section titles, Lens's (captured).
var clusterSectionLabels = map[string]string{engine.SectionGeneral: "General", engine.SectionProxy: proxySectionLabel,
	engine.SectionTerminal: "Terminal", engine.SectionNamespaces: namespacesSectionLabel, engine.SectionMetrics: "Metrics"}

// contextParam is the context parameter of the cluster settings commands.
const contextParam = "context"

// settingsItem is "Settings…" in a cluster's menus (the navigator's section rows and root, the
// hotbar's tiles, the welcome page's rows): its Settings page. Nil when the build has no cluster settings or no context.
func (sh *shell) settingsItem(context string) *fyne.MenuItem {
	if _, ok := sh.eng.Registry().Get("cluster.settings.open"); !ok || context == "" {
		return nil
	}
	return fyne.NewMenuItem("Settings…", func() { sh.openClusterSettings(context) })
}

// openClusterSettings opens a context's Settings page.
func (sh *shell) openClusterSettings(context string) {
	sh.dispatch("cluster.settings.open", clusterSettingsOpenParams{Context: context}, nil)
}

// settingsMenuItems are the Cluster menu's "Cluster Settings…" (⌘⇧,, the connected cluster's) and
// "Preferences…" (⌘,), each with the registry's shortcut, so that they work from any focus. On
// macOS Fyne moves "Preferences…" into the application menu, where Lens has it.
func (sh *shell) settingsMenuItems() []*fyne.MenuItem {
	var items []*fyne.MenuItem
	if _, ok := sh.eng.Registry().Get("cluster.settings.open"); ok {
		item := sh.withShortcut(fyne.NewMenuItem("Cluster Settings…", func() { sh.openClusterSettings("") }), "cluster.settings.open")
		item.Disabled = sh.ui.Context == ""
		items = append(items, item)
	}
	if _, ok := sh.eng.Registry().Get("preferences.open"); ok {
		items = append(items, sh.withShortcut(fyne.NewMenuItem("Preferences…", func() {
			sh.dispatch("preferences.open", preferencesOpenParams{}, nil)
		}), "preferences.open"))
	}
	return items
}

// requestView reads the shown context's settings (cluster.settings.get).
func (p *settingsPage) requestView() {
	p.viewSeq++
	seq, ctx := p.viewSeq, p.shown.Context
	p.sh.dispatch("cluster.settings.get", clusterSettingsContext{Context: ctx}, func(res any, err error) {
		if seq != p.viewSeq || p.shown.Context != ctx || !p.visible() {
			return
		}
		var v engine.ClusterSettingsView
		if err != nil || decode(res, &v) != nil {
			return // the page keeps what it shows; the next change reads it again
		}
		p.view = &v
		p.syncControls()
	})
}

// clusterView is the shown context's settings; empty while they load.
func (p *settingsPage) clusterView() engine.ClusterSettingsView {
	if p.view != nil {
		return *p.view
	}
	return engine.ClusterSettingsView{Context: p.shown.Context, Name: engine.ContextLabel(p.shown.Context)}
}

// setCluster dispatches cluster.settings.set for one setting of the shown context.
func (p *settingsPage) setCluster(name string, value any) {
	if p.syncing {
		return
	}
	params := map[string]any{contextParam: p.shown.Context, name: value}
	p.sh.dispatchInOrder("cluster.settings.set", params, p.settled(name))
}

// clusterSection builds one section of a cluster's Settings page.
func (p *settingsPage) clusterSection(section string) fyne.CanvasObject {
	switch section {
	case engine.SectionProxy:
		return p.clusterProxySection() // clustersettings_proxy.go
	case engine.SectionTerminal:
		return p.clusterTerminalSection()
	case engine.SectionNamespaces:
		return p.clusterNamespacesSection() // clustersettings_namespaces.go
	case engine.SectionIntegrations:
		return p.clusterIntegrationsSection() // clustersettings_integrations.go
	case engine.SectionMetrics:
		return p.clusterMetricsSection()
	case engine.SectionAgents:
		return p.clusterAgentsSection() // clustersettings_agents.go
	case engine.SectionNodeShell:
		return p.clusterNodeShellSection() // clustersettings_nodeshell.go
	}
	if in, ok := engine.IntegrationOfSection(section); ok {
		return p.clusterIntegrationSection(in) // clustersettings_integration.go
	}
	return p.clusterGeneralSection()
}

func (p *settingsPage) clusterGeneralSection() fyne.CanvasObject {
	ctx := p.shown.Context
	name := widget.NewEntry()
	name.OnChanged = func(string) { p.typed("name", func() { p.setCluster("name", strings.TrimSpace(name.Text)) }) }

	preview := newClusterBadge()
	preview.side = sizeHotbarTile
	preview.setIdentity(p.sh.identity(ctx))
	p.controls["iconPreview"] = preview
	more := widget.NewButtonWithIcon("", theme.MoreHorizontalIcon(), nil)
	more.Importance = widget.LowImportance
	more.OnTapped = func() {
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(more)
		p.iconMenu(pos.AddXY(0, more.Size().Height))
	}
	p.controls["iconMenuButton"] = more
	swatches := container.NewHBox()
	for _, c := range engine.IconColors {
		col := c
		swatches.Add(newSwatch(p.sh, col, func() { p.setCluster("iconColor", col) }))
	}
	p.controls["swatches"] = swatches
	// A colour picked freely (A93): its swatch while one is set, and the picker beside the eight.
	custom := newSwatch(p.sh, "#000000", func() { p.pickCustomColor() })
	custom.Hide()
	p.controls["customSwatch"] = custom
	pickColor := newTipButton(p.sh, theme.ColorPaletteIcon(), "Pick any colour", func() { p.pickCustomColor() })
	p.controls["customColor"] = pickColor
	swatches.Add(custom)
	swatches.Add(pickColor)
	icon := container.NewHBox(preview, container.NewCenter(more), newHGap(theme.SizeNameInnerPadding), container.NewCenter(swatches))

	// The path whole, wrapped at any character and selectable (the owner's WF28: a drag, ⌘A and ⌘C,
	// the copy shortcut putting the selection on the clipboard as it is); a sentence where the
	// context has no file of its own.
	path := newSecretText("", nil, func(sel string) { p.sh.copyText(sel, false) })
	p.controls["kubeconfigPath"] = path
	sentence := newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	sentence.truncate = true
	pathBg := newSurface(theme.ColorNameInputBackground)
	pathBg.radius, pathBg.stroke = theme.SizeNameInputRadius, colorHairline
	open := newTipButton(p.sh, theme.FolderOpenIcon(), "Show the kubeconfig's folder in the file manager", nil)
	open.OnTapped = func() {
		if f := p.clusterView().Kubeconfig; f != "" {
			p.openFolder(filepath.Dir(f))
		}
	}
	p.controls["openFolder"] = open
	// The copy icons of the name and the path (F27, clustersettings_copy.go).
	nameCopy := p.settingCopyIcon("nameCopy", "Copy the cluster name", func() string { return p.shownClusterName(name) })
	pathCopy := p.settingCopyIcon("kubeconfigCopy", "Copy the kubeconfig's path", func() string { return p.clusterView().Kubeconfig })
	kc := container.NewBorder(nil, nil, nil, container.NewHBox(container.NewCenter(pathCopy), open),
		container.NewStack(pathBg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewStack(path, sentence))))
	accent, syncAccent := p.accentColorSetting() // clustersettings_accent.go
	login, syncLogin := p.clusterLoginSetting()  // clustersettings_login.go

	p.sync = func() {
		syncLogin()
		syncAccent()
		v := p.clusterView()
		name.SetPlaceHolder(v.ContextName)
		set := ""
		if v.NameSet {
			set = v.Name
		}
		p.setEntry(name, name, "name", set)
		preview.setIdentity(p.sh.identity(ctx))
		cs := p.sh.ui.ClusterSettings[ctx] // the UI state's, so the swatch follows at once
		for i, o := range swatches.Objects[:len(engine.IconColors)] {
			if s, ok := o.(*swatch); ok {
				s.setSelected(cs.Icon == "" && cs.IconColor == engine.IconColors[i])
			}
		}
		if cs.Icon == "" && engine.IsCustomIconColor(cs.IconColor) {
			custom.color = cs.IconColor
			custom.Show()
			custom.setSelected(true)
			custom.Refresh()
		} else {
			custom.Hide()
		}
		switch {
		case v.Kubeconfig != "": // a path in monospace, whole: what is selected is the real path
			path.setText(v.Kubeconfig)
			open.Enable()
		case engine.IsDemoContext(ctx):
			sentence.SetText("None: the demo cluster is generated in memory.")
			open.Disable()
		default:
			sentence.SetText("Merged from the default kubeconfig files ($KUBECONFIG, else ~/.kube/config).")
			open.Disable()
		}
		if v.Kubeconfig != "" { // a sentence in the box is no path to copy
			path.Show()
			sentence.Hide()
			pathCopy.Show()
		} else {
			path.Hide()
			sentence.Show()
			pathCopy.Hide()
		}
		kc.Refresh() // the path's lines as its last layout wrapped them
	}
	nameSetting := p.setting("name", "Cluster name", "Shown in the hotbar, the navigator and the welcome page; the kubeconfig is not changed.",
		container.NewBorder(nil, nil, nil, container.NewCenter(nameCopy), name))
	p.controls["name"] = name // the field itself, not the row with its copy icon
	return container.NewVBox(
		nameSetting,
		p.setting("icon", "Icon", "Upload an image (.png, .jpeg, .svg) or pick a colour; Cogpit keeps a copy in its own folder.", icon),
		accent,
		p.setting("kubeconfig", "Kubeconfig", "The file this context comes from.", kc),
		login,
	)
}

// iconMenu is the icon's "…" menu, as captured: Upload Icon, Pick Icon Color, Reset Icon.
func (p *settingsPage) iconMenu(pos fyne.Position) {
	ctx := p.shown.Context
	v := p.clusterView()
	colors := make([]*fyne.MenuItem, 0, len(engine.IconColors))
	for _, c := range engine.IconColors {
		col := c
		item := fyne.NewMenuItem(strings.ToUpper(col[:1])+col[1:], func() { p.setCluster("iconColor", col) })
		item.Checked = v.IconImage == "" && v.IconColor == col
		colors = append(colors, item)
	}
	anyColour := fyne.NewMenuItem("Any Colour…", func() { p.pickCustomColor() }) // A93
	anyColour.Checked = v.IconImage == "" && engine.IsCustomIconColor(v.IconColor)
	colors = append(colors, fyne.NewMenuItemSeparator(), anyColour)
	pick := fyne.NewMenuItem("Pick Icon Color", nil)
	pick.ChildMenu = fyne.NewMenu("", colors...)
	upload := fyne.NewMenuItem("Upload Icon…", func() {
		p.pickIcon(func(path string) {
			p.sh.dispatch("cluster.settings.uploadIcon", clusterIconParams{Context: ctx, Path: path},
				func(_ any, err error) { p.showResult("icon", err) })
		})
	})
	reset := fyne.NewMenuItem("Reset Icon", func() {
		p.sh.dispatch("cluster.settings.resetIcon", clusterSettingsContext{Context: ctx}, func(_ any, err error) { p.showResult("icon", err) })
	})
	reset.Disabled = v.IconImage == "" && v.IconColor == ""
	p.iconMenuModel = fyne.NewMenu("", upload, pick, reset)
	widget.NewPopUpMenu(p.iconMenuModel, p.sh.win.Canvas()).ShowAtPosition(pos)
}

// pickCustomColor opens the colour picker for an icon colour picked freely (A93; pickColor,
// clustersettings_accent.go).
func (p *settingsPage) pickCustomColor() {
	p.pickColor(iconPickerTitle, func(hex string) { p.setCluster("iconColor", hex) })
}

func (p *settingsPage) clusterTerminalSection() fyne.CanvasObject {
	dir := widget.NewEntry()
	dir.SetPlaceHolder("$HOME")
	dir.OnChanged = func(string) {
		p.typed("terminalDir", func() { p.setCluster("terminalDir", strings.TrimSpace(dir.Text)) })
	}
	browse := newTipButton(p.sh, theme.FolderOpenIcon(), "Pick a folder", nil)
	browse.OnTapped = func() {
		p.pickFolder(func(path string) {
			dir.SetText(path) // OnChanged sets it after the pause, as typing does
		})
	}
	ns := widget.NewEntry()
	ns.SetPlaceHolder("default")
	ns.OnChanged = func(string) {
		p.typed("terminalNamespace", func() { p.setCluster("terminalNamespace", strings.TrimSpace(ns.Text)) })
	}
	p.sync = func() {
		v := p.clusterView()
		p.setEntry(dir, dir, "terminalDir", v.TerminalDir)
		p.setEntry(ns, ns, "terminalNamespace", v.TerminalNamespace)
	}
	return container.NewVBox(
		p.setting("terminalDir", "Working directory", "Where this cluster's terminals start: an absolute path, or one starting with $HOME.",
			container.NewBorder(nil, nil, nil, browse, dir)),
		p.setting("terminalNamespace", "Default namespace", "kubectl's namespace in this cluster's terminals; when empty, the namespace filter's single namespace, else the context's.", ns),
	)
}

// clusterMetricsSection builds Metrics: the two integrations it reads, each with its state and a
// link to its own section, and Hide metrics (clustersettings_metrics.go; D-87).
func (p *settingsPage) clusterMetricsSection() fyne.CanvasObject {
	hide, hideSync := p.hideMetricsSetting()
	sources, sourcesSync := p.metricsSourcesSetting()
	p.sync = func() {
		sourcesSync()
		hideSync()
	}
	return container.NewVBox(sources, hide)
}

// metricsChanged follows a change of the metrics' providers: the Prometheus setting, or the one in
// use, may be new.
func (p *settingsPage) metricsChanged() {
	if p.visible() && p.shown.Page == engine.SettingsCluster && p.shown.Section == engine.IntegrationSection(engine.IntegrationPrometheus) &&
		p.view != nil && p.view.Connected {
		p.requestPrometheus()
	}
}

// pickIconWithDialog asks for an image file in Fyne's file dialog: pickIcon's fallback where the
// system has no panel (filepicker_choosers.go).
func (p *settingsPage) pickIconWithDialog(done func(path string)) {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		path := filepath.FromSlash(r.URI().Path()) // a file URI's path has slashes, on Windows too (C:/Users/…)
		_ = r.Close()
		done(path)
	}, p.sh.win)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg", ".svg"}))
	p.sh.showDialog(d)
}

// pickFolderWithDialog asks for a folder in Fyne's dialog: pickFolder's fallback where the
// system has no panel (filepicker_choosers.go).
func (p *settingsPage) pickFolderWithDialog(done func(path string)) {
	p.sh.showDialog(dialog.NewFolderOpen(func(l fyne.ListableURI, err error) {
		if err == nil && l != nil {
			done(filepath.FromSlash(l.Path()))
		}
	}, p.sh.win))
}

// openFolderWithFileManager shows a folder in the file manager (Finder on macOS): the folder that
// holds the kubeconfig, never the file itself, which the system could open with a program.
func (sh *shell) openFolderWithFileManager(dir string) {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		sh.toast("warning", "The folder "+dir+" does not exist.")
		return
	}
	if err := sh.app.OpenURL(folderURL(dir)); err != nil {
		sh.toast("error", "Could not open the folder: "+err.Error())
	}
}

// folderURL is the file URL of the absolute folder dir: file:///Users/me/.kube, and on Windows
// file:///C:/Users/me/.kube. A URL's path takes slashes and begins with one: Windows' backslashes
// would be escaped and the drive read as the host, a URL Explorer does not open.
func folderURL(dir string) *url.URL {
	p := filepath.ToSlash(dir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return &url.URL{Scheme: storage.NewFileURI(dir).Scheme(), Path: p}
}

// swatch is one colour of the icon palette under the icon: a tinted square in the colour, ringed
// while it is the cluster's.
type swatch struct {
	widget.BaseWidget
	sh       *shell
	color    string
	onTap    func()
	selected bool
	hovered  bool
}

var (
	_ fyne.Tappable     = (*swatch)(nil)
	_ desktop.Hoverable = (*swatch)(nil)
)

func newSwatch(sh *shell, color string, tap func()) *swatch {
	s := &swatch{sh: sh, color: color, onTap: tap}
	s.ExtendBaseWidget(s)
	return s
}

func (s *swatch) setSelected(on bool) {
	if on != s.selected {
		s.selected = on
		s.Refresh()
	}
}

// Tapped implements fyne.Tappable.
func (s *swatch) Tapped(*fyne.PointEvent) {
	s.sh.tip.hideFor(s)
	s.onTap()
}

// MouseIn implements desktop.Hoverable.
func (s *swatch) MouseIn(ev *desktop.MouseEvent) {
	s.hovered = true
	s.Refresh()
	s.sh.tip.hover(s, strings.ToUpper(s.color[:1])+s.color[1:], ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (s *swatch) MouseMoved(ev *desktop.MouseEvent) { s.sh.tip.moved(s, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (s *swatch) MouseOut() {
	s.hovered = false
	s.Refresh()
	s.sh.tip.hideFor(s)
}

// CreateRenderer implements fyne.Widget.
func (s *swatch) CreateRenderer() fyne.WidgetRenderer {
	r := &swatchRenderer{s: s, ring: canvas.NewRectangle(color.Transparent), fill: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type swatchRenderer struct {
	s          *swatch
	ring, fill *canvas.Rectangle
}

func (r *swatchRenderer) side() float32 { return r.s.Theme().Size(theme.SizeNameInlineIcon) + 6 }

func (r *swatchRenderer) MinSize() fyne.Size {
	return fyne.NewSize(r.side()+r.s.Theme().Size(theme.SizeNamePadding), r.side())
}

func (r *swatchRenderer) Layout(fyne.Size) {
	side := r.side()
	r.ring.Move(fyne.NewPos(0, 0))
	r.ring.Resize(fyne.NewSize(side, side))
	r.fill.Move(fyne.NewPos(4, 4))
	r.fill.Resize(fyne.NewSize(side-8, side-8))
}

func (r *swatchRenderer) Refresh() {
	th := r.s.Theme()
	c := themeColor(th, identityColorName(r.s.color))
	r.fill.FillColor = c
	r.fill.CornerRadius = th.Size(theme.SizeNameSelectionRadius) / 2
	r.ring.FillColor = color.Transparent
	r.ring.CornerRadius = th.Size(theme.SizeNameInputRadius)
	r.ring.StrokeWidth = 0
	switch {
	case r.s.selected:
		r.ring.StrokeColor, r.ring.StrokeWidth = themeColor(th, theme.ColorNameForeground), 2
	case r.s.hovered:
		r.ring.StrokeColor, r.ring.StrokeWidth = withAlpha(c, 0x99), 2
	}
	r.Layout(r.s.Size())
	r.ring.Refresh()
	r.fill.Refresh()
}

func (r *swatchRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.ring, r.fill} }
func (r *swatchRenderer) Destroy()                     {}
