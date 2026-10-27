# Releasing

Every push to `main` and every pull request runs the checks ([CI](#ci)). Once every job of a push
run on `main` passed, semantic-release publishes a GitHub release ([release flow](#release-flow)),
and [`build.yml`](../../.github/workflows/build.yml) builds the complete product from its tag
([D-16](decisions.md#d-16-releases-and-what-they-carry)), attaches it, and publishes the update
feeds and the APT repository on GitHub Pages. What vouches for each file:
[updates](../security/updates.md#what-a-release-holds); the platform targets against Lens:
[platform parity](../parity/platform-parity.md#operating-systems-and-packages).


## CI

[`release.yml`](../../.github/workflows/release.yml) runs on every push to `main` and every pull
request; a newer push cancels the running run of its ref. Every check is a job of its own with its
own make target, so it runs the same locally; no job should need more than ten minutes (each has a
30-minute limit for runs without a build cache). Linux jobs run on the organisation's self-hosted
runners (one shared machine) unless the repository variable `LINUX_RUNNER` names others
(`ubuntu-latest`); the macOS and Windows jobs run on GitHub's hosted runners. Every job's setup is
[`.github/actions/go-setup`](../../.github/actions/go-setup/action.yml): Go from `go.mod`, a build
cache per kind of build (saved by `main`'s pushes only), Fyne's Linux build dependencies installed
in the background. `GOFLAGS=-count=1` keeps cached test results from passing a gate.

| Job | Make target | Runs |
|---|---|---|
| Code Linting | `make lint`, `make check-nodemo` | golangci-lint with the team's linters and depguard's layering; the release variant (`-tags nodemo`) has no demo cluster, its lint and its tests |
| Parity | `make parity` | [`hack/parity`](../../hack/parity/main.go) with `-check` ([parity README](../parity/README.md#make-parity)) |
| GoSec Security Scan | `make gosec` | gosec over the shipped code, tests left out |
| Vulnerability Check | `make vuln` | govulncheck over the code paths Cogpit calls |
| Cyclomatic Complexity | `make cyclo` | gocyclo over `cmd`, `internal`, `hack`, `third_party`; fails above `CYCLO_THRESHOLD` |
| Unit Tests (no race) | `make test-norace-coverage` | what the race build skips, and the UI's render tests in `UI_RENDER_SHARDS` parallel shards, with coverage |
| Race Tests (UI n/8) | `make test-race-ui RACE_SHARD=n RACE_SHARDS=8` | `internal/ui/fyneui` under the race detector without its render tests, one shard of eight (hack/shard: a test's shard is a hash of its name) |
| Race Tests (other packages) | `make test-race-rest` | every other package and the terminal fork under the race detector (`internal/ui/logo` runs in the no-race job) |
| Integration Tests (kind) | `make kind-up`, `make test-kind` | the cluster tests (`TestKind*`, tag `kind`) on a kind cluster created while they compile; the add-on stacks' tests are skipped (`KIND_ADDONS=1` runs them) |
| Integration Tests (kind drain) | `make kind-drain-up`, `make test-kind-drain` | cordon and drain on a three-node cluster |
| Integration Tests (envtest) | `make test-envtest` | the envtest tests against a real kube-apiserver and etcd, pinned by SHA-512 |
| macOS Tests | `make test-darwin` | the macOS tier ([testing › Tiers](testing.md#tiers)) under the race detector |
| macOS Lint and App | `make lint LINT_PKGS=…`, `make app` | lint of the packages with darwin-only files; the app bundle |
| Unit Tests (windows) | `make test-windows-unit` | `go vet` and every package's tests but the window's, without the race detector |
| UI Tests (windows) | `make test-windows-ui` | the window's tests in `WINDOWS_UI_SHARDS` shards, `WINDOWS_UI_JOBS` at once |
| Linux Package | `make dist-linux`, `make test-linux-package` | the `.deb` as releases build it, lintian, the APT repository's scripts end to end with a throwaway key |
| Windows Package | `make dist-windows`, `make test-windows-package` | the setup as releases build it, a silent install and uninstall for the runner's user |
| Combined Coverage Report | `make coverage-report` | merges every job's profile |
| Semantic Release | – | needs every job above; publishes the release ([release flow](#release-flow)) |

### Coverage

Every test job writes its profiles to `.cache/coverage` and uploads them: `unit-*.out` (no race,
`-covermode=set`), `race-ui-<n>.out` and `race-rest.out`, `integration.out`, `integration-drain.out`
and `envtest.out` (the cluster tests with `-coverpkg=./...`, so they count for every package they
reach). The Combined Coverage Report job merges them with [`hack/coverage`](../../hack/coverage/main.go):
a block counts once, as covered when any profile covered it. It writes the total and a table per
package to the job summary and [`.github/badges/coverage.json`](../../.github/badges/coverage.json),
the README's badge (≥ 90 % brightgreen, ≥ 80 % green, ≥ 70 % yellow, ≥ 60 % orange, else red),
which Semantic Release commits with the release. Two runs of `main` differ by a few dozen
statements that only some runs reach (a cancellation, a retry): no change of coverage.

### Cyclomatic complexity

`CYCLO_THRESHOLD` is a ratchet (the owner's decision): a change that makes the most complex function
simpler lowers it to the new maximum in the same commit (`make cyclo-report` shows it); no
refactoring of its own for it.

### Running a job locally

```sh
make lint check-nodemo parity gosec vuln cyclo
make test-norace-coverage                      # what the race build skips, and the UI's render tests
make test-race-ui RACE_SHARD=3 RACE_SHARDS=8   # one shard; make test-ui-race runs all eight in parallel
make test-race-rest
make kind-up && make test-kind                 # the cluster tests; make kind-down after
make test-envtest                              # fetches the pinned binaries first
make test-darwin                               # macOS only
make coverage-report                           # merges what is in .cache/coverage
```

Tests run with `KUBECONFIG=/dev/null` and without `KUBERNETES_SERVICE_HOST` and
`KUBERNETES_SERVICE_PORT` (`TEST_ENV` in the [Makefile](../../Makefile)): inside a pod, as on the
self-hosted runners, client-go would otherwise take the pod's namespace as a context's default.

### Renovate

[`renovate.yml`](../../.github/workflows/renovate.yml) runs self-hosted Renovate with an
installation token of the team's GitHub App (organization secrets `APP_CLIENT_ID`,
`APP_PRIVATE_KEY`); the configuration is [`.github/renovate.json`](../../.github/renovate.json).

- Minor, patch and digest updates of Go modules, the Go version, GitHub Actions and the Makefile's
  tool pins merge on their own once CI passes; majors wait for review. Groups: the Go version with
  `golang.org/x/*`; `k8s.io/*` and `sigs.k8s.io/*`; Fyne.
- The Makefile's tool pins carry a `# renovate:` comment a custom manager reads; `bin/` tools are
  rebuilt when their pin or the Go of `go.mod` changes (the Makefile's `tool_stamp`).
- dmgbuild's requirements ([`dmgbuild-requirements.txt`](../../hack/macos/dmgbuild-requirements.txt))
  never merge on their own: their output ships in every disk image.
- Sparkle and WinSparkle are pinned in the Makefile by release tag and SHA-256 (`SPARKLE_VERSION`,
  `SPARKLE_SHA256`, `WINSPARKLE_TAG`, `WINSPARKLE_SHA256`) under a
  `# renovate: datasource=github-release-attachments` comment; Renovate's pull request updates both
  lines. Neither merges on its own: their code installs Cogpit's updates.
- The node shell's and the debug container's default images (`DefaultNodeShellImage` in
  [`nodeshell.go`](../../internal/engine/nodeshell.go), `DefaultDebugImage` in
  [`debugcontainer.go`](../../internal/engine/debugcontainer.go)), pinned by tag and digest, have
  regex managers of their own; minor and digest updates merge once the kind job opened a node shell
  and a debug container with the new image (`TestKindNodeShell`, `TestKindDebugContainer`).
- Nobody pushes onto a Renovate branch: Renovate never rebases a branch that carries someone else's
  commit. A pull request that needs a change of its own is closed, or its rebase box ticked.
- sigstore's trusted root ([`sigstore_trusted_root.json`](../../internal/kubectl/sigstore_trusted_root.json))
  is no dependency Renovate sees: `make sigstore-root` refreshes it through sigstore's TUF
  repository; run it before a release and commit a diff as `build(deps):`
  ([H-35](../security/trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)).

## Release flow

[`.releaserc.json`](../../.releaserc.json) (preset `conventionalcommits`), the tooling pinned in
[`package.json`](../../package.json) and [`package-lock.json`](../../package-lock.json):

1. Commits follow [Conventional Commits](conventions.md#commits-and-pull-requests): `feat:` makes a
   minor release, `fix:` and `perf:` a patch release, other types none.
2. Once every job of a push run on `main` passed, the job Semantic Release
   ([`release.yml`](../../.github/workflows/release.yml)) takes a token of the organisation's GitHub
   App (`permission-contents: write`), runs `npm ci` and `npm audit signatures`, puts the combined
   coverage badge in place and runs `npx semantic-release`: the version from the commits since the
   last tag, the notes, the badge committed as `chore(release): <version> [skip ci]` with the notes
   in its body when the badge changed, the tag `v<version>` on that commit (else on the released
   one) and the GitHub release. `[skip ci]` keeps the release commit from starting a run.
3. The release's `published` event starts [the release pipeline](#the-release-pipeline), because
   the App's token made the release: one made with `GITHUB_TOKEN` starts no workflow.

A release that commits the badge moves `main`: `git pull --rebase` before the next push.

**Releases stay below 1.0.0** until the owner decides on 1.0: they continue from the anchor tag
`v0.0.0`, `releaseRules` (`{"breaking": true, "release": "minor"}`) make a breaking change a minor
release, and commits carry no `!` and no `BREAKING CHANGE:` footer
([conventions](conventions.md#commits-and-pull-requests)).

### When no release comes

Semantic Release needs every job of the run: one failed or cancelled job skips it. A newer push
cancels the running run (`cancel-in-progress`), which loses nothing: the next green run releases
every commit since the last tag. A release is missing when no run on `main`'s newest commit got
through.

- **A job failed setting up or timed out:** `gh run list --branch main --limit 3`, then for the
  newest commit's run `gh run view <id>` and `gh run rerun <id> --failed`. Nothing re-runs by
  itself. Do not push while the re-run runs: the push cancels it.
- **Runner packages** come through [`hack/ci/apt-install.sh`](../../hack/ci/apt-install.sh): each
  try bounded (4 minutes for `apt-get update`, 6 for the install), three tries,
  `dpkg --configure -a` before a retry, so a hanging mirror fails its step instead of the job's
  timeout.
- **semantic-release stopped after the tag** (a tag without a GitHub release, no pipeline): a re-run
  publishes nothing, its commit being behind `main`. Check that `package.json`'s `repository.url`
  names the repository as GitHub spells it (through a rename's redirect the tag passes, the notes
  ref does not). Finish by hand:

  ```sh
  git notes --ref semantic-release-v<version> add -f -m '{"channels":[null]}' v<version>
  git push origin refs/notes/semantic-release-v<version>
  git log -1 --format=%B <release commit> | tail -n +3 > notes.md
  gh release create v<version> --verify-tag --title v<version> --notes-file notes.md --latest
  ```

  A person's token starts the pipeline as the App's does, with that person as the release's author.

## What gets built

| Platform | Download | Signing | Not built |
|---|---|---|---|
| macOS 13 and later ([below](#the-minimum-macos)), Apple silicon | `Cogpit-<v>-macos-arm64.dmg`: `Cogpit.app` beside a link to Applications, `cogpit-cli` in `Contents/MacOS` ([macOS](#macos)) | the app, its `cogpit-cli` and the disk image signed with the Developer ID, the disk image notarized and its ticket stapled ([signing and notarization](#signing-and-notarization)); without the secrets `Cogpit_<v>_darwin_arm64.zip`, signed ad-hoc | the universal (Intel) app `Cogpit-<v>-macos-universal.dmg` and the Homebrew cask ([D-4](decisions.md#d-4-packaging-and-updates)) |
| Windows 10 1809 and later, x64 | `Cogpit-<v>-windows-x64-setup.exe` with `cogpit.exe`, `cogpit-cli.exe` and `WinSparkle.dll` ([Windows](#windows)) | no Authenticode ([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)); the update key's EdDSA signature, which WinSparkle checks before it runs the setup | the arm64 setup, winget, Scoop ([D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)) |
| Linux amd64 and arm64: Debian 13, Ubuntu 24.04 and later | `cogpit_<v>_amd64.deb`, `cogpit_<v>_arm64.deb` with both binaries ([Linux](#linux)) | the packages unsigned, as Debian's are; the APT repository's index signed with Cogpit's OpenPGP key ([the APT repository](#the-apt-repository)), and apt checks each package against that index | rpm, Flatpak, AppImage |

Every release also carries `SHA256SUMS` over its files, unsigned. Not built either: an SBOM and
signed checksums ([G14](../parity/gaps.md#g14-packaging-signing-and-updates-macos)).

**Only the complete product** ([D-16](decisions.md#d-16-releases-and-what-they-carry)): `cogpit-cli`
ships inside the app, the package and the setup, never as a download of its own. It is pure Go and
builds for any system from source (`CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build
./cmd/cogpit-cli`); the app needs cgo for Fyne's OpenGL driver, so each platform builds on its own
runner. The `.deb` is built by [`hack/linux/deb.sh`](../../hack/linux/deb.sh) with `dpkg-deb`, not
by `fyne package`.

**No demo cluster** ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)):
`make dist-app`, `dist-linux` and `dist-windows` build with the [`Makefile`](../../Makefile)'s
`DIST_TAGS` (`nodemo`), which leave out package `internal/demo`, the flags `--demo`, `--demo-pods`
and `--demo-events`, and the context `demo`. Each target fails unless both binaries recorded the
tag (`go version -m`). Development builds, `make run-demo`, the tests and `make showcase` keep the
demo. CI's Code Linting job builds the variant (`make check-nodemo`), and
[`TestReleaseBuildsLeaveTheDemoOut`](../../internal/engine/connector_test.go) fails when the dist
targets stop passing `DIST_TAGS` or the tags stop naming the one
[`connector_nodemo.go`](../../internal/engine/connector_nodemo.go) is built with.

**Fyne's thread check off**
([D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)): the dist
targets default `FYNE_THREAD_CHECKS` to `off`, which adds the tag `nothreadcheck`: the app declares
itself migrated to `fyne.Do` (`appMetadata` in [`fyneui.go`](../../internal/ui/fyneui/fyneui.go)),
and Fyne stops checking on each canvas refresh which goroutine it runs on. Fyne's own tag
`migrated_fynedo` is not used: it would also make Fyne's caches single-threaded. Every other build
has the check on, and the dist targets fail when the binaries' tags disagree with
`FYNE_THREAD_CHECKS`. **To turn the check back on without a code change,** set the repository
variable `FYNE_THREAD_CHECKS` to `on`: [`build.yml`](../../.github/workflows/build.yml) passes it to
the macOS, Linux and Windows builds, and their About says "Fyne thread check on". Locally:
`make dist-app VERSION=x.y.z FYNE_THREAD_CHECKS=on`.

### The minimum macOS

macOS 13 or later: the target of
[platform parity](../parity/platform-parity.md#operating-systems-and-packages) and Go 1.27's own
minimum (its linker writes `minos 13.0`). On macOS the [`Makefile`](../../Makefile) sets
`MACOSX_DEPLOYMENT_TARGET` and `-mmacosx-version-min` in `CGO_CFLAGS` and `CGO_LDFLAGS` from
`MACOS_MIN`, so the cgo objects target 13.0 and not the build machine's macOS; `make app` writes
`LSMinimumSystemVersion` 13.0 into the bundle's `Info.plist` and fails when either binary says
another `minos` (`otool -l`). Untested on macOS 13: CI's macOS jobs run on `macos-latest`.

### The version a build reports

One variable holds it for both binaries: `version` in
[`internal/buildinfo`](../../internal/buildinfo/buildinfo.go), which the Makefile's `DIST_LDFLAGS`
set (`-X github.com/guided-traffic/cogpit/internal/buildinfo.version=$(VERSION)`). About, the top
bar's version label, `cogpit-cli version`, the MCP server, the stdio bridge, the diagnostics report
and the Linux and Windows updaters read it; Sparkle compares the bundle's `CFBundleVersion`.

| Build | The binaries report | The bundle's versions |
|---|---|---|
| A release: `make dist-app VERSION=0.7.0` # example, likewise `dist-linux` and `dist-windows` | `0.7.0`; About "Version 0.7.0" | `0.7.0` and `0.7.0` |
| `go install …/cmd/cogpit-cli@v0.7.0`, or a build of a clean checkout of the tag | `0.7.0`, the module version Go recorded | – |
| `make build`, `make app`, `go build` anywhere else | `dev` and the commit Go recorded, `-dirty` with uncommitted changes: `cogpit-cli dev 0691fb3277bb-dirty (go1.27.3, darwin/arm64)` # example; About "Development build", "Commit 0691fb3277bb (modified)" # example | `0.0.1` and `0.0.1`, `VERSION`'s default # default: integers for Sparkle, below every release; such a bundle has no update key, so no updater |
| `go run ./cmd/cogpit` | `dev`, About "Development build": Go records no commit for `go run` | – |

Nothing asks git or the network at run time: Go writes the module version and the commit
(`vcs.revision`, `vcs.modified`) into a binary it builds inside a git checkout. The dist targets
refuse to run without `VERSION=`, so that no local build claims to be release 0.0.1.
[`TestMakefileSetsTheVersion`](../../internal/buildinfo/buildinfo_test.go) fails when
`DIST_LDFLAGS` stop naming the variable (the linker ignores a `-X` for a missing symbol, and the
release would say `dev`); the release jobs also check that `cogpit-cli version` says the release's
version.

## The release pipeline

[`build.yml`](../../.github/workflows/build.yml) "Release Build" runs on `release: published`, on the
tag (`github.ref` is `refs/tags/v<version>`, which the environments' rule "tags `v*`" admits). Every
job checks out the tag; builds take `VERSION` from it without the `v` and use no build cache
(`cache: none`), so no artifact comes from objects another run wrote.

```text
macOS    Cogpit.app ─► Sign and submit ─► Notarize and staple ─► Attach ─► Appcast draft ─► sign ─► deploy
Linux    Cogpit.deb amd64, arm64 ─► Attach (Linux) ─► APT repository sign ─► deploy
Windows  Cogpit Setup ─► Attach (Windows, after Attach (Linux)) ─► Windows appcast draft ─► sign ─► deploy
```

| Job | Runner | Make target | What it does |
|---|---|---|---|
| Cogpit.app (macOS arm64) | `macos-latest` | `k8s-eol K8SEOL_FLAGS=-fallback`, `dist-app`, `dist-dmg-template` | [the Kubernetes release table](#the-kubernetes-release-table); `Cogpit_<v>_darwin_arm64.zip` (`make app` with Sparkle 2 and `cogpit-cli` in `Contents/MacOS`, signed ad-hoc, both binaries [as a release builds them](#what-gets-built), which `checkDistBinaries` confirms); checks the bundle's versions, Sparkle's key against `SPARKLE_PUBLIC_ED_KEY`, `codesign --verify --strict --deep`, `cogpit-cli`'s architecture, identifier and `version`; [the window's template](#the-disk-images-window). No secret |
| Sign and submit (macOS) | `macos-latest`, environment `release` | `dist-macos-sign` | the signed disk image, submitted to Apple without waiting (artifact `macos-signed`, [signing](#signing-and-notarization)); without the secrets the zip unchanged |
| Notarize and staple (macOS) | `macos-latest`, environment `release` (API key only) | `dist-macos-notarize` | Apple's verdict on that submission (up to 330 minutes; timeout 345, a hosted job's limit 360), the ticket stapled, Gatekeeper's checks; outputs `signed`, `identity` |
| Attach to the release | `LINUX_RUNNER` or self-hosted | `dist-checksums` | exactly the verdict's files (the disk image, or unsigned the zip) and `SHA256SUMS`; the download note: signed by which certificate, or unsigned and how macOS lets it start |
| Cogpit.deb (Linux amd64), (Linux arm64) | `ubuntu-24.04`, `ubuntu-24.04-arm` | `k8s-eol K8SEOL_FLAGS=-fallback`, `dist-linux` | [the package](#linux); checks its fields, lintian, `cogpit-cli version`, the sources file's URI. No secret |
| Attach to the release (Linux) | `ubuntu-26.04` | [`publish.sh`](../../hack/linux/publish.sh) | both packages or none, the install note |
| APT repository (sign), (deploy to Pages) | `ubuntu-26.04`, environments `apt`, `github-pages` | `apt-repo`; `apt.sh verify`, `newer` | [the APT repository](#the-apt-repository) |
| Appcast (draft), (sign), (deploy to Pages) | `macos-latest`; `macos-latest` in `sparkle`; `LINUX_RUNNER` or self-hosted in `github-pages` | `appcast-draft`, `appcast-sign`, `appcast-verify` | [the appcast](#the-appcast), for a signed download only |
| Cogpit Setup (Windows x64) | `windows-latest`, MSYS2's MINGW64 | `k8s-eol K8SEOL_FLAGS=-fallback`, `dist-windows`, `test-windows-package` | [the setup](#windows); checks the update key in `cogpit.exe`, installs, reinstalls and uninstalls it. No secret |
| Attach to the release (Windows) | `ubuntu-26.04` | [`publish.sh`](../../hack/windows/publish.sh) | the setup, the install note |
| Windows appcast (draft), (sign), (deploy to Pages) | `ubuntu-26.04`; `macos-latest` in `sparkle`; `ubuntu-26.04` in `github-pages` | `appcast-windows-draft`, `appcast-windows-sign`, `appcast-windows-verify` | `appcast-windows.xml`, signed with the same update key ([Windows](#windows)) |

Each attach job merges its lines into the release's one `SHA256SUMS`
([`sha256sums.sh`](../../hack/ci/sha256sums.sh)) and appends its note to the release's text, in
turn in the concurrency group `release-files-<tag>`. GitHub keeps one running and one waiting job
per group and cancels an older waiting one, so the Windows attach job waits for the Linux one. The
Pages deploys share the group `pages`, where a waiting deploy is cancelled the same way when
another queues behind it: re-run it, or the next release's deploy of that part brings the newest.
Pages serves only what the last deploy uploaded, so each deploy carries the other parts as Pages
serves them (a feed byte for byte, the APT repository checked against its key by `apt.sh mirror`),
in a site artifact named per run attempt (`actions/deploy-pages` refuses two of one name in a run).

### The Kubernetes release table

Both binaries embed [`releases.json`](../../internal/model/k8seol/releases.json), by which the window
marks a cluster's Kubernetes version and kubelets against upstream's support
([D-91](decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)): every minor's end of
life (an ended one's final patch), the maintained minors' release day and maintenance start, the
fetch day. Cogpit never fetches it at run time.

- **Source:** [`hack/k8seol`](../../hack/k8seol/main.go) downloads `kubernetes/website`'s
  `data/releases/schedule.yaml` and `eol.yaml` (branch `main`, CC BY 4.0,
  [ATTRIBUTION.md](../../ATTRIBUTION.md)), checks them (dates parse, minors without a gap, ends of
  life in order, each 12 to 18 months after its release) and the new table against the committed
  one (no minor lost, no newest minor older, no end of life moved by more than 120 days), prints the
  changes and writes the file.
- **Each release carries the table of its day:** the macOS, Linux and Windows build jobs run
  `make k8s-eol K8SEOL_FLAGS=-fallback` first; a failed download or check keeps the committed table
  with a warning on the run (a minor newer than it gets the estimate). A changed table makes the
  release's binaries record `vcs.modified=true` (`go version -m`).
- **The committed table** (what local builds embed) is refreshed by hand: `make k8s-eol`, and
  commit the file when it changed. No job commits it (D-91).
- **A changed file** can shift the dates the window shows, never run code
  ([H-130](../security/updates.md#h-130-the-releases-kubernetes-release-table-is-fetched-unreviewed)).

## macOS

To check a download (locally the same steps run into `dist/`:
[running the signing locally](#running-the-signing-locally), then `make dist-checksums`):

```sh
shasum -a 256 -c --ignore-missing SHA256SUMS
spctl -a -t open --context context:primary-signature -vv Cogpit-<v>-macos-arm64.dmg  # source=Notarized Developer ID
"/Volumes/Cogpit <v>/Cogpit.app/Contents/MacOS/cogpit-cli" version                  # mounted: the release's version
```

### The disk image's window

The window [D-70](decisions.md#d-70-the-disk-images-window) settles (600 × 340 on the app's plate,
the icons at 124 points, an arrow to Applications with Cogpit's helm wheel faint behind it, names on
grey pills; the app's icon on the volume) comes from
[`packaging/dmg/lab.json`](../../packaging/dmg/lab.json) (the DMG lab's link inside) and
[`background.png`](../../packaging/dmg/background.png) with `@2x`, the lab's drawing without icons
and names. To change the look: change it in the lab, replace `lab.json` and both PNGs, and look at
both themes in a real Finder window.

Two jobs make it, so that no code from PyPI runs beside the signing identity:

1. **"Cogpit.app (macOS arm64)"**: `make dist-dmg-template`
   ([`dmg-template.sh`](../../hack/macos/dmg-template.sh)) installs dmgbuild, pinned by version and
   hash ([`dmgbuild-requirements.txt`](../../hack/macos/dmgbuild-requirements.txt)), into a throwaway
   virtual environment (Python 3.15 on CI, locally 3.10 or newer, `PYTHON` picks one) and writes
   `Cogpit-<v>-template.dmg`: a writable HFS+ image of the volume "Cogpit <v>" with `.DS_Store`
   (written by dmgbuild without Finder, [`dmg_settings.py`](../../hack/macos/dmg_settings.py)),
   `.background.tiff`, `.VolumeIcon.icns` and the link to `/Applications`, no app; it travels in the
   artifact `macos-app`.
2. **"Sign and submit (macOS)"** ([`release.sh`](../../hack/macos/release.sh) `build_dmg`, Apple's
   tools only): checks the template (UDRW, the volume's name, exactly those four entries, the link's
   target), resizes it, copies the signed app in with `ditto`, removes `.fseventsd`, converts it to
   UDZO (zlib level 9) and signs it. No Finder flag on the app: an extended attribute on the bundle
   fails `codesign`'s strict check.

### Signing and notarization

[`hack/macos/release.sh`](../../hack/macos/release.sh) runs in "Sign and submit (macOS)"
(`make dist-macos-sign`, steps 1 to 4) and "Notarize and staple (macOS)"
(`make dist-macos-notarize`, steps 5 to 7), the only jobs that see the environment `release`'s
secrets (the second only the team API key); `make dist-macos` runs it all at once. It builds
nothing and runs no Go, not even the bundled `cogpit-cli`, so no code of Cogpit's or its
dependencies' runs beside the signing identity.

1. **Keychain:** a temporary one (random, masked password, first in the search list) with the `.p12`
   from `MACOS_SIGN_P12` (key not extractable, for `codesign` only) and Apple's intermediate
   `DeveloperIDG2CA.cer`, checked against the workflow's `MACOS_SIGN_CA_SHA256`; a `.p12` exported
   with its chain brings the intermediate along, and the script checks that one against the pin.
   Exactly one valid "Developer ID Application" identity of `APPLE_TEAM_ID`, used by its SHA-1.
2. **The app, inside out, never `--deep`:** Sparkle's `Autoupdate`, `Updater.app`,
   `Sparkle.framework` (Sparkle's documented order; no XPC services), `cogpit-cli` (identifier
   `io.guided-traffic.cogpit.cli`), the bundle; each with the hardened runtime, Apple's timestamp and
   no entitlements. Other nested code is refused (`Contents/MacOS` holds exactly the app's executable
   and `cogpit-cli`, `Contents/Frameworks` only Sparkle; no `PlugIns`, `XPCServices`, `Helpers`,
   `Library`), and every Mach-O file must carry the bundle's authority and the hardened runtime, so a
   helper a newer Sparkle adds cannot reach Apple signed ad-hoc.
3. **The disk image** `Cogpit-<v>-macos-arm64.dmg`: [the window's template](#the-disk-images-window)
   with the signed app, signed; mounted, it holds the window's files, the link and `Cogpit.app` only.
4. **Submission** with `notarytool` and the team API key (the `.p8` owner-only in the work
   directory, deleted after use), without waiting: the disk image, the app inside it. Its id, name
   and SHA-256 go into `notary-submission.json` in the artifact `macos-signed`.
5. **Verdict:** the disk image's SHA-256 checked against the record, the team's latest submissions
   listed, Apple asked every minute for up to `NOTARY_TIMEOUT_MINUTES` (330 on CI, 45 by default),
   Apple's log printed; anything but `Accepted` fails.
6. **Stapling and Gatekeeper:** `stapler staple` and `validate`, so the disk image opens offline;
   `spctl -a -t open --context context:primary-signature -vv` on it and `spctl -a -t execute -vv` on
   the mounted app must say `source=Notarized Developer ID`, after its contents, signatures and
   version are checked again. With Gatekeeper off (`override=security disabled`) the script warns,
   and `Accepted` with the stapled ticket stand alone.
7. **Cleanup** of the keychain, the search list and the key files when the script ends, and again in
   an `if: always()` step.

Secrets reach only the steps that use them, as environment variables; the script echoes nothing
derived from them and runs without `set -x`. The notarization job's outputs `signed` and `identity`
(the certificate's name) tell "Attach to the release" which files to expect and what its note says.

Only the disk image is submitted and carries the stapled ticket; Apple checks the app and its
`cogpit-cli` nested in it, and the copy in Applications has no stapled ticket of its own. Not
verified: that this copy starts offline the first time, and that a quarantined copy's `cogpit-cli`,
run from a terminal before the app was ever opened, starts without a prompt.

**No entitlements** for any part: Go generates no code at run time, the app loads only Apple's
frameworks and Sparkle (same Developer ID, so library validation holds), and neither binary uses the
camera, microphone, location, contacts or Apple Events. Re-signing drops the
`com.apple.application-identifier` Sparkle's ad-hoc `Autoupdate` ships with, as Sparkle documents.
A needed entitlement also changes `check_signature`, which refuses any. Sparkle's EdDSA signature
over the disk image is made after stapling, its last change, in "Appcast (sign)".

### When signing or notarization fails

With the secrets present, a failed signature, notarization or check fails its job, and so do some
secrets without the others; nothing falls back to an unsigned download. Without any of them (a fork,
a run outside the environment) the release gets the unsigned app zip, and its note says so. A
failure leaves the release without the macOS download and its appcast item; the Linux and Windows
jobs attach theirs independently.

1. The failed job's error line names what failed; for the notarization, the submission, Apple's
   status and Apple's log.
2. **Still in progress** after 330 minutes: `gh run rerun <run id> --failed`. "Notarize and staple
   (macOS)" and the jobs after it run again with the artifact `macos-signed` and wait for the same
   submission, so a verdict given meanwhile is there at once. Never re-run "Sign and submit (macOS)"
   for this: a new timestamp makes a new disk image, a new submission at the end of Apple's queue.
3. **Invalid:** Apple's log lists each issue with its file. Fix the cause in a `fix:` commit, which
   makes a new release; delete the failed one with its tag, or leave it.
4. **Keychain or identity** ("0 valid identities", a wrong password, an expired certificate): fix
   the secret in the environment `release`, then re-run the failed jobs.

A re-run uses the artifacts earlier attempts of the run uploaded (kept seven days); a new upload
replaces an artifact of the same name.

### Running the signing locally

```sh
make dist-app VERSION=0.0.99                      # dist/: the app zip, cogpit-cli inside
make dist-dmg-template VERSION=0.0.99             # dist/: the window's template (Python 3.10+)
make dist-macos VERSION=0.0.99 MACOS_SIGN=adhoc   # dist/macos: the disk image, signed ad-hoc
make dist-macos VERSION=0.0.99                    # no MACOS_SIGN and no secrets: the app zip, unchanged
```

`MACOS_SIGN=adhoc` runs every step but the notarization and Gatekeeper, with ad-hoc signatures, the
hardened runtime, each signature's check and the disk image's. The Developer ID comes from the
secrets only, never from a login keychain. Sourced, `release.sh` only defines its functions, to try
one with stand-ins for `notarytool`, `stapler` and `spctl` on `PATH`.

### The appcast

Sparkle's appcast, `https://guided-traffic.github.io/cogpit/appcast.xml`, is the feed every installed
copy reads at its `SUFeedURL` (the Makefile's `SPARKLE_FEED_URL`,
[D-4](decisions.md#d-4-packaging-and-updates)): one signed RSS feed with an item per release
(versions, minimum macOS, notes, the disk image's URL, length and EdDSA signature). Renaming the
repository moves it, and installed copies keep the old URL
([H-97](../security/updates.md#h-97-the-feed-url-follows-the-repositorys-name)). How often copies
read it: [updates](#updates); what each check prevents:
[updates › chain of trust](../security/updates.md#chain-of-trust). The three jobs run after
"Attach to the release", so an item never points at a missing download, and cannot take a
release's files back:

| Job | What it does |
|---|---|
| Appcast (draft): `make appcast-draft` | reads the app in the stapled disk image; an app without `SUPublicEDKey` gets no item (`drafted=false`). Checks that the key is the repository variable, the release asset downloadable and the served feed (past Pages' cache; a 404 starts a new one) signed by the key; [`hack/appcast`](../../hack/appcast/main.go) `add` drafts the feed with the release's notes and dates (`gh release view`) and a placeholder for the disk image's signature. No secret; Go's standard library only |
| Appcast (sign): `make appcast-sign`, environment `sparkle` | without `SPARKLE_ED_PRIVATE_KEY` a notice and `signed=false`, never a failure. With it: refuses unless `sparkle` admits tags `v*` only ([H-92](../security/updates.md#h-92-an-environment-sparkle-without-rules-hands-the-update-key-to-any-run)), asks for no reviewer ([H-141](../security/updates.md#h-141-the-update-key-signs-without-an-approval)); Sparkle's pinned `sign_update` signs the disk image the draft names (by SHA-256), then the feed, and verifies both. The key reaches `sign_update` on standard input only; no Go runs; work files removed with `if: always()` |
| Appcast (deploy to Pages): `make appcast-verify`, environment `github-pages` | `hack/appcast verify`: the feed's signature, every item, the release's item naming this disk image by length and signature; `-live`: the served feed verifies, and no newer release it serves is lost (below). Then deploys the site |

Items are newest first by `sparkle:version`; a re-run replaces its item; each channel keeps its 10
newest. A prerelease goes to the channel `beta`, which no copy follows (Preferences › Update channel
offers Stable only; a prerelease's version has no `CFBundleVersion` scheme, and
[`sparkle.sh`](../../hack/macos/sparkle.sh) refuses it, D-4). The notes are the release's text up
to the download note (its `---`), at most 32 KiB, Markdown, with a link to the release.

**Overlapping releases:** each draft builds on the feed served when it runs. The deploy refuses a
feed that would take back a newer served release (a channel's newest served item newer than the new
feed's newest, or a channel gone) and names it: re-run the refused release's "Appcast (draft)" (sign
and deploy follow). A served item below the new feed's newest that the feed lacks is left out with a
warning (`the feed leaves out <version> (stable) …`); re-running the deployed release's draft keeps
it. Tests: `TestVerifyKeepsTheServedFeed`, `TestFetchLive`.

A failed appcast job leaves the release's files in place: re-run the failed jobs. Locally:
`make appcast-draft`, `appcast-sign`, `appcast-verify` with `VERSION=…` and the jobs' variables
(`GITHUB_REPOSITORY`, `gh`, `SPARKLE_PUBLIC_ED_KEY`; `SPARKLE_ED_PRIVATE_KEY` to sign).

### Setting up Sparkle

In this order, so that the key never sits in an environment without its rules
([H-92](../security/updates.md#h-92-an-environment-sparkle-without-rules-hands-the-update-key-to-any-run));
steps 3 to 5 are the owner's, the private key never passes through an agent:

1. **The environment `sparkle`** (the first run that names it may have created it without rules):
   deployment branches and tags "Selected branches and tags" with the one rule tag `v*`;
   "Allow administrators to bypass" off; no required reviewer
   ([H-141](../security/updates.md#h-141-the-update-key-signs-without-an-approval)). "Appcast (sign)"
   signs only when it reads exactly the tag rule `v*`; its log says
   `environment sparkle: <n> required reviewer(s), deployments from tag:v*`.
2. **GitHub Pages:** Settings → Pages → Source "GitHub Actions". The environment `github-pages` it
   creates admits the default branch only: add the tag rule `v*`, or a release's deploy is refused.
3. **The key:** `make sparkle`, then `.cache/sparkle/<SPARKLE_VERSION>/bin/generate_keys`: it keeps
   the private key in the login keychain and prints the public key; `generate_keys -x <file>`
   exports the private key (the base64 of its seed). Delete the file after step 4; the keychain item,
   or an offline copy, is the only backup.
4. **The secret** `SPARKLE_ED_PRIVATE_KEY` in `sparkle`: the exported file's content.
5. **The repository variable** `SPARKLE_PUBLIC_ED_KEY` (the app's build job has no environment): the
   public key. Not a secret: every installed copy shows it in its `Info.plist`.

A signed feed ends in a `sparkle-signatures` block:
`curl -s https://guided-traffic.github.io/cogpit/appcast.xml`.

## Linux

A Debian package per architecture, `cogpit_<version>_amd64.deb` and `cogpit_<version>_arm64.deb`,
and Cogpit's APT repository on GitHub Pages, which the package sets up for updates
([D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)). Built on Ubuntu
24.04, it runs on Debian 13 and Ubuntu 24.04 and later; install with
`sudo apt install ./cogpit_<version>_amd64.deb`.

`make dist-linux VERSION=x.y.z` ([`deb.sh`](../../hack/linux/deb.sh), on Debian or Ubuntu with
`dpkg-dev`, for the machine's architecture) builds both binaries as a release builds them,
position-independent (`-buildmode=pie`), and packs them with
[`packaging/linux`](../../packaging/linux):

| Path | What |
|---|---|
| `/usr/bin/cogpit`, `/usr/bin/cogpit-cli` | the app and the command-line tool |
| `/usr/share/applications/io.guided-traffic.cogpit.desktop` | the launcher; `StartupWMClass=Cogpit` and its name, the Wayland app_id, pair the window with it |
| `/usr/share/icons/hicolor/<n>x<n>/apps/io.guided-traffic.cogpit.png` | the icon in eight sizes, 16 to 512, drawn by `hack/icon` |
| `/usr/share/metainfo/io.guided-traffic.cogpit.metainfo.xml` | the AppStream entry software centres show |
| `/usr/libexec/cogpit/update`, `/usr/share/polkit-1/actions/io.guided-traffic.cogpit.update.policy` | the update helper the Update button runs through pkexec, and its polkit action (an administrator's password, kept a few minutes) |
| `/etc/apt/sources.list.d/cogpit.sources` | the repository with its public key inline (`Signed-By`); a conffile: deleted, it stays deleted through upgrades; `apt purge cogpit` removes it |
| `/usr/share/doc/cogpit/`, `/usr/share/lintian/overrides/cogpit` | copyright and changelog; lintian's accepted findings with their reasons |

`Depends`: the libraries the app links (dpkg-shlibdeps), the X11 and Wayland libraries GLFW loads at
run time, and libdecor with a plugin, which draws the title bar's buttons where the compositor draws
none (GNOME without XWayland); `Recommends: pkexec`. No maintainer scripts: dpkg's triggers refresh
the desktop and icon caches. `Maintainer` is the Makefile's `DEB_MAINTAINER`. lintian fails on an
error; newer lintian's `appstream-metadata-validation-failed` (the hyphen in the app's id) is not
overridden, since CI's older lintian treats an override of an unknown tag as an error.

### The APT repository

`https://guided-traffic.github.io/cogpit/apt`, beside the appcasts on the same Pages site:

```text
apt/
  files.sha256                                every file with its SHA-256, for the next deploy
  dists/stable/InRelease, Release, Release.gpg
  dists/stable/main/binary-<arch>/Packages, Packages.gz, by-hash/SHA256/<hash>
  pool/main/c/cogpit/cogpit_<version>_<arch>.deb
```

[`hack/linux/apt.sh`](../../hack/linux/apt.sh) makes it:

- **"APT repository (sign)"** (environment `apt`, tags `v*`, no reviewer; bash, apt-ftparchive and
  gpg only) runs `make apt-repo`: `mirror` fetches the served repository and checks every file
  against the packages' key (none served: nothing; a failed check: an error); `build` adds this
  run's packages, keeps the **two newest versions per architecture** (`KEEP`), writes the index with
  `Acquire-By-Hash` and keeps the served generation's by-hash files; `sign` signs `InRelease` and
  `Release.gpg` with `APT_SIGNING_KEY` (on gpg's standard input, in a temporary keyring whose gpg
  agent is ended after both signatures) and refuses a key whose fingerprint is not that of
  [`cogpit-archive-keyring.asc`](../../packaging/linux/cogpit-archive-keyring.asc). Without the key
  nothing is signed or deployed.
- **"APT repository (deploy to Pages)"** checks it (`verify`), skips the deploy with a warning when
  Pages serves a newer release (`newer`: an older release's run ending after a newer one's), adds
  the served appcasts and deploys.

Pages caches files for up to ten minutes; since apt fetches index files by the hash an `InRelease`
names and the previous generation stays, a client holding an older `InRelease` still finds what it
names. CI's job Linux Package runs `make test-linux-package` on every push: lintian and
[`apt-test.sh`](../../hack/linux/apt-test.sh), the scripts end to end with a throwaway key.

**The key:** OpenPGP Ed25519, no passphrase, fingerprint
`BE09 5407 4B36 CBDE 8875  7E52 A9D2 608B 0DC7 6494`. To renew it before it expires
([renewals](#secrets)): extend the expiry with the private key (`gpg --quick-set-expire`), replace
the secret and `cogpit-archive-keyring.asc`, and release; the new package's sources file carries
the renewed key, and dpkg replaces an unchanged conffile (a user who changed it keeps the old key
and sees apt's error once it expires). A leaked key:
[H-135](../security/updates.md#h-135-the-apt-key-signs-without-an-approval).

## Windows

The Windows download is `Cogpit-<version>-windows-x64-setup.exe`, an NSIS setup for Windows 10
1809 (build 17763) and later, x64, which installs for the user without an administrator by default
([D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)); WinSparkle updates
it. Its options, files and entries: [usage › Install on Windows](usage.md#install-on-windows).

**Building it:** `make dist-windows VERSION=x.y.z` on Windows, in Git Bash or MSYS2's bash with
MinGW-w64 gcc and NSIS 3 ([`setup.sh`](../../hack/windows/setup.sh),
[`cogpit.nsi`](../../packaging/windows/cogpit.nsi)); on CI in MSYS2's MINGW64 shell, whose signed
packages (`msys2/setup-msys2`) bring gcc, make, unzip and NSIS.

- Both executables are built as the macOS app's (`DIST_TAGS`, the thread check off,
  `checkDistBinaries`). `cogpit.exe` is a GUI program (`-H=windowsgui`: no console window) with the
  feed's URL and the update key's public half linked in (`internal/update`'s `windowsFeedURL` and
  `edPublicKey`, from `SPARKLE_PUBLIC_ED_KEY`; without the key it has no updater).
  `TestMakefileBuildsTheFeedIntoTheApp` checks those names: the linker ignores a `-X` for a missing
  symbol.
- [`resources.sh`](../../hack/windows/resources.sh) makes both executables' resources with go-winres
  for the build and removes them after it: the icon (`hack/icon -ico`, 16 to 256 pixels, the group
  `GLFW_ICON`, the name GLFW loads the window's icon by), the manifest (Windows 10 and later,
  per-monitor DPI awareness v2, long paths, common controls 6, no elevation) and the version
  information ([`winres.json`](../../packaging/windows/winres.json)).
- `WinSparkle.dll` (x64) is the WinSparkle release the Makefile pins by tag and archive SHA-256
  (`WINSPARKLE_TAG`, `WINSPARKLE_SHA256`; [`winsparkle.sh`](../../hack/windows/winsparkle.sh)),
  unsigned as WinSparkle ships it
  ([H-140](../security/updates.md#h-140-winsparkledll-is-a-third-party-binary-without-a-signature)).

**On an update** the setup updates the installation where and as it was installed, found by its
uninstall entry `…\Uninstall\io.guided-traffic.cogpit` (HKCU, or HKLM's 64-bit view), whose
`InstallLocation` also tells `internal/update` that a `cogpit.exe` is the setup's. It waits for a
running `cogpit.exe` to quit (silently up to a minute, then it fails; interactively it asks), moves
a running `cogpit-cli.exe` aside, and with `/relaunch` starts Cogpit as the desktop's user.
`make test-windows-package` ([`setup-test.sh`](../../hack/windows/setup-test.sh); CI's job Windows
Package, [CI](#ci)) installs a setup silently into a folder of its own, checks it, installs it again
over itself and uninstalls it; it refuses to run where Cogpit is installed for the user.

**The feed** `https://guided-traffic.github.io/cogpit/appcast-windows.xml` sits beside
`appcast.xml` and the APT repository on Pages, in Sparkle's format and signed with the same update
key ([the appcast](#the-appcast)). Its items carry `sparkle:os="windows-x64"`,
`sparkle:installerArguments="/S /relaunch"`, `sparkle:minimumSystemVersion` 10.0.17763 and the
release's page as `sparkle:releaseNotesLink` (WinSparkle shows HTML, not Markdown).
[`hack/windows/appcast.sh`](../../hack/windows/appcast.sh) drafts it with
[`hack/appcast`](../../hack/appcast/main.go) (`-os windows-x64 -installer-args …`), which builds
only on a feed the update key signed and keeps one system's items per feed;
[`hack/macos/appcast.sh`](../../hack/macos/appcast.sh) `sign` signs the setup and the feed as it
does for macOS; the deploy refuses a feed that would take back a newer release Pages serves.

## Updates

| Platform | Updater | Reads | Installs only |
|---|---|---|---|
| macOS | Sparkle 2 (`SPARKLE_VERSION`) in the bundle, loaded at run time by [`internal/update`](../../internal/update/) | `appcast.xml`, signed with the update key (`SURequireSignedFeed`) | a disk image the update key signed, checked before extraction (`SUVerifyUpdateBeforeExtraction`), whose app has the same Developer ID team and a newer `CFBundleVersion` |
| Linux | apt | Cogpit's APT repository ([above](#the-apt-repository)) | packages its signed index names |
| Windows | WinSparkle (`WinSparkle.dll`) | `appcast-windows.xml`: Cogpit checks its signature before the Update button shows a release; WinSparkle reads it again to install and checks only the setup's signature ([updates › Windows](../security/updates.md#windows)) | a setup the update key signed, run as `/S /relaunch` |

Each copy checks at every start and every hour while Preferences › App › Check for updates
automatically is on (the default); a newer release shows the Update button (`app.installUpdate`),
and Help › Check for Updates… (`app.checkForUpdates`) checks now. On macOS these checks are
Cogpit's own (`scheduledChecks` in [`sparkle_darwin.go`](../../internal/update/sparkle_darwin.go),
each Sparkle's probing check): [`sparkle.sh`](../../hack/macos/sparkle.sh) writes
`SUEnableAutomaticChecks` and `SUAllowsAutomaticUpdates` NO into `Info.plist`, so Sparkle neither
checks on a schedule of its own nor downloads a release before the user installs it. A check reads
no kubeconfig and talks to no cluster. Only a release build updates itself: on macOS and Windows one
built with the update key (`SPARKLE_PUBLIC_ED_KEY`) and installed from the disk image or by the
setup, on Linux the installed package's `/usr/bin/cogpit` with its sources file in place; any other
copy says why. `cogpit-cli` has no updater: it comes with the app, the package or the setup.
What a user sees on each platform:
[usage › Preferences › App: updates](usage.md#preferences--app-updates); the mechanism:
[architecture § 11](architecture.md#11-updates-internalupdate); the chain of trust:
[updates](../security/updates.md).

## Secrets

| Secret or variable | For | Where | Without it |
|---|---|---|---|
| `APP_CLIENT_ID`, `APP_PRIVATE_KEY` (the organisation's GitHub App) | semantic-release's tag, release and release commit; Renovate | organisation secrets | no release: the Semantic Release job fails |
| `MACOS_SIGN_P12` (the Developer ID Application certificate with its key, base64), `MACOS_SIGN_P12_PASSWORD` | signing the app, its `cogpit-cli` and the disk image in "Sign and submit (macOS)" ([signing and notarization](#signing-and-notarization)) | environment `release` | none of the Apple secrets: the unsigned app zip, and the release's note says so; only some of them: the job fails ([when it fails](#when-signing-or-notarization-fails)) |
| `APPLE_TEAM_ID` (a variable, `6L57P2HXN6`; every signed app shows it) | the signing identity: the script signs only with a Developer ID of this team and checks every signature for it | environment `release` | the job fails when the secrets are there |
| `APPLE_API_KEY_P8` (an App Store Connect team API key, role Developer, base64 or the `.p8`'s PEM text), `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID` | `notarytool` in "Sign and submit (macOS)" and "Notarize and staple (macOS)" | environment `release` | as for `MACOS_SIGN_P12` |
| `SPARKLE_ED_PRIVATE_KEY` (Sparkle's EdDSA update key, the base64 seed `generate_keys -x` exports) | signing the disk image, the setup and both feeds ("Appcast (sign)", "Windows appcast (sign)"); refused while the environment lacks its tag rule `v*` ([setting up Sparkle](#setting-up-sparkle)) | environment `sparkle` | no feed item for the release; Pages keeps the feed it has (a notice, never a failure) |
| `APT_SIGNING_KEY` (the private half of [the APT repository's key](#the-apt-repository), armored) | signing `InRelease` and `Release.gpg` ("APT repository (sign)"); refused unless it is the key of [`cogpit-archive-keyring.asc`](../../packaging/linux/cogpit-archive-keyring.asc) | environment `apt` | the repository is not signed; Pages keeps the one it has (a notice, never a failure) |
| `SPARKLE_PUBLIC_ED_KEY` (a variable; every installed copy carries it) | `SUPublicEDKey` in the macOS app, `edPublicKey` in `cogpit.exe`, the appcast jobs' checks | repository variable | no updater ("this build has no update key") and no feed items |
| Authenticode signing material | the Windows setup and both executables | none exists: not built ([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)) | SmartScreen warns at the setup's first start; updates are checked by the update key all the same |

The environments `release`, `sparkle` and `apt` admit only tags `v*` and have no required reviewer
([H-141](../security/updates.md#h-141-the-update-key-signs-without-an-approval) for `sparkle`,
[H-135](../security/updates.md#h-135-the-apt-key-signs-without-an-approval) for `apt`). Two more
repository variables change the release build when set: `FYNE_THREAD_CHECKS`
([what gets built](#what-gets-built)) and `LINUX_RUNNER` ([CI](#ci)). Who holds each key and what a
leak means: [updates › keys](../security/updates.md#keys-and-who-holds-them).

**Renewals:**

- The Developer ID Application certificate ("Developer ID Application: Hans Fischer (6L57P2HXN6)",
  issued by Apple's Developer ID G2 CA) expires on 2027-11-13: renew it at Apple and replace
  `MACOS_SIGN_P12` and its password before that day.
- Apple's G2 intermediate `DeveloperIDG2CA.cer`, pinned by its SHA-256 in
  [`build.yml`](../../.github/workflows/build.yml), expires on 2031-09-17.
- The APT key expires on 2031-10-19: a release must carry its renewal
  ([the APT repository](#the-apt-repository)) before that day.
