# Conventions

The rules every change follows. [`AGENTS.md`](../../AGENTS.md) states them in short; this page has
the detail and the reasons.

## Language, module and licence

- **Go** ([D-7](decisions.md#d-7-language-and-platforms)), module
  `github.com/guided-traffic/cogpit`, the Go version the team's other repositories use (their
  `go.mod` files declare `go 1.27.2` on 2026-10-11).
- **No JavaScript, TypeScript or webview** in the product. The UI is Fyne
  ([D-1](decisions.md#d-1-ui-toolkit)); only if a platform's re-check fails does a separate
  Rust/egui UI process join for that platform, and then only behind the engine's protocol.
- **Licence:** Apache-2.0 ([`LICENSE`](../../LICENSE)). Dependencies must be compatible
  (Apache-2.0, MIT, BSD, ISC, MPL-2.0 as a library).

## Clean-room

The target is the current Lens Desktop ([D-12](decisions.md#d-12-parity-sources)), proprietary
software of Mirantis. We match its behaviour and look by observation and its public
documentation.

- **Never use** code, text, icons, logos, illustrations, fonts or other assets of Lens Desktop,
  OpenLens or Freelens, even where they ship as readable files or are open source.
- **Branding.** Lens is Mirantis's product and brand. User-facing text never presents Cogpit as
  Lens, as made by Mirantis, or as endorsed by it. Comparisons in documentation ("matches Lens's
  Pods view") are fine.
- **Third-party assets** must be permissively licensed, keep their licence file next to them, and
  get a row in [`ATTRIBUTION.md`](../../ATTRIBUTION.md) (path, title, author, source, licence) in
  the same change.

## Never surprise a cluster

- **Read-only unless asked.** Opening a view, hovering or selecting never writes to a cluster.
- **Declare verbs.** Every mutating command lists the API verbs and resources it uses.
- **Confirm destructive actions** (delete, drain, evict, scale to zero, a data key removed in a
  drawer or through the Edit tab, Helm uninstall and rollback, node shell) with a dialog naming the
  objects, the namespace and the context. The engine enforces it, for the CLI and agents too.
- **Agent origin** follows [D-2](decisions.md#d-2-agent-interface-scope): mutating commands need the
  user's acceptance unless allowed for that context, destructive ones always; all of it is logged.
- **No implicit installs, no implicit kubeconfig writes.** A node shell, which creates a privileged
  pod, says so before it runs ([privilege footprint](../security/privilege-footprint.md)).

## Cache first

- Views, commands and MCP tools read from the cache ([architecture §5](architecture.md#5-cache)).
  Only `cluster`, `cache`, `terminal`, `helm`, `metrics` and `alerting` talk to the API server.
- Acquire a cache handle for what you show, release it when you stop showing it. Never start your
  own informer or watch.
- Never poll a resource that can be watched. Polled data (metrics, chart indexes) is polled only
  while visible.
- A new data need is a change to the cache layer, with a test that counts the API requests it costs.

## Crash-tolerant, crash-safe

[D-11](decisions.md#d-11-crash-stance): a crash is a bug, not a catastrophe; a half-finished
cluster operation is worse than a crash.

- **Errors, not panics.** Return errors and wrap them with `%w`; `errcheck` is on. Do not call
  `panic` in non-test code; a programming error found at start-up (duplicate registry ids) may stop
  start-up with an error message.
- **Recover at goroutine boundaries.** Every goroutine that does cluster I/O starts through a helper
  that recovers a panic, logs it with its stack, and reports it to the engine as an error. One
  failing watch must not close every cluster.
- **Unstructured data is hostile input.** Read it through the `model` helpers, which never panic on
  unexpected shapes; check bounds; cap buffers sized by input (log lines, YAML documents, terminal
  scrollback).
- **No data races.** `go test -race` runs in CI; maps shared between goroutines are guarded,
  because a concurrent map write ends the process and cannot be recovered.
- **Multi-step mutations are crash-safe:** they run in a worker process that survives the UI, or
  the next start detects the half-finished state and offers to finish or revert it.
- **Unsaved editor content** is kept as a local draft.

## Secrets stay secret

- Secret values, bearer tokens, client keys, a credential plugin's standard output and LLM API
  keys never appear in logs, errors, crash reports or telemetry; a credential plugin's standard
  error only after a failed login, scrubbed and cut to 4 KiB, in the connection log
  ([D-66](decisions.md#d-66-the-connection-log)). Credential types implement `slog.LogValuer` and
  `fmt.Stringer` as `"<redacted>"`.
- Secret data in the UI is hidden until the user reveals one field.
- Nothing from a Secret's data is sent to an LLM endpoint unless the user explicitly allows it for
  that request ([agents and MCP](../security/agents-and-mcp.md)).

## Commands, not handlers

New user-visible behaviour is an engine command with tests; the UI and MCP call it. Checklist:
[`extending.md`](extending.md#adding-a-command).

The documented exceptions are GUI-only on purpose, so that no agent can reach them: the status
bar's link to the owner's page and its MCP switch ([D-23](decisions.md#d-23-a-link-to-the-owner-in-the-status-bar)),
selecting and copying text (the drawer's values, the lists' cells, since 2026-10-19 a log tab's
lines and a toast's text: [D-25](decisions.md#d-25-copying-values),
[D-99](decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)), and the MCP details
with Regenerate token ([D-53](decisions.md#d-53-the-mcp-details-and-a-token-kept-until-the-user-regenerates-it):
the owner's F6, window-only as the lead's default, open to revision). A new exception needs
a decision of the owner.

## Tests

Every change comes with tests ([`testing.md`](testing.md)). Kind profiles have golden tests;
anything that parses input (kubeconfig, YAML, terminal output, MCP and LLM messages) has
malformed-input tests or fuzz targets.

## Style

- `gofmt` and `goimports`; golangci-lint with the team's linter set (errcheck, govet, ineffassign,
  staticcheck, unused, misspell, unconvert, unparam, goconst, prealloc, revive) plus `depguard` for
  the layering.
- Doc comments on every exported identifier (revive's `exported` rule). Comments explain *why*.
- Names: packages short and lowercase (`cache`, `engine`); command ids `<kind-or-area>.<action>`
  in lowerCamelCase segments (`deployment.restart`, `helm.release.rollback`), their MCP tool names
  with underscores (`helm_release_rollback`); environment variables `COGPIT_<NAME>`.
- Logging with `log/slog`.
- User-facing text says **Cogpit**. Machine names stay lowercase.

## UI

- Colours, sizes and paddings come from Cogpit's Fyne theme, never hard-coded in a widget.
- Status colours come from the row's status token, not from the view.
- The palettes are Solarized Dark and Solarized Light, copied once ([D-22](decisions.md#d-22-colour-theme),
  [`theme.go`](../../internal/ui/fyneui/theme.go)): a new colour is a token there, in both
  palettes, with the VS Code key it comes from or a note why it differs. Text on a surface must
  pass the floors of `TestPaletteContrast`; add the new surface to that test.
- Widget updates from other goroutines go through `fyne.Do`.
- Nothing runs Fyne's theme or `fyne.CurrentApp()` at package initialisation (a package-level
  `var x = theme.SomeIcon()`): no app exists yet, Fyne logs "Attempt to access current Fyne app
  when none is started" and hands out the fallback theme's value. Store the function
  (`theme.SomeIcon`) and call it when drawing; `TestStartUpLogsNoFyneError` in `cmd/cogpit` fails
  on such a line (2026-10-12).
- Verify every visual change with an offscreen render (Fyne's `test` package) or a screenshot, in
  dark and light themes, and attach before/after images to the pull request.

### Dialogs, reviews and status lines

The owner's rule (walk of 2026-10-18, WF13, WF14, WF16; [D-85](decisions.md#d-85-dialogs-as-tables-and-an-objects-state-without-its-resourceversion)):
"Vermeide solche Texte, bei der täglichen Arbeit braucht man genau so eine Tabelle … Schnell zu
erfassen, was jetzt passiert." and "Die Reihenfolge für die Ortsbeschreibung von
Kubernetes-Objekten ist immer: Context, Namespace, Objects."

- A confirmation, a save review or any dialog that says what is about to happen is a **table of
  rows** (name, value), read at a glance; **no sentence repeats the rows**. A destructive
  confirmation still names the objects, the namespace and the context: through its rows.
- **An object's place is always Context › Namespace › Object(s)**, in rows, titles and texts.
- **An object is named in one form everywhere** (the owner's WF32, 2026-10-19: "Config Map ->
  walk-cm oder etwas ähnliches"; [D-100](decisions.md#d-100-objects-and-effects-in-one-form-and-a-drawers-edited-state-beside-save)):
  the kind muted with an arrow, the name bold, "Config Map → walk-cm"; several grouped by kind,
  the kind once, the names under each other; in plain text (a title) "Config Map → walk-cm". A
  dialog builds its rows with `placeRows` and `objectsRow` ([`objectnames.go`](../../internal/ui/fyneui/objectnames.go)),
  never with `kind + " " + name`.
- **What a command does beyond its objects is a quick view, never a sentence** (the owner's WF33:
  "technisch präzise Schnellansicht"): a row per fact with its own short name ("Removes",
  "To revision", "Image"), the value terse, names bold, what is lost bold in the error colour, what
  to weigh in the warning colour. A command gives them as `Spec.Effects` (`engine.Effect`); a name
  never stands inside a sentence, where it reads as a word ("Removes key added.").
- **A state the user must act on stands where they act**: a drawer's unsaved edits as pills
  beside Revert and Save, not a muted line under them (WF31).
- **No Kubernetes lectures** ("a workload that reads a removed key no longer finds it"): what is
  lost is said by the red **"This cannot be undone."**, nothing more.
- **No raw `resourceVersion`** in the window: an object's state is "since your edits began", "its
  current state", a time ("Saved 1 value at 14:02:05"); the number stays in what agents, the CLI
  and the audit log read.
- Status lines and source lines are **short**: details go into a tooltip.
- The YAML of an object shows `apiVersion`, `kind`, `metadata` (`name`, `namespace` first) before
  the rest, as the Kubernetes docs write a manifest ([D-86](decisions.md#d-86-the-order-of-an-objects-yaml)).
- A suggested file name names a cluster by the short name the window shows (D-68), never the
  kubeconfig file it comes from.

## Decisions

The owner decides product direction, scope, security posture and anything irreversible or
outward-facing, one question at a time, each with its options weighed and a recommendation. The
lead (and an agent working for the lead) decides the details below that and records each one in
the page where it lives: the question, the options weighed, the choice and why, marked as the
lead's decision and open to revision ([D-74](decisions.md#d-74-how-the-leads-decisions-are-reviewed),
the owner, 2026-10-17). No confirmation queue: a detail stands as built until a walk in a real
window, use or someone's feedback changes it. A choice that only a report, a chat or a commit
message names is not documented yet: write it where it lives in the same change. A decision that
changes what the owner sees goes into the walk script of the next walk.

## The docs show the current state

The owner's rule of 2026-10-15 ([`AGENTS.md`](../../AGENTS.md#the-other-rules), rule 10): every
change, patch and fix updates the documentation it affects in the same commit as the code, so that
no page describes yesterday's Cogpit. Before committing, search the docs for what the change
touches (its command ids, the decision, the `H-<n>`, "not built", "planned", "none yet") and
correct every sentence that no longer holds: the parity row, the gap and the numbers they feed
([rules](../parity/README.md#rules-for-every-progress-page)); the decision's status in
[`decisions.md`](decisions.md) ("decided, not built" becomes "built", with its evidence); the
security page whose mechanism changed; [architecture › Current state](architecture.md#current-state-2026-10-19);
[`usage.md`](usage.md) for what a user sees; the package table in [`README.md`](README.md) for a
new package; the table in [`commands.md`](commands.md) for a new or changed command; the current
focus in [`roadmap.md`](../parity/roadmap.md) when the change closes or moves one of its items.
Dated history (revision rows, "built 2026-10-13") stays as it was; a sentence about the present
that the change makes false does not.

## Commits and pull requests

Small, focused commits in the [Conventional Commits](https://www.conventionalcommits.org/) form
the team's other repositories use (`feat:`, `fix:`, `chore:`, `test:`, `docs:`, with a scope where
it helps: `fix(cache): …`), because semantic-release derives the next version from them and,
since 2026-10-13, publishes it: every `feat:` and `fix:` on `main` is released once CI is green
([releasing](releasing.md#release-flow)). No commit carries `!` or a `BREAKING CHANGE:` footer until
the owner wants 1.0.0; a body line that starts with `BREAKING CHANGE:` counts as such a footer too
(commit `f3ad000` has one). Releases continue from the anchor tag `v0.0.0`, and until 1.0
`.releaserc.json` turns a breaking change into a minor release. Commits and pull requests carry no `Co-Authored-By` trailers and no
"Generated with" lines (owner, 2026-10-11). A pull request that moves a parity number updates the progress docs in
the same change ([rules](../parity/README.md#rules-for-every-progress-page)).

**A commit that only changes documentation carries `[skip ci]`** in its message (owner,
2026-10-15: the pipeline needs not run for it; the runner is one shared machine). GitHub
recognises `[skip ci]` and `[ci skip]` only, and skips a push by its newest commit's message: a
docs-only commit is never the newest commit of a push that also carries code (push code with a
code commit on top, or the docs commit on its own). Run `go run ./hack/parity` before such a
commit, since CI's parity job does not run for it. Renders and the generated `commands.md` that
come with a code change are part of that change, not docs-only.

The root README is the showcase page (owner, 2026-10-14); the reference lives in
[`usage.md`](usage.md) and [`commands.md`](commands.md). A change that adds or changes a command
regenerates the table in `commands.md` with `go run ./hack/readmecmds` in the same change.
