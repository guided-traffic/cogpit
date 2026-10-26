#!/usr/bin/env bash
# Sparkle 2, the app's updater (D-4; docs/developer/releasing.md#updates): the pinned release
# archive and what the app bundle takes from it. `make sparkle` and `make app` run it.
#
#   hack/macos/sparkle.sh fetch <version> <sha256> <dir>
#   hack/macos/sparkle.sh bundle <Cogpit.app> <dir> <feed url> <public key> <bundle version>
#
# fetch downloads Sparkle-<version>.tar.xz from Sparkle's GitHub release, refuses it unless its
# SHA-256 is the pinned one (the Makefile's SPARKLE_SHA256), and keeps Sparkle.framework, the
# signing tool bin/sign_update, bin/generate_keys (for the owner, who makes the update key with it:
# docs/developer/releasing.md#setting-up-sparkle) and the licence in <dir> (.cache/sparkle/<version>).
# Nothing from the archive runs here.
#
# bundle puts the framework into <Cogpit.app>/Contents/Frameworks before the app is signed:
#   - without its XPC services (Installer.xpc, Downloader.xpc): they are for sandboxed apps only,
#     and Sparkle's documentation allows removing them from an app that is not sandboxed
#     (sparkle-project.org/documentation/sandboxing#removing-xpc-services), so nothing in the
#     bundle needs an entitlement; the framework is signed again ad-hoc, its seal named them;
#   - Info.plist: CFBundleVersion set to <bundle version>, the version Sparkle compares with the
#     appcast's sparkle:version (fyne package writes 1 for every build); SUFeedURL; SUPublicEDKey
#     when <public key> is given, else none, and the app's updater does not start
#     (internal/update); SUEnableAutomaticChecks NO, so that Sparkle neither asks for permission
#     nor checks on a schedule of its own: a release its own scheduled check finds is held until
#     the user acts on it, and Sparkle checks no more while it is held (the owner's report of
#     2026-10-26); Cogpit's preference "Check for updates automatically" runs Cogpit's scheduled
#     checks instead, at every start and every hour (internal/update); SUAllowsAutomaticUpdates
#     NO, so that Sparkle's window offers no automatic download and install: an update is
#     downloaded only when the user clicks Install Update; SUVerifyUpdateBeforeExtraction
#     and SURequireSignedFeed YES, so that Sparkle checks the disk image's EdDSA signature before it
#     extracts anything and accepts only an appcast signed with the same key (Sparkle 2.9 and
#     later).
# The app's binary does not link Sparkle: it loads the framework at run time (internal/update), so
# the binary needs no rpath.
set -euo pipefail

die() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'error: %s\n' "$*" >&2; fi
	exit 1
}

# check_key refuses a public key that is not the base64 of 32 bytes (an Ed25519 public key, as
# Sparkle's generate_keys prints it).
check_key() {
	local n
	case "$1" in *[!A-Za-z0-9+/=]*) die "SPARKLE_PUBLIC_ED_KEY is not base64" ;; esac
	n="$(printf '%s' "$1" | base64 --decode 2>/dev/null | wc -c | tr -d ' ')" || n=0
	[ "$n" = 32 ] || die "SPARKLE_PUBLIC_ED_KEY is not an Ed25519 public key (base64 of 32 bytes, as generate_keys prints it)"
}

fetch() {
	[ "$#" -eq 3 ] || die "usage: $0 fetch <version> <sha256> <dir>"
	local version=$1 sha=$2 dir=$3 url got
	case "$version" in "" | *[!0-9.]*) die "Sparkle version $version: digits and dots" ;; esac
	case "$sha" in *[!0-9a-f]*) die "the pinned SHA-256 is not lower-case hex" ;; esac
	[ "${#sha}" = 64 ] || die "the pinned SHA-256 has ${#sha} characters, not 64"
	url="https://github.com/sparkle-project/Sparkle/releases/download/$version/Sparkle-$version.tar.xz"
	mkdir -p "$(dirname "$dir")"
	# Global, for the trap that removes it when the script ends.
	tmp="$(mktemp -d "$(dirname "$dir")/.sparkle.XXXXXX")"
	trap 'rm -rf "$tmp"' EXIT
	curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$tmp/sparkle.tar.xz" "$url"
	got="$(shasum -a 256 "$tmp/sparkle.tar.xz" | cut -d' ' -f1)"
	[ "$got" = "$sha" ] || die "Sparkle-$version.tar.xz has SHA-256 $got, the pin is $sha"
	mkdir "$tmp/x"
	tar -xJf "$tmp/sparkle.tar.xz" -C "$tmp/x" ./Sparkle.framework ./bin/sign_update ./bin/generate_keys ./LICENSE
	[ -f "$tmp/x/Sparkle.framework/Versions/B/Sparkle" ] || die "the archive holds no Sparkle.framework"
	[ -x "$tmp/x/bin/sign_update" ] || die "the archive holds no bin/sign_update"
	printf '%s  Sparkle-%s.tar.xz\n' "$sha" "$version" >"$tmp/x/.verified"
	rm -rf "$dir"
	mv "$tmp/x" "$dir"
	echo "Sparkle $version in $dir (SHA-256 $sha)"
}

bundle() {
	[ "$#" -eq 5 ] || die "usage: $0 bundle <Cogpit.app> <dir> <feed url> <public key> <bundle version>"
	local app=$1 dir=$2 feed=$3 key=$4 version=$5
	local fw="$app/Contents/Frameworks/Sparkle.framework" plist="$app/Contents/Info.plist"
	[ -f "$dir/.verified" ] || die "no verified Sparkle in $dir: make sparkle"
	[ -f "$plist" ] || die "$app has no Info.plist"
	case "$feed" in https://*) ;; *) die "the appcast URL $feed is not https" ;; esac
	if [ -n "$key" ]; then check_key "$key"; fi
	# CFBundleVersion: at most three period-separated integers (Apple's format), which Sparkle
	# compares part by part. A prerelease's version (0.6.0-beta.1) needs a scheme of its own.
	printf '%s\n' "$version" | grep -Eq '^[0-9]+(\.[0-9]+){0,2}$' ||
		die "bundle version $version: one to three period-separated integers"

	rm -rf "$app/Contents/Frameworks"
	mkdir -p "$app/Contents/Frameworks"
	ditto "$dir/Sparkle.framework" "$fw"
	rm -rf "$fw/Versions/B/XPCServices" "$fw/XPCServices"
	codesign --force --sign - --options runtime "$fw"
	codesign --verify --strict "$fw"

	plutil -replace CFBundleVersion -string "$version" "$plist"
	plutil -replace SUFeedURL -string "$feed" "$plist"
	plutil -replace SUEnableAutomaticChecks -bool NO "$plist"
	plutil -replace SUAllowsAutomaticUpdates -bool NO "$plist"
	plutil -replace SUVerifyUpdateBeforeExtraction -bool YES "$plist"
	plutil -replace SURequireSignedFeed -bool YES "$plist"
	if [ -n "$key" ]; then
		plutil -replace SUPublicEDKey -string "$key" "$plist"
		echo "Sparkle bundled with the update key; the appcast is $feed"
	else
		plutil -remove SUPublicEDKey "$plist" 2>/dev/null || true
		echo "Sparkle bundled without an update key (SPARKLE_PUBLIC_ED_KEY is empty): this build does not update itself"
	fi
}

case "${1:-}" in
fetch | bundle)
	cmd=$1
	shift
	"$cmd" "$@"
	;;
*) die "usage: $0 fetch <version> <sha256> <dir> | bundle <Cogpit.app> <dir> <feed url> <public key> <bundle version>" ;;
esac
