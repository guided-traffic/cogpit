# AGENTS.md: guide for AI agents and contributors

Cogpit is an open-source, native Kubernetes IDE written in **Go**: a clean-room reimplementation of
the current Lens Desktop for macOS, Linux and Windows. The goal is Lens parity (the same navigation,
resource views, actions, shortcuts and workflows), a UI that stays fast because it reads only from a
cache, and a tool that agents can drive completely over MCP, the UI included. The product name is
always written **Cogpit** in user-facing text; machine names stay lowercase (module
`github.com/guided-traffic/cogpit`, binaries `cogpit` and `cogpit-cli`). Read this file first, then
`docs/`.

**State:** alpha. What is built and what comes next: [`ROADMAP.md`](ROADMAP.md) and
[architecture](docs/developer/architecture.md). Releases (a signed, notarized macOS disk image with
Sparkle updates, a Windows setup with WinSparkle updates, Linux `.deb` packages with an APT
repository) and their notes: the GitHub releases. The documentation method comes from
[PhotoCraft](https://github.com/storytold/photocraft) ([attribution](ATTRIBUTION.md)).

## 1. Orientation (5 minutes)

| Read | Why |
|---|---|
| [`ROADMAP.md`](ROADMAP.md) | One page: stage, headline numbers, every dimension, what's next |
| [`docs/developer/decisions.md`](docs/developer/decisions.md) | What is decided and why, what is open |
| [`docs/developer/architecture.md`](docs/developer/architecture.md) | Packages, layering, the engine/UI seam, the cache, MCP and the agent |
| [`docs/parity/target-app-parity.md`](docs/parity/target-app-parity.md) | The assessment against Lens Desktop 2026.10.51501 |
| [`docs/parity/gaps.md`](docs/parity/gaps.md) | Every open shortfall, ranked: the work list to pick from |
| [`docs/parity/roadmap.md`](docs/parity/roadmap.md) | Alpha gate, milestones, **current focus** |
| [`docs/parity/ui-parity.md`](docs/parity/ui-parity.md), [`resource-parity.md`](docs/parity/resource-parity.md), [`platform-parity.md`](docs/parity/platform-parity.md), [`beyond-lens.md`](docs/parity/beyond-lens.md) | The checklists |
| [`docs/parity/README.md`](docs/parity/README.md) | How the progress pages work and the rules for updating them |
| [`docs/developer/conventions.md`](docs/developer/conventions.md) | The rules in detail, the documentation rules included |
| [`docs/developer/extending.md`](docs/developer/extending.md) | Checklists: adding a command, a kind profile, a package, a cache consumer |
| [`docs/developer/testing.md`](docs/developer/testing.md) | Test tiers, fixtures, the rules against flaky tests, the budgets |
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
  key from a Config Map or Secret, adding a debug container to a pod) need a confirmation that
  names the objects, the namespace and the context. The engine enforces it for every origin: GUI,
  CLI, external agents and the built-in agent.
- **Agents follow D-2:** reads and UI operations run freely; mutating commands need the user's
  acceptance unless allowed for that context; destructive commands always need it; everything an
  agent does is in the audit log.
- **No implicit installs and no implicit kubeconfig writes.**
- **Cogpit never installs anything into a cluster for its own features** (metrics, agents,
  exporters): it finds and uses what is installed; installing is the user's own action.
- The verb-by-verb contract is [privilege footprint](docs/security/privilege-footprint.md).

### Cache first

The UI and agents read only from the cache; only the cache layer lists and watches. Never call the
API server from a view, a command or an MCP tool for data the cache holds, and never poll what can
be watched. New data needs go into the cache layer ([architecture §5](docs/developer/architecture.md#5-cache)).

### Crash-tolerant UI, crash-safe cluster operations

A crash is a bug, not a catastrophe: the cluster keeps its state ([D-11](docs/developer/decisions.md#d-11-crash-stance)).
Multi-step operations (Helm install, upgrade and rollback; drain) survive a crash or are finishable
after one, and every goroutine doing cluster I/O recovers from a panic at its boundary. CI runs the
tests with the race detector.

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
   developer's `~/.kube/config`; cluster tests create their own `kind` cluster or envtest control
   plane. A test waits for the state it asserts, never for time ([testing › Asynchronous
   results](docs/developer/testing.md#asynchronous-results)).
6. **The UI is thin and data-driven.** Colours come from theme tokens and status tokens.
7. **Verify UI changes visually** with an offscreen render (Fyne's test driver) or a screenshot,
   and attach before/after images to the pull request.
8. **Performance is a feature.** Lists at 10,000 rows, 1,000 watch events per second, and the API
   request budgets in [`testing.md`](docs/developer/testing.md#performance-budgets).
9. **Secrets stay secret.** Tokens, keys, a credential plugin's standard output and Secret values
   never reach logs, errors, crash reports or an LLM endpoint; a plugin's standard error only after
   a failed login, scrubbed and capped, into the connection log (D-66)
   ([credentials](docs/security/credentials.md), [agents and MCP](docs/security/agents-and-mcp.md)).
10. **The docs show the current state, and only that.** Every change updates the documentation it
    affects in the same commit: the checklist row, the gap and the numbers they feed
    ([rules](docs/parity/README.md#rules-for-every-progress-page)), the decision's status, the
    security page whose mechanism changed and the `H-<n>` it opens or closes, architecture,
    `usage.md` for what a user sees, the current focus in `roadmap.md`. Docs carry **no history**
    (no dates, release versions, "since", "no longer", change lines or revision tables: git and the
    GitHub releases hold them), **no stories of fixed bugs**, **no process meta** (quotes, walk or
    finding ids, session names), and **no long text for simple facts**; each fact has one home and
    other pages link to it ([conventions › Documentation](docs/developer/conventions.md#documentation)).
11. **Decide details, record every one** ([D-74](docs/developer/decisions.md#d-74-how-the-leads-decisions-are-reviewed)):
    the owner decides direction, scope, security posture and the irreversible or outward-facing; a
    detail below that is decided by whoever builds it, recorded in
    [`decisions.md`](docs/developer/decisions.md) (decision, why, rejected options) and stated as
    behaviour on the page where it lives, open to revision.
12. **One version of every tool and dependency**: when two versions of a control tool, the
    toolchain or a dependency are in use (locally against CI, or two installs on the machine), the
    newer one goes in everywhere; code is never bent to satisfy both. bin/'s tools (golangci-lint,
    gocyclo, govulncheck, fyne) are built with go.mod's Go and rebuilt when their pin or that Go
    changes (the Makefile's `tool_stamp`); format only through `make fmt`, never another `gofmt`
    from PATH. Anything stale or broken seen on the way is fixed or written down for the next
    session, never left unmentioned.

## 4. Picking work

Priorities: infrastructure first, then the alpha-gate workflows, then breadth, then the long tail.

0. **Read [`ROADMAP.md`](ROADMAP.md), then [`docs/parity/gaps.md`](docs/parity/gaps.md).** When
   your work moves a number or closes a gap, update the checklist row, the gap entry and the
   numbers they feed in the same pull request
   ([rules](docs/parity/README.md#rules-for-every-progress-page)).
1. [`docs/parity/roadmap.md`](docs/parity/roadmap.md) → **Current focus**.
2. The scorecard ([`docs/parity/scorecard.md`](docs/parity/scorecard.md), `make scorecard`): the
   budgets not met and the ones not measured yet.
3. `log/devlog.md` (local, gitignored) → the "Still open" bullets of recent entries.

## 5. Before you finish a task

Locally, check only what the change touches; CI is the full gate.

```sh
go build ./...                          # the tree builds
go vet ./internal/<pkg>/...             # the packages you touched
go test ./internal/<pkg>/...            # their tests, without -race
make test-ui                            # internal/ui/fyneui's tests in parallel shards (D-112)
go run ./hack/parity                    # only when a checklist or a progress page changed; rewrites
                                        # docs/parity/parity-checklist.md, commit it with the change
go run ./hack/readmecmds                # only when commands changed: the table in docs/developer/commands.md
# -race on the one package only when the change is about concurrency (cache, journals, terminal)
```

CI ([`release.yml`](.github/workflows/release.yml), one job per check with its own make target,
none longer than ten minutes: [releasing › CI](docs/developer/releasing.md#ci)) runs on every push
and pull request: lint (depguard layering included), parity, GoSec, govulncheck, cyclomatic
complexity, the race tests (the UI's package in five shards), the tests the race detector keeps
from running, the kind, drain and envtest cluster tests, the macOS-only tests and the app, every
test on Windows, the Linux and Windows packages, on Linux and Windows the shipped app started once
(`make smoke`), and the combined coverage. Read its result when
you next push (`gh run list`, then `gh run view <id> --log-failed`) and fix what is red first. Once
every job on `main` is green, semantic-release publishes a release for the `feat:` and `fix:`
commits since the last one and pushes a release commit to `main`, so `git pull --rebase` before
your next push.

Before committing, bring the docs to the new state ([rule 10](#the-other-rules)): search them for
what the change touches (its command ids, the decision, the `H-<n>`, "not built") and correct every
sentence that no longer holds.

Then commit and push, in [Conventional Commits](docs/developer/conventions.md#commits-and-pull-requests)
form, **without `Co-Authored-By` trailers or "Generated with" lines**, and **never with `!` or a
`BREAKING CHANGE:` footer** (1.0.0 is the owner's call; until then `.releaserc.json` turns a
breaking change into a minor release). A commit that only changes documentation carries
`[skip ci]` and is never the newest commit of a push with code. Agents may commit and push to
`main` on their own; don't wait for CI before the next step. Append a terse entry to
`log/devlog.md`: what landed, numbers, what's still open. Keep the tree building at every step.

## 6. Parallel agents

- Edit only the files you own. Shared files (the command list, the UI's menu wiring, package
  registries) get small, surgical edits; re-read before editing.
- Put new commands in a **new file** (`internal/engine/commands/<area>.go`) and new kind profiles in
  their own file (`internal/model/kinds/<kind>.go`) rather than growing a shared file.
- If someone else's in-progress edit breaks the build, wait and retry; don't fix their files.
- `go.mod` and `go.sum` change through `go get` and `go mod tidy` only, by one agent at a time.

**Several leads at once** (up to three lead sessions, each on its own bugs and features):

- **Own worktree, own branch.** Each lead works in its own git worktree, never in a folder another
  lead uses: the owner works in VS Code, one window per lead, opened on `.claude/worktrees/<name>`
  (made with `git worktree add -b worktree-<name> .claude/worktrees/<name> origin/main` when the
  owner opens a further lead). A task gets a branch of its own in it
  (`git switch -c <topic> origin/main`) and ends as a pull request the lead merges itself once CI
  is green: rebase on `main` first. The worktree stays for the lead's next task and goes, once
  clean, when the owner closes that lead.
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
- The GitHub releases: what each release changed.
- `plan/` and `log/` (local, gitignored): research notes and the dev log.
