#!/usr/bin/env bash
# The UI's race shards from one build (D-112; docs/developer/releasing.md#the-uis-shards). make
# test-race-ui-build puts internal/ui/fyneui's race test binary with coverage into a folder, with
# each shard's -run pattern, the timeout and, through `libs`, the shared libraries the binary links.
# CI's job Race Tests (UI build) uploads the folder and each Race Tests (UI n/5) runs its shard from
# it here, on a runner without Go, the build cache, make or Fyne's build dependencies; make
# test-race-ui runs both on one machine.
#
#   hack/ci/race-ui.sh libs <dir>                 the libraries <dir>/fyneui.test links, into <dir>/lib
#   hack/ci/race-ui.sh run <dir> <n> <cover dir>  shard <n>, its profile at <cover dir>/race-ui-<n>.out
#
# libs (Linux; nothing elsewhere) copies every library ldd resolves for the binary but glibc's, which
# every runner has: OpenGL, X11 and Wayland's client, which GLFW links, and theirs (about 5 MB). The
# runner image has none of them, and apt would bring them with Mesa's drivers (33 packages, 43 MB)
# for a binary that opens no window. The shards load them through LD_LIBRARY_PATH; the build job and
# the shards run on the same runners (LINUX_RUNNER), so they fit their glibc.
#
# run runs the binary as go test runs a package's tests: in the package's folder (the tests read
# their testdata and the repository's files by relative paths), with -test.paniconexit0, the
# timeout the build wrote (RACE_TIMEOUT) and -test.coverprofile, whose profile has the blocks and
# the covered set of go test -coverprofile's; and as the Makefile's TEST_ENV, without the pod's
# Kubernetes service variables and with KUBECONFIG=/dev/null. SHELL is /bin/bash where none is
# exported, as .github/actions/go-setup sets it for the other jobs: the runner pods export none, and
# Cogpit's local terminal would fall back to zsh, which their image lacks.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)

libs() {
	local dir=$1 lib path
	[ "$(uname -s)" = Linux ] || return 0
	if ldd "$dir/fyneui.test" | grep -q 'not found'; then
		ldd "$dir/fyneui.test" >&2
		echo "race-ui.sh: a library the binary links is missing here" >&2
		exit 1
	fi
	mkdir -p "$dir/lib"
	ldd "$dir/fyneui.test" | awk '$2 == "=>" && $3 ~ /^\// {print $1, $3}' | while read -r lib path; do
		case $lib in
		ld-linux* | libc.so.* | libm.so.* | libmvec.so.* | libpthread.so.* | libdl.so.* | librt.so.* | \
			libresolv.so.* | libutil.so.*) continue ;;
		esac
		cp -L "$path" "$dir/lib/$lib"
	done
	echo "race-ui.sh: the binary's libraries for the shards:" "$(cd "$dir/lib" && echo *)"
}

run() {
	local dir=$1 n=$2 cover=$3 status=0 start shards out
	dir=$(cd "$dir" && pwd)
	if [ ! -s "$dir/shard-$n.run" ]; then
		echo "race-ui.sh: no shard $n in $dir (make test-race-ui-build writes RACE_SHARDS of them)" >&2
		exit 2
	fi
	mkdir -p "$cover"
	cover=$(cd "$cover" && pwd)
	shards=("$dir"/shard-*.run)
	out=$(mktemp)
	chmod +x "$dir/fyneui.test" # an artifact keeps no file modes
	if [ -f "$dir/shard-$n.log" ]; then cat "$dir/shard-$n.log"; fi
	cd "$root/internal/ui/fyneui"
	start=$SECONDS
	env -u KUBERNETES_SERVICE_HOST -u KUBERNETES_SERVICE_PORT KUBECONFIG=/dev/null SHELL="${SHELL:-/bin/bash}" \
		LD_LIBRARY_PATH="$dir/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" \
		"$dir/fyneui.test" -test.paniconexit0 -test.timeout="$(cat "$dir/timeout")" \
		-test.run "$(cat "$dir/shard-$n.run")" -test.coverprofile="$cover/race-ui-$n.out" >"$out" 2>&1 || status=$?
	# As go test with a package's path: the tests' output only when they failed.
	if [ "$status" = 0 ]; then
		echo "ok   internal/ui/fyneui race shard $n of ${#shards[@]}, $((SECONDS - start)) s, $(grep '^coverage:' "$out" | tail -n 1)"
	else
		cat "$out"
		echo "FAIL internal/ui/fyneui race shard $n of ${#shards[@]}, $((SECONDS - start)) s"
	fi
	rm -f "$out"
	return "$status"
}

case "${1:-} $#" in
"libs 2") libs "$2" ;;
"run 4") run "$2" "$3" "$4" ;;
*)
	echo "usage: $0 libs <dir> | run <dir> <shard> <coverage dir>" >&2
	exit 2
	;;
esac
