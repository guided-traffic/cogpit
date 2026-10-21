#!/usr/bin/env bash
# WinSparkle's appcast for a release (D-111; docs/developer/releasing.md#windows): the Windows app's
# update feed at https://guided-traffic.github.io/cogpit/appcast-windows.xml, beside Sparkle's,
# in the same format and signed with the same update key. The release pipeline runs it through make:
#
#   hack/windows/appcast.sh draft <version> <windows dir> <draft dir> <feed url>
#
# draft (job "Windows appcast (draft)", no secret): an app built without the update key (the
# repository variable SPARKLE_PUBLIC_ED_KEY empty in this run, so the build job wrote none into
# it) gets no item, and the jobs after stop here. Otherwise it checks that the release's setup
# Cogpit-<version>-windows-<arch>-setup.exe is downloadable where the item will point, fetches the
# feed as Pages serves it (404: the first item starts it), the release's dates (gh), and drafts the
# feed with hack/appcast, which builds only on a feed the update key signed: sparkle:os
# windows-<arch>, sparkle:installerArguments "/S /relaunch" (packaging/windows/cogpit.nsi: silent,
# then Cogpit started again), Windows 10 1809 as the least system (ConPTY, the local terminal's
# console), the release's page for the notes. The draft carries a placeholder for the setup's EdDSA
# signature, which hack/macos/appcast.sh sign fills in the job "Windows appcast (sign)", with
# Sparkle's sign_update in the environment sparkle, as for the macOS feed.
set -euo pipefail

# The placeholder hack/macos/appcast.sh's sign replaces.
placeholder="COGPIT-SPARKLE-DMG-SIGNATURE"
installer_args="/S /relaunch"
min_windows="10.0.17763"

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

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}

draft() {
	[ "$#" -eq 4 ] || die "usage: $0 draft <version> <windows dir> <draft dir> <feed url>"
	local version=$1 in=$2 out=$3 feed=$4
	local setups setup arch url code length published link prerelease channel
	: "${GITHUB_REPOSITORY:?the repository, owner/name}"
	shopt -s nullglob
	setups=("$in"/Cogpit-"$version"-windows-*-setup.exe)
	shopt -u nullglob
	[ "${#setups[@]}" -eq 1 ] || die "expected one Cogpit-$version-windows-*-setup.exe in $in, found ${#setups[@]}"
	setup=${setups[0]}
	arch=$(basename "$setup" | sed -E "s/^Cogpit-$version-windows-(x64|arm64)-setup\.exe$/\1/")
	case "$arch" in x64 | arm64) ;; *) die "$(basename "$setup"): no architecture x64 or arm64" ;; esac
	rm -rf "$out"
	mkdir -p "$out"

	if [ -z "${SPARKLE_PUBLIC_ED_KEY:-}" ]; then
		notice "the Windows app of $version carries no update key (SPARKLE_PUBLIC_ED_KEY was empty): no appcast item"
		summary "No Windows appcast item: the app carries no update key (the repository variable \`SPARKLE_PUBLIC_ED_KEY\` was empty)."
		output drafted false
		return 0
	fi

	url="https://github.com/$GITHUB_REPOSITORY/releases/download/v$version/$(basename "$setup")"
	curl -fsSIL --proto '=https' --tlsv1.2 --retry 3 -o /dev/null "$url" ||
		die "$url is not downloadable: the item would point nowhere (are the release's files attached?)"

	# The feed as Pages serves it, past its cache (ten minutes), as the macOS draft fetches its own.
	code="$(curl -sS --proto '=https' --tlsv1.2 --retry 3 -o "$out/current.xml" -w '%{http_code}' \
		"$feed?cogpit-run=${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}")"
	case "$code" in
	200) log "the Windows appcast on Pages: $(wc -c <"$out/current.xml" | tr -d ' ') bytes" ;;
	404)
		rm -f "$out/current.xml"
		notice "no Windows appcast on Pages yet: $version's item starts it"
		;;
	*) die "fetching $feed: HTTP $code" ;;
	esac

	published="$(gh release view "v$version" --repo "$GITHUB_REPOSITORY" --json publishedAt --jq .publishedAt)"
	link="$(gh release view "v$version" --repo "$GITHUB_REPOSITORY" --json url --jq .url)"
	prerelease="$(gh release view "v$version" --repo "$GITHUB_REPOSITORY" --json isPrerelease --jq .isPrerelease)"
	channel=""
	if [ "$prerelease" = true ]; then channel=beta; fi
	length="$(wc -c <"$setup" | tr -d ' ')"

	go run ./hack/appcast add -feed "$out/current.xml" -pubkey "$SPARKLE_PUBLIC_ED_KEY" -out "$out/appcast.draft.xml" \
		-version "$version" -short "$version" -min "$min_windows" -url "$url" -length "$length" \
		-signature "$placeholder" -link "$link" -published "$published" -channel "$channel" \
		-os "windows-$arch" -installer-args "$installer_args"
	# The name hack/macos/appcast.sh's sign checks the download against.
	(cd "$(dirname "$setup")" && sha256 "$(basename "$setup")") >"$out/dmg.sha256"
	printf '%s\n' "$version" >"$out/version"
	summary "Windows appcast drafted: Cogpit $version (${channel:-stable}), $(basename "$setup"), $length bytes."
	output drafted true
}

case "${1:-}" in
draft) shift && draft "$@" ;;
*) die "usage: $0 draft <version> <windows dir> <draft dir> <feed url>" ;;
esac
