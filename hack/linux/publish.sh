#!/usr/bin/env bash
# The Linux packages on their release (D-108; docs/developer/releasing.md#linux): the release
# pipeline's job "Attach to the release (Linux)" runs it with the packages the Linux jobs built.
#
#   hack/linux/publish.sh <tag> <dir>
#
# <dir> holds cogpit_<version>_amd64.deb and cogpit_<version>_arm64.deb, nothing else. The release's
# SHA256SUMS lists every download of the release, the macOS one too: the packages' lines are merged
# into the one the release has (hack/ci/sha256sums.sh), and the macOS job merges its own the same
# way; the two jobs share a concurrency group, so they take turns. Then the packages and SHA256SUMS
# are uploaded (replacing the ones a re-run attached) and the note on installing them is appended
# to the release's text, once. Needs gh with GH_TOKEN (contents: write).
set -euo pipefail

die() { echo "publish.sh: $*" >&2; exit 1; }

[ "$#" -eq 2 ] || die "usage: $0 <tag> <dir>"
tag=$1 dir=$2
[[ "$tag" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]] || die "tag $tag: v<x.y.z> expected"
version=${BASH_REMATCH[1]}
here=$(cd "$(dirname "$0")" && pwd)

mapfile -t debs < <(find "$dir" -maxdepth 1 -type f -printf '%f\n' | sort)
want=("cogpit_${version}_amd64.deb" "cogpit_${version}_arm64.deb")
[ "${debs[*]}" = "${want[*]}" ] || die "$dir holds ${debs[*]:-nothing}, expected ${want[*]}"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
(cd "$dir" && sha256sum "${debs[@]}") >"$work/linux.sums"
"$here/../ci/sha256sums.sh" "$tag" "$work/linux.sums" "$work/SHA256SUMS"
cat "$work/SHA256SUMS"

gh release upload "$tag" "${debs[@]/#/$dir/}" "$work/SHA256SUMS" --clobber

# The note, once: a re-run finds its heading in the text already.
heading="**Linux (Debian, Ubuntu).**"
gh release view "$tag" --json body --jq .body >"$work/body.md"
if grep -qF "$heading" "$work/body.md"; then
	echo "the release's text has the Linux note already"
	exit 0
fi
cat >>"$work/body.md" <<EOF

---

$heading \`cogpit_${version}_amd64.deb\` (x86-64) and \`cogpit_${version}_arm64.deb\` (ARM) are Cogpit for Debian 13, Ubuntu 24.04 and later: the desktop app, with the command-line tool \`cogpit-cli\`. Install with \`sudo apt install ./cogpit_${version}_amd64.deb\`. The package sets up Cogpit's APT repository (\`https://guided-traffic.github.io/cogpit/apt\`, signed with the key \`BE09 5407 4B36 CBDE 8875  7E52 A9D2 608B 0DC7 6494\`), so that apt and the Software Updater offer new releases and Cogpit's Update button installs them after an administrator's password; delete \`/etc/apt/sources.list.d/cogpit.sources\` to stop that. Check the packages against \`SHA256SUMS\` (\`sha256sum -c --ignore-missing SHA256SUMS\`).
EOF
gh release edit "$tag" --notes-file "$work/body.md" >/dev/null
echo "appended the Linux note to $tag's text"
