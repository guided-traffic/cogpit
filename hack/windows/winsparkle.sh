#!/usr/bin/env bash
# WinSparkle, the Windows app's updater (D-111; docs/developer/releasing.md#windows): Sparkle's
# Windows port (MIT), which the setup puts beside cogpit.exe and the app loads at run time
# (internal/update/winsparkle_windows.go).
#
#   hack/windows/winsparkle.sh fetch <version> <sha256> <dir>
#
# fetch downloads WinSparkle-<version>.zip from WinSparkle's GitHub release, refuses it unless its
# SHA-256 is the pinned one (the Makefile's WINSPARKLE_SHA256, GitHub's digest of the asset), and
# keeps the x64 and arm64 WinSparkle.dll, its header (the API internal/update calls, for reading)
# and its licences in <dir> (.cache/winsparkle/<version>). Nothing from the archive runs here: the
# DLL is unsigned, so the pin is what vouches for it.
set -euo pipefail

die() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'error: %s\n' "$*" >&2; fi
	exit 1
}

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

fetch() {
	[ "$#" -eq 3 ] || die "usage: $0 fetch <version> <sha256> <dir>"
	local version=$1 sha=$2 dir=$3 url got src
	case "$version" in "" | *[!0-9.]*) die "WinSparkle version $version: digits and dots" ;; esac
	case "$sha" in *[!0-9a-f]*) die "the pinned SHA-256 is not lower-case hex" ;; esac
	[ "${#sha}" = 64 ] || die "the pinned SHA-256 has ${#sha} characters, not 64"
	url="https://github.com/vslavik/winsparkle/releases/download/v$version/WinSparkle-$version.zip"
	mkdir -p "$(dirname "$dir")"
	# Global, for the trap that removes it when the script ends.
	tmp="$(mktemp -d "$(dirname "$dir")/.winsparkle.XXXXXX")"
	trap 'rm -rf "$tmp"' EXIT
	curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$tmp/winsparkle.zip" "$url"
	got="$(sha256 "$tmp/winsparkle.zip")"
	[ "$got" = "$sha" ] || die "WinSparkle-$version.zip has SHA-256 $got, the pin is $sha"
	unzip -q "$tmp/winsparkle.zip" -d "$tmp/zip"
	src="$tmp/zip/WinSparkle-$version"
	mkdir -p "$tmp/x/x64" "$tmp/x/arm64"
	cp "$src/x64/Release/WinSparkle.dll" "$tmp/x/x64/WinSparkle.dll"
	cp "$src/ARM64/Release/WinSparkle.dll" "$tmp/x/arm64/WinSparkle.dll"
	cp "$src/include/winsparkle.h" "$src/COPYING" "$src/COPYING.expat" "$tmp/x/"
	# A PE image's machine field: 0x8664 x64, 0xaa64 arm64 (the PE header's offset is at 0x3c).
	for arch in x64:6486 arm64:64aa; do
		local dll="$tmp/x/${arch%%:*}/WinSparkle.dll" pe machine
		pe=$(od -An -t u4 -j 60 -N 4 "$dll" | tr -d ' ')
		machine=$(od -An -t x1 -j $((pe + 4)) -N 2 "$dll" | tr -d ' ')
		[ "$machine" = "${arch##*:}" ] || die "$dll: machine $machine, not ${arch%%:*}"
	done
	printf '%s  WinSparkle-%s.zip\n' "$sha" "$version" >"$tmp/x/.verified"
	rm -rf "$dir"
	mv "$tmp/x" "$dir"
	echo "WinSparkle $version in $dir (SHA-256 $sha)"
}

case "${1:-}" in
fetch) shift && fetch "$@" ;;
*) die "usage: $0 fetch <version> <sha256> <dir>" ;;
esac
