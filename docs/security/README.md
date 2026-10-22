# Security architecture

Cogpit's security architecture, one page per perspective. No page tries to cover everything: a
document nobody finishes is a document nobody checks. The file names are the index.

| Page | Perspective |
|---|---|
| [`trust-boundaries.md`](trust-boundaries.md) | Who and what Cogpit trusts, and where untrusted data crosses into trusted state |
| [`credentials.md`](credentials.md) | Where cluster credentials and Cogpit's own secrets come from, where they live, and where they must never end up |
| [`privilege-footprint.md`](privilege-footprint.md) | What Cogpit does in a cluster with the user's identity, verb by verb |
| [`agents-and-mcp.md`](agents-and-mcp.md) | Who may drive Cogpit besides the user: external agents over MCP, the built-in agent, the LLM endpoint |
| [`updates.md`](updates.md) | The update channel: how Cogpit replaces itself, and the chain of trust behind it |
| [`file-access.md`](file-access.md) | What Cogpit reads and writes of the user's own files, how macOS grants it the protected folders, and the bookmarks that keep the grant |

## State of this documentation

These pages describe the boundaries that exist in the problem itself (kubeconfig, the Kubernetes
API, credential plugins, terminals, agents, updates) and the check each boundary needs. Since the
MVP (2026-10-11) the mechanisms Cogpit implements link to their code and tests; a required check
without such a link says what of it is not built. When a mechanism lands, its row gains a link to
the code and the test that proves it, in the same change. The owner's decision round of 2026-10-14
evening is recorded on every page whose mechanisms it changes, in the form below; none of the
changes it decided was built then. Every change of that round that these pages describe is built
since (the night run of 2026-10-14/15), except two: the GitHub settings for the release
environment, a tag ruleset and no administrator bypass, made with the owner's OK only on
2026-10-22 ([updates › H-62](updates.md#h-62-every-push-to-main-that-passes-the-gates-is-released));
and several windows, postponed by the owner
([D-52](../developer/decisions.md#d-52-several-windows), below).

## Form of every page

- One perspective per page, stated in its first paragraph.
- Facts about Kubernetes, kubectl or Lens cite their source. Facts about Cogpit link to code.
- Every page ends with `## What this does not cover`. An open gap lives there, in the page whose
  mechanism has it, under a heading with a stable identifier `H-<n>`. Identifiers are never reused
  and never collected in a central list.
- An owner's decision that is not built yet is written where the mechanism it changes is
  described: "Decided (owner, <date>): <what>. Not built yet; until then <today's behaviour>, live
  or dormant." The gap stays open until the change is built. A default the owner confirmed reads
  "Confirmed by the owner (<date>): <what stays>", followed by what remains. Decided and built are
  never merged into one sentence.
- No tickets, checkbox lists or schedules on these pages. Work items belong in
  [`../parity/gaps.md`](../parity/gaps.md).

Reporting a vulnerability: [`SECURITY.md`](../../SECURITY.md).

## What this does not cover

- **Isolation between clusters:** Cogpit is a single-user desktop app with no server side; the
  isolation that matters is between the clusters open in one process. An action, an editor apply,
  a terminal or an agent's tool call must reach only the cluster whose view started it. Since
  2026-10-16 several clusters stay connected at once
  ([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once) stage 1): every command
  acts on the cluster its parameters name (a reference's context, a dock tab's, a `context`
  parameter; the shown cluster without one), references of two clusters in one command are refused,
  a write confirmed for one context refuses to run on another (`CheckConfirmedContext` in
  [`confirm.go`](../../internal/engine/confirm.go)), its cluster's connection, access reviews and the
  allowance of agent writes decide for it, and each terminal is bound to its context by its own
  kubeconfig ([architecture §4.3](../developer/architecture.md#several-clusters-connected-d-69)).
  Since stage 2 (2026-10-16) the dock shows only the shown cluster's tabs and those of no cluster
  ([`dockscope.go`](../../internal/engine/dockscope.go)), so a terminal of one cluster never stands
  beside another cluster's view, and every connected cluster's dot shows that it is connected. What
  agents can do with clusters the window does not show is
  [H-115](agents-and-mcp.md#h-115-agents-read-and-act-on-connected-clusters-the-window-does-not-show). The owner asked on 2026-10-14 evening
  for a hotbar tile dragged out of the window to open another Cogpit window with that cluster; it
  is designed (2026-10-15, [D-52](../developer/decisions.md#d-52-several-windows), postponed by
  the owner until the rest is done: one UI state per window, which window agents' `ui_*` tools drive, which window
  a confirmation belongs to), and building it is where this page becomes due.
