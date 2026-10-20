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

## What this does not cover yet

- Typography: Fyne's bundled font stays; Inter (OFL) through the theme is a proposal, not built.
- Lens parity checks: this branch does not update the parity checklists.
