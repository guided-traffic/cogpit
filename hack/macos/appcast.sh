#!/usr/bin/env bash
# Sparkle's appcast for a release (D-4; docs/developer/releasing.md#the-appcast): the update feed at
# SUFeedURL that every installed copy reads, one feed with the releases' history, signed with the
# update key and served by GitHub Pages. The release pipeline runs it through make:
#
#   hack/macos/appcast.sh draft <version> <macos dir> <draft dir> <feed url>
#   hack/macos/appcast.sh sign <version> <macos dir> <draft dir> <site dir> <sparkle dir> [<download glob> <feed file>]
#
# draft (job "Appcast (draft)", no secret): reads the app in the stapled disk image
# Cogpit-<version>-macos-<arch>.dmg (CFBundleVersion, the short version, LSMinimumSystemVersion,
# SUFeedURL, SUPublicEDKey); an app without an update key gets no item and the jobs after stop
# here. Otherwise it checks that the key is the repository variable SPARKLE_PUBLIC_ED_KEY and
# that the release's disk image is downloadable where the item will point, fetches the feed as
# Pages serves it (404: the first item starts it), the release's notes and dates (gh), and
# drafts the feed with hack/appcast, which builds only on a feed the update key signed. The
# draft carries a placeholder for the disk image's EdDSA signature.
#
# sign (job "Appcast (sign)", environment sparkle, SPARKLE_ED_PRIVATE_KEY): without the key it
# says so and stops, signed=false, nothing to deploy, never a failure. With it, it first refuses
# to sign unless the environment sparkle has a required reviewer and admits only tags v*
# (GitHub creates an environment a workflow names without any rule: docs/security/updates.md,
# H-92); then Sparkle's own sign_update, from the pinned and verified archive (make sparkle),
# signs the disk image (the draft's, checked by its SHA-256), the signature replaces the
# placeholder, sign_update signs the feed (Sparkle 2.9's signed feeds) and verifies both. The
# key reaches sign_update on its standard input from the environment variable (printf is bash's
# builtin), so it is never on a command line or on disk; GitHub masks it in the log.
#
# Nothing here runs Go or a dependency's code in the job with the key: drafting and the check
# before Pages (hack/appcast verify) run in jobs without it.
set -euo pipefail

placeholder="COGPIT-SPARKLE-DMG-SIGNATURE"

log() { printf '%s\n' "$*"; }
die() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'error: %s\n' "$*" >&2; fi
	exit 1
}
notice() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::notice::%s\n' "$*"; else printf 'notice: %s\n' "$*"; fi
}
output() {
	if [ -n "${GITHUB_OUTPUT:-}" ]; then printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"; fi
	log "$1=$2"
}
summary() { if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >>"$GITHUB_STEP_SUMMARY"; fi; }

# one <dir> <glob>: the one file in dir that matches glob.
one() {
	local found=() f
	# shellcheck disable=SC2086 # $2 is the pattern
	for f in "$1"/$2; do [ ! -f "$f" ] || found+=("$f"); done
	[ "${#found[@]}" -eq 1 ] || die "expected one $2 in $1, found ${#found[@]}"
	printf '%s\n' "${found[0]}"
}

# plist_get <plist> <key>: the key's value, empty when it is missing.
plist_get() { plutil -extract "$2" raw -o - "$1" 2>/dev/null || true; }

draft() {
	[ "$#" -eq 4 ] || die "usage: $0 draft <version> <macos dir> <draft dir> <feed url>"
	local version=$1 in=$2 out=$3 feed=$4
	local dmg mnt plist bundle_version short min app_feed app_key url code channel length published link prerelease
	: "${GITHUB_REPOSITORY:?the repository, owner/name}"
	dmg="$(one "$in" "Cogpit-$version-macos-*.dmg")"
	rm -rf "$out"
	mkdir -p "$out"

	mnt="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/cogpit-appcast-mnt.XXXXXX")"
	hdiutil attach -quiet -readonly -nobrowse -noautoopen -mountpoint "$mnt" "$dmg"
	plist="$mnt/Cogpit.app/Contents/Info.plist"
	bundle_version="$(plist_get "$plist" CFBundleVersion)"
	short="$(plist_get "$plist" CFBundleShortVersionString)"
	min="$(plist_get "$plist" LSMinimumSystemVersion)"
	app_feed="$(plist_get "$plist" SUFeedURL)"
	app_key="$(plist_get "$plist" SUPublicEDKey)"
	hdiutil detach -quiet "$mnt"
	rmdir "$mnt"

	[ "$short" = "$version" ] || die "the app in $(basename "$dmg") is version $short, not $version"
	[ "$app_feed" = "$feed" ] || die "the app's SUFeedURL is '$app_feed', not $feed"
	if [ -z "$app_key" ]; then
		notice "the app in $(basename "$dmg") carries no update key (SPARKLE_PUBLIC_ED_KEY was empty when it was built): no appcast item for $version"
		summary "No appcast item: the app carries no update key (the repository variable \`SPARKLE_PUBLIC_ED_KEY\` was empty)."
		output drafted false
		return 0
	fi
	[ "$app_key" = "${SPARKLE_PUBLIC_ED_KEY:-}" ] ||
		die "the app's SUPublicEDKey is not the repository variable SPARKLE_PUBLIC_ED_KEY"

	url="https://github.com/$GITHUB_REPOSITORY/releases/download/v$version/$(basename "$dmg")"
	curl -fsSIL --proto '=https' --tlsv1.2 --retry 3 -o /dev/null "$url" ||
		die "$url is not downloadable: the item would point nowhere (are the release's files attached?)"

	# The feed as Pages serves it; the query passes its cache (ten minutes), so a release right
	# after another builds on the other's item.
	code="$(curl -sS --proto '=https' --tlsv1.2 --retry 3 -o "$out/current.xml" -w '%{http_code}' \
		"$feed?cogpit-run=${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}")"
	case "$code" in
	200) log "the appcast on Pages: $(wc -c <"$out/current.xml" | tr -d ' ') bytes" ;;
	404)
		rm -f "$out/current.xml"
		notice "no appcast on Pages yet: $version's item starts it"
		;;
	*) die "fetching $feed: HTTP $code" ;;
	esac

	gh release view "v$version" --repo "$GITHUB_REPOSITORY" --json body --jq .body >"$out/notes.md"
	published="$(gh release view "v$version" --repo "$GITHUB_REPOSITORY" --json publishedAt --jq .publishedAt)"
	link="$(gh release view "v$version" --repo "$GITHUB_REPOSITORY" --json url --jq .url)"
	prerelease="$(gh release view "v$version" --repo "$GITHUB_REPOSITORY" --json isPrerelease --jq .isPrerelease)"
	channel=""
	if [ "$prerelease" = true ]; then channel=beta; fi
	length="$(stat -f %z "$dmg")"

	go run ./hack/appcast add -feed "$out/current.xml" -pubkey "$app_key" -out "$out/appcast.draft.xml" \
		-version "$bundle_version" -short "$short" -min "$min" -url "$url" -length "$length" \
		-signature "$placeholder" -notes "$out/notes.md" -link "$link" -published "$published" -channel "$channel"
	(cd "$(dirname "$dmg")" && shasum -a 256 "$(basename "$dmg")") >"$out/dmg.sha256"
	printf '%s\n' "$bundle_version" >"$out/version"
	summary "Appcast drafted: Cogpit $short (\`$bundle_version\`, ${channel:-stable}), $(basename "$dmg"), $length bytes."
	output drafted true
}

# check_environment refuses to sign unless the environment sparkle has a required reviewer and
# admits only tags v*: GitHub creates an environment a workflow names without any rule, and its
# secret would then reach any run that names it.
check_environment() {
	local api="repos/$GITHUB_REPOSITORY/environments/sparkle" reviewers custom policies
	reviewers="$(gh api "$api" --jq '[.protection_rules[]? | select(.type == "required_reviewers") | .reviewers[]?] | length')" ||
		die "cannot read the protection rules of the environment sparkle"
	custom="$(gh api "$api" --jq '.deployment_branch_policy.custom_branch_policies // false')"
	policies=""
	if [ "$custom" = true ]; then
		policies="$(gh api "$api/deployment-branch-policies" --jq '[.branch_policies[] | .type + ":" + .name] | sort | join(" ")')"
	fi
	[ "$reviewers" -ge 1 ] ||
		die "the environment sparkle has no required reviewer: refusing to sign (docs/developer/releasing.md#setting-up-sparkle)"
	[ "$policies" = "tag:v*" ] ||
		die "the environment sparkle must admit tags v* only, it admits '${policies:-every branch and tag}': refusing to sign (docs/developer/releasing.md#setting-up-sparkle)"
	log "environment sparkle: $reviewers required reviewer(s), deployments from $policies"
}

sign() {
	[ "$#" -eq 5 ] || [ "$#" -eq 7 ] ||
		die "usage: $0 sign <version> <macos dir> <draft dir> <site dir> <sparkle dir> [<download glob> <feed file>]"
	local version=$1 in=$2 drafts=$3 site=$4 sparkle=$5
	# The Windows app's feed (D-111, hack/windows/appcast.sh) is signed here too, with the same key:
	# its setup in place of the disk image, appcast-windows.xml in place of appcast.xml.
	local glob=${6:-"Cogpit-$version-macos-*.dmg"} name=${7:-appcast.xml}
	local tool="$sparkle/bin/sign_update" dmg sig
	if [ -z "${SPARKLE_ED_PRIVATE_KEY:-}" ]; then
		notice "no SPARKLE_ED_PRIVATE_KEY in the environment sparkle: the appcast is not signed and Pages keeps the one it has"
		summary "Appcast **not signed**: the environment \`sparkle\` holds no \`SPARKLE_ED_PRIVATE_KEY\`; nothing is deployed."
		output signed false
		return 0
	fi
	: "${GITHUB_REPOSITORY:?the repository, owner/name}"
	check_environment
	if [ ! -f "$sparkle/.verified" ] || [ ! -x "$tool" ]; then die "no verified sign_update in $sparkle: make sparkle"; fi
	dmg="$(one "$in" "$glob")"
	(cd "$(dirname "$dmg")" && shasum -a 256 -c "$drafts/dmg.sha256" >/dev/null) ||
		die "$(basename "$dmg") is not the disk image the draft was made for"
	[ "$(grep -c "$placeholder" "$drafts/appcast.draft.xml")" = 1 ] || die "the draft does not hold one placeholder"

	sig="$(printf '%s' "$SPARKLE_ED_PRIVATE_KEY" | "$tool" --ed-key-file - -p "$dmg")"
	case "$sig" in "" | *[!A-Za-z0-9+/=]*) die "sign_update printed no signature for $(basename "$dmg")" ;; esac
	[ "${#sig}" = 88 ] || die "sign_update printed a signature of ${#sig} characters, not an Ed25519 one"
	rm -rf "$site"
	mkdir -p "$site"
	sed "s|$placeholder|$sig|" "$drafts/appcast.draft.xml" >"$site/$name"
	printf '%s' "$SPARKLE_ED_PRIVATE_KEY" | "$tool" --ed-key-file - "$site/$name"
	printf '%s' "$SPARKLE_ED_PRIVATE_KEY" | "$tool" --verify --ed-key-file - "$site/$name" ||
		die "sign_update does not verify the appcast it signed"
	printf '%s' "$SPARKLE_ED_PRIVATE_KEY" | "$tool" --verify --ed-key-file - "$dmg" "$sig" ||
		die "sign_update does not verify the disk image's signature"
	summary "Appcast signed: $(basename "$dmg") and the feed, with the update key of the environment \`sparkle\`."
	output signed true
}

case "${1:-}" in
draft | sign)
	cmd=$1
	shift
	"$cmd" "$@"
	;;
*) die "usage: $0 draft <version> <macos dir> <draft dir> <feed url> | sign <version> <macos dir> <draft dir> <site dir> <sparkle dir> [<download glob> <feed file>]" ;;
esac
