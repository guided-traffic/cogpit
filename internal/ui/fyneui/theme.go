package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// ThemeMode picks Cogpit's colour palette.
type ThemeMode string

// Theme modes. The zero value means ThemeDark: Cogpit starts dark, on its night stage.
const (
	ThemeDark   ThemeMode = "dark"
	ThemeLight  ThemeMode = "light"
	ThemeSystem ThemeMode = "system" // follow the operating system's light or dark setting
)

// Cogpit's own colour tokens, beyond Fyne's. Widgets look them up through the theme so that no
// widget decides a colour itself (docs/developer/conventions.md, UI).
const (
	colorTopBar      fyne.ThemeColorName = "cogpit.topBar"
	colorStatusBar   fyne.ThemeColorName = "cogpit.statusBar"
	colorSidebar     fyne.ThemeColorName = "cogpit.sidebar"
	colorRowHover    fyne.ThemeColorName = "cogpit.rowHover"
	colorRowSelected fyne.ThemeColorName = "cogpit.rowSelected"
	// colorContainerRunning is a running container's square in a pod's Containers cell: the
	// Running green Cogpit had before the Solarized theme, which the owner wants there (2026-10-12).
	colorContainerRunning fyne.ThemeColorName = "cogpit.containerRunning"
	colorRowSelectedText  fyne.ThemeColorName = "cogpit.rowSelectedText" // text on a selected row
	colorDrawer           fyne.ThemeColorName = "cogpit.drawer"
	colorDrawerHeader     fyne.ThemeColorName = "cogpit.drawerHeader"
	colorDock             fyne.ThemeColorName = "cogpit.dock"
	colorDockTab          fyne.ThemeColorName = "cogpit.dockTab"
	colorDockTabText      fyne.ThemeColorName = "cogpit.dockTabText" // the active tab's title
	colorDockTabDim       fyne.ThemeColorName = "cogpit.dockTabDim"  // an inactive tab's title
	colorMuted            fyne.ThemeColorName = "cogpit.muted"
	colorAccent           fyne.ThemeColorName = "cogpit.accent"
	colorChartSecond      fyne.ThemeColorName = "cogpit.chartSecond" // a chart's second series (Transmit, Write); the first is the accent
	colorChip             fyne.ThemeColorName = "cogpit.chip"
	colorToast            fyne.ThemeColorName = "cogpit.toast"     // a toast's surface: stronger than the window
	colorToastText        fyne.ThemeColorName = "cogpit.toastText" // the text on it
	colorLink             fyne.ThemeColorName = "cogpit.link"      // a cell or drawer value that names an object
	colorTooltip          fyne.ThemeColorName = "cogpit.tooltip"   // a tooltip's background
	// colorCode is inline code's text on its chip in the agent chat's answers (chat_markdown.go): a
	// warm yellow, as the owner's design direction has it (plan/captures/chat), 4.5:1 on the chip.
	colorCode fyne.ThemeColorName = "cogpit.code"
	// colorToastSelection is selected text on a toast: the theme's selection is too close to the
	// dark toast's surface to be seen (D-99).
	colorToastSelection fyne.ThemeColorName = "cogpit.toastSelection"

	// The design prototype's tokens (docs/developer/design-language.md).
	colorAgent      fyne.ThemeColorName = "cogpit.agent"      // violet, the agent's light: "the thinking"
	colorStageLight fyne.ThemeColorName = "cogpit.stageLight" // the stage's top light
	colorGrid       fyne.ThemeColorName = "cogpit.grid"       // the horizon's grid lines
	colorBracket    fyne.ThemeColorName = "cogpit.bracket"    // a bracket under a group's label
	colorTokenDim   fyne.ThemeColorName = "cogpit.tokenDim"   // an empty token
)

// Status colour tokens, one per model.Status.
const (
	colorStatusOK          fyne.ThemeColorName = "cogpit.status.ok"
	colorStatusSucceeded   fyne.ThemeColorName = "cogpit.status.succeeded"
	colorStatusPending     fyne.ThemeColorName = "cogpit.status.pending"
	colorStatusWarning     fyne.ThemeColorName = "cogpit.status.warning"
	colorStatusError       fyne.ThemeColorName = "cogpit.status.error"
	colorStatusTerminating fyne.ThemeColorName = "cogpit.status.terminating"
	colorStatusUnknown     fyne.ThemeColorName = "cogpit.status.unknown"
	colorStatusStopped     fyne.ThemeColorName = "cogpit.status.stopped"
	colorStatusEphemeral   fyne.ThemeColorName = "cogpit.status.ephemeral"
)

// Cogpit's own size tokens. Fyne's sizes (text, padding) are set for density in [cogTheme.Size].
const (
	sizeSidebarWidth   fyne.ThemeSizeName = "cogpit.sidebarWidth"   // the navigator's width
	sizeHotbarWidth    fyne.ThemeSizeName = "cogpit.hotbarWidth"    // the hotbar's width (hotbar.go)
	sizeHotbarTile     fyne.ThemeSizeName = "cogpit.hotbarTile"     // a hotbar tile's square
	sizeNavBadge       fyne.ThemeSizeName = "cogpit.navBadge"       // the cluster's badge at the navigator's root (navigator.go)
	sizeBarHeight      fyne.ThemeSizeName = "cogpit.barHeight"      // the top bar's height
	sizeStatusHeight   fyne.ThemeSizeName = "cogpit.statusHeight"   // the status bar's height
	sizeStatusText     fyne.ThemeSizeName = "cogpit.statusText"     // the status bar's text
	sizeCellPadding    fyne.ThemeSizeName = "cogpit.cellPadding"    // horizontal padding inside a table cell
	sizeRowPadding     fyne.ThemeSizeName = "cogpit.rowPadding"     // vertical padding inside a table row
	sizeDrawerLabel    fyne.ThemeSizeName = "cogpit.drawerLabel"    // the name column of the drawer's details
	sizeChipRadius     fyne.ThemeSizeName = "cogpit.chipRadius"     // corner radius of chips and pills
	sizeSortIcon       fyne.ThemeSizeName = "cogpit.sortIcon"       // the sort arrow in a column header
	sizeDotDiameter    fyne.ThemeSizeName = "cogpit.dot"            // connection and status dots
	sizeToastWidth     fyne.ThemeSizeName = "cogpit.toastWidth"     // the width of a toast
	sizeFilterWidth    fyne.ThemeSizeName = "cogpit.filterWidth"    // the namespace filter button
	sizeSearchWidth    fyne.ThemeSizeName = "cogpit.searchWidth"    // the search entry
	sizeFindCountWidth fyne.ThemeSizeName = "cogpit.findCountWidth" // the find widget's "1 of 1"
	sizePickerWidth    fyne.ThemeSizeName = "cogpit.pickerWidth"    // the palette's and dialogs' width (the context picker's until D-57)
	sizePickerHeight   fyne.ThemeSizeName = "cogpit.pickerHeight"   // the palette's and dialogs' height
	sizeConfirmWidth   fyne.ThemeSizeName = "cogpit.confirmWidth"   // the confirmation dialog
	sizePopupMaxList   fyne.ThemeSizeName = "cogpit.popupMaxList"   // the tallest list in a popup
	sizeDockTabMaxLen  fyne.ThemeSizeName = "cogpit.dockTabMaxLen"  // the widest dock tab title
	sizeMenuColumn     fyne.ThemeSizeName = "cogpit.menuColumn"     // the ⋮ column at each row's end
	sizeCellIcon       fyne.ThemeSizeName = "cogpit.cellIcon"       // an icon in a table cell (the warning triangle, ⋮)
	sizeTooltipWidth   fyne.ThemeSizeName = "cogpit.tooltipWidth"   // the widest tooltip; longer text wraps
	sizeTooltipOffset  fyne.ThemeSizeName = "cogpit.tooltipOffset"  // a tooltip's distance from the pointer
	sizeDialogWidth    fyne.ThemeSizeName = "cogpit.dialogWidth"    // the create and port-forward dialogs
	sizeDialogPadX     fyne.ThemeSizeName = "cogpit.dialogPadX"     // a framed dialog's sides together, Fyne's dialog padWidth (dialogframe.go)
	sizeDialogPadY     fyne.ThemeSizeName = "cogpit.dialogPadY"     // above its content and below its buttons, Fyne's dialog padHeight
	sizeSliderWidth    fyne.ThemeSizeName = "cogpit.sliderWidth"    // a parameter dialog's slider (paramdialog.go, D-104)
	sizeHintIcon       fyne.ThemeSizeName = "cogpit.hintIcon"       // the "?" after a dialog field's name (D-104)
	sizeAboutWidth     fyne.ThemeSizeName = "cogpit.aboutWidth"     // About Cogpit's column (about.go)
	sizeHealthBar      fyne.ThemeSizeName = "cogpit.healthBar"      // a health bar's height (Workloads Overview)
	sizeLogo           fyne.ThemeSizeName = "cogpit.logo"           // the header's mark (D-24: 20 pixels)
)

// palette is one variant's colours. Since the design prototype (docs/developer/design-language.md,
// "Dark Cockpit") the palettes are Cogpit's own, drawn from the logo and the owner's reference image:
// a deep navy stage whose surfaces step up in luminance instead of carrying borders, and three
// lights on it, each with one meaning: the logo's glow blue is Cogpit itself (primary actions, focus,
// selection, links), cyan is the cluster's live data ("the writing": charts, the pulse, info), violet
// is the agent ("the thinking": the chat, its suggestions and runs). Status hues are spread so that no
// two tokens are confused at a glance; glow is kept for what needs attention.
type palette struct {
	fyne   map[fyne.ThemeColorName]color.Color
	status map[model.Status]color.Color
}

func hex(rgb uint32) color.NRGBA {
	return color.NRGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 0xff}
}

func alpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// The three lights of the dark palette. Status tokens read as text at 3:1 on the view, sidebar,
// hover and header surfaces and 2.5:1 on a selected row (TestPaletteContrast).
const (
	glowBlue   = 0x4aa8ff // the logo's C: primary buttons, focus
	glowCyan   = 0x5ad1e3 // the cluster: charts' first series, the pulse, info
	glowViolet = 0xa48bff // the agent: the chat, its runs and suggestions
)

var darkPalette = palette{
	fyne: map[fyne.ThemeColorName]color.Color{
		theme.ColorNameBackground:          hex(0x0d121a), // the stage: the view behind lists and pages
		theme.ColorNameButton:              hex(0x1a2230),
		theme.ColorNameDisabledButton:      hex(0x121822),
		theme.ColorNameDisabled:            hex(0x4d5869),
		theme.ColorNameError:               hex(0xff6b74),
		theme.ColorNameFocus:               alpha(hex(glowBlue), 0xaa),
		theme.ColorNameForeground:          hex(0xd5dde8), // 13.7:1 on the stage
		theme.ColorNameForegroundOnError:   hex(0x1a0508),
		theme.ColorNameForegroundOnPrimary: hex(0x04111f),
		theme.ColorNameForegroundOnSuccess: hex(0x03140c),
		theme.ColorNameForegroundOnWarning: hex(0x1f0e02),
		theme.ColorNameHeaderBackground:    hex(0x141b26), // a table's header, the hotbar
		theme.ColorNameHover:               alpha(hex(0xffffff), 0x0e),
		theme.ColorNameHyperlink:           hex(0x7cc4ff),
		theme.ColorNameInputBackground:     hex(0x131a25),
		theme.ColorNameInputBorder:         hex(0x263042),
		theme.ColorNameMenuBackground:      hex(0x121925),
		theme.ColorNameOverlayBackground:   hex(0x121925), // dialogs, the palette: a lifted plate
		theme.ColorNamePlaceHolder:         hex(0x6f7b8e),
		theme.ColorNamePressed:             alpha(hex(0xffffff), 0x1a),
		theme.ColorNamePrimary:             hex(glowBlue),
		theme.ColorNameScrollBar:           alpha(hex(0xffffff), 0x33),
		theme.ColorNameScrollBarBackground: alpha(hex(0x000000), 0x00),
		theme.ColorNameSelection:           hex(0x22406a),
		theme.ColorNameSeparator:           hex(0x1b2330), // a hairline: surfaces part by luminance first
		theme.ColorNameShadow:              alpha(hex(0x000000), 0x8c),
		theme.ColorNameSuccess:             hex(0x3dd68c),
		theme.ColorNameWarning:             hex(0xff9f5a),

		colorTopBar:           hex(0x0a0e14), // the chrome recedes: darker than the stage
		colorStatusBar:        hex(0x0a0e14),
		colorSidebar:          hex(0x0b0f16),
		colorRowHover:         hex(0x141b26),
		colorRowSelected:      hex(0x183252), // the glow blue, deep: the selected object
		colorContainerRunning: hex(0x3dd68c), // the Running green (owner, 2026-10-12)
		colorRowSelectedText:  hex(0xf2f7ff),
		colorDrawer:           hex(0x10161f), // in front of the stage, a step lighter
		colorDrawerHeader:     hex(0x151d29),
		colorDock:             hex(0x0b0f16),
		colorDockTab:          hex(0x0d121a),
		colorDockTabText:      hex(0xe3e9f2),
		colorDockTabDim:       hex(0x8d99ab),
		colorMuted:            hex(0x8592a5),
		colorAccent:           hex(glowCyan),
		colorChartSecond:      hex(glowViolet),
		colorChip:             hex(0x1a2331),
		colorToast:            hex(0x182131),
		colorToastText:        hex(0xeef3fa),
		colorToastSelection:   hex(0x2b4a73),
		colorLink:             hex(0x7cc4ff),
		colorTooltip:          hex(0x182131),
		colorCode:             hex(0xf2c14e),

		colorAgent:      hex(glowViolet),
		colorStageLight: hex(0x15233b),              // the stage's top light, a radial gradient's centre
		colorGrid:       alpha(hex(0x5ab8ff), 0x1c), // the horizon's grid lines
		colorBracket:    hex(0x34435c),              // a bracket's line under a group's label
		colorTokenDim:   hex(0x1c2636),              // an empty token's square
	},
	status: map[model.Status]color.Color{
		model.StatusOK:          hex(0x3dd68c), // mint
		model.StatusSucceeded:   hex(0x5ab8ff), // the glow blue: done
		model.StatusPending:     hex(0xf2c14e), // amber
		model.StatusWarning:     hex(0xff9f5a), // orange
		model.StatusError:       hex(0xff6b74), // coral
		model.StatusTerminating: hex(0xee82b8), // rose: going away (violet is the agent's)
		model.StatusUnknown:     hex(0x8a95a8),
		model.StatusStopped:     hex(0x9ba5b5), // a stopped container's square, legible on a selected row too
		model.StatusEphemeral:   hex(0x5ab8ff), // as Succeeded: an ephemeral container's square (D-102)
	},
}

var lightPalette = palette{
	fyne: map[fyne.ThemeColorName]color.Color{
		theme.ColorNameBackground:          hex(0xfbfcfd),
		theme.ColorNameButton:              hex(0xe6eaf0),
		theme.ColorNameDisabledButton:      hex(0xeef1f5),
		theme.ColorNameDisabled:            hex(0xa1aab8),
		theme.ColorNameError:               hex(0xd23b45),
		theme.ColorNameFocus:               alpha(hex(0x0a6fd6), 0x99),
		theme.ColorNameForeground:          hex(0x1a2230), // 15.5:1 on the view
		theme.ColorNameForegroundOnError:   hex(0xffffff),
		theme.ColorNameForegroundOnPrimary: hex(0xffffff),
		theme.ColorNameForegroundOnSuccess: hex(0xffffff),
		theme.ColorNameForegroundOnWarning: hex(0xffffff),
		theme.ColorNameHeaderBackground:    hex(0xeef1f6),
		theme.ColorNameHover:               alpha(hex(0x000000), 0x0a),
		theme.ColorNameHyperlink:           hex(0x0a62c2),
		theme.ColorNameInputBackground:     hex(0xf0f3f7),
		theme.ColorNameInputBorder:         hex(0xcdd4df),
		theme.ColorNameMenuBackground:      hex(0xffffff),
		theme.ColorNameOverlayBackground:   hex(0xffffff),
		theme.ColorNamePlaceHolder:         hex(0x697486),
		theme.ColorNamePressed:             alpha(hex(0x000000), 0x14),
		theme.ColorNamePrimary:             hex(0x0a6fd6), // the glow blue, deep enough to carry white text
		theme.ColorNameScrollBar:           alpha(hex(0x000000), 0x40),
		theme.ColorNameScrollBarBackground: alpha(hex(0x000000), 0x00),
		theme.ColorNameSelection:           hex(0xcfe2ff),
		theme.ColorNameSeparator:           hex(0xe1e6ed),
		theme.ColorNameShadow:              alpha(hex(0x0b1a33), 0x2e),
		theme.ColorNameSuccess:             hex(0x138a52),
		theme.ColorNameWarning:             hex(0xc4570e),

		colorTopBar:           hex(0xeef1f5),
		colorStatusBar:        hex(0xeef1f5),
		colorSidebar:          hex(0xf1f3f7),
		colorRowHover:         hex(0xf0f4fa),
		colorRowSelected:      hex(0xd6e6ff),
		colorContainerRunning: hex(0x128a4f), // the Running green, darkened to read on a selected row (owner, 2026-10-16)
		colorRowSelectedText:  hex(0x0b1a33),
		colorDrawer:           hex(0xf7f8fa),
		colorDrawerHeader:     hex(0xeef1f6),
		colorDock:             hex(0xeef1f5),
		colorDockTab:          hex(0xfbfcfd),
		colorDockTabText:      hex(0x1a2230),
		colorDockTabDim:       hex(0x5b6678),
		colorMuted:            hex(0x5f6a7c),
		colorAccent:           hex(0x0b7c93),
		colorChartSecond:      hex(0x6d4ee8),
		colorChip:             hex(0xe6eaf0),
		colorToast:            hex(0xffffff),
		colorToastText:        hex(0x0f1a2a),
		colorToastSelection:   hex(0xcfe2ff),
		colorLink:             hex(0x0a62c2),
		colorTooltip:          hex(0xf4f6f9),
		colorCode:             hex(0x9a4d00),

		colorAgent:      hex(0x6d4ee8),
		colorStageLight: hex(0xe4edfb),
		colorGrid:       alpha(hex(0x0a6fd6), 0x16),
		colorBracket:    hex(0xb9c2cf),
		colorTokenDim:   hex(0xe3e8ef),
	},
	status: map[model.Status]color.Color{
		model.StatusOK:          hex(0x138a52),
		model.StatusSucceeded:   hex(0x0a6fd6),
		model.StatusPending:     hex(0x9a6a00),
		model.StatusWarning:     hex(0xc4570e),
		model.StatusError:       hex(0xd23b45),
		model.StatusTerminating: hex(0xb8417f),
		model.StatusUnknown:     hex(0x66717f),
		model.StatusStopped:     hex(0x4f5a6b),
		model.StatusEphemeral:   hex(0x0a6fd6),
	},
}

// identityColorName is the token of a cluster's icon colour (Cluster settings › General › Pick
// Icon Color): one per accent of the palette, engine.IconColors.
func identityColorName(color string) fyne.ThemeColorName {
	return fyne.ThemeColorName("cogpit.identity." + color)
}

// identityColors are the icon colours per variant: the palette's lights and status hues, so that a
// cluster's initials read at 3:1 on the hotbar, the navigator and the welcome page's list
// (TestIdentityColorsContrast); cyan is each variant's accent.
var identityColors = map[fyne.ThemeVariant]map[string]uint32{
	theme.VariantDark: {"cyan": glowCyan, "blue": 0x5ab8ff, "violet": glowViolet, "magenta": 0xee82b8,
		"red": 0xff6b74, "orange": 0xff9f5a, "yellow": 0xf2c14e, "green": 0x3dd68c},
	theme.VariantLight: {"cyan": 0x0b7c93, "blue": 0x0a6fd6, "violet": 0x6d4ee8, "magenta": 0xb8417f,
		"red": 0xd23b45, "orange": 0xc4570e, "yellow": 0x9a6a00, "green": 0x138a52},
}

// init files each palette's status and identity colours under their theme names, so that Color
// is one lookup.
func init() {
	for _, p := range []*palette{&darkPalette, &lightPalette} {
		for s, name := range statusColorNames {
			p.fyne[name] = p.status[s]
		}
	}
	for v, colors := range identityColors {
		for name, rgb := range colors {
			paletteFor(v).fyne[identityColorName(name)] = hex(rgb)
		}
	}
}

var statusColorNames = map[model.Status]fyne.ThemeColorName{
	model.StatusOK:          colorStatusOK,
	model.StatusSucceeded:   colorStatusSucceeded,
	model.StatusPending:     colorStatusPending,
	model.StatusWarning:     colorStatusWarning,
	model.StatusError:       colorStatusError,
	model.StatusTerminating: colorStatusTerminating,
	model.StatusUnknown:     colorStatusUnknown,
	model.StatusStopped:     colorStatusStopped,
	model.StatusEphemeral:   colorStatusEphemeral,
}

// statusColorName returns the theme colour name of a status token; StatusNone and unknown tokens
// map to the foreground, so that an unexpected token never hides text.
func statusColorName(s model.Status) fyne.ThemeColorName {
	if n, ok := statusColorNames[s]; ok {
		return n
	}
	return theme.ColorNameForeground
}

// StatusColor returns the colour of a status token in the given variant: ok green, succeeded teal,
// pending amber, warning orange, error red, terminating violet, unknown grey. StatusNone returns
// the variant's foreground colour.
func StatusColor(s model.Status, v fyne.ThemeVariant) color.Color {
	p := paletteFor(v)
	if c, ok := p.status[s]; ok {
		return c
	}
	return p.fyne[theme.ColorNameForeground]
}

func paletteFor(v fyne.ThemeVariant) *palette {
	if v == theme.VariantLight {
		return &lightPalette
	}
	return &darkPalette
}

// cogTheme is Cogpit's fyne.Theme: its own palettes and a denser size set on top of Fyne's fonts
// and icons.
type cogTheme struct {
	mode ThemeMode
	base fyne.Theme
	// rowHeight is the Preferences' table row height in points (0: the default); rowPadding is
	// the padding above and below a row's text that gives it.
	rowHeight  int
	rowPadding float32
}

var _ fyne.Theme = (*cogTheme)(nil)

// newTheme returns the theme for a mode; the zero mode is dark.
func newTheme(mode ThemeMode) *cogTheme {
	if mode == "" {
		mode = ThemeDark
	}
	return &cogTheme{mode: mode, base: theme.DefaultTheme()}
}

// newThemeFor returns the theme for a mode with the Preferences' table row height (0: the
// default). It measures text, so an app must exist.
func newThemeFor(mode ThemeMode, rowHeight int) *cogTheme {
	t := newTheme(mode)
	if rowHeight > 0 {
		t.rowHeight = rowHeight
		t.rowPadding = max(1, (float32(rowHeight)-rowTextHeight())/2)
	}
	return t
}

// defaultRowPadding is the padding above and below a table row's text without a preference.
const defaultRowPadding = 6

// rowTextHeight is the height of a table row's text, the views' text size.
func rowTextHeight() float32 {
	return fyne.MeasureText("Mg", textSize, fyne.TextStyle{}).Height
}

// defaultRowHeight is a table row's height without a preference, rounded to whole points.
func defaultRowHeight() int { return int(rowTextHeight() + 2*defaultRowPadding + 0.5) }

// textSize is the views' text size, a step smaller than Fyne's default (Size).
const textSize = 13

// variant resolves the mode against the variant Fyne asks for: only ThemeSystem follows it.
func (t *cogTheme) variant(v fyne.ThemeVariant) fyne.ThemeVariant {
	switch t.mode {
	case ThemeLight:
		return theme.VariantLight
	case ThemeSystem:
		return v
	}
	return theme.VariantDark
}

// Color implements fyne.Theme.
func (t *cogTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	v = t.variant(v)
	if c, ok := paletteFor(v).fyne[n]; ok {
		return c
	}
	if c, ok := customIdentityColor(n, v); ok { // a freely picked cluster colour (A93, identitycolor.go)
		return c
	}
	return t.base.Color(n, v)
}

// Font implements fyne.Theme.
func (t *cogTheme) Font(s fyne.TextStyle) fyne.Resource { return t.base.Font(s) }

// iconChevronDown is Cogpit's own open-branch chevron, matching Fyne's closed-branch chevron;
// Fyne's tree draws an arrow (MoveDown) there, which reads as "move", not "expanded".
var iconChevronDown = fyne.NewStaticResource("cogpit-chevron-down.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
		`<path d="M7.41 8.59 12 13.17l4.59-4.58L18 10l-6 6-6-6z"/></svg>`))

// Icon implements fyne.Theme.
func (t *cogTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	if n == theme.IconNameMoveDown {
		return theme.NewThemedResource(iconChevronDown)
	}
	return t.base.Icon(n)
}

// Size implements fyne.Theme. Text is a step smaller than Fyne's default and paddings are tighter:
// a Kubernetes IDE shows many rows, and Lens is compact too.
func (t *cogTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return textSize
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameHeadingText:
		return 20
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameInnerPadding:
		return 6
	case theme.SizeNameLineSpacing:
		return 3
	case theme.SizeNameInlineIcon:
		return 16
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameScrollBarSmall:
		return 3
	case theme.SizeNameInputRadius, theme.SizeNameButtonRadius:
		return 7
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameDialogRadius:
		return 14
	case theme.SizeNameSplitThickness:
		return 4

	case sizeSidebarWidth:
		return 216
	case sizeHotbarWidth:
		return 64
	case sizeHotbarTile:
		return 38
	case sizeNavBadge:
		return 28
	case sizeBarHeight:
		return 40
	case sizeStatusHeight:
		// VS Code's status bar as the owner sees it beside Cogpit (2026-10-14: 26 was too flat).
		return 30
	case sizeStatusText:
		// The text size of the views, as VS Code's status bar reads (owner, 2026-10-12: the
		// caption size, 11, was too small).
		return 13
	case sizeCellPadding:
		return 10
	case sizeRowPadding:
		if t.rowPadding > 0 {
			return t.rowPadding
		}
		return defaultRowPadding
	case sizeDrawerLabel:
		return 128
	case sizeChipRadius:
		return 9
	case sizeSortIcon:
		return 14
	case sizeDotDiameter:
		return 8
	case sizeToastWidth:
		return 400
	case sizeFilterWidth:
		return 190
	case sizeSearchWidth:
		return 240
	case sizeFindCountWidth:
		return 80
	case sizePickerWidth:
		return 600
	case sizePickerHeight:
		return 460
	case sizeConfirmWidth:
		return 500
	case sizePopupMaxList:
		return 320
	case sizeDockTabMaxLen:
		return 220
	case sizeMenuColumn:
		return 30
	case sizeCellIcon:
		return 14
	case sizeTooltipWidth:
		return 440
	case sizeTooltipOffset:
		return 14
	case sizeDialogWidth:
		return 420
	case sizeDialogPadX:
		return 32 // Fyne's dialog.padWidth (v2.8.2), so that a framed dialog looks like Fyne's own
	case sizeDialogPadY:
		return 16 // Fyne's dialog.padHeight (v2.8.2)
	case sizeSliderWidth:
		return 180 // 30 steps of 6 points: each count can be hit with the pointer
	case sizeHintIcon:
		return 14
	case sizeAboutWidth:
		return 300
	case sizeHealthBar:
		return 6
	case sizeLogo:
		return 20
	}
	return t.base.Size(n)
}
