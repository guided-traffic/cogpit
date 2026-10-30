# Cogpit — developer targets. CI (.github/workflows/release.yml) runs the same ones, one job each:
# lint with check-nodemo, parity, gosec, vuln, cyclo, test-norace-coverage, test-race-ui-build (its
# shards run hack/ci/race-ui.sh, as test-race-ui does), test-race-rest, kind-up with test-kind,
# kind-drain-up with test-kind-drain, envtest-bin with test-envtest, coverage-report, on macOS
# test-darwin and lint-darwin and gosec-darwin with dist-app, dist-linux with smoke and
# test-linux-package, and on Windows test-windows-unit, test-windows-ui and dist-windows with
# test-windows-package WINDOWS_SMOKE=on, the installed app's smoke test
# (docs/developer/releasing.md#ci).
#
# Go: go.mod declares go 1.27.3 (1.27.2 before 2026-10-21: GO-2026-6809 in crypto/x509); GOTOOLCHAIN=auto
# fetches it when the local Go is older.
# Kubeconfig safety: every target that talks to a cluster uses KIND_KUBECONFIG, a separate file;
# nothing here reads or writes ~/.kube/config. Tests run with KUBECONFIG=/dev/null so that code
# falling back to the default loading rules finds no cluster, and without KUBERNETES_SERVICE_HOST
# and KUBERNETES_SERVICE_PORT, so that inside a pod (CI's self-hosted runners are pods) client-go
# neither reaches the pod's own cluster nor takes the pod's namespace as a context's default.

export GOTOOLCHAIN := auto

# The oldest macOS Cogpit runs on: Go 1.27's own minimum (its linker writes minos 13.0). Without
# it the cgo objects, and so the app, were built for the build machine's macOS (minos 26.0 on
# GitHub's runners, so v0.4.0's app would not start before macOS 26); fyne package writes it into
# the bundle's Info.plist too. CGO_CFLAGS and CGO_LDFLAGS are part of Go's build cache key.
MACOS_MIN       := 13.0
ifeq ($(shell uname -s),Darwin)
export MACOSX_DEPLOYMENT_TARGET := $(MACOS_MIN)
export CGO_CFLAGS  ?= -O2 -g -mmacosx-version-min=$(MACOS_MIN)
export CGO_LDFLAGS ?= -O2 -g -mmacosx-version-min=$(MACOS_MIN)
endif

GO              ?= go
BIN             := $(CURDIR)/bin
# One version of every tool (owner, 2026-10-24): bin/'s tools are built with the Go go.mod names and
# built again when their pinned version or that Go changes, so a local bin/ never holds another
# version than CI builds (a bin/golangci-lint of v2.14.0 built with Go 1.27.2 linted beside CI's
# v2.15.0 with Go 1.27.3). A tool's rule depends on its stamp, $(call tool_stamp,<name>,<version>).
GO_VERSION      := $(shell $(GO) env GOVERSION)
TOOL_STAMPS     := $(BIN)/.stamps
tool_stamp       = $(TOOL_STAMPS)/$(1)@$(2)-$(GO_VERSION)
CACHE           := $(CURDIR)/.cache
KIND_CLUSTER    ?= cogpit
KIND_KUBECONFIG ?= $(CACHE)/kind/kubeconfig
KIND_CONTEXT    := kind-$(KIND_CLUSTER)
KUBECTL         := KUBECONFIG=$(KIND_KUBECONFIG) kubectl --context $(KIND_CONTEXT)
# renovate: datasource=go depName=fyne.io/tools/cmd/fyne
FYNE_TOOLS      ?= v1.7.3
FYNE            := $(BIN)/fyne
# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.15.0
GOLANGCI_LINT   := $(BIN)/golangci-lint
# The version fyne package writes into the bundle's Info.plist (CFBundleShortVersionString;
# sparkle.sh writes CFBundleVersion). The release pipeline sets it to the release's (dist-app), and
# the binaries report it then too (DIST_LDFLAGS). Any other build is a development build: a local
# make app's bundle says 0.0.1 (integers for Sparkle, below every release), its binaries say "dev"
# and the commit Go recorded (internal/buildinfo).
VERSION         ?= 0.0.1
# Linker flags for build; the dist targets set the version (-X …/internal/buildinfo.version).
GO_LDFLAGS      ?=
# Build tags for build; the dist targets set the release's (DIST_TAGS: no demo cluster, D-67).
# Empty: a development build, with the synthetic demo cluster.
GO_TAGS         ?=
# Fyne's thread check (D-103), the one build parameter that chooses speed or safety (owner,
# 2026-10-19). on: Fyne checks on every canvas refresh that it runs on its main goroutine and moves
# a stray call there, with a warning in the app log (safety; the default of build, app, run and
# run-demo). off: Cogpit's build tag nothreadcheck makes the app declare itself migrated to
# fyne.Do (its metadata's Migrations["fyneDo"], internal/ui/fyneui/fyneui.go), which turns the check
# off and keeps fast scrolling at 60 frames per second; a stray call races instead (speed; the
# default of dist-app, so of every release). Not Fyne's own tag migrated_fynedo: that also makes
# Fyne's caches and queues single-threaded, so a stray call could end the app. Either way
# overridable per build: make dist-app VERSION=x.y.z FYNE_THREAD_CHECKS=on is a release with the
# check, make app FYNE_THREAD_CHECKS=off a development build at a release's speed. The tests need
# none: Fyne's test driver has no such check.
FYNE_THREAD_CHECKS ?=
FYNE_NOCHECK_TAG   := nothreadcheck
comma           := ,
empty           :=
space           := $(empty) $(empty)
# threadMode names the mode a build gets: $(1) is the default when FYNE_THREAD_CHECKS is empty.
threadMode       = $(if $(filter-out on off,$(or $(FYNE_THREAD_CHECKS),$(1))),$(error FYNE_THREAD_CHECKS must be on or off, not "$(FYNE_THREAD_CHECKS)"),$(or $(FYNE_THREAD_CHECKS),$(1)))
# BUILD_TAGS is GO_TAGS with the thread check's tag when the build turns it off, comma-separated.
BUILD_TAGS       = $(subst $(space),$(comma),$(strip $(subst $(comma),$(space),$(GO_TAGS)) $(if $(filter off,$(call threadMode,on)),$(FYNE_NOCHECK_TAG))))
APP_ID          := io.guided-traffic.cogpit
# The code-signing identifier of the cogpit-cli in Cogpit.app/Contents/MacOS (D-16); the release's
# Developer ID signature uses the same (hack/macos/release.sh, cli_identifier).
CLI_ID          := $(APP_ID).cli
APP_OS          ?= $(shell $(GO) env GOOS)
# Windows' executables end in .exe: go build -o names the file exactly as given.
EXE              = $(if $(filter windows,$(APP_OS)),.exe)
# Linker flags for the app's binary only, after GO_LDFLAGS: dist-windows makes it a GUI program
# (-H=windowsgui), which opens no console window; cogpit-cli stays a console program.
APP_LDFLAGS     ?=
TEST_ENV        := env -u KUBERNETES_SERVICE_HOST -u KUBERNETES_SERVICE_PORT KUBECONFIG=/dev/null

.PHONY: all build run run-demo test test-unit test-unit-race test-kind test-kind-compile bench lint parity fmt tidy \
        kind-up kind-down kind-seed app icon clean help

all: lint test-unit-race build ## Lint, race tests, build

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

build: ## Build bin/cogpit (cgo, Fyne) and bin/cogpit-cli (pure Go)
	$(GO) build -tags '$(BUILD_TAGS)' -ldflags '$(strip $(GO_LDFLAGS) $(APP_LDFLAGS))' -o $(BIN)/cogpit$(EXE) ./cmd/cogpit
	CGO_ENABLED=0 $(GO) build -tags '$(BUILD_TAGS)' -ldflags '$(GO_LDFLAGS)' -o $(BIN)/cogpit-cli$(EXE) ./cmd/cogpit-cli
	@echo "Fyne's thread check: $(call threadMode,on) (FYNE_THREAD_CHECKS, D-103)"

run: build ## Run Cogpit against the kind cluster (make kind-up first)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	$(BIN)/cogpit$(EXE) --kubeconfig $(KIND_KUBECONFIG) --context $(KIND_CONTEXT)

run-demo: build ## Run Cogpit on the synthetic demo cluster (10,000 pods, 1,000 events/s); development builds only, a release has none (D-67)
	$(BIN)/cogpit$(EXE) --demo

test: test-unit-race ## All tests that need no cluster (race detector on); test-kind needs make kind-up

# The UI's package (internal/ui/fyneui) holds most of the tests' time, and its tests cannot run in
# parallel within one process (one Fyne app at a time), so the local targets run it as hack/shard's
# shards, each a go test run of its own, UI_JOBS at once (D-112): compiled once, then UI_SHARDS shards
# balanced by the tests' measured time (UI_DURATIONS, UI_RACE_DURATIONS; make ui-durations), whose
# results Go's test cache keeps.
# One process for the whole package took 344 s and 15.6 GB without the race detector (an M1 Ultra,
# 2026-10-21; Fyne parses its fonts again for every test's app and go-text keeps every parsed face);
# eight at once take about a fifth of the time. UI_JOBS empty: half the CPUs, and no more shards at
# once than three quarters of the memory holds (about 3 GB a shard, 8 GB under the race detector).
UI_JOBS         ?=
UI_SHARDS       ?= 8
UI_SHARD_RUN     = $(GO) run ./hack/shard -pkg $(UI_PKG) -of $(UI_SHARDS) -exec $(if $(UI_JOBS),-jobs $(UI_JOBS))
NOT_UI_PKGS      = $$($(GO) list ./... | grep -v -x -e 'github.com/guided-traffic/cogpit/internal/ui/fyneui')

.PHONY: test-ui test-ui-race ui-durations

# Both halves run, and the target fails at the end if either failed, as go test ./... reports every
# package.
test-unit: ## Unit tests without the race detector, the UI's package in parallel shards (UI_JOBS)
	$(TEST_ENV) $(GO) test $(NOT_UI_PKGS); s=$$?; $(TEST_ENV) $(UI_SHARD_RUN) -durations $(UI_DURATIONS) && exit $$s

test-ui: ## internal/ui/fyneui's tests without the race detector, UI_SHARDS shards, UI_JOBS at once
	$(TEST_ENV) $(UI_SHARD_RUN) -durations $(UI_DURATIONS)

# The race detector makes the UI's tests (internal/ui/fyneui) take about 18 minutes in one process
# (night run 2026-10-13) and more than 25 on CI's macOS runner; RACE_TIMEOUT bounds a package
# instead. CI splits them into shards (test-race-ui) and runs the rest in one job (test-race-rest).
RACE_TIMEOUT ?= 40m

test-unit-race: test-fork ## Unit tests with the race detector, the terminal widget's included, the UI's package in parallel shards (UI_JOBS)
	$(TEST_ENV) $(GO) test -race -timeout $(RACE_TIMEOUT) $(NOT_UI_PKGS); s=$$?; \
		$(TEST_ENV) $(UI_SHARD_RUN) -race -timeout $(RACE_TIMEOUT) -durations $(UI_RACE_DURATIONS) && exit $$s

test-ui-race: ## internal/ui/fyneui's tests with the race detector, UI_SHARDS shards, UI_JOBS at once
	$(TEST_ENV) $(UI_SHARD_RUN) -race -timeout $(RACE_TIMEOUT) -durations $(UI_RACE_DURATIONS)

# The files that balance the UI's shards (hack/shard -durations): each test's seconds in its shard's
# process, under the race detector with coverage as CI's race shards run it (UI_RACE_DURATIONS) and
# without it (UI_DURATIONS), measured on this machine in UI_SHARDS shards. A test the files lack goes
# to a shard by its name's hash, so they need a refresh only when the shards' times drift apart
# (hack/shard -summary -durations sums each shard): make ui-durations, then commit both files.
ui-durations: ## Measure internal/ui/fyneui's tests with and without the race detector into the files that balance the shards (UI_RACE_DURATIONS, UI_DURATIONS)
	@mkdir -p $(CACHE)/durations
	$(TEST_ENV) $(UI_SHARD_RUN) -race -timeout $(RACE_TIMEOUT) -coverdir $(CACHE)/durations -covername race \
		$(if $(wildcard $(UI_RACE_DURATIONS)),-durations $(UI_RACE_DURATIONS)) -record $(UI_RACE_DURATIONS)
	$(TEST_ENV) $(UI_SHARD_RUN) $(if $(wildcard $(UI_DURATIONS)),-durations $(UI_DURATIONS)) -record $(UI_DURATIONS)

# The patched terminal widget (third_party/fyne-terminal, D-14) is a module of its own, which
# go test ./... from the root leaves out: its tests and fuzz seeds run here, all of them under the
# race detector, the tests that start a local shell included since its races were patched
# (2026-10-15, A116).
FORK_DIR         := third_party/fyne-terminal

.PHONY: test-fork
test-fork: ## Tests and fuzz seeds of the patched terminal widget, a module of its own, race detector on
	cd $(FORK_DIR) && $(TEST_ENV) $(GO) test -race ./...

# The cluster tests: the TestKind* functions of the packages with files tagged kind, race detector on,
# with the coverage of every package they reach (-coverpkg). The drain tests have their own cluster
# and target (test-kind-drain). KIND_RUN narrows the run, e.g. KIND_RUN='^TestKindHelm'.
# The add-on tests (KIND_ADDON_TESTS) need a stack kind-up does not install and skip without it,
# most after connecting to the cluster: test-kind leaves them out, and with them internal/ui/fyneui,
# whose one kind test is the Prometheus drawer's (a race build of the UI's package for a skip).
# KIND_ADDONS=1 runs them after their add-on's target (kind-prometheus; TestKindKubePrometheusStack
# kind-kps, TestKindVictoriaMetrics kind-vm, TestKindAlertmanager kind-alertmanager, TestKindKyverno
# kind-kyverno, TestKindTrivy kind-trivy; TestKindMetricsFallback also COGPIT_KIND_FALLBACK=1), e.g.
# make kind-prometheus && make test-kind KIND_ADDONS=1 KIND_RUN='^TestKindPrometheus'.
KIND_ALL_PKGS    = $(sort $(addprefix ./,$(dir $(shell grep -rl --include='*_test.go' '^//go:build kind' cmd internal))))
KIND_PKGS        = $(if $(KIND_ADDONS),$(KIND_ALL_PKGS),$(filter-out ./internal/ui/fyneui/,$(KIND_ALL_PKGS)))
KIND_RUN        ?= ^TestKind
KIND_ADDON_TESTS := TestKindPrometheus TestKindPrometheusDrawer TestKindNodeExporterByAddress TestKindMetricsFallback \
	TestKindKubePrometheusStack TestKindVictoriaMetrics TestKindAlertmanager TestKindKyverno TestKindTrivy
KIND_SKIP        = ^(TestKindDrain.*$(if $(KIND_ADDONS),,|$(subst $(space),|,$(strip $(KIND_ADDON_TESTS)))))$$

test-kind-compile: ## Compile the cluster tests without a cluster or a run (CI does it while the cluster starts)
	$(GO) test -race -tags kind -covermode=atomic -coverpkg=./... -run '^$$' $(KIND_PKGS)

test-kind: ## Cluster tests against the kind cluster (make kind-up first), with coverage (.cache/coverage/integration.out); KIND_ADDONS=1 with the add-ons' tests
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	@mkdir -p $(COVERAGE_DIR)
	$(TEST_ENV) COGPIT_KIND_KUBECONFIG=$(KIND_KUBECONFIG) COGPIT_KIND_CONTEXT=$(KIND_CONTEXT) \
		$(GO) test -race -timeout $(RACE_TIMEOUT) -tags kind -run '$(KIND_RUN)' -skip '$(KIND_SKIP)' \
		-covermode=atomic -coverpkg=./... -coverprofile=$(COVERAGE_DIR)/integration.out $(KIND_PKGS)

bench: ## Benchmarks: demo churn and build, and the gate criteria S1 (snapshots under churn) and S2 (sort)
	$(TEST_ENV) $(GO) test -run '^$$' -bench . -benchmem ./internal/engine/... ./internal/demo/... ./internal/ui/...

# The test targets CI runs, one job each (docs/developer/releasing.md#ci). Every one writes a
# coverage profile to .cache/coverage; coverage-report merges what is there into the README's badge.
COVERAGE_DIR    := $(CACHE)/coverage
COVERAGE_BADGE  := .github/badges/coverage.json
UNIT_TIMEOUT    ?= 20m

.PHONY: test-norace-coverage test-race-ui test-race-rest test-darwin coverage-report

# On Linux every test runs under the race detector (test-race-ui, test-race-rest), and here, without
# it, what the race detector keeps from running (D-112): internal/ui/logo, which test-race-rest
# leaves out, the UI's render tests, which test-race-ui leaves out (UI_RENDER_TESTS, below), as
# UI_RENDER_SHARDS of hack/shard's shards that run at once (UI_JOBS), and in every package the
# tests that skip themselves, or part of their checks, under it (they read raceEnabled, directly or
# through a helper) or that only the build without the race tag compiles (hack/shard -norace): the
# terminal tests of internal/ui/fyneui (their rendering races Fyne's test driver), the heap
# measurements of internal/cache, the log view's budget. Every package a second time without the
# race detector would take twelve minutes of a runner for 240 of 79,744 statements no other profile
# covers: those are covered here. Coverage mode set: the profiles are merged as
# covered or not (hack/coverage), and atomic counting, which only the race detector needs, made the
# logo's tests four times slower (33 s against 8.4 s, an M1 Ultra, 2026-10-27).
NORACE_LIST      = $(CACHE)/norace-tests.txt
UI_RENDER_SHARDS ?= 4

test-norace-coverage: ## The tests the race detector keeps from running and the UI's render tests, without it, with coverage profiles (.cache/coverage/unit-*.out)
	@mkdir -p $(COVERAGE_DIR)
	$(TEST_ENV) $(GO) test -timeout $(UNIT_TIMEOUT) -covermode=set -coverprofile=$(COVERAGE_DIR)/unit-logo.out ./internal/ui/logo
	$(TEST_ENV) $(GO) run ./hack/shard -pkg $(UI_PKG) -run '$(UI_RENDER_TESTS)' -of $(UI_RENDER_SHARDS) -exec \
		-durations $(UI_DURATIONS) -timeout $(UNIT_TIMEOUT) -coverdir $(COVERAGE_DIR) -covername unit-render \
		$(if $(UI_JOBS),-jobs $(UI_JOBS))
	$(GO) run ./hack/shard -norace -pkg ./... > $(NORACE_LIST)
	@while read -r pkg run; do \
		name=$$(echo "$$pkg" | sed -e 's|^github.com/guided-traffic/cogpit/||' -e 's|/|-|g'); \
		echo "$(GO) test $$pkg -run '$$run'"; \
		$(TEST_ENV) $(GO) test -timeout $(UNIT_TIMEOUT) -covermode=set \
			-coverprofile=$(COVERAGE_DIR)/unit-$$name.out -run "$$run" $$pkg || exit 1; \
	done < $(NORACE_LIST)

# The UI's race tests in RACE_SHARDS shards, CI's race-ui jobs (release.yml's matrix; five, D-112);
# locally any shard runs alone. hack/shard balances the tests of internal/ui/fyneui by their time
# under the race detector (UI_RACE_DURATIONS) and places a test the file lacks by a hash of its
# name, so a new test lands in a shard without a change here or in CI. The render tests
# (UI_RENDER_TESTS) are left out before the split, the others balanced among themselves: each paints
# a scene with Fyne's software painter, pure Go on the test's goroutine, which the race detector
# instruments access by access, so that under it they took two thirds of the shards' time (1,411 of
# 2,111 s for 262 of 1,139 tests, an M1 Ultra, 2026-10-27). A race only a render test reaches goes
# unseen, a risk the behaviour tests keep small: they drive the same widgets under the race
# detector. test-norace-coverage runs the render tests without it, with coverage, and Windows' UI
# job with every other test.
#
# The shards run one build (D-112): test-race-ui-build compiles the test binary with coverage once
# into RACE_UI_DIR, with every shard's -run pattern, the timeout and, on Linux, the libraries the
# binary links (hack/ci/race-ui.sh libs); hack/ci/race-ui.sh run runs a shard from that folder
# without Go or make. CI's build job uploads the folder for the five race-ui jobs; test-race-ui
# builds and runs one shard here.
UI_PKG            := ./internal/ui/fyneui
UI_RENDER_TESTS   := ^TestRender
UI_DURATIONS      := hack/shard/durations/fyneui.txt
UI_RACE_DURATIONS := hack/shard/durations/fyneui-race.txt
RACE_SHARDS       ?= 5
RACE_SHARD        ?= 1
RACE_UI_DIR       ?= $(CACHE)/race-ui

.PHONY: test-race-ui-build

test-race-ui-build: ## Build internal/ui/fyneui's race test binary with coverage once, with each of RACE_SHARDS shards' -run pattern and (Linux) the libraries it links, into RACE_UI_DIR
	@rm -rf $(RACE_UI_DIR) && mkdir -p $(RACE_UI_DIR)
	$(GO) test -c -race -covermode=atomic -o $(RACE_UI_DIR)/fyneui.test $(UI_PKG)
	@for n in $$(seq 1 $(RACE_SHARDS)); do \
		$(GO) run ./hack/shard -pkg $(UI_PKG) -tags race -skip '$(UI_RENDER_TESTS)' -durations $(UI_RACE_DURATIONS) \
			-shard $$n -of $(RACE_SHARDS) > $(RACE_UI_DIR)/shard-$$n.run 2> $(RACE_UI_DIR)/shard-$$n.log; \
		s=$$?; cat $(RACE_UI_DIR)/shard-$$n.log; test $$s = 0 || exit $$s; \
	done
	@echo '$(RACE_TIMEOUT)' > $(RACE_UI_DIR)/timeout
	hack/ci/race-ui.sh libs $(RACE_UI_DIR)

test-race-ui: test-race-ui-build ## Race tests of internal/ui/fyneui but its render tests, shard RACE_SHARD of RACE_SHARDS, with coverage (.cache/coverage/race-ui-<n>.out)
	hack/ci/race-ui.sh run $(RACE_UI_DIR) $(RACE_SHARD) $(COVERAGE_DIR)

# internal/ui/logo is left out: it rasterises the logo in pure Go, on one goroutine, and took nine of
# the job's fifteen minutes under the race detector (2026-10-17); the unit tests run it, coverage
# included.
test-race-rest: test-fork ## Race tests of every package but internal/ui/fyneui and internal/ui/logo, and the terminal widget's, with coverage (.cache/coverage/race-rest.out)
	@mkdir -p $(COVERAGE_DIR)
	$(TEST_ENV) $(GO) test -race -timeout $(RACE_TIMEOUT) -covermode=atomic -coverprofile=$(COVERAGE_DIR)/race-rest.out \
		$$($(GO) list ./... | grep -v -x -e 'github.com/guided-traffic/cogpit/internal/ui/fyneui' -e 'github.com/guided-traffic/cogpit/internal/ui/logo')

# What only macOS can test, for CI's job macOS Tests, race detector on: every test of a
# *_darwin_test.go file (the title bar's AppKit harness, the updater's harness with make sparkle's
# framework, the file panels' harness, the locale's CoreFoundation read), the tests whose
# expectations branch on runtime.GOOS for macOS (DARWIN_GOOS_TESTS: its keys and shortcut glyphs),
# and what macOS runs its own way beneath the window, which the Linux jobs test on Linux only
# (DARWIN_UNIX_FILES): every test of internal/terminal (the local shell's PTY, its kubectl bindings
# for zsh, bash and fish) and of internal/cluster (a credential plugin's process search,
# pluginproc_darwin.go, and the login's run and end above it), and the engine's waits for a login.
# One go test run with one pattern, so that tests run while other packages compile: a test of
# another package listed that has one of these names runs too. CI's job installs fish first.
DARWIN_TEST_FILES = $(shell find cmd internal -name '*_darwin_test.go')
DARWIN_GOOS_TESTS := TestTitleHistoryArrows TestTabShortcuts TestRegistryShortcutsDoNotCollide TestClickModifiers \
	TestRootRowGearOpensClusterSettings TestHelpAndWindowMenus TestTerminalCopyAndPaste
DARWIN_UNIX_FILES  = $(wildcard internal/terminal/*_test.go internal/cluster/*_test.go) internal/engine/loginwait_test.go
DARWIN_TESTS       = $(shell sed -n -E 's/^func (Test[A-Za-z0-9_]*).*/\1/p' $(DARWIN_TEST_FILES) $(DARWIN_UNIX_FILES)) \
	$(DARWIN_GOOS_TESTS)
DARWIN_PKGS        = $(sort $(addprefix ./,$(dir $(DARWIN_TEST_FILES) $(DARWIN_UNIX_FILES))) $(UI_PKG)/ $(UI_PKG)/termview/)

test-darwin: sparkle ## The macOS-only tests, and the terminal's and the logins' beneath the window, race detector on (macOS only); Sparkle for the updater's harness
	@test "$$($(GO) env GOOS)" = darwin || { echo "test-darwin runs on macOS only"; exit 1; }
	$(TEST_ENV) $(GO) test -race -run '^($(subst $(space),|,$(strip $(DARWIN_TESTS))))$$' $(DARWIN_PKGS)

coverage-report: ## Merge the profiles in .cache/coverage into combined.out, the badge (.github/badges/coverage.json) and summary.md
	@ls $(COVERAGE_DIR)/*.out >/dev/null 2>&1 || { echo "no profiles in $(COVERAGE_DIR): run a test target with coverage first"; exit 1; }
	$(GO) run ./hack/coverage -o $(COVERAGE_DIR)/combined/combined.out -badge $(COVERAGE_BADGE) \
		-summary $(COVERAGE_DIR)/combined/summary.md $(COVERAGE_DIR)/*.out
	@cat $(COVERAGE_BADGE)

# The static checks CI runs, one job each, besides lint.
# renovate: datasource=go depName=golang.org/x/vuln/cmd/govulncheck
GOVULNCHECK_VERSION ?= v1.9.0
GOVULNCHECK     := $(BIN)/govulncheck
# renovate: datasource=go depName=github.com/fzipp/gocyclo/cmd/gocyclo
GOCYCLO_VERSION ?= v0.6.0
GOCYCLO         := $(BIN)/gocyclo
# The highest cyclomatic complexity main had when the check came (2026-10-13: 56, the shell's
# applyUIState), so that no function grows past it; the team's other repositories use 15, and 121
# functions are above that. It comes down step by step (owner, 2026-10-13): a change that makes the
# most complex function simpler lowers it to the new maximum (docs/developer/releasing.md#ci).
CYCLO_THRESHOLD ?= 56
# Our code: cmd, internal, hack and the patched terminal widget; tests and generated code ignored.
CYCLO_DIRS      := cmd internal hack third_party
CYCLO_IGNORE    := _test\.go|zz_generated

.PHONY: vuln gosec cyclo cyclo-report

$(GOVULNCHECK): $(call tool_stamp,govulncheck,$(GOVULNCHECK_VERSION))
	GOBIN=$(BIN) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@touch $@

vuln: $(GOVULNCHECK) ## govulncheck: known vulnerabilities in the code paths Cogpit calls
	$(GOVULNCHECK) ./...

# gosec through golangci-lint (the same build of it as lint), the shipped code only, for this
# system's build and Windows' (WINDOWS_LINT_PKGS, at lint); an accepted finding is excluded in
# .golangci.yml or marked #nosec with its reason, a real one is fixed or an H-gap in docs/security.
gosec: $(GOLANGCI_LINT) ## gosec: security findings in the code Cogpit ships (tests excluded), for this system's build and Windows'
	$(GOLANGCI_LINT) run --enable-only gosec --tests=false ./...
	$(if $(WINDOWS_LINT_PKGS),$(WINDOWS_LINT_ENV) $(GOLANGCI_LINT) run --enable-only gosec --tests=false $(WINDOWS_LINT_PKGS))

$(GOCYCLO): $(call tool_stamp,gocyclo,$(GOCYCLO_VERSION))
	GOBIN=$(BIN) $(GO) install github.com/fzipp/gocyclo/cmd/gocyclo@$(GOCYCLO_VERSION)
	@touch $@

cyclo: $(GOCYCLO) ## Cyclomatic complexity: no function above CYCLO_THRESHOLD
	@echo "Functions above complexity $(CYCLO_THRESHOLD):"
	@out="$$($(GOCYCLO) -over $(CYCLO_THRESHOLD) -ignore '$(CYCLO_IGNORE)' $(CYCLO_DIRS))"; \
		if [ -n "$$out" ]; then echo "$$out"; exit 1; fi; echo "none"

cyclo-report: $(GOCYCLO) ## The 20 most complex functions
	@$(GOCYCLO) -top 20 -ignore '$(CYCLO_IGNORE)' $(CYCLO_DIRS)

# golangci-lint built with the project's Go: v2.13.x cannot read Go 1.27.2 export data.
$(GOLANGCI_LINT): $(call tool_stamp,golangci-lint,$(GOLANGCI_LINT_VERSION))
	GOBIN=$(BIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@touch $@

# A tool's stamp, made when its version or the Go changed: the stamps of its other versions go, so
# the tool's rule (which depends on the new stamp) builds it again.
$(TOOL_STAMPS)/%:
	@mkdir -p $(TOOL_STAMPS)
	@rm -f $(TOOL_STAMPS)/$(firstword $(subst @, ,$*))@*
	@touch $@

# A lint or GoSec run sees one system's build: lint and gosec run a second pass for Windows over
# the packages with Windows files, and CI's macOS job runs lint-darwin and gosec-darwin over the
# packages with darwin-only files. A system's packages are found by its files' names,
# *_<system>.go and *_<system>_*.go (docs/developer/conventions.md#style). The Windows pass runs
# without cgo, which internal/ui/fyneui needs for Fyne: its Windows file is vetted on Windows
# (test-windows-unit), not linted (docs/developer/releasing.md#lint-and-gosec-per-system).
goos_pkgs          = $(sort $(addprefix ./,$(dir $(shell find cmd internal hack -name '*_$(1).go' -o -name '*_$(1)_*.go'))))
WINDOWS_LINT_PKGS  = $(filter-out $(UI_PKG)/,$(call goos_pkgs,windows))
DARWIN_LINT_PKGS   = $(call goos_pkgs,darwin)
WINDOWS_LINT_ENV  := GOOS=windows CGO_ENABLED=0

.PHONY: lint-darwin gosec-darwin

lint: $(GOLANGCI_LINT) ## golangci-lint: the team's linters plus depguard layering, for this system's build and Windows'
	$(GOLANGCI_LINT) run ./...
	$(if $(WINDOWS_LINT_PKGS),$(WINDOWS_LINT_ENV) $(GOLANGCI_LINT) run $(WINDOWS_LINT_PKGS))

lint-darwin: $(GOLANGCI_LINT) ## golangci-lint over the packages with darwin-only files (macOS only); CI's macOS Lint and App
	@test "$$($(GO) env GOOS)" = darwin || { echo "lint-darwin runs on macOS only"; exit 1; }
	$(GOLANGCI_LINT) run $(DARWIN_LINT_PKGS)

gosec-darwin: $(GOLANGCI_LINT) ## gosec over the packages with darwin-only files (macOS only, tests excluded); CI's macOS Lint and App
	@test "$$($(GO) env GOOS)" = darwin || { echo "gosec-darwin runs on macOS only"; exit 1; }
	$(GOLANGCI_LINT) run --enable-only gosec --tests=false $(DARWIN_LINT_PKGS)

# The release build's variant (DIST_TAGS, D-67), which no other check builds, for CI's lint job:
# internal/demo is linked into neither binary with the tags and into both without them (so the
# check is not empty), the packages with nodemo files and their tests lint with the tags, and the
# tests written for the variant (TestNoDemo*) run.
NODEMO_PKGS     := ./cmd/... ./internal/engine/...
DEMO_PKG        := github.com/guided-traffic/cogpit/internal/demo

.PHONY: check-nodemo
check-nodemo: $(GOLANGCI_LINT) ## The release build's variant (DIST_TAGS): no internal/demo in either binary, its lint and its tests
	@for b in ./cmd/cogpit ./cmd/cogpit-cli; do \
		$(GO) list -deps $$b | grep -x '$(DEMO_PKG)' >/dev/null || { echo "$$b: no $(DEMO_PKG) in a development build"; exit 1; }; \
		if $(GO) list -deps -tags '$(DIST_TAGS)' $$b | grep -x '$(DEMO_PKG)' >/dev/null; then \
			echo "$$b: $(DEMO_PKG) in the release build"; exit 1; fi; \
	done; echo "$(DEMO_PKG): in both binaries without -tags $(DIST_TAGS), in neither with it"
	$(GOLANGCI_LINT) run --build-tags '$(DIST_TAGS)' $(NODEMO_PKGS)
	$(TEST_ENV) $(GO) test -tags '$(DIST_TAGS)' -run '^TestNoDemo' ./internal/engine/ ./cmd/cogpit-cli/

# The packages only the Windows app links (D-142): Mesa's download and its 7z decoder, imported
# from internal/mesa's Windows file alone, are in cogpit.exe and in no other binary, the apps for
# macOS and Linux and every cogpit-cli; the Windows app must link them, so the check is not empty.
# go list reads each system's build without compiling it; CGO_ENABLED=1 keeps the app's cgo files.
WINDOWS_ONLY_PKGS := github.com/guided-traffic/cogpit/internal/mesa/fetch github.com/bodgit/sevenzip

.PHONY: check-windows-only
check-windows-only: ## The packages only cogpit.exe links (WINDOWS_ONLY_PKGS): in the Windows app, in no other binary
	@win=$$(CGO_ENABLED=1 GOOS=windows $(GO) list -deps ./cmd/cogpit) || exit 1; \
	others=$$(for os in darwin linux; do CGO_ENABLED=1 GOOS=$$os $(GO) list -deps ./cmd/cogpit || exit 1; done; \
		for os in darwin linux windows; do CGO_ENABLED=1 GOOS=$$os $(GO) list -deps ./cmd/cogpit-cli || exit 1; done) || exit 1; \
	for p in $(WINDOWS_ONLY_PKGS); do \
		echo "$$win" | grep -qx "$$p" || { echo "$$p: not in cogpit.exe"; exit 1; }; \
		if echo "$$others" | grep -qx "$$p"; then echo "$$p: in a binary other than cogpit.exe"; exit 1; fi; \
	done; echo "$(WINDOWS_ONLY_PKGS): in cogpit.exe only"

parity: ## Check the parity checklists and the numbers the progress pages report (D-15); PARITY_FLAGS=-rows skips the numbers
	$(GO) run ./hack/parity -check $(PARITY_FLAGS)

# The scorecard (D-15, docs/parity/README.md#the-pages): the budget tests and benchmarks of
# docs/developer/testing.md's performance budgets, run without the race detector, their
# measurements written to docs/parity/scorecard.md (hack/scorecard; the tests record through
# internal/testkit/scorecard). Not in CI: D-15 waits for budgets measured regularly on a fixed
# runner, and CI's shared runners would measure their own load. Run it on a quiet machine and
# commit the page; SCORECARD_FLAGS='-keep <dir>' keeps the measurements, '-from <dir>' renders them.
.PHONY: scorecard
scorecard: ## Measure the performance and API-load budgets and write docs/parity/scorecard.md (D-15; not in CI)
	$(TEST_ENV) $(GO) run ./hack/scorecard -o docs/parity/scorecard.md $(SCORECARD_FLAGS)

fmt: $(GOLANGCI_LINT) ## Format with gofmt and goimports
	$(GOLANGCI_LINT) fmt ./...

tidy: ## go mod tidy
	$(GO) mod tidy

# sigstore's trusted root, which the kubectl download's signature check embeds (D-50): Renovate
# cannot see the file, so this refreshes it through sigstore's TUF repository, verified from the TUF
# root sigstore-go embeds (hack/sigstoreroot); the file's diff is the review. Run it before a
# release, and when a download says the trusted root does not know a certificate or a log.
.PHONY: sigstore-root
sigstore-root: ## Refresh internal/kubectl/sigstore_trusted_root.json from sigstore's TUF repository (D-50)
	$(GO) run ./hack/sigstoreroot -o internal/kubectl/sigstore_trusted_root.json

# The Kubernetes release table the window's version marks read (D-91): every minor's upstream end of
# life from kubernetes/website's data/releases/schedule.yaml and eol.yaml, checked (hack/k8seol) and
# written to internal/model/k8seol/releases.json, which the binaries embed; Cogpit never fetches it
# at run time. The release pipeline runs it before it builds, with K8SEOL_FLAGS=-fallback: a failed
# download or check keeps the committed table and the build goes on. No job commits it: run it by
# hand and commit the file when its diff shows a change.
K8SEOL_FLAGS    ?=

.PHONY: k8s-eol
k8s-eol: ## Refresh internal/model/k8seol/releases.json from upstream's release schedule (D-91); K8SEOL_FLAGS=-fallback keeps the committed table on a failure
	$(GO) run ./hack/k8seol -o internal/model/k8seol/releases.json $(K8SEOL_FLAGS)

# The kind targets need docker and kind; kubectl talks only to KIND_KUBECONFIG.
kind-up: ## Create the kind cluster (own kubeconfig file), or reuse it, seed it from hack/kind, add the Metrics Server
	@command -v kind >/dev/null || { echo "kind is not installed: https://kind.sigs.k8s.io"; exit 1; }
	@mkdir -p $(dir $(KIND_KUBECONFIG))
	@if kind get clusters 2>/dev/null | grep -qx '$(KIND_CLUSTER)'; then \
		echo "kind cluster $(KIND_CLUSTER) exists"; \
		kind export kubeconfig --name $(KIND_CLUSTER) --kubeconfig $(KIND_KUBECONFIG); \
	else \
		kind create cluster --name $(KIND_CLUSTER) --kubeconfig $(KIND_KUBECONFIG) \
			--config hack/kind/cluster.yaml --wait 120s; \
	fi
	$(MAKE) kind-seed kind-metrics-server
	@echo "kubeconfig: $(KIND_KUBECONFIG) (context $(KIND_CONTEXT))"

kind-seed: ## Apply the seed manifests (namespace cogpit-demo) and wait for web and api
	$(KUBECTL) apply -f hack/kind/seed/
	@$(KUBECTL) -n cogpit-demo rollout status deployment/web --timeout=180s && \
		$(KUBECTL) -n cogpit-demo rollout status deployment/api --timeout=180s || \
		echo "the seed is still starting (image pulls); check: $(KUBECTL) get all -n cogpit-demo"

# The Kubernetes Metrics Server, as the clusters Cogpit users have it: the release's manifest, with
# --kubelet-insecure-tls because kind's kubelets serve self-signed certificates.
# renovate: datasource=github-releases depName=kubernetes-sigs/metrics-server
METRICS_SERVER_VERSION ?= v0.9.0

.PHONY: kind-metrics-server
kind-metrics-server: ## Install the Metrics Server into the kind cluster and wait for its API
	$(KUBECTL) apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/$(METRICS_SERVER_VERSION)/components.yaml
	$(KUBECTL) -n kube-system patch deployment metrics-server --type=json \
		-p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
	$(KUBECTL) -n kube-system rollout status deployment/metrics-server --timeout=180s
	$(KUBECTL) wait --for=condition=Available apiservice/v1beta1.metrics.k8s.io --timeout=180s

kind-down: ## Delete the kind cluster and its kubeconfig file
	kind delete cluster --name $(KIND_CLUSTER) --kubeconfig $(KIND_KUBECONFIG)
	rm -f $(KIND_KUBECONFIG)

# A real Prometheus in the kind cluster for the drawer's charts (architecture §9): the
# prometheus-community `prometheus` chart, the server alone (hack/kind/prometheus-values.yaml),
# release prometheus in the namespace monitoring, so its Service is monitoring/prometheus-server.
# Fetched with --repo: no helm repository is added to the user's helm configuration.
PROMETHEUS_CHART_REPO    ?= https://prometheus-community.github.io/helm-charts
PROMETHEUS_CHART_VERSION ?= 29.36.1
HELM_KIND                := helm --kubeconfig $(KIND_KUBECONFIG) --kube-context $(KIND_CONTEXT)

.PHONY: kind-prometheus kind-prometheus-down

kind-prometheus: ## Install or upgrade a real Prometheus in the kind cluster (namespace monitoring)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	@command -v helm >/dev/null || { echo "helm is not installed: https://helm.sh"; exit 1; }
	$(HELM_KIND) upgrade --install prometheus prometheus --repo $(PROMETHEUS_CHART_REPO) \
		--version $(PROMETHEUS_CHART_VERSION) --namespace monitoring --create-namespace \
		--values hack/kind/prometheus-values.yaml --wait --timeout 5m
	@echo "Prometheus: monitoring/prometheus-server (scrapes every minute; charts fill after 2-3 minutes)"

kind-prometheus-down: ## Uninstall the kind cluster's Prometheus and its namespace
	$(HELM_KIND) uninstall prometheus --namespace monitoring --ignore-not-found
	$(KUBECTL) delete namespace monitoring --ignore-not-found

# A real kube-prometheus-stack for one check (TestKindKubePrometheusStack), release kps in its own
# namespace cogpit-kps, the operator and one Prometheus only (hack/kind/kube-prometheus-stack-values.yaml).
# Detection prefers it to the `prometheus` chart the other kind tests expect: uninstall it after.
# It adds the operator's CRDs and a kubelet Service in kube-system, which kind-kps-down removes.
KPS_CHART_VERSION ?= 92.3.0
KPS_CRDS := alertmanagerconfigs alertmanagers podmonitors probes prometheusagents prometheuses \
	prometheusrules scrapeconfigs servicemonitors thanosrulers

.PHONY: kind-kps kind-kps-down

kind-kps: ## Install a minimal kube-prometheus-stack into the kind cluster (namespace cogpit-kps)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	$(HELM_KIND) upgrade --install kps kube-prometheus-stack --repo $(PROMETHEUS_CHART_REPO) \
		--version $(KPS_CHART_VERSION) --namespace cogpit-kps --create-namespace \
		--values hack/kind/kube-prometheus-stack-values.yaml --wait --timeout 10m

kind-kps-down: ## Uninstall it, its CRDs, its kubelet Service and its namespace
	$(HELM_KIND) uninstall kps --namespace cogpit-kps --ignore-not-found --wait
	$(KUBECTL) -n kube-system delete service kps-kube-prometheus-stack-kubelet --ignore-not-found
	$(KUBECTL) delete crd --ignore-not-found $(addsuffix .monitoring.coreos.com,$(KPS_CRDS))
	$(KUBECTL) delete namespace cogpit-kps --ignore-not-found

# VictoriaMetrics for one check (TestKindVictoriaMetrics, D-71): single-node and a small cluster
# (vmselect under /select/0/prometheus) in the namespace cogpit-vm, both fed from the kind
# cluster's Prometheus (make kind-prometheus first) through its /federate endpoint
# (hack/kind/victoriametrics). Detection prefers that Prometheus: the test names each.
.PHONY: kind-vm kind-vm-down

kind-vm: ## Install VictoriaMetrics (single-node and a cluster) into the kind cluster (namespace cogpit-vm)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	$(KUBECTL) apply -f hack/kind/victoriametrics/
	$(KUBECTL) -n cogpit-vm rollout status deployment --timeout 5m

kind-vm-down: ## Delete the kind cluster's VictoriaMetrics and its namespace
	$(KUBECTL) delete namespace cogpit-vm --ignore-not-found

# Trivy Operator for the image ratings' check (TestKindTrivy, D-78): the aquasecurity chart, release
# trivy in the namespace cogpit-trivy, scanning only the namespaces cogpit-trivy-a and cogpit-trivy-b
# with the workloads of hack/kind/trivy, one scan job at a time (hack/kind/trivy-operator-values.yaml).
# The Job made from the suspended CronJob is named as the CronJob controller names its Jobs. The
# chart's CRDs (aquasecurity.github.io) stay after an uninstall: kind-trivy-down deletes them.
TRIVY_CHART_REPO    ?= https://aquasecurity.github.io/helm-charts/
TRIVY_CHART_VERSION ?= 0.37.0

.PHONY: kind-trivy kind-trivy-down

kind-trivy: ## Install Trivy Operator and its test workloads into the kind cluster (namespaces cogpit-trivy*)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	$(HELM_KIND) upgrade --install trivy trivy-operator --repo $(TRIVY_CHART_REPO) \
		--version $(TRIVY_CHART_VERSION) --namespace cogpit-trivy --create-namespace \
		--values hack/kind/trivy-operator-values.yaml --wait --timeout 10m
	$(KUBECTL) apply -f hack/kind/trivy/
	$(KUBECTL) -n cogpit-trivy-a create job nightly-29000000 --from=cronjob/nightly --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@echo "Trivy Operator scans cogpit-trivy-a and cogpit-trivy-b one image at a time: the reports follow within minutes"

kind-trivy-down: ## Uninstall Trivy Operator, its CRDs and the namespaces cogpit-trivy*
	$(KUBECTL) delete namespace cogpit-trivy-a cogpit-trivy-b --ignore-not-found
	$(HELM_KIND) uninstall trivy --namespace cogpit-trivy --ignore-not-found --wait
	for crd in $$($(KUBECTL) get crd -o name | grep '\.aquasecurity\.github\.io$$'); do \
		$(KUBECTL) delete --ignore-not-found $$crd; done
	$(KUBECTL) delete namespace cogpit-trivy --ignore-not-found

# Alertmanager for one check (TestKindAlertmanager, D-77): one Alertmanager in the namespace
# cogpit-alerts, its Service named as the alertmanager chart names its own, so that Cogpit finds it
# (hack/kind/alertmanager). The test posts its own alerts and resolves them at its end.
.PHONY: kind-alertmanager kind-alertmanager-down

kind-alertmanager: ## Install an Alertmanager into the kind cluster (namespace cogpit-alerts)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	$(KUBECTL) apply -f hack/kind/alertmanager/
	$(KUBECTL) -n cogpit-alerts rollout status deployment --timeout 5m

kind-alertmanager-down: ## Delete the kind cluster's Alertmanager and its namespace
	$(KUBECTL) delete namespace cogpit-alerts --ignore-not-found

# Kyverno for one check (TestKindKyverno, D-79): the admission and reports controllers in the
# namespace cogpit-kyverno, their webhooks limited to the namespaces labelled cogpit-kyverno=true
# (hack/kind/kyverno/values.yaml), three ClusterPolicies matching only cogpit-kyverno-* namespaces
# (two Audit, one Enforce with failurePolicy Ignore) and a ValidatingPolicy in Audit matching only
# the labelled namespaces, objects made before and after them in
# cogpit-kyverno-a. A test setup: Cogpit installs nothing. kind-kyverno-down removes all of it,
# Kyverno's CRDs and webhook configurations included.
KYVERNO_CHART_REPO    ?= https://kyverno.github.io/kyverno/
KYVERNO_CHART_VERSION ?= 3.9.1
KYVERNO_CRD_GROUPS_RE := kyverno\.io|wgpolicyk8s\.io

.PHONY: kind-kyverno kind-kyverno-down

kind-kyverno: ## Install Kyverno, test policies and violating objects into the kind cluster (namespaces cogpit-kyverno, cogpit-kyverno-a)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	$(HELM_KIND) upgrade --install kyverno kyverno --repo $(KYVERNO_CHART_REPO) \
		--version $(KYVERNO_CHART_VERSION) --namespace cogpit-kyverno --create-namespace \
		--values hack/kind/kyverno/values.yaml --wait --timeout 10m
	$(KUBECTL) apply -f hack/kind/kyverno/before.yaml
	$(KUBECTL) apply -f hack/kind/kyverno/policies.yaml
	$(KUBECTL) wait --for=condition=Ready clusterpolicy -l cogpit-kyverno=true --timeout 2m
	$(KUBECTL) apply -f hack/kind/kyverno/after.yaml

kind-kyverno-down: ## Remove the kind cluster's Kyverno, its policies, CRDs, webhooks and namespaces
	-$(KUBECTL) delete clusterpolicy,validatingpolicy -l cogpit-kyverno=true --ignore-not-found
	$(KUBECTL) delete namespace cogpit-kyverno-a --ignore-not-found
	$(HELM_KIND) uninstall kyverno --namespace cogpit-kyverno --ignore-not-found --wait
	$(KUBECTL) delete validatingwebhookconfiguration,mutatingwebhookconfiguration -l webhook.kyverno.io/managed-by=kyverno --ignore-not-found
	@# The chart's uninstall removes its CRDs; any it left (a release installed otherwise) go here.
	crds=$$($(KUBECTL) get crd -o name | grep -E '\.($(KYVERNO_CRD_GROUPS_RE))$$'); \
		if [ -n "$$crds" ]; then $(KUBECTL) delete --ignore-not-found $$crds; fi
	$(KUBECTL) delete namespace cogpit-kyverno --ignore-not-found

# The drain cluster: one control plane and two workers, for the tests that cordon and drain a
# node, which must never run against the shared single-node cluster above.
KIND_DRAIN_CLUSTER    ?= cogpit-drain
KIND_DRAIN_KUBECONFIG ?= $(CACHE)/kind-drain/kubeconfig
KIND_DRAIN_CONTEXT    := kind-$(KIND_DRAIN_CLUSTER)

.PHONY: kind-drain-up test-kind-drain test-kind-drain-compile kind-drain-down

kind-drain-up: ## Create the multi-node drain cluster (own kubeconfig file), or reuse it
	@command -v kind >/dev/null || { echo "kind is not installed: https://kind.sigs.k8s.io"; exit 1; }
	@mkdir -p $(dir $(KIND_DRAIN_KUBECONFIG))
	@if kind get clusters 2>/dev/null | grep -qx '$(KIND_DRAIN_CLUSTER)'; then \
		echo "kind cluster $(KIND_DRAIN_CLUSTER) exists"; \
		kind export kubeconfig --name $(KIND_DRAIN_CLUSTER) --kubeconfig $(KIND_DRAIN_KUBECONFIG); \
	else \
		kind create cluster --name $(KIND_DRAIN_CLUSTER) --kubeconfig $(KIND_DRAIN_KUBECONFIG) \
			--config hack/kind/drain-cluster.yaml --wait 120s; \
	fi
	@echo "kubeconfig: $(KIND_DRAIN_KUBECONFIG) (context $(KIND_DRAIN_CONTEXT))"

test-kind-drain: ## Cordon, uncordon and drain tests against the drain cluster (make kind-drain-up first), with coverage
	@test -s $(KIND_DRAIN_KUBECONFIG) || { echo "no kubeconfig at $(KIND_DRAIN_KUBECONFIG): run make kind-drain-up first"; exit 1; }
	@mkdir -p $(COVERAGE_DIR)
	$(TEST_ENV) COGPIT_KIND_DRAIN_KUBECONFIG=$(KIND_DRAIN_KUBECONFIG) COGPIT_KIND_DRAIN_CONTEXT=$(KIND_DRAIN_CONTEXT) \
		$(GO) test -race -tags kind -count=1 -v -timeout 15m -run 'TestKindDrain' \
		-covermode=atomic -coverpkg=./... -coverprofile=$(COVERAGE_DIR)/integration-drain.out ./internal/testkit/integration/

# The tests against a real cluster someone names (real_kind_test.go): never in CI, only with
# REAL_KUBECONFIG and REAL_CONTEXT, in a HOME of their own. REAL_RUN narrows them; the node, Helm and
# Flux tests also need their COGPIT_REAL_* variables (the file's comment lists them).
REAL_RUN  ?= ^TestReal
REAL_HOME ?= $(CURDIR)/.cache/real-home

.PHONY: test-real
test-real: ## Tests against a real cluster (REAL_KUBECONFIG=<file> REAL_CONTEXT=<context>), never in CI
	@test -n "$(REAL_KUBECONFIG)" -a -n "$(REAL_CONTEXT)" || { echo "set REAL_KUBECONFIG and REAL_CONTEXT"; exit 1; }
	@mkdir -p $(REAL_HOME)
	$(TEST_ENV) HOME=$(REAL_HOME) COGPIT_REAL_KUBECONFIG=$(REAL_KUBECONFIG) COGPIT_REAL_CONTEXT='$(REAL_CONTEXT)' \
		$(GO) test -tags kind -count=1 -v -timeout 60m -run '$(REAL_RUN)' ./internal/testkit/integration/

test-kind-drain-compile: ## Compile the drain tests without a cluster or a run
	$(GO) test -race -tags kind -covermode=atomic -coverpkg=./... -run '^$$' ./internal/testkit/integration/

kind-drain-down: ## Delete the drain cluster and its kubeconfig file
	kind delete cluster --name $(KIND_DRAIN_CLUSTER) --kubeconfig $(KIND_DRAIN_KUBECONFIG)
	rm -f $(KIND_DRAIN_KUBECONFIG)

# envtest (D-64; docs/developer/testing.md): a real kube-apiserver and etcd, no Docker and no
# kubelet, for the tests tagged envtest (internal/testkit/envtest starts them through
# controller-runtime's envtest). The binaries are envtest's release archive of Kubernetes
# ENVTEST_K8S_VERSION from controller-tools' release store, the one setup-envtest downloads, pinned
# by version and the SHA-512 its index publishes per platform, kept in .cache/envtest/<version>
# (hack/envtest/fetch.sh). Both SHA-512 were pinned 2026-10-16 from the index
# (https://raw.githubusercontent.com/kubernetes-sigs/controller-tools/HEAD/envtest-releases.yaml),
# agree with the downloads, whose SHA-256 agree with GitHub's digests of the assets. A new version
# is pinned by hand, both SHA-512 from that index. 1.37.0: kind's Kubernetes in CI; client-go v0.37.1.
ENVTEST_K8S_VERSION := 1.37.0
ENVTEST_SHA512_Darwin_arm64   := fb38cfacdd71b5e97a4d4cceac861af5f55069cf783f0e49cf181bfc32eb3e557c2091a534dc5d38f1b92c5ba142bc1979f215a5385b3630ea6a661be6fa161b
ENVTEST_PLATFORM_Darwin_arm64 := darwin-arm64
ENVTEST_SHA512_Linux_x86_64   := 1d1c453633b72c161a5d5a886cde7ac850be1a2ac796a9e1d4ffacacc64868295bdd2d57aa66cd0c158f5ce510f5dfe3fbc61ac21bd3dcfb875bd70658aa663a
ENVTEST_PLATFORM_Linux_x86_64 := linux-amd64
ENVTEST_HOST     = $(shell uname -s)_$(shell uname -m)
ENVTEST_DIR     := $(CACHE)/envtest/$(ENVTEST_K8S_VERSION)
ENVTEST_PKGS     = $(sort $(addprefix ./,$(dir $(shell grep -rl --include='*_test.go' '^//go:build envtest' cmd internal))))
ENVTEST_RUN     ?= ^TestEnvtest

.PHONY: envtest-bin envtest-key test-envtest test-envtest-compile

envtest-bin: $(ENVTEST_DIR)/.verified ## Download the pinned envtest binaries (etcd, kube-apiserver) into .cache/envtest (checksum verified)

$(ENVTEST_DIR)/.verified:
	hack/envtest/fetch.sh '$(ENVTEST_K8S_VERSION)' '$(ENVTEST_PLATFORM_$(ENVTEST_HOST))' \
		'$(ENVTEST_SHA512_$(ENVTEST_HOST))' '$(ENVTEST_DIR)'

envtest-key: ## The CI cache key of the envtest binaries: version, platform and pinned SHA-512
	@echo 'envtest-$(ENVTEST_K8S_VERSION)-$(ENVTEST_PLATFORM_$(ENVTEST_HOST))-$(ENVTEST_SHA512_$(ENVTEST_HOST))'

test-envtest-compile: ## Compile the envtest tests without binaries or a run (CI does it while they download)
	$(GO) test -race -tags envtest -covermode=atomic -coverpkg=./... -run '^$$' $(ENVTEST_PKGS)

test-envtest: envtest-bin ## The TestEnvtest* tests against envtest's kube-apiserver and etcd, race detector on, with coverage (.cache/coverage/envtest.out)
	@mkdir -p $(COVERAGE_DIR)
	$(TEST_ENV) COGPIT_ENVTEST_ASSETS=$(ENVTEST_DIR) $(GO) test -race -tags envtest -timeout 10m -run '$(ENVTEST_RUN)' \
		-covermode=atomic -coverpkg=./... -coverprofile=$(COVERAGE_DIR)/envtest.out $(ENVTEST_PKGS)

$(FYNE): $(call tool_stamp,fyne,$(FYNE_TOOLS))
	GOBIN=$(BIN) $(GO) install fyne.io/tools/cmd/fyne@$(FYNE_TOOLS)
	@touch $@

# Sparkle 2, the macOS app's updater (D-4; docs/developer/releasing.md#updates): the release archive
# pinned by version and SHA-256 (GitHub's digest of the asset), kept in .cache/sparkle/<version>
# (hack/macos/sparkle.sh). make app bundles its framework; the release pipeline signs the update
# with its sign_update. Renovate updates both lines: its github-release-attachments datasource finds
# the asset the SHA-256 pins in this release and hashes the same asset of the new one. The three
# lines stay together, the version a release tag; the update never merges on its own
# (.github/renovate.json).
# renovate: datasource=github-release-attachments depName=sparkle-project/Sparkle
SPARKLE_VERSION := 2.10.1
SPARKLE_SHA256  := 1a856d2a9edb8e628de88121d7be04cf0e2167f8641650b78fca00b14244cf27
SPARKLE_DIR     := $(CACHE)/sparkle/$(SPARKLE_VERSION)
# The appcast every installed copy keeps (owner, 2026-10-14) and the public half of the update key
# (base64, as Sparkle's generate_keys prints it). Empty by default: such a build has no
# SUPublicEDKey and does not update itself; the release pipeline passes the repository variable.
SPARKLE_FEED_URL      := https://guided-traffic.github.io/cogpit/appcast.xml
SPARKLE_PUBLIC_ED_KEY ?=

.PHONY: sparkle
sparkle: $(SPARKLE_DIR)/.verified ## Download the pinned Sparkle release into .cache/sparkle (checksum verified)

$(SPARKLE_DIR)/.verified:
	hack/macos/sparkle.sh fetch '$(SPARKLE_VERSION)' '$(SPARKLE_SHA256)' '$(SPARKLE_DIR)'

# fyne package runs in bin/, so the package lands there: bin/Cogpit.app on macOS, bin/Cogpit.tar.xz
# on Linux, bin/Cogpit.exe on Windows. It wraps the binary `make build` produced (--executable):
# letting fyne build it would inject a file importing fyne.io/fyne/v2/app, whose driver modules are
# only in go.sum once cmd/cogpit uses Fyne. A packaged binary therefore carries no injected Fyne
# metadata, so cmd/cogpit calls app.NewWithID("io.guided-traffic.cogpit") rather than app.New.
# Unsigned: macOS asks once on first open.
# Sparkle on macOS only; uname, not go env: a prerequisite list is expanded whenever make reads this
# file, and the appcast's signing job runs make without Go.
#
# macOS: the bundle carries cogpit-cli (owner, 2026-10-14, D-16): the bin/cogpit-cli `make build`
# made beside the app's binary, with the same GO_LDFLAGS (dist-app: the release's version), so the
# bundle's architecture; Go's linker writes minos 13.0 into it as into the app. It goes into
# Contents/MacOS, where Apple's "Placing Content in a Bundle" puts helper tools, and is signed
# before the bundle (ad-hoc here, with its identifier and the hardened runtime; the release signs it
# again with the Developer ID, hack/macos/release.sh).
APP_MACOS        = $(BIN)/Cogpit.app/Contents/MacOS
# What macOS says when Cogpit reads a folder its privacy settings protect (D-72): a kubeconfig
# source, a CA file or a folder the user chose there in an earlier run, or a terminal or credential
# plugin Cogpit started there (macOS asks under the app's name). A file picked in the system's
# panel needs no prompt. One sentence per place, the second the same everywhere.
TCC_WHAT         = Cogpit reads the kubeconfigs, folders and certificate files you chose
TCC_TOO          = Programs Cogpit starts for you, such as a terminal or a login plugin, ask through Cogpit too.
APP_PLIST        = $(BIN)/Cogpit.app/Contents/Info.plist
app: build $(FYNE) $(if $(filter Darwin,$(shell uname -s)),sparkle) ## Package Cogpit for this OS with fyne package (macOS: bin/Cogpit.app, with Sparkle and cogpit-cli)
	rm -rf $(BIN)/Cogpit.app
	cd $(BIN) && $(FYNE) package --os $(APP_OS) --src $(CURDIR)/cmd/cogpit --executable $(BIN)/cogpit --release \
		--name Cogpit --app-id $(APP_ID) --app-version $(VERSION) --icon $(CURDIR)/packaging/icon.png --use-raw-icon
	@# macOS: fyne package leaves the category empty (only fyne release sets one); the ad-hoc
	@# signature seals the bundle so codesign --verify passes. Then check what Finder needs.
	@if [ "$(APP_OS)" = darwin ]; then \
		plutil -replace LSApplicationCategoryType -string public.app-category.developer-tools $(BIN)/Cogpit.app/Contents/Info.plist && \
		plutil -replace LSMinimumSystemVersion -string $(MACOS_MIN) $(BIN)/Cogpit.app/Contents/Info.plist && \
		plutil -replace NSDesktopFolderUsageDescription -string '$(TCC_WHAT) on your Desktop. $(TCC_TOO)' $(APP_PLIST) && \
		plutil -replace NSDocumentsFolderUsageDescription -string '$(TCC_WHAT) in your Documents folder. $(TCC_TOO)' $(APP_PLIST) && \
		plutil -replace NSDownloadsFolderUsageDescription -string '$(TCC_WHAT) in your Downloads folder. $(TCC_TOO)' $(APP_PLIST) && \
		plutil -replace NSRemovableVolumesUsageDescription -string '$(TCC_WHAT) on a removable volume. $(TCC_TOO)' $(APP_PLIST) && \
		plutil -replace NSNetworkVolumesUsageDescription -string '$(TCC_WHAT) on a network volume. $(TCC_TOO)' $(APP_PLIST) && \
		otool -l $(APP_MACOS)/cogpit | grep -A3 LC_BUILD_VERSION | grep -q "minos $(MACOS_MIN)" && \
		install -m 0755 $(BIN)/cogpit-cli $(APP_MACOS)/cogpit-cli && \
		test "$$(lipo -archs $(APP_MACOS)/cogpit-cli)" = "$$(lipo -archs $(APP_MACOS)/cogpit)" && \
		otool -l $(APP_MACOS)/cogpit-cli | grep -A3 LC_BUILD_VERSION | grep -q "minos $(MACOS_MIN)" && \
		plutil -replace CFBundleURLTypes -json '[{"CFBundleURLName":"$(APP_ID)","CFBundleURLSchemes":["cogpit"]}]' $(BIN)/Cogpit.app/Contents/Info.plist && \
		hack/macos/sparkle.sh bundle $(BIN)/Cogpit.app '$(SPARKLE_DIR)' '$(SPARKLE_FEED_URL)' '$(SPARKLE_PUBLIC_ED_KEY)' '$(VERSION)' && \
		codesign --force --sign - --options runtime --identifier $(CLI_ID) $(APP_MACOS)/cogpit-cli && \
		codesign --force --sign - $(BIN)/Cogpit.app && \
		codesign --verify --strict --deep $(BIN)/Cogpit.app && \
		test -x $(APP_MACOS)/cogpit && test -x $(APP_MACOS)/cogpit-cli && \
		test "$$(plutil -extract CFBundleIdentifier raw $(BIN)/Cogpit.app/Contents/Info.plist)" = "$(APP_ID)" && \
		echo "built $(BIN)/Cogpit.app with $(APP_MACOS)/cogpit-cli"; \
	fi

icon: ## Redraw packaging/icon.png from hack/icon
	$(GO) run ./hack/icon -o packaging/icon.png

# The README's pictures and the repository's social preview (docs/images, A110–A114, D-24;
# docs/developer/testing.md): the showcase renders of internal/ui/fyneui, each in the dark and the
# light theme (the social preview in the dark one), then oxipng over them, lossless. oxipng
# is the release binary of shssoichiro/oxipng pinned by version and the SHA-256 of its archive for
# the two platforms it runs on here, kept in .cache/oxipng/<version> (hack/showcase/oxipng.sh);
# both SHA-256 were pinned on first download, 2026-10-15, and agree with GitHub's digests of the
# assets. A new version is pinned by hand, with both SHA-256 (gh api
# repos/shssoichiro/oxipng/releases/tags/v<version> --jq '.assets[] | .name + " " + .digest').
# OXIPNG_FLAGS: --zopfli takes about two minutes of one core per picture (2026-10-15: 2.2 % smaller
# than without); drop it for a quick look. Never --alpha, which alters pixels.
OXIPNG_VERSION  := 10.2.1
OXIPNG_SHA256_Darwin_arm64 := 7039fcfc78e8aa1ed2b57d848057a0296f082e92b3e1807ac65402d10d926764
OXIPNG_TARGET_Darwin_arm64 := aarch64-apple-darwin
OXIPNG_SHA256_Linux_x86_64 := 46e3c4beb9aae57290ad809dd3374b07153579d3322a3778c53900633618b7c6
OXIPNG_TARGET_Linux_x86_64 := x86_64-unknown-linux-gnu
OXIPNG_PLATFORM  = $(shell uname -s)_$(shell uname -m)
OXIPNG_DIR      := $(CACHE)/oxipng/$(OXIPNG_VERSION)
OXIPNG_FLAGS    ?= -o max --fast --zopfli --strip safe
SHOWCASE_DIR    := docs/images

.PHONY: oxipng showcase

oxipng: $(OXIPNG_DIR)/.verified ## Download the pinned oxipng release into .cache/oxipng (checksum verified)

$(OXIPNG_DIR)/.verified:
	hack/showcase/oxipng.sh fetch '$(OXIPNG_VERSION)' '$(OXIPNG_TARGET_$(OXIPNG_PLATFORM))' \
		'$(OXIPNG_SHA256_$(OXIPNG_PLATFORM))' '$(OXIPNG_DIR)'

showcase: oxipng ## Render the README's pictures into docs/images, dark and light, and optimise them losslessly with oxipng
	$(TEST_ENV) $(GO) test -count=1 -run '^TestShowcase' ./internal/ui/fyneui -showcase
	hack/showcase/oxipng.sh optimise '$(OXIPNG_DIR)/oxipng' '$(OXIPNG_FLAGS)' $(SHOWCASE_DIR)/*.png

# What a release holds (docs/developer/releasing.md#the-release-pipeline): the complete product
# only, today the macOS app in its disk image with cogpit-cli inside it, and SHA256SUMS (owner,
# 2026-10-14, D-16: no cogpit-cli archives any more, for any platform). The release pipeline
# (.github/workflows/build.yml) runs these with VERSION set to the release's, and they run the same
# here. The app needs macOS (cgo with AppKit). dist-app builds it unsigned; dist-macos signs and
# notarizes it (hack/macos/release.sh) when the Developer ID secrets are in its environment, which
# only the pipeline's signing job has; MACOS_SIGN=adhoc runs its steps here, signed ad-hoc and not
# notarized (docs/security/updates.md). cogpit-cli for another platform, for a local need:
# CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build ./cmd/cogpit-cli.
DIST            := $(CURDIR)/dist
# The one variable the version is set in, for both binaries (internal/buildinfo; its
# TestMakefileSetsTheVersion checks that this names it: the linker ignores a -X for a missing symbol).
DIST_LDFLAGS     = -s -w -X github.com/guided-traffic/cogpit/internal/buildinfo.version=$(VERSION)
# The build tags of a release (owner, 2026-10-16, D-67): nodemo leaves the synthetic demo cluster
# out of both binaries (package internal/demo, the flags --demo, --demo-pods and --demo-events, the
# context "demo"); development builds, make run-demo, the tests and make showcase keep it. The tag
# selects internal/engine's connector_nodemo.go and cmd/cogpit's nodemo.go; internal/engine's
# TestReleaseBuildsLeaveTheDemoOut checks that dist-app passes these and that they name that tag,
# dist-app that both binaries recorded them, and CI's lint job the variant itself (check-nodemo).
DIST_TAGS        = nodemo
MACOS_SIGN      ?=

# checkDistBinaries fails unless each binary of $(1) recorded the release's build tags: DIST_TAGS,
# and Fyne's thread check's tag exactly when the release turns the check off (D-103). dist-app and
# dist-linux run it on what they built.
define checkDistBinaries
	@for b in $(1); do \
		tags=",$$($(GO) version -m $$b | sed -n -E 's/^[[:space:]]+build[[:space:]]+-tags=//p'),"; \
		case "$$tags" in *,$(DIST_TAGS),*) ;; *) echo "$$b: not built with -tags=$(DIST_TAGS), so it would ship the demo cluster"; exit 1;; esac; \
		case "$(call threadMode,off):$$tags" in off:*,$(FYNE_NOCHECK_TAG),*|on:*) ;; \
			*) echo "$$b: not built with -tags=$(FYNE_NOCHECK_TAG), so Fyne's thread check would stay on"; exit 1;; esac; \
		case "$(call threadMode,off):$$tags" in on:*,$(FYNE_NOCHECK_TAG),*) echo "$$b: built with $(FYNE_NOCHECK_TAG) though FYNE_THREAD_CHECKS=on"; exit 1;; esac; \
	done
	@echo "Fyne's thread check in this release build: $(call threadMode,off) (FYNE_THREAD_CHECKS, D-103)"
endef

.PHONY: dist-app dist-dmg-template dist-macos dist-macos-sign dist-macos-notarize dist-checksums

dist-app: ## The macOS app with cogpit-cli inside, built with DIST_TAGS (no demo cluster) and Fyne's thread check off unless FYNE_THREAD_CHECKS=on (D-103), zipped, in dist/ (macOS only); VERSION=x.y.z
	@test "$$($(GO) env GOOS)" = darwin || { echo "dist-app runs on macOS only"; exit 1; }
	@test "$(origin VERSION)" != file || { echo "dist-app needs VERSION=x.y.z: the binaries would report release $(VERSION)"; exit 1; }
	$(MAKE) app GO_LDFLAGS='$(DIST_LDFLAGS)' GO_TAGS='$(DIST_TAGS)' FYNE_THREAD_CHECKS='$(call threadMode,off)'
	$(call checkDistBinaries,$(APP_MACOS)/cogpit $(APP_MACOS)/cogpit-cli)
	@mkdir -p $(DIST)
	ditto -c -k --keepParent $(BIN)/Cogpit.app $(DIST)/Cogpit_$(VERSION)_darwin_$$($(GO) env GOARCH).zip
	@echo "dist/Cogpit_$(VERSION)_darwin_$$($(GO) env GOARCH).zip"

dist-dmg-template: ## The disk image's window as a template without the app (D-70: packaging/dmg/, dmgbuild pinned in hack/macos/dmgbuild-requirements.txt, Python 3.10+), in dist/ for dist-macos; after dist-app (macOS only); VERSION=x.y.z
	hack/macos/dmg-template.sh '$(VERSION)' '$(BIN)/Cogpit.app' '$(DIST)'

dist-macos: ## The macOS download in dist/macos from dist-app's zip and dist-dmg-template's template: the disk image, the app and its cogpit-cli signed and notarized with the release secrets, MACOS_SIGN=adhoc for a local check, else the zip unsigned (macOS only); VERSION=x.y.z
	MACOS_SIGN='$(MACOS_SIGN)' hack/macos/release.sh '$(VERSION)' '$(DIST)' '$(DIST)/macos'

dist-macos-sign: ## The release pipeline's first macOS job: dist-macos's signing and disk image, the disk image submitted to Apple's notary service without waiting, the submission recorded in dist/macos/notary-submission.json; VERSION=x.y.z
	MACOS_SIGN='$(MACOS_SIGN)' hack/macos/release.sh sign '$(VERSION)' '$(DIST)' '$(DIST)/macos'

dist-macos-notarize: ## The release pipeline's second macOS job: Apple's verdict on the submission dist-macos-sign recorded (a re-run waits for the same one), the ticket stapled and checked; VERSION=x.y.z
	hack/macos/release.sh notarize '$(VERSION)' '$(DIST)/macos'

# Sparkle's appcast (D-4; docs/developer/releasing.md#the-appcast): the release pipeline's three
# appcast jobs. appcast-draft drafts the feed with the release's item in dist/appcast (no secret),
# appcast-sign signs the disk image and the feed with Sparkle's sign_update and
# SPARKLE_ED_PRIVATE_KEY into dist/site (the environment sparkle; without the key it signs
# nothing), appcast-verify checks the signed feed and the disk image with SPARKLE_PUBLIC_ED_KEY
# before Pages gets it, and that it keeps every item of the feed Pages serves now.
APPCAST         := $(DIST)/appcast

.PHONY: appcast-draft appcast-sign appcast-verify

appcast-draft: ## Draft the appcast with this release's item in dist/appcast (release pipeline, macOS only); VERSION=x.y.z
	hack/macos/appcast.sh draft '$(VERSION)' '$(DIST)/macos' '$(APPCAST)' '$(SPARKLE_FEED_URL)'

appcast-sign: sparkle ## Sign the disk image and the drafted appcast into dist/site with SPARKLE_ED_PRIVATE_KEY (release pipeline's environment sparkle); VERSION=x.y.z
	hack/macos/appcast.sh sign '$(VERSION)' '$(DIST)/macos' '$(APPCAST)' '$(DIST)/site' '$(SPARKLE_DIR)'

appcast-verify: ## Check dist/site/appcast.xml and the disk image with SPARKLE_PUBLIC_ED_KEY, and that it takes back no newer release Pages serves, before Pages gets them; VERSION=x.y.z
	$(GO) run ./hack/appcast verify -feed '$(DIST)/site/appcast.xml' -pubkey '$(SPARKLE_PUBLIC_ED_KEY)' \
		-dmg "$$(ls $(DIST)/macos/Cogpit-$(VERSION)-macos-*.dmg)" -version "$$(cat $(APPCAST)/version)" \
		-live '$(SPARKLE_FEED_URL)'

dist-checksums: ## SHA256SUMS of the disk images and app zips in dist/
	cd $(DIST) && files=$$(ls *.zip *.dmg 2>/dev/null || true) && test -n "$$files" && \
		if command -v sha256sum >/dev/null; then sha256sum $$files; else shasum -a 256 $$files; fi > SHA256SUMS
	@cat $(DIST)/SHA256SUMS

# Cogpit for Linux (D-108; docs/developer/releasing.md#linux): the Debian package
# dist/cogpit_<version>_<arch>.deb with both binaries, built as a release builds them (DIST_TAGS,
# Fyne's thread check off unless FYNE_THREAD_CHECKS=on), the launcher, the icons drawn by hack/icon,
# the update helper and Cogpit's APT repository (hack/linux/deb.sh); for this machine's architecture
# (cgo with OpenGL and X11 does not cross-compile), so the release pipeline builds each on its own
# runner. The APT repository on GitHub Pages (https://guided-traffic.github.io/cogpit/apt) is made
# by hack/linux/apt.sh: apt-repo builds it from the packages in dist/linux and the repository Pages
# serves, signed with APT_SIGNING_KEY (the release pipeline's environment apt; without it, unsigned
# and not deployed), apt-verify checks it with the public key the packages carry.
DEB_ARCH        ?= $(shell dpkg --print-architecture 2>/dev/null)
DEB_MAINTAINER  ?= Hans Fischer <mail@hans-fischer.com>
# The hicolor theme's sizes the icon is drawn in.
LINUX_ICON_SIZES := 16 24 32 48 64 128 256 512
LINUX_ICONS     := $(CACHE)/linux/icons
APT_URL         := https://guided-traffic.github.io/cogpit/apt
APT_KEYRING     := packaging/linux/cogpit-archive-keyring.asc

.PHONY: dist-linux apt-repo apt-verify

dist-linux: ## The Linux package dist/linux/cogpit_<version>_<arch>.deb for this machine's architecture, built with DIST_TAGS and Fyne's thread check off unless FYNE_THREAD_CHECKS=on (D-103, D-108; Linux only, dpkg-dev); VERSION=x.y.z
	@test "$$($(GO) env GOOS)" = linux || { echo "dist-linux runs on Linux only"; exit 1; }
	@test "$(origin VERSION)" != file || { echo "dist-linux needs VERSION=x.y.z: the binaries would report release $(VERSION)"; exit 1; }
	@test -n "$(DEB_ARCH)" || { echo "dist-linux needs dpkg (Debian or Ubuntu)"; exit 1; }
	@# Position-independent, as Debian builds its own Go programs: the system lays them out at random.
	GOFLAGS="$${GOFLAGS:+$$GOFLAGS }-buildmode=pie" \
		$(MAKE) build GO_LDFLAGS='$(DIST_LDFLAGS)' GO_TAGS='$(DIST_TAGS)' FYNE_THREAD_CHECKS='$(call threadMode,off)'
	$(call checkDistBinaries,$(BIN)/cogpit $(BIN)/cogpit-cli)
	rm -rf $(LINUX_ICONS) && mkdir -p $(LINUX_ICONS)
	$(GO) build -o $(CACHE)/linux/icon ./hack/icon
	for s in $(LINUX_ICON_SIZES); do $(CACHE)/linux/icon -size $$s -o $(LINUX_ICONS)/$$s.png; done
	DEB_MAINTAINER='$(DEB_MAINTAINER)' hack/linux/deb.sh '$(VERSION)' '$(DEB_ARCH)' '$(BIN)' '$(LINUX_ICONS)' '$(DIST)/linux'

.PHONY: test-linux-package
test-linux-package: ## Check the package dist-linux built: lintian (an error fails) and the APT repository's scripts end to end with a throwaway key (hack/linux/apt-test.sh); CI's job Linux Package
	@n=$$(ls $(DIST)/linux/cogpit_*.deb 2>/dev/null | wc -l); test "$$n" = 1 || { echo "dist/linux holds $$n packages: run make dist-linux VERSION=x.y.z first, once"; exit 1; }
	lintian --fail-on error $(DIST)/linux/cogpit_*.deb
	hack/linux/apt-test.sh $(DIST)/linux

apt-repo: ## The APT repository in dist/apt from the packages in dist/linux and the one Pages serves, signed with APT_SIGNING_KEY when it is set (release pipeline's environment apt)
	rm -rf $(DIST)/apt $(CACHE)/apt-live
	hack/linux/apt.sh mirror '$(APT_URL)' '$(APT_KEYRING)' '$(CACHE)/apt-live'
	hack/linux/apt.sh build '$(DIST)/apt' '$(CACHE)/apt-live' $(DIST)/linux/cogpit_*.deb
	hack/linux/apt.sh sign '$(DIST)/apt' '$(APT_KEYRING)'

apt-verify: ## Check the signed APT repository in dist/apt with the packages' public key, and that it takes back no newer release Pages serves
	hack/linux/apt.sh verify '$(DIST)/apt' '$(APT_KEYRING)'
	hack/linux/apt.sh newer '$(DIST)/apt' '$(APT_URL)' '$(APT_KEYRING)'

# Cogpit for Windows (D-111; docs/developer/releasing.md#windows): the setup
# dist/windows/Cogpit-<version>-windows-<arch>-setup.exe (NSIS, packaging/windows/cogpit.nsi) with
# both executables, built as a release builds them (DIST_TAGS, Fyne's thread check off unless
# FYNE_THREAD_CHECKS=on), the app a GUI program, both with their icon, manifest and version
# information (hack/windows/resources.sh, go-winres), and WinSparkle, the app's updater; for this
# machine's architecture (cgo with OpenGL does not cross-compile), on Windows with MSYS2's MinGW-w64
# gcc, NSIS 3 and Git Bash or MSYS2's bash (hack/windows/setup.sh).
# renovate: datasource=go depName=github.com/tc-hib/go-winres
GO_WINRES_VERSION ?= v0.3.3
GO_WINRES       := $(BIN)/go-winres$(EXE)
# WinSparkle (D-111), Sparkle's Windows port: the release archive pinned by its release tag and
# SHA-256 (GitHub's digest of the asset), kept in .cache/winsparkle/<version> (hack/windows/winsparkle.sh).
# Renovate updates both lines as it does Sparkle's (the tag, since its datasource looks the release
# up by it) and never merges the update on its own.
# renovate: datasource=github-release-attachments depName=vslavik/winsparkle
WINSPARKLE_TAG     := v0.9.4
WINSPARKLE_SHA256  := 6037df37fc263bd1650a1c4949681a9d40ffe991d01f35892a406cb5d103c976
WINSPARKLE_VERSION := $(patsubst v%,%,$(WINSPARKLE_TAG))
WINSPARKLE_DIR     := $(CACHE)/winsparkle/$(WINSPARKLE_VERSION)
# The setup's architecture names (x64, arm64) for Go's (amd64, arm64).
WIN_ARCH         = $(if $(filter arm64,$(shell $(GO) env GOARCH)),arm64,x64)
WIN_ICON        := $(CACHE)/windows/cogpit.ico
WIN_ICON_SIZES  := 16,20,24,32,40,48,64,256
# The Windows app's update feed (D-111), beside Sparkle's on Pages, and the update key's public
# half, the one Sparkle's feed is signed with (SPARKLE_PUBLIC_ED_KEY, the repository variable): the
# linker writes both into the app (internal/update's windowsFeedURL and edPublicKey; their
# TestMakefileBuildsTheFeedIntoTheApp checks these names, since the linker ignores a -X for a
# missing symbol). Without the key the app has no updater, as a macOS app without it.
WINDOWS_FEED_URL := https://guided-traffic.github.io/cogpit/appcast-windows.xml
UPDATE_PKG       := github.com/guided-traffic/cogpit/internal/update
WIN_APP_LDFLAGS   = -H=windowsgui -X $(UPDATE_PKG).windowsFeedURL=$(WINDOWS_FEED_URL) \
	$(if $(SPARKLE_PUBLIC_ED_KEY),-X $(UPDATE_PKG).edPublicKey=$(SPARKLE_PUBLIC_ED_KEY)) \
	-X $(MESA_PKG).version=$(MESA_VERSION) -X $(MESA_PKG).archiveSHA256=$(MESA_SHA256)
# Mesa's software OpenGL for Windows: pal1000/mesa-dist-win's MSVC build, the release archive
# pinned by its tag and SHA-256 (GitHub's digest of the asset). The smoke test of a Windows x64
# build draws with it (D-112; smoke, SMOKE_MESA): hack/windows/mesa.sh keeps the x64 opengl32.dll
# and libgallium_wgl.dll (the llvmpipe driver) in .cache/mesa/<version>, as GitHub's Windows runners
# have no OpenGL 2.1 driver, Fyne's minimum. The linker writes the pin into a release cogpit.exe
# (WIN_APP_LDFLAGS; internal/mesa's version and archiveSHA256), which offers to download it on a
# computer without an OpenGL to draw with (D-142); no setup carries it. Renovate's update of the pin
# waits for a person.
MESA_PKG        := github.com/guided-traffic/cogpit/internal/mesa
# renovate: datasource=github-release-attachments depName=pal1000/mesa-dist-win
MESA_VERSION    := 26.2.4
MESA_SHA256     := 351fc8c8b695878ffb3eaa044b3ead08672a48b1a045e3c3e3975811df0f6695
MESA_DIR        := $(CACHE)/mesa/$(MESA_VERSION)
SMOKE_MESA       = $(if $(and $(filter windows,$(APP_OS)),$(filter x64,$(WIN_ARCH))),$(MESA_DIR)/x64)
# The installed app's smoke test in test-windows-package (D-112): on for CI's job Windows Package;
# off by default, as in the release pipeline's Windows job, which ships the setup it builds and runs
# no Mesa beside it.
WINDOWS_SMOKE   ?= off

.PHONY: winsparkle mesa dist-windows test-windows-package

winsparkle: $(WINSPARKLE_DIR)/.verified ## Download the pinned WinSparkle release into .cache/winsparkle (checksum verified)

$(WINSPARKLE_DIR)/.verified:
	hack/windows/winsparkle.sh fetch '$(WINSPARKLE_VERSION)' '$(WINSPARKLE_SHA256)' '$(WINSPARKLE_DIR)'

mesa: $(MESA_DIR)/.verified ## Download the pinned Mesa for Windows into .cache/mesa (checksum verified): the Windows smoke test's software OpenGL

$(MESA_DIR)/.verified:
	hack/windows/mesa.sh fetch '$(MESA_VERSION)' '$(MESA_SHA256)' '$(MESA_DIR)'

$(GO_WINRES):
	GOBIN=$(BIN) $(GO) install github.com/tc-hib/go-winres@$(GO_WINRES_VERSION)

dist-windows: winsparkle $(GO_WINRES) ## The Windows setup dist/windows/Cogpit-<version>-windows-<arch>-setup.exe for this machine's architecture, built with DIST_TAGS and Fyne's thread check off unless FYNE_THREAD_CHECKS=on (D-103, D-111; Windows only, MinGW-w64 gcc and NSIS 3); VERSION=x.y.z
	@test "$$($(GO) env GOOS)" = windows || { echo "dist-windows runs on Windows only"; exit 1; }
	@test "$(origin VERSION)" != file || { echo "dist-windows needs VERSION=x.y.z: the binaries would report release $(VERSION)"; exit 1; }
	@mkdir -p $(CACHE)/windows
	$(GO) run ./hack/icon -ico $(WIN_ICON_SIZES) -o $(WIN_ICON)
	@# The resources carry the version: made for this build only, removed whatever it comes to.
	hack/windows/resources.sh make '$(VERSION)' '$(WIN_ICON)' '$(GO_WINRES)' "$$($(GO) env GOARCH)"
	$(MAKE) build GO_LDFLAGS='$(DIST_LDFLAGS)' GO_TAGS='$(DIST_TAGS)' FYNE_THREAD_CHECKS='$(call threadMode,off)' \
		APP_LDFLAGS='$(strip $(WIN_APP_LDFLAGS))'; rc=$$?; hack/windows/resources.sh clean; exit $$rc
	$(call checkDistBinaries,$(BIN)/cogpit.exe $(BIN)/cogpit-cli.exe)
	hack/windows/setup.sh '$(VERSION)' '$(WIN_ARCH)' '$(BIN)' '$(WIN_ICON)' '$(WINSPARKLE_DIR)' '$(DIST)/windows'

# WinSparkle's appcast (D-111; docs/developer/releasing.md#windows): the release pipeline's three
# Windows appcast jobs, as the macOS feed's. appcast-windows-draft drafts the feed with the
# release's item in dist/appcast-windows (no secret), appcast-windows-sign signs the setup and the
# feed with Sparkle's sign_update and SPARKLE_ED_PRIVATE_KEY into dist/site-windows (the
# environment sparkle, on macOS; hack/macos/appcast.sh's sign), appcast-windows-verify checks both
# with SPARKLE_PUBLIC_ED_KEY before Pages gets them.
APPCAST_WINDOWS := $(DIST)/appcast-windows

.PHONY: appcast-windows-draft appcast-windows-sign appcast-windows-verify

appcast-windows-draft: ## Draft the Windows appcast with this release's item in dist/appcast-windows (release pipeline); VERSION=x.y.z
	hack/windows/appcast.sh draft '$(VERSION)' '$(DIST)/windows' '$(APPCAST_WINDOWS)' '$(WINDOWS_FEED_URL)'

appcast-windows-sign: sparkle ## Sign the setup and the drafted Windows appcast into dist/site-windows with SPARKLE_ED_PRIVATE_KEY (release pipeline's environment sparkle, macOS); VERSION=x.y.z
	hack/macos/appcast.sh sign '$(VERSION)' '$(DIST)/windows' '$(APPCAST_WINDOWS)' '$(DIST)/site-windows' '$(SPARKLE_DIR)' \
		'Cogpit-$(VERSION)-windows-*-setup.exe' appcast-windows.xml

appcast-windows-verify: ## Check dist/site-windows/appcast-windows.xml and the setup with SPARKLE_PUBLIC_ED_KEY, and that it takes back no newer release Pages serves; VERSION=x.y.z
	$(GO) run ./hack/appcast verify -feed '$(DIST)/site-windows/appcast-windows.xml' -pubkey '$(SPARKLE_PUBLIC_ED_KEY)' \
		-file "$$(ls $(DIST)/windows/Cogpit-$(VERSION)-windows-*-setup.exe)" -version "$$(cat $(APPCAST_WINDOWS)/version)" \
		-live '$(WINDOWS_FEED_URL)'

# The tests on Windows (D-111): CI's jobs Unit Tests (windows) and UI Tests (windows) on GitHub's
# runner in MSYS2. Every package's tests without the race detector, the window's in a job of their
# own (200 of the 222 seconds of a whole run on the owner's workstation, 2026-10-21). Until
# 2026-10-21 CI only compiled the tests there.
.PHONY: test-windows-unit test-windows-ui

test-windows-unit: winsparkle ## Every package's tests on Windows but the window's (internal/ui/fyneui), after go vet; CI's Unit Tests (windows)
	$(GO) vet ./...
	$(TEST_ENV) $(GO) test -count=1 -timeout 20m $$($(GO) list ./... | grep -v '/internal/ui/fyneui$$')

# The window's tests in four of hack/shard's shards, two at a time (D-112): GitHub's Windows runner
# has 4 CPUs and 16 GB, and one process for the package peaked at 15.6 GB (macOS, 2026-10-21), as
# every test's app makes Fyne parse its fonts again; a quarter of the tests peaks at about 4 GB.
WINDOWS_UI_SHARDS ?= 4
WINDOWS_UI_JOBS   ?= 2

test-windows-ui: ## The window's tests (internal/ui/fyneui) on Windows, WINDOWS_UI_SHARDS shards, WINDOWS_UI_JOBS at once; CI's UI Tests (windows)
	$(TEST_ENV) $(GO) run ./hack/shard -pkg $(UI_PKG) -of $(WINDOWS_UI_SHARDS) -jobs $(WINDOWS_UI_JOBS) -exec -timeout 20m \
		-durations $(UI_DURATIONS)

test-windows-package: $(if $(filter on,$(WINDOWS_SMOKE)),$(if $(SMOKE_MESA),mesa)) ## Check the setup dist-windows built: a silent install for this user into a folder of its own, its files, versions and uninstall entry, with WINDOWS_SMOKE=on the installed app's smoke test, an update over it, then a silent uninstall (hack/windows/setup-test.sh); CI's job Windows Package
	@n=$$(ls $(DIST)/windows/Cogpit-*-windows-*-setup.exe 2>/dev/null | wc -l); test "$$n" = 1 || { echo "dist/windows holds $$n setups: run make dist-windows VERSION=x.y.z first, once"; exit 1; }
	SMOKE_BOUND='$(if $(filter on,$(WINDOWS_SMOKE)),$(SMOKE_BOUND))' SMOKE_MESA='$(SMOKE_MESA)' \
		hack/windows/setup-test.sh $(DIST)/windows/Cogpit-*-windows-*-setup.exe

# The smoke test (D-112): the app's binary SMOKE_BIN started once with --smoke SMOKE_BOUND
# (hack/smoke.sh): the window with the real driver in a home of its own until it drew a frame, no
# MCP server, updater, login item or cluster; on Linux without a display under xvfb-run, on Windows
# x64 with Mesa's llvmpipe (SMOKE_MESA). CI runs it on what a release ships: Linux Package on
# dist-linux's binary, Windows Package on the installed cogpit.exe (test-windows-package
# WINDOWS_SMOKE=on). macOS runs it by hand only (bin/cogpit, or after dist-app
# SMOKE_BIN=bin/Cogpit.app/Contents/MacOS/cogpit): GLFW asks NSOpenGL for an accelerated pixel
# format only, which GitHub's macOS VMs do not offer.
SMOKE_BIN       ?= $(BIN)/cogpit$(EXE)
SMOKE_BOUND     ?= 2m

.PHONY: smoke

smoke: $(if $(SMOKE_MESA),mesa) ## Start the app's binary SMOKE_BIN (bin/cogpit) with --smoke: its window in a home of its own until the first frame, then quit; fails on a logged error, a quit that hangs or after SMOKE_BOUND (D-112)
	SMOKE_MESA='$(SMOKE_MESA)' hack/smoke.sh '$(SMOKE_BIN)' '$(SMOKE_BOUND)'

clean: ## Remove build output
	rm -rf $(BIN) dist
