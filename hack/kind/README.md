# Local kind cluster

A single-node [kind](https://kind.sigs.k8s.io) cluster named `cogpit` for running Cogpit and the
cluster tests against a real API server, with the Metrics Server and a small seed of workloads. Its
kubeconfig is its own file, `.cache/kind/kubeconfig` (context `kind-cogpit`); nothing here reads or
writes `~/.kube/config`, so Cogpit and kubectl never reach another cluster by accident.

| File | What it is |
|---|---|
| [`cluster.yaml`](cluster.yaml) | kind configuration: one control-plane node, the default service CIDR `10.96.0.0/16` |
| [`seed/`](seed/) | the manifests `make kind-seed` applies, all in the namespace `cogpit-demo` |
| [`prometheus-values.yaml`](prometheus-values.yaml) | the `prometheus` chart's values ([Prometheus](#prometheus)) |
| [`kube-prometheus-stack-values.yaml`](kube-prometheus-stack-values.yaml) | the `kube-prometheus-stack` chart's values ([kube-prometheus-stack](#kube-prometheus-stack)) |
| [`victoriametrics/`](victoriametrics/) | VictoriaMetrics single-node and a small cluster, plain manifests ([VictoriaMetrics](#victoriametrics)) |
| [`trivy-operator-values.yaml`](trivy-operator-values.yaml), [`trivy/`](trivy/) | Trivy Operator's values and the workloads it scans ([Trivy Operator](#trivy-operator)) |
| [`alertmanager/`](alertmanager/) | one Alertmanager, plain manifests ([Alertmanager](#alertmanager)) |
| [`kyverno/`](kyverno/) | Kyverno's values, its test policies and the objects they judge ([Kyverno](#kyverno)) |
| [`drain-cluster.yaml`](drain-cluster.yaml) | the second cluster `cogpit-drain` (one control plane, two workers) for the tests that cordon and drain a node ([The drain cluster](#the-drain-cluster)) |

## Use

Needs Docker, `kind` and `kubectl`; the chart-based stacks also `helm`.

```sh
make kind-up     # create (or reuse) the cluster, its kubeconfig file, the seed, the Metrics Server
make run         # build Cogpit and open it on the kind cluster
make test-kind   # the cluster tests (TestKind*, race detector on) but the drain and the stacks' tests
make kind-seed   # re-apply the seed (unchanged objects stay unchanged)
make kind-down   # delete the cluster and its kubeconfig file
```

`make kind-up` is idempotent: for an existing cluster it exports the kubeconfig again and re-applies
the seed and the Metrics Server (its release manifest, version pinned in the Makefile, with
`--kubelet-insecure-tls` for kind's self-signed kubelet certificates). `make kind-seed` waits up to
three minutes each for `web` and `api` to roll out, on the first run mostly image pulls.
`KIND_CLUSTER=<name>` uses another cluster (context `kind-<name>`), e.g. one per worktree;
`KIND_RUN='^TestKindHelm'` narrows `make test-kind`.

kubectl against the cluster, always with the explicit file; resetting the seed:

```sh
KUBECONFIG=.cache/kind/kubeconfig kubectl --context kind-cogpit get all -n cogpit-demo
KUBECONFIG=.cache/kind/kubeconfig kubectl --context kind-cogpit delete namespace cogpit-demo && make kind-seed
```

An image pull that stalls inside the kind node holds the next pod too (the kubelet pulls one image
at a time). Pull it on the host and load it:

```sh
docker pull <image> && kind load docker-image <image> --name cogpit
```

## The seed

Namespace `cogpit-demo`. Every image tag is pinned.

| Object | Image | What it shows |
|---|---|---|
| ConfigMap `web-content` | – | an `index.html`, mounted into `web` |
| Deployment `web`, 3 replicas | `nginx:1.30.0-alpine` | healthy pods with a readiness probe |
| Service `web`, ClusterIP `10.96.100.100`, port 80 | – | the fixed address the MCP demo finds with `resources_find {kind: services, field: clusterIP, value: 10.96.100.100}` |
| Deployment `api`, 2 replicas | `registry.k8s.io/e2e-test-images/agnhost:2.53` (`netexec`) | an HTTP echo server, as a non-root container |
| Service `api`, port 80 → 8080 | – | a second service, with an allocated ClusterIP |
| Deployment `crashloop`, 1 replica | `busybox:1.37.0` | a container that exits with an error after 5 s: CrashLoopBackOff, a growing restart count, BackOff events, logs |
| Job `migrate` | `busybox:1.37.0` | a job that completes and leaves a Completed pod (no TTL, so it stays) |
| Pod `toolbox` | `busybox:1.37.0` | a long-running pod with a shell, for the pod-shell demo |

Except `web` (nginx binds port 80 as root), the seed's containers run as user 65534 without
privilege escalation and with all capabilities dropped.

## Optional stacks

Each target installs into the running cluster (`make kind-up` first) and has a `-down` target that
removes it again. A test skips without its stack; `make test-kind` runs the stacks' tests (the
Makefile's `KIND_ADDON_TESTS`) only with `KIND_ADDONS=1`, e.g.
`make kind-prometheus && make test-kind KIND_ADDONS=1 KIND_RUN='^TestKindPrometheus'`. Cogpit
itself installs nothing into a cluster: these are test setups.

| Target | Installs | Namespaces | Tests that need it |
|---|---|---|---|
| `make kind-prometheus` | the `prometheus` chart | `monitoring` | `TestKindPrometheus`, `TestKindNodeExporterByAddress`, `TestKindClusterOverview`, `TestKindPrometheusDrawer` |
| `make kind-kps` | a minimal `kube-prometheus-stack` | `cogpit-kps` | `TestKindKubePrometheusStack` |
| `make kind-vm` | VictoriaMetrics (after `make kind-prometheus`) | `cogpit-vm` | `TestKindVictoriaMetrics` |
| `make kind-trivy` | Trivy Operator and the workloads it scans | `cogpit-trivy`, `cogpit-trivy-a`, `cogpit-trivy-b` | `TestKindTrivy` |
| `make kind-alertmanager` | one Alertmanager | `cogpit-alerts` | `TestKindAlertmanager` |
| `make kind-kyverno` | Kyverno, test policies, violating objects | `cogpit-kyverno`, `cogpit-kyverno-a` | `TestKindKyverno` |

The charts are fetched with `--repo` at the version the Makefile pins, so no helm repository is
added to your helm configuration.

### Prometheus

`make kind-prometheus` installs or upgrades (`--wait`) the prometheus-community `prometheus` chart
as the release `prometheus` with [`prometheus-values.yaml`](prometheus-values.yaml): the server,
node-exporter and kube-state-metrics (no Alertmanager or Pushgateway), a week of retention. Its
Service `monitoring/prometheus-server` is a name Cogpit's detection looks for. The chart's
`kubernetes-nodes-cadvisor` job scrapes the kubelet's cAdvisor every minute (the pods' charts, a
node's network and filesystem); its `kubernetes-service-endpoints` job scrapes node-exporter (a
node's CPU and memory history, the Overview's CPU and Memory) and kube-state-metrics (the Overview's
Pods history), each labelled with its node. Charts fill two or three minutes after the install;
`TestKindClusterOverview` wants half an hour of node-exporter's history. `make kind-prometheus-down`
uninstalls it and deletes the namespace.

`TestKindMetricsFallback` scales the Metrics Server and then Prometheus to zero and back, so it
runs only with `COGPIT_KIND_FALLBACK=1`. Running it, and reading Prometheus through the service
proxy as Cogpit does:

```sh
COGPIT_KIND_FALLBACK=1 make test-kind KIND_ADDONS=1 KIND_RUN='^TestKindMetricsFallback'
KUBECONFIG=.cache/kind/kubeconfig kubectl --context kind-cogpit get --raw \
  '/api/v1/namespaces/monitoring/services/prometheus-server:http/proxy/api/v1/status/buildinfo'
```

What kind shows differently from a real cluster:

- Its containerd reports no `container_fs_usage_bytes` for containers, so a pod's Filesystem chart
  shows read and write rates; the node's shows the usage of its disk.
- A kind node's `/` is an overlay, which node-exporter's filesystems leave out, so the Nodes list's
  Disk column stays empty.
- node-exporter reads the Docker VM's `/proc`, the Metrics Server the node container's cgroup, so a
  node's history (node-exporter's) and its live values (the Metrics Server's) do not match.

### kube-prometheus-stack

`make kind-kps` installs the release `kps` with
[`kube-prometheus-stack-values.yaml`](kube-prometheus-stack-values.yaml): the operator and one
Prometheus (two hours of retention), the kubelet's cAdvisor scraped every 30 s, node-exporter (on
port 9110, beside the `prometheus` chart's on 9100) and kube-state-metrics; no Alertmanager,
Grafana or rules. Detection prefers it to the `prometheus` chart the other kind tests expect, so
remove it after the test: `make kind-kps-down` uninstalls it and deletes its CRDs, its kubelet
Service in `kube-system` and the namespace.

### VictoriaMetrics

`make kind-vm` adds VictoriaMetrics for D-71's check ([`victoriametrics/`](victoriametrics/), images
pinned to v1.106.1): single-node (`cogpit-vm/vmsingle-cogpit`, port `http` 8428) and a small
cluster of vmstorage, vminsert and vmselect (`cogpit-vm/vmselect-cogpit`, port `http` 8481, the
Prometheus API under `/select/0/prometheus`) with a vmagent. The Services are named as
VictoriaMetrics' operator names a VMSingle's and a VMCluster's. Neither scrapes the cluster: both
read the kind Prometheus's `/federate` endpoint every 30 seconds, so their history starts when they
do; run the test a few minutes after. Detection prefers the kind Prometheus to them, so the test
names each in the Prometheus setting. `make kind-vm-down` deletes the namespace.

### Trivy Operator

`make kind-trivy` installs Trivy Operator for the image ratings' check (D-78): the aquasecurity
`trivy-operator` chart as the release `trivy` in `cogpit-trivy`, with
[`trivy-operator-values.yaml`](trivy-operator-values.yaml): vulnerability reports only (no
configuration audits, RBAC or infrastructure assessments, exposed secrets, SBOMs or compliance), of
the namespaces `cogpit-trivy-a` and `cogpit-trivy-b` alone, one scan job at a time, the
vulnerability database downloaded once by the chart's built-in Trivy server (a StatefulSet with a
2 GiB volume), reports kept until their workload changes. Then it applies [`trivy/`](trivy/):

| Namespace | Workload | Images |
|---|---|---|
| `cogpit-trivy-a` | Deployment `mixed` | an init container `busybox:1.37.0`; containers `nginx:1.16.1` (an old Debian image with known vulnerabilities) and `busybox:1.37.0` |
| `cogpit-trivy-a` | StatefulSet `store` | `nginx:1.30.0-alpine` |
| `cogpit-trivy-a` | Job `once` | `busybox:1.37.0` |
| `cogpit-trivy-a` | suspended CronJob `nightly`, and a Job made from it, named as the CronJob controller names its Jobs | `alpine:3.12` |
| `cogpit-trivy-b` | bare Pod `bare` | `alpine:3.12` |

The reports follow within a few minutes; Trivy Operator's own table of them:

```sh
KUBECONFIG=.cache/kind/kubeconfig kubectl --context kind-cogpit get vulnerabilityreports -A -o wide
```

`make kind-trivy-down` deletes the workloads' namespaces, uninstalls the operator and deletes its
`aquasecurity.github.io` definitions and the namespace `cogpit-trivy`.

### Alertmanager

`make kind-alertmanager` adds one Alertmanager for D-77's check ([`alertmanager/`](alertmanager/)):
`quay.io/prometheus/alertmanager:v0.28.1`, its Service `cogpit-alerts/cogpit-alertmanager` (port
`http` 9093, named as the alertmanager chart names its own, so Cogpit finds it by name), every alert
routed to a receiver that sends nothing. Nothing fires alerts into it: the test posts its own
through the API server's service proxy (as the cluster's admin; Cogpit itself only reads) and
resolves them at its end. `make kind-alertmanager-down` deletes the namespace.

### Kyverno

`make kind-kyverno` installs Kyverno for the Kyverno integration's check (D-79): the admission and
reports controllers of Kyverno's chart (3.9.1, Kyverno 1.19.1) in `cogpit-kyverno`
([`kyverno/values.yaml`](kyverno/values.yaml)), its webhooks limited to the namespaces labelled
`cogpit-kyverno=true`, so the other kind tests never pass through it, the background scan every
minute. Then, in this order:

1. [`kyverno/before.yaml`](kyverno/before.yaml), the objects made before the policies: the
   namespace `cogpit-kyverno-a`, a ConfigMap without and one with an `owner` label, a Deployment
   and a Service.
2. [`kyverno/policies.yaml`](kyverno/policies.yaml): two ClusterPolicies in Audit and one in
   Enforce with `failurePolicy: Ignore`, each matching only `cogpit-kyverno-*`, and a
   ValidatingPolicy in Audit for the labelled namespaces.
3. [`kyverno/after.yaml`](kyverno/after.yaml), an object made after them.

The reports follow within a minute of the install. `make kind-kyverno-down` removes the policies,
both namespaces, Kyverno, its CRDs and its webhook configurations.

## The synthetic cluster

`make run-demo` needs no cluster at all: [`internal/demo`](../../internal/demo/demo.go) generates
10,000 pods, 500 deployments, 300 services, 50 nodes, 20 namespaces and 2,000 events in memory and
changes them at 1,000 watch events per second, through the same cache and informers as a real
cluster. Its well-known service is `demo-mcp/web` with ClusterIP `10.96.100.100`.

## The drain cluster

Cordon, uncordon and drain change a node, and the cluster above has only one: draining it would
stop every other test. Those tests run against a second cluster, `cogpit-drain`, with one control
plane and two workers ([`drain-cluster.yaml`](drain-cluster.yaml)); its kubeconfig is its own file,
`.cache/kind-drain/kubeconfig` (context `kind-cogpit-drain`).

```sh
make kind-drain-up     # create the cluster (or reuse it) and write .cache/kind-drain/kubeconfig
make test-kind-drain   # TestKindDrainCrashFinishUncordon, in the namespace cogpit-drain-test
make kind-drain-down   # delete the cluster and its kubeconfig file
```

The test drains the first worker by name: a Deployment's two pods, a pod a PodDisruptionBudget
guards and a DaemonSet's pod are put on it; Cogpit "crashes" while the budget blocks an eviction,
the next start offers the unfinished drain, finishing it fails while the budget stands and
succeeds once it is gone, and the node is uncordoned at the end (and on cleanup). It pulls
`busybox:1.37.0`. `make test-kind` leaves it out; it runs only when `COGPIT_KIND_DRAIN_KUBECONFIG`
and `COGPIT_KIND_DRAIN_CONTEXT` are set, as `make test-kind-drain` sets them, and refuses the
context `kind-cogpit`.
