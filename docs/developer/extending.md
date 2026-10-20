# Extending Cogpit

Ordered checklists for the common additions. The packages they name are in
[`architecture.md`](architecture.md#2-repository-layout-planned); which of them exist is in its
[Current state](architecture.md#current-state-2026-10-19). Worked examples: the commands in
[`internal/engine/commands`](../../internal/engine/commands/) and the kind profiles in
[`internal/model/kinds`](../../internal/model/kinds/).

## Adding a command

1. **Find the action in Lens.** Look it up in [`resource-parity.md`](../parity/resource-parity.md)
   or [`ui-parity.md`](../parity/ui-parity.md) and use the id given there. If the row has none,
   name it `<kind-or-area>.<action>` (`statefulset.restart`, `cluster.disconnect`) and add the id to
   the row in the same change.
2. **Logic** goes in the lowest package that fits: row and status rules in `model`, Kubernetes
   calls in `cluster`, Helm in `helm`, using the kubectl libraries where `kubectl` already has the
   behaviour (`k8s.io/kubectl/pkg/drain`, rollout). Unit-test it there.
3. **Command** goes in `internal/engine/commands/<area>.go` and registers:
   - `id`, `label`, `menu path`, the kinds it applies to and Lens's default shortcut, if any;
   - `Order`, its place in the kind's row menu and drawer ([architecture §4.2](architecture.md#42-the-command-registry)),
     with `DrawerOnly` when Lens shows it in the drawer but not in the row menu (Copy);
     `Enabled` wraps `engine.ErrNotApplicable` when the command does not apply to the object at all
     (Cordon on a cordoned node), so that the menu hides it; `Escalate` when some invocations of a
     mutating command are destructive; `AgentsAsk` when some invocations by an agent must wait for
     the user even where agent writes are allowed (a role binding: a command that can write one
     declares it, [H-27](../security/privilege-footprint.md#h-27-a-binding-is-judged-by-its-roles-name-and-the-cache));
     `Prefill` when a dialog should start from cached values; `Dialog` when a field of the
     generated dialog needs a hint for a person (the schema's description speaks to agents) or a
     whole number a slider over the range most values fall in (`engine.DialogField`,
     [D-104](decisions.md#d-104-dialog-fields-their-hints-and-dropdowns-the-gate-walks-gw2-gw5-gw6));
   - for a command that asks the user ([`registry.go`](../../internal/engine/registry.go) `Spec`):
     `Targets`, the objects the confirmation names and binds by UID; `Effects`, a row each for what
     the objects do not show (`engine.Effect`: a short name, a terse value and a tone, "Removes"
     the keys a save loses, the drain's options that lose data), never a sentence
     ([D-100](decisions.md#d-100-objects-and-effects-in-one-form-and-a-drawers-edited-state-beside-save));
     a dialog of the command's own names its objects with `placeRows` and `objectsRow`
     ([`objectnames.go`](../../internal/ui/fyneui/objectnames.go)); `Warning`, what the user must
     know before answering, in the warning colour; `Review`, a text of the parameters to read (an
     agent's Helm values); `Undoable` for a destructive command that loses nothing (a grant, a
     rollback), whose confirmation then leaves out "This cannot be undone."; `Refuse` to refuse an
     origin before anyone is asked; `GUIOnly` for a command only the user's own action in the
     window runs (a new GUI-only command needs a decision of the owner,
     [conventions](conventions.md#commands-not-handlers));
   - a parameter struct with `json` and `jsonschema` tags (the schema agents read);
   - **its cluster** (several clusters are connected at once since 2026-10-16,
     [D-69](decisions.md#d-69-several-clusters-connected-at-once), [architecture §4.3](architecture.md#several-clusters-connected-d-69)):
     a command on an object takes the cluster of its `ref`'s context, one on a dock tab its tab's
     (the parameter type implements `engine.DockTargeted`), and one that reads or acts on a cluster
     without either embeds `engine.OnCluster`, an optional `context`, the shown cluster by default.
     Its `enabled` and `run` reach the backend through that context (`connectedOn`, `writableOn`,
     `streamableOn` in [`commands.go`](../../internal/engine/commands/commands.go),
     `Session.TargetBackend`, `KindIn`, `ObjectsIn`), never through `Session.Backend()`, which is
     the window's shown cluster; only commands on the window's own state (`ui.*`, the open view)
     use that;
   - `enabled`, returning the reason when disabled;
   - `verbs` (every API verb and resource it uses) and `danger` (`Read`, `UI`, `Mutating`,
     `Destructive`). When the parameters name an object (`ref`), the registry asks the access
     reviews about each verb and greys the command "not permitted" on a denial, so declare the
     verb its purpose needs (Edit declares `update`, which its tab ends in);
   - `run`, returning a JSON-serializable result. Reads come from the cache. A result that carries
     cluster text (YAML, log lines, values) an agent reads passes `Dispatch`'s agent filter: a
     result type of `engine` is listed in `Session.forAgent`, one of `commands` implements
     `engine.AgentResult` and filters its text with the `AgentView` it gets (`Text`, `YAML`,
     `TabText`, `Detail`), so that the cached Secrets' values and Flux's credential fields stay
     out ([architecture §7](architecture.md#7-mcp-and-the-built-in-agent), agents-and-mcp H-82,
     H-63). A command that takes text an agent wrote calls `GuardSecretText` or
     `GuardSecretEditor`, which also refuse the placeholders Cogpit showed in place of a value.
     A read that withholds a value by who asks never asks the origin alone: it asks
     `engine.ReadsLikeUser(ctx)` (withheld from everyone but the window) or
     `engine.ReadsAsAgent(ctx)` (withheld from agents only), so that the built-in agent over a
     private connection reads what the window shows (D-10, agents-and-mcp H-94); a write rule
     keeps asking `engine.OriginOf(ctx)`.
   Cluster writes go through an interface the backend's writer implements: `*cluster.Conn` in
   the app, a fake in tests. Each area declares its own interface in its own engine file (as
   `engine.Actions` does for the MVP's actions) and reaches it with a type assertion on
   `Backend.Writer()`, so that two areas never edit the same interface; the test fake gets the
   area's methods in the area's own test file.
4. **Multi-step mutations** (several API calls in a row) are crash-safe
   ([conventions](conventions.md#crash-tolerant-crash-safe)): a worker process, or detection of the
   half-finished state on the next start (the drain's and Helm's journals). Work that outlives the
   command's call (waiting for a controller, as Reconcile with Source waits for Flux's source)
   runs as a background job: `Session.StartJob` with a `JobSpec`
   ([`jobs.go`](../../internal/engine/jobs.go)) opens the job's dock tab, whose steps agents read in
   `ui_state`, and the command returns at once; `job.cancel` (the tab's Cancel), closing the tab, a
   cluster change or the session's end cancels the job. Pass the command's context as
   `JobSpec.Parent`, so that the job keeps the origin and the confirmation's UIDs. Jobs are not
   journaled, so the command says why a crash in the middle is harmless
   ([`fluxsourcejob.go`](../../internal/engine/commands/fluxsourcejob.go)); an operation that must
   survive a crash keeps its own journal around the job, as the drain does
   ([`drainjob.go`](../../internal/engine/commands/drainjob.go)): record it before the job starts
   (and forget it if the job does not start), let the job's stops end in the journal as an entry to
   offer, clear it when the work is done. Steps learnt while the job runs go in with `Job.SetSteps`. A new action on a
   selection (a required `refs`, one request per object) goes through `runMany` with a
   `manyAction` ([`manyjob.go`](../../internal/engine/commands/manyjob.go)): at most ten objects
   within the call, more as a job with a step per object
   ([D-63](decisions.md#d-63-long-commands-as-background-jobs)).
5. **Respect the context:** cluster context and namespace, a multi-selection (a row inside the
   selection acts on the whole selection), the user's permissions, and the origin's confirmation
   rules.
6. **Tests:** behaviour against fake clients, the requests sent, disabled states with their
   reasons, bad params returning errors, confirmation for each origin, and the fuzz target. Add
   the command's id and danger to `wantCommands` in
   [`commands_test.go`](../../internal/engine/commands/commands_test.go).
7. **UI and MCP** follow from the registry: menus, context menus, the palette and the MCP tool
   appear without UI code. With an `Order`, the command is in its kinds' row menu and drawer
   toolbar ([architecture §4.2](architecture.md#42-the-command-registry)); a `container`
   parameter makes it a submenu of the pod's containers; required parameters beyond `ref` and
   `container` get a schema-generated dialog (integer, number, string and boolean fields, each as
   wide as what it holds, its hint the tooltip of a "?" after its name: `Spec.Dialog`'s, else the
   `jsonschema` tag; a slider beside a whole number where `Spec.Dialog` gives one), started from
   `Prefill`; a command with a `Prefill` gets the dialog for its optional parameters too. Add a custom dialog only when the schema
   can't express it. Give the command an icon in the drawer's toolbar in
   [`cmdicons.go`](../../internal/ui/fyneui/cmdicons.go); others get a generic one. The command
   palette lists an object action with its object, and any other command with a `MenuPath`
   ([rules](architecture.md#42-the-command-registry)); one with a required parameter beyond its
   object (a context, a name, a file) is listed only where
   [`paletteRunners`](../../internal/ui/fyneui/palette.go) opens a dialog for it. A `Shortcut` such as
   `Cmd+Shift+P` (Cmd is Ctrl on Windows and Linux) is shown in the palette; check it against
   `TestRegistryShortcutsDoNotCollide` and bind it where the window binds the others.
8. **Command table:** regenerate the table in [`commands.md`](commands.md) with
   `go run ./hack/readmecmds` and replace the content of its details block with the output (do not
   edit the table by hand).
9. **Progress docs:** flip the row in the parity page and update [`gaps.md`](../parity/gaps.md);
   `make parity` checks the row and the numbers it feeds.

## Adding an integration (what Lens calls an extension)

Integrations are built into Cogpit's code ([D-5](decisions.md#d-5-extensions)); there is no
mechanism for external extensions. An integration such as Flux CD or Argo CD is a set of kind
profiles (below), its commands (above) and, where Lens has one, a dashboard view. The plan was a
package of its own under `internal/`; Flux CD, the first built (2026-10-13, G23), showed that the
existing layers hold it without one, each part in its own files of the package whose layer it
belongs to:

1. **Curated profiles** of the integration's CRD kinds in `internal/model/kinds/<integration>*.go`,
   appended to `curated` by the file's `init` (as [`flux.go`](../../internal/model/kinds/flux.go)
   does), keyed `<plural>.<group>`, at the version the integration serves with older ones as
   `FallbackVersions`; `kinds.ProfileFor` gives a definition at another version the generic
   profile. Goldens per kind in `testdata/<key>/`, run by `TestCuratedGolden` and
   `TestCuratedTypeConfusion`.
2. **Commands** in `internal/engine/commands/<integration>.go` with `Kinds` set to the curated
   keys, writes through an interface of the engine that `*cluster.Conn` implements
   ([`engine/flux.go`](../../internal/engine/flux.go), [`cluster/flux.go`](../../internal/cluster/flux.go)),
   bound to the object's UID; a `refs` variant for selections.
3. **A dashboard** as a page: a key `<group>/<page>` with its title in `pageTitles`, a worker in
   `internal/engine/<integration>dashboard.go` hooked into `state` (views.go) as the Overviews are,
   its read command, and its panel in `internal/ui/fyneui` (`tableview.go`'s page layout).
4. **The navigator group** in `internal/ui/fyneui/<integration>nav.go` (Flux's
   [`fluxnav.go`](../../internal/ui/fyneui/fluxnav.go); Argo CD's group sits in
   [`argocd.go`](../../internal/ui/fyneui/argocd.go)), one line in `navLayout`
   ([`sidebar.go`](../../internal/ui/fyneui/sidebar.go)); a group of pages alone is left out, so the
   section goes with the integration's CRDs.

## Adding a built-in kind profile

A custom resource kind without a curated profile already gets a generic view from its definition
(its printer columns); a built-in kind without a profile gets no view at all
([architecture › Current state](architecture.md#current-state-2026-10-19)). A profile makes a kind
match Lens.

1. Take the columns, status rules and actions from the kind's row in
   [`resource-parity.md`](../parity/resource-parity.md).
2. Add `internal/model/kinds/<kind>.go`: columns (title, value, sort key, default visibility), the
   status token rule, searchable fields for agents (`clusterIP`, `podIP`, `node`), and the detail
   sections. The drawer starts with Lens's Properties section, which the engine fills with the
   metadata; a first section titled `model.TitleProperties` adds the kind's own properties to it.
   Items that name objects carry a `Link`, lists of short values `Chips`, long lists a `Fold`
   noun ([`model/detail.go`](../../internal/model/detail.go)).
3. Decide what the cache needs ([architecture § Scopes](architecture.md#scopes-local-and-virtual-kinds)):
   full objects, or metadata only (`MetadataOnly`, when no column or drawer item needs more), and a
   `LabelSelector` if the kind is a labelled subset (it then reads from the unselected scope of the
   same resource while that runs: covering scopes, as Helm's releases read from the Secrets). A column or drawer section that shows objects of another kind
   declares the index it reads in `Needs` (add the index to
   [`kinds/related.go`](../../internal/model/kinds/related.go) if it is new) and reads it in
   `CellsWith` or `DetailWith` ([architecture §4.4](architecture.md#44-tables-details-editors-and-streams)).
   Objects Cogpit makes itself are a local kind (`Local`, listed in `kinds.Local()`, written with
   `Backend.PutLocal`); objects made from other cached objects are a virtual kind (`Derive`). A
   virtual kind made of several kinds' objects cannot derive from one scope: it gets a cache of its
   own over `cache.Held`, filled while it is read, as the Applications are
   ([architecture § Scopes](architecture.md#scopes-local-and-virtual-kinds),
   [`engine/applications.go`](../../internal/engine/applications.go)).
4. Golden tests from recorded objects in `internal/model/kinds/testdata/<kind>/`: healthy,
   failing, terminating, and an object with optional fields missing (`minimal`). `TestGolden`
   and `TestTestdataComplete` pick them up; run `go test ./internal/model/kinds/ -update` and read
   the golden diff. The kind's own `internal/model/kinds/<kind>_test.go` holds its fuzz target
   (`func FuzzCronJobs(f *testing.F) { fuzzProfile(f, "cronjobs") }`) and its status assertions
   (`checkStatusTexts(t, []statusText{...})`), so that adding a kind edits no shared test table.
5. Cells that name another object carry a link (`linkTo`; `namespaceCell` already links the
   namespace); a view with Lens's warning icon has `warningColumn` right after Name, filled with
   `warningCell`. Mark any rule that is not captured from Lens as inferred in the parity row.
6. Register the profile with its group, version and resource: one line in `builtin` in
   [`kinds.go`](../../internal/model/kinds/kinds.go). Discovery decides at runtime whether the
   cluster serves it; a kind that older clusters serve at another version of the same shape lists
   those in `FallbackVersions`, preferred first. Built-in kinds keep the bare plural as their key; kinds from custom resource
   definitions are `<plural>.<group>` ([architecture §4.5](architecture.md#45-kind-keys)).
   A curated profile of a kind from a custom resource definition (the Vertical Pod Autoscalers)
   keeps that key and goes in `curated` in
   [`verticalpodautoscalers.go`](../../internal/model/kinds/verticalpodautoscalers.go), not in
   `builtin`: the custom kinds watch takes it through `kinds.ProfileFor` while the cluster's
   definition serves its version; its goldens are `TestCuratedGolden`'s.
7. Add its actions as commands (above).

## Adding a cache consumer

1. Acquire a handle for the (cluster, kind, namespace scope) you show; release it when the view,
   drawer or query ends. The cache keeps the informer warm for the grace period after the last
   release.
2. Read snapshots; never start an informer, a watch or a poll yourself.
3. Add a test with the counting round-tripper that asserts the API requests your consumer costs
   (budgets P9–P11 in [`testing.md`](testing.md#performance-budgets-proposed-not-measured)).

## Adding a package

1. Pick its layer in [`architecture.md`](architecture.md#3-dependency-layering-enforced) and add its
   `depguard` rule to `.golangci.yml` in the same change, and the new package to the deny lists of
   the rules that must keep other packages off it.
2. Give it a package doc comment.
3. Add a row to the package table in [`README.md`](README.md) and to
   [architecture › Current state](architecture.md#current-state-2026-10-19), and a line to the
   package map in [`AGENTS.md`](../../AGENTS.md#2-package-map).

## Changing the logo

Every version of the logo stays in [`internal/ui/logo`](../../internal/ui/logo/versions.go)
([D-24](decisions.md#d-24-logo)): a new one is appended, none is replaced.

1. Design it in an icon lab, an interactive claude.ai artifact built for the purpose (the one of
   2026-10-15 is deleted): presets, a control per `Look` field, the wheel under the C's glow, a
   Dock preview against the current logo, and two exports, the settings as JSON and the SVG.
2. Save them as `docs/developer/assets/logo/<name>.json` and `<name>.svg`.
3. For a C helm, add a `Look` with the settings to [`versions.go`](../../internal/ui/logo/versions.go):
   each field is the lab's setting of the same name. A drawing of another kind gets its own
   `layers` function, as [`gauge.go`](../../internal/ui/logo/gauge.go) has. Append its `Version`
   (name, date, what it is) to `Versions` and point `Current` at it.
4. Add the look to `labLooks` in [`logo_test.go`](../../internal/ui/logo/logo_test.go), which holds it
   to its JSON and its SVG.
5. `make icon`, then copy `packaging/icon.png` to `docs/developer/assets/logo/<name>.png`:
   `TestEveryVersionDrawsItsPicture` and `TestPackagedIconIsCurrent` check both.
6. `make showcase`: the README's banner, the social preview and every picture that shows the header.
7. Record the choice in D-24 (the settings, where the logo shows), and upload
   `docs/images/social_preview_dark.png` under the repository's Settings › General › Social
   preview, by hand: GitHub's API cannot set it.

To bring an earlier version back, point `Current` at it and run steps 5 to 7 (its picture is
already there; skip the copy).
