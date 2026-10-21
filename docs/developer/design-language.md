# Design language: Dark Cockpit

> **Status:** Cogpit's design language since 2026-10-21 ([D-109](decisions.md#d-109-dark-cockpit-cogpits-design-language)).
> Built in the night of 2026-10-20 on the branch `design-prototype` at the owner's request ("eine
> neue Art von UI … darf sich gerne von Lens abheben"), approved by him the next morning ("Finde ich
> gut gelungen!"). Lens parity is not what this page is about: it says how Cogpit looks.
> Before and after, dark: [assets/design-prototype/](assets/design-prototype/) (`<scene>_before_dark.webp`,
> `<scene>_after_dark.webp`).

Cogpit's name is a cockpit. Aviation designs its cockpits **dark**: when every system is normal, no
light is on; only a deviation lights up. That is the whole idea of this design. The window is a calm,
deep navy stage; what is healthy stays quiet; what needs the operator glows. Life comes from the
cluster itself (a pulse, a changed row, a firing alert, an agent at work), not from decoration.

The language comes from two sources: the **logo** (a navy plate, a glowing blue C over a thin white helm
wheel with seven handles and a hexagon hub) and the owner's **reference image** (a violet wave
labelled "the thinking", cyan rounded tokens of decreasing granularity labelled "the writing", thin
brackets with end ticks, lowercase labels with a middle dot, a faint perspective grid floor).

## Principles

1. **Quiet when healthy, lit where it matters.** Healthy rows, running pods and ready conditions draw
   no extra colour. A failing row gets a status spine; a destructive dialog gets a coral rim; the
   selected object gets the blue. Never more than one glow in a region.
2. **A stage and plates, not boxes.** Surfaces part by luminance (chrome darker than the stage, the
   drawer and dialogs a step lighter), with hairlines only where luminance is not enough.
3. **Three lights, one meaning each.**
   | Light | Token | Means | Used for |
   |---|---|---|---|
   | Glow blue (the logo's C) | `theme.ColorNamePrimary` | Cogpit, you | primary actions, focus, selection, links |
   | Cyan | `colorAccent` | the cluster, "the writing" | live data: charts' first series, the pulse, info |
   | Violet | `colorAgent` | the agent, "the thinking" | the agent chat, its runs, MCP activity — nothing else |
4. **Tokens for things you count.** Containers, pods, replicas, findings and integrations are rounded
   squares (`tokenRow`); a hierarchy coarsens from squares to bars to long bars, as the reference's
   12 squares, 6 bars and 3 long bars do (pod → workload → namespace).
5. **Brackets for groups.** A group's head is a lowercase label with a middle dot ("properties · 14",
   "integrations · 4 of 5 live") above a hairline with end ticks (`bracketLabel`), in the group's light.
6. **The horizon only where there is no data.** The perspective grid and the stage light (`horizon`)
   belong to the welcome page, empty states, About and a connect's wait — never behind a list, a
   drawer or a dialog.
7. **Glow is never the only signal.** Every glowing state also has a word, a glyph or a shape; text is
   never drawn on a glow. Motion is slow and purposeful and stops under Reduce motion.

## Palette

The palettes in [theme.go](../../internal/ui/fyneui/theme.go) replace Solarized (D-22) on this branch.
Dark: stage `#0d121a`, chrome `#0a0e14`, sidebar `#0b0f16`, drawer `#10161f`, overlay `#121925`,
hairline `#1b2330`; text `#d5dde8`, muted `#8592a5` (5.9:1 on the stage); glow blue `#4aa8ff`, cyan
`#5ad1e3`, violet `#a48bff`; status mint `#3dd68c`, amber `#f2c14e`, orange `#ff9f5a`, coral `#ff6b74`,
rose `#ee82b8` (Terminating; violet is the agent's), blue `#5ab8ff` (Succeeded). Light keeps the same
roles on white plates with the lights deepened for 4.5:1 text. Every pair still passes
`TestPaletteContrast`, `TestChartColoursAreDistinct` and `TestIdentityColorsContrast`.

## Shapes

[glow.go](../../internal/ui/fyneui/glow.go) holds the shapes, drawn from vector canvas objects so that
they cost the GPU, not a raster per frame (the wave is one raster, redrawn only on Refresh):

| Shape | What it draws | Where |
|---|---|---|
| `bracketLabel` | lowercase label in a light, a hairline with end ticks | drawer sections, dialog heads, settings groups, Overview |
| `tokenRow` / `tok` | rounded squares, or bars by weight, optionally fading | containers, pod ladders, integrations, findings |
| `glowFrame` | three rounded outlines of falling alpha around content | the active tile, a destructive dialog, the running agent |
| `wave` | a glowing sine or series, crisp core and faint halo | the agent's thinking, the cluster's pulse, sparklines |
| `horizon` | stage light and perspective grid floor | welcome, empty states, About, connecting |

## Instruments (cluster Overview, integrations, dashboards)

Built on the shapes above, in `instrument_*.go` and the views they sit in:

| Instrument | What it is | Where |
|---|---|---|
| Health header | bracketed groups in the cyan, three equal columns on the page's one left edge ([columnsLayout](../../internal/ui/fyneui/instrument_health.go)): nodes as squares that fill their column (28 points at most), pods as bars by state, the integrations' bearing; healthy tokens at about a third of their light, a legend names only the states that are not healthy | Overview ([instrument_health.go](../../internal/ui/fyneui/instrument_health.go)) |
| Bearing | the integrations as the same 28 point tiles as in Settings, spread over the column with the short name beneath: tint and outline by state (cyan live, amber needs the user, coral failing, dim off or absent), a glyph per integration; a tooltip says it in words | Overview header, top of Settings › Integrations ([instrument_bearing.go](../../internal/ui/fyneui/instrument_bearing.go), [instrument_tile.go](../../internal/ui/fyneui/instrument_tile.go)) |
| Instrument rows | the tile, the name and a pill switch on one line; under it the status (a dot, muted words, coloured only for amber and coral) and what it adds; grouped under `enabled · n` and `off · n` | Settings › Integrations ([instrument_tile.go](../../internal/ui/fyneui/instrument_tile.go), [instrument_switch.go](../../internal/ui/fyneui/instrument_switch.go)) |
| Segmented control | one dark plate with a hairline, the chosen segment a step lighter with a 2 point blue underline, the other muted; the buttons keep their behaviour and only their theme changes | Overview's Master / Worker ([instrument_segment.go](../../internal/ui/fyneui/instrument_segment.go)) |
| Headroom wave | usage as a crisp line over a faint wide glow stroke, the area fading to zero downwards; allocatable and capacity as 25 % hairlines, requests and limits as a tick with their value at the right edge, full-width dashed lines with names only while the pointer is on the legend's "Reference lines" | every chart ([chart.go](../../internal/ui/fyneui/chart.go), [chartpath.go](../../internal/ui/fyneui/chartpath.go)) |
| Segmented alerts pill | the triangle in the worst severity's colour, then a token per severity in five widths (8 to 24 points) with the number, "99+" from a hundred; a hairline on the neutral plate | title bar ([alerts.go](../../internal/ui/fyneui/alerts.go)) |
| Quiet dashboards | healthy counts and Ready segments muted, a spine and the status colour only on what needs a look; an empty kind is a hairline card with no bar, a kind without a state has no bar; activity in text colour with a coloured dot and age | Flux, Argo CD, Workloads Overview |

The bracket head (glow.go) draws its line in its own light at about 55 %, with 6 point ticks, 8 points
under the label and 12 above what it introduces; the groups on a page are 20 points apart.

The Overview's node and pod states come from the engine's own build of the page
(`ClusterOverviewModel.NodeStates`, `PodStates`), counted from the cache; the bearing reads the
integrations' states through `cluster.settings.get`, which sends no request to the cluster. Flux and
Argo CD are not in the bearing: the engine reports no state for them as integrations.

## Lists and window chrome

| Element | Look | Where |
|---|---|---|
| Status spine | a 3 point rounded bar at a row's left edge in its status colour, only for rows that are not healthy; an error row also gets a 6 % tint (3.5 % in light) | every list ([list_rows.go](../../internal/ui/fyneui/list_rows.go), [tableview.go](../../internal/ui/fyneui/tableview.go)) |
| Status word | healthy: foreground text with a 4 point dot at 55 %; abnormal: a pill (the status colour at 14 %, 18 % in light, a dot, the word moved in lightness until it holds 4.5:1 on its tint); a reason that was a warning triangle is the word's tooltip | Status columns, the drawer's status and conditions ([chrome_tones.go](../../internal/ui/fyneui/chrome_tones.go)) |
| Container tokens | 9 point rounded squares; running at 40 % of the Running green, stopped hollow, warning, error and ephemeral in full colour | Pods' Containers column |
| Quiet links | names of other objects in the foreground at 92 %, glow blue with an underline only under the pointer; the drawer's property links stay blue | Namespace, Controlled By, Node cells |
| Selection | about 14 % glow blue (dark `#16293f`, light `#e0ebfb`), a 2 point glow-blue bar at the inner left edge, near-white text | rows, navigator, palette ([chrome_nav.go](../../internal/ui/fyneui/chrome_nav.go)) |
| Hotbar plates | the identity colour as a vertical gradient from 22 % to 10 % with a white 8 % inner highlight; the shown cluster's tile in a glow frame; the connection dot as a badge at the corner | hotbar ([chrome_plate.go](../../internal/ui/fyneui/chrome_plate.go)) |
| Tab, pulse | the active tab's 2 point glow-blue underline with a faint wide one; an 80×14 cyan wave of the last 24 event rates in the status bar (the demo cluster's counter only: real clusters have no rate history yet). The cluster's pill in the title bar (a capsule: its dot, name and kubeconfig) is gone since 2026-10-21 at the owner's request: the title bar names no cluster, the status bar and the navigator do | tab bar, status bar ([chrome_bar.go](../../internal/ui/fyneui/chrome_bar.go)) |
| Appearance toggle | a sun in the dark and a moon in the light, at the bell's size, left of the bell; the hover plate as the bell's; greyed out in a run started with `--theme` | title bar ([themetoggle.go](../../internal/ui/fyneui/themetoggle.go), D-54) |
| Create and Delete | Create is a 28 point chip at the right end of the dock's strip and never covers a row; the selection's Delete is a coral outline on a 12 % plate instead of a solid block | list toolbar, dock ([chrome_actions.go](../../internal/ui/fyneui/chrome_actions.go)) |

A cluster's identity colour stays the hue its user picked (the lead's decision, open to revision): a
track of the prototype mapped "red" to a teal so that no tile borrows a status hue, but a red tile is
often a user's own production warning, and that cue outranks the calm.

## Drawer, dialogs, forms, welcome and agent

| Element | Look | Where |
|---|---|---|
| Drawer header | an eyebrow bracket "pod · inventory" in the cyan, the name at 20 points on its own row, the status badge under it, a 48 point wash in the status colour (14 % to 0) only for abnormal states; section titles as bracket heads | drawer ([stage_header.go](../../internal/ui/fyneui/stage_header.go), [drawer.go](../../internal/ui/fyneui/drawer.go)) |
| Dialog family | every framed dialog a card: radius 14, the title, an eyebrow bracket as the header rule, a footer with right-aligned plain buttons; a destructive confirmation in a coral glow frame with "This cannot be undone." in the footer, one asked by an agent in a violet frame with "asked by · agent" | [dialogframe.go](../../internal/ui/fyneui/dialogframe.go), [confirm.go](../../internal/ui/fyneui/confirm.go) |
| Forms | one bracket per real group ("general · 4", "updates · 2"), a plain label, the control, muted help wrapped under it, 24 points between settings; the nav's selection as in lists | Preferences, Cluster Settings |
| Welcome | the horizon behind the logo's stage light, the grid floor only below the last line (a scrim follows the scroll), cluster rows as plates with a hairline | [stage_welcome.go](../../internal/ui/fyneui/stage_welcome.go) |
| Agent chat | the violet header "agent · …", the thinking wave while a reply runs (still under Reduce motion and in tests), tool rows without fill and with a 2 point status spine, the row that waits for the user on a 10 % violet tint, a violet stop | chat ([chat.go](../../internal/ui/fyneui/chat.go), [chat_items.go](../../internal/ui/fyneui/chat_items.go)) |
| About | the logo's glow as the only light | [about.go](../../internal/ui/fyneui/about.go) |

## What this does not cover yet

- Typography: Fyne's bundled font stays; Inter (OFL) through the theme is a proposal, not built.
- Fyne's own CustomDialog dialogs that are not confirm, create or parameter dialogs (Helm, kubeconfig,
  the integrations question) keep Fyne's frame.
- Measured speed: the per-cell objects are reused by Fyne's table, but no 10,000-row scroll run with
  the perf harness has been made on this branch.
- The real window: everything was judged from Fyne's software renderer (offscreen renders), not in
  the GL window on a Mac.
