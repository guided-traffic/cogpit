#!/usr/bin/env bash
# The Windows download of a release (D-111; docs/developer/releasing.md#windows): the setup
# Cogpit-<version>-windows-<arch>-setup.exe, made with NSIS from what `make dist-windows` built. The
# release pipeline's job "Cogpit Setup (Windows x64)" runs it through make; any Windows machine with
# NSIS 3 (makensis) and Git Bash or MSYS2 runs the same.
#
#   hack/windows/setup.sh <version> <arch> <bin dir> <icon.ico> <winsparkle dir> <out dir>
#
# <arch> is the setup's name for it (x64, arm64); <bin dir> holds cogpit.exe and cogpit-cli.exe for
# it, with their resources (hack/windows/resources.sh); <winsparkle dir> is make winsparkle's
# (.cache/winsparkle/<version>). The setup installs, from packaging/windows/cogpit.nsi:
#
#   cogpit.exe, cogpit-cli.exe        the app and the command-line tool (D-16: the complete product)
#   WinSparkle.dll                    the updater the app loads (internal/update)
#   LICENSE.txt                       Cogpit's licence
#   THIRD-PARTY-NOTICES.txt           where the binaries' third-party code is listed, and the licences
#                                     of WinSparkle and the expat inside it, which ship as they are
#   uninstall.exe                     NSIS's uninstaller
#
# and a Start menu entry and the uninstall entry Settings › Apps lists.
set -euo pipefail

die() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'setup.sh: %s\n' "$*" >&2; fi
	exit 1
}

[ "$#" -eq 6 ] || die "usage: $0 <version> <arch> <bin dir> <icon.ico> <winsparkle dir> <out dir>"
version=$1 arch=$2 bins=$3 icon=$4 winsparkle=$5 out=$6
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "version $version: x.y.z expected"
case "$arch" in x64 | arm64) ;; *) die "arch $arch: x64 or arm64" ;; esac
for b in cogpit.exe cogpit-cli.exe; do [ -f "$bins/$b" ] || die "no $bins/$b"; done
[ -f "$icon" ] || die "no icon $icon"
[ -f "$winsparkle/$arch/WinSparkle.dll" ] || die "no $winsparkle/$arch/WinSparkle.dll: run make winsparkle"
command -v makensis >/dev/null || die "makensis is missing (NSIS 3: winget install NSIS.NSIS, and its folder on PATH)"
command -v cygpath >/dev/null || die "cygpath is missing: run this in Git Bash or MSYS2"

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$out"

cp "$bins/cogpit.exe" "$bins/cogpit-cli.exe" "$stage/"
cp "$winsparkle/$arch/WinSparkle.dll" "$stage/"
cp "$icon" "$stage/cogpit.ico"
cp "$root/LICENSE" "$stage/LICENSE.txt"
{
	echo "Cogpit $version for Windows ($arch)"
	echo
	echo "Cogpit is licensed under the Apache License, Version 2.0 (LICENSE.txt). The third-party code"
	echo "cogpit.exe and cogpit-cli.exe contain, and its licences:"
	echo "https://github.com/guided-traffic/cogpit/blob/v$version/ATTRIBUTION.md and the modules go.mod names"
	echo "(https://github.com/guided-traffic/cogpit/blob/v$version/go.mod)."
	echo
	echo "WinSparkle.dll is WinSparkle $(sed -n -E 's/^[0-9a-f]{64}  WinSparkle-([0-9.]+)\.zip$/\1/p' "$winsparkle/.verified"),"
	echo "https://winsparkle.org, as its release ships it. Its licence:"
	echo
	cat "$winsparkle/COPYING"
	echo
	echo "WinSparkle contains expat, whose licence:"
	echo
	cat "$winsparkle/COPYING.expat"
} >"$stage/THIRD-PARTY-NOTICES.txt"

setup="Cogpit-$version-windows-$arch-setup.exe"
# makensis is a Windows program: Windows paths, and options with - (MSYS2 would take /D… for a path).
makensis -V2 -INPUTCHARSET UTF8 \
	-DVERSION="$version" -DARCH="$arch" \
	-DSTAGE="$(cygpath -w "$stage")" -DOUTFILE="$(cygpath -w "$out/$setup")" \
	"$(cygpath -w "$root/packaging/windows/cogpit.nsi")"
[ -s "$out/$setup" ] || die "makensis wrote no $out/$setup"
echo "$out/$setup"
