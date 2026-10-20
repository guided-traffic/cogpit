# Releasing

> **Last reviewed:** 2026-10-13 · **Last updated:** 2026-10-20 · **Change:** minor (What gets built: Fyne's thread check off in releases since v0.30.0); minor (What gets built: a release has Fyne's thread check off, the repository variable `FYNE_THREAD_CHECKS` its way back, D-103, on the branch `fast-scroll`); minor (Renovate: the debug container's default image, D-102); minor (the release pipeline: the Kubernetes release table refreshed before the app is built, `make k8s-eol K8SEOL_FLAGS=-fallback`, D-91; Renovate's list: the table is no dependency it sees); minor (CI: `internal/ui/logo` out of the race tests, the job at its fifteen-minute limit); minor (v0.22.0 released 2026-10-17 08:14 UTC, its appcast waiting for the owner's approval with v0.20.0's and v0.21.0's); minor (v0.20.0 and v0.21.0 released in the night of 2026-10-16/17, their appcasts waiting for the owner's approval); minor (CI's macOS job: the file panels' harness and the lint of `internal/ui/nativedialog`, 2026-10-16 night); minor (the appcast: every start checks at once while the automatic checks are on, the owner's of 2026-10-16 night); minor (v0.17.0 to v0.19.0 released 2026-10-16, the feed holds 0.19.0 to 0.15.0); minor (the appcast: the deploy refuses only a feed that takes back a newer release Pages serves, an older item a newer release's draft never saw left out with a warning, the owner's correction of F18 after v0.19.0's refused deploy); minor (the appcast: the waiting appcast runs of v0.13.0 and v0.14.0 cancelled on 2026-10-16, the owner's choice, so neither can reset the feed); minor (Renovate: the rules for Cogpit Metrics' bundled chart and `make metrics-bundle` removed with the stack, D-61 withdrawn by the owner 2026-10-16); minor (v0.13.0 has its disk image: the re-run after its 330-minute wait, 2026-10-15 23:52 UTC); minor (the disk image's window, D-70, 2026-10-16: the template made by dmgbuild in the app's job, filled and signed in the signing job with Apple's tools only; Renovate's rule for dmgbuild); minor (releases leave the demo cluster out: `make dist-app` builds with `DIST_TAGS` `nodemo` and checks both binaries recorded it, CI's lint job runs `make check-nodemo`, D-67); minor (the appcast's deploy refuses a feed that drops an item Pages serves, F18, the owner's decision 2026-10-16); minor (the appcast read every hour by each running copy, `SUScheduledCheckInterval` 3600, owner 2026-10-16; what it costs); minor (the first update through the feed seen on 2026-10-16: the owner's MacBook from 0.15.0 to 0.16.0); minor (the signed feed served since 2026-10-16 with 0.16.0 and 0.15.0; appcasts approved in release order, the site artifact named per run attempt); minor (CI: the job Integration Tests (envtest), `make test-envtest` against envtest's etcd and kube-apiserver pinned by SHA-512 and cached per pin, its coverage profile `envtest.out`, in Semantic Release's needs, D-64; not observed yet); minor (Renovate: Cogpit Metrics' bundled chart version and its manual archive update, `make metrics-bundle`, D-61, 2026-10-16); minor (CI: apt-install.sh repairs an interrupted dpkg before a retry, the install's deadline six minutes); minor (signing and notarization: signed releases with `cogpit-cli` inside since v0.7.0, the quarantined-copy and offline checks not tried with them; v0.13.0's pipeline still waiting for Apple's notarization on 2026-10-15 at 20:10 UTC, no files yet; What gets built as of 2026-10-15); minor (Sparkle's update key set up by the owner on 2026-10-15 and in the releases since v0.11.0, no signed appcast yet: no release with the key has passed notarization; the releases with files are v0.6.0 to v0.8.0); minor (when no release comes: `hack/ci/apt-install.sh` with deadlines and tries, `rerun.yml` re-running jobs of main\'s newest run that failed setting up); major (notarization in a job of its own: "Sign and submit (macOS)" submits the disk image and records the submission, "Notarize and staple (macOS)" waits up to 330 minutes for that submission and a re-run waits for the same one, after v0.9.0 to v0.12.0 got no download); minor (Sparkle: steps 1 and 2 set up 2026-10-15, the environment `sparkle` and Pages; the repository renamed back to `cogpit`, the feed URL with its spelling, Pages being case-sensitive, H-97); minor (the version a build reports: one variable for both binaries, `internal/buildinfo`, set by `DIST_LDFLAGS`; development builds say `dev` and their commit; `make dist-app` needs `VERSION`); major (D-16 revised by the owner: a release carries only the complete product, the disk image with `cogpit-cli` inside the app (`Contents/MacOS`), and `SHA256SUMS`; the `cogpit-cli` job, its archives and `make dist-cli` removed; the signing job signs the bundled `cogpit-cli` inside-out and submits only the disk image; agent CLIBUNDLE); major (Sparkle 2: the framework in the app, pinned by version and SHA-256, Sparkle's parts signed inside-out, the appcast's three jobs (drafted, signed in the environment `sparkle`, deployed to GitHub Pages) and the owner's steps to set them up; no update key yet); major (the release pipeline signs the macOS app and `cogpit-cli` with the Developer ID, puts the app into a disk image, notarizes both and staples the disk image's ticket, in a job of its own in the environment `release`; a failed signature or notarization fails that job and the release gets no files until it is re-run, the lead's decision for the owner to confirm); minor (the Apple signing and notarization material set up in the environment `release`); minor (Renovate: the node shell's default image, D-37); earlier major (CI split into parallel jobs of at most ten minutes, the UI's race tests sharded; semantic-release live, D-16 revised; the release pipeline attaches `cogpit-cli` and the unsigned app; Renovate: sigstore's trusted root refreshed by make sigstore-root, D-50) · **Target:** Lens Desktop 2026.10.51501

Every push to `main` and every pull request runs the checks, each a job of its own with a make
target that runs the same on a laptop. Since 2026-10-13 ([D-16](decisions.md#d-16-the-first-real-release))
semantic-release is live: once every check on `main` is green it creates the tag and the GitHub
release, and the release pipeline builds the artifacts from that tag and attaches them
([release flow](#release-flow)). Since 2026-10-14 the pipeline signs the macOS app with the
Developer ID, ships it in a disk image and has it notarized by Apple
([signing and notarization](#signing-and-notarization)); v0.6.0 to v0.8.0 carry that disk image,
v0.5.0 and v0.9.0 to v0.12.0 have no files (v0.5.0's "Sign and notarize" job failed; from v0.9.0 on Apple kept each
submission beyond the job's 45 minutes, [when it fails](#when-signing-or-notarization-fails)),
v0.13.0's run was still in "Notarize and staple (macOS)" two hours after its tag (20:10 UTC on
2026-10-15, no files yet), and v0.4.0 and earlier are **unsigned**. Since 2026-10-14 (the owner,
[D-16](decisions.md#d-16-the-first-real-release)) a release carries **only the complete product**:
today the disk image, with `cogpit-cli` inside the app, and `SHA256SUMS`; no `cogpit-cli`
archives for any platform (releases up to v0.6.0 had them, unsigned for Linux and Windows,
[updates](../security/updates.md#what-a-release-holds-today)). Since 2026-10-14 the app carries
Sparkle 2, and three jobs add each release to Sparkle's appcast on GitHub Pages, signed with the
update key in the environment `sparkle` ([the appcast](#the-appcast)). The owner set up the key on
2026-10-15 ([setting up Sparkle](#setting-up-sparkle)) and the releases carry it since v0.11.0;
v0.13.0 to v0.16.0 passed notarization (v0.9.0 to v0.12.0 did not; v0.13.0's files came with a
re-run after the 330-minute wait), and since 2026-10-16 ~06:20
UTC Pages serves the signed feed with 0.16.0 and 0.15.0, approved by the owner; v0.17.0 to v0.19.0 followed the same day with their disk images, each appcast approved in release order, so by the evening the feed holds 0.19.0, 0.18.0, 0.17.0, 0.16.0 and 0.15.0; v0.20.0 (2026-10-16 19:21 UTC) and v0.21.0 (23:39 UTC) followed in the night and v0.22.0 (08:14 UTC) and v0.22.1 (09:32 UTC) on 2026-10-17, their appcasts waiting for the owner's approval. The first
installed copy updated itself through it on 2026-10-16: the owner's, from 0.15.0 to 0.16.0
([the appcast](#the-appcast)).
Platform targets and how they compare with Lens Desktop:
[platform parity](../parity/platform-parity.md#operating-systems-and-packages).

## CI

Four workflows in [`.github/workflows/`](../../.github/workflows/), laid out as in the team's
other repositories (`valkey-operator`):

| Workflow | Triggers | What it does |
|---|---|---|
| [`release.yml`](../../.github/workflows/release.yml) "Test and Release" | push to `main`, pull request to `main`, manual | the checks below, each a job; on `main`, Semantic Release once all of them passed. The README's build badge shows this workflow. |
| [`build.yml`](../../.github/workflows/build.yml) "Release Build" | a release published (`release: types: [published]`) | builds the artifacts from the release's tag, signs and notarizes the macOS ones, and attaches them ([the release pipeline](#the-release-pipeline)) |
| [`renovate.yml`](../../.github/workflows/renovate.yml) | daily at 02:00 Berlin time, manual | dependency updates ([Renovate](#renovate)) |
| [`rerun.yml`](../../.github/workflows/rerun.yml) "Re-run on runner failures" | a push run of "Test and Release" on `main` completed red or cancelled | re-runs its failed jobs once when each of them stopped in a set-up step (actions not downloadable, an apt mirror hanging) and the run is for `main`'s newest commit; never a failing test ([when no release comes](#when-no-release-comes)) |

**Budget: no job needs more than ten minutes** (owner, 2026-10-13). Measured on the first live run
(38445382032, 2026-10-13, the table's last column): the longest job took 9.6 minutes, with all
Linux jobs on one shared self-hosted machine; the whole run about 25 minutes, most of it jobs
waiting for a free runner (the owner finds 20 minutes and more acceptable). The second live run
(38449778410) stayed at most 7.7 minutes per job; its macOS job got no hosted runner within its
15 minutes once (no step ran) and passed on a rerun of the failed jobs in 6.2. The UI's race tests take about
18 minutes in one process on a Linux runner, so they run in eight shards. Every job's timeout is
15 minutes: a run after a `go.sum` change starts without caches and saves them (two minutes more,
measured up to 13.8 minutes for the kind job in run 38439307913); the budget is for the runs in
between.

**Concurrency:** a newer push cancels the running run of the same branch, on `main` too:
semantic-release releases every commit since the last tag, so a cancelled run loses nothing.

**Runners:** the Linux jobs run on the organisation's self-hosted runners (`runs-on: self-hosted`),
as the team's other repositories do: ephemeral pods on Ubuntu 24.04 and Docker, but without
`make`, a C compiler or `SHELL`. They all share **one machine with 16 CPUs** (every job reports
the same boot time; the load stood at 13 to 20 during a run), so the jobs' setup and builds compete
for its CPUs and network. The repository variable `LINUX_RUNNER` moves them elsewhere without a
commit (`ubuntu-latest`: GitHub's hosted runners); unset, they stay self-hosted (owner,
2026-10-13). The macOS job and the Windows
compile run on GitHub's hosted runners (`macos-latest`, Apple silicon; `windows-latest`), which cost
nothing for a public repository; a Fyne app with cgo and AppKit does not cross-compile from Linux.

**Setup:** every Go job starts with [`.github/actions/go-setup`](../../.github/actions/go-setup/action.yml):
Go from `go.mod`, the build cache under a key per kind of build (`lint`, `unit`, `race-ui`,
`race-rest`, `kind`, `macos`, …) and `go.sum`, Fyne's Linux build dependencies with
`build-essential`, `SHELL=/bin/bash` where the pod exports none, and the modules from the module
proxy. A key is saved once per `go.sum` (about two minutes from the self-hosted runners, 11 to
14 MB/s up), so the next runs start with the dependencies compiled. The modules are not cached:
the self-hosted runners restore a cache at 6 to 9 MB/s, slower than `go mod download`.
**Every test runs on every run** (`GOFLAGS=-count=1`): the restored build cache also holds Go's
cached test results, and a race shard once "passed" in four seconds without running a test.

**Tokens:** every job's `GITHUB_TOKEN` is read-only (`contents: read`) except the release
pipeline's "Attach to the release" (`contents: write`, to attach the artifacts), "Appcast (sign)"
(`actions: read` as well, for its environment's rules) and "Appcast (deploy to Pages)" (`pages:
write` and `id-token: write`, to deploy). Semantic Release and Renovate work
with an installation token of the organisation's GitHub App (`APP_CLIENT_ID`, `APP_PRIVATE_KEY`,
organisation secrets visible to this repository; [keys](../security/updates.md#keys-and-who-holds-them)).
The Apple material reaches one job only, the release pipeline's signing job, through the
environment `release` ([secrets](#secrets)).

| Job | Runner | Make target | What it proves | Minutes, run 38445382032 |
|---|---|---|---|---:|
| Code Linting | self-hosted | `make lint`, `make check-nodemo` | golangci-lint with the team's linters and `depguard`'s layering ([architecture §3](architecture.md#3-dependency-layering-enforced)); since 2026-10-16 also the release build's variant, which no other job builds: `internal/demo` in neither binary with `-tags nodemo` and in both without, `./cmd/...` and `./internal/engine/...` linted with the tag, their `TestNoDemo*` tests ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases); about 1 minute more locally, not measured on CI yet) | 3.2 |
| Parity | self-hosted | `make parity` | the parity checklists' rows and the numbers the progress pages report ([`make parity`](../parity/README.md#make-parity)) | 1.6 |
| GoSec Security Scan | self-hosted | `make gosec` | golangci-lint's gosec on the shipped code (tests, `internal/testkit` and `hack` left out); every accepted finding is excluded in [`.golangci.yml`](../../.golangci.yml) with its reason, a real one is fixed or an H-gap in [`docs/security`](../security/README.md) | 2.6 |
| Vulnerability Check | self-hosted | `make vuln` | govulncheck: no known vulnerability in a function Cogpit's code reaches | 1.6 |
| Cyclomatic Complexity | self-hosted | `make cyclo` | gocyclo: no function above `CYCLO_THRESHOLD`; the 20 most complex in the job summary ([below](#cyclomatic-complexity)) | 0.8 |
| Unit Tests | self-hosted | `make test-unit-coverage` | every package's tests without the race detector, with a coverage profile | 5.7 |
| Race Tests (UI n/8) | self-hosted, eight jobs | `make test-race-ui RACE_SHARD=n RACE_SHARDS=8` | one shard of `internal/ui/fyneui`'s tests under the race detector ([sharding](#the-uis-race-tests-in-shards)) | 5.2–9.3 |
| Race Tests (other packages) | self-hosted | `make test-race-rest` | every other package under the race detector but `internal/ui/logo` (pure-Go rasterising on one goroutine: nine of the job's fifteen minutes under the race detector; the unit tests run it, since 2026-10-17, the lead's decision after the job ran out of time), and the patched terminal widget's tests (`make test-fork`) | 6.6 |
| Integration Tests (kind) | self-hosted | `make kind-up`, `make test-kind` | the `TestKind*` tests against a real API server and kubelet (kind v0.33.0, Kubernetes 1.37.0, the seed of [`hack/kind`](../../hack/kind/README.md) and the Metrics Server); the tests compile while the cluster starts | 9.6 |
| Integration Tests (kind drain) | self-hosted | `make kind-drain-up`, `make test-kind-drain` | cordon, a blocked drain, a simulated crash and the finish on a three-node cluster of its own | 4.0 |
| Integration Tests (envtest) | self-hosted | `make envtest-bin`, `make test-envtest` | the `TestEnvtest*` tests against envtest's etcd and kube-apiserver of Kubernetes 1.37.0, no Docker and no kubelet: D-58's standby case with the API server stopped by SIGSTOP ([D-64](decisions.md#d-64-envtest-a-real-api-server-in-ci-without-docker)); the binaries, pinned by SHA-512, download while the tests compile and are cached per pin (`make envtest-key`) | – (since 2026-10-16, not observed yet; expected 5–8) |
| macOS | `macos-latest` | `make test-darwin`, `make lint LINT_PKGS=…`, `make app` | the tests only macOS can run (the title bar's AppKit harness, the updater's harness with Sparkle's framework from `make sparkle`, the file panels' harness and the bookmarks' round trip of `internal/ui/nativedialog` (since 2026-10-16), the locale's CoreFoundation read, the tests that expect macOS's shortcut glyphs), the lint of the packages with darwin-only files (`internal/ui/nativedialog` among them since 2026-10-16), and the app bundle with Sparkle | 5.0 |
| Compile (windows) | `windows-latest` | `go build`, `go vet`, `go test -run '^$'` | every package and test compiles and links on Windows with cgo (MinGW-w64 from MSYS2) and Fyne; it runs no test | 2.9 |
| Combined Coverage Report | self-hosted, after the test jobs | `make coverage-report` | the unit, race and cluster profiles merged: the job summary, and the badge Semantic Release commits ([coverage](#coverage)) | 1.0 |
| Semantic Release | self-hosted, `main` only, after every job | `npx semantic-release` | the tag, the GitHub release with its notes, and the release commit with the badge ([release flow](#release-flow)) | 0.9 |

Linux needs no compile job: the unit and race jobs build and link every package and test there with
cgo. The cluster jobs name their kind cluster after the run (`cogpit-<run id>`), so two runs on one
Docker host cannot meet; `KUBECONFIG` points at the file `make kind-up` writes, and nothing touches
`~/.kube`.

**Reading a run:** `gh run list --limit 5`, then `gh run view <id>` for the jobs, `gh run view <id>
--log-failed` for what failed, and `gh run view <id> --json jobs` for each job's start and end. The
job summaries hold the coverage per package and the complexity report.

### The UI's race tests in shards

[`hack/shard`](../../hack/shard/main.go) lists the top-level tests, examples and fuzz targets of
`internal/ui/fyneui` from the test files `go list -tags race` reports, assigns each name to a shard
by an FNV-1a hash of the name, and prints one shard's `-run` pattern; `make test-race-ui` runs it.
A test added later lands in a shard without a change to the workflow, and a name keeps its shard as
long as the number of shards stays. Eight shards from measurements (2026-10-13, the package's 548
tests under the race detector, 1,236 s locally on an M1 Ultra, 1,077 s on a 4-CPU Linux runner):
the hash puts 111 to 179 s of tests into a shard, the longest single test is
`TestRenderPrometheusMetrics` at 46 s. To see the split: `go run ./hack/shard -pkg
./internal/ui/fyneui -tags race -of 8 -summary`, with `-durations <go test -json output>` for each
shard's time. To change the number of shards, change the matrix and `RACE_SHARDS` in
`release.yml`'s `race-ui` job together. The `Makefile`'s own default, `RACE_SHARDS ?= 8`, is CI's
number (set equal 2026-10-15), so a local `make test-race-ui RACE_SHARD=<n>` runs CI's shard n.

### Coverage

Every test job writes a profile to `.cache/coverage` and uploads it (`profile-*`): `unit.out`
(every package without the race detector, each package counting its own tests), `race-ui-<n>.out`
and `race-rest.out` (the same under the race detector), `integration.out` and
`integration-drain.out` (the cluster tests with `-coverpkg=./...`, so they count for every package
they reach), and since 2026-10-16 `envtest.out` (the envtest tests, the same way). The Combined Coverage Report job merges them with [`hack/coverage`](../../hack/coverage/main.go)
(`make coverage-report`): a block counts once, as covered when any profile covered it. It writes the
total and a table per package to the job summary and
[`.github/badges/coverage.json`](../../.github/badges/coverage.json), the [shields.io endpoint
badge](https://shields.io/badges/endpoint-badge), coloured as in the team's other repositories
(≥ 90 % brightgreen, ≥ 80 % green, ≥ 70 % yellow, ≥ 60 % orange, else red). Semantic Release
commits the badge with the release (`@semantic-release/git`), so the README's number moves with
releases. **Measured:** 83.6 % on 2026-10-13 (run 38439307913: 37,635 of 45,002 statements; the unit profile alone gave 82.9 %).

### Cyclomatic complexity

`make cyclo` runs gocyclo v0.6.0 over `cmd`, `internal`, `hack` and `third_party`, tests and
generated code left out, and fails above `CYCLO_THRESHOLD`. The threshold is **56**, the highest
complexity `main` had when the check came (2026-10-13: `(*shell).applyUIState` in
[`shell.go`](../../internal/ui/fyneui/shell.go)), so no function grows past it. The team's other
repositories use 15; 121 functions are above that today. **Decided (owner, 2026-10-13):** 56 stays
as a ratchet and comes down step by step: a change that makes the most complex function simpler
lowers `CYCLO_THRESHOLD` to the new maximum in the same commit (`make cyclo-report` shows it); no
refactoring of its own for it.

### Running a job locally

Each job's make target runs the same here (fast local checks first: [AGENTS.md §5](../../AGENTS.md#5-before-you-finish-a-task)):

```sh
make lint check-nodemo parity gosec vuln cyclo
make test-unit-coverage
make test-race-ui RACE_SHARD=3 RACE_SHARDS=8   # one shard; make test-unit-race runs everything
make test-race-rest
make kind-up && make test-kind                 # the cluster tests; make kind-down after
make test-envtest                              # the envtest tests; fetches the pinned binaries first
make test-darwin                               # macOS only
make coverage-report                           # merges what is in .cache/coverage
```

Tests run with `KUBECONFIG=/dev/null` and without `KUBERNETES_SERVICE_HOST` and
`KUBERNETES_SERVICE_PORT` (`TEST_ENV` in the [Makefile](../../Makefile)): inside a pod, as on CI's
self-hosted runners, client-go would otherwise take the pod's namespace as a context's default and
could reach the pod's own cluster.

### Renovate

[`renovate.yml`](../../.github/workflows/renovate.yml) runs self-hosted Renovate with an
installation token of the team's GitHub App (`guided-traffic-automation`), from the organization
secrets `APP_CLIENT_ID` and `APP_PRIVATE_KEY`; both are visible to this repository (2026-10-11), and
the Semantic Release job's token from the same App proves it is installed here (the dry run of
run 38439307913, 2026-10-13: "Allowed to push to the Git repository", next version 0.1.0). The configuration is
[`.github/renovate.json`](../../.github/renovate.json), the team's with what Cogpit lacks taken
out (container images, Helm charts, Valkey images) and one group added (Fyne):

- Minor, patch and digest updates of Go modules, the Go version, GitHub Actions and the tools the
  Makefile pins merge on their own once CI passes; majors wait for review (Actions excepted, as in
  the team's repositories).
- Groups: the Go version with `golang.org/x/*`; `k8s.io/*` and `sigs.k8s.io/*`; Fyne
  (`fyne.io/*`, `github.com/fyne-io/*`).
- The Makefile's tool pins (`FYNE_TOOLS`, `GOLANGCI_LINT_VERSION`, `GOVULNCHECK_VERSION`,
  `GOCYCLO_VERSION`, `METRICS_SERVER_VERSION`) carry a
  `# renovate:` comment the custom manager reads; the `go` line of `go.mod` has its own manager,
  and the workflows follow it through `go-version-file`.
- dmgbuild and its dependencies (2026-10-16, [D-70](decisions.md#d-70-the-disk-images-window)):
  [`dmgbuild-requirements.txt`](../../hack/macos/dmgbuild-requirements.txt), read by Renovate's
  `pip_requirements` manager, never merged on their own (their output ships in every disk image);
  if Renovate cannot write the new hashes, take them from PyPI's JSON (the wheel's `sha256`).
- Sparkle (2026-10-14, [D-4](decisions.md#d-4-packaging-and-updates)): `SPARKLE_VERSION` in the
  Makefile, a release tag without a `v`, has its own regex manager; Renovate cannot know the new
  archive's SHA-256, so its pull request fails `make sparkle` until `SPARKLE_SHA256` is set to the
  asset's digest (`gh api repos/sparkle-project/Sparkle/releases/tags/<version>`, the `.tar.xz`
  asset's `digest`), and it never merges on its own: Sparkle's code installs Cogpit's updates.
- The node shell's default image (2026-10-14, [D-37](decisions.md#d-37-the-node-shell)):
  `DefaultNodeShellImage` in [`engine/nodeshell.go`](../../internal/engine/nodeshell.go), Alpine
  pinned by tag and index digest, carries a `// renovate: datasource=docker` comment that its own
  regex manager reads; a new Alpine release (a minor update, 3.24 to 3.25) and a new digest of the
  tag merge on their own once CI passes, whose kind job opens a node shell with the new image
  (`TestKindNodeShell`); a major waits for review. Checked with a dry run of Renovate 44.155.8
  (`--platform=local --dry-run=lookup` on a copy of the two files): it reads the tag and the digest,
  and for a pin at 3.23 proposes 3.24 and 3.23's newest digest.
- The debug container's default image (2026-10-19, [D-102](decisions.md#d-102-debug-containers-added-from-the-drawer-shown-in-blue-reached-by-logs-shell-and-attach)):
  `DefaultDebugImage` in [`engine/debugcontainer.go`](../../internal/engine/debugcontainer.go),
  nicolaka/netshoot pinned by tag and index digest, the same comment and a regex manager of its
  own; a new netshoot release (a minor, v0.16 to v0.17) and a new digest of the tag merge on their
  own once CI passes, whose kind job adds a debug container with the new image, attaches to it and
  reads its log (`TestKindDebugContainer`); a major waits for review. Not checked with a dry run of
  Renovate (the regex was matched against the file with Python's `re`, the same groups).
- sigstore's trusted root (2026-10-15, [D-50](decisions.md#d-50-kubectls-signature-check)):
  [`sigstore_trusted_root.json`](../../internal/kubectl/sigstore_trusted_root.json), which the check
  of a kubectl download's signature embeds, is no dependency Renovate can see. `make sigstore-root`
  ([`hack/sigstoreroot`](../../hack/sigstoreroot/main.go)) fetches it through sigstore's TUF
  repository, verified from the TUF root sigstore-go embeds, and writes it only when it changed: run
  it before a release and commit a diff as `build(deps):`. Until it runs, a key sigstore rotated
  stops the downloads of releases signed under it (fails closed,
  [H-35](../security/trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)).
  sigstore-go and its modules update as every Go module does; the tests with two real releases'
  files and Rekor entries (`TestRealReleaseSignature`, `TestRealReleaseRefused`) are their gate.
- The Kubernetes release table (2026-10-18, [D-91](decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)):
  [`releases.json`](../../internal/model/k8seol/releases.json), upstream's end-of-life dates, is no
  dependency Renovate can see. The release pipeline refreshes it for each release
  ([below](#the-kubernetes-release-table)); `make k8s-eol` refreshes the committed copy by hand,
  committed as `chore:` when its diff shows a change.
- Dependency updates (Go modules, the Go version, the Makefile's tool pins) are committed as
  `build(deps):`, which semantic-release does not release (owner, 2026-10-13: releases come from
  Cogpit's own `feat:` and `fix:`; an update ships with the next of them, and govulncheck checks
  every push); Actions updates as `chore:`. The team's other repositories commit updates as `fix:`
  and release each one. Not verified: whether Renovate rewrites the commits of the pull requests
  already open on 2026-10-13 (#5, #24, #46); one merged with its old `fix(deps):` message would still
  make a patch release.
- `third_party/**` is not scanned (2026-10-12): the patched terminal widget keeps upstream's
  `go.mod` and workflows, which Cogpit's build does not use (the root `go.mod` decides every version
  through MVS, and GitHub runs only the root's workflows). The widget itself is tracked through
  the `github.com/fyne-io/terminal` line of the root `go.mod`, which never merges on its own: the
  `replace` makes that bump a no-op, so its pull request is the signal to move the copy onto the new
  upstream commit ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)).
- Held back, each with the reason in its rule: `k8s.io/kube-openapi`, `structured-merge-diff` and
  `sigs.k8s.io/kustomize/*` follow the `k8s.io/*` release (kustomize v0.21.3 needs a kube-openapi
  on `structured-merge-diff/v7`, which apimachinery v0.37.x does not compile against);
  `github.com/godbus/dbus/v5` v5.2.3 does not compile on macOS and Windows.

Automerge needs every check green, so one flaky test holds back every pull request: on 2026-10-12,
44 were open, 3 of them broken by the update itself (the three held back above) and the rest by
tests that assumed an order of asynchronous results, which CI's slower runners do not keep.

Renovate's pull requests change `go.mod` and `go.sum` on their own branches; agents working in
parallel rebase onto them like onto any other commit on `main`. Renovate runs `go mod tidy` after
each Go update, which drops a requirement that no package imports yet.


## Release flow

The team's Go repositories release with semantic-release on `main`; Cogpit has the same
[`.releaserc.json`](../../.releaserc.json) (the `conventionalcommits` preset, `@semantic-release/git`
committing the coverage badge with the release) and the same pinned tooling
([`package.json`](../../package.json), [`package-lock.json`](../../package-lock.json)):

1. Commits on `main` follow [Conventional Commits](conventions.md#commits-and-pull-requests):
   `feat:` makes a minor release, `fix:` and `perf:` a patch release, every other type none.
2. Once every job of a push to `main` is green, the Semantic Release job creates a token of the
   organisation's GitHub App (`actions/create-github-app-token@v3`, `permission-contents: write`),
   installs the tooling from `package-lock.json` with verified registry signatures (`npm audit
   signatures`), puts the combined coverage badge in place and runs `npx semantic-release`: it
   derives the version from the commits since the last tag, writes the release notes, commits the
   badge as `chore(release): <version> [skip ci]` to `main` when its number changed (v0.1.1 had no
   such commit), tags that commit, or else the release's own, `v<version>` and publishes the GitHub
   release. `[skip ci]` keeps the release commit from starting another run.
3. The release's `published` event starts [the release pipeline](#the-release-pipeline), which
   builds the artifacts from the tag. It starts because the App's token created the release: a
   release made with `GITHUB_TOKEN` starts no workflow.

### When no release comes

semantic-release runs only when **every** job of a push run on `main` passed: one red or cancelled
job of some twenty skips it, and a newer push cancels the run before (`concurrency`), which loses
nothing, since the next green run releases every commit since the last tag. A release is missing
when no run on `main`'s newest commit got through. On 2026-10-15 none did after `6ffd0ae` for
three reasons: real failures (a data race and a lint finding, then a GoSec finding), runs cancelled
by newer pushes, and the last run green but for one UI shard whose `apt-get update` hung 13
minutes on the self-hosted node until the job's 15-minute timeout. What keeps that from recurring:

- [`hack/ci/apt-install.sh`](../../hack/ci/apt-install.sh) installs the runners' Debian packages
  with a deadline a try (four minutes for `apt-get update`, six for the install), short network
  timeouts and three tries; a try after a failed one first runs `dpkg --configure -a`, since an
  install the deadline ended leaves dpkg interrupted and every later try refused (run 38808178422,
  2026-10-15).
- [`rerun.yml`](../../.github/workflows/rerun.yml) re-runs the failed jobs of `main`'s newest run
  once when each of them stopped in a set-up step ("Set up job", "Checkout", "Set up Go",
  "Install …", "Download the modules", "Restore the build cache"); the re-run ends with the
  Semantic Release job. A test, lint or check that fails is never re-run.
- By hand: `gh run list --branch main --limit 3`, then for the newest commit's run `gh run view
  <id>` and, if a job failed setting up or timed out, `gh run rerun <id> --failed`. Do not push
  while that re-run runs: the push cancels it.

**v0 until the owner decides on 1.0.** Releases continue from the owner's anchor tag `v0.0.0`
(2026-10-11). A breaking change makes a **minor** release, not 1.0.0 (`releaseRules: [{"breaking":
true, "release": "minor"}]` in the commit analyzer; the other rules are its defaults). The rule is
needed already: commit `f3ad000` has a body line starting with `BREAKING CHANGE:`, which the
analyzer reads as a breaking change, and without the rule the first release would have been 1.0.0
(the dry run of 2026-10-13 said so; with it, 0.1.0). Commits still carry no `!` and no `BREAKING
CHANGE:` footer ([conventions](conventions.md#commits-and-pull-requests)).

**After a release, `main` has moved:** the release commit is on `main`, so the next local push
needs `git pull --rebase` first.

**When semantic-release stops after the tag** (v0.6.0, 2026-10-14: GitHub rejected the push of
`refs/notes/semantic-release-v0.6.0` with a bare "failed" after the release commit and the tag
were pushed; the cause: the repository had been renamed to `guided-traffic/Cogpit`, and
`package.json`'s `repository.url`, from which semantic-release takes the URL it pushes to, still
named `…/cogpit.git`, whose redirect ("This repository moved") let the commit and the tag through
but not the notes; the URL follows the new name since): there is a tag but no GitHub release, so the release pipeline does not start, and
re-running the job does not help (its commit is behind `main` now, and semantic-release then
publishes nothing). Finish it by hand, as semantic-release would have:
`git notes --ref semantic-release-v<version> add -f -m '{"channels":[null]}' v<version>`, push that
ref, and create the release from the notes the release commit carries
(`git log -1 --format=%B <release commit> | tail -n +3 > notes.md`,
`gh release create v<version> --verify-tag --title v<version> --notes-file notes.md --latest`). A
release made with a person's token starts the pipeline as the App's does (only `GITHUB_TOKEN`'s
does not); the release then shows that person as its author.

**The first release:** [v0.1.0](https://github.com/guided-traffic/cogpit/releases/tag/v0.1.0)
(2026-10-13, 457 commits since `v0.0.0`), with the badge at 83.6 % in its release commit `53b96c9`.
Checked by download: `SHA256SUMS` matches the archives, `cogpit-cli --version` says `cogpit-cli
0.1.0 (go1.27.2, darwin/arm64)`, the app's bundle version is 0.1.0, its signature ad-hoc, and
`spctl --assess` rejects it, as an unsigned app is rejected. Its generated notes carried a "BREAKING
CHANGES" section quoting `f3ad000`'s body line (the version stayed minor by the v0 rule); the owner
had it removed from the release page (2026-10-13). The second release,
[v0.1.1](https://github.com/guided-traffic/cogpit/releases/tag/v0.1.1), came from the next `fix:`
(the local terminal's shell, `1f6f9b8`) and was checked the same way (`cogpit-cli 0.1.1`, the Linux
binary a static x86-64 ELF, the app's bundle version 0.1.1).


## The release pipeline

[`build.yml`](../../.github/workflows/build.yml) "Release Build" runs on `release: published`. A
release event runs on the release's tag: `github.ref` is `refs/tags/v<version>` (v0.4.0's run
38538012862 checked out `refs/tags/v0.4.0`), which the deployment rule of the environment `release`
(tags `v*`) admits. Every job checks out the tag and builds with `VERSION` set to the tag without
its `v`:

| Job | Runner | Make target | Builds |
|---|---|---|---|
| Cogpit.app (macOS arm64) | `macos-latest` | `make k8s-eol K8SEOL_FLAGS=-fallback`, `make dist-app` | first the Kubernetes release table of the release's day ([below](#the-kubernetes-release-table)); then `Cogpit_<v>_darwin_arm64.zip`: `make app` (`fyne package`, the version in `Info.plist` as both versions and in the binary, Sparkle 2 in `Contents/Frameworks` with the update key's public half from the repository variable `SPARKLE_PUBLIC_ED_KEY`, `cogpit-cli` in `Contents/MacOS`, an ad-hoc signature) zipped with `ditto`. Both binaries are built with `DIST_TAGS` (`nodemo`: no demo cluster, [below](#what-gets-built)), and the target fails unless both recorded the tag. The bundled `cogpit-cli` is `make build`'s, pure Go (`CGO_ENABLED=0`), `-s -w`, the version set with `-X …/internal/buildinfo.version` as in the app's binary ([the version a build reports](#the-version-a-build-reports)) ([D-16](decisions.md#d-16-the-first-real-release)). The job checks the bundle's versions, the framework, that its `SUPublicEDKey` is the variable (none while it is unset), its signature seal with the nested code, its architecture, and that `cogpit-cli` has the app's architecture, the identifier `io.guided-traffic.cogpit.cli` and says the release's version (`cogpit-cli version`; this job runs it, the signing job never does). No secret reaches it.; then the disk image's window as a template without the app, `make dist-dmg-template` (dmgbuild, [below](#the-disk-images-window)) |
| Sign and submit (macOS) | `macos-latest`, after the app, environment `release` | `make dist-macos-sign` | `Cogpit-<v>-macos-arm64.dmg`: the app with Sparkle and `cogpit-cli` signed inside-out with the Developer ID, in the checked template of the disk image's window ([below](#the-disk-images-window)), converted and signed, submitted to Apple's notary service without waiting; the artifact `macos-signed` carries it with `notary-submission.json`, the submission's id and the disk image's SHA-256 ([below](#signing-and-notarization)); without the secrets, the app zip unchanged |
| Notarize and staple (macOS) | `macos-latest`, after the signing, environment `release` (the team API key only) | `make dist-macos-notarize` | Apple's verdict on that submission, asked every minute for up to 330 minutes; the ticket stapled, the disk image and the app in it checked as Gatekeeper checks them; the record removed. A re-run waits for the same submission ([when it fails](#when-signing-or-notarization-fails)); without a submission, the unsigned zip passes |
| Attach to the release | self-hosted, after the notarization | `make dist-checksums` | `SHA256SUMS` over the download; checks that the release gets exactly the files the signing job's verdict promises and nothing else (signed: the disk image; unsigned: the app zip; with `SHA256SUMS`); attaches them (`softprops/action-gh-release@v3`) and appends a note to the release text: what the download is, that `cogpit-cli` comes inside the app and where, and that the app, its `cogpit-cli` and the disk image are signed and notarized, naming the certificate the signature carries, or else that they are unsigned and how macOS lets the user start the app anyway; Cogpit for Linux and Windows with M11 |
| Appcast (draft), Appcast (sign), Appcast (deploy to Pages) | `macos-latest`, `macos-latest` in the environment `sparkle`, self-hosted in `github-pages` | `make appcast-draft`, `appcast-sign`, `appcast-verify` | the release's item in Sparkle's appcast, signed and deployed to GitHub Pages ([the appcast](#the-appcast)); without the update key nothing is signed or deployed and the release is complete without them |

The release jobs build without a build cache (`cache: none`): an artifact never comes from objects
another run wrote, and for v0.1.0 saving the six platforms' cache took 5:18 minutes, longer than
the build (5:30). Measured for v0.1.0 (run 38447771669): the `cogpit-cli` job (removed
2026-10-14) 11.7 minutes with that save, the app 2.0, attaching 0.8; for v0.1.1 without caches
(run 38452933957): `cogpit-cli` 6.5, the app 4.5, attaching 0.8. The bundled `cogpit-cli` adds
one pure-Go link to the app's job, whose packages `make build` compiles anyway (not measured on CI
yet). Signing took 22 s locally (ad-hoc, without notarization). Apple's notary service takes its
own time: v0.8.0's whole run took 11 minutes, while on 2026-10-15 Apple kept every submission of
v0.9.0 to v0.12.0 in progress for more than the 45 minutes the one signing job then waited, five
attempts in all, each a new submission. Since then the notarization job waits 330 minutes (its
timeout 345, a GitHub-hosted job's limit 360) on the one submission the signing job made.

`make dist-app VERSION=0.0.99`, `make dist-macos VERSION=0.0.99` and `make dist-checksums` run the
same locally into `dist/` (gitignored; [running the signing locally](#running-the-signing-locally)).
To check a download on a Mac: `shasum -a 256 -c SHA256SUMS`, then `spctl -a -t open --context
context:primary-signature -vv Cogpit-<v>-macos-arm64.dmg`, which says `source=Notarized Developer
ID`; open it and run `"/Volumes/Cogpit <v>/Cogpit.app/Contents/MacOS/cogpit-cli" version`, which
says the release's version.

**Not built yet:** the universal (Intel) app and the Homebrew cask
([D-4](decisions.md#d-4-packaging-and-updates)); desktop packages for Linux and Windows (M11); an
SBOM; signed checksums.

### The Kubernetes release table

The window marks a cluster's Kubernetes version and its nodes' kubelets by upstream's support
([D-91](decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)) from a table built into
both binaries, [`internal/model/k8seol/releases.json`](../../internal/model/k8seol/releases.json)
(`go:embed`): every minor's end of life, the maintained minors' release day and maintenance mode,
the day it was fetched. Cogpit never fetches it at run time.

- **Where it comes from:** [`hack/k8seol`](../../hack/k8seol/main.go) downloads
  `kubernetes/website`'s `data/releases/schedule.yaml` (the maintained minors) and
  `data/releases/eol.yaml` (the ended ones) from `raw.githubusercontent.com` (the branch `main`,
  which kubernetes.io is built from; CC BY 4.0, [ATTRIBUTION.md](../../ATTRIBUTION.md)), merges and
  checks them: the dates parse, the minors follow one another without a gap, the ends of life in
  their order, each 12 to 18 months after its release, the newest with a release day; then
  against the committed table: no minor lost, no newest minor older, no known minor's end of life
  moved by more than 120 days. It prints what changed and writes the file, one minor a line.
- **Every release carries the table of its day:** the job "Cogpit.app (macOS arm64)" runs
  `make k8s-eol K8SEOL_FLAGS=-fallback` before `make dist-app`. A failed download or check keeps
  the committed table, says why (a warning on the run) and the build goes on: a minor newer than
  that table gets the estimate. What changed against the commit is in the step's log. The tag's
  tree is then not the release's: a build of the tag carries the committed table, and the release's
  binaries record `vcs.modified=true` (`go version -m`) when the table changed (a release reports
  its version all the same, [the version a build reports](#the-version-a-build-reports)).
- **The committed table is refreshed by hand:** `make k8s-eol`, and commit the file when its diff
  shows a change (upstream changes the files about monthly). No job commits it: a scheduled
  workflow pushing to `main` would be an unasked bot push (the lead's decision, D-91); the
  committed table only matters for local builds and the tests, which carry their own tables.
- **What a changed file can do:** shift the dates the window shows (a warning too early or too
  late), never run code; [H-130](../security/updates.md#h-130-the-releases-kubernetes-release-table-is-fetched-unreviewed).

### The disk image's window

Since 2026-10-16 ([D-70](decisions.md#d-70-the-disk-images-window)) the disk image opens as the
owner chose it in a visual review: a 600 × 340 window with the app's plate as the background (the
logo's navy gradient and halo), Cogpit's icon on the left, an arrow to the Applications folder on
the right, Cogpit's own helm wheel faint behind the folder, the icons at 124 points with their
names on grey pills that read in light and dark Finder (since 2026-10-19, the owner's WF42: centred
on where Finder writes the names, measured on his Mac, and wide enough for "Cogpit.app"); the volume
carries the app's icon. The
settings are [`packaging/dmg/lab.json`](../../packaging/dmg/lab.json) (from the review's lab, the
link inside); the background is the lab's drawing of them without icons and names
([`background.png`](../../packaging/dmg/background.png) and `@2x`, joined into one TIFF).

Two jobs make it, so that no code from PyPI runs beside the signing identity (the owner's choice):

1. **"Cogpit.app (macOS arm64)"** (no secrets): after `make dist-app`, `make dist-dmg-template`
   ([`dmg-template.sh`](../../hack/macos/dmg-template.sh)) installs dmgbuild and its dependencies,
   pinned by version and hash ([`dmgbuild-requirements.txt`](../../hack/macos/dmgbuild-requirements.txt)),
   into a throwaway virtual environment (Python 3.13 from `actions/setup-python`; locally any
   Python 3.10 or newer, `PYTHON` chooses one) and writes `Cogpit-<v>-template.dmg`: a writable
   image of the volume "Cogpit <v>" with the window's settings (`.DS_Store`, which dmgbuild writes
   itself, without Finder or AppleScript, [`dmg_settings.py`](../../hack/macos/dmg_settings.py)),
   `.background.tiff`, `.VolumeIcon.icns` and the link to `/Applications`, but no app (about 5 MB).
   It travels with the app zip in the artifact `macos-app`.
2. **"Sign and submit (macOS)"** ([`release.sh`](../../hack/macos/release.sh) `build_dmg`): checks the
   template (a UDRW image, the volume's name, exactly those four entries, the link's target),
   resizes it to the app's size plus a fifth, copies the signed app in with `ditto`, removes the
   events log macOS starts on a writable volume, converts the image to a compressed one (UDZO,
   zlib level 9) and signs it; `check_dmg` then expects the four entries and `Cogpit.app`. No
   Finder flag is set on the app: an extended attribute on the bundle fails `codesign`'s strict
   check, and Finder hides an app's `.app` anyway.

**Checked 2026-10-16** locally on macOS 26.6: `make dist-app`, `make dist-dmg-template` and
`make dist-macos MACOS_SIGN=adhoc` with VERSION 0.0.99, the template's contents and the ad-hoc
disk image's checks passing. **Not checked yet:** the steps on CI (they run with the next release),
and the window in Finder on another Mac than the owner's. To change the look: the lab, then
`lab.json` and the two PNGs from the lab's drawing, both themes looked at in a real Finder window.

### Signing and notarization

[`hack/macos/release.sh`](../../hack/macos/release.sh) runs in the jobs "Sign and submit (macOS)"
(`make dist-macos-sign`, steps 1 to 4's submission) and "Notarize and staple (macOS)" (`make
dist-macos-notarize`, the rest), the two jobs in the environment `release` and so the only ones that
see its secrets; the second gets the team API key only, no certificate. `make dist-macos` runs
the whole course in one go. It builds nothing and runs no Go, not even the bundled `cogpit-cli`: the app comes from
the build job as an artifact, so no code of Cogpit's or its dependencies' runs in a job that holds
the signing identity. In order:

1. **Keychain.** A temporary keychain in the runner's temporary directory, with a random password
   masked in the log, first in the keychain search list. Into it: the `.p12` from
   `MACOS_SIGN_P12` (its key not extractable, usable by `codesign` only) and Apple's intermediate
   `DeveloperIDG2CA.cer`, downloaded and checked against the SHA-256 the workflow pins
   (`f16cd3c5…3df3a`, valid to 2031-09-17); without the intermediate the identity is not valid.
   A `.p12` exported with its chain brings the intermediate along, and importing it again fails
   with "already exists": the script then checks that the one in the keychain is the pinned one
   (the first tag run, v0.5.0 on 2026-10-14, failed there; v0.5.0 has no files).
   The script needs exactly one valid "Developer ID Application" identity of `APPLE_TEAM_ID` and
   signs with it by its SHA-1.
2. **The app, inside out, never with `--deep`.** Its nested code is Sparkle (since
   2026-10-14), without its XPC services, which `make app` leaves out: `Autoupdate`,
   `Updater.app`, then `Sparkle.framework`, the order of Sparkle's documentation for signing
   outside Xcode; then `cogpit-cli` in `Contents/MacOS` (since 2026-10-14,
   [D-16](decisions.md#d-16-the-first-real-release)), with the identifier
   `io.guided-traffic.cogpit.cli`; then the bundle. Each with the hardened runtime (`--options
   runtime`), Apple's secure timestamp and no entitlements. `Contents/MacOS` must hold exactly the
   app's executable and `cogpit-cli`, a regular executable file of the app's architecture; anything
   else in `Contents/Frameworks`, and any `PlugIns`, `XPCServices`, `Helpers` or `Library`, is
   refused. Checked: `codesign --verify --strict --deep`, the runtime flag, no entitlements, the
   Developer ID authority and team, the timestamp, for each part, and `cogpit-cli`'s identifier;
   then every Mach-O file in the bundle (five) must carry the bundle's authority and the hardened
   runtime, so a helper a newer Sparkle adds cannot reach Apple signed ad-hoc.
3. **The disk image** `Cogpit-<v>-macos-arm64.dmg` (HFS+, zlib-compressed `UDZO`, volume name
   `Cogpit <v>`) holds `Cogpit.app` and a link to `/Applications`, nothing else: opening it shows
   the app beside Applications. No background picture or Finder layout (that needs Finder and
   AppleScript on the runner). Signed with the timestamp.
4. **Notarization** with `notarytool` and the team API key (`--key`, `--key-id`, `--issuer`; the
   `.p8` written owner-only into the work directory and deleted after use). The disk image is the
   one submission, the app and its nested code (Sparkle, `cogpit-cli`) inside it. The signing job
   submits it without waiting and records the submission in `notary-submission.json` (its id, the
   disk image's name and SHA-256). The notarization job checks that the disk image is the one
   submitted (the SHA-256), lists the team's latest submissions and their states, asks Apple for
   the verdict every minute (`notarytool info`; a question Apple does not answer is asked again)
   for up to 330 minutes (`NOTARY_TIMEOUT_MINUTES`; 45 for `make dist-macos`), then prints Apple's
   log (the files Apple checked and any issue; no secret in it). Anything but `Accepted` fails the
   job.
5. **Stapling and Gatekeeper.** The disk image's ticket is stapled (`stapler staple`, `stapler
   validate`), so it opens without a network. `spctl -a -t open --context
   context:primary-signature -vv` must say `source=Notarized Developer ID` for the disk image.
   Then the script mounts it, checks that it holds exactly `Cogpit.app` and the link, verifies the
   app's signature (and `cogpit-cli`'s on its own) and version, and `spctl -a -t execute -vv` must
   say the same for the app. On a
   runner with Gatekeeper turned off, `spctl` accepts everything without a verdict
   (`override=security disabled`); the script then warns and the notary service's `Accepted` and
   the stapled ticket stand alone (whether GitHub's macOS runners have it on is not verified).
6. **Cleanup:** the keychain deleted, the search list restored, the key files removed, whenever
   the script ends and again in a step with `if: always()`, for a cancelled run.

The job's outputs `signed` and `identity` (the certificate's name as the signature carries it)
tell the last job which files to expect and what the note says; its summary lists the
submissions with their status. Secrets reach the one step that signs, as environment variables;
the script never echoes them or anything decoded from them and runs without `set -x`.

**Entitlements: none,** for the app, Sparkle's parts and `cogpit-cli`. Go generates no code at run
time (no JIT, no unsigned executable memory), the app loads only Apple's frameworks and
Sparkle.framework, which carries the app's own Developer ID and so passes library validation,
`cogpit-cli` (pure Go) only the system's libraries, and neither uses the camera, microphone,
location, contacts or Apple Events; `kubectl`, credential plugins and shells are
programs of their own. Sparkle's distribution signs `Autoupdate` ad-hoc with
`com.apple.application-identifier`; Sparkle's documented commands sign it again without it, and so
does the script. Checked locally (2026-10-14, before Sparkle): the app signed ad-hoc with the
hardened runtime, taken from the disk image, started on the demo cluster, drew and scrolled the
Pods table for three seconds (`--demo --perf 3s`) and quit cleanly; a release build has no
`--demo` since 2026-10-16 ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)), so a
repeat names a test cluster with `--context`. Not started since Sparkle is
in the bundle: an app without the update key never loads the framework; one with the key signed
ad-hoc with the hardened runtime would be refused the ad-hoc framework by library validation,
which a Developer ID signature of both avoids (Sparkle's documentation says so). An entitlement
comes only with the failure that needs it, and the script's check that there is none changes with
it.

**Only the disk image is submitted**, with the app and its `cogpit-cli` inside; the app is not
notarized and stapled on its own first. Apple checks the code nested in a submission with it. Not
verified: that the copy in Applications starts without a network the first time (Gatekeeper then
needs the app's ticket, which only the disk image carries); that a `cogpit-cli` run from a
terminal in a quarantined copy, before the app was ever opened, starts without a prompt. Signed
releases with `cogpit-cli` inside exist since v0.7.0 (v0.7.0 and v0.8.0: the bundled `cogpit-cli`
signed with the Developer ID as `io.guided-traffic.cogpit.cli`, notarized with the disk image,
running and reporting its version, checked by download on 2026-10-14); neither check above has been
tried with them. Until 2026-10-14 `cogpit-cli` for macOS was a bare binary of
its own, notarized in a second submission, which could not carry a ticket
([D-16](decisions.md#d-16-the-first-real-release)).

**Sparkle's EdDSA signature** is computed over the disk image after stapling, its last change, in
a job of its own whose environment holds the update key and needs the owner's approval
([the appcast](#the-appcast)).

### When signing or notarization fails

**Decided (lead, 2026-10-14, open to revision):** with the secrets present, a failed
signature, notarization or check fails the job "Sign and submit (macOS)" or "Notarize and staple
(macOS)" visibly; nothing falls
back to an unsigned download under a release that should be signed. Some of the secrets but not
all fail it too. Without any of them (a fork, a run outside the environment) the release gets the
unsigned app zip and its note says so.

**What a failure leaves:** the release exists (semantic-release created it with its notes) but
has **no files**: "Attach to the release" needs the notarization job, and nothing is half attached.

**What to do:**

1. Read the job's log: the error line names what failed; for the notarization it names the
   submission, Apple's status and Apple's log.
2. **Still in progress** after 330 minutes: Apple keeps working on the submission. Re-run the
   failed jobs: `gh run rerun <run id> --failed` (or "Re-run failed jobs" on the run's page). Only
   "Notarize and staple (macOS)" runs again; it takes the signed disk image and the record from the
   artifact `macos-signed` and waits for the same submission, so a verdict Apple gave meanwhile is
   there at once. Never re-run "Sign and submit (macOS)" for this: a new signature's timestamp makes
   a new disk image, a new submission at the end of Apple's queue (what happened to v0.9.0 to
   v0.12.0, when signing and waiting were one job). The artifact is kept seven days.
3. **Invalid** (Apple rejected it): its log lists each issue with its file. Fix the cause in a
   commit, which makes a new release; the failed release stays without files, to delete with its
   tag or to leave.
4. **Keychain or identity** ("0 valid identities", a wrong password, an expired certificate): fix
   the secret in the environment `release`, then re-run the failed jobs.

A re-run of the failed jobs runs the failed job and the ones after it again on the same tag, with
the artifacts the earlier jobs uploaded (kept seven days): a re-run downloads what an earlier
attempt of the same run uploaded (seen 2026-10-15: the third attempt of run 38704721283 took
`macos-app` from the first). The uploads overwrite an artifact of the same name from an earlier
attempt.

### Running the signing locally

```sh
make dist-app VERSION=0.0.99                      # dist/: the app zip, cogpit-cli inside
make dist-dmg-template VERSION=0.0.99             # dist/: the window's template (Python 3.10+)
make dist-macos VERSION=0.0.99 MACOS_SIGN=adhoc   # dist/macos: the disk image, signed ad-hoc
make dist-macos VERSION=0.0.99                    # no MACOS_SIGN and no secrets: the app zip, unchanged
```

`MACOS_SIGN=adhoc` runs every step but the notarization and Gatekeeper's verdict, with ad-hoc
signatures: the hardened runtime, the checks of each signature and the disk image's contents. The
script takes the Developer ID from the secrets only, never from a login keychain. **Checked
2026-10-14** on macOS 26.6, under bash 3.2 and 5: the ad-hoc and unsigned runs (again with Sparkle
in the bundle: its three parts and the app signed inside-out, four Mach-O files with the hardened
runtime, `codesign --verify --strict --deep` on the app and on the copy in the disk image); the Developer ID
path up to the identity with a throwaway self-signed certificate (it stops at "0 valid
identities", as it must for a certificate Apple did not issue, and leaves the search list as it
was; a wrong pin of the intermediate stops it before); `codesign` with a key from a temporary
keychain, Apple's timestamp and the hardened runtime; the notarization's course with stand-ins for
`notarytool`, `stapler` and `spctl` (accepted, invalid, still in progress); `actionlint` 1.7.7
and `shellcheck` on the workflow and the script. **Not checked before the first tag run:** a
signature with the real certificate from the secrets, the notarization, the stapling and
Gatekeeper's verdict. **Checked again 2026-10-14** (agent CLIBUNDLE, macOS 26.6) with
`cogpit-cli` in the bundle: `make dist-app` and `make dist-macos MACOS_SIGN=adhoc`, `cogpit-cli`
signed before the bundle with `io.guided-traffic.cogpit.cli` and the hardened runtime, five Mach-O
files, `codesign --verify --strict --deep` validating `cogpit-cli` as nested code on the app and on
the copy in the disk image; `cogpit-cli version` run from the mounted disk image said `cogpit-cli
0.0.99 (go1.27.2, darwin/arm64)`; the app job's new checks and the last job's file check
(signed, unsigned, a stray archive refused) run locally; `actionlint` 1.7.12 and `shellcheck`
0.11.0 on the workflow and the script.

**What the first signed release must show** in the logs of "Sign and submit (macOS)" and "Notarize
and staple (macOS)": `1 valid
identities found` with "Developer ID Application: Hans Fischer (6L57P2HXN6)"; each signature with
`flags=0x10000(runtime)`, the Developer ID authority, `TeamIdentifier=6L57P2HXN6` and a
`Timestamp=`, `cogpit-cli`'s with `Identifier=io.guided-traffic.cogpit.cli`; the disk image's
submission `Accepted` with a log without issues (its file list naming `cogpit-cli`); `The staple and validate
action worked!`; `source=Notarized Developer ID` for the disk image and for the app, or the
warning that Gatekeeper is off on the runner. Then, on a Mac: download, `shasum -a 256 -c
SHA256SUMS`, `spctl -a -t open --context context:primary-signature -vv` on the disk image, open
it, drag Cogpit into Applications and, before starting the app, run
`/Applications/Cogpit.app/Contents/MacOS/cogpit-cli version` in a terminal, then start the app,
each with no warning; once without a network, to settle whether the app needs a ticket of its own
([only the disk image is submitted](#signing-and-notarization)); Help › Copy MCP command copies
the stdio registration of that path; and in its log, each of Sparkle's parts (`Autoupdate`,
`Updater.app`, `Sparkle.framework`) and `cogpit-cli` signed like the app, and `5 Mach-O files in
Cogpit.app, each signed Developer ID Application: …`.

### The appcast

Sparkle's appcast is the update feed every installed copy reads at its `SUFeedURL`,
`https://guided-traffic.github.io/cogpit/appcast.xml` (owner, 2026-10-14,
[D-4](decisions.md#d-4-packaging-and-updates)). The path carries the repository's name as GitHub
spells it: Pages serves a project site case-sensitively (checked 2026-10-15 on two other sites, the
other spelling answering 404). The repository is `guided-traffic/cogpit` again since 2026-10-15
(the owner's rename back from `Cogpit`, before any build with the update key). Renaming the
repository moves the feed, and every installed copy keeps the old URL
([H-97](../security/updates.md#h-97-the-feed-url-follows-the-repositorys-name)).
It is one RSS feed with the releases' history, an item
per release with its versions, minimum macOS, release notes, the disk image's download URL, length
and EdDSA signature, and, since Sparkle 2.9, an EdDSA signature over the feed itself
(`SURequireSignedFeed` in the app). Three jobs of [`build.yml`](../../.github/workflows/build.yml)
add a release to it, after "Attach to the release", so an item never points at a missing download
and nothing here can take a release's files back:

| Job | Runner | Make target | What it does |
|---|---|---|---|
| Appcast (draft) | `macos-latest`, after "Notarize and staple (macOS)" signed and "Attach to the release" | `make appcast-draft` | reads the app in the stapled disk image (`CFBundleVersion`, the short version, `LSMinimumSystemVersion`, `SUFeedURL`, `SUPublicEDKey`); an app without the key gets no item (`drafted=false`, the jobs after are skipped). Otherwise: the key must be the repository variable, the disk image's release asset must be downloadable, the feed is fetched from Pages past its cache (404: the first item starts it) and must carry a valid signature by that key, the release's notes and dates come from `gh release view`, and [`hack/appcast`](../../hack/appcast/main.go) drafts the feed with a placeholder for the disk image's signature. No secret, Go and the standard library only |
| Appcast (sign) | `macos-latest`, environment `sparkle` | `make appcast-sign` | without `SPARKLE_ED_PRIVATE_KEY`: a notice, `signed=false`, nothing deployed, never a failure. With it: refuses unless the environment `sparkle` has a required reviewer and admits tags `v*` only (read from the API; GitHub creates an environment a workflow names without any rule, [H-92](../security/updates.md#h-92-an-environment-sparkle-without-rules-hands-the-update-key-to-any-run)); then Sparkle's own `sign_update` from the pinned archive (`make sparkle`) signs the disk image (the one the draft names, by its SHA-256), the signature takes the placeholder's place, `sign_update` signs the feed and verifies both. The key reaches `sign_update` on its standard input only, never a command line or a file; GitHub masks it; the job runs no Go and no dependency's code; its work files are removed with `if: always()` |
| Appcast (deploy to Pages) | self-hosted, environment `github-pages`, `pages: write`, `id-token: write` | `make appcast-verify` | checks with the public key what is about to be served: the feed's signature, every item, and that the release's item names this disk image by its length with a signature over its bytes; fetches the feed Pages serves now (past its cache) and refuses a feed that would take back a newer release it serves; an older item the feed lacks is named in a warning (since 2026-10-16, below); then `actions/upload-pages-artifact@v5` and `actions/deploy-pages@v5` replace the site with `appcast.xml`. One deploy at a time (`concurrency: pages`) |

The feed's items are newest first by `sparkle:version`; a re-run replaces its release's item, and
each channel keeps its 10 newest. A prerelease's item goes to `<sparkle:channel>beta</sparkle:channel>`,
which no copy follows yet (Preferences › Update channel offers Stable only; a prerelease's
`CFBundleVersion` has no scheme yet, [D-4](decisions.md#d-4-packaging-and-updates)). The release
notes are the release's text up to the pipeline's download note, at most 32 KiB, as Markdown
inside the signed feed, with a link to the release. The site holds nothing but the feed: Pages
serves only what the last deploy uploaded.

**Every running copy reads it once an hour** (owner, 2026-10-16,
[D-4](decisions.md#d-4-packaging-and-updates)): `make app` writes `SUScheduledCheckInterval`
3600 into the app's `Info.plist` ([`hack/macos/sparkle.sh`](../../hack/macos/sparkle.sh)).
Sparkle's default is a day, and an hour is the least it uses (a shorter value is raised to its
`minimumUpdateCheckInterval`). Every start checks at once (the owner, 2026-10-16 night; before,
a launch checked only when the last check was older than the hour), then Sparkle every hour; a
check that finds a release shows the Update button, not Sparkle's window. What it costs: one HTTPS
request for `appcast.xml` to GitHub Pages per start and per hour for each running copy with
Preferences › App › Check for updates automatically on, instead of one a day; the disk image is downloaded only when the user chooses to install (Cogpit sets no
`SUAutomaticallyUpdate`). No cluster traffic: a check reads no kubeconfig and talks to no
cluster. A copy takes the interval with the release that brings it.

**Approve the appcasts in the order of the releases.** Each draft builds on the feed Pages serves
when the draft runs, so a release approved after a newer one would deploy its older feed over the
newer one (2026-10-16: v0.15.0's approval after v0.16.0's put the feed back to 0.15.0 alone, mended
by re-running v0.16.0's draft). **Since 2026-10-16 the deploy refuses such a feed** (the owner's
decision, F18, corrected by him the same day): `hack/appcast verify -live` fetches the served feed,
which must verify with the update key, and fails when the new feed would take back a newer release
it serves: a channel's newest served item newer than the new feed's newest of that channel, or a
channel gone. The job's error names the item and the way out: re-run the refused release's
"Appcast (draft)" (sign and deploy follow; sign waits for the approval again), which builds on the
feed Pages serves now and adds its release. **The newer release wins:** approving in order does not
help when both drafts ran before the first approval, as on 2026-10-16, when v0.19.0's draft (16:01
UTC) predated v0.18.0's deploy (16:34 UTC) and the stricter first version of the check refused
v0.19.0. Now such a feed is deployed, and the served items it lacks below its channel's newest are
named in a warning (`::warning::appcast: the feed leaves out 0.18.0 (stable) …`): copies are offered
the newest release, and the left-out item leaves the feed's history unless the release's draft is
re-run before the deploy. An item older than every item its channel keeps (each channel keeps its
10 newest) goes without a warning. Tested with signed feeds (`TestVerifyKeepsTheServedFeed`,
`TestFetchLive`). The site's artifact is named
`github-pages-<run attempt>`, since `actions/deploy-pages` refuses a run holding two artifacts of
one name and every attempt of the deploy job uploads its own; before that change (2026-10-16) a
re-run failed so, mended by deleting the run's `github-pages` artifacts (`gh api -X DELETE
repos/guided-traffic/cogpit/actions/artifacts/<id>`) and re-running the deploy job.

**The runs that waited from before that check** (2026-10-16): a waiting job runs the workflow and
code of its tag, and a tag older than the check has neither. The appcast runs of v0.13.0 and v0.14.0
(their drafts of 2026-10-15 23:53 UTC, made before any feed was served, held their own release
alone) waited for an approval that would have put the feed back to that release; they were
cancelled on 2026-10-16 (the owner's choice), so neither can reset the feed. 0.13.0 and 0.14.0
have no item in it, which costs nothing (Sparkle offers the newest only); their disk images stay
at their releases.

**Locally**, with the variables the jobs get: `make appcast-draft VERSION=…` (needs
`GITHUB_REPOSITORY`, `gh` and `SPARKLE_PUBLIC_ED_KEY`), `make appcast-sign VERSION=…`
(`SPARKLE_ED_PRIVATE_KEY`), `make appcast-verify VERSION=…`. **Checked 2026-10-14** on macOS 26.6
with stand-ins for `gh` and the network and a throwaway Ed25519 key made with Go's `crypto/ed25519`
(Sparkle's `generate_keys` keeps its key in the login keychain, which was not touched): the first
item starts the feed (404), Sparkle's `sign_update` signs the disk image and the feed and
`hack/appcast verify` accepts both; a second release builds on that signed feed and keeps the first
item; a feed altered after signing is refused by the draft, an environment without a reviewer by
the signature, a missing key skips with `signed=false`, a prerelease goes to `beta`;
`actionlint` 1.7.12 (with `shellcheck`) on the workflow, `shellcheck` on the scripts. **Not checked
before the first run with the key:** the environment API's answer to the job's token, Pages'
deploy from a tag, Sparkle in an installed copy reading the feed.

### Setting up Sparkle

The owner's steps, in this order, so that the key never sits in an environment without its rules
([H-92](../security/updates.md#h-92-an-environment-sparkle-without-rules-hands-the-update-key-to-any-run)).
**State on 2026-10-15:** steps 1 and 2 done by the lead with the owner's go-ahead (`gh api`: the
environment `sparkle` with the owner as required reviewer, the tag rule `v*` only and no admin
bypass, which `appcast.sh`'s own check reads as "1 required reviewer(s), deployments from tag:v*";
Pages from GitHub Actions, the environment `github-pages` admitting `main` and tags `v*`); steps
3 to 5 are the owner's (the key never passes through an agent), done by the owner the same day: the
secret in `sparkle` since 07:00 UTC and the variable set (checked with `gh secret list --env
sparkle` and `gh variable list`, 2026-10-15). Since then the builds carry the key (the app job of
v0.11.0 and v0.12.0: "Sparkle bundled with the update key"), but v0.11.0 and v0.12.0 got no files
([when it fails](#when-signing-or-notarization-fails)) and v0.13.0's notarization was still running
when this was written, so the first signed appcast below has not been seen yet:

1. **The environment `sparkle`** (Settings → Environments; it may already exist without rules,
   created by the first run that named it): required reviewer the owner; deployment branches and
   tags "Selected branches and tags" with one rule, tag `v*`, and no branch; optionally "Allow
   administrators to bypass" off. The signing job refuses to sign until it sees a reviewer and
   exactly the tag rule `v*`.
2. **GitHub Pages:** Settings → Pages → Source "GitHub Actions". That creates the environment
   `github-pages`, whose deployment rule admits the default branch only: add a tag rule `v*`
   (Settings → Environments → `github-pages`), or the deploy from a release's tag is refused
   ("Tag … is not allowed to deploy to github-pages due to environment protection rules", as a
   third party reported it; not tried here).
3. **The key:** on the owner's Mac, Sparkle's `generate_keys` from the pinned and verified archive
   (`make sparkle`, then `.cache/sparkle/2.10.0/bin/generate_keys`): it keeps the private key in
   the login keychain and prints the public key. `generate_keys -x <file>` exports the private key (the base64 of its
   seed) for the secret; delete the file afterwards and keep the keychain item, or an offline copy,
   as the only backup.
4. **The secret** `SPARKLE_ED_PRIVATE_KEY` in the environment `sparkle`: the exported file's
   content.
5. **The variable** `SPARKLE_PUBLIC_ED_KEY` in the repository (Settings → Variables → Actions; a
   repository variable, since the app's build job has no environment): the public key as printed.
   Not a secret: every installed copy shows it in its `Info.plist`.

The next release is then the first built with the key, and its appcast jobs wait for the owner's
approval in `sparkle`. **What the first signed appcast must show:** "Cogpit.app (macOS arm64)"
prints `update key: <the public key>`; "Appcast (draft)" a notice that no appcast is on Pages yet
and `drafted=true`; "Appcast (sign)" `environment sparkle: 1 required reviewer(s), deployments
from tag:v*` and `signed=true`; "Appcast (deploy to Pages)" `appcast: … verifies with the update
key, 1 items` and the deployment's URL. Then `curl -s https://guided-traffic.github.io/cogpit/appcast.xml`
ends in a `sparkle-signatures` block. The copy from that release cannot update to anything yet; the
release after it is the first update: Help › Check for Updates… in the installed copy shows it,
installs it with one click, and Cogpit starts again in the new version (Console.app logs Sparkle's
steps). **Seen 2026-10-16:** the owner's MacBook, 0.15.0 installed from its disk image, updated to
0.16.0 on 2026-10-16: after `defaults delete io.guided-traffic.cogpit SULastCheckTime` and a
relaunch, the launch check found 0.16.0 at once and showed the Update button (Sparkle's gentle
reminder); its click handed the update to Sparkle, which installed it and started Cogpit again (a
person's observation, not a test). Not seen yet: a scheduled check of a copy that has a last check
time, and Console.app's log of the steps. A release whose appcast jobs fail keeps its files; re-run
the failed jobs.

## What gets built

| Platform | Today (2026-10-15) | Planned | Signing |
|---|---|---|---|
| macOS (minimum: [below](#the-minimum-macos)) | `Cogpit-<v>-macos-arm64.dmg` with `Cogpit.app` for Apple silicon, from the first release after v0.4.0 on, in the releases whose pipeline finished (v0.6.0 to v0.8.0; v0.4.0 and earlier: `Cogpit_<v>_darwin_arm64.zip`); `cogpit-cli` inside the app (`Contents/MacOS`) from the first release after v0.6.0 on, before as archives for amd64 and arm64 | `Cogpit-<v>-macos-universal.dmg` ([D-4](decisions.md#d-4-packaging-and-updates)), built per architecture with cgo and joined with `lipo` | the app with its `cogpit-cli` and the disk image signed with the Developer ID (hardened runtime, timestamp), the disk image notarized, its ticket stapled; without the secrets none (ad-hoc only) |
| Windows 10+ x64 and arm64 | nothing since D-16's revision (2026-10-14; up to v0.6.0 a `cogpit-cli` zip) | `Cogpit-<v>-windows-<arch>.msi`, a portable zip (M11), on native runners (cgo), with `cogpit-cli` inside | planned: Authenticode |
| Linux x86_64 and aarch64 | nothing since D-16's revision (2026-10-14; up to v0.6.0 a `cogpit-cli` tar.gz) | AppImage, deb, rpm, tar.xz, Flatpak (M11), on native runners (cgo, OpenGL and X11/Wayland headers), with `cogpit-cli` inside | planned: checksums, SBOM, signed update manifest |

**The macOS download is the app bundle `Cogpit.app` in a disk image**: the standard way to install
a Mac app outside the App Store, without a terminal. Opening the image shows the app next to a link
to Applications; the user drags it across. The disk image is notarized and its ticket stapled, so
it also opens offline without a Gatekeeper prompt. Sparkle's updates use the same disk image
(built 2026-10-14; the update key exists since 2026-10-15), and the Homebrew cask (`auto_updates true`) is to.

**A release is the complete product only** (owner, 2026-10-14,
[D-16](decisions.md#d-16-the-first-real-release)): `cogpit-cli` comes inside the app, never as a
download of its own, so a system gets `cogpit-cli` when it gets the desktop app. `cogpit-cli` is
pure Go (`CGO_ENABLED=0`) and builds for any system from source (`CGO_ENABLED=0 GOOS=<os>
GOARCH=<arch> go build ./cmd/cogpit-cli`). The desktop app needs cgo for Fyne's OpenGL driver, so
each platform builds on its own runner. Whether `fyne package`
alone covers MSI, AppImage, deb, rpm and Flatpak, or `packaging/` needs extra tools, is criterion S7
of the [spike](spike-fyne.md).

**A release has no demo cluster** (owner, 2026-10-16,
[D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)): the synthetic cluster is for
development. `make dist-app` builds both binaries with the [`Makefile`](../../Makefile)'s
`DIST_TAGS` (`nodemo`), which leave out package `internal/demo`, the flags `--demo`, `--demo-pods`
and `--demo-events`, and the context `demo` (the welcome page, the navigator, `cluster_contexts`
and `cogpit-cli contexts` list none, and a kubeconfig context called `demo` is one like any
other); it fails unless both binaries recorded the tag (`go version -m` shows `-tags=nodemo`).
Development builds (`make build`, `make app`, `go run`), `make run-demo`, the tests and
`make showcase` keep the demo. What it saves (2026-10-16, `-s -w`, darwin/arm64): 308,768 bytes of
the app's `cogpit` (108,649,938 to 108,341,170) and 271,024 of `cogpit-cli` (75,175,330 to
74,904,306), about 0.3 % each. CI's lint job checks the variant (`make check-nodemo`,
[the jobs](#ci)); [`TestReleaseBuildsLeaveTheDemoOut`](../../internal/engine/connector_test.go)
fails when `dist-app` no longer passes `DIST_TAGS` or they no longer name the tag
`connector_nodemo.go` is built with. Releases up to v0.16.0 carry the demo cluster.

**A release has Fyne's thread check off** (owner, 2026-10-19 and 2026-10-20, since v0.30.0;
v0.29.3 and earlier have it on;
[D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)): `make dist-app`
builds with the Makefile's `FYNE_THREAD_CHECKS` defaulting to `off`, which adds Cogpit's build tag
`nothreadcheck`: the app then declares itself migrated to `fyne.Do` in its metadata, and Fyne no
longer checks on every canvas refresh which goroutine it runs on (that check cost most of a fast
scroll's frame). Not Fyne's own tag `migrated_fynedo`, which would also make Fyne's caches
single-threaded. Every other build has the check on. `dist-app` fails unless both binaries
recorded the tag, or recorded it though `FYNE_THREAD_CHECKS=on`. **The way back without a
code change:** set the repository variable `FYNE_THREAD_CHECKS` to `on` (GitHub › Settings ›
Variables); [`build.yml`](../../.github/workflows/build.yml) passes it to `make dist-app`, and the
next releases keep the check, their About saying "Fyne thread check on". Locally: `make dist-app
VERSION=x.y.z FYNE_THREAD_CHECKS=on`.

### The minimum macOS

macOS 13 or later, as D-4 and the parity pages aim (since 2026-10-14): 13.0 is Go 1.27's own
minimum (its linker writes `minos 13.0`). The [`Makefile`](../../Makefile) sets
`MACOSX_DEPLOYMENT_TARGET` and `-mmacosx-version-min` in `CGO_CFLAGS` and `CGO_LDFLAGS` on macOS
(`MACOS_MIN`), and `make app` writes `LSMinimumSystemVersion` 13.0 into the bundle's `Info.plist`
and fails when the binary says another `minos`. Before, the cgo build set no deployment target:
v0.4.0's app says `minos 26.0`, the macOS of the runner that built it, and `LSMinimumSystemVersion`
10.11 (`fyne package`'s default), so it is likely not to start on a macOS older than 26 (checked
with `vtool -show-build`; not tried on an older macOS). Not tried on macOS 13 either: no such Mac
or runner was at hand.

### The version a build reports

Since 2026-10-14 (the owner's finding F2,
[D-16](decisions.md#d-16-the-first-real-release)) one variable holds it for both binaries:
[`internal/buildinfo`](../../internal/buildinfo/buildinfo.go)'s `version`, which the
[`Makefile`](../../Makefile)'s `DIST_LDFLAGS` set
(`-X github.com/guided-traffic/cogpit/internal/buildinfo.version=$(VERSION)`). About Cogpit,
`cogpit-cli version`, the MCP server and the stdio bridge read it; Sparkle does not, it compares the
bundle's `CFBundleVersion` ([updates](#updates)).

| Build | The binaries report | The bundle's versions |
|---|---|---|
| A release: `make dist-app VERSION=0.7.0` in the pipeline | `0.7.0`; About "Version 0.7.0" # example | `0.7.0` and `0.7.0` # example |
| `go install …/cmd/cogpit-cli@v0.7.0`, or a build of a clean checkout of the tag | `0.7.0`, the module version Go recorded # example | – |
| `make build`, `make app`, `go build` anywhere else | `dev` and the commit Go recorded, `-dirty` with uncommitted changes (`cogpit-cli dev 0691fb3277bb-dirty (go1.27.2, darwin/arm64)` # example); About "Development build", "Commit 0691fb3277bb (modified)" # example | `0.0.1` and `0.0.1`, `VERSION`'s default: integers for Sparkle, below every release; such a bundle has no update key, so Sparkle never compares it |
| `go run ./cmd/cogpit` | `dev`: Go records no commit for `go run` | – |

Nothing asks git or the network at run time: Go writes the module version and the commit
(`vcs.revision`, `vcs.modified`) into the binary when it builds inside a git checkout. `make
dist-app` refuses to run without `VERSION=` on its command line, so that no local build claims to be
release 0.0.1, and [`TestMakefileSetsTheVersion`](../../internal/buildinfo/buildinfo_test.go) fails
when `DIST_LDFLAGS` no longer name the variable (the linker ignores a `-X` for a symbol that does
not exist, and the release would say `dev`; the release job's `cogpit-cli version` check catches it
too). Checked locally (2026-10-14): `go run` reports `dev`, `go build` `dev` and the commit with
`-dirty`, the flag `0.0.99`; a release's About not tried in a real window. Before, About read Fyne's
app metadata, whose version is Fyne's default 0.0.1 in every Cogpit build, a release's too
(`fyne package --executable` injects none), and both binaries defaulted to `0.0.1-mvp`.

## Updates

Cogpit shows available updates and installs them with one click
([D-4](decisions.md#d-4-packaging-and-updates)); on macOS through Sparkle 2, built 2026-10-14.
The app ([`internal/update`](../../internal/update/)) loads `Sparkle.framework` from its bundle at
run time and starts it once its window runs; Preferences › App › Check for updates automatically
(on by default) lets Sparkle check the appcast at every start and every hour (since 2026-10-16;
Sparkle's default, a day, before), Help › Check for Updates… checks now,
and Sparkle's own window shows a new release with its notes and installs it on the user's click:
it verifies the disk image's EdDSA signature with the key in the app's `Info.plist` before it
extracts anything, then Apple's code signature of the new app (the same Developer ID team), and
offers only a `CFBundleVersion` newer than the running one. The appcast itself is signed with the
same key ([the appcast](#the-appcast)), which lives in the environment `sparkle` behind the owner's
approval. A build without the key (every local one, every release up to v0.10.0) has no updater, and
says so. `cogpit-cli` has no updater of its own: the one inside the app is
replaced with the app; installs owned by a package manager (Homebrew, winget, Flathub, deb or rpm repositories) are to hand over to the manager.
Security perspective: [updates](../security/updates.md).

## Secrets

| Secret | For | Where | Without it |
|---|---|---|---|
| `APP_CLIENT_ID`, `APP_PRIVATE_KEY` (the organisation's GitHub App) | Semantic Release's tag, release and release commit; Renovate | organisation secrets, exist | no release (the job fails) |
| `MACOS_SIGN_P12` (the Developer ID Application certificate with its key, base64), `MACOS_SIGN_P12_PASSWORD` | signing the app with its `cogpit-cli`, and the disk image | GitHub environment `release` (created 2026-10-13, deployment rule "tags `v*`"): exist since 2026-10-14, the certificate "Developer ID Application: Hans Fischer (6L57P2HXN6)" from Apple's G2 sub-CA, valid to 2027-11-13, its key also in the owner's login keychain; checked locally by a signature with the hardened runtime and Apple's timestamp. Used since 2026-10-14 by the job "Sign and notarize (macOS)" only, since 2026-10-15 "Sign and submit (macOS)" only, imported into a temporary keychain with Apple's intermediate `DeveloperIDG2CA.cer`, which the workflow pins ([signing and notarization](#signing-and-notarization)); used by every tag run since v0.5.0 | all of the Apple secrets missing: unsigned app zip, and the release page says so; some of them missing: the job fails ([when it fails](#when-signing-or-notarization-fails)) |
| `APPLE_TEAM_ID` (a variable) | the signing identity: the script signs only with a Developer ID of this team and checks every signature for it | environment `release`, exists since 2026-10-14 (`6L57P2HXN6`, not a secret: every signed app shows it) | the job fails when the secrets are there |
| `APPLE_API_KEY_P8` (an App Store Connect team API key, role Developer, base64), `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID` | notarization with `notarytool` | environment `release`, exist since 2026-10-14; checked locally: `notarytool` accepts the key and a submission with the role Developer. Used since 2026-10-14 by the same job, since 2026-10-15 by "Sign and submit (macOS)" for the submission and "Notarize and staple (macOS)" for the verdict; the `.p8` (base64, or its PEM text as is) lives in a file of each job's temporary directory, in the first until the disk image is submitted, in the second until Apple's verdict is in | as for `MACOS_SIGN_P12` |
| Windows code-signing certificate or signing service credentials | Authenticode | planned (M11) | SmartScreen warns |
| `SPARKLE_ED_PRIVATE_KEY` (Sparkle's EdDSA update key, the base64 of its seed as `generate_keys -x` exports it) | signing each release's disk image and the appcast at `https://guided-traffic.github.io/cogpit/appcast.xml` (D-4, owner, 2026-10-14), in the job "Appcast (sign)" only, through `sign_update`'s standard input ([the appcast](#the-appcast)) | decided (owner, 2026-10-13): a secret of the GitHub environment `sparkle`, the owner its required reviewer, deployment rule tags `v*`; generated by the owner with Sparkle's `generate_keys`, never seen by an agent ([setting up Sparkle](#setting-up-sparkle)). Created by the owner on 2026-10-15 (checked with `gh secret list --env sparkle`); the job refuses to use it while the environment lacks its reviewer or its tag rule | that release's item is not added and Pages keeps the feed it has (a notice, never a failure) |
| `SPARKLE_PUBLIC_ED_KEY` (a repository variable) | the app's `SUPublicEDKey` (`make dist-app`), and the appcast jobs' checks with the public key | set by the owner on 2026-10-15 ([setting up Sparkle](#setting-up-sparkle)); not a secret: every installed copy shows it | the app has no updater ("this build has no update key"), and the release gets no appcast item |

Who holds which key and what follows if one leaks:
[updates › keys](../security/updates.md#keys-and-who-holds-them).
**Human:** the Apple material exists since 2026-10-14 (set up by the owner with the lead), and the
release pipeline uses it since the same day; the certificate expires on 2027-11-13 and must be
renewed and the secret replaced before then. The
Windows material does not exist yet; Sparkle is built since 2026-10-14 and its update key exists
since 2026-10-15; each release's appcast jobs wait for the owner's approval in `sparkle`
([setting up Sparkle](#setting-up-sparkle),
[G14](../parity/gaps.md#g14-packaging-signing-and-updates-macos)).
