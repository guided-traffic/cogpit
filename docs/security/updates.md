# Updates

How a Cogpit copy learns of a new release and replaces itself: Sparkle 2 on macOS, apt on Linux,
WinSparkle on Windows ([D-4](../developer/decisions.md#d-4-packaging-and-updates),
[D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages),
[D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)).
`cogpit-cli` has no updater of its own: the app bundle
([D-16](../developer/decisions.md#d-16-releases-and-what-they-carry)), the package or the setup replaces
it under the same checks. The update channel is one of the two places where Cogpit downloads code
that then runs; the other, the `kubectl` a terminal offers on the user's click, is checked against
the Kubernetes release's sigstore signature and its transparency-log entry, not Cogpit's keys
([trust boundaries](trust-boundaries.md#boundaries-and-required-checks),
[H-35](trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)).

## Chain of trust

```text
release pipeline ─► Cogpit-<v>-macos-arm64.dmg (cogpit-cli inside the app): Developer ID-signed, notarized, stapled
   "Appcast (draft)", no secret ─► the served feed (only if the update key signed it) + this release's item
   "Appcast (sign)", environment sparkle ─► EdDSA signatures over the disk image, then the feed (sign_update)
   "Appcast (deploy to Pages)" ─► checked with the public key ─► https://guided-traffic.github.io/cogpit/appcast.xml
                                   │
Cogpit.app (Sparkle 2) ─► reads the feed over HTTPS: at every start, every hour, on Help › Check for Updates…
       ─► the feed's EdDSA signature with SUPublicEDKey ─► an item newer than CFBundleVersion, for this macOS?
       ─► Cogpit's Update button ─► the user's click: Sparkle's window, Install Update
       ─► the disk image from the release ─► its EdDSA signature, before anything is extracted
       ─► Apple's code signature of the new app (the running app's team) ─► replace the app, relaunch
```

The chain is Sparkle 2's ([documentation](https://sparkle-project.org/documentation/#segue-for-security-concerns)),
set up in the app's `Info.plist` by [`hack/macos/sparkle.sh`](../../hack/macos/sparkle.sh); the
app's side is [`internal/update`](../../internal/update/), the pipeline's
[`build.yml`](../../.github/workflows/build.yml), [`hack/macos/appcast.sh`](../../hack/macos/appcast.sh)
and [`hack/appcast`](../../hack/appcast/main.go)
([releasing › the appcast](../developer/releasing.md#the-appcast)).

**How often a copy asks:** at every start and every hour while Preferences › App › Check for
updates automatically is on; Help › Check for Updates… asks in any case. On macOS these are
Cogpit's own probing checks (`scheduledChecks` in
[`sparkle_darwin.go`](../../internal/update/sparkle_darwin.go)); Sparkle's own are off
(`SUEnableAutomaticChecks` NO), since a release one of them finds is held and no check runs while
it is held ([D-4](../developer/decisions.md#d-4-packaging-and-updates);
`TestSparkleChecksAtEveryStart`, `TestSparkleScheduledChecksFindTheNewestRelease`). Each check is
one HTTPS request to GitHub Pages, which sees the copy's address, the time and the User-Agent
Sparkle sends with the app's version; no system profile (`SUSendProfileInfo` is never set, and
`SUEnableAutomaticChecks` being set keeps away Sparkle's prompt that would offer it), no
kubeconfig, cluster or credential, and no download before the user clicks Install Update
(`SUAllowsAutomaticUpdates` NO). On Windows Cogpit reads the feed itself on the same schedule; on
Linux a check reads apt's local lists only.

| Check | Where | Prevents |
|---|---|---|
| The feed's EdDSA signature with the key in the app (`SURequireSignedFeed`) | every copy, every check | a feed changed on or by the host: forged notes or links, an item pointing elsewhere (long failures: [H-91](#h-91-after-20-days-of-unverifiable-feeds-sparkle-presents-items-again)) |
| The disk image's EdDSA signature before extraction (`SUVerifyUpdateBeforeExtraction`) | every copy, each update | a swapped download; nothing of an unsigned archive is unpacked |
| Apple's code signature of the new app against the running app's | every copy, each update | an app that is not Cogpit's team's |
| Only a newer `CFBundleVersion` is offered; `sparkle:minimumSystemVersion` | every copy | a downgrade; an update the Mac cannot run |
| The environment `sparkle` admits tags `v*` only, checked before signing (`check_environment` in `appcast.sh`) | "Appcast (sign)", "Windows appcast (sign)" | a branch's run, or one in an environment without rules, signing ([H-92](#h-92-an-environment-sparkle-without-rules-hands-the-update-key-to-any-run)) |
| The history kept only from a served feed the update key signed | both feeds' draft jobs | a feed altered on Pages being signed again with the next release |
| The signed feed and the disk image checked with the public key before Pages gets them | "Appcast (deploy to Pages)" | a feed every copy would refuse (a secret that does not match the app's key) |
| No channel's newest item older than the newest Pages serves (`hack/appcast verify -live`) | both feeds' deploy jobs | an older release's run, ending after a newer one's, taking the feed back ([H-93](#h-93-the-feeds-host-can-withhold-updates)) |
| HTTPS | the feed and the download | eavesdroppers learning which version a user runs (integrity is the signatures') |

**Tests:** `TestSparkleScriptWritesTheSettings` (the `Info.plist` settings),
`TestAddRefusesAnUnsignedHistory`, `TestVerify`, `TestVerifyKeepsTheServedFeed`. No test runs a
whole Sparkle update.

A build without the update key (every local `make app`) has no updater and says so in Preferences
› App and Help › Check for Updates…; it moves onto the chain only by installing a release's
download.

## What a release holds

semantic-release creates a tag and a GitHub release once every gate on `main` is green; the release
pipeline ([`build.yml`](../../.github/workflows/build.yml)) builds from that tag and attaches the
complete product only ([D-16](../developer/decisions.md#d-16-releases-and-what-they-carry),
[releasing › the release pipeline](../developer/releasing.md#the-release-pipeline)):

| Artifact | What vouches for it |
|---|---|
| `Cogpit-<v>-macos-arm64.dmg`, the app with `Contents/MacOS/cogpit-cli` (`make dist-app`, then `make dist-macos-sign` and `make dist-macos-notarize`, [`release.sh`](../../hack/macos/release.sh)) | the Developer ID signature (hardened runtime, Apple's timestamp) on the app, its `cogpit-cli` (sealed into the app's signature) and the disk image; Apple's notarization with the ticket stapled, so Gatekeeper accepts it offline as `Notarized Developer ID`. A failed signature, notarization or check leaves the release without files until the failed jobs are re-run, never with unsigned files under a signed note ([releasing › signing and notarization](../developer/releasing.md#signing-and-notarization)) |
| `Cogpit_<v>_darwin_arm64.zip`, in place of the disk image when a build lacks the signing secrets (a fork) | an ad-hoc signature, which seals the bundle but names nobody: not notarized, so Gatekeeper blocks the first start until the user allows it |
| `cogpit_<v>_amd64.deb`, `cogpit_<v>_arm64.deb` (`make dist-linux`) | from the release: HTTPS only ([H-136](#h-136-the-first-linux-install-has-no-signature-of-its-own)); from the APT repository: its OpenPGP signature, which apt checks ([Linux](#linux)) |
| `Cogpit-<v>-windows-x64-setup.exe` (`make dist-windows`) | from the release: HTTPS only, no Authenticode ([H-139](#h-139-the-windows-setup-is-not-signed-with-authenticode)); as WinSparkle downloads it: the update key's EdDSA signature ([Windows](#windows)) |
| `SHA256SUMS`, each of the three attach jobs merging its lines | nothing: unsigned, it shows that a download matches what CI attached, not who attached it ([H-61](#h-61-sha256sums-is-not-signed)) |
| The release's items in `appcast.xml` and `appcast-windows.xml` | the update key's EdDSA signature over the feed and over the download it names, made without an approval ([H-141](#h-141-the-update-key-signs-without-an-approval)), checked with the public key before Pages gets it |

**What the Developer ID signature and the notarization vouch for:** that the code was signed with
team `6L57P2HXN6`'s key and is unchanged since, and that Apple's automated check found no known
malware in it at submission; not which commit it was built from. Any run on a `v*` tag can have the
key sign what the run built ([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)).
The release page names the certificate the signature carries, or says the build is unsigned.

Gatekeeper checks the Developer ID signature at the first start of a download, Sparkle against
the running app's team at each update. **What does not vouch for an update:** the hosts (Pages,
GitHub's release storage), which can withhold one or serve an older signed feed
([H-93](#h-93-the-feeds-host-can-withhold-updates)), not make a copy install anything the key did
not sign.

### Keys and who holds them

| Key | Where it lives | What it can do | If it leaks |
|---|---|---|---|
| The organisation's GitHub App private key (`APP_CLIENT_ID`, `APP_PRIVATE_KEY`) | organisation secrets of `guided-traffic`, visible to this repository; Semantic Release makes a `contents: write` token from it, the Renovate job ([`renovate.yml`](../../.github/workflows/renovate.yml)) one with all of the App's permissions | push to `main`; create tags (the `v*` tag ruleset's only bypass) and releases, which start the release pipeline on whatever the tag points at | rotate the key in the App's settings, replace the secret, delete the tags and releases it made. Pull requests from forks get no secrets |
| The release pipeline's `GITHUB_TOKEN` | per job: `contents: write` only in the three attach jobs, `pages: write` only in the three Pages deploy jobs | upload assets to and edit this repository's releases; deploy to Pages | expires with the job |
| Developer ID Application certificate, App Store Connect team API key (role Developer) | secrets of the environment `release` (tags `v*` only, administrator bypass off). "Sign and submit (macOS)" reads both, "Notarize and staple (macOS)" the API key only; neither builds anything or runs Go. A temporary keychain (key not extractable, usable by `codesign` only) and an owner-only key file, deleted at the job's end ([releasing › signing and notarization](../developer/releasing.md#signing-and-notarization)) | sign and notarize as team `6L57P2HXN6`: the result passes Gatekeeper on every Mac until Apple revokes the certificate | revoke the certificate at Apple and the key in App Store Connect. Not verified: that releases notarized before stay valid |
| The APT repository's key (OpenPGP Ed25519, fingerprint `BE09 5407 4B36 CBDE 8875  7E52 A9D2 608B 0DC7 6494`, expires 2031-10-19) | `APT_SIGNING_KEY` in the environment `apt` (tags `v*` only, no required reviewer, administrator bypass on). Only "APT repository (sign)" reads it, on GitHub's hosted runner; it runs no Go and nothing of the packages, hands the key to gpg on its standard input in a temporary keyring and ends gpg's agent after signing. The owner keeps a copy and the revocation certificate outside GitHub; the public half is in every package's sources file | sign a repository whose packages every Linux copy installs, as root, as Cogpit's updates | [H-135](#h-135-the-apt-key-signs-without-an-approval) |
| The update key (Sparkle's EdDSA key) | `SPARKLE_ED_PRIVATE_KEY` in the environment `sparkle` (tags `v*` only, no required reviewer, administrator bypass off), and the owner's login keychain, where Sparkle's `generate_keys` keeps it ([setting up Sparkle](../developer/releasing.md#setting-up-sparkle)). Only "Appcast (sign)" and "Windows appcast (sign)" read it: each refuses while the environment lacks its tag rule, hands the key to `sign_update` on its standard input and runs no Go and no dependency's code. The public half, the repository variable `SPARKLE_PUBLIC_ED_KEY`, is built into every copy (`SUPublicEDKey`; on Windows `edPublicKey`) | sign feeds, disk images and Windows setups that every copy accepts | [H-15](#h-15-update-key-compromise-and-rotation) |

## Linux

The package installs `/etc/apt/sources.list.d/cogpit.sources` ([`deb.sh`](../../hack/linux/deb.sh)),
a conffile naming Cogpit's APT repository on Pages with its public key inline, so apt trusts that
key for that repository only
([D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages),
[releasing › Linux](../developer/releasing.md#linux)).

```text
release pipeline ─► cogpit_<v>_<arch>.deb (both binaries), attached to the release, its line in SHA256SUMS
   "APT repository (sign)", environment apt ─► the served repository, checked against the key
        ─► this release's packages beside the newest served ones ─► Release, signed: InRelease, Release.gpg
   "APT repository (deploy to Pages)" ─► checked with the public key, refused if Pages serves a newer one
        ─► https://guided-traffic.github.io/cogpit/apt
                                   │
apt (apt update: the system's timer, the Software Updater, or Cogpit's helper)
       ─► InRelease's signature with the key in cogpit.sources ─► Packages by the hash InRelease names
       ─► each package's SHA-256 as Packages names it ─► dpkg installs it as root
Cogpit (apt_linux.go) ─► apt-cache policy cogpit at every start and every hour: no network, no root
       ─► the Update button ─► pkexec /usr/libexec/cogpit/update (polkit: an administrator's password)
       ─► apt-get update of Cogpit's list only, apt-get install --only-upgrade cogpit ─► Restart to Update
```

| Check | Where | Prevents |
|---|---|---|
| InRelease's OpenPGP signature with the key in `cogpit.sources` (`Signed-By`) | apt, at every update of the list | a repository changed on or by the host: another package, version or index |
| The index files by the hash InRelease names (`Acquire-By-Hash`), the packages by Packages' SHA-256 and size | apt | a swapped index or package; an index cached from before a deploy mixed with a newer one |
| Only a newer version is installed (`apt upgrade`, the helper's `--only-upgrade`) | apt | a downgrade by an older signed repository (a freeze stays possible: [H-137](#h-137-the-apt-repositorys-host-can-withhold-updates)) |
| The key's fingerprint is the packages' before anything is signed (`apt.sh sign`) | "APT repository (sign)" | a secret replaced with another key, whose repository every copy would refuse |
| The served repository is fetched and checked against the key before it is kept (`apt.sh mirror`) | "APT repository (sign)", both feeds' deploy jobs | a repository altered on Pages being signed again, or deployed again with a feed |
| The new repository checked with the public key, refused when Pages serves a newer release (`apt.sh verify`, `newer`) | "APT repository (deploy to Pages)" | a repository every copy would refuse; an older release's run taking a newer one back |
| The [helper](../../packaging/linux/update) takes no argument and nothing from the caller's environment, upgrades only the package `cogpit` from Cogpit's list, and is the polkit action's only program (`org.freedesktop.policykit.exec.path`) | pkexec, the helper | the Update button running anything else as root |
| On Linux only the window runs `app.checkForUpdates` ([agents and MCP](agents-and-mcp.md)) | the engine ([`updates.go`](../../internal/engine/updates.go)) | an agent or the CLI putting a password prompt or a restart in front of the user |

**What does not vouch for an update:** the host, which can withhold the repository or serve an
older signed one ([H-137](#h-137-the-apt-repositorys-host-can-withhold-updates)). Whether an update
installs without the user is the user's own setting (unattended-upgrades takes a third-party
repository only when the user adds it), never the package's.

**Tests:** [`hack/linux/apt-test.sh`](../../hack/linux/apt-test.sh) (CI's job "Linux Package",
every push): `verify` refuses a tampered package and another key, `mirror` a tampered repository
(leaving nothing), `sign` a key that is not the packages', `newer` an older repository. No test
runs polkit's password dialog in a desktop session.

## Windows

Cogpit for Windows is an NSIS setup ([`cogpit.nsi`](../../packaging/windows/cogpit.nsi)), and
WinSparkle, Sparkle's Windows port, updates it from a second Sparkle feed signed with the same
update key in the same environment `sparkle`
([D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle),
[releasing › Windows](../developer/releasing.md#windows)).

```text
release pipeline ─► Cogpit-<v>-windows-x64-setup.exe (cogpit.exe carrying the feed URL and the update key's
                    public half, cogpit-cli.exe, WinSparkle.dll pinned by SHA-256), attached, in SHA256SUMS
   "Windows appcast (draft)" ─► builds only on a served feed the key verifies
   "Windows appcast (sign)", environment sparkle ─► sign_update over the setup and the feed
   "Windows appcast (deploy to Pages)" ─► both checked with the public key, refused if Pages serves a newer release
        ─► https://guided-traffic.github.io/cogpit/appcast-windows.xml
                                   │
Cogpit (winsparkle_windows.go), the setup's cogpit.exe in a release build
       ─► reads the feed at every start and every hour, verifies it with the key ─► the Update button
       ─► the click installs at once (Help › Check for Updates… asks first, in WinSparkle's window):
          WinSparkle reads the feed again, unverified ─► downloads the setup ─► its EdDSA signature
       ─► runs it as "/S /relaunch" ─► asks Cogpit to quit ─► the setup waits, installs, starts the new release
```

| Check | Where | Prevents |
|---|---|---|
| The setup's EdDSA signature with the key built into the app (`win_sparkle_set_eddsa_public_key`) | WinSparkle, before it runs the setup | a setup swapped on GitHub, on Pages or in transit running as the user (or as an administrator for an installation for all users) |
| The feed's signature with the same key ([`appcast.go`](../../internal/update/appcast.go)) | Cogpit, before the Update button shows a release | the button offering a release the key did not sign; WinSparkle checks only the setup's signature ([H-new](#h-new-winsparkle-installs-from-a-feed-it-does-not-verify)) |
| The feed and the setup checked with the public key, refused when Pages serves a newer release (`hack/appcast verify -live`) | "Windows appcast (deploy to Pages)" | a feed every copy would refuse; an older release's run taking a newer one back |
| `WinSparkle.dll` loaded by its full path, only in a release build that runs from the folder an uninstall entry names | Cogpit (`load` in [`winsparkle_windows.go`](../../internal/update/winsparkle_windows.go)) | a `WinSparkle.dll` found elsewhere through the DLL search order; a copy outside the setup's folder installing a second Cogpit |
| `WinSparkle.dll` pinned by the SHA-256 of WinSparkle's release archive (`WINSPARKLE_SHA256`) | `make winsparkle` | another DLL than the reviewed release in a setup ([H-140](#h-140-winsparkledll-is-a-third-party-binary-without-a-signature)) |
| Only the window runs `app.installUpdate` ([agents and MCP](agents-and-mcp.md)) | the engine | an agent or the CLI installing a release and restarting Cogpit |

**What does not vouch for an update:** Authenticode
([H-139](#h-139-the-windows-setup-is-not-signed-with-authenticode)); the feed WinSparkle acts on
([H-new](#h-new-winsparkle-installs-from-a-feed-it-does-not-verify)); the host, which can withhold
the feed ([H-93](#h-93-the-feeds-host-can-withhold-updates)). A per-user installation updates
without an administrator; one for all users asks UAC at each update, since its setup runs again
elevated.

**Tests:** `TestNewestReleaseRefusesAFeedTheKeyDoesNotVerify` (the Update button's feed check),
`TestWindowsUpdaterLoadsWinSparkle` (the pinned DLL has every function Cogpit calls; CI's "Unit
Tests (windows)"), and CI's job "Windows Package", which installs the setup, installs it again over
itself and uninstalls it. No test runs a WinSparkle update end to end.

## What this does not cover

### H-97: The feed URL follows the repository's name

Live. Every copy keeps the URLs it was built with, all under
`https://guided-traffic.github.io/cogpit/`: `SPARKLE_FEED_URL` and `WINDOWS_FEED_URL` in the
[`Makefile`](../../Makefile), the APT repository in `cogpit.sources`. Pages serves a project site
under the repository's name, case-sensitively, so renaming or transferring the repository moves all
three: macOS and Windows copies stop finding updates without an error the user sees, apt reports a
missing repository, and only a manual download brings a copy back. A custom domain for Pages (none
is set) or a redirect page at the old path would keep the URLs.

### H-61: `SHA256SUMS` is not signed

Live. Whoever can change a release's assets (the GitHub App's key, a maintainer's account, a
release job's `contents: write` token) can replace a download and its checksum line together where
nothing else vouches for it: the Linux packages and the Windows setup at a first install
([H-136](#h-136-the-first-linux-install-has-no-signature-of-its-own),
[H-139](#h-139-the-windows-setup-is-not-signed-with-authenticode)), an unsigned build's app zip,
and the unsigned `cogpit-cli` archives and app zips the earliest releases carry. A swapped
macOS disk image would need the Developer ID key as well. Not built: signed checksums.

### H-62: Every push to `main` that passes the gates is released

Live. `main` has no branch protection and no required review, agents and maintainers push to it
directly, and semantic-release releases every `feat:`, `fix:` or `perf:` commit once CI is green:
the gates are the only check before a signed release that Sparkle and WinSparkle offer and apt
installs. Renovate's automerged updates ship with the next release
([H-18](#h-18-dependencies)). What bounds the tags: the ruleset "Release tags v*" (id 25618416)
lets only the semantic-release GitHub App (`guided-traffic-automation`, app id 5070048) create,
move or delete `v*` tags, and `release` has the administrator bypass off; an emergency tag means an
administrator switches the ruleset off, which the organisation's audit log records. What remains:

- **A commit on `main`:** released, tagged through the App, built, signed and notarized.
- **The App's private key:** whoever holds it, or a token made from it (Semantic Release's, or
  Renovate's with all of the App's permissions), can tag any commit `v*`.
- **An administrator's account:** can switch the ruleset off and then create such a tag.

### H-15: Update key compromise and rotation

Live. Whoever holds the update key and can serve the feeds can offer every copy an update it
accepts: on Windows the key is the only check, and on macOS a release may change either the Apple
signing certificate or the EdDSA key, so the key alone lets an update carry another team's
Developer ID ([Sparkle: rotating signing keys](https://sparkle-project.org/documentation/#rotating-signing-keys);
not tried). Replacing a leaked key: a release whose app carries the new public key, offered in a
feed and an update still signed with the old one; on macOS, with `SUVerifyUpdateBeforeExtraction`,
a change of the EdDSA key needs a Developer ID-signed disk image, which Cogpit's is. A lost key: no
feed can be signed again, and after 20 days Sparkle shows an unsigned feed's items
([H-91](#h-91-after-20-days-of-unverifiable-feeds-sparkle-presents-items-again)). No second,
offline rotation key exists.

### H-16: Availability of the release host

Updates depend on GitHub Pages (both feeds, the APT repository with its packages) and GitHub's
release storage (the disk image, the setup). If either is unreachable, Cogpit keeps running and
offers no update: a scheduled check fails silently, Help › Check for Updates… shows Sparkle's or
WinSparkle's error (on Linux a notification with apt's last line). There is no mirror.

### H-92: An environment `sparkle` without rules hands the update key to any run

Dormant: `sparkle` admits tags `v*` only, administrator bypass off. Live if the rules are removed or
loosened while the secret is there: GitHub creates an environment a workflow names without any
rule, and then any run naming `environment: sparkle`, on any branch, gets the key. Cogpit's sign
jobs refuse unless they read exactly the tag rule `v*`; a workflow written to take the key does not
ask. Live with the rules: a run on a `v*` tag does with the key what the workflow at that tag says
([H-141](#h-141-the-update-key-signs-without-an-approval)), and the feed signs the release's text
as the draft job read it, so whoever edits that text first has their text and links in Sparkle's
window.

### H-93: The feed's host can withhold updates

Live. The signed feeds have no expiry, and every feed ever deployed is public. The host can serve
nothing ([H-16](#h-16-availability-of-the-release-host)) or an older signed feed (a freeze): copies
older than its newest item are offered at most that item, newer copies nothing; a Mac is not
downgraded, since Sparkle offers only a newer `CFBundleVersion` (Windows:
[H-new](#h-new-winsparkle-installs-from-a-feed-it-does-not-verify)). The deploy jobs' `hack/appcast
verify -live` keeps the pipeline from freezing a feed; a workflow that skips it can. Dormant while
every release has the same `LSMinimumSystemVersion` (`MACOS_MIN`): if one raised it, a left-out
older item would be the newest a Mac on the older macOS may install. Who can change what Pages
serves: a run with `pages: write` that the environment `github-pages` admits (`main` and tags `v*`,
administrator bypass on), so whoever may push a workflow to `main`
([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)); "Appcast (deploy to Pages)"
runs on the organisation's self-hosted runners. Not built: an expiry or freshness check.

### H-91: After 20 days of unverifiable feeds, Sparkle presents items again

Accepted. Live by design: Sparkle's `SUSignedFeedFailureExpirationInterval` (default 20 days,
which Cogpit keeps) lets a copy that has not validated a feed for that long present an unverified
feed's items, without notes and links, as a failsafe for a lost key
([Sparkle's security settings](https://sparkle-project.org/documentation/customization/#security-settings)).
Such an item still needs a disk image the update key signed or, for a change of the key, a
Developer ID-signed one. An attacker who can serve the feed
([H-93](#h-93-the-feeds-host-can-withhold-updates)) and get a disk image signed with this team's
Developer ID (a run on a `v*` tag in `release`, which has no reviewer,
[H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)) could, after 20 days of failing
feeds, have copies offer an update that changes the update key; the user still clicks Install.
Setting the interval to 0 would close this and turn a lost key into one manual download per copy.

### H-17: Installs owned by a package manager

Where a package manager installs Cogpit, an update's integrity is the manager's. Built for Linux:
the .deb is apt's, the Update button hands the install to apt through the package's helper, and
Cogpit never writes a file the package owns ([Linux](#linux)). Dormant for the rest: no Homebrew
cask (decided in [D-4](../developer/decisions.md#d-4-packaging-and-updates), not built), winget or
Flathub package exists.

### H-135: The APT key signs without an approval

Accepted, so that a release reaches Linux users without waiting for anyone. `apt` has no required
reviewer: whoever can create a `v*` tag, or change the workflow a tag runs, can publish a package
that every Linux copy installs as root with its next upgrade, silently where the user allowed
unattended-upgrades for it. Bounded as the release is
([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)). A leaked key needs a new key
and every user's manual install of a new package, since apt trusts only the key in the sources
file; the revocation certificate withdraws the old key only for whoever imports it, which apt does
not.

### H-141: The update key signs without an approval

Accepted: "Appcast (sign)" and "Windows appcast (sign)" run without a manual click, as the APT key
does ([H-135](#h-135-the-apt-key-signs-without-an-approval)). Whoever can create a `v*` tag, or
change the workflow a tag runs, can have a disk image and a setup signed and put in the feeds that
every copy offers. A required reviewer would see the run and its tag, not what changed. Bounded as
the release is ([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)); the sign jobs
refuse an environment without its tag rule
([H-92](#h-92-an-environment-sparkle-without-rules-hands-the-update-key-to-any-run)).

### H-136: The first Linux install has no signature of its own

Live. The .deb from the release carries no signature (Debian's convention: apt trusts the
repository's index), and `SHA256SUMS` is unsigned ([H-61](#h-61-sha256sums-is-not-signed)), so a
first install trusts GitHub's HTTPS and the release page, and the package brings the key every
later update is checked against. A user can compare the key in
`/etc/apt/sources.list.d/cogpit.sources` with the fingerprint the release note and this page name,
`BE09 5407 4B36 CBDE 8875  7E52 A9D2 608B 0DC7 6494`, before the first `apt update`. A signed
`SHA256SUMS` would cover the download itself.

### H-137: The APT repository's host can withhold updates

Live, as [H-93](#h-93-the-feeds-host-can-withhold-updates). The repository's `Release` carries no
`Valid-Until`, so apt accepts an old signed index indefinitely: Pages can keep a copy on an older
release, not make it install a package the key did not sign. A `Valid-Until` would turn every pause
between releases longer than its period into apt errors on every machine, so none is set. A user
who changed `cogpit.sources` keeps its old key through a package update that renews it (a
conffile) and sees apt's error once that key expires (2031-10-19).

### H-138: polkit keeps the password for the update helper a few minutes

Accepted. The helper's polkit action is `auth_admin_keep`
([policy](../../packaging/linux/io.guided-traffic.cogpit.update.policy)): after the password, any
process of the same user may run the helper again without it for polkit's keep time (five minutes
by default). The helper takes no argument and only upgrades the package `cogpit` from the signed
repository, so the most such a process can do is start that upgrade. `auth_admin` would ask at
every click, also for Help › Check for Updates… right after the Update button.

### H-139: The Windows setup is not signed with Authenticode

Accepted ([D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)).
Neither the setup nor `cogpit.exe`, `cogpit-cli.exe` or the uninstaller carries an Authenticode
signature: SmartScreen shows "Windows protected your PC" at the downloaded setup's first start
(More info, then Run anyway), UAC names no publisher for `/allusers`, and application control that
admits signed publishers only (WDAC, AppLocker) refuses Cogpit. A first install trusts GitHub's
HTTPS and the release page ([H-61](#h-61-sha256sums-is-not-signed)); updates carry the update
key's signature. What closes it: signing material (Azure Artifact Signing takes EU organisations,
not EU individuals; a cloud-HSM certificate such as Certum's or SSL.com's serves an individual) and
a signing job in an environment of its own, as Apple's
([releasing › secrets](../developer/releasing.md#secrets)).

### H-140: WinSparkle.dll is a third-party binary without a signature

Accepted. The setup ships the x64 DLL of the WinSparkle release the [`Makefile`](../../Makefile)
pins (`WINSPARKLE_TAG`) as its archive carries it: built by WinSparkle's maintainer, not by
Cogpit's pipeline, without an Authenticode signature. It runs inside `cogpit.exe` with the user's
rights and decides which setup runs as an update. The archive's SHA-256 is pinned
(`WINSPARKLE_SHA256`), and a new version comes only through a reviewed change of the pin (Renovate
proposes it and never merges it); not bounded: that the maintainer's build is what its source says.
Not built: WinSparkle built from source in the pipeline.

### H-new: WinSparkle installs from a feed it does not verify

Live. WinSparkle checks the EdDSA signature of the setup it downloads, not the feed's: the version
it compares with the running one, the download's URL and signature, the installer arguments and
the notes link all come from `appcast-windows.xml` as served. Cogpit verifies the feed only for the
Update button, which keeps its release when a later read fails to verify; the button's click
(`win_sparkle_check_update_with_ui_and_install`, which installs without asking) and Help › Check
for Updates… have WinSparkle read the feed again. Whoever can change what Pages serves
([H-93](#h-93-the-feeds-host-can-withhold-updates)) can list an older setup the key signed, with
its genuine signature, under a newer version: WinSparkle runs it, the setup compares no versions,
and the copy goes back to a release with flaws fixed since. A downgrade check in the setup would not
close it, since every older signed setup lacks it; an install that acts only on the item Cogpit
verified (Cogpit downloading and checking the setup itself) would.

### H-18: Dependencies

The Go modules, the GitHub Actions and the pinned tools are a supply chain of their own. Renovate
([`renovate.yml`](../../.github/workflows/renovate.yml),
[`.github/renovate.json`](../../.github/renovate.json)) merges the minor, patch and digest updates
of Go modules, the Go version, Actions (their majors too) and the Makefile's tool pins on its own
once CI passes, Sparkle's, WinSparkle's and dmgbuild's never
([releasing › Renovate](../developer/releasing.md#renovate)); govulncheck and GoSec run on every
push to `main` and every pull request ("Vulnerability Check", "GoSec Security Scan", and GoSec for
the darwin-only files in "macOS Lint and App", in [`release.yml`](../../.github/workflows/release.yml)).
What remains, per case:

- **An automerged update is reviewed by nobody:** it ships signed in the next release
  ([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)), an Action's in the next run.
- **Actions are named by a version tag, not a commit** (`actions/checkout@v7`,
  `actions/download-artifact@v8`, `actions/upload-artifact@v7`), also in the jobs of
  [`build.yml`](../../.github/workflows/build.yml) that hold the Developer ID material, the update
  key and the APT key: whoever can move such a tag runs code in those jobs.
- **The disk image's window is made by code from PyPI**
  ([D-70](../developer/decisions.md#d-70-the-disk-images-window)): dmgbuild and its two
  dependencies, pinned by version and hash
  ([`dmgbuild-requirements.txt`](../../hack/macos/dmgbuild-requirements.txt)), run in the app's
  job, which holds no secret; the signing job checks only the template's file list
  ([releasing › the disk image's window](../developer/releasing.md#the-disk-images-window)). Not
  covered: the bytes of its `.DS_Store`, `.background.tiff` and `.VolumeIcon.icns`, which Finder
  reads on every Mac that opens the image; a crafted one would ship signed and notarized.
- **Mesa's DLLs run in CI's job Windows Package,** beside the installed `cogpit.exe` for the smoke
  test ([releasing › the smoke test](../developer/releasing.md#the-smoke-test)): a third-party
  build, pinned by SHA-256 and merged by Renovate on its own once CI passes, in a job that holds no
  secret and whose build cache no release reads (the release pipeline builds without one). The
  release pipeline's Windows job, whose setup ships, does not load them.
- **What Renovate cannot see,** each moved by hand: sigstore's trusted root (`make sigstore-root`,
  [H-35](trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)),
  the terminal widget's copy in `third_party/`
  ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)), and the envtest binaries of
  Kubernetes 1.37.0, pinned by SHA-512 in the `Makefile`
  ([D-64](../developer/decisions.md#d-64-envtest-a-real-api-server-in-ci-without-docker); CI only,
  never shipped); and the Kubernetes release table
  ([H-130](#h-130-the-releases-kubernetes-release-table-is-fetched-unreviewed)).
- **A release names no inputs:** no SBOM, no provenance attestation of the build.

### H-130: The release's Kubernetes release table is fetched unreviewed

Accepted. Both binaries embed upstream's end-of-life dates, by which the window marks a cluster's
and its kubelets' versions
([D-91](../developer/decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)); the app
makes no connection for them. Each platform's build job of a release fetches
`kubernetes/website`'s `data/releases/schedule.yaml` and `eol.yaml` from `raw.githubusercontent.com`
over HTTPS, unpinned (no hash can name next month's file, upstream signs neither), and builds them
in without a person reading the diff. Whoever can change those files on `kubernetes/website`'s
`main`, or what GitHub's raw host serves the job, can shift dates, nothing else: the generator
([`hack/k8seol`](../../hack/k8seol/main.go)) keeps the committed table unless the new one is
plausible against it (no minor lost, no end of life moved by more than 120 days;
[releasing › the Kubernetes release table](../developer/releasing.md#the-kubernetes-release-table)).
Within those bounds a false date ships: a ⚠ too early, or a minor past its end of life shown as
supported. The step's log lists every changed minor, and the binaries record `vcs.modified=true`
when the table differs from the committed one. Not built: a second source to compare with (D-91
rejects endoflife.date, a third-party mirror), a person's approval of the diff, or the diff in the
release notes.
