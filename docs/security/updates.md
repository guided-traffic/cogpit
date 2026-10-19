# Updates

The update channel: how Cogpit learns about a new version and replaces itself, one of the two
places where Cogpit downloads code that then runs. Since 2026-10-14 the macOS app carries Sparkle 2
and the release pipeline signs and publishes its appcast
([D-4](../developer/decisions.md#d-4-packaging-and-updates),
[releasing › the appcast](../developer/releasing.md#the-appcast)); the owner set up the update
key on 2026-10-15 and every release since v0.11.0 is built with it; v0.14.0 to v0.16.0 passed
notarization on 2026-10-16, and since ~06:20 UTC that day Pages serves the signed feed with 0.16.0
and 0.15.0 (the owner approved the appcasts; checked: the feed's signature and the 0.16.0 disk
image's EdDSA signature against the public key); v0.17.0 to v0.19.0 followed the same day with their disk images, each appcast approved in release order, so by the evening the feed holds 0.19.0, 0.18.0, 0.17.0, 0.16.0 and 0.15.0; v0.20.0 (2026-10-16 19:21 UTC) and v0.21.0 (23:39 UTC) followed in the night and v0.22.0 (08:14 UTC) and v0.22.1 (09:32 UTC) on 2026-10-17, their appcasts waiting for the owner's approval. The first installed copy updated on 2026-10-16:
the owner's, from 0.15.0 to 0.16.0, through the signed feed and Sparkle's install (a person's
observation; [setting up Sparkle](../developer/releasing.md#setting-up-sparkle)). `cogpit-cli` has no
updater of its own: since 2026-10-14 it is part of the app bundle
([D-16](../developer/decisions.md#d-16-the-first-real-release)), so Sparkle's update replaces it
with the app under the same checks; Windows and Linux have no updater. The other place, built 2026-10-13, is the
`kubectl` a terminal offers for its cluster: downloaded only on the user's click and verified against
the Kubernetes release's own sigstore signature and, since 2026-10-15, that signature's entry in
sigstore's transparency log, not Cogpit's update key
([trust boundaries](trust-boundaries.md#boundaries-and-required-checks),
[H-35](trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)).

The owner's decision round of 2026-10-14 evening confirms the failed-signing behaviour and the
20-day failsafe ([H-91](#h-91-after-20-days-of-unverifiable-feeds-sparkle-presents-items-again))
and decides two GitHub settings for the release environment ([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released);
decided further on 2026-10-15), which are made only with the owner's explicit OK and are not made
yet (checked 2026-10-15). The rest of the Sparkle set-up was not put to the
owner and stands as built: the signed feed, the preference driving Sparkle, checks from any origin,
Beta refused until prereleases exist, Sparkle's XPC services removed, no entitlements on Sparkle's
parts, the three appcast jobs, the history from the signed feed.

## Chain of trust

```text
release pipeline ─► Cogpit-<v>-macos-arm64.dmg (the app, its cogpit-cli inside), Developer ID-signed, notarized, stapled
   "Appcast (draft)", no secret ─► the feed from Pages (only if the update key signed it) + this release's item
   "Appcast (sign)", environment sparkle, after the owner's approval
        ─► EdDSA signature over the disk image, then over the feed (Sparkle's sign_update)
   "Appcast (deploy)" ─► checked with the public key ─► https://guided-traffic.github.io/cogpit/appcast.xml
                                   │
Cogpit.app (Sparkle 2) ─► fetch the feed over HTTPS at every start and every hour, or on Help › Check for Updates…
       ─► the feed's EdDSA signature with SUPublicEDKey from the app's Info.plist
       ─► an item newer than the running CFBundleVersion, for this macOS? ─► Sparkle's window, the user's click
       ─► download the disk image from the release ─► its EdDSA signature, before anything is extracted
       ─► Apple's code signature of the new app (the running app's Developer ID team)
       ─► replace the app and relaunch
```

The chain is Sparkle 2's ([D-4](../developer/decisions.md#d-4-packaging-and-updates); its
[documentation](https://sparkle-project.org/documentation/#segue-for-security-concerns)), set up
in the app's `Info.plist` by [`hack/macos/sparkle.sh`](../../hack/macos/sparkle.sh). The app's side
is [`internal/update`](../../internal/update/); the pipeline's is
[`build.yml`](../../.github/workflows/build.yml), [`hack/macos/appcast.sh`](../../hack/macos/appcast.sh)
and [`hack/appcast`](../../hack/appcast/main.go).

**How often a copy asks** (owner, 2026-10-16): every hour, `SUScheduledCheckInterval` 3600 in the
`Info.plist` (Sparkle's default is a day; an hour is the least Sparkle 2.10.0 uses), and at every
start (the owner, 2026-10-16 night; before, at a launch only when the last check was older than the
hour): the app runs the scheduled check once right after Sparkle started
(`TestSparkleChecksAtEveryStart`: one request per start, whenever the last check was). Each check is
one HTTPS request for the feed to GitHub Pages, which sees the copy's address, the time and the
User-Agent Sparkle sends, the app's name and version beside Sparkle's (`Cogpit/0.16.0
Sparkle/2.10.0`), now up to 24 times a day plus one per start instead of once a day. No system profile goes with it: `SUSendProfileInfo` is never set, and Sparkle's
permission prompt, which would offer it, does not show because the `Info.plist` sets
`SUEnableAutomaticChecks`. A check involves no kubeconfig, cluster or credential, and one that
finds a release downloads nothing until the user chooses to install. Preferences › App › Check
for updates automatically off stops the scheduled checks and the start check; Help › Check for
Updates… still asks.

| Check | Where | Prevents |
|---|---|---|
| The feed's EdDSA signature with the key in the app (`SURequireSignedFeed`, Sparkle 2.9) | every copy, on every check | a feed changed on or by the host: forged release notes or links, a critical or informational mark, an item pointing elsewhere ([H-91](#h-91-after-20-days-of-unverifiable-feeds-sparkle-presents-items-again) for when it fails for long) |
| The disk image's EdDSA signature, before extraction (`SUVerifyUpdateBeforeExtraction`) | every copy, per update | a swapped download; nothing of an unsigned archive is unpacked |
| Apple's code signature of the new app against the running app's | every copy, per update | an app that is not Cogpit's team's |
| Only a newer `CFBundleVersion` is offered; `sparkle:minimumSystemVersion` | every copy | a downgrade to a version with known flaws; an update the Mac cannot run |
| The update key behind the owner's approval, the environment's rules checked before signing | "Appcast (sign)" | a run the owner did not approve signing a feed ([H-92](#h-92-an-environment-sparkle-without-rules-hands-the-update-key-to-any-run)) |
| The history taken from Pages only when the update key signed it | "Appcast (draft)" | a feed altered on Pages being signed again with the next release |
| The signed feed and the disk image checked with the public key before Pages gets them | "Appcast (deploy)" | a feed every copy would refuse (a secret that does not match the app's key) |
| The new feed keeps every item the feed on Pages serves, unless its channel's newest were kept (the owner's decision, 2026-10-16) | "Appcast (deploy)" | an older release's appcast, approved after a newer one's, putting the feed back to the older version (2026-10-16): a freeze made by the pipeline itself ([H-93](#h-93-the-feeds-host-can-withhold-updates)) |
| HTTPS | the feed and the download | eavesdroppers learning which version a user runs (not integrity: the signatures provide that) |
| Package-manager installs hand over to the manager | not built: no package manager installs Cogpit ([H-17](#h-17-installs-owned-by-a-package-manager)) | Cogpit overwriting files a package manager owns |

## What a release holds today

Since 2026-10-13 ([D-16](../developer/decisions.md#d-16-the-first-real-release)) semantic-release
creates a tag and a GitHub release once every gate on `main` is green, and the release pipeline
([`build.yml`](../../.github/workflows/build.yml)) builds the artifacts from that tag and attaches
them ([releasing › the release pipeline](../developer/releasing.md#the-release-pipeline)). Since
2026-10-14 (the owner, D-16 revised) a release carries only the complete product: the disk image
with `cogpit-cli` inside the app, and `SHA256SUMS`. The `cogpit-cli` archives for six platforms
that releases up to v0.6.0 carry (unsigned for Linux and Windows) are not built any more:

| Artifact | Built by | What vouches for it today |
|---|---|---|
| `Cogpit-<v>-macos-arm64.dmg`, the app bundle in a disk image (from the first release after v0.4.0 on: v0.6.0, v0.7.0 and v0.8.0 carry it, v0.5.0 and v0.9.0 to v0.12.0 have no files; checked 2026-10-15) | `make dist-app` on GitHub's `macos-latest`, signed and imaged by `make dist-macos-sign` ([`hack/macos/release.sh`](../../hack/macos/release.sh)) in the job "Sign and submit (macOS)", notarized and stapled by `make dist-macos-notarize` in "Notarize and staple (macOS)" | the app and the disk image signed with the Developer ID "Developer ID Application: Hans Fischer (6L57P2HXN6)", the hardened runtime and Apple's timestamp; the disk image notarized by Apple, its ticket stapled, so Gatekeeper accepts it offline as `Notarized Developer ID`; its SHA-256 in `SHA256SUMS`. When a signature, the notarization or a check fails, the job fails and the release gets no files until the failed jobs are re-run, never unsigned files under a signed note; the notarization job waits 330 minutes at most for Apple's verdict on the one submission the signing job made, and a re-run waits for the same one; it staples only the disk image whose SHA-256 the signing job recorded with the submission ([`build.yml`](../../.github/workflows/build.yml), [`release.sh`](../../hack/macos/release.sh); the lead's default, confirmed by the owner on 2026-10-14 evening) |
| `cogpit-cli` in the app, `Cogpit.app/Contents/MacOS/cogpit-cli` (from the first release after v0.6.0 on) | `make app` from `make build`'s binary, in the same job | the same as the app: signed with the same Developer ID before the bundle (identifier `io.guided-traffic.cogpit.cli`, the hardened runtime, the timestamp), sealed into the app's signature, so changing it breaks the app's; notarized with the disk image, whose stapled ticket covers it; replaced by Sparkle's update with the bundle. Not verified (the signed releases v0.7.0 and v0.8.0 carry it): that it runs from a terminal in a quarantined copy, before the app was ever opened, without a prompt |
| `Cogpit_<v>_darwin_arm64.zip`, the app bundle (v0.4.0 and earlier, and any build without the signing secrets) | `make dist-app` on GitHub's `macos-latest` | its SHA-256 in `SHA256SUMS`; an ad-hoc signature (`codesign --sign -`), which seals the bundle but names nobody: **no Developer ID, not notarized**, so Gatekeeper blocks the first start until the user allows it |
| `SHA256SUMS` | the release pipeline's last job | nothing: it is not signed, so it shows that a download matches what CI attached to the release, not who attached it |
| The release's item in `https://guided-traffic.github.io/cogpit/appcast.xml` (from the first release built with the update key that passes notarization; none yet on 2026-10-15: v0.11.0 to v0.13.0 are built with the key, v0.11.0 and v0.12.0 have no files, v0.13.0's notarization was in progress) | the three appcast jobs after "Attach to the release" ([releasing › the appcast](../developer/releasing.md#the-appcast)) | the update key's EdDSA signature over the feed and over the disk image it names, made after the owner approved the run; checked with the public key before Pages gets it |

**What the Developer ID signature and the notarization vouch for:** that the code was signed with
the key of team 6L57P2HXN6's certificate and has not changed since, and that Apple's automated
check found no known malware in it when it was submitted. They do not say which commit it was
built from, and they reach as far as the key: whoever runs a workflow on a `v*` tag in this
repository can have that key sign what the workflow built
([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)). The release page says what was
signed, naming the certificate the signature carries, or that the build was unsigned.

The owner set up the update key on 2026-10-15 (checked with `gh` that day, ~19:00 UTC: the secret
`SPARKLE_ED_PRIVATE_KEY` in the environment `sparkle` since 07:00 UTC, the repository variable
`SPARKLE_PUBLIC_ED_KEY` set), and v0.11.0, v0.12.0 and v0.13.0 are built with it (the app job's
log of v0.11.0 and v0.12.0: "Sparkle bundled with the update key"). No appcast was signed before 2026-10-16, when
`https://guided-traffic.github.io/cogpit/appcast.xml` first served one: v0.9.0 to v0.12.0
got no files (Apple kept each notarization "In Progress" beyond the old job's 45 minutes);
v0.13.0's first wait gave up after 330 minutes (2026-10-15 23:43 UTC); a re-run of its failed jobs found the same submission accepted and attached its disk image at 23:52 UTC;
v0.14.0 to v0.16.0 passed, and the owner approved the appcasts of v0.15.0 and v0.16.0 (the feed
holds both since ~06:20 UTC). The first update went through the same morning: the owner's MacBook
(0.15.0 installed from its disk image) updated to 0.16.0; after `defaults delete
io.guided-traffic.cogpit SULastCheckTime` and a relaunch, the launch check found 0.16.0 at once and
showed the Update button (Sparkle's gentle reminder), and its click handed the update to Sparkle,
which installed it and started Cogpit again (a person's observation, not a test). Sparkle requires
the feed's signature and the disk image's (`SURequireSignedFeed`, `SUVerifyUpdateBeforeExtraction`),
so the update passed both checks with the key built into the app. A build without the key (a
local `make app`; the releases with files, v0.8.0 and earlier) says "this build has no update key"
under Preferences › App › Check for updates automatically and greys Help › Check for Updates….
Copies installed before the first release built with the key never update themselves (no updater,
or none with a key): one manual download moves them onto the chain above.

**What vouches for an update once a signed feed is served:** the update key's EdDSA signatures over the
feed and over the disk image, computed in a job the owner approved, checked by Sparkle in every
copy; Apple's Developer ID signature and notarization of the app inside, checked by Sparkle against
the running app's and by Gatekeeper. **What does not:** the host. GitHub Pages serves the feed and
GitHub's release storage the disk image; either can withhold an update or serve an older signed
feed ([H-93](#h-93-the-feeds-host-can-withhold-updates)), and neither can make a copy install
anything the key did not sign.

### Keys and who holds them

| Key | Where it lives | What it can do | If it leaks |
|---|---|---|---|
| The organisation's GitHub App private key (`APP_CLIENT_ID`, `APP_PRIVATE_KEY`) | organisation secrets of `guided-traffic`, visible to this repository; used by the Semantic Release job (token with `contents: write`) and by Renovate | push to `main`, create tags and releases; a release it creates starts the release pipeline, which builds whatever the tag points at | rotate the App's private key in the App's settings and replace the organisation secret; delete the releases and tags it made. Secrets are not handed to workflows of pull requests from forks |
| The release pipeline's `GITHUB_TOKEN` | per job; `contents: write` only in its last job, to attach the artifacts | upload assets to and edit the text of this repository's releases | expires with the job |
| Developer ID Application certificate, App Store Connect team API key (role Developer) | secrets of the GitHub environment `release` (created 2026-10-13; the material in it since 2026-10-14), which only tag workflows may use (deployment rule: tags `v*`), so that no pull request workflow can read them (decided by the owner on 2026-10-14 evening and further on 2026-10-15, GitHub settings not made yet: only the semantic-release GitHub App may create `v*` tags, as the tag ruleset's only bypass, and the environment's administrator bypass off; [H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)). Two jobs read them (since 2026-10-15; before, one job "Sign and notarize (macOS)"): "Sign and submit (macOS)" with the certificate and the API key, "Notarize and staple (macOS)" with the API key only. Neither builds anything or runs Go; the first imports the certificate into a temporary keychain (key not extractable, usable by `codesign` only), both write the API key into an owner-only file, and each deletes them when it ends ([releasing › signing and notarization](../developer/releasing.md#signing-and-notarization)) | sign and notarize code as team 6L57P2HXN6: whatever is signed and notarized with the two passes Gatekeeper on every Mac until Apple revokes the certificate | revoke the certificate at Apple, and the key in App Store Connect; releases notarized before stay valid (the owner's note, 2026-10-13; not checked against Apple's documentation) |
| The update key (Sparkle's EdDSA key; made by the owner on 2026-10-15) | decided (owner, 2026-10-13) and set up 2026-10-15 (the secret since 07:00 UTC; the environment with the owner as required reviewer, the tag rule `v*` only and no administrator bypass; checked with `gh` that day): `SPARKLE_ED_PRIVATE_KEY`, a secret of the environment `sparkle` with the owner as required reviewer and tags `v*` only, so CI signs a release's feed only after the owner approved that run; made by the owner with Sparkle's `generate_keys`, which keeps it in his login keychain ([setting up Sparkle](../developer/releasing.md#setting-up-sparkle)). One job reads it, "Appcast (sign)": it refuses while the environment lacks its rules, hands the key to Sparkle's `sign_update` on its standard input only and runs no Go and no dependency's code. Its public half is the repository variable `SPARKLE_PUBLIC_ED_KEY`, built into every copy as `SUPublicEDKey` | sign appcasts and disk images that every copy with this key accepts | [H-15](#h-15-update-key-compromise-and-rotation) |

## What this does not cover

### H-97: The feed URL follows the repository's name

Every installed copy keeps the feed URL its `Info.plist` was built with (`SUFeedURL`,
`SPARKLE_FEED_URL` in the [`Makefile`](../../Makefile)): `https://guided-traffic.github.io/cogpit/appcast.xml`.
GitHub Pages serves a project site under the repository's name, case-sensitively (checked
2026-10-15: another spelling of the name answers 404); the repository is `guided-traffic/cogpit`
(renamed back from `Cogpit` on 2026-10-15, before any build with the update key). Renaming the repository, or moving it to
another owner, moves the feed: the copies installed before stop finding updates, without an
error the user sees, and only a manual download brings them back. Dormant until a release built
with the update key has files to install (the environment `sparkle`, Pages and the key set up
2026-10-15; every release since v0.11.0 is built with the key: v0.11.0 and v0.12.0 have no files,
v0.13.0 has its disk image since 2026-10-15 23:52 UTC, v0.14.0 to v0.20.0 theirs since 2026-10-16, the signed feed serves 0.19.0 to 0.15.0 until the owner approves v0.20.0's and v0.21.0's appcasts).
What bounds it: nothing in the code; the rename is a GitHub setting. A custom domain for Pages, or
a redirect page at the old path, would keep the URL stable; neither is set up.

### H-61: Releases are unsigned

Live since the first release (2026-10-13); narrowed twice on 2026-10-14. v0.4.0 and earlier carry
no code signature that names Cogpit, and the `cogpit-cli` archives for Linux and Windows of every
release up to v0.6.0 none at all. Since 2026-10-14 the release pipeline signs the macOS app with
its `cogpit-cli` and the disk image with the Developer ID and has them notarized
([releasing › signing and notarization](../developer/releasing.md#signing-and-notarization)),
and a release carries nothing else but `SHA256SUMS`
([D-16](../developer/decisions.md#d-16-the-first-real-release)); v0.6.0, v0.7.0 and v0.8.0 carry
that signed, notarized disk image (v0.5.0 and v0.9.0 to v0.12.0 no files; checked 2026-10-15), so
the unsigned case holds for v0.4.0 and earlier and for the archives up to v0.6.0. What stays live:
`SHA256SUMS` is not signed, and the unsigned archives of the releases up to v0.6.0 stay on their
release pages, so whoever can change a release's assets (the GitHub App's key, a maintainer's
account) can replace one of those archives and its checksum together, and a user has nothing to
check them against. A swapped macOS download would need the Developer ID key as well. Signing the
checksums is not designed; Linux and Windows downloads come back with their desktop app (M11),
whose signing is planned there.

### H-62: Every push to `main` that passes the gates is released

Live since 2026-10-13. `main` has no branch protection and no required review (checked
2026-10-13), agents and maintainers push to it directly, and semantic-release releases every
`fix:` and `feat:` commit once CI is green, so the checks are the only gate between a commit and a
published release. Since 2026-10-14 that release's macOS downloads are signed with the owner's
Developer ID and notarized, so a commit that passes the gates ships as code Gatekeeper accepts
without a warning. The environment `release` admits any tag `v*`: whoever may create a release in
this repository can also have an arbitrary commit, tagged `v<anything>`, built, signed and
notarized; the environment has no required reviewer. Renovate's automerged dependency updates are `build(deps):` commits and release
nothing of their own (owner, 2026-10-13); they ship with the next `feat:` or `fix:`, unreviewed like
every automerged update.

Decided (owner, 2026-10-14 evening): a repository ruleset lets only the semantic-release GitHub App
(as a bypass actor) and the owner create `v*` tags, and "Allow administrators to bypass" is off on
the environment `release`. Both are GitHub settings, an outward change made with the owner's
approval; neither is made yet, so the arbitrary-tag case above is live today (checked with `gh`
2026-10-15 ~19:00 UTC: no ruleset exists, and `release` still has its administrator bypass on).
Decided further (owner, 2026-10-15 afternoon): the ruleset's only bypass actor is the
semantic-release GitHub App (`guided-traffic-automation`, app id 5070048, the releases' author);
nobody creates a `v*` tag by hand, and an emergency tag means an administrator switches the
ruleset off for the moment, which the organisation's audit log records. Options weighed: the App
plus a team holding only the owner (a team to keep, for a path nobody uses); the App plus the
Repository admin role (the bot's token alone could start a signed release). Not verified: whether
a ruleset accepts a single user as a bypass actor, which is why the owner is not named directly.
What remains once they are made, per case:

- **A commit on `main`.** Branch protection is not part of the decision: a commit that passes the
  gates is still released and, since semantic-release tags it through the App, built, signed and
  notarized, as the first paragraph says.
- **The App's private key.** It is an organisation secret that the Semantic Release job and
  Renovate use ([keys](#keys-and-who-holds-them)); whoever holds it can create a `v*` tag on any
  commit, which the ruleset admits as the App's. Live for every holder of the key.
- **An administrator's account** can switch the ruleset off and then create such a tag: the
  emergency path the owner decided on 2026-10-15; the organisation's audit log records the switch.
  Live for every administrator once the ruleset exists.

### H-15: Update key compromise and rotation

Dormant until a copy built with the key is installed: the key exists since 2026-10-15, and v0.11.0
to v0.13.0 are built with it, but none of them had passed notarization on 2026-10-15 ~19:00 UTC.
Whoever holds the update key and can serve the feed (Pages) can offer
every copy an update Sparkle accepts. Sparkle's documentation says a release may change either the
Apple code signing certificate or the EdDSA key, but not both, so the key alone lets an update
carry another team's Developer ID ([rotating signing keys](https://sparkle-project.org/documentation/#rotating-signing-keys);
not tried). Where it lives is decided (owner, 2026-10-13: an environment secret that CI may use only
after the owner approved the run), which leaves a workflow the owner approves able to sign whatever
it built ([H-92](#h-92-an-environment-sparkle-without-rules-hands-the-update-key-to-any-run)).
Replacing a leaked key: a release whose app carries the new public key, offered in a feed and a
disk image still signed with the old one; with `SUVerifyUpdateBeforeExtraction` a change of the
EdDSA key needs a Developer ID-signed disk image, which Cogpit's is. A lost key: no feed can be
signed again; after 20 days Sparkle shows the items of an unsigned feed
([H-91](#h-91-after-20-days-of-unverifiable-feeds-sparkle-presents-items-again)). No second, offline
rotation key exists.

### H-16: Availability of the release host

Updates depend on two GitHub hosts: Pages for the feed (`guided-traffic.github.io`) and the release
storage for the disk image. If either is unreachable, Cogpit keeps running and offers no update; a
scheduled check fails silently, Help › Check for Updates… shows Sparkle's error. There is no
mirror.

### H-92: An environment `sparkle` without rules hands the update key to any run

Dormant: since 2026-10-15 the environment `sparkle` holds the secret (since 07:00 UTC), and it has
its rules: the owner as required reviewer, the tag rule `v*` only, no administrator bypass (checked
with `gh` 2026-10-15 ~19:00 UTC). It turns live if the rules are removed or loosened while the
secret is there. GitHub creates an environment the moment a
workflow run names it, without a reviewer and without a deployment rule; the first release run
after 2026-10-14 does so ("Appcast (sign)"). Live from the moment the owner stores
`SPARKLE_ED_PRIVATE_KEY` in it before the reviewer and the tag rule: then any workflow run in this
repository that names `environment: sparkle`, on any branch (agents and maintainers push to `main`
and branches directly, [H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)), gets the
key without anyone's approval. Cogpit's own job refuses to sign until it reads a required reviewer
and exactly the tag rule `v*` from the API, but a workflow written to take the key does not ask.
The owner's steps put the rules first ([setting up Sparkle](../developer/releasing.md#setting-up-sparkle)).
What stays live with the rules: the owner approves a run on a tag `v*`, and the job does with the
key what the workflow at that tag says; whoever may create such a tag (the GitHub App's key, a
maintainer) can bring a run of a changed workflow to the owner's approval, and the approval page
names the run and its tag, not what changed. The feed also signs the release's text as it stood
when the job ran: whoever can edit it before then (the same actors) has their text and links shown
in Sparkle's window. Administrators may bypass the reviewer while "Allow administrators to bypass"
is on (GitHub's default; `release` has it on). Decided (owner, 2026-10-14 evening): the bypass goes
off on `release`, a setting made only with the owner's explicit OK, not made yet (checked
2026-10-15) ([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released));
the decision names `release`, not `sparkle`; `sparkle` has the bypass off since its set-up on
2026-10-15 (checked with `gh` that day), and `github-pages` keeps its bypass (the owner, 2026-10-15:
the feed is signed).

### H-93: The feed's host can withhold updates

Live once a signed feed is on Pages. The signed feed has no expiry, and every feed ever deployed
is public. Per case: the host serves nothing or an error, and copies stay on their version without
a notice ([H-16](#h-16-availability-of-the-release-host)); the host serves an older signed feed (a
freeze), and copies older than its newest item are offered at most that item, a version with flaws
fixed since, while newer copies see nothing; a downgrade is not possible, since Sparkle offers only
a `CFBundleVersion` newer than the running one. The pipeline no longer freezes the feed by
accident: since 2026-10-16 "Appcast (deploy)" refuses a feed that would take back a newer release
Pages serves (`hack/appcast verify -live`); a workflow that skips the check still can. A newer
release's feed that lacks an older served item (its draft ran before that item was deployed) is
deployed with a warning, which freezes nothing: copies are offered the newer release. Dormant
while every release has the same `LSMinimumSystemVersion` (`MACOS_MIN`): if a release raised it, a
left-out older item would be the newest a copy on the older macOS may install, and such a copy
would be offered nothing newer than its own version. The two runs that waited
for an approval with such a workflow, v0.13.0's and v0.14.0's (their tags predate the check, their
drafts held their own release alone), were cancelled on 2026-10-16 (the owner's choice), so neither
can put the feed back. Who can change what Pages serves: a workflow run
with `pages: write` that the environment `github-pages` admits (by default the default branch, and
the tag rule the owner adds), so whoever may push a workflow to `main` (H-62). No expiry or
freshness check is designed.

### H-91: After 20 days of unverifiable feeds, Sparkle presents items again

Dormant until a signed feed is served (the key exists since 2026-10-15; `appcast.xml` answered 404
on 2026-10-15 ~19:00 UTC); then live by design. Sparkle's `SUSignedFeedFailureExpirationInterval`
(default 20 days, Cogpit keeps it) lets a copy that has not validated a feed for that long present
the items of an unverified feed, without their release notes and links, as a failsafe for a lost
key ([Sparkle's security settings](https://sparkle-project.org/documentation/customization/#security-settings)).
Such an item still needs a disk image the update key signed, or, for a change of the key, a
Developer ID-signed disk image. Per case: an attacker who can serve the feed (H-93) and get a disk
image signed with this team's Developer ID (any run on a tag `v*` in the environment `release`,
which has no reviewer, H-62) could, after 20 days of failing feeds, have copies offer an update
that changes the update key; the copies' logs show the failures, the user still clicks Install.
Setting the interval to 0 closes this and turns a lost key into one manual download per copy.

Confirmed by the owner (2026-10-14 evening): Sparkle's 20 days stay (the owner declined setting the
interval to 0). What remains, dormant until the first signed feed is served and live by design
after: the case above. Once the tag ruleset decided the same evening (and further on 2026-10-15)
is made ([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)), the Developer
ID-signed disk image it needs comes only from a run on a tag the GitHub App created, or one an
administrator created with the ruleset switched off.

### H-17: Installs owned by a package manager

For Homebrew (and, later, winget, Flathub and Linux repositories) the integrity of an update is
the package manager's, not Cogpit's. Dormant: this repository builds no package for a package
manager (no Homebrew cask, no winget, Flathub or Linux package; checked 2026-10-15 in the
`Makefile`, `packaging/` and the workflows), so the hand-over to the manager in the table above is
not built either.

### H-18: Dependencies

The Go modules Cogpit is built from, and the GitHub Actions its pipeline runs, are a supply chain
of their own. As built: Renovate ([`renovate.yml`](../../.github/workflows/renovate.yml),
[`.github/renovate.json`](../../.github/renovate.json)) proposes updates, and the minor, patch and
digest updates of Go modules, the Go version, Actions and the Makefile's tool pins merge on their
own once CI passes (an Action's major update too), Sparkle's never; govulncheck checks every push
to `main` for known vulnerabilities in code Cogpit reaches, and GoSec scans the shipped code (the
jobs "Vulnerability Check" and "GoSec Security Scan" in
[`release.yml`](../../.github/workflows/release.yml)). The policy is written in
[releasing › Renovate](../developer/releasing.md#renovate). What remains, per case:

- **An automerged update is reviewed by nobody.** A module's minor or patch release that passes
  CI ships in the next `feat:` or `fix:` release, signed and notarized
  ([H-62](#h-62-every-push-to-main-that-passes-the-gates-is-released)), and an Action's runs in the
  next workflow run. Live for every update Renovate merges on its own.
- **Actions are named by a version tag, not by a commit** (`actions/checkout@v7`,
  `actions/download-artifact@v8`, `actions/upload-artifact@v7`), also in the jobs that hold the
  Developer ID certificate and the API key (environment `release`) and the update key (environment
  `sparkle`) in [`build.yml`](../../.github/workflows/build.yml): whoever can move such a tag in its
  repository runs code in those jobs. Live for every release run.
- **The disk image's window is made by code from PyPI** (since 2026-10-16,
  [D-70](../developer/decisions.md#d-70-the-disk-images-window)): dmgbuild and its two
  dependencies, pinned by version and hash ([`dmgbuild-requirements.txt`](../../hack/macos/dmgbuild-requirements.txt),
  never merged by Renovate on its own), run in the app's job "Cogpit.app (macOS arm64)", which
  holds no secret, and make a template without the app. The signing job runs none of it: it checks
  that the template is a writable image of the volume "Cogpit <version>" holding exactly
  `.DS_Store`, `.background.tiff`, `.VolumeIcon.icns` and the link to `/Applications`, then copies
  the signed app in with Apple's tools. What the check does not cover: the bytes of those three
  files, which Finder reads on every Mac that opens the disk image (a crafted `.DS_Store`, TIFF or
  icon from a compromised release of these packages, or of the app job's own dependencies, would
  reach users inside a signed, notarized image). Live for every release built since this change.
- **What Renovate cannot see:** sigstore's trusted root, refreshed by hand
  ([trust boundaries › H-35](trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)),
  the terminal widget's copy in `third_party/`, which moves to a new upstream commit by hand
  ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)), and, since 2026-10-16, the envtest
  binaries (etcd and kube-apiserver of Kubernetes 1.37.0), pinned in the `Makefile` by version
  and SHA-512 and moved by hand ([D-64](../developer/decisions.md#d-64-envtest-a-real-api-server-in-ci-without-docker)).
  They run only in tests, never ship: a changed archive fails `make envtest-bin`; the archive the
  pin names runs in CI's envtest job, which holds no secret beyond the read-only `GITHUB_TOKEN`.
- **A release names no inputs.** It carries no SBOM and no provenance attestation of the build
  (none in the workflows, checked 2026-10-15).
- **The Kubernetes release table** (since 2026-10-18, [D-91](../developer/decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)):
  data, not code, which Renovate cannot see either; each release fetches it at build time
  ([H-130](#h-130-the-releases-kubernetes-release-table-is-fetched-unreviewed)).

### H-130: The release's Kubernetes release table is fetched unreviewed

The window marks a cluster's Kubernetes version and its kubelets by upstream's end-of-life dates
from a table built into both binaries ([releasing › the Kubernetes release table](../developer/releasing.md#the-kubernetes-release-table)).
The owner decided (2026-10-18) that it is fetched at build time so that every release carries the
table of its day, and never at run time: the app makes no connection for it. The release job
"Cogpit.app (macOS arm64)" downloads `kubernetes/website`'s `data/releases/schedule.yaml` and
`eol.yaml` from `raw.githubusercontent.com` over HTTPS, unpinned (they change about monthly; no
hash can name next month's file, and upstream signs neither), and builds what it gets into the
signed, notarized release, without a person reading the diff first. Live for every release built
since this change.

- **Who can change what ships:** whoever can change the two files on `kubernetes/website`'s
  `main` (the Kubernetes project's reviewers and its schedule-builder automation), and whoever can
  alter what GitHub's raw host serves to the job.
- **What a changed file can do:** shift dates, nothing else. The files are parsed into a fixed
  structure of strings in a job that holds no secret, written as JSON and embedded; Cogpit reads
  them as dates. A ⚠ can show too early, or a minor past its end of life can show as supported,
  which may delay a user's upgrade. The generator's checks bound it: every date parses, the minors
  follow one another without a gap and end in their order, each 12 to 18 months after its release,
  and against the committed table no minor is lost and no known minor's end of life moves by more
  than 120 days; anything else keeps the committed table. Within those bounds a false date ships.
- **What remains visible:** the step's log lists every minor that changed against the committed
  table, so a release's table can be reviewed after the fact; a build of the tag carries the
  committed table, and the release's binaries record `vcs.modified=true` when the two differ.
- **Not built:** a second source to compare with (endoflife.date was weighed and left out, a third
  party with a quality record of its own, [D-91](../developer/decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)),
  a person's approval of the table's diff before a release.

Accepted by the owner (2026-10-19 afternoon, asked as one question: "lassen"): the table stays
fetched as built (option (a)), an accepted risk that this entry keeps documented. Showing the
table's diff in the release notes, so that it is reviewed after the fact (option (c)), is not wanted
for now.
