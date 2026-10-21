#!/usr/bin/env bash
# The Windows resources of both executables (D-111): the icon Explorer, the taskbar and the Start
# menu show (named GLFW_ICON, the name GLFW loads the window's icon by; under another name the
# window's title bar shows Windows' generic one), the manifest (Windows 10 and later, per-monitor DPI awareness v2, long paths, common
# controls 6 for WinSparkle's dialogs, no elevation) and the version information Explorer's
# Properties and WinSparkle read. `make dist-windows` runs it before `go build`, which links a
# package's .syso files into its executable, and cleans up after.
#
#   hack/windows/resources.sh make <version> <icon.ico> <go-winres> <goarch>
#   hack/windows/resources.sh clean
#
# make fills packaging/windows/winres.json per executable (its description, its file name, the
# version as x.y.z.0) and writes cmd/cogpit/rsrc_windows_<goarch>.syso and
# cmd/cogpit-cli/rsrc_windows_<goarch>.syso with go-winres (pinned in the Makefile); clean removes
# them, so that no other build links a release's version. Both files are ignored by git.
set -euo pipefail

die() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'error: %s\n' "$*" >&2; fi
	exit 1
}

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)

make_resources() {
	[ "$#" -eq 4 ] || die "usage: $0 make <version> <icon.ico> <go-winres> <goarch>"
	local version=$1 icon=$2 winres=$3 goarch=$4 work name desc
	[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "version $version: x.y.z expected"
	case "$goarch" in amd64 | arm64) ;; *) die "goarch $goarch: amd64 or arm64" ;; esac
	[ -f "$icon" ] || die "no icon $icon"
	[ -x "$winres" ] || die "no go-winres at $winres"
	work=$(mktemp -d)
	trap 'rm -rf "$work"' RETURN
	for name in cogpit cogpit-cli; do
		case "$name" in
		cogpit) desc="Cogpit" ;;
		cogpit-cli) desc="Cogpit command-line tool" ;;
		esac
		mkdir -p "$work/$name"
		cp "$icon" "$work/$name/cogpit.ico"
		sed -e "s/@VERSION@/$version/g" -e "s/@NAME@/$name/g" -e "s/@DESCRIPTION@/$desc/g" \
			"$root/packaging/windows/winres.json" >"$work/$name/winres.json"
		"$winres" make --in "$work/$name/winres.json" --arch "$goarch" --out "$root/cmd/$name/rsrc"
		[ -s "$root/cmd/$name/rsrc_windows_$goarch.syso" ] || die "go-winres wrote no cmd/$name/rsrc_windows_$goarch.syso"
	done
	echo "resources for $version ($goarch): cmd/cogpit/rsrc_windows_$goarch.syso, cmd/cogpit-cli/rsrc_windows_$goarch.syso"
}

clean() {
	rm -f "$root"/cmd/cogpit/rsrc_windows_*.syso "$root"/cmd/cogpit-cli/rsrc_windows_*.syso
}

case "${1:-}" in
make) shift && make_resources "$@" ;;
clean) clean ;;
*) die "usage: $0 make <version> <icon.ico> <go-winres> <goarch> | clean" ;;
esac
