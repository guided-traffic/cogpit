#!/usr/bin/env bash
# Cogpit's APT repository (D-108; docs/developer/releasing.md#linux): the packages of the newest
# releases and their signed index, served by GitHub Pages at
# https://guided-traffic.github.io/cogpit/apt beside Sparkle's appcast. The .deb sets it up
# (/etc/apt/sources.list.d/cogpit.sources, its public key inline), so apt, the Software Updater and
# Cogpit's Update button find new releases. The release pipeline runs this through make:
#
#   hack/linux/apt.sh mirror <url> <keyring.asc> <dir>   the repository Pages serves, into <dir>
#   hack/linux/apt.sh build <dir> <live dir> <deb...>    the new repository, unsigned
#   hack/linux/apt.sh sign <dir> <keyring.asc>           InRelease and Release.gpg (APT_SIGNING_KEY)
#   hack/linux/apt.sh verify <dir> <keyring.asc>         every signature and checksum
#   hack/linux/apt.sh newer <dir> <url> <keyring.asc>    refuses a repository older than the served one
#
# The layout: pool/main/c/cogpit/cogpit_<version>_<arch>.deb, the newest KEEP versions of each
# architecture (the new packages and the served ones together); dists/stable/Release with
# InRelease and Release.gpg; dists/stable/main/binary-<arch>/Packages(.gz) and their by-hash
# copies (Acquire-By-Hash). Pages caches every file for up to ten minutes, so a client may read an
# InRelease from before a deploy: by-hash names make it fetch the index that InRelease names, and
# build keeps the served generation's by-hash files and packages for it. files.sha256 lists every
# file with its SHA-256, so that the next deploy (Linux or the appcast's) can fetch the repository
# again: mirror takes the files it lists and checks them all against the signed index before
# anything of them is kept, a repository it cannot check is an error, and none served (404) is an
# empty <dir>.
#
# The signing key reaches gpg on its standard input only, in a temporary GNUPGHOME removed on exit;
# nothing derived from it is echoed. sign refuses a key whose fingerprint is not the one
# <keyring.asc> holds (the packages' key). Without APT_SIGNING_KEY (a fork) sign signs nothing and
# says so (signed=false in $GITHUB_OUTPUT): the repository is not deployed.
set -euo pipefail

KEEP=${KEEP:-2}
SUITE=stable
COMPONENT=main
ARCHS=(amd64 arm64)

die() { echo "apt.sh: $*" >&2; exit 1; }
log() { echo "apt.sh: $*"; }
notice() { if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::notice::apt.sh: $*"; else log "$*"; fi; }
output() { if [ -n "${GITHUB_OUTPUT:-}" ]; then echo "$1" >>"$GITHUB_OUTPUT"; fi; }

need() { for t in "$@"; do command -v "$t" >/dev/null || die "$t is missing"; done; }

# mktmp <var>: a new temporary directory in <var>, removed when the script exits.
TMPS=()
trap 'rm -rf "${TMPS[@]}"' EXIT
mktmp() {
	local d
	d=$(mktemp -d)
	TMPS+=("$d")
	printf -v "$1" %s "$d"
}

# fetch <url> <file>: the file as Pages serves it now (a query passes its cache); prints the
# HTTP status, 000 on a network error. HTTPS only; APT_TEST_PROTO opens plain HTTP for a local test.
fetch() {
	curl -sS --proto "${APT_TEST_PROTO:-=https}" --tlsv1.2 --retry 3 --retry-all-errors -o "$2" -w '%{http_code}' \
		"$1?t=$(date +%s)" 2>/dev/null || true
}

# keyring <keyring.asc> <dir>: the key as a binary keyring for gpgv, in <dir>/keyring.gpg.
keyring() {
	gpg --batch --dearmor <"$1" >"$2/keyring.gpg" 2>/dev/null || die "$1: not an armored OpenPGP key"
}

# release_entries <Release>: "<sha256> <size> <path>" for each file the Release lists.
release_entries() {
	awk '/^SHA256:/ { on = 1; next } /^[^ ]/ { on = 0 } on && NF == 3 { print $1, $2, $3 }' "$1"
}

# versions <Packages>: "<version> <arch> <filename> <sha256> <size>" for each package.
packages_entries() {
	awk -v RS= -F '\n' '{
		v = a = f = s = z = ""
		for (i = 1; i <= NF; i++) {
			split($i, kv, ": ")
			if (kv[1] == "Version") v = kv[2]
			else if (kv[1] == "Architecture") a = kv[2]
			else if (kv[1] == "Filename") f = kv[2]
			else if (kv[1] == "SHA256") s = kv[2]
			else if (kv[1] == "Size") z = kv[2]
		}
		if (v != "") print v, a, f, s, z
	}' "$1"
}

# check_file <file> <sha256> [<size>]
check_file() {
	[ -f "$1" ] || die "$1 is missing"
	local got
	got=$(sha256sum "$1" | cut -d' ' -f1)
	[ "$got" = "$2" ] || die "$1: SHA-256 $got, the index says $2"
	if [ -n "${3:-}" ]; then
		[ "$(stat -c %s "$1")" = "$3" ] || die "$1: $(stat -c %s "$1") bytes, the index says $3"
	fi
}

# verify <dir> <keyring.asc>: InRelease and Release.gpg signed by the key, InRelease's text the
# Release, every file the Release lists and its by-hash copy, and every package the Packages list.
verify() {
	local dir=$1 tmp
	need gpg gpgv sha256sum awk
	mktmp tmp
	keyring "$2" "$tmp"
	local d=$dir/dists/$SUITE
	gpgv --quiet --keyring "$tmp/keyring.gpg" --output "$tmp/inrelease.txt" "$d/InRelease" 2>"$tmp/gpgv.log" ||
		die "$d/InRelease: no valid signature by $2: $(cat "$tmp/gpgv.log")"
	gpgv --quiet --keyring "$tmp/keyring.gpg" "$d/Release.gpg" "$d/Release" 2>"$tmp/gpgv.log" ||
		die "$d/Release.gpg: no valid signature by $2: $(cat "$tmp/gpgv.log")"
	cmp -s "$tmp/inrelease.txt" "$d/Release" || die "$d/InRelease does not sign the text of $d/Release"
	grep -qx 'Acquire-By-Hash: yes' "$d/Release" || die "$d/Release: no Acquire-By-Hash"
	local hash size path n=0
	while read -r hash size path; do
		check_file "$d/$path" "$hash" "$size"
		check_file "$d/$(dirname "$path")/by-hash/SHA256/$hash" "$hash" "$size"
		n=$((n + 1))
	done < <(release_entries "$d/Release")
	[ "$n" -gt 0 ] || die "$d/Release lists no files"
	local version arch file sha p=0
	for arch in "${ARCHS[@]}"; do
		local list=$d/$COMPONENT/binary-$arch/Packages
		[ -f "$list" ] || continue
		while read -r version arch file sha size; do
			check_file "$dir/$file" "$sha" "$size"
			p=$((p + 1))
		done < <(packages_entries "$list")
	done
	[ "$p" -gt 0 ] || die "$dir: no packages"
	log "$dir: signed by $(fingerprint "$2"), $n index files, $p packages: $(newest "$dir" | tr '\n' ' ')"
}

# fingerprint <keyring.asc>: the key's fingerprint.
fingerprint() {
	gpg --batch --with-colons --import-options show-only --import <"$1" 2>/dev/null |
		awk -F: '/^fpr/ { print $10; exit }'
}

# newest <dir>: "<arch> <version>" of the newest package of each architecture in <dir>'s index.
newest() {
	local arch list
	for arch in "${ARCHS[@]}"; do
		list=$1/dists/$SUITE/$COMPONENT/binary-$arch/Packages
		[ -f "$list" ] || continue
		packages_entries "$list" | awk '{ print $1 }' | sort -V | tail -n 1 | sed "s/^/$arch /"
	done
}

# mirror <url> <keyring.asc> <dir>: the repository Pages serves, checked; INDEX_ONLY=1 takes the
# index alone (dists/), for newer.
mirror() {
	local url=${1%/} out=$3 dir code
	need curl sha256sum
	rm -rf "$out"
	mkdir -p "$(dirname "$out")"
	# Fetched and checked in a directory of its own: <dir> holds a checked repository or nothing.
	mktmp dir
	code=$(fetch "$url/files.sha256" "$dir/files.sha256")
	case "$code" in
	200) ;;
	404)
		mkdir -p "$out"
		log "no APT repository at $url yet"
		return 0
		;;
	*) die "$url/files.sha256: HTTP $code" ;;
	esac
	local sha path n=0
	while read -r sha path; do
		case "$path" in
		'' | /* | *..*) die "$url/files.sha256 names $path" ;;
		esac
		if [ "${INDEX_ONLY:-}" = 1 ] && [[ "$path" != dists/* ]]; then continue; fi
		mkdir -p "$dir/$(dirname "$path")"
		code=$(fetch "$url/$path" "$dir/$path")
		[ "$code" = 200 ] || die "$url/$path: HTTP $code"
		check_file "$dir/$path" "$sha"
		n=$((n + 1))
	done <"$dir/files.sha256"
	if [ "${INDEX_ONLY:-}" = 1 ]; then
		local tmp
		mktmp tmp
		keyring "$2" "$tmp"
		gpgv --quiet --keyring "$tmp/keyring.gpg" "$dir/dists/$SUITE/InRelease" 2>/dev/null ||
			die "$url: InRelease has no valid signature by $2"
	else
		verify "$dir" "$2"
	fi
	mv "$dir" "$out"
	log "mirrored $n files from $url"
}

# build <dir> <live dir> <deb...>
build() {
	local dir=$1 live=$2
	shift 2
	need apt-ftparchive dpkg-deb gzip sha256sum
	[ "$#" -gt 0 ] || die "build: no packages"
	rm -rf "$dir"
	local pool=$dir/pool/$COMPONENT/c/cogpit
	mkdir -p "$pool"
	local deb name
	for deb in "$@" "$live"/pool/$COMPONENT/c/cogpit/*.deb; do
		[ -f "$deb" ] || continue
		name=cogpit_$(dpkg-deb --field "$deb" Version)_$(dpkg-deb --field "$deb" Architecture).deb
		[ "$(dpkg-deb --field "$deb" Package)" = cogpit ] || die "$deb is not the package cogpit"
		[ -e "$pool/$name" ] || cp "$deb" "$pool/$name"
	done
	# The newest KEEP versions of each architecture.
	local arch archs=()
	for arch in "${ARCHS[@]}"; do
		mapfile -t old < <(find "$pool" -name "cogpit_*_$arch.deb" -printf '%f\n' | sort -V | head -n -"$KEEP")
		for name in "${old[@]}"; do rm -f "$pool/$name"; done
		if compgen -G "$pool/cogpit_*_$arch.deb" >/dev/null; then archs+=("$arch"); fi
	done
	local d=$dir/dists/$SUITE
	for arch in "${archs[@]}"; do
		local b=$d/$COMPONENT/binary-$arch
		mkdir -p "$b"
		(cd "$dir" && apt-ftparchive --arch "$arch" packages "pool/$COMPONENT") >"$b/Packages"
		gzip -9nk "$b/Packages"
	done
	(cd "$d" && apt-ftparchive \
		-o APT::FTPArchive::Release::Origin=Cogpit \
		-o APT::FTPArchive::Release::Label=Cogpit \
		-o APT::FTPArchive::Release::Suite="$SUITE" \
		-o APT::FTPArchive::Release::Codename="$SUITE" \
		-o APT::FTPArchive::Release::Architectures="${archs[*]}" \
		-o APT::FTPArchive::Release::Components="$COMPONENT" \
		-o APT::FTPArchive::Release::Description="Cogpit, the native Kubernetes IDE: https://github.com/guided-traffic/cogpit" \
		-o APT::FTPArchive::Release::Acquire-By-Hash=yes \
		-o APT::FTPArchive::MD5=false -o APT::FTPArchive::SHA1=false -o APT::FTPArchive::SHA512=false \
		release .) >"$dir/Release.tmp"
	mv "$dir/Release.tmp" "$d/Release"
	# This generation's by-hash copies, and the served generation's that its Release names.
	local hash size path
	while read -r hash size path; do
		mkdir -p "$d/$(dirname "$path")/by-hash/SHA256"
		cp "$d/$path" "$d/$(dirname "$path")/by-hash/SHA256/$hash"
	done < <(release_entries "$d/Release")
	if [ -f "$live/dists/$SUITE/Release" ]; then
		while read -r hash size path; do
			local from=$live/dists/$SUITE/$(dirname "$path")/by-hash/SHA256/$hash
			local to=$d/$(dirname "$path")/by-hash/SHA256/$hash
			if [ -f "$from" ] && [ ! -e "$to" ] && [ -d "$(dirname "$(dirname "$to")")" ]; then cp "$from" "$to"; fi
		done < <(release_entries "$live/dists/$SUITE/Release")
	fi
	log "built $dir: $(newest "$dir" | tr '\n' ' ')($(find "$pool" -name '*.deb' | wc -l) packages)"
}

# files <dir>: files.sha256, every file of the repository but itself.
files() {
	local tmp
	mktmp tmp
	(cd "$1" && find . -type f ! -name files.sha256 -printf '%P\n' | LC_ALL=C sort | xargs -r sha256sum) >"$tmp/files.sha256"
	mv "$tmp/files.sha256" "$1/files.sha256"
}

# sign <dir> <keyring.asc>
sign() {
	local dir=$1 d=$1/dists/$SUITE
	need gpg
	if [ -z "${APT_SIGNING_KEY:-}" ]; then
		notice "no APT_SIGNING_KEY in the environment apt: the repository is not signed and Pages keeps the one it has"
		output signed=false
		return 0
	fi
	local want got gnupg
	want=$(fingerprint "$2")
	[ -n "$want" ] || die "$2: no key"
	mktmp gnupg
	printf '%s\n' "$APT_SIGNING_KEY" | GNUPGHOME=$gnupg gpg --batch --quiet --import 2>/dev/null ||
		die "APT_SIGNING_KEY is not an OpenPGP secret key"
	got=$(GNUPGHOME=$gnupg gpg --batch --with-colons --list-secret-keys 2>/dev/null | awk -F: '/^fpr/ { print $10; exit }')
	[ "$got" = "$want" ] || die "APT_SIGNING_KEY is the key $got, not the packages' key $want ($2)"
	local args=(--batch --yes --quiet --pinentry-mode loopback --passphrase '' --digest-algo SHA512 --local-user "$want")
	GNUPGHOME=$gnupg gpg "${args[@]}" --clearsign --output "$d/InRelease" "$d/Release"
	GNUPGHOME=$gnupg gpg "${args[@]}" --armor --detach-sign --output "$d/Release.gpg" "$d/Release"
	files "$dir"
	verify "$dir" "$2"
	output signed=true
}

# newer <dir> <url> <keyring.asc>: exit 0 unless Pages serves a newer package of an architecture
# than <dir> holds (an older release's run that ends after a newer one's), then exit 3.
newer() {
	local dir=$1 tmp
	mktmp tmp
	INDEX_ONLY=1 mirror "$2" "$3" "$tmp/live" >/dev/null
	local arch served ours
	while read -r arch served; do
		ours=$(newest "$dir" | awk -v a="$arch" '$1 == a { print $2 }')
		if [ -z "$ours" ] || [ "$(printf '%s\n%s\n' "$ours" "$served" | sort -V | tail -n 1)" != "$ours" ]; then
			echo "apt.sh: Pages serves cogpit $served for $arch, newer than ${ours:-none} here: not deployed" >&2
			return 3
		fi
	done < <(newest "$tmp/live")
	log "nothing newer on Pages than $(newest "$dir" | tr '\n' ' ')"
}

cmd=${1:-}
[ -n "$cmd" ] && shift
case "$cmd" in
mirror) [ "$#" -eq 3 ] || die "usage: $0 mirror <url> <keyring.asc> <dir>"; mirror "$@" ;;
build) [ "$#" -ge 3 ] || die "usage: $0 build <dir> <live dir> <deb...>"; build "$@" ;;
sign) [ "$#" -eq 2 ] || die "usage: $0 sign <dir> <keyring.asc>"; sign "$@" ;;
verify) [ "$#" -eq 2 ] || die "usage: $0 verify <dir> <keyring.asc>"; verify "$@" ;;
newer) [ "$#" -eq 3 ] || die "usage: $0 newer <dir> <url> <keyring.asc>"; newer "$@" ;;
*) die "usage: $0 mirror|build|sign|verify|newer …" ;;
esac
