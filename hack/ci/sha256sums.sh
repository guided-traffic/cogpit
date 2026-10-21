#!/usr/bin/env bash
# sha256sums.sh <tag> <sums> <out>: the release's SHA256SUMS with the lines of <sums> merged in.
# A release's checksums file lists every download, and three jobs attach downloads (the macOS one,
# the Linux packages, D-108, and the Windows setup, D-111): each writes the lines for its own files,
# and this keeps the other jobs' lines from the SHA256SUMS the release has now (none yet: <sums>
# alone). A line of <sums> replaces the release's line for the same file (a re-run). The jobs share
# a concurrency group, so they never merge at the same time. The release's file is read from its
# public download URL.
set -euo pipefail

die() { echo "sha256sums.sh: $*" >&2; exit 1; }

[ "$#" -eq 3 ] || die "usage: $0 <tag> <sums> <out>"
tag=$1 sums=$2 out=$3
command -v curl >/dev/null || "$(dirname "$0")/apt-install.sh" curl
repo=${GITHUB_REPOSITORY:-guided-traffic/cogpit}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

code=$(curl -sSL --proto '=https' --tlsv1.2 --retry 3 -o "$work/release.sums" -w '%{http_code}' \
	"https://github.com/$repo/releases/download/$tag/SHA256SUMS" || true)
case "$code" in
200) ;;
404) : >"$work/release.sums" ;;
*) die "the release's SHA256SUMS: HTTP $code" ;;
esac

# Each line "<sha256>  <name>": every name of <sums> once, then the release's other names.
awk 'NR == FNR { if (NF == 2) { mine[$2] = 1; print } next } NF == 2 && !($2 in mine) { print }' \
	"$sums" "$work/release.sums" | sort -k2 >"$out"
[ -s "$out" ] || die "no checksums"
