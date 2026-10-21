#!/usr/bin/env bash
# Checks a Windows setup end to end (D-111; make test-windows-package, CI's job Windows Package):
# a silent install for this user into a folder of its own, the files, both executables' version
# information, `cogpit-cli version`, the uninstall entry and the Start menu entry; then a second
# silent install over it (an update, as WinSparkle runs it, without /relaunch), then a silent
# uninstall, after which the folder, the entry and the shortcut are gone.
#
#   hack/windows/setup-test.sh <Cogpit-<version>-windows-<arch>-setup.exe>
#
# It installs into the user's profile and registry of the machine it runs on and removes it again:
# on CI a throwaway runner; locally an existing per-user Cogpit is refused, since the setup would
# update it instead of the test's folder.
set -euo pipefail

die() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'setup-test.sh: %s\n' "$*" >&2; fi
	exit 1
}

[ "$#" -eq 1 ] || die "usage: $0 <setup.exe>"
setup=$1
[ -f "$setup" ] || die "no $setup"
name=$(basename "$setup")
[[ "$name" =~ ^Cogpit-([0-9]+\.[0-9]+\.[0-9]+)-windows-(x64|arm64)-setup\.exe$ ]] || die "$name: Cogpit-<version>-windows-<arch>-setup.exe expected"
version=${BASH_REMATCH[1]}
# Neither Git Bash nor MSYS2 may turn the setup's /S, /D=… into paths.
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'
key='HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\io.guided-traffic.cogpit'
reg query "$key" >/dev/null 2>&1 && die "Cogpit is installed for this user already ($key): the setup would update that one"

dir=$(mktemp -d)
win_dir=$(cygpath -w "$dir/Cogpit")
# Whatever the test comes to, it leaves no installation behind: the uninstaller if there is one,
# else the entry and the shortcut by hand.
cleanup() {
	if [ -f "$dir/Cogpit/uninstall.exe" ]; then
		"$dir/Cogpit/uninstall.exe" /S "_?=$win_dir" || true
	fi
	reg delete "$key" /f >/dev/null 2>&1 || true
	powershell.exe -NoProfile -NonInteractive -Command \
		"Remove-Item -ErrorAction SilentlyContinue (Join-Path ([Environment]::GetFolderPath('Programs')) 'Cogpit.lnk')" || true
	rm -rf "$dir"
}
trap cleanup EXIT

# ps runs a PowerShell expression and prints its result.
ps() { powershell.exe -NoProfile -NonInteractive -Command "$1" | tr -d '\r'; }

check_installed() {
	local f got
	for f in cogpit.exe cogpit-cli.exe WinSparkle.dll LICENSE.txt THIRD-PARTY-NOTICES.txt uninstall.exe; do
		[ -f "$dir/Cogpit/$f" ] || die "the setup installed no $f"
	done
	for f in cogpit.exe cogpit-cli.exe; do
		got=$(ps "(Get-Item '$win_dir\\$f').VersionInfo.ProductVersion")
		[ "$got" = "$version" ] || die "$f's version information says $got, not $version"
	done
	got=$("$dir/Cogpit/cogpit-cli.exe" version)
	case "$got" in "cogpit-cli $version "*) ;; *) die "cogpit-cli version: $got" ;; esac
	got=$(reg query "$key" /v DisplayVersion | sed -n -E 's/.*REG_SZ +//p' | tr -d '\r')
	[ "$got" = "$version" ] || die "the uninstall entry's DisplayVersion is $got, not $version"
	got=$(reg query "$key" /v InstallLocation | sed -n -E 's/.*REG_SZ +//p' | tr -d '\r')
	[ "$got" = "$win_dir" ] || die "the uninstall entry's InstallLocation is $got, not $win_dir"
	got=$(ps "Test-Path (Join-Path ([Environment]::GetFolderPath('Programs')) 'Cogpit.lnk')")
	[ "$got" = True ] || die "no Start menu entry Cogpit.lnk"
	# The app is a GUI program (no console window): the PE optional header's subsystem is 2.
	local pe sub
	pe=$(od -An -t u4 -j 60 -N 4 "$dir/Cogpit/cogpit.exe" | tr -d ' ')
	sub=$(od -An -t u2 -j $((pe + 92)) -N 2 "$dir/Cogpit/cogpit.exe" | tr -d ' ')
	[ "$sub" = 2 ] || die "cogpit.exe's subsystem is $sub, not 2 (windows GUI)"
}

echo "install $name into $win_dir"
"$setup" /S /currentuser "/D=$win_dir" || die "the setup failed: exit $?"
check_installed
echo "install it again over itself, as an update does"
"$setup" /S "/D=$win_dir" || die "the second setup failed: exit $?"
check_installed

echo "uninstall"
# _?= runs the uninstaller in place and waits for it (without, it copies itself away and returns).
"$dir/Cogpit/uninstall.exe" /S "_?=$win_dir" || die "the uninstaller failed: exit $?"
rm -f "$dir/Cogpit/uninstall.exe" # in place it cannot remove itself
for f in cogpit.exe cogpit-cli.exe WinSparkle.dll LICENSE.txt THIRD-PARTY-NOTICES.txt; do
	[ ! -e "$dir/Cogpit/$f" ] || die "the uninstaller left $f"
done
reg query "$key" >/dev/null 2>&1 && die "the uninstaller left $key"
got=$(ps "Test-Path (Join-Path ([Environment]::GetFolderPath('Programs')) 'Cogpit.lnk')")
[ "$got" = False ] || die "the uninstaller left the Start menu entry"
echo "$name: installed, updated and uninstalled"
