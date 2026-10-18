# Cogpit — developer targets. CI (.github/workflows/release.yml) runs the same ones, one job each:
# lint with check-nodemo, parity, gosec, vuln, cyclo, test-unit-coverage, test-race-ui (sharded),
# test-race-rest, kind-up with test-kind, kind-drain-up with test-kind-drain, envtest-bin with
# test-envtest, coverage-report, and test-darwin with app on macOS (docs/developer/releasing.md#ci).
#
# Go: go.mod declares go 1.27.2; GOTOOLCHAIN=auto fetches it when the local Go is older.
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
CACHE           := $(CURDIR)/.cache
KIND_CLUSTER    ?= cogpit
KIND_KUBECONFIG ?= $(CACHE)/kind/kubeconfig
KIND_CONTEXT    := kind-$(KIND_CLUSTER)
KUBECTL         := KUBECONFIG=$(KIND_KUBECONFIG) kubectl --context $(KIND_CONTEXT)
# renovate: datasource=go depName=fyne.io/tools/cmd/fyne
FYNE_TOOLS      ?= v1.7.3
FYNE            := $(BIN)/fyne
# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.14.0
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
APP_ID          := io.guided-traffic.cogpit
# The code-signing identifier of the cogpit-cli in Cogpit.app/Contents/MacOS (D-16); the release's
# Developer ID signature uses the same (hack/macos/release.sh, cli_identifier).
CLI_ID          := $(APP_ID).cli
APP_OS          ?= $(shell $(GO) env GOOS)
TEST_ENV        := env -u KUBERNETES_SERVICE_HOST -u KUBERNETES_SERVICE_PORT KUBECONFIG=/dev/null

.PHONY: all build run run-demo test test-unit test-unit-race test-kind test-kind-compile bench lint parity fmt tidy \
        kind-up kind-down kind-seed app icon clean help

all: lint test-unit-race build ## Lint, race tests, build

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

build: ## Build bin/cogpit (cgo, Fyne) and bin/cogpit-cli (pure Go)
	$(GO) build -tags '$(GO_TAGS)' -ldflags '$(GO_LDFLAGS)' -o $(BIN)/cogpit ./cmd/cogpit
	CGO_ENABLED=0 $(GO) build -tags '$(GO_TAGS)' -ldflags '$(GO_LDFLAGS)' -o $(BIN)/cogpit-cli ./cmd/cogpit-cli

run: build ## Run Cogpit against the kind cluster (make kind-up first)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	$(BIN)/cogpit --kubeconfig $(KIND_KUBECONFIG) --context $(KIND_CONTEXT)

run-demo: build ## Run Cogpit on the synthetic demo cluster (10,000 pods, 1,000 events/s); development builds only, a release has none (D-67)
	$(BIN)/cogpit --demo

test: test-unit-race ## All tests that need no cluster (race detector on); test-kind needs make kind-up

test-unit: ## Unit tests without the race detector
	$(TEST_ENV) $(GO) test ./...

# The race detector makes the UI's tests (internal/ui/fyneui) take about 18 minutes here (night run
# 2026-10-13) and more than 25 on CI's macOS runner; RACE_TIMEOUT bounds a package instead. CI
# splits them into shards (test-race-ui) and runs the rest in one job (test-race-rest).
RACE_TIMEOUT ?= 40m

test-unit-race: test-fork ## Unit tests with the race detector, the terminal widget's included
	$(TEST_ENV) $(GO) test -race -timeout $(RACE_TIMEOUT) ./...

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
KIND_PKGS        = $(sort $(addprefix ./,$(dir $(shell grep -rl --include='*_test.go' '^//go:build kind' cmd internal))))
KIND_RUN        ?= ^TestKind

test-kind-compile: ## Compile the cluster tests without a cluster or a run (CI does it while the cluster starts)
	$(GO) test -race -tags kind -covermode=atomic -coverpkg=./... -run '^$$' $(KIND_PKGS)

test-kind: ## Cluster tests against the kind cluster (make kind-up first), with coverage (.cache/coverage/integration.out)
	@test -s $(KIND_KUBECONFIG) || { echo "no kubeconfig at $(KIND_KUBECONFIG): run make kind-up first"; exit 1; }
	@mkdir -p $(COVERAGE_DIR)
	$(TEST_ENV) COGPIT_KIND_KUBECONFIG=$(KIND_KUBECONFIG) COGPIT_KIND_CONTEXT=$(KIND_CONTEXT) \
		$(GO) test -race -timeout $(RACE_TIMEOUT) -tags kind -run '$(KIND_RUN)' -skip '^TestKindDrain' \
		-covermode=atomic -coverpkg=./... -coverprofile=$(COVERAGE_DIR)/integration.out $(KIND_PKGS)

bench: ## Benchmarks: demo churn and build, and the gate criteria S1 (snapshots under churn) and S2 (sort)
	$(TEST_ENV) $(GO) test -run '^$$' -bench . -benchmem ./internal/engine/... ./internal/demo/... ./internal/ui/...

# The test targets CI runs, one job each (docs/developer/releasing.md#ci). Every one writes a
# coverage profile to .cache/coverage; coverage-report merges what is there into the README's badge.
COVERAGE_DIR    := $(CACHE)/coverage
COVERAGE_BADGE  := .github/badges/coverage.json
UNIT_TIMEOUT    ?= 20m

.PHONY: test-unit-coverage test-race-ui test-race-rest test-darwin coverage-report

test-unit-coverage: ## Unit tests without the race detector, with a coverage profile (.cache/coverage/unit.out)
	@mkdir -p $(COVERAGE_DIR)
	$(TEST_ENV) $(GO) test -timeout $(UNIT_TIMEOUT) -covermode=atomic -coverprofile=$(COVERAGE_DIR)/unit.out ./...
	@$(GO) tool cover -func=$(COVERAGE_DIR)/unit.out | tail -n 1

# The UI's race tests in RACE_SHARDS shards: hack/shard assigns each test of internal/ui/fyneui to a
# shard by a hash of its name, so a new test lands in one without a change here or in CI. The
# number of shards is CI's (release.yml's matrix); locally any shard runs alone.
UI_PKG          := ./internal/ui/fyneui
RACE_SHARDS     ?= 8
RACE_SHARD      ?= 1

test-race-ui: ## Race tests of internal/ui/fyneui, shard RACE_SHARD of RACE_SHARDS, with coverage (.cache/coverage/race-ui-<n>.out)
	@mkdir -p $(COVERAGE_DIR)
	@run="$$($(GO) run ./hack/shard -pkg $(UI_PKG) -tags race -shard $(RACE_SHARD) -of $(RACE_SHARDS))" && test -n "$$run" && \
		echo "$(GO) test -race $(UI_PKG) -run <shard $(RACE_SHARD) of $(RACE_SHARDS)>" && \
		$(TEST_ENV) $(GO) test -race -timeout $(RACE_TIMEOUT) -covermode=atomic \
			-coverprofile=$(COVERAGE_DIR)/race-ui-$(RACE_SHARD).out -run "$$run" $(UI_PKG)

# internal/ui/logo is left out: it rasterises the logo in pure Go, on one goroutine, and took nine of
# the job's fifteen minutes under the race detector (2026-10-17); the unit tests run it, coverage
# included.
test-race-rest: test-fork ## Race tests of every package but internal/ui/fyneui and internal/ui/logo, and the terminal widget's, with coverage (.cache/coverage/race-rest.out)
	@mkdir -p $(COVERAGE_DIR)
	$(TEST_ENV) $(GO) test -race -timeout $(RACE_TIMEOUT) -covermode=atomic -coverprofile=$(COVERAGE_DIR)/race-rest.out \
		$$($(GO) list ./... | grep -v -x -e 'github.com/guided-traffic/cogpit/internal/ui/fyneui' -e 'github.com/guided-traffic/cogpit/internal/ui/logo')

# What only macOS can test, for CI's macOS job: every test of a *_darwin_test.go file (the title
# bar's AppKit harness, the updater's harness with make sparkle's framework, the locale's
# CoreFoundation read), and the tests whose expectations branch
# on runtime.GOOS for the macOS shortcut glyphs (the terminal view's package, the title history).
empty           :=
space           := $(empty) $(empty)
DARWIN_TEST_FILES = $(shell find cmd internal -name '*_darwin_test.go')
DARWIN_TESTS      = $(shell sed -n -E 's/^func (Test[A-Za-z0-9_]*).*/\1/p' $(DARWIN_TEST_FILES)) TestTitleHistoryArrows

test-darwin: sparkle ## The macOS-only tests, race detector on (macOS only); Sparkle for the updater's harness
	@test "$$($(GO) env GOOS)" = darwin || { echo "test-darwin runs on macOS only"; exit 1; }
	$(TEST_ENV) $(GO) test -race -run '^($(subst $(space),|,$(strip $(DARWIN_TESTS))))$$' \
		$(sort $(addprefix ./,$(dir $(DARWIN_TEST_FILES))) $(UI_PKG)/)
	$(TEST_ENV) $(GO) test -race ./internal/ui/fyneui/termview/

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

$(GOVULNCHECK):
	GOBIN=$(BIN) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

vuln: $(GOVULNCHECK) ## govulncheck: known vulnerabilities in the code paths Cogpit calls
	$(GOVULNCHECK) ./...

# gosec through golangci-lint (the same build of it as lint), the shipped code only; an accepted
# finding is excluded in .golangci.yml with its reason, a real one is fixed or an H-gap in
# docs/security.
gosec: $(GOLANGCI_LINT) ## gosec: security findings in the code Cogpit ships (tests excluded)
	$(GOLANGCI_LINT) run --enable-only gosec --tests=false ./...

$(GOCYCLO):
	GOBIN=$(BIN) $(GO) install github.com/fzipp/gocyclo/cmd/gocyclo@$(GOCYCLO_VERSION)

cyclo: $(GOCYCLO) ## Cyclomatic complexity: no function above CYCLO_THRESHOLD
	@echo "Functions above complexity $(CYCLO_THRESHOLD):"
	@out="$$($(GOCYCLO) -over $(CYCLO_THRESHOLD) -ignore '$(CYCLO_IGNORE)' $(CYCLO_DIRS))"; \
		if [ -n "$$out" ]; then echo "$$out"; exit 1; fi; echo "none"

cyclo-report: $(GOCYCLO) ## The 20 most complex functions
	@$(GOCYCLO) -top 20 -ignore '$(CYCLO_IGNORE)' $(CYCLO_DIRS)

# golangci-lint built with the project's Go: v2.13.x cannot read Go 1.27.2 export data.
$(GOLANGCI_LINT):
	GOBIN=$(BIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# LINT_PKGS narrows the packages: CI's macOS job lints the ones with darwin-only files, which the
# Linux lint job does not see.
LINT_PKGS       ?= ./...

lint: $(GOLANGCI_LINT) ## golangci-lint: the team's linters plus depguard layering
	$(GOLANGCI_LINT) run $(LINT_PKGS)

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

$(FYNE):
	GOBIN=$(BIN) $(GO) install fyne.io/tools/cmd/fyne@$(FYNE_TOOLS)

# Sparkle 2, the macOS app's updater (D-4; docs/developer/releasing.md#updates): the release archive
# pinned by version and SHA-256 (GitHub's digest of the asset), kept in .cache/sparkle/<version>
# (hack/macos/sparkle.sh). make app bundles its framework; the release pipeline signs the update
# with its sign_update. Renovate proposes a new version but cannot know the archive's SHA-256: the
# update pull request fails make sparkle until SPARKLE_SHA256 is set to the new asset's digest, and
# it never merges on its own (.github/renovate.json).
# renovate: datasource=github-releases depName=sparkle-project/Sparkle
SPARKLE_VERSION := 2.10.0
SPARKLE_SHA256  := c2bf58aa8387266ac179357b1415d6f2635f044da8be41042af32425dae6da0c
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

.PHONY: dist-app dist-dmg-template dist-macos dist-macos-sign dist-macos-notarize dist-checksums

dist-app: ## The macOS app with cogpit-cli inside, built with DIST_TAGS (no demo cluster), zipped, in dist/ (macOS only); VERSION=x.y.z
	@test "$$($(GO) env GOOS)" = darwin || { echo "dist-app runs on macOS only"; exit 1; }
	@test "$(origin VERSION)" != file || { echo "dist-app needs VERSION=x.y.z: the binaries would report release $(VERSION)"; exit 1; }
	$(MAKE) app GO_LDFLAGS='$(DIST_LDFLAGS)' GO_TAGS='$(DIST_TAGS)'
	@for b in $(APP_MACOS)/cogpit $(APP_MACOS)/cogpit-cli; do \
		$(GO) version -m $$b | grep -Ex '[[:space:]]+build[[:space:]]+-tags=$(DIST_TAGS)' >/dev/null || \
			{ echo "$$b: not built with -tags=$(DIST_TAGS), so it would ship the demo cluster"; exit 1; }; \
	done
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

clean: ## Remove build output
	rm -rf $(BIN) dist
