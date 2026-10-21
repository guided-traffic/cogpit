#!/usr/bin/env bash
# The Windows setup on its release (D-111; docs/developer/releasing.md#windows): the release
# pipeline's job "Attach to the release (Windows)" runs it with the setup the Windows job built.
#
#   hack/windows/publish.sh <tag> <dir>
#
# <dir> holds Cogpit-<version>-windows-x64-setup.exe, nothing else. Its line is merged into the
# release's SHA256SUMS (hack/ci/sha256sums.sh), which the macOS and the Linux jobs merge into the
# same way; the three jobs share a concurrency group, so they take turns. Then the setup and
# SHA256SUMS are uploaded (replacing the ones a re-run attached) and the note on installing it is
# appended to the release's text, once. Needs gh with GH_TOKEN (contents: write).
set -euo pipefail

die() { echo "publish.sh: $*" >&2; exit 1; }

[ "$#" -eq 2 ] || die "usage: $0 <tag> <dir>"
tag=$1 dir=$2
[[ "$tag" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]] || die "tag $tag: v<x.y.z> expected"
version=${BASH_REMATCH[1]}
here=$(cd "$(dirname "$0")" && pwd)

mapfile -t files < <(find "$dir" -maxdepth 1 -type f -printf '%f\n' | sort)
setup="Cogpit-${version}-windows-x64-setup.exe"
[ "${files[*]}" = "$setup" ] || die "$dir holds ${files[*]:-nothing}, expected $setup"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
(cd "$dir" && sha256sum "$setup") >"$work/windows.sums"
"$here/../ci/sha256sums.sh" "$tag" "$work/windows.sums" "$work/SHA256SUMS"
cat "$work/SHA256SUMS"

gh release upload "$tag" "$dir/$setup" "$work/SHA256SUMS" --clobber

# The note, once: a re-run finds its heading in the text already.
heading="**Windows.**"
gh release view "$tag" --json body --jq .body >"$work/body.md"
if grep -qF "$heading" "$work/body.md"; then
	echo "the release's text has the Windows note already"
	exit 0
fi
cat >>"$work/body.md" <<EOF

---

$heading \`$setup\` is Cogpit for Windows 10 (1809) and 11 on x64 (ARM machines run it emulated): the desktop app, with the command-line tool \`cogpit-cli.exe\` beside it. The setup installs for your user without an administrator, into \`%LOCALAPPDATA%\\Programs\\Cogpit\`; \`/allusers\` installs for every user into Program Files, \`/S\` silently, \`/D=<folder>\` elsewhere. Cogpit updates itself through WinSparkle from its signed feed (\`https://guided-traffic.github.io/cogpit/appcast-windows.xml\`): the Update button, or Help › Check for Updates…. Check the setup against \`SHA256SUMS\` (in PowerShell: \`(Get-FileHash $setup).Hash\`).

**Not signed with Authenticode yet.** Windows SmartScreen warns when the setup is first opened: More info, then Run anyway. Updates are checked by WinSparkle against Cogpit's update key before they run ([docs/security/updates.md](https://github.com/guided-traffic/cogpit/blob/main/docs/security/updates.md#windows)).
EOF
gh release edit "$tag" --notes-file "$work/body.md" >/dev/null
echo "appended the Windows note to $tag's text"
