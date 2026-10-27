#!/usr/bin/env bash
# Mesa's software OpenGL for Windows (D-112; make mesa): pal1000/mesa-dist-win's MSVC build of
# Mesa (MIT), whose llvmpipe driver draws the window of the Windows smoke test (hack/smoke.sh's
# SMOKE_MESA) on GitHub's runners, which have no OpenGL 2.1 driver. CI's tool only: no setup
# carries it.
#
#   hack/windows/mesa.sh fetch <version> <sha256> <dir>
#
# fetch downloads mesa3d-<version>-release-msvc.7z from the project's GitHub release, refuses it
# unless its SHA-256 is the pinned one (the Makefile's MESA_SHA256, GitHub's digest of the asset),
# and keeps the x64 opengl32.dll and libgallium_wgl.dll in <dir>/x64 (.cache/mesa/<version>).
# Unpacking needs 7z (MSYS2's p7zip) or bsdtar. Nothing from the archive runs here.
set -euo pipefail

die() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'error: %s\n' "$*" >&2; fi
	exit 1
}

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

dlls=(opengl32.dll libgallium_wgl.dll)

fetch() {
	[ "$#" -eq 3 ] || die "usage: $0 fetch <version> <sha256> <dir>"
	local version=$1 sha=$2 dir=$3 archive url got dll
	case "$version" in "" | *[!0-9.]*) die "Mesa version $version: digits and dots" ;; esac
	case "$sha" in *[!0-9a-f]*) die "the pinned SHA-256 is not lower-case hex" ;; esac
	[ "${#sha}" = 64 ] || die "the pinned SHA-256 has ${#sha} characters, not 64"
	archive="mesa3d-$version-release-msvc.7z"
	url="https://github.com/pal1000/mesa-dist-win/releases/download/$version/$archive"
	mkdir -p "$(dirname "$dir")"
	# Global, for the trap that removes it when the script ends.
	tmp="$(mktemp -d "$(dirname "$dir")/.mesa.XXXXXX")"
	trap 'rm -rf "$tmp"' EXIT
	curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$tmp/$archive" "$url"
	got="$(sha256 "$tmp/$archive")"
	[ "$got" = "$sha" ] || die "$archive has SHA-256 $got, the pin is $sha"
	mkdir -p "$tmp/7z"
	local members=("${dlls[@]/#/x64/}")
	if command -v 7z >/dev/null; then
		(cd "$tmp/7z" && 7z x -y "../$archive" "${members[@]}" >/dev/null)
	elif command -v bsdtar >/dev/null; then
		bsdtar -xf "$tmp/$archive" -C "$tmp/7z" "${members[@]}"
	else
		die "unpacking $archive needs 7z (MSYS2's p7zip) or bsdtar"
	fi
	mkdir -p "$tmp/x/x64"
	for dll in "${dlls[@]}"; do
		[ -f "$tmp/7z/x64/$dll" ] || die "$archive holds no x64/$dll"
		cp "$tmp/7z/x64/$dll" "$tmp/x/x64/$dll"
		# A PE image's machine field: 0x8664 for x64 (the PE header's offset is at 0x3c).
		local pe machine
		pe=$(od -An -t u4 -j 60 -N 4 "$tmp/x/x64/$dll" | tr -d ' ')
		machine=$(od -An -t x1 -j $((pe + 4)) -N 2 "$tmp/x/x64/$dll" | tr -d ' ')
		[ "$machine" = 6486 ] || die "x64/$dll: machine $machine, not x64"
	done
	printf '%s  %s\n' "$sha" "$archive" >"$tmp/x/.verified"
	rm -rf "$dir"
	mv "$tmp/x" "$dir"
	echo "Mesa $version in $dir (SHA-256 $sha)"
}

case "${1:-}" in
fetch) shift && fetch "$@" ;;
*) die "usage: $0 fetch <version> <sha256> <dir>" ;;
esac
