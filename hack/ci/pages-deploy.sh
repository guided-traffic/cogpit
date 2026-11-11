#!/usr/bin/env bash
# pages-deploy.sh <artifact id> <site dir>: deploys the uploaded site artifact to GitHub Pages and
# waits until Pages serves it (D-164; docs/developer/releasing.md#github-pages). It does what
# actions/deploy-pages does (the Pages API: create the deployment with the job's OIDC token, poll
# its status, cancel it on a timeout) with one difference: the deployment's build version is
# <commit>-<run id>-<run attempt>, where deploy-pages always takes the commit. Pages keys a
# deployment by that version, so every deploy of one release (a re-run, a second attempt) used the
# same ID and could report an earlier deploy's status as its own. Then it fetches the feeds and the
# APT index from Pages until they are the bytes of <site dir>: a deploy reported as succeeded that
# Pages does not serve fails here.
#
# Needs GH_TOKEN (pages: write), the job's id-token: write and GitHub's runner variables. The
# tokens reach curl through header and body files in a private temporary directory, never its
# arguments, and are never printed.
set -euo pipefail

die() { echo "pages-deploy.sh: $*" >&2; exit 1; }

[ "$#" -eq 2 ] || die "usage: $0 <artifact id> <site dir>"
artifact=$1 site=$2
[[ "$artifact" =~ ^[0-9]+$ ]] || die "artifact id \"$artifact\" is not a number"
[ -d "$site" ] || die "no site directory $site"
: "${GH_TOKEN:?pages-deploy.sh: GH_TOKEN is not set}"
: "${GITHUB_REPOSITORY:?}" "${GITHUB_SHA:?}" "${GITHUB_RUN_ID:?}" "${GITHUB_RUN_ATTEMPT:?}"
[ -n "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ] && [ -n "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ] ||
	die "no OIDC token to request: the job needs the permission id-token: write"
command -v curl >/dev/null && command -v jq >/dev/null || "$(dirname "$0")/apt-install.sh" curl jq

api=${GITHUB_API_URL:-https://api.github.com}
pages=${PAGES_URL:-https://guided-traffic.github.io/cogpit}
version="$GITHUB_SHA-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT"
timeout=${PAGES_DEPLOY_TIMEOUT:-600} # seconds for Pages to report, and again to serve
work=$(mktemp -d)
chmod 700 "$work"
trap 'rm -rf "$work"' EXIT

curlx() { curl -sS --proto '=https' --tlsv1.2 "$@"; }

# api_call <method> <path> [<body file>]: the answer in $work/answer, the HTTP status on stdout.
printf 'Authorization: Bearer %s\nAccept: application/vnd.github+json\nX-GitHub-Api-Version: 2022-11-28\n' \
	"$GH_TOKEN" >"$work/api.headers"
api_call() {
	local method=$1 path=$2 body=${3:-}
	local args=(-X "$method" -H "@$work/api.headers" -o "$work/answer" -w '%{http_code}')
	[ -z "$body" ] || args+=(-H 'Content-Type: application/json' --data-binary "@$body")
	curlx "${args[@]}" "$api/repos/$GITHUB_REPOSITORY/$path" || echo 000
}
answer() { jq -c . "$work/answer" 2>/dev/null || head -c 500 "$work/answer"; }

# The job's OIDC token, as deploy-pages asks for it (no audience).
printf 'Authorization: Bearer %s\n' "$ACTIONS_ID_TOKEN_REQUEST_TOKEN" >"$work/oidc.headers"
code=$(curlx -H "@$work/oidc.headers" -o "$work/oidc" -w '%{http_code}' "$ACTIONS_ID_TOKEN_REQUEST_URL" || echo 000)
[ "$code" = 200 ] || die "GET the job's OIDC token (ACTIONS_ID_TOKEN_REQUEST_URL): HTTP $code"
jq -e '.value | strings | length > 0' "$work/oidc" >/dev/null 2>&1 ||
	die "GET the job's OIDC token (ACTIONS_ID_TOKEN_REQUEST_URL): HTTP 200 without a token"
jq -n --argjson id "$artifact" --arg v "$version" --rawfile t <(jq -j .value "$work/oidc") \
	'{artifact_id: $id, pages_build_version: $v, oidc_token: $t}' >"$work/create.json"
rm -f "$work/oidc"

path=pages/deployments
code=$(api_call POST "$path" "$work/create.json")
rm -f "$work/create.json"
case "$code" in
200 | 201) ;;
*) die "POST $api/repos/$GITHUB_REPOSITORY/$path with artifact_id $artifact, pages_build_version $version and the job's OIDC token: HTTP $code, $(answer)" ;;
esac
id=$(jq -r '.id // (.status_url // "" | split("/") | last) // empty' "$work/answer")
[ -n "$id" ] || id=$version
page_url=$(jq -r '.page_url // empty' "$work/answer")
echo "created Pages deployment $id (artifact $artifact)"
[ -z "${GITHUB_OUTPUT:-}" ] || echo "page_url=$page_url" >>"$GITHUB_OUTPUT"

cancel() {
	local c
	c=$(api_call POST "pages/deployments/$id/cancel")
	echo "POST $api/repos/$GITHUB_REPOSITORY/pages/deployments/$id/cancel: HTTP $c" >&2
}

# Poll as deploy-pages does: a final error status ends it, an HTTP error ten times in a row too.
deadline=$((SECONDS + timeout)) errors=0 last=
while :; do
	sleep 5
	code=$(api_call GET "pages/deployments/$id")
	if [ "$code" != 200 ]; then
		errors=$((errors + 1))
		echo "GET $api/repos/$GITHUB_REPOSITORY/pages/deployments/$id: HTTP $code, $(answer)" >&2
		[ "$errors" -lt 10 ] || { cancel; die "the deployment's status: ten errors in a row"; }
	else
		errors=0
		status=$(jq -r '.status // ""' "$work/answer")
		[ "$status" = "$last" ] || echo "status: ${status:-(empty)}"
		last=$status
		case "$status" in
		succeed) break ;;
		deployment_failed | deployment_content_failed | deployment_cancelled | deployment_lost)
			die "Pages deployment $id: $status" ;;
		esac
	fi
	[ "$SECONDS" -lt "$deadline" ] || { cancel; die "Pages deployment $id: no success after ${timeout}s (last status: ${last:-(empty)})"; }
done

# What Pages serves: the two feeds and the APT index of <site dir>, byte for byte. Pages' cache
# keys on the whole URL, so a query of its own reads past it.
files=()
for f in appcast.xml appcast-windows.xml apt/dists/stable/InRelease; do
	[ ! -f "$site/$f" ] || files+=("$f")
done
deadline=$((SECONDS + timeout))
for f in "${files[@]}"; do
	while :; do
		code=$(curlx -o "$work/served" -w '%{http_code}' "$pages/$f?deploy=$version-$SECONDS" || echo 000)
		if [ "$code" = 200 ] && cmp -s "$work/served" "$site/$f"; then
			echo "Pages serves this deploy's $f"
			break
		fi
		[ "$SECONDS" -lt "$deadline" ] ||
			die "GET $pages/$f: HTTP $code, $(wc -c <"$work/served" 2>/dev/null || echo 0) bytes, not the $(wc -c <"$site/$f") bytes deployed in $id"
		sleep 10
	done
done
