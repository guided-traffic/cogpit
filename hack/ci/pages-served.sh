#!/usr/bin/env bash
# pages-served.sh <site dir>: waits until GitHub Pages serves the feeds and the APT index of the
# site just deployed, byte for byte (D-164; docs/developer/releasing.md#github-pages):
# appcast.xml, appcast-windows.xml and apt/dists/stable/InRelease, those of them <site dir> holds.
# actions/deploy-pages reports the status Pages gives the deployment's ID, the commit, so a deploy
# can report success while Pages serves another site; this fails the job then. Pages' cache keys on
# the whole URL, so a query of its own reads past it.
set -euo pipefail

die() { echo "pages-served.sh: $*" >&2; exit 1; }

[ "$#" -eq 1 ] || die "usage: $0 <site dir>"
site=$1
[ -d "$site" ] || die "no site directory $site"
command -v curl >/dev/null || "$(dirname "$0")/apt-install.sh" curl
pages=${PAGES_URL:-https://guided-traffic.github.io/cogpit}
timeout=${PAGES_SERVED_TIMEOUT:-600} # seconds
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

files=()
for f in appcast.xml appcast-windows.xml apt/dists/stable/InRelease; do
	[ ! -f "$site/$f" ] || files+=("$f")
done
[ "${#files[@]}" -gt 0 ] || die "$site holds no feed and no APT index"

deadline=$((SECONDS + timeout))
for f in "${files[@]}"; do
	while :; do
		code=$(curl -sS --proto '=https' --tlsv1.2 -o "$work/served" -w '%{http_code}' \
			"$pages/$f?run=${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}-$SECONDS" || echo 000)
		if [ "$code" = 200 ] && cmp -s "$work/served" "$site/$f"; then
			echo "Pages serves this deploy's $f"
			break
		fi
		if [ "$SECONDS" -ge "$deadline" ]; then
			served=0
			[ ! -f "$work/served" ] || served=$(($(wc -c <"$work/served")))
			die "GET $pages/$f: HTTP $code, $served bytes, not the $(($(wc -c <"$site/$f"))) bytes deployed, after ${timeout}s"
		fi
		sleep 10
	done
done
