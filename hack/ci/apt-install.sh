#!/usr/bin/env bash
# apt-install.sh [apt-get option...] <package...>: installs Debian packages on a CI runner (options
# such as --no-install-recommends go to apt-get install as given) without letting a hanging
# mirror take the job with it. On 2026-10-15 `apt-get update` hung 13 minutes on the shared
# self-hosted node until the job's timeout cancelled the job, and with it the run's release. Each
# try here has a deadline, the network timeouts are short, and there are three tries. An install
# the deadline ended leaves dpkg interrupted (run 38808178422: every later try refused with "dpkg
# was interrupted"), so a try after a failed one first finishes dpkg's configuration.
set -euo pipefail

# apt <deadline> <apt-get arguments...>: dpkg without its fsync calls (force-unsafe-io), which a
# runner's disk, shared with the other jobs and thrown away after the job, has no use for.
apt() {
	local deadline=$1
	shift
	sudo timeout "$deadline" apt-get -qq -o Acquire::Retries=3 -o Acquire::http::Timeout=20 \
		-o Acquire::https::Timeout=20 -o DPkg::Lock::Timeout=60 -o Dpkg::Options::=--force-unsafe-io "$@"
}

# repair finishes what an interrupted dpkg left half configured.
repair() {
	sudo timeout 240 dpkg --configure -a >/dev/null 2>&1 || true
}

# try <what> <command...>: the command, up to three times, 10 and 20 seconds apart; messages go to
# stderr, so that a caller's redirect of the command's output keeps them in the log.
try() {
	local what=$1 n
	shift
	for n in 1 2 3; do
		if "$@"; then return 0; fi
		[ "$n" -lt 3 ] || break
		echo "apt-get ${what} failed or hung (try ${n} of 3); again in $((n * 10)) s" >&2
		sleep $((n * 10))
		repair
	done
	echo "::error::apt-get ${what} failed three times" >&2
	return 1
}

[ "$#" -gt 0 ] || { echo "usage: $0 <package...>" >&2; exit 2; }
try update apt 240 update
try install apt 360 install -y "$@" >/dev/null
