#!/usr/bin/env bash
# The smoke test of the app as it ships (D-112; make smoke, and through it CI's macOS Lint and App
# and Linux Package; hack/windows/setup-test.sh for the installed cogpit.exe in Windows Package):
# the app's binary started with --smoke, which opens the window with the real driver in a home of
# its own, quits once the window drew a frame and fails on a logged error or after the bound.
#
#   [SMOKE_MESA=<dir>] hack/smoke.sh <cogpit binary> [<bound, 2m>]
#
# The run's home lands in a temporary folder of this script's (TMPDIR, and TMP and TEMP on
# Windows), removed once the app ended. On Linux the app gets what a desktop session gives it,
# where the caller has none: an X server in memory (xvfb-run) on a 1920×1200 screen of 24 bits, as
# xvfb-run's own 640×480×8 offers GLFW no GLX visual; a D-Bus session bus (dbus-run-session),
# without which Fyne logs an error for its theme watch; and XDG_RUNTIME_DIR. On Windows with
# SMOKE_MESA naming a folder with Mesa's opengl32.dll and libgallium_wgl.dll (make mesa), the two
# lie beside the binary for the run, the first place Windows looks for them, and Mesa's llvmpipe
# driver draws: GitHub's Windows runners have no OpenGL 2.1 driver, Fyne's minimum.
set -euo pipefail

die() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'smoke.sh: %s\n' "$*" >&2; fi
	exit 1
}

[ "$#" -ge 1 ] && [ "$#" -le 2 ] || die "usage: $0 <cogpit binary> [<bound>]"
bin=$1
bound=${2:-2m}
[ -f "$bin" ] && [ -x "$bin" ] || die "no executable $bin"

tmp=$(mktemp -d)
placed=()
cleanup() {
	local f
	for f in ${placed[@]+"${placed[@]}"}; do rm -f "$f"; done
	rm -rf "$tmp"
}
trap cleanup EXIT

run=("$bin" --smoke "$bound")
tmp_native=$tmp
case "$(uname -s)" in
Linux)
	if [ -z "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
		command -v xvfb-run >/dev/null || die "no display, and no xvfb-run (Debian's packages xvfb and xauth)"
		run=(xvfb-run -a -s '-screen 0 1920x1200x24' "${run[@]}")
	fi
	if [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ]; then
		command -v dbus-run-session >/dev/null || die "no D-Bus session bus, and no dbus-run-session (Debian's package dbus)"
		run=(dbus-run-session -- "${run[@]}")
	fi
	if [ -z "${XDG_RUNTIME_DIR:-}" ]; then
		mkdir -m 0700 "$tmp/run"
		export XDG_RUNTIME_DIR=$tmp/run
	fi
	;;
MINGW* | MSYS* | CYGWIN*)
	tmp_native=$(cygpath -w "$tmp")
	if [ -n "${SMOKE_MESA:-}" ]; then
		for dll in opengl32.dll libgallium_wgl.dll; do
			[ -f "$SMOKE_MESA/$dll" ] || die "no $SMOKE_MESA/$dll: make mesa fetches it"
			to="$(dirname "$bin")/$dll"
			[ ! -e "$to" ] || die "$to is there already"
			cp "$SMOKE_MESA/$dll" "$to"
			placed+=("$to")
		done
		export GALLIUM_DRIVER=llvmpipe
	fi
	;;
esac

echo "smoke test: ${run[*]}"
TMPDIR=$tmp TMP=$tmp_native TEMP=$tmp_native "${run[@]}"
