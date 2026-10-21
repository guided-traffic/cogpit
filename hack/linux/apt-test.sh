#!/usr/bin/env bash
# apt-test.sh <deb dir>: the APT repository's script (hack/linux/apt.sh) checked end to end on the
# packages make dist-linux built, with a throwaway key and a local web server, as CI's job "Linux
# Package" runs it on every push (make test-linux-package): a first repository signed and served,
# a second built on the first one's mirror (both versions kept, the first generation's by-hash
# files too), verify refusing a tampered package and another key, the mirror refusing a tampered
# file and leaving nothing, and newer refusing to replace the served repository with an older one.
# <deb dir> holds one cogpit_<version>_<arch>.deb; a second version is made from it with dpkg-deb.
set -euo pipefail

die() { echo "apt-test.sh: FAIL: $*" >&2; exit 1; }
ok() { echo "apt-test.sh: ok: $*"; }

[ "$#" -eq 1 ] || die "usage: $0 <deb dir>"
here=$(cd "$(dirname "$0")" && pwd)
apt=$here/apt.sh
mapfile -t debs < <(find "$1" -maxdepth 1 -name 'cogpit_*.deb')
[ "${#debs[@]}" -eq 1 ] || die "$1 holds ${#debs[@]} packages, expected one"
for t in python3 gpg gpgv dpkg-deb apt-ftparchive curl; do command -v "$t" >/dev/null || die "$t is missing"; done

work=$(mktemp -d)
pids=()
cleanup() {
	for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
	GNUPGHOME=$work/gnupg gpgconf --kill all 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

# A throwaway key, and a second version of the package (its control file's version raised).
export GNUPGHOME=$work/gnupg
mkdir -m 700 "$GNUPGHOME"
gpg --batch --quiet --passphrase '' --quick-gen-key 'Cogpit apt-test' ed25519 sign 1d 2>/dev/null
gpg --batch --armor --export 'Cogpit apt-test' >"$work/key.asc"
key=$(gpg --batch --armor --export-secret-keys 'Cogpit apt-test')
gpg --batch --quiet --passphrase '' --quick-gen-key 'Cogpit apt-test other' ed25519 sign 1d 2>/dev/null
other=$work/other.asc
gpg --batch --armor --export 'Cogpit apt-test other' >"$other"
unset GNUPGHOME

v1=$(dpkg-deb --field "${debs[0]}" Version)
arch=$(dpkg-deb --field "${debs[0]}" Architecture)
IFS=. read -r major minor patch <<<"$v1"
v2=$major.$minor.$((patch + 1))
mkdir -p "$work/pkg2" "$work/debs2"
dpkg-deb -R "${debs[0]}" "$work/pkg2"
sed -i "s/^Version: .*/Version: $v2/" "$work/pkg2/DEBIAN/control"
dpkg-deb --root-owner-group -Zgzip -z1 --build "$work/pkg2" "$work/debs2/cogpit_${v2}_${arch}.deb" >/dev/null

# serve <dir> <port>: a local web server on <dir>.
serve() {
	python3 -m http.server "$2" --bind 127.0.0.1 --directory "$1" >/dev/null 2>&1 &
	pids+=($!)
	for _ in $(seq 50); do curl -s -o /dev/null "http://127.0.0.1:$2/" && return 0; sleep 0.1; done
	die "no web server on port $2"
}
export APT_TEST_PROTO='=http,https'
port=$((20000 + RANDOM % 20000))

APT_SIGNING_KEY=$key "$apt" build "$work/repo1" "$work/none" "${debs[0]}" >/dev/null
APT_SIGNING_KEY=$key "$apt" sign "$work/repo1" "$work/key.asc" >/dev/null
ok "the first repository is signed and verifies ($v1)"
serve "$work/repo1" "$port"

"$apt" mirror "http://127.0.0.1:$port" "$work/key.asc" "$work/live" >/dev/null
[ -f "$work/live/pool/main/c/cogpit/cogpit_${v1}_${arch}.deb" ] || die "the mirror lacks $v1"
APT_SIGNING_KEY=$key "$apt" build "$work/repo2" "$work/live" "$work/debs2/cogpit_${v2}_${arch}.deb" >/dev/null
APT_SIGNING_KEY=$key "$apt" sign "$work/repo2" "$work/key.asc" >/dev/null
for v in "$v1" "$v2"; do
	[ -f "$work/repo2/pool/main/c/cogpit/cogpit_${v}_${arch}.deb" ] || die "the second repository lacks $v"
done
for h in "$work"/repo1/dists/stable/main/binary-"$arch"/by-hash/SHA256/*; do
	[ -f "$work/repo2/dists/stable/main/binary-$arch/by-hash/SHA256/$(basename "$h")" ] || die "the first generation's by-hash file $(basename "$h") is gone"
done
ok "the second repository keeps $v1 beside $v2 and the first generation's index"

"$apt" newer "$work/repo2" "http://127.0.0.1:$port" "$work/key.asc" >/dev/null || die "newer refused a newer repository"
port2=$((port + 1))
serve "$work/repo2" "$port2"
set +e
"$apt" newer "$work/repo1" "http://127.0.0.1:$port2" "$work/key.asc" 2>/dev/null
rc=$?
set -e
[ "$rc" -eq 3 ] || die "newer let an older repository replace a newer one (exit $rc)"
ok "an older repository does not replace the served newer one"

cp -r "$work/repo2" "$work/bad"
printf x >>"$work/bad/pool/main/c/cogpit/cogpit_${v2}_${arch}.deb"
if "$apt" verify "$work/bad" "$work/key.asc" 2>/dev/null; then die "verify passed a tampered package"; fi
if "$apt" verify "$work/repo2" "$other" 2>/dev/null; then die "verify passed another key's repository"; fi
ok "verify refuses a tampered package and another key"

(cd "$work/bad" && find . -type f ! -name files.sha256 -printf '%P\n' | LC_ALL=C sort | xargs sha256sum) >"$work/bad/files.sha256"
port3=$((port + 2))
serve "$work/bad" "$port3"
if "$apt" mirror "http://127.0.0.1:$port3" "$work/key.asc" "$work/live-bad" 2>/dev/null; then die "the mirror took a tampered repository"; fi
[ ! -e "$work/live-bad" ] || die "the mirror left a tampered repository behind"
ok "the mirror refuses a tampered package and leaves nothing"

if APT_SIGNING_KEY=$key "$apt" sign "$work/repo1" "$other" 2>/dev/null; then die "sign used a key that is not the packages' key"; fi
ok "sign refuses a key that is not the packages' key"
echo "apt-test.sh: all checks passed"
