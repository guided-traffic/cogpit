# Security architecture

Cogpit's security architecture, one page per perspective; the file names are the index. The pages
describe the boundaries that exist in the problem itself (kubeconfig, the Kubernetes API,
credential plugins, terminals, agents, updates, the user's files) and the check each one needs. No
page tries to cover everything: a document nobody finishes is a document nobody checks.

| Page | Perspective |
|---|---|
| [`trust-boundaries.md`](trust-boundaries.md) | Who and what Cogpit trusts, and where untrusted data crosses into trusted state |
| [`credentials.md`](credentials.md) | Where cluster credentials and Cogpit's own secrets come from, where they live, and where they must never end up |
| [`privilege-footprint.md`](privilege-footprint.md) | What Cogpit does in a cluster with the user's identity, verb by verb |
| [`agents-and-mcp.md`](agents-and-mcp.md) | Who may drive Cogpit besides the user: external agents over MCP, the built-in agent, the LLM endpoint |
| [`updates.md`](updates.md) | The update channel: how Cogpit replaces itself, and the chain of trust behind it |
| [`file-access.md`](file-access.md) | What Cogpit reads and writes of the user's own files, how macOS grants it the protected folders, and the bookmarks that keep the grant |

Reporting a vulnerability: [`SECURITY.md`](../../SECURITY.md).

## Form of every page

- One perspective per page, stated in its first paragraph. Current state only; history lives in
  git.
- Facts about Kubernetes, kubectl or Lens cite their source. A mechanism Cogpit implements links to
  its code and the test that proves it, added in the change that builds it; a required check
  without such a link says what of it is not built.
- Every page ends with `## What this does not cover`. An open gap lives there, in the page whose
  mechanism has it, as `### H-<n>: <title>`: the risk, when it is live (today, dormant, or what it
  needs), and what would close it. Identifiers are never reused and never collected in a central
  list.
- A risk the owner accepted stays in its gap, marked "Accepted.", with what remains. A closed gap's
  section goes; where the closing mechanism matters, the main text describes it with `(H-<n>)`
  beside it once, so the id stays findable.
- A decided change that is not built stands where the mechanism it changes is described: what is
  decided, with its `D-<n>`, then "Not built." and today's behaviour, live or dormant. Its gap stays
  open until the change is built; decided and built never share a sentence.
- No tickets, checkbox lists or schedules: work items belong in
  [`../parity/gaps.md`](../parity/gaps.md).

## What this does not cover

**Isolation between clusters** has no page of its own. Cogpit is a single-user desktop app with no
hosted service, so the isolation that matters is between the clusters connected in one process: an
action, an editor apply, a terminal or an agent's tool call must reach only the cluster it was
started for. Several clusters stay connected at once
([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once),
[architecture §4.3](../developer/architecture.md#d-69-several-clusters-connected)); what keeps them
apart:

- A command acts on one cluster: its `context` parameter's, else its dock tab's, else its
  references', else the shown one (`targetContext` in
  [`target.go`](../../internal/engine/target.go)). References of two clusters in one command are
  refused before anything is checked or asked (`oneCluster`).
- That cluster's connection, access reviews and allowance of agent writes decide for the command,
  and its confirmation names that context; a write confirmed for one context refuses to run on
  another (`CheckConfirmedContext` in [`confirm.go`](../../internal/engine/confirm.go)).
- A local terminal is bound to its context by a kubeconfig of its own, first on `KUBECONFIG`
  ([`terminal/local.go`](../../internal/terminal/local.go)).
- The dock shows only the shown cluster's tabs and those of no cluster
  ([`dockscope.go`](../../internal/engine/dockscope.go)), so a terminal of one cluster never stands
  beside another cluster's view; every connected cluster's dot shows that it is connected
  ([`clustermarks.go`](../../internal/ui/fyneui/clustermarks.go)).

What agents can do on connected clusters the window does not show is
[H-115](agents-and-mcp.md#h-115-agents-read-and-act-on-connected-clusters-the-window-does-not-show).
Several windows ([D-52](../developer/decisions.md#d-52-several-windows), open, not built) would add
one UI state per window, the window an agent's `ui_*` call drives and the window a confirmation
belongs to; building them is where this perspective needs its own page.
