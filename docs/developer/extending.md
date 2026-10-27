# Extending Cogpit

Ordered checklists for the common additions. Packages and layers:
[architecture §2](architecture.md#2-repository-layout) and
[§3](architecture.md#3-dependency-layering-enforced). Worked examples: the commands in
[`internal/engine/commands`](../../internal/engine/commands/) and the kind profiles in
[`internal/model/kinds`](../../internal/model/kinds/).

## Adding a command

1. **Id.** Use the one in the action's row in [`resource-parity.md`](../parity/resource-parity.md)
   or [`ui-parity.md`](../parity/ui-parity.md). Without one, name it `<kind-or-area>.<action>` in
   lowerCamelCase segments (`statefulset.restart`, `cluster.disconnect`) and add it to the row in
   the same change.
2. **Logic** goes in the lowest package that fits, with its unit tests: row and status rules in
   `model`, API calls in `cluster`, Helm in `helm`. Reuse `k8s.io/kubectl` where kubectl has the
   behaviour (the drain runs `k8s.io/kubectl/pkg/drain`,
   [`cluster/nodes.go`](../../internal/cluster/nodes.go)), else do what kubectl does (Restart sets
   `kubectl rollout restart`'s annotation, [`cluster/actions.go`](../../internal/cluster/actions.go)).
3. **Writes** go through an interface the backend's writer implements: `*cluster.Conn` in the app,
   a fake set as `Backend.Actions` in tests. Each area declares its own interface in its own engine
   file (as `NodeActions` in [`nodeactions.go`](../../internal/engine/nodeactions.go)) and reaches it
   with a type assertion on `Backend.Writer()`; the test fake `fakeWriter`
   ([`fixture_test.go`](../../internal/engine/commands/fixture_test.go)) gets the area's methods in
   the area's own test file. No two areas edit one interface.
4. **Register** the command in `internal/engine/commands/<area>.go`: a `register<Area>(g)` that
   calls `add(g, engine.Spec[P]{…})`, and one line in `RegisterAll`
   ([`commands.go`](../../internal/engine/commands/commands.go)). `P` is a struct with `json` and
   `jsonschema` tags: the schema agents read. Every command sets `ID`, `Label`, `Description` (for
   agents), `Danger` (`DangerRead`, `DangerUI`, `DangerMutating`, `DangerDestructive`), `Verbs`
   (every API verb and resource it uses), `Enabled` (the reason when it cannot run) and `Run` (a
   JSON-serializable result; reads from the cache); `MenuPath`, `Kinds` and Lens's `Shortcut` where
   they apply. `engine.Register` refuses a spec that breaks the registry's rules. Each field's
   meaning: [architecture §4.2](architecture.md#42-the-command-registry). Set the others when:

   | Field | Set it when |
   |---|---|
   | `Order`, `DrawerOnly` | the command is one of its kinds' actions in the row menu and the drawer's toolbar, below `OrderEdit` (900) in Lens's order; `DrawerOnly` for one the drawer shows and the row menu does not (Copy) |
   | `Enabled` wrapping `engine.ErrNotApplicable` | the command does not apply to the object at all (Cordon on a cordoned node): menus hide it instead of greying it |
   | `Targets` | always for mutating and destructive commands: the objects the confirmation names and binds by UID |
   | `Escalate` | some invocations of a mutating command are destructive (Scale to zero) |
   | `AgentsAsk` | an agent's call must wait for the user even where agent writes are allowed: every command that can write a role or a binding declares it ([H-27](../security/privilege-footprint.md#h-27-a-binding-is-judged-by-its-roles-name-and-the-cache)) |
   | `Effects` | the confirmation needs rows for what the objects do not show: `engine.Effect`, a short name, a terse value and a tone, never a sentence (the keys a save removes, the drain's options that lose data; [D-100](decisions.md#d-100-objects-and-effects-in-one-form-and-a-drawers-edited-state-beside-save)). A dialog of the command's own names its objects with `placeRows` and `objectsRow` ([`objectnames.go`](../../internal/ui/fyneui/objectnames.go)) |
   | `Warning`, `Review` | the user must know something before answering (shown in the warning colour); the parameters need reading (an agent's Helm values) |
   | `Undoable` | a destructive command loses nothing (a grant, a rollback): its confirmation leaves out "This cannot be undone." |
   | `Refuse` | an origin is refused before anyone is asked |
   | `GUIOnly` | only the user's own action in the window may run it; a new GUI-only command needs the owner's decision ([conventions](conventions.md#commands-not-handlers)) |
   | `Prefill`, `Dialog` | a dialog starts from cached values; a field needs a hint for a person (the schema's description speaks to agents) or a slider over the range most whole numbers fall in (`engine.DialogField`, [D-104](decisions.md#d-104-dialog-fields-hints-dropdowns-and-button-outlines)) |
   | `MoreKeys`, `PlatformKeys` | further keys no menu shows (add the command to `moreKeyRuns`, [`menukeys.go`](../../internal/ui/fyneui/menukeys.go), which binds them from any focus); keys that differ per GOOS, the first the shown one ([`engine/keys.go`](../../internal/engine/keys.go), [D-134](decisions.md#d-134-the-windows-zoom)) |

5. **Its cluster** ([D-69](decisions.md#d-69-several-clusters-connected-at-once),
   [architecture](architecture.md#d-69-several-clusters-connected)): a command on an object acts on
   its `ref`'s context, one on a dock tab on its tab's (the parameters implement
   `engine.DockTargeted`), any other embeds `engine.OnCluster` (an optional `context`, the shown
   cluster by default). `Enabled` and `Run` reach the backend through that context (`connectedOn`,
   `writableOn`, `streamableOn` in [`commands.go`](../../internal/engine/commands/commands.go);
   `Session.TargetBackend`, `KindIn`, `ObjectsIn`), never through `Session.Backend()`, the shown
   cluster, which only commands on the window's own state (`ui.*`, the open view) use.
6. **The context it runs in:** the namespace; a selection (a selected row's menu offers the
   selection's actions, so an action that makes sense on many objects gets a `refs` variant); the
   user's permissions (when the parameters name objects, `ref` or `refs`, the registry asks the
   access reviews about every declared verb and greys the command "not permitted" on a denial, so
   declare the verb its purpose needs: Edit declares `update`); the origin's confirmation rules.
7. **What agents read and write** ([architecture §7](architecture.md#7-mcp-and-the-built-in-agent),
   [agents and MCP](../security/agents-and-mcp.md) H-82, H-63, H-94):
   - A result with cluster text (YAML, log lines, values) passes `Dispatch`'s agent filter: a
     result type of `engine` is listed in `Session.forAgent`
     ([`agentview.go`](../../internal/engine/agentview.go)); one of `commands` implements
     `engine.AgentResult` and filters its text with the `AgentView` it gets (`Text`, `Lines`,
     `YAML`, `TabText`, `Detail`), so that the cached Secrets' values and Flux's credential fields
     stay out.
   - Text an agent wrote goes through `GuardSecretText` or `Session.GuardSecretEditor`
     ([`secretedit.go`](../../internal/engine/secretedit.go)), which also refuse the placeholders
     Cogpit shows in place of a value.
   - A read that withholds a value by who asks calls `engine.ReadsLikeUser(ctx)` (withheld from all
     but the window) or `engine.ReadsAsAgent(ctx)` (from agents only), so that the built-in agent
     over a private connection reads what the window shows
     ([`readslikeuser.go`](../../internal/engine/readslikeuser.go),
     [D-10](decisions.md#d-10-built-in-agent)); a write rule asks `engine.OriginOf(ctx)`.
8. **Long and multi-step work** is crash-safe
   ([conventions](conventions.md#crash-tolerant-crash-safe)):
   - Work that outlives the call (waiting for a controller) runs as a background job:
     `Session.StartJob` with a `JobSpec` ([`jobs.go`](../../internal/engine/jobs.go)) opens the job's
     dock tab, whose steps agents read in `ui_state`, and the command returns at once. Pass the
     command's context as `JobSpec.Parent` (the job keeps the origin and the confirmed UIDs); report
     progress with `Job.SetStep`, and steps learnt on the way with `Job.SetSteps`. `job.cancel` (the
     tab's Cancel), closing the tab, its cluster's connection interrupted, its login lapsing, its
     cluster disconnected or the session's end stops the job; showing another cluster does not.
   - Jobs are not journaled: the command's comment says why a crash midway is harmless
     ([`fluxsourcejob.go`](../../internal/engine/commands/fluxsourcejob.go)). An operation that must
     survive a crash keeps its own journal around the job, as the drain does
     ([`drainjob.go`](../../internal/engine/commands/drainjob.go)): recorded before the job starts
     (dropped if it does not start), a stop kept as an entry to offer, cleared when the work is done.
   - An action on a selection (a required `refs`, one request per object) runs through `runMany`
     with a `manyAction` ([`manyjob.go`](../../internal/engine/commands/manyjob.go)): up to
     `engine.DirectSelection` (10) objects within the call, more as a job with a step per object
     ([D-63](decisions.md#d-63-long-commands-as-background-jobs)).
9. **Tests:** behaviour against fake clients, the requests sent, disabled states with their
   reasons, bad parameters returning errors, confirmation for each origin. Add the id and its danger
   to `wantCommands` in [`commands_test.go`](../../internal/engine/commands/commands_test.go):
   `TestRegistryComplete` also checks the label, a description of at least 20 characters, an object
   schema and the verbs of every write. `FuzzDispatch`
   ([`fuzz_test.go`](../../internal/engine/commands/fuzz_test.go)) feeds every registered command
   adversarial parameters from every origin.
10. **UI and MCP** follow from the registry: the MCP tool, the menus, the row menus and the palette
    need no UI code.
    - With an `Order` the command is in its kinds' row menu and drawer toolbar. A `container`
      parameter makes it a submenu of the pod's containers. Required parameters beyond `ref` and
      `container` (with a `Prefill`, the optional ones too) get a generated dialog of integer,
      number, string and boolean fields, each as wide as what it holds, its hint the tooltip of a
      "?" after its name ([`paramdialog.go`](../../internal/ui/fyneui/paramdialog.go)); another type
      needs a dialog of its own.
    - Its icon in the drawer's toolbar goes in [`cmdicons.go`](../../internal/ui/fyneui/cmdicons.go);
      without one it gets the generic icon.
    - The palette lists an object action with its object and any other command with a `MenuPath`;
      one with a required parameter beyond its object only where the window runs it its own way
      ([`paletteRunners`](../../internal/ui/fyneui/palette.go) or a create dialog;
      [rules](architecture.md#the-command-palette)).
    - A `Shortcut` (`Cmd` is Ctrl on Windows and Linux) must pass
      `TestRegistryShortcutsDoNotCollide` and `TestKeysDoNotCollideOnAnyPlatform`. Bind it in the
      window: a main-menu item takes it from any focus (`mainMenu` in
      [`shell.go`](../../internal/ui/fyneui/shell.go)); a key a focused widget may keep goes in
      `windowKeys` ([`menukeys.go`](../../internal/ui/fyneui/menukeys.go);
      [keys](architecture.md#keys)).
11. **Docs:** regenerate the table in [`commands.md`](commands.md) with `go run ./hack/readmecmds`
    (never edit it by hand); flip the parity row and update [`gaps.md`](../parity/gaps.md);
    `make parity` checks the row and the numbers it feeds.

## Adding an integration (what Lens calls an extension)

Integrations such as Flux CD and Argo CD are part of Cogpit's code; there is no mechanism for
external extensions ([D-5](decisions.md#d-5-extensions)). An integration is a set of kind profiles,
its commands and, where Lens has one, a dashboard, each part in its own files of the package whose
layer it belongs to; there is no integration package. (A cluster tool Cogpit reads with a switch per
cluster, as Prometheus or Trivy, is an `engine.Integration`:
[architecture §4.7](architecture.md#47-integrations-d-76-d-82).)

1. **Curated profiles** of its CRD kinds in `internal/model/kinds/<integration>*.go`, appended to
   `curated` by the file's `init` (as [`flux.go`](../../internal/model/kinds/flux.go) does), keyed
   `<plural>.<group>`, at the version the integration serves, older ones as `FallbackVersions`;
   `kinds.ProfileFor` gives a definition at any other version the generic profile. Goldens per kind
   in `testdata/<key>/`, run by `TestCuratedGolden` and `TestCuratedTypeConfusion`.
2. **Commands** in `internal/engine/commands/<integration>.go` with `Kinds` set to the curated keys,
   bound to the object's UID, a `refs` variant for selections; writes through an engine interface
   `*cluster.Conn` implements ([`engine/flux.go`](../../internal/engine/flux.go),
   [`cluster/flux.go`](../../internal/cluster/flux.go)).
3. **A dashboard** as a page: a key `<group>/<page>` with its title in `pageTitles`, a worker in its
   own engine file ([`fluxdashboard.go`](../../internal/engine/fluxdashboard.go)) held in `state`
   ([`views.go`](../../internal/engine/views.go)) as the Overviews' are, its read command, and its
   panel in `internal/ui/fyneui` ([`fluxdashboard.go`](../../internal/ui/fyneui/fluxdashboard.go)),
   placed by [`tableview.go`](../../internal/ui/fyneui/tableview.go)'s page layout.
4. **The navigator group** in `internal/ui/fyneui/<integration>nav.go`
   ([`fluxnav.go`](../../internal/ui/fyneui/fluxnav.go); Argo CD's sits in
   [`argocd.go`](../../internal/ui/fyneui/argocd.go)), one line in `navLayout`
   ([`sidebar.go`](../../internal/ui/fyneui/sidebar.go)). A group whose kinds the cluster does not
   serve is left out with its pages.

## Adding a built-in kind profile

A custom resource kind without a curated profile gets a generic view from its definition's printer
columns; a built-in kind without a profile gets no view
([architecture › Packages](architecture.md#packages)). A profile makes a kind match Lens.

1. Take the columns, status rules and actions from the kind's row in
   [`resource-parity.md`](../parity/resource-parity.md).
2. Add `internal/model/kinds/<kind>.go` ([`model/profile.go`](../../internal/model/profile.go) has
   the fields): `Columns` (id, title, type, default visibility), `Cells` (each cell's text and sort
   key, the row's status token), `Fields` (what agents search by with `resources_find`:
   `clusterIP`, `podIP`, `node`) and `Detail` (the drawer's sections). The engine fills the
   drawer's first section, Properties, with the metadata; a first section titled
   `model.TitleProperties` adds the kind's own properties to it. Items that name objects carry a
   `Link`, lists of short values `Chips`, long lists a `Fold` noun
   ([`model/detail.go`](../../internal/model/detail.go)).
3. Decide what the cache holds
   ([architecture § Scopes](architecture.md#scopes-local-and-virtual-kinds)):
   - full objects, or `MetadataOnly` when no column or drawer item needs more;
   - a `LabelSelector` for a labelled subset, which reads from the unselected scope of the same
     resource while that runs (covering scopes: Helm's releases read from the Secrets);
   - a column or drawer section that shows another kind's objects declares the index it reads in
     `Needs` (`DetailNeeds` when only the drawer reads it; a new index goes in
     [`kinds/related.go`](../../internal/model/kinds/related.go)) and reads it in `CellsWith` or
     `DetailWith` ([architecture §4.4](architecture.md#44-tables-details-editors-and-streams));
   - objects Cogpit makes itself are a local kind (`Local`, listed in `local` in
     [`kinds.go`](../../internal/model/kinds/kinds.go), written with `Backend.PutLocal`); objects
     made from other cached objects a virtual kind (`Derive`). A virtual kind made of several kinds'
     objects cannot derive from one scope: it gets a cache of its own over `cache.Held`, filled
     while it is read, as the Applications do
     ([`engine/applications.go`](../../internal/engine/applications.go)).
4. Golden tests from recorded objects in `internal/model/kinds/testdata/<kind>/`: `healthy`,
   `failing`, `terminating` and `minimal` (optional fields missing), which `TestTestdataComplete`
   requires and `TestGolden` renders; run `go test ./internal/model/kinds/ -update` and read the
   golden diff. The kind's own `internal/model/kinds/<kind>_test.go` holds its fuzz target
   (`func FuzzCronJobs(f *testing.F) { fuzzProfile(f, "cronjobs") }`) and its status assertions
   (`checkStatusTexts(t, []statusText{…})`), so that adding a kind edits no shared test table.
5. Cells that name another object carry a link (`linkTo`; `namespaceCell` links the namespace); a
   view with Lens's warning icon has `warningColumn` right after Name, filled with `warningCell`
   ([`common.go`](../../internal/model/kinds/common.go)). Mark a rule not captured from Lens as
   inferred in the parity row.
6. Register it: one line in `builtin` in [`kinds.go`](../../internal/model/kinds/kinds.go) (the
   profile names its group, version and resource; `TestBuiltinProfiles` checks what every built-in
   profile must have). Discovery decides at runtime whether the cluster serves it; a kind older
   clusters serve at another version of the same shape lists those in `FallbackVersions`,
   preferred first. Built-in kinds keep the bare plural as their key, kinds from custom resource
   definitions `<plural>.<group>` ([architecture §4.5](architecture.md#45-kind-keys)). A curated
   profile of a kind from a custom resource definition (the Vertical Pod Autoscalers) keeps that
   key and goes in `curated`
   ([`verticalpodautoscalers.go`](../../internal/model/kinds/verticalpodautoscalers.go)), not in
   `builtin`: the custom kinds watch takes it through `kinds.ProfileFor` while the cluster's
   definition serves its version; its goldens are `TestCuratedGolden`'s.
7. Add its actions as commands (above).

## Adding a cache consumer

1. Acquire a handle on what you show and release it when the view, drawer or query ends; the cache
   keeps the informer warm for a grace period after the last release. For a kind, use
   `Backend.Acquire(profile)` ([`kindscope.go`](../../internal/engine/kindscope.go)), which serves
   local kinds, the Applications, Helm's releases and the per-namespace watches of a user who may not
   list the kind everywhere; the backend's `Cache.Acquire(scope)` only for a scope without a profile.
2. Read snapshots; never start an informer, a watch or a poll yourself.
3. A read across kinds (as `resources.find`,
   [`resources.go`](../../internal/engine/commands/resources.go)) starts no watch of a kind
   `engine.HoldsSecretValues` names or of a virtual kind: it reads them only while `Backend.Cached`
   says they run.
4. Add a test that asserts the API requests your consumer costs: `testkit.MemorySource`'s `Lists`
   and `Watches` ([`memsource.go`](../../internal/testkit/memsource.go)), or a connection's
   `cluster.RequestCounter` ([`counter.go`](../../internal/cluster/counter.go)); budgets P9–P11 in
   [`testing.md`](testing.md#performance-budgets).

## Adding a package

1. Pick its layer ([architecture §3](architecture.md#3-dependency-layering-enforced)) and add its
   `depguard` rule to [`.golangci.yml`](../../.golangci.yml) in the same change, and the package to
   the deny lists of the rules that must keep other packages off it.
2. Give it a package doc comment.
3. Add it to the layout in [architecture §2](architecture.md#2-repository-layout), a row to
   [architecture › Packages](architecture.md#packages) when it has mechanisms of its own, and a line
   to the package map in [`AGENTS.md`](../../AGENTS.md#2-package-map).

## Changing the logo

Every version of the logo stays drawable in [`internal/ui/logo`](../../internal/ui/logo/versions.go)
([D-24](decisions.md#d-24-logo)): a new one is appended, none is replaced.

1. Design it in an icon lab, an interactive claude.ai artifact built for the purpose: presets, a
   control per `Look` field, a Dock preview against the current logo, and two exports, the settings
   as JSON and the SVG.
2. Save them as `docs/developer/assets/logo/<name>.json` and `<name>.svg`.
3. For a C helm, add a `Look` to [`versions.go`](../../internal/ui/logo/versions.go): each field
   holds the lab setting its comment names ([`logo.go`](../../internal/ui/logo/logo.go)). A drawing
   of another kind gets its own `layers` function, as [`gauge.go`](../../internal/ui/logo/gauge.go)
   has. Append its `Version` (name, date, what it is) to `Versions` and point `Current` at it.
4. Add the look to `labLooks` in [`logo_test.go`](../../internal/ui/logo/logo_test.go):
   `TestLabLooksAreTheLabsSettings` holds it to its JSON, `TestLabLooksMatchTheirReferenceSVGs` to
   its SVG.
5. `make icon`, then copy `packaging/icon.png` to `docs/developer/assets/logo/<name>.png`:
   `TestEveryVersionDrawsItsPicture` and `TestPackagedIconIsCurrent` check both.
6. `make showcase`: the README's banner, the social preview and every picture that shows the header.
7. Record the choice in D-24 (the settings, where the logo shows), and upload
   `docs/images/social_preview_dark.png` under the repository's Settings › General › Social
   preview by hand: GitHub's API cannot set it.

To bring an earlier version back, point `Current` at it and run steps 5 to 7 (its picture is
already there; skip the copy).
