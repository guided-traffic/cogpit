# Design language: Dark Cockpit (prototype)

> **Status:** a design prototype on the branch `design-prototype`, built in the night of
> 2026-10-20 at the owner's request ("eine neue Art von UI … darf sich gerne von Lens abheben").
> Not merged; the owner decides in a visual review. Lens parity is not a goal of this branch.

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
under the label and 12 above what it introduces; the groups on a page are 20 points apart. The
create button is a 36 point dark plate with a blue outline, set one menu column in from the right so
that it never covers a row's menu.

The Overview's node and pod states come from the engine's own build of the page
(`ClusterOverviewModel.NodeStates`, `PodStates`), counted from the cache; the bearing reads the
integrations' states through `cluster.settings.get`, which sends no request to the cluster. Flux and
Argo CD are not in the bearing: the engine reports no state for them as integrations.

## What this does not cover yet

- Typography: Fyne's bundled font stays; Inter (OFL) through the theme is a proposal, not built.
- Lens parity checks: this branch does not update the parity checklists.
