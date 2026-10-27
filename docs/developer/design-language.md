# Design language: Dark Cockpit

Cogpit's look ([D-110](decisions.md#d-110-dark-cockpit-cogpits-design-language)). As in an
aircraft's dark cockpit, nothing is lit while every system is normal; only a deviation lights up.
The window is a deep navy stage: what is healthy stays quiet, what needs the operator glows, and
life comes from the cluster (a pulse, a changed row, a firing alert, the agent at work), not from
decoration. Tokens are in [`theme.go`](../../internal/ui/fyneui/theme.go), the shapes in
[`glow.go`](../../internal/ui/fyneui/glow.go); how to add a colour:
[conventions › UI](conventions.md#ui).

## Principles

1. **Quiet when healthy, lit where it matters.** Healthy rows, running pods and ready conditions
   draw no extra colour. A failing row gets a status spine, a destructive dialog a coral rim, the
   object you work on the glow blue. At most one glow per region.
2. **A stage and plates, not boxes.** Surfaces part by luminance (the chrome darker than the stage,
   the drawer and dialogs a step lighter), with hairlines only where luminance is not enough.
3. **Three lights, one meaning each:** glow blue (`theme.ColorNamePrimary`, the logo's C) is
   Cogpit and you: primary actions, focus, selection, links. Cyan (`colorAccent`) is the cluster's
   live data: charts' first series, the pulse, info. Violet (`colorAgent`) is the agent: its chat,
   its runs, its requests; besides the agent it draws only a chart's second series
   (`colorChartSecond`).
4. **Tokens for things you count.** Containers, nodes and integrations are rounded squares; a count
   too large for squares becomes bars as wide as their share (the Overview's pods, more than 40
   nodes).
5. **Brackets for groups.** A group's head is a lowercase label with a middle dot
   ("properties · 14", "integrations · 4 of 5 live") above a hairline with end ticks, in the
   group's light (`bracketLabel`).
6. **The horizon only where there is no data:** the stage light and the perspective grid floor
   stand behind the welcome page and a failed connect's error page, never behind a list, a drawer or
   a dialog, and never under a line of text.
7. **Glow is never the only signal.** Every glowing state also has a word, a glyph or a shape. Motion
   is slow and purposeful and stops under Reduce motion
   ([D-107](decisions.md#d-107-the-navigator-slides-and-reduce-motion)).

## Palette

| Role | Token | Dark | Light |
|---|---|---|---|
| Stage: behind lists and pages | `theme.ColorNameBackground` | `#0d121a` | `#fbfcfd` |
| Chrome: top and status bar | `colorTopBar`, `colorStatusBar` | `#0a0e14` | `#eef1f5` |
| Sidebar | `colorSidebar` | `#0b0f16` | `#f1f3f7` |
| Drawer | `colorDrawer` | `#10161f` | `#f7f8fa` |
| Dialogs, menus, the palette | `theme.ColorNameOverlayBackground` | `#121925` | `#ffffff` |
| Hairline | `theme.ColorNameSeparator` | `#1b2330` | `#e1e6ed` |
| A well's or a card's edge | `colorHairline` | `#263042` | `#cdd4df` |
| A field's outline | `theme.ColorNameInputBorder` | `#3e4f6a` | `#a1acbc` |
| Text | `theme.ColorNameForeground` | `#d5dde8` | `#1a2230` |
| Muted text | `colorMuted` | `#8592a5` | `#5f6a7c` |
| Glow blue | `theme.ColorNamePrimary` | `#4aa8ff` | `#0a6fd6` |
| Cyan | `colorAccent` | `#5ad1e3` | `#0b7c93` |
| Violet | `colorAgent` | `#a48bff` | `#6d4ee8` |
| Selection fill | `colorRowSelected` | `#16293f` | `#e0ebfb` |

The selection fill is set in [`chrome_tones.go`](../../internal/ui/fyneui/chrome_tones.go)'s
`init`, over `theme.go`'s value.

| Status token | Dark | Light |
|---|---|---|
| OK: mint | `#3dd68c` | `#138a52` |
| Succeeded, Ephemeral: blue | `#5ab8ff` | `#0a6fd6` |
| Pending: amber | `#f2c14e` | `#9a6a00` |
| Warning: orange | `#ff9f5a` | `#c4570e` |
| Error: coral | `#ff6b74` | `#d23b45` |
| Terminating: rose (violet is the agent's) | `#ee82b8` | `#b8417f` |
| Unknown | `#8a95a8` | `#66717f` |
| Stopped | `#9ba5b5` | `#4f5a6b` |

Both palettes pass `TestPaletteContrast` (text 4.5:1 on the stage, 4.3:1 on the large panels,
3.5:1 on the small ones; status tokens but the greys Unknown and Stopped, links and chart lines
3:1; a field's outline 2:1 on every surface a field stands on), `TestStatusColoursAreDistinct`,
`TestChartColoursAreDistinct` and `TestIdentityColorsContrast`. The typeface is Fyne's bundled
font.

## Shapes

[`glow.go`](../../internal/ui/fyneui/glow.go) draws them from vector canvas objects, so that they
cost the GPU and not a raster per frame; the wave is one raster, redrawn only on Refresh.

| Shape | What it draws | Where |
|---|---|---|
| `bracketLabel` | a lowercase label in a light above a hairline at about 55 % of it, with 6-point end ticks; 8 points from the label to the line, 12 to what it introduces; 20 points above a head that follows content (`groupGap`) | drawer sections, dialog heads, settings groups, the Overview, dashboards, the chat |
| `tokenRow`, `tok` | rounded squares, or bars by weight; a healthy (`quiet`) token at about 40 % of its light | the Overview's nodes and pods, the alert board |
| `glowFrame` | three rounded outlines of falling alpha, 0, 2 and 4 points outside the content | a destructive or agent-asked dialog; the hotbar draws the same rings round the shown cluster's tile |
| `wave` | a sine or a series, a crisp core and a faint halo | the agent's thinking, the status bar's pulse |
| `horizon` | the stage light and a perspective grid floor | the welcome page and the failed connect's error page (`newWelcomeStage`) |

## Instruments (cluster Overview, integrations, dashboards)

| Instrument | What it is | Where |
|---|---|---|
| Health header | three bracketed groups in cyan, equal columns on the page's one left edge (`columnsLayout`): nodes as squares that fill their column (at most 28 points; bars by state beyond 40 nodes), pods as bars by state, the integrations' bearing; healthy tokens quiet; the nodes' legend names only the states that are not Ready, the pods' every state with pods | Overview ([`instrument_health.go`](../../internal/ui/fyneui/instrument_health.go)) |
| Bearing | the five integrations (Metrics Server, Prometheus, Alertmanager, Trivy, Kyverno) as 28-point tiles with a glyph and a short name beneath: filled cyan live, an amber outline needs the user, coral failing, dim off or absent; a tooltip says it in words | Overview header, top of Cluster Settings › Integrations ([`instrument_bearing.go`](../../internal/ui/fyneui/instrument_bearing.go), [`instrument_tile.go`](../../internal/ui/fyneui/instrument_tile.go)) |
| Instrument rows | the tile, the name and a pill switch (36 by 20 points, cyan when on) on one line; under it the status (a dot, muted words, coloured only for amber and coral) and what it adds; grouped under `enabled · n` (cyan) and `off · n` (muted) | Cluster Settings › Integrations ([`clustersettings_integrations.go`](../../internal/ui/fyneui/clustersettings_integrations.go), [`instrument_tile.go`](../../internal/ui/fyneui/instrument_tile.go), [`instrument_switch.go`](../../internal/ui/fyneui/instrument_switch.go)) |
| Segmented control | one dark plate with a hairline; the chosen segment on the selection fill with a 2-point glow-blue underline, the other muted and flat; the buttons keep their behaviour, only their theme changes | the Overview's Master / Worker ([`instrument_segment.go`](../../internal/ui/fyneui/instrument_segment.go)) |
| Headroom wave | usage as a crisp line over a wide faint stroke, its area fading to nothing downwards; allocatable and capacity as hairlines at a quarter of their colour; requests and limits as a 12-point tick with their value at the right edge, full width, dashed and named while the pointer is on the legend's "Reference lines" | every chart ([`chart.go`](../../internal/ui/fyneui/chart.go), [`chartpath.go`](../../internal/ui/fyneui/chartpath.go)) |
| Alerts pill | the triangle in the worst severity's colour, then per severity a token in five widths (8 to 24 points) with its count, "99+" beyond 99 | top bar, left of the bell ([`alerts.go`](../../internal/ui/fyneui/alerts.go)) |
| Quiet dashboards | healthy counts and Ready segments muted; a spine and the status colour only on what needs a look; a kind without objects a dim card with a hairline; activity in the text colour with a coloured dot and its age | Flux, Argo CD, Workloads Overview ([`fluxdashboard.go`](../../internal/ui/fyneui/fluxdashboard.go), [`argocd_dashboard.go`](../../internal/ui/fyneui/argocd_dashboard.go), [`overview.go`](../../internal/ui/fyneui/overview.go)) |

The health header's states come from the engine's `ClusterOverviewModel.NodeStates` and
`PodStates`, counted from the cache; the bearing reads the integrations' states through
`cluster.settings.get`. Flux and Argo CD are not in the bearing: they are no `engine.Integration`.

## Lists and window chrome

| Element | Look | Where |
|---|---|---|
| Status spine | a 3-point rounded bar at a row's left edge in its status colour, only for a row that is Pending, Warning, Error, Terminating or Unknown; an error row also gets a 6 % tint (3.5 % in light) | every list ([`list_rows.go`](../../internal/ui/fyneui/list_rows.go)) |
| Status word | healthy: the word in the text colour after a 4-point dot in the status colour; otherwise a pill: the status colour at 14 % (18 % in light), a 6-point dot, the word's lightness moved until it holds 4.5:1 on the pill (`pillInk`). On such a row the warning column stays empty and its reason is the word's tooltip | Status columns ([`chrome_tones.go`](../../internal/ui/fyneui/chrome_tones.go), [`tableview.go`](../../internal/ui/fyneui/tableview.go)) |
| Container squares | 9 points (two thirds of the text size), rounded; running at 40 % of the Running green, stopped hollow at 55 %, warning, error and ephemeral in full colour | Pods' Containers column (`markColor`, [`list_rows.go`](../../internal/ui/fyneui/list_rows.go)) |
| Column headers | muted bold text one point smaller than the rows, a hairline under them and a short divider between the columns, both in `colorBracket`; no band; a column that keeps its width (warning, image grade) draws no divider | every list ([`tableview.go`](../../internal/ui/fyneui/tableview.go)) |
| Links in cells | the text colour at 92 % until the pointer is on them, then `colorLink`, underlined; the drawer's links are always `colorLink` | Namespace, Controlled By, Node cells ([`tableview.go`](../../internal/ui/fyneui/tableview.go)), the drawer ([`links.go`](../../internal/ui/fyneui/links.go)) |
| Row marks | a checked row: the selection fill; the drawer's row: a 2-point glow-blue bar at its left edge, no fill ([D-124](decisions.md#d-124-checked-rows-the-drawers-row-and-the-keyboards-cell)); the navigator's open item: the fill as an inset plate with the bar | lists, navigator ([`chrome_nav.go`](../../internal/ui/fyneui/chrome_nav.go)) |
| Hotbar plates | the identity colour as a vertical gradient from about 22 % to 10 % with an 8 % white inner highlight; the shown cluster's tile in the glow blue's rings; the connection dot as a badge at the corner. The identity colour is the hue its user picked | hotbar ([`chrome_plate.go`](../../internal/ui/fyneui/chrome_plate.go), [`hotbar.go`](../../internal/ui/fyneui/hotbar.go)) |
| Active tab | the stage's colour with a crisp 2-point glow-blue underline over a faint, wider one | tab bar ([`tabbar.go`](../../internal/ui/fyneui/tabbar.go)) |
| Pulse | an 80×14 cyan wave of the last 24 event rates, beside the demo cluster's counter only | status bar ([`chrome_bar.go`](../../internal/ui/fyneui/chrome_bar.go)) |
| Appearance toggle | a sun in the dark theme and a moon in the light (the look a click goes to), at the bell's size, left of the bell; greyed out in a run started with `--theme` | top bar ([`themetoggle.go`](../../internal/ui/fyneui/themetoggle.go), [D-54](decisions.md#d-54-the-top-bar-layout-toggles-the-update-button-the-theme-toggle)) |
| Strip chips, Delete | 28-point chips with a glyph in the accent colour at the ends of the dock's strip: the terminal chip after the tabs (a shell window drawn 18 by 14 of its 24 units, as heavy as the "+"), the create chip at the right end; the selection's Delete a coral outline on a 12 % plate | dock strip, list toolbar ([`stripchip.go`](../../internal/ui/fyneui/stripchip.go), [`terminalbutton.go`](../../internal/ui/fyneui/terminalbutton.go), [`chrome_actions.go`](../../internal/ui/fyneui/chrome_actions.go)) |

## Drawer, dialogs, forms, welcome and agent

| Element | Look | Where |
|---|---|---|
| Drawer header | an eyebrow bracket in cyan with the kind and, for a namespaced object, its namespace ("pod · inventory"); the name bold at 20 points; under it the status badge, a 6-point dot and the word, on a 14 % plate of the status colour when the state needs a look, and then a wash of that colour from 14 % to nothing over the header's top 48 points; section titles as bracket heads with their count | drawer ([`stage_header.go`](../../internal/ui/fyneui/stage_header.go), [`drawer.go`](../../internal/ui/fyneui/drawer.go)) |
| Dialog card | radius 14, the title, an eyebrow bracket as the header rule, plain right-aligned buttons in the footer; a destructive confirmation has a coral rim, a red verb button and, unless `Undoable`, "This cannot be undone." in the footer; an agent's request carries "asked by · agent" in violet and, unless destructive, a violet rim. Confirmations, parameter, create, debug container, Helm install and rollback, data key reviews, the cluster icon designer and Argo CD's actions use it | [`dialogframe.go`](../../internal/ui/fyneui/dialogframe.go), [`confirm.go`](../../internal/ui/fyneui/confirm.go) |
| Forms | a bracket head per group with its count ("general · 5"; a section without groups is one group), an ordinary label, the control 6 points under it, muted help wrapped under that, 24 points to the next setting | Preferences, Cluster Settings ([`preferences_page.go`](../../internal/ui/fyneui/preferences_page.go)) |
| Welcome page | the horizon behind the logo's glow; cluster rows on raised plates with a hairline edge; the grid floor beside the page's column, and under it only below the last line: a soft-edged scrim in the stage's colour lies behind the column and follows the scroll and the page's height | [`welcome.go`](../../internal/ui/fyneui/welcome.go), [`stage_welcome.go`](../../internal/ui/fyneui/stage_welcome.go); the failed connect's page has the same backdrop ([`errorpage.go`](../../internal/ui/fyneui/errorpage.go)) |
| Agent chat | the violet head "agent · <model>"; a violet thinking wave while a reply runs (still under Reduce motion and in tests); tool rows without fill with a 2-point spine in their state's colour (none once finished); the row that waits for the user on a 10 % violet wash; the stop icon in violet | chat ([`chat.go`](../../internal/ui/fyneui/chat.go), [`chat_items.go`](../../internal/ui/fyneui/chat_items.go), [`stage_chat.go`](../../internal/ui/fyneui/stage_chat.go)) |
| About | the logo's glow as the only light, no horizon | [`about.go`](../../internal/ui/fyneui/about.go) |

## What this does not cover

- Dialogs built on Fyne's own dialogs keep Fyne's frame: About, Kubeconfig Sources, Helm
  Repositories, the integrations question, a port forward's, Connection Updates and the MCP token's.
