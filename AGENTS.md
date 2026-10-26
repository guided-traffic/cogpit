# AGENTS.md: guide for AI agents and contributors

Cogpit is an open-source, native Kubernetes IDE written in **Go**: a clean-room reimplementation
of the current Lens Desktop for macOS, Linux and Windows. The goal is Lens parity (the same
navigation, resource views, actions, shortcuts and workflows), a UI that stays fast because it
reads only from a cache, and a tool that agents can drive completely over MCP, the UI included.
The product name is always written **Cogpit** in user-facing text; machine names stay lowercase
(module `github.com/guided-traffic/cogpit`, binaries `cogpit` and `cogpit-cli`). Read this file
first, then `docs/`.

**State on 2026-10-26:** implementation has started (owner's go-ahead, 2026-10-11). A local MVP
runs on macOS, extended by eighteen further implementation runs (2026-10-11 to 2026-10-20, night runs and
day runs): 46 built-in views and the custom resource views from the cache, the cluster Overview,
the detail drawer (a Config Map's and a Secret's values and keys edited in it) and row menus, one
hotbar that scrolls and whose tiles are dragged live (D-83), the navigator with its sections, a tab bar, the command palette, Preferences and Cluster
settings, a welcome page, YAML editing with drafts and undo (D-84), the create dialogs, row selection (each view
keeps its own), Delete of a selection and copy icons in the lists, node and workload actions with a
crash-safe drain as a background job, Helm install from the repositories of Helm's own `repositories.yaml` (credentials included), upgrade, rollback and uninstall (Secrets and
ConfigMaps drivers), kubeconfig sources with approved credential plugins (a login that waits
cancellable and bounded, D-73), macOS's own file panels for every chooser (D-72), **several clusters
connected at once with one shown** (D-69), standby and lost connections with a connection log on
disk, the logged-in user from a SelfSubjectReview, metrics read from the Metrics Server and the
Prometheus, Thanos or VictoriaMetrics a cluster runs (Cogpit installs nothing into a cluster for
its own features), logs, a
terminal with a verified kubectl download, pod shells,
`cogpit-cli` with an MCP stdio bridge, Flux CD and Argo CD sections, a node shell, a built-in agent chat
on OpenAI-compatible endpoints (local models first), CI on every push, releases
with a signed, notarized disk image in the window the owner chose and Sparkle 2 updates through a
signed feed, checked at every start (the first installed copy updated on 2026-10-16; v0.49.0 the
newest, released 2026-10-26 morning: a Helm release's revisions in a dock tab, each revision's values and rendered manifest compared with another, the comparison's direction a preference, every text of the tab selectable (D-131, the owner's colleague's k9s request, walked by the owner before the merge); v0.48.2 released 2026-10-25 late evening: a failed revision's description keeps the release's name its values repeat (D-128, the owner's screenshot); v0.48.1 released 2026-10-25 evening with the owner's findings on v0.48.0: back brings the place back with its filters, drawer and scroll (D-129) and a find bar over a release's values (D-130); v0.48.0 released 2026-10-25 afternoon: the drawer's row marked by the bar alone (D-124 revised) and a Helm release's History in its drawer (D-128); v0.47.0 the same day with the owner's preview of the day accepted live: the agent's connection debugger, TTFT and OTPS, the wheel over every text field, find in the drawer's YAML (D-10, D-125 to D-127); v0.46.0 the same day with the VW walk's VW15–VW20 (D-123, D-124); v0.45.0 released 2026-10-24 night after the owner's live walk: the light strip under a view's
header in the shown cluster's accent colour, static or animated (VW14, his LS1–LS23: one static
colour spreading from the tab, the animations framed with a CPU note, Preferences › App › Light
strip on battery, the tab bar always shown), cluster icons 50 points in the hotbar and the
navigator's head (LS19); v0.44.0 released 2026-10-24 from the AFK session: a b64 button beside a
Secret value's copy icon, a DNS step in Test Connection, cluster icons rounded as macOS's, Copy
Kubeconfig Path, credential plugins in more folders (D-120), the blurry text after a wake worked
around (D-121), integrations GUI-only both ways (H-133), the owner's VW1–VW4; v0.43.0 released 2026-10-23 with the cluster icon designer, an Internal icon of up to five letters
or a Custom icon cut from an image the user places (the owner's VW5, D-119); v0.42.0 the same day
with Preferences and Cluster Settings as a card over the whole window, Cogpit dimmed behind it (the
owner's VW6, D-118); v0.41.0 the same day with the owner's button findings (every content button outlined, room
beside every word, Show and Hide one width, a key's buttons one height; D-104), a tab that comes
back where its list was scrolled (D-117), a view entered that draws its rows from the top, and a
connect and a drawer race fixed; v0.40.2 the same day with one preview tab per cluster (D-117);
v0.40.1 with the owner's re-walk findings RW1–RW4 (a Secret key's buttons on its
name line, the field's outline on every text button, a Secret's Show marked as exposing, the welcome
page's message in a banner naming its level) and the logo at the display's pixels; the stage alpha
since 2026-10-22 ([`ROADMAP.md`](ROADMAP.md)); v0.30.0 released 2026-10-20 morning with **fast scrolling at 60 frames per second**: Fyne's thread check off in releases, a build parameter (D-103); v0.29.3 released 2026-10-20 night with a refused read sent once and the plugin's process search fixed, v0.29.2
before it with a log opened again under a name another pod holds shown once, v0.29.1 with H-118's waiting line and the custom kinds' refusal read once; fast scrolling
built that night and merged after the owner's look of 2026-10-20 (PR #64); v0.29.0 released 2026-10-19 evening with **debug containers** added from a pod's drawer, blue in the Pods list and reached by Logs, Shell and Attach (D-102); v0.28.0 the same evening: agents kept from a cluster's name, icon and integrations (H-125, H-126), a reused row's stale container square and a click's own ⌘ and ⇧ (G16); v0.27.1 the same day with the disk image's name pills the owner chose that afternoon (WF42, D-70) and two race fixes; v0.27.0 the same day with his walk findings WF31–WF45 but WF42; v0.26.0 released 2026-10-18 with his findings WF11–WF30, v0.25.0 its first part the same morning; in v0.27.0: a connect that goes on in the background when another cluster is shown and a failed connect that stays on its cluster, D-97; a Terminating namespace's blockers with Remove finalizers, D-98; text selection in logs and toasts, D-99; dialogs' "Config Map → walk-cm" and effect rows, D-100; the kubeconfig sources' "−" and a removed cluster leaving the hotbar, D-101), **a Linux package** (a `.deb` for amd64 and arm64 from the releases, with Cogpit's signed APT repository on GitHub Pages and updates through apt, built 2026-10-20 night, first in the release after v0.32.0, D-108), **Cogpit for Windows** (an NSIS setup for x64 per user, WinSparkle's updates with one click from a feed signed with the update key, the terminal on ConPTY, every test run on Windows; built 2026-10-21 on the branch `windows-release`, released once merged, D-111), **integrations per cluster** (2026-10-17, D-76 to D-79: the Metrics
Server and Prometheus, Alertmanager with the title bar's alerts pill and an alert board, the Trivy
Operator's image ratings of the pods, Kyverno's policy violations on the objects; each switched on or
off in its own section of Cluster Settings, a new cluster asks first and nothing is looked for before
the answer, D-82, D-87), the cluster's Kubernetes version against upstream's end of life from a table
built into each release (D-91), dialogs as tables (D-85), a Reconnect that keeps the tabs and a
Log in again that takes a renewed certificate from the kubeconfig file (D-81), and MCP control of all of it (M9 done: an external agent's triage and change
workflows over MCP on kind); it doubled
as the Fyne spike ([results](docs/developer/spike-fyne.md#results)).
What is built and where it differs from the plan: [architecture › Current state](docs/developer/architecture.md#current-state-2026-10-19). The documentation method, and parts of
this file, come from [PhotoCraft](https://github.com/storytold/photocraft)
([attribution](ATTRIBUTION.md)).

## 1. Orientation (5 minutes)

| Read | Why |
|---|---|
| [`ROADMAP.md`](ROADMAP.md) | One page: stage, headline numbers, every dimension, what's next |
| [`docs/developer/decisions.md`](docs/developer/decisions.md) | What is decided (Go, Fyne after the spike gate, MCP control, built-in agent, updates, crash stance, cache) and what is open |
| [`docs/developer/architecture.md`](docs/developer/architecture.md) | Packages, layering, the engine/UI seam, the cache, MCP and the agent |
| [`docs/parity/target-app-parity.md`](docs/parity/target-app-parity.md) | The authoritative assessment against Lens Desktop 2026.10.51501 |
| [`docs/parity/gaps.md`](docs/parity/gaps.md) | Every shortfall, ranked: the work list to pick from |
| [`docs/parity/roadmap.md`](docs/parity/roadmap.md) | Alpha gate, milestones, **current focus** |
| [`docs/parity/ui-parity.md`](docs/parity/ui-parity.md), [`resource-parity.md`](docs/parity/resource-parity.md), [`platform-parity.md`](docs/parity/platform-parity.md), [`beyond-lens.md`](docs/parity/beyond-lens.md) | The checklists |
| [`docs/parity/README.md`](docs/parity/README.md) | How the progress docs work and the rules for updating them |
| [`docs/developer/conventions.md`](docs/developer/conventions.md) | The rules in detail |
| [`docs/developer/extending.md`](docs/developer/extending.md) | Checklists: adding a command, a kind profile, a package, a cache consumer |
| [`docs/developer/spike-fyne.md`](docs/developer/spike-fyne.md) | The UI toolkit gate before M2 |
| [`docs/developer/usage.md`](docs/developer/usage.md) | Running Cogpit, flags, files it writes, naming, agents' set-up |
| [`docs/developer/commands.md`](docs/developer/commands.md) | Every command and MCP tool, generated |
| [`docs/security/`](docs/security/README.md) | Trust boundaries, credentials, privilege footprint, agents and MCP, updates |

## 2. Package map

```text
cmd/cogpit, cmd/cogpit-cli    L5 desktop app (Fyne, cgo); headless CLI (contexts, get) and MCP stdio bridge (pure Go)
internal/ui/fyneui            L4 the Fyne shell; the only package that imports Fyne
internal/ui/logo              L4 the logo, every version of it, rasterised for the app icon and the GUI (pure Go)
internal/ui/nativedialog      L4 the system's file panels (macOS: NSOpenPanel, NSSavePanel; cgo) and the bookmarks of what they grant (no Fyne)
internal/mcp agent update     L4 MCP server; built-in agent; update check and install
internal/app                  L4 the wiring both binaries share: Cogpit's directories, kubeconfig sources, audit log
internal/engine               L3 Session, command registry, UI state, jobs, events, confirmation, audit log
internal/engine/commands      L3 the commands, one file per area
internal/helm metrics demo    L2 Helm SDK; metrics from the Metrics Server and Prometheus; the synthetic demo cluster (development builds only: releases build with -tags nodemo, D-67)
internal/alerting             L2 the cluster's Alertmanager: found by name, its active alerts polled while shown (D-77)
internal/cluster cache        L1 connections and auth; shared informers (the cache)
internal/terminal             L1 terminal sessions: a local shell on a PTY (ConPTY on Windows), pod and node shells, attach
internal/kubectl              L1 kubectl for terminals: client version, skew, verified download (release host and sigstore's log only)
internal/locale               L1 the computer's date and time format (macOS region via cgo, else $LC_TIME), D-31
internal/model kubeconfig     L0 the resource model (references, kind profiles, rows, detail sections); kubeconfig parsing (no I/O)
internal/model/kinds          L0 the built-in kind profiles, one file per kind
internal/model/k8seol         L0 Kubernetes' upstream release table, embedded and refreshed at release build (make k8s-eol, D-91)
internal/buildinfo            L0 the version both binaries report (release, else dev and the commit)
internal/testkit              fixtures, a fake API server and clients, a fake LLM endpoint, a sigstore of its own; kind tests in testkit/integration
```

**Layering is enforced** by golangci-lint's `depguard` in `make lint`: a package imports only
lower layers, only `internal/ui/...` imports Fyne, `model` and `kubeconfig` do no I/O, and only
`cluster`, `cache`, `terminal`, `helm`, `metrics` and `alerting` talk to the API server.

## 3. Golden rules

### Never surprise a cluster (outranks feature work)

Cogpit acts with the user's identity on clusters that may be production.

- **Read-only unless asked.** Browsing never writes.
- **Every mutating command declares its verbs and a danger level.** `Destructive` commands
  (delete, drain, evict, scale to zero, Helm uninstall and rollback, node shell, removing a data
  key from a Config Map or Secret, in its drawer or through the Edit tab, adding a debug container
  to a pod) need a confirmation that
  names the objects, the namespace and the context. The engine enforces it for every origin: GUI,
  CLI, external agents and the built-in agent.
- **Agents follow D-2:** reads and UI operations run freely; mutating commands need the user's
  acceptance unless allowed for that context; destructive commands always need it; everything an
  agent does is in the audit log.
- **No implicit installs and no implicit kubeconfig writes.**
- **Cogpit never installs anything into a cluster for its own features** (metrics, agents,
  exporters): it finds and uses what is installed and makes its information useful; installing is
  the user's own action on their charts (owner, 2026-10-16).
- The verb-by-verb contract is [privilege footprint](docs/security/privilege-footprint.md).

### Cache first

The UI and agents read only from the cache; only the cache layer lists and watches. Never call the
API server from a view, a command or an MCP tool for data the cache holds, and never poll what can
be watched. New data needs go into the cache layer ([architecture §5](docs/developer/architecture.md#5-cache)).

### Crash-tolerant UI, crash-safe cluster operations

A crash is a bug, not a catastrophe: the cluster keeps its state ([D-11](docs/developer/decisions.md#d-11-crash-stance)).
But multi-step operations (Helm install, upgrade and rollback; drain) must survive a crash or be
finishable after one, and every goroutine doing cluster I/O recovers from a panic at its boundary.
Run tests with the race detector.

### The other rules

1. **Everything is a command.** New user-visible behaviour is an engine command with an id, label,
   menu path, shortcut, parameter type, `enabled` (with a reason), `verbs`, `danger` and `run`, plus
   tests. It becomes an MCP tool automatically. Use the ids in
   [`resource-parity.md`](docs/parity/resource-parity.md) and
   [`ui-parity.md`](docs/parity/ui-parity.md).
2. **UI state is engine state.** The GUI changes view, filter, selection and drawer only through
   `ui.*` commands, so agents see and drive the same state.
3. **Discover, don't assume.** Kinds, versions, permissions and metrics providers come from the
   cluster at runtime. A missing kind, permission or provider is a state the UI shows, not an error.
4. **Clean-room against the current Lens Desktop.** Behaviour and look by observation and its
   documentation. No code, text or asset from Lens Desktop, OpenLens or Freelens
   ([D-12](docs/developer/decisions.md#d-12-parity-sources)).
5. **Tests are the gate** ([`testing.md`](docs/developer/testing.md)). Tests never read the
   developer's `~/.kube/config`; cluster tests create their own `kind` cluster or envtest
   control plane.
6. **The UI is thin and data-driven.** Colours come from theme tokens and status tokens.
7. **Verify UI changes visually** with an offscreen render (Fyne's test driver) or a screenshot,
   and attach before/after images to the pull request.
8. **Performance is a feature.** Lists at 10,000 rows, 1,000 watch events per second, and the API
   request budgets in [`testing.md`](docs/developer/testing.md#performance-budgets-proposed-not-measured).
9. **Secrets stay secret.** Tokens, keys, a credential plugin's standard output and Secret values
   never reach logs, errors, crash reports or an LLM endpoint; its standard error only after a
   failed login, scrubbed and capped, into the connection log (D-66) ([credentials](docs/security/credentials.md),
   [agents and MCP](docs/security/agents-and-mcp.md)).
10. **The docs always show the current state** (owner, 2026-10-15). Every change, patch and fix
    updates the documentation it affects in the same commit as the code, so the docs never
    describe yesterday's Cogpit: the parity checklist row, the gap entry and the numbers they feed
    ([rules](docs/parity/README.md#rules-for-every-progress-page)); the decision's status in
    [`decisions.md`](docs/developer/decisions.md) ("decided, not built" becomes "built", with its
    evidence); the security page whose mechanism changed, and the `H-<n>` it opens or closes; the
    current state in [`architecture.md`](docs/developer/architecture.md); [`usage.md`](docs/developer/usage.md)
    for what a user sees; and the current focus in [`roadmap.md`](docs/parity/roadmap.md) when the
    change closes or moves one of its items. A sentence saying "not built yet", "planned" or
    "none yet" about what the change just built is wrong the moment it lands. A change is not
    finished while a page still describes the old state.
11. **Decide details, record every one** (owner, 2026-10-17,
    [D-74](docs/developer/decisions.md#d-74-how-the-leads-decisions-are-reviewed)): the owner
    decides direction, scope, security posture and the irreversible or outward-facing; a detail
    below that is decided by whoever builds it and recorded where it lives (question, options,
    choice, why), as the lead's decision, open to revision. Nothing undocumented: a choice only a
    report or a commit names goes into its page in the same change.
12. **One version of every tool and dependency** (owner, 2026-10-24): when two versions of a
    control tool, the toolchain or a dependency are in use (locally against CI, or two installs on
    the machine), the newer one goes in everywhere; code is never bent to satisfy both. bin/'s
    tools (golangci-lint, gocyclo, govulncheck, fyne) are built with go.mod's Go and rebuilt when
    their pin or that Go changes (the Makefile's `tool_stamp`); format only through `make fmt`, the
    pinned golangci-lint, never another `gofmt` from PATH. Found 2026-10-24: a bin/golangci-lint
    v2.14.0 built with Go 1.27.2 beside CI's v2.15.0 with 1.27.3, and Homebrew's Go 1.26.5 `gofmt`
    on PATH: a file both CI and Go 1.27.3 accept looked unformatted. Anything stale or broken seen
    on the way is fixed or written into `plan/next-session-prompt.md`, never left unmentioned.

## 4. Picking work

Priorities: infrastructure first, then the alpha-gate workflows, then breadth, then the long tail.

0. **Read [`ROADMAP.md`](ROADMAP.md), then [`docs/parity/gaps.md`](docs/parity/gaps.md).** When
   your work moves a number or closes a gap, update the checklist row, the gap entry and the
   numbers they feed, with the date, in the same pull request
   ([rules](docs/parity/README.md#rules-for-every-progress-page)).
1. [`docs/parity/roadmap.md`](docs/parity/roadmap.md) → **Current focus**.
2. The scorecard ([`docs/parity/scorecard.md`](docs/parity/scorecard.md), `make scorecard`): the budgets not met and the ones not measured yet.
3. `log/devlog.md` (local, gitignored) → the "Still open" bullets of recent entries.

## 5. Before you finish a task

Locally, keep it fast (owner, 2026-10-12): CI is the full gate, so check here only what the change
touches.

```sh
go build ./...                          # the tree builds
go vet ./internal/<pkg>/...             # the packages you touched
go test ./internal/<pkg>/...            # their tests, without -race
make test-ui                            # internal/ui/fyneui's tests: eight shards at once, about a minute (D-112)
go run ./hack/parity                    # only when a checklist or a progress page changed; rewrites
                                        # docs/parity/parity-checklist.md, commit it with the change
go run ./hack/readmecmds                # only when commands changed: the table in docs/developer/commands.md
# -race on the one package only when the change is about concurrency (cache, journals, terminal)
```

CI ([`release.yml`](.github/workflows/release.yml), one job per check, none longer than ten
minutes: [releasing › CI](docs/developer/releasing.md#ci)) runs on every push what used to run
here, each with its own make target: lint (depguard layering included), parity, GoSec,
govulncheck, cyclomatic complexity, the tests the race detector keeps from running (without it),
the UI's race tests in eight shards and the other packages' race tests, the kind and drain cluster tests, the envtest tests, the macOS-only tests and the app, the
Windows unit and UI tests (every test on Windows, without the race detector), the Linux and Windows
packages, and the combined coverage. Read its result when you next push (`gh run list`, then
`gh run view <id> --log-failed`) and fix what is red first. Once every job on `main` is green,
semantic-release publishes a release for the `feat:` and `fix:` commits since the last one and
pushes a release commit to `main`, so `git pull --rebase` before your next push.

Before committing, bring the docs to the new state ([rule 10](#the-other-rules)): search them for
what the change touches (its command ids, the decision, the `H-<n>`, "not built", "planned") and
correct every sentence that no longer holds.

Then commit and push. A commit that only changes documentation carries `[skip ci]` and is never
the newest commit of a push with code (owner, 2026-10-15; [conventions](docs/developer/conventions.md#commits-and-pull-requests)).
Agents may commit and push to `main` on their own (owner, 2026-10-11), in
[Conventional Commits](docs/developer/conventions.md#commits-and-pull-requests) form and **without
`Co-Authored-By` trailers or "Generated with" lines**, and **never with `!` or a `BREAKING CHANGE:`
footer**: releases continue from the owner's anchor tag `v0.0.0`, and 1.0.0 is the owner's call
(until then `.releaserc.json` turns a breaking change into a minor release). Don't wait for CI before the next step (owner, 2026-10-11): run the
quick local checks, push, and look at CI when you next push. Append a terse entry to `log/devlog.md`:
what landed, numbers, what's still open. Keep the tree building at every step.

## 6. Parallel agents

- Edit only the files you own. Shared files (the command list, the UI's menu wiring, package
  registries) get small, surgical edits; re-read before editing.
- Put new commands in a **new file** (`internal/engine/commands/<area>.go`) and new kind profiles in
  their own file (`internal/model/kinds/<kind>.go`) rather than growing a shared file.
- If someone else's in-progress edit breaks the build, wait and retry; don't fix their files.
- `go.mod` and `go.sum` change through `go get` and `go mod tidy` only, by one agent at a time.

**Several leads at once** (owner, 2026-10-22: up to three lead sessions, each on its own bugs and
features; these rules are the lead's, open to revision):

- **Own worktree, own branch.** Each lead works in its own git worktree, never in a folder
  another lead uses: the owner works in VS Code only, one window per lead, opened on
  `.claude/worktrees/<name>` (made with `git worktree add -b worktree-<name>
  .claude/worktrees/<name> origin/main`; made when the owner opens a second or third lead; `lead-2` and `lead-3`, made 2026-10-22, removed 2026-10-23 once he closed them, empty). A task gets
  a branch of its own in it (`git switch -c <topic> origin/main`) and ends as a pull request the
  lead merges itself once CI is green: rebase on `main` first; in the docs' status lines and
  revision tables keep both sides. The worktree stays for the lead's next task and goes, once clean, when the owner closes that lead.
- **Numbers at merge time.** A new decision, gap or security gap is written `D-new`, `G-new`,
  `H-new` on the branch and gets the next free number on `main` in the rebase before the merge, so
  two leads never take the same one.
- **One real app at a time.** Every Cogpit process shares `<user config dir>/Cogpit` (sources,
  settings, the MCP endpoint and token) and `bin/Cogpit.app` registers with LaunchServices: only one
  lead runs or quits the real app at once; render tests and unit tests are free. A kind cluster per
  lead: `make test-kind KIND_CLUSTER=cogpit-<name>`.
- **One devlog.** `log/` is gitignored, so a worktree has none: append to the main checkout's
  `log/devlog.md` by its absolute path.
- **RAM.** Three leads use the budget of three agents: a lead starts a subagent only while
  `memory_pressure -Q` shows 50% free or more.

## 7. Where things are tracked

- [`ROADMAP.md`](ROADMAP.md) and [`docs/parity/`](docs/parity/README.md): where Cogpit stands
  against Lens, what's next, the work list.
- [`docs/developer/decisions.md`](docs/developer/decisions.md): decisions, open and taken.
- [`docs/security/`](docs/security/README.md): the security architecture and its open gaps
  (`H-<n>`).
- `plan/` and `log/` (local, gitignored): research notes and the dev log.
