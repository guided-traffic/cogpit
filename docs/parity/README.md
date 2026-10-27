# Parity and progress documents

> **Target:** Lens Desktop 2026.10.51501

Cogpit is a clean-room reimplementation of the Kubernetes IDE Lens. These pages answer two questions
and nothing else: **how close are we to Lens**, and **what is left to do**. The method comes from
[PhotoCraft](https://github.com/storytold/photocraft) (a Photoshop reimplementation in Rust).

## The pages

| Page | Answers | Kind |
|---|---|---|
| [`../../ROADMAP.md`](../../ROADMAP.md) | One-page summary: stage, the two headline numbers, every dimension, what's next | summary |
| [`target-app-parity.md`](target-app-parity.md) | The authoritative assessment: numbers, weights, evidence, how to re-measure | assessment |
| [`gaps.md`](gaps.md) | Every open shortfall, ranked: the work list to pick from | work list |
| [`roadmap.md`](roadmap.md) | Milestones with definitions of done, the alpha gate, the current focus | plan |
| [`ui-parity.md`](ui-parity.md) | Layout, navigation, tables, detail panels, dock, shortcuts, menus, preferences | checklist |
| [`resource-parity.md`](resource-parity.md) | Every Kubernetes resource view Lens has, with its columns and actions | checklist |
| [`platform-parity.md`](platform-parity.md) | Operating systems, architectures, packaging, signing, updates, cluster auth methods | checklist |
| [`beyond-lens.md`](beyond-lens.md) | Cogpit's own targets beyond Lens: MCP control of UI and cluster, built-in agent, updates, cache transparency, crash recovery | checklist, not counted in parity numbers |
| [`parity-checklist.md`](parity-checklist.md) | Every row of the four checklists in one list with its status and section, and the counts | generated (`go run ./hack/parity`), never edited |
| [`scorecard.md`](scorecard.md) | The performance and API-load budgets as measured: per budget the target, the values, pass or fail, the date and the machine | generated (`make scorecard`, by hand, not in CI), never edited |

The checklists in the hand-written pages are the only source
([D-15](../developer/decisions.md#d-15-progress-generators)). `go run ./hack/parity`
([`hack/parity`](../../hack/parity/checklist.go)) writes `parity-checklist.md` when it differs, and
`make parity` (CI, `-check`) refuses a page that no longer matches the checklists, so a change of a
row's status or first cell commits the regenerated page with it. `make scorecard`
([`hack/scorecard`](../../hack/scorecard/main.go)) runs the budget tests and benchmarks of
[testing › Performance budgets](../developer/testing.md#performance-budgets) without the race
detector, by hand on a quiet machine; its numbers are those of the date and machine the page names.

## Rules for every progress page

1. **Status vocabulary.** A checklist row is **done** (behaves as in Lens, and a test proves it),
   **partial** (exists, but differs from Lens or is untested) or **missing**. "Exists" is not
   "done": a row without a test is partial at best.
2. **Evidence.** Every non-missing row names its evidence: a code file, a test name, a scorecard id
   or an issue number. A row without evidence is missing.
3. **Sources.** A row about Lens comes from Lens Desktop's documentation or release notes, or from a
   capture of the running Lens Desktop, never from OpenLens or Freelens
   ([D-12](../developer/decisions.md#d-12-parity-sources)). What the documentation leaves open is
   marked *capture needed*.
4. **Measured or estimated.** Every number says which it is. A measured number names how it was
   measured, so the next person can repeat it. Move an estimate only with new evidence.
5. **Hours.** Estimates are agent-hours: one agent working sequentially, including tests, on
   PhotoCraft's calibration (2–9 h per checklist item of UI or tool depth, 8–20 h per subsystem)
   until Cogpit has ~100 merged PRs to calibrate its own.
6. **Current state only.** A cell says what Cogpit does now and how it differs from Lens, tersely:
   no dates, no history, no revision tables (the [documentation rules](../developer/conventions.md#documentation)).
   A closed gap leaves [`gaps.md`](gaps.md).
7. **Same change.** Work that moves a number or closes a gap updates, in the same pull request, the
   checklist row, the gap entry in [`gaps.md`](gaps.md), the number in
   [`target-app-parity.md`](target-app-parity.md) and, when a headline moves,
   [`ROADMAP.md`](../../ROADMAP.md).
8. **Stable headings.** Numbers go in the text under a heading, never in the heading itself, so
   links to a section survive every re-measurement.

## make parity

`make parity` ([`hack/parity`](../../hack/parity/main.go)) reads `ROADMAP.md` and the pages above,
and runs in CI ([releasing › CI](../developer/releasing.md#ci)). It refuses:

- **a row that appears twice:** two rows of one section with the same first cell (links read as
  their text, code marks and case ignored);
- **a status it cannot read:** a checklist row's Status cell starts with `done`, `partial`,
  `missing`, `not a target` or `–` (nothing to build);
- **a row marked done or partial without evidence** in its Status or Evidence cell: a link to a code
  file or a picture (a link to another page of documentation is not evidence), a test name in
  backticks, or an issue (`#12`);
- **a reference that does not resolve:** a link on a progress page to a file that does not exist,
  a test named in a checklist that no `_test.go` file declares;
- **a command id the registry does not know** in a checklist's status or evidence. The first column,
  Actions and MCP tool name the target and may name a planned id; anywhere else an id is a claim
  about the code;
- **a generated page that differs** from what the checklists give (`parity-checklist.md`, under
  `-check`; a plain run writes the page instead);
- **a reported number that differs from the count:** the Feature breadth and Beyond Lens rows of
  `ROADMAP.md`, the headline and the breadth table of [`target-app-parity.md`](target-app-parity.md)
  (fractions, percentages and the weighted sum), and the Summary paragraphs of the four checklists.
  Keep those sentences' form ("55 of 64 resource views exist"): a reworded one fails until
  [`claims.go`](../../hack/parity/claims.go) learns it.

While a recount is due (agents working in parallel move rows, the lead recounts after the merge),
`make parity PARITY_FLAGS=-rows` checks the rows and leaves the reported numbers alone.

### How make parity counts

A row **exists** when it is done or partial. Rows marked `not a target` or `–` count in no
denominator.

| Component | Rows counted |
|---|---|
| Resource views | the rows of [`resource-parity.md`](resource-parity.md)'s view tables (first column "View") outside Paid Lens Desktop views; a row is one view, or the number its first cell gives as "(N views" (the Gateway API rows) |
| Resource actions | the distinct command ids in those tables' Actions columns and in the first column of Every kind, except an id followed by "not a target" before the next comma; an action exists when the registry has its id |
| Interface rows | the rows of [`ui-parity.md`](ui-parity.md) by their Plan column, `free` or `paid`; every table there has one |
| Packages, authentication | the rows of [`platform-parity.md`](platform-parity.md)'s two sections; a row whose first cell says "(paid)" belongs to another gap and does not count |
| Beyond Lens | every row of [`beyond-lens.md`](beyond-lens.md); not part of breadth |
| Weighted breadth | each component's fraction times its weight in the breadth table; the paid features' row comes from Lens's premium list, not a checklist, and is taken as written |

## Stages

| Stage | Meaning |
|---|---|
| **pre-alpha** | The [alpha gate](roadmap.md#alpha-gate) fails: at least one core workflow does not complete end to end |
| **alpha** | Every core workflow completes end to end on macOS; ready for real work is in the ~40–75% band |
| **beta** | Ready for real work ≥ ~75%, and no gap marked *blocks beta* in [`gaps.md`](gaps.md) |
| **1.0** | Ready for real work ≥ ~95% for the mainstream audience on macOS, Windows and Linux, signed releases with one-click updates |

## The two headline numbers

- **Feature breadth:** does each Lens feature exist in some form? Partly measurable (resource
  views, actions, menu items, shortcuts, preferences), weighted in
  [`target-app-parity.md`](target-app-parity.md#feature-breadth).
- **Ready for real work:** could someone who uses Lens every day switch to Cogpit? Estimated,
  weighted by dimension, with evidence per row. This is the number that decides the stage.

Breadth always runs ahead of readiness: a menu checklist can reach 100% while broken basics keep
real users away. Never quote breadth as progress on its own.
