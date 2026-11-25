#!/usr/bin/env bash
# The macOS download of a release (docs/developer/releasing.md#the-release-pipeline), made from
# what `make dist-app` built: the app, with cogpit-cli inside it (D-16), in a disk image, signed
# with the Developer ID and notarized by Apple when the signing secrets are there. The release
# pipeline's job "Sign and notarize (macOS)" runs it through `make dist-macos`; on a Mac it runs
# the same, signed ad-hoc.
#
#   hack/macos/release.sh <version> <in dir> <out dir>        everything (make dist-macos)
#   hack/macos/release.sh sign <version> <in dir> <out dir>   the pipeline's first job
#   hack/macos/release.sh notarize <version> <dir>            the pipeline's second job
#   hack/macos/release.sh cleanup
#
# <in dir> holds Cogpit_<version>_darwin_<arch>.zip; <out dir> receives the download. The pipeline
# splits the course in two jobs: `sign` signs the app and the disk image, submits the disk image to
# Apple's notary service without waiting and records the submission in
# <out dir>/notary-submission.json (its id, the disk image's name and SHA-256); `notarize` waits
# for Apple's verdict on exactly that submission, staples the ticket, checks the result and removes
# the record. A re-run of a `notarize` that ran out of time waits for the same submission: signing
# and submitting again makes a new disk image that starts Apple's queue over (v0.9.0 to v0.12.0,
# 2026-10-15: every re-run a new submission, none finished within the 45 minutes the one job
# waited). What happens depends on the environment:
#
#   Developer ID  MACOS_SIGN_P12 (the .p12, base64), MACOS_SIGN_P12_PASSWORD, APPLE_API_KEY_P8 (the
#                 App Store Connect key, base64), APPLE_API_KEY_ID and APPLE_API_ISSUER_ID all set,
#                 with APPLE_TEAM_ID and Apple's intermediate pinned in MACOS_SIGN_CA_URL and
#                 MACOS_SIGN_CA_SHA256: a temporary keychain; the app and its nested code (Sparkle,
#                 cogpit-cli) signed inside-out with the hardened runtime and a secure timestamp;
#                 Cogpit-<version>-macos-<arch>.dmg built and signed, notarized and its ticket
#                 stapled; every signature checked, and the disk image and the app in it as
#                 Gatekeeper checks them.
#   ad-hoc        MACOS_SIGN=adhoc and none of the secrets (a Mac without the material): the same
#                 steps signed ad-hoc, without notarization and Gatekeeper's check.
#   unsigned      none of the secrets: the app zip passes through unchanged.
#
# Some of the secrets but not all is an error, never a quiet fallback to an unsigned download; so is
# every failed signature, notarization or check. `cleanup` deletes the temporary keychain and the
# key files; the pipeline runs it with if: always() after a cancelled run. Secrets never reach the
# log: no set -x, nothing derived from them is echoed, the keychain's password is masked.
#
# Entitlements: none, for the app, Sparkle's parts and cogpit-cli. Go generates no code at run time
# (no JIT, no unsigned executable memory), the app loads only Apple's frameworks and
# Sparkle.framework, which carries the app's own signature (library validation holds), cogpit-cli
# (pure Go) loads only the system's libraries, and neither uses the camera, microphone, location,
# contacts or Apple Events. `make dist-macos MACOS_SIGN=adhoc` and starting the app
# signed this way is the check (docs/developer/releasing.md#signing-and-notarization).
set -euo pipefail

work="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
work="${work%/}/cogpit-macos-release"
keychain="$work/signing.keychain-db"
secrets="MACOS_SIGN_P12 MACOS_SIGN_P12_PASSWORD APPLE_API_KEY_P8 APPLE_API_KEY_ID APPLE_API_ISSUER_ID"
# The cogpit-cli in Contents/MacOS: its name and code-signing identifier (the Makefile's CLI_ID).
cli_name="cogpit-cli"
cli_identifier="io.guided-traffic.cogpit.cli"
# How long to wait for Apple's notary service, and how often to ask it.
notary_minutes="${NOTARY_TIMEOUT_MINUTES:-45}"
poll_seconds="${NOTARY_POLL_SECONDS:-60}"

log() { printf '%s\n' "$*"; }
err() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::error::%s\n' "$*" >&2; else printf 'error: %s\n' "$*" >&2; fi
}
die() { err "$@"; exit 1; }
warn() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::warning::%s\n' "$*" >&2; else printf 'warning: %s\n' "$*" >&2; fi
}
group() { if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::group::%s\n' "$*"; else printf '== %s\n' "$*"; fi; }
endgroup() { if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::endgroup::\n'; fi; }
mask() { if [ -n "${GITHUB_ACTIONS:-}" ]; then printf '::add-mask::%s\n' "$1"; fi; }
output() {
	if [ -n "${GITHUB_OUTPUT:-}" ]; then printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"; fi
	log "$1=$2"
}
summary() { if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >>"$GITHUB_STEP_SUMMARY"; fi; }

# retry <attempts> <command...>: Apple's timestamp and ticket services, and hdiutil on CI runners,
# fail now and then and succeed seconds later.
retry() {
	local attempt=1 attempts=$1
	shift
	until "$@"; do
		[ "$attempt" -lt "$attempts" ] || return 1
		log "$1 failed; again in $((attempt * 10)) s"
		sleep $((attempt * 10))
		attempt=$((attempt + 1))
	done
}

# signing_mode prints developer-id, adhoc or unsigned.
signing_mode() {
	local have=0 missing="" name
	for name in $secrets; do
		if [ -n "${!name:-}" ]; then have=$((have + 1)); else missing="$missing $name"; fi
	done
	if [ "$have" -eq 0 ]; then
		if [ "${MACOS_SIGN:-}" = adhoc ]; then echo adhoc; else echo unsigned; fi
		return
	fi
	[ -z "$missing" ] || die "the Developer ID signing needs every secret; missing:$missing"
	for name in APPLE_TEAM_ID MACOS_SIGN_CA_URL MACOS_SIGN_CA_SHA256; do
		[ -n "${!name:-}" ] || die "the Developer ID signing needs $name"
	done
	echo developer-id
}

# cleanup detaches the disk image, deletes the keychain, restores the keychain search list and
# removes the work directory with the key files; it runs in a subshell so that its set +e stays
# there.
cleanup() (
	set +e
	for m in "$work/mnt" "$work/template-mnt"; do
		# mount names the real path (/private/var/… for $TMPDIR's /var/…)
		if [ -d "$m" ] && mount | grep -qF " on $(cd "$m" && pwd -P) ("; then hdiutil detach -quiet -force "$m"; fi
	done
	if [ -f "$keychain" ]; then security delete-keychain "$keychain"; fi
	if [ -f "$work/keychains" ]; then
		saved=()
		while IFS= read -r line; do saved+=("$line"); done <"$work/keychains"
		security list-keychains -d user -s ${saved[@]+"${saved[@]}"}
	fi
	rm -rf "$work"
)

# setup_keychain creates the temporary keychain with the Developer ID identity and Apple's
# intermediate, and sets identity to the identity's SHA-1.
setup_keychain() {
	group "Temporary keychain with the Developer ID identity"
	local password p12="$work/signing.p12" ca="$work/DeveloperIDCA.cer" line identities count
	password="$(openssl rand -hex 32)"
	mask "$password"

	# Apple's intermediate, pinned by its SHA-256: without it beside the .p12 the identity is not
	# valid ("0 valid identities found").
	curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$ca" "$MACOS_SIGN_CA_URL"
	[ "$(shasum -a 256 "$ca" | cut -d' ' -f1)" = "$MACOS_SIGN_CA_SHA256" ] ||
		die "Apple's intermediate from $MACOS_SIGN_CA_URL does not match its pinned SHA-256"

	(umask 077 && printf '%s' "$MACOS_SIGN_P12" | base64 --decode >"$p12")
	# The user's keychain search list as it was, for cleanup to restore.
	security list-keychains -d user | sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//' |
		grep -vxF "$keychain" >"$work/keychains" || true
	security create-keychain -p "$password" "$keychain"
	security set-keychain-settings -lut 21600 "$keychain"
	security unlock-keychain -p "$password" "$keychain"
	# The keychain first in the search list, so codesign and the trust evaluation find the identity
	# and the intermediate.
	local search=("$keychain")
	while IFS= read -r line; do search+=("$line"); done <"$work/keychains"
	security list-keychains -d user -s "${search[@]}"

	security import "$p12" -k "$keychain" -f pkcs12 -x -P "$MACOS_SIGN_P12_PASSWORD" -T /usr/bin/codesign >/dev/null
	rm -f "$p12"
	# A .p12 exported with its chain carries Apple's intermediate already, and importing it again
	# fails with "already exists" (the first tag run, v0.5.0): then the one in the keychain must be
	# the pinned one.
	local imported hashes
	if ! imported="$(security import "$ca" -k "$keychain" 2>&1)"; then
		case "$imported" in
		*"already exists"*) ;;
		*) die "Apple's intermediate could not be imported: $imported" ;;
		esac
		hashes="$(security find-certificate -a -Z "$keychain")"
		grep -qix "SHA-256 hash: $MACOS_SIGN_CA_SHA256" <<<"$hashes" ||
			die "the keychain holds an intermediate that is not the pinned one ($MACOS_SIGN_CA_SHA256)"
		log "Apple's intermediate came with the .p12; it is the pinned one"
	fi
	security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$password" "$keychain" >/dev/null

	identities="$(security find-identity -v -p codesigning "$keychain")"
	log "$identities"
	identity="$(printf '%s\n' "$identities" |
		awk -v team="($APPLE_TEAM_ID)\"" '/"Developer ID Application: / && index($0, team) { print $2 }')"
	count="$(printf '%s' "$identity" | grep -c . || true)"
	[ "$count" = 1 ] || die "expected one valid Developer ID Application identity of team $APPLE_TEAM_ID, found $count (none: Apple's intermediate missing, or the certificate expired or revoked)"
	endgroup
}

# sign <path> [codesign options...]
sign() {
	local path=$1
	shift
	if [ "$mode" = developer-id ]; then
		retry 3 codesign --force --sign "$identity" --keychain "$keychain" --timestamp "$@" "$path"
	else
		codesign --force --sign - "$@" "$path"
	fi
}

# check_signature <path> <code|image>: the seal, the hardened runtime and no entitlements for code;
# for the Developer ID also the certificate's team and Apple's timestamp.
check_signature() {
	local path=$1 kind=$2 info
	codesign --verify --strict --deep --verbose=2 "$path"
	info="$(codesign --display --verbose=4 "$path" 2>&1)"
	printf '%s\n' "$info" | grep -E '^(Identifier=|Format=|CodeDirectory |Authority=|TeamIdentifier=|Timestamp=|Runtime Version=)' || true
	if [ "$kind" = code ]; then
		printf '%s\n' "$info" | grep -Eq '^CodeDirectory .*flags=0x[0-9a-f]+\([^)]*runtime' ||
			die "$path: signed without the hardened runtime"
		[ -z "$(codesign --display --entitlements - --xml "$path" 2>/dev/null)" ] ||
			die "$path: signed with entitlements; Cogpit needs none (hack/macos/release.sh, header)"
	fi
	if [ "$mode" = developer-id ]; then
		printf '%s\n' "$info" | grep -q "^Authority=Developer ID Application: .*($APPLE_TEAM_ID)\$" ||
			die "$path: not signed with team $APPLE_TEAM_ID's Developer ID Application certificate"
		printf '%s\n' "$info" | grep -qx "TeamIdentifier=$APPLE_TEAM_ID" || die "$path: team identifier is not $APPLE_TEAM_ID"
		printf '%s\n' "$info" | grep -q '^Timestamp=' || die "$path: no secure timestamp"
	fi
}

# sign_app signs the bundle inside-out, never with --deep. Its nested code is Sparkle (D-4;
# hack/macos/sparkle.sh put it there without its XPC services, which only sandboxed apps use):
# Autoupdate, Updater.app, then the framework, the order of Sparkle's documentation for signing
# outside Xcode (sparkle-project.org/documentation/sandboxing#code-signing); and cogpit-cli in
# Contents/MacOS beside the app's executable (D-16), with its own identifier; then the app. Each
# with the hardened runtime, Apple's timestamp and no entitlements (Sparkle's own ad-hoc signature
# of Autoupdate carries com.apple.application-identifier; its documented command does not keep it).
# Every Mach-O file in the bundle must then carry the bundle's signature authority, so a helper a
# newer Sparkle adds cannot reach Apple ad-hoc signed. Anything else nested is refused. cogpit-cli
# is never run here: this job runs no code of Cogpit's or its dependencies' beside the identity.
sign_app() {
	group "Sign $(basename "$app")"
	local dir part fw="$app/Contents/Frameworks/Sparkle.framework" cli="$app/Contents/MacOS/$cli_name"
	for dir in PlugIns XPCServices Helpers Library; do
		[ ! -e "$app/Contents/$dir" ] ||
			die "$app/Contents/$dir holds nested code: sign it inside-out before the bundle (no --deep)"
	done
	if [ -e "$app/Contents/Frameworks" ]; then
		[ "$(ls -A "$app/Contents/Frameworks")" = Sparkle.framework ] ||
			die "$app/Contents/Frameworks holds more than Sparkle.framework: sign it inside-out before the bundle (no --deep)"
		[ ! -e "$fw/XPCServices" ] && [ ! -e "$fw/Versions/B/XPCServices" ] ||
			die "Sparkle's XPC services are in the bundle; make app leaves them out (hack/macos/sparkle.sh)"
		for part in "$fw/Versions/B/Autoupdate" "$fw/Versions/B/Updater.app" "$fw"; do
			[ -e "$part" ] || die "$part is missing: not the Sparkle layout this script signs"
			sign "$part" --options runtime
			check_signature "$part" code
		done
	fi
	local entries
	entries="$(find "$app/Contents/MacOS" -mindepth 1 -maxdepth 1 -exec basename {} \; | sort | tr '\n' ' ')"
	[ "$entries" = "$(printf '%s\n' "$exe" "$cli_name" | sort | tr '\n' ' ')" ] ||
		die "$app/Contents/MacOS holds $entries, not $exe and $cli_name (make app)"
	if [ ! -f "$cli" ] || [ -L "$cli" ] || [ ! -x "$cli" ]; then die "$cli is not an executable file"; fi
	[ "$(lipo -archs "$cli")" = "$(lipo -archs "$app/Contents/MacOS/$exe")" ] ||
		die "$cli is built for $(lipo -archs "$cli"), the app for $(lipo -archs "$app/Contents/MacOS/$exe")"
	sign "$cli" --options runtime --identifier "$cli_identifier"
	check_signature "$cli" code
	codesign --display --verbose=2 "$cli" 2>&1 | grep -x "Identifier=$cli_identifier" >/dev/null ||
		die "$cli: identifier is not $cli_identifier"
	sign "$app" --options runtime
	check_signature "$app" code
	check_nested "$app"
	endgroup
}

# check_nested: every Mach-O file in the bundle is signed by the authority the bundle is signed
# by (ad-hoc locally), with the hardened runtime.
check_nested() {
	local bundle=$1 want file info n=0
	want="$(codesign --display --verbose=2 "$bundle" 2>&1 | sed -n 's/^Authority=//p' | head -n 1)"
	while IFS= read -r file; do
		file -b "$file" | grep -q '^Mach-O' || continue
		n=$((n + 1))
		info="$(codesign --display --verbose=4 "$file" 2>&1)" || die "$file is not signed"
		printf '%s\n' "$info" | grep -Eq '^CodeDirectory .*flags=0x[0-9a-f]+\([^)]*runtime' ||
			die "$file: signed without the hardened runtime"
		if [ -n "$want" ]; then
			printf '%s\n' "$info" | grep -qxF "Authority=$want" || die "$file is not signed by $want"
		else
			printf '%s\n' "$info" | grep -qx 'Signature=adhoc' || die "$file is not signed ad-hoc like the bundle"
		fi
	done < <(find "$bundle/Contents" -type f -perm -u+x)
	log "$n Mach-O files in $(basename "$bundle"), each signed ${want:-ad-hoc} with the hardened runtime"
}

# build_dmg puts the signed app into the template of the disk image's window (D-70) and signs the
# disk image. The template comes from the job that built the app (hack/macos/dmg-template.sh,
# dmgbuild: code from PyPI, which never runs here); this job only checks it and fills it in with
# Apple's tools. The check: the template is a compressed HFS+ image (UDZO; dmgbuild writes no
# writable one), converted to a writable one here, whose volume is "Cogpit <version>" and holds
# exactly the window's settings (.DS_Store), its background, the volume's icon and the link to
# /Applications, nothing else. Then it is resized to fit the app, the app copied in
# (no Finder flags: an xattr on the bundle fails codesign's strict check; Finder hides an app's
# .app anyway), and the image converted to a compressed one.
build_dmg() {
	group "Disk image $(basename "$dmg")"
	local template="$in/Cogpit-$version-template.dmg" rw="$work/template.dmg" mnt="$work/template-mnt" mb found want
	[ -f "$template" ] || die "no disk image template $template (make dist-dmg-template VERSION=$version)"
	hdiutil imageinfo "$template" | grep -qx 'Format: UDZO' || die "$template is not a compressed disk image (UDZO)"
	retry 3 hdiutil convert -quiet "$template" -format UDRW -o "$rw"
	mb=$(($(du -sm "$app" | cut -f1) * 12 / 10 + 32))
	retry 3 hdiutil resize -quiet -size "${mb}m" "$rw"
	mkdir "$mnt"
	retry 3 hdiutil attach -quiet -readwrite -nobrowse -noautoopen -noverify -mountpoint "$mnt" "$rw"
	[ "$(diskutil info "$mnt" | sed -n 's/^ *Volume Name: *//p')" = "Cogpit $version" ] ||
		{ hdiutil detach -quiet "$mnt"; die "the template's volume is not named Cogpit $version"; }
	found="$(cd "$mnt" && find . -mindepth 1 ! -path './.fseventsd' ! -path './.fseventsd/*' | LC_ALL=C sort | tr '\n' ' ')"
	want="./.DS_Store ./.VolumeIcon.icns ./.background.tiff ./Applications "
	if [ "$found" != "$want" ] || [ "$(readlink "$mnt/Applications")" != /Applications ] ||
		[ -L "$mnt/.DS_Store" ] || [ -L "$mnt/.background.tiff" ] || [ -L "$mnt/.VolumeIcon.icns" ]; then
		hdiutil detach -quiet "$mnt"
		die "the template holds $found, want $want with Applications linking to /Applications"
	fi
	ditto "$app" "$mnt/Cogpit.app"
	rm -rf "$mnt/.fseventsd" # the events log macOS starts on a writable volume
	retry 3 hdiutil detach -quiet "$mnt"
	retry 3 hdiutil convert -quiet "$rw" -format UDZO -imagekey zlib-level=9 -ov -o "$dmg"
	sign "$dmg"
	check_signature "$dmg" image
	endgroup
}

notary() {
	xcrun notarytool "$@" --key "$work/notary.p8" --key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER_ID"
}

# notary_key writes the team API key where notarytool reads it: given as base64, or as the .p8
# file's PEM text itself.
notary_key() {
	case "$APPLE_API_KEY_P8" in
	-----BEGIN*) (umask 077 && printf '%s\n' "$APPLE_API_KEY_P8" >"$work/notary.p8") ;;
	*) (umask 077 && printf '%s' "$APPLE_API_KEY_P8" | base64 --decode >"$work/notary.p8") ;;
	esac
}

# submit hands the disk image, with the app and its nested code (Sparkle, cogpit-cli) inside, to
# Apple's notary service without waiting, and records the submission beside it in
# notary-submission.json: its id, the disk image's name and its SHA-256.
submit() {
	local id sum
	notary_key
	group "Submit to Apple's notary service"
	notary submit "$dmg" --no-wait --output-format json >"$work/submit-dmg.json"
	rm -f "$work/notary.p8"
	id="$(plutil -extract id raw -o - "$work/submit-dmg.json")"
	[[ "$id" =~ ^[0-9a-f-]{36}$ ]] || die "Apple's notary service answered the submission with an unexpected id: $id"
	sum="$(shasum -a 256 "$dmg" | cut -d' ' -f1)"
	printf '{"id":"%s","file":"%s","sha256":"%s"}\n' "$id" "$(basename "$dmg")" "$sum" >"$out/notary-submission.json"
	log "$(basename "$dmg"): submission $id"
	summary "\`$(basename "$dmg")\` submitted to Apple's notary service: submission \`$id\`."
	endgroup
}

# notary_history prints the team's latest submissions as Apple lists them (dates, states, names,
# ids; no secret): how long Apple is taking these days.
notary_history() {
	local i field line
	group "Apple's latest submissions"
	if notary history --output-format json >"$work/history.json" 2>/dev/null; then
		for i in 0 1 2 3 4 5 6 7 8 9; do
			plutil -extract "history.$i" json -o - "$work/history.json" >/dev/null 2>&1 || break
			line=""
			for field in createdDate status name id; do
				line="$line  $(plutil -extract "history.$i.$field" raw -o - "$work/history.json" 2>/dev/null || true)"
			done
			log "${line#  }"
		done
	else
		log "no history from Apple's notary service"
	fi
	endgroup
}

# await <id> <what>: asks Apple for its verdict on a submission every poll_seconds until it is in or
# the deadline has passed, then prints its log (the files Apple checked and any issue); fails unless
# Apple accepted it. A question Apple does not answer is asked again. Called with ||, so errexit is
# off in here: every step checks its own result.
await() {
	local id=$1 what=$2 status said=0 now
	group "Notarization of $what (submission $id)"
	while :; do
		status=""
		if notary info "$id" --output-format json >"$work/info.json" 2>"$work/info.err"; then
			status="$(plutil -extract status raw -o - "$work/info.json" 2>/dev/null)" || status=""
		fi
		[ -n "$status" ] || log "no status for submission $id this time: $(tr '\n' ' ' <"$work/info.err" | cut -c1-200)"
		case "$status" in "" | "In Progress") ;; *) break ;; esac
		now=$(date +%s)
		[ "$now" -lt "$deadline" ] || break
		if [ $((now - said)) -ge 600 ]; then
			log "$(date -u '+%H:%M') UTC: ${status:-no answer}; asking again every ${poll_seconds} s until $(date -u -r "$deadline" '+%H:%M') UTC"
			said=$now
		fi
		sleep "$poll_seconds"
	done
	summary "| $what | \`$id\` | ${status:-no answer} |"
	case "$status" in
	"" | "In Progress")
		endgroup
		err "$what: Apple has not finished notarizing submission $id within $notary_minutes minutes (last answer: ${status:-none}) and keeps working on it. Re-run the failed job \"Notarize and staple (macOS)\": it waits for this same submission and submits nothing new (docs/developer/releasing.md#when-signing-or-notarization-fails)"
		return 1
		;;
	esac
	notary log "$id" || log "no log for submission $id"
	endgroup
	if [ "$status" != Accepted ]; then
		err "$what: Apple's notary service says $status for submission $id; its log is above. This disk image cannot be notarized: fix the cause and release again"
		return 1
	fi
	log "$what: Accepted (submission $id)"
}

# verdict waits for Apple's verdict on the submission recorded in notary-submission.json, made of
# exactly this disk image, staples the disk image's ticket, checks it as Gatekeeper does and removes
# the record, which is no part of the download.
verdict() {
	local record="$out/notary-submission.json" id file sum failed=0
	id="$(plutil -extract id raw -o - "$record")"
	file="$(plutil -extract file raw -o - "$record")"
	sum="$(plutil -extract sha256 raw -o - "$record")"
	[[ "$id" =~ ^[0-9a-f-]{36}$ ]] || die "$record: no submission id"
	[ "$file" = "$(basename "$dmg")" ] || die "$record is about $file, not $(basename "$dmg")"
	[ "$(shasum -a 256 "$dmg" | cut -d' ' -f1)" = "$sum" ] ||
		die "$(basename "$dmg") is not the disk image submission $id was made of (SHA-256 differs)"
	notary_key
	notary_history
	summary "| Notarized | Submission | Status |"
	summary "|---|---|---|"
	deadline=$(($(date +%s) + notary_minutes * 60))
	await "$id" "$file" || failed=1
	rm -f "$work/notary.p8"
	[ "$failed" = 0 ] || exit 1

	group "Staple and check $(basename "$dmg")"
	retry 5 xcrun stapler staple "$dmg"
	xcrun stapler validate "$dmg"
	gatekeeper "$dmg" -t open --context context:primary-signature
	endgroup
	rm -f "$record"
}

# gatekeeper <path> <spctl options...>: Gatekeeper's verdict, which must be a notarized Developer ID.
# A machine with Gatekeeper turned off accepts everything without a verdict; then the notary
# service's Accepted and the stapled ticket stand alone, and the log says so.
gatekeeper() {
	local path=$1 verdict
	shift
	verdict="$(spctl --assess -vv "$@" "$path" 2>&1)" || die "Gatekeeper rejects $path: $verdict"
	log "$verdict"
	if printf '%s\n' "$verdict" | grep -q '^override=security disabled'; then
		warn "Gatekeeper is turned off on this machine ($(spctl --status 2>&1)): no verdict on $(basename "$path")"
		return 0
	fi
	printf '%s\n' "$verdict" | grep -q '^source=Notarized Developer ID$' ||
		die "Gatekeeper accepts $path, but not as notarized: $verdict"
}

# check_dmg [gatekeeper] mounts the disk image and checks what opening it shows: the app and the
# link; with gatekeeper (once notarized) also Gatekeeper's verdict on the app in it.
check_dmg() {
	group "Check the contents of $(basename "$dmg")"
	local mnt="$work/mnt" entries
	mkdir -p "$mnt"
	hdiutil attach -quiet -readonly -nobrowse -noautoopen -mountpoint "$mnt" "$dmg"
	entries="$(find "$mnt" -mindepth 1 -maxdepth 1 -exec basename {} \; | LC_ALL=C sort | tr '\n' ' ')"
	log "contents: $entries"
	# The window's settings, background and the volume's icon (D-70), the link and the app.
	[ "$entries" = ".DS_Store .VolumeIcon.icns .background.tiff Applications Cogpit.app " ] ||
		die "$(basename "$dmg") holds $entries, not the window's files, Applications and Cogpit.app"
	[ "$(readlink "$mnt/Applications")" = /Applications ] || die "Applications in $(basename "$dmg") does not link to /Applications"
	codesign --verify --strict --deep --verbose=2 "$mnt/Cogpit.app"
	codesign --verify --strict --verbose=2 "$mnt/Cogpit.app/Contents/MacOS/$cli_name"
	[ "$(plutil -extract CFBundleShortVersionString raw "$mnt/Cogpit.app/Contents/Info.plist")" = "$version" ] ||
		die "the app in $(basename "$dmg") is not version $version"
	if [ "${1:-}" = gatekeeper ] && [ "$mode" = developer-id ]; then gatekeeper "$mnt/Cogpit.app" -t execute; fi
	hdiutil detach -quiet "$mnt"
	endgroup
}

# workdir makes a fresh work directory, deleted on exit with the keychain and the key files.
workdir() {
	case "$work" in */cogpit-macos-release) ;; *) die "unexpected work directory $work" ;; esac
	cleanup
	trap cleanup EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	mkdir -m 700 "$work"
}

# sign_cmd <version> <in dir> <out dir>: with the Developer ID, the app signed in its signed disk
# image, checked, and submitted to Apple's notary service; ad-hoc, the same without the
# submission; unsigned, the zip as it is. Says the mode as the output mode.
sign_cmd() {
	version=$1
	local in=$2 zips=() archive arch
	out=$3

	mode="$(signing_mode)"
	log "signing: $mode"
	for archive in "$in"/Cogpit_"$version"_darwin_*.zip; do [ ! -f "$archive" ] || zips+=("$archive"); done
	[ "${#zips[@]}" -eq 1 ] || die "expected one Cogpit_${version}_darwin_<arch>.zip in $in (make dist-app), found ${#zips[@]}"
	mkdir -p "$out"
	rm -f "$out/notary-submission.json"

	if [ "$mode" = unsigned ]; then
		cp "${zips[0]}" "$out/"
		output mode unsigned
		summary "macOS download **unsigned**: the signing secrets are not set (a fork, or a run outside the environment \`release\`)."
		return
	fi

	workdir
	if [ "$mode" = developer-id ]; then setup_keychain; fi

	ditto -x -k "${zips[0]}" "$work/app"
	app="$work/app/Cogpit.app"
	[ -d "$app" ] || die "${zips[0]} holds no Cogpit.app"
	[ "$(plutil -extract CFBundleShortVersionString raw "$app/Contents/Info.plist")" = "$version" ] ||
		die "${zips[0]}: the app is not version $version"
	exe="$(plutil -extract CFBundleExecutable raw "$app/Contents/Info.plist")"
	arch="$(lipo -archs "$app/Contents/MacOS/$exe")"
	case "$arch" in *" "*) arch=universal ;; esac
	dmg="$out/Cogpit-$version-macos-$arch.dmg"

	sign_app
	build_dmg
	check_dmg
	if [ "$mode" = developer-id ]; then submit; fi
	output mode "$mode"
	ls -l "$out"
}

# notarize_cmd <version> <dir>: waits for Apple's verdict on the submission sign_cmd made of the
# disk image in <dir>, staples its ticket and checks the disk image and the app in it as Gatekeeper
# does. Needs the team API key only, no certificate. Without a submission (a run without the
# signing secrets) the unsigned download passes on. Says signed and identity as outputs.
notarize_cmd() {
	version=$1
	out=$2
	local dmgs=() file name
	if [ ! -f "$out/notary-submission.json" ]; then
		output signed false
		log "no notarization submission in $out: the download is not signed"
		return
	fi
	for name in APPLE_API_KEY_P8 APPLE_API_KEY_ID APPLE_API_ISSUER_ID APPLE_TEAM_ID; do
		[ -n "${!name:-}" ] || die "the notarization needs $name"
	done
	mode=developer-id
	for file in "$out"/Cogpit-"$version"-macos-*.dmg; do [ ! -f "$file" ] || dmgs+=("$file"); done
	[ "${#dmgs[@]}" -eq 1 ] || die "expected one Cogpit-${version}-macos-<arch>.dmg in $out, found ${#dmgs[@]}"
	dmg=${dmgs[0]}

	workdir
	verdict
	check_dmg gatekeeper
	say_signed
	ls -l "$out"
}

# say_signed says the outcome of a notarized download: the output signed, the identity and the summary.
say_signed() {
	local authority
	authority="$(codesign --display --verbose=2 "$dmg" 2>&1 | sed -n 's/^Authority=\(Developer ID Application: .*\)$/\1/p')"
	output signed true
	output identity "$authority"
	summary ""
	summary "\`$(basename "$dmg")\`, the app and its cogpit-cli signed by **$authority**, notarized; the disk image's ticket stapled."
}

# main <version> <in dir> <out dir>: the whole course in one run (make dist-macos).
main() {
	sign_cmd "$@"
	case "$mode" in
	developer-id)
		verdict
		check_dmg gatekeeper
		say_signed
		;;
	adhoc)
		output signed false
		log "ad-hoc signed, not notarized: a local check of the steps, not a download to publish"
		;;
	*) output signed false ;;
	esac
}

# Sourced (to try a function with stubs in PATH): the functions only.
if [ "${BASH_SOURCE[0]}" != "$0" ]; then return 0; fi
case "${1:-}" in
cleanup)
	cleanup
	exit 0
	;;
sign)
	[ "$#" -eq 4 ] || die "usage: $0 sign <version> <in dir> <out dir>"
	sign_cmd "$2" "$3" "$4"
	;;
notarize)
	[ "$#" -eq 3 ] || die "usage: $0 notarize <version> <dir>"
	notarize_cmd "$2" "$3"
	;;
*)
	[ "$#" -eq 3 ] || die "usage: $0 <version> <in dir> <out dir> | $0 sign <version> <in dir> <out dir> | $0 notarize <version> <dir> | $0 cleanup"
	main "$@"
	;;
esac
