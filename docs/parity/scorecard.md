# Scorecard

> **Generated** by `make scorecard` ([`hack/scorecard`](../../hack/scorecard/main.go)) from what the budget
> tests and benchmarks measured; do not edit. Not run in CI ([D-15](../developer/decisions.md#d-15-progress-generators)):
> run it on the machine named below and commit the page. The budgets and how each is measured:
> [testing › Performance budgets](../developer/testing.md#performance-budgets); the work list:
> [G15](gaps.md#g15-performance-and-api-load-at-scale).

**Measured 2026-10-16** on darwin/arm64 (Apple M1 Ultra, 20 CPUs, macOS 26.6.2) with go1.27.2,
the race detector off. A budget passes when every bounded measurement of it is within its limit;
a measurement marked context explains the others and has no limit. The limits are testing.md's
proposed targets, not yet validated against Lens Desktop on the same machine, and the tests
measure Cogpit's share: against fakes on the loopback, offscreen, without a real cluster's round
trips or a GPU frame.

## Summary

| Budget | Scenario | Proposed target | Result | Measured by |
|---|---|---|---|---|
| [P1](#p1) | Open a context to the first rows of the Pods list, 1,000-pod cluster | ≤ 1 s | **pass** (2 of 2) | `TestBudgetFirstRows` |
| [P2](#p2) | Sort a 10,000-pod list by a column | ≤ 50 ms | **pass** (2 of 2) | `BenchmarkSortChange10k`, `BenchmarkSortCommand10k` |
| P3 | Scroll a 10,000-pod list | every frame ≤ 16 ms | not measured | – |
| [P4](#p4) | Apply a burst of 1,000 watch events per second to an open list | no frame over 16 ms | **pass** (3 of 3) | `BenchmarkSnapshot10k` |
| [P5](#p5) | Follow a log stream at 10,000 lines per second | no frame over 16 ms; memory bounded by the scrollback limit | **pass** (1 of 1) | `TestLogsP5` |
| P6 | Resident memory with three clusters open on their Pods lists | track only until measured against Lens | not measured | – |
| P7 | Cold start to an interactive window | ≤ 1 s | not measured | – |
| [P8](#p8) | Type into a 5 MB YAML document in the editor, highlighting on | keystroke to repaint ≤ 16 ms | **pass** (1 of 1) | `BenchmarkKeystroke5MB`, `BenchmarkKeystrokeLayout5MB` |
| [P9](#p9) | A view stays open on an idle cluster for 10 minutes | 0 list requests; only the view's watches, and since 2026-10-16 the connection's heartbeat, one `get` of … | **pass** (2 of 2) | `TestBudgetIdleViewAndWarmReopen` |
| [P10](#p10) | Reopen a view within the warm period | 0 list requests; rows shown ≤ 100 ms | **pass** (4 of 4) | `TestBudgetIdleViewAndWarmReopen` |
| [P11](#p11) | An agent asks `ui_state`, `resources_find` and `ui_navigate` for an object in a warm cache | 0 API requests | **pass** (1 of 1) | `TestShowcaseServiceByClusterIP` |
| [P12](#p12) | Metrics: a Pods list is open on a cluster with a metrics-server; a pod's drawer is open; nothing is open | Pods list: 1 `list pods.metrics.k8s.io` per 60 s; drawer: 1 `get pods.metrics.k8s.io` per 15 s; discovery … | **pass** (1 of 1) | `TestBudgetNamespaceLimitedUser` |
| [P13](#p13) | Metrics with a Prometheus behind the API server's service proxy and a Metrics Server (the normal case since … | Pods list: 0 Prometheus requests while the Metrics Server answers (2 instant queries per 60 s only while it … | **pass** (2 of 2) | `TestNodesListDiskBudget` |
| [P14](#p14) | The cluster Overview (G12, 2026-10-13) is open on a cluster with a Metrics Server and a Prometheus; its range … | open: 1 Prometheus probe (when not detected yet) and 1 history of 4 range queries (CPU and memory over every … | **pass** (3 of 3) | `TestClusterBudget` |
| [P15](#p15) | A user who may list pods only in two namespaces, named as the cluster's accessible namespaces, opens the Pods … | the restricted user: one watch per namespace, no cluster-wide list or watch once the access reviews said no … | **pass** (2 of 2) | `TestBudgetAccessibleNamespaces` |
| [P16](#p16) | An agent reads an object or a log for the first time in a session, then again (H-82, the owner's option b … | the first read: the Secrets' one streaming watch (a namespace-limited user's per namespace), nothing else … | **pass** (3 of 3) | `TestBudgetAgentReadTakesTheSecrets` |

## Measurements

### P1

Open a context to the first rows of the Pods list, 1,000-pod cluster

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| from Connect to the first rows in the Pods view, 1000 pods | 108 ms | at most 1000 ms | pass | `TestBudgetFirstRows/1000` |
| from Connect to all rows in the Pods view, 1000 pods | 108 ms | at most 1000 ms | pass | `TestBudgetFirstRows/1000` |
| from Connect to the first rows in the Pods view, 10000 pods | 286 ms | context | – | `TestBudgetFirstRows/10000` |
| from Connect to all rows in the Pods view, 10000 pods | 375 ms | context | – | `TestBudgetFirstRows/10000` |

- `TestBudgetFirstRows`: the engine on the real client-go stack against testkit's fake API server over HTTPS on the loopback; no window, no real cluster's round trips.

### P2

Sort a 10,000-pod list by a column

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| re-sort a 10,000-pod view by another column from warm rows | 6.75 ms | at most 50 ms | pass | `BenchmarkSortChange10k` |
| a sort command to the new 10,000-row snapshot: the UI state change, the rebuild, the table event | 11.5 ms | at most 50 ms | pass | `BenchmarkSortCommand10k` |

- `BenchmarkSortChange10k`: the benchmark's time per operation.
- `BenchmarkSortCommand10k`: the benchmark's time per operation.

### P4

Apply a burst of 1,000 watch events per second to an open list

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| a 10,000-row snapshot from warm rows (name), the engine's share of a frame | 3.32 ms | at most 16 ms | pass | `BenchmarkSnapshot10k/name` |
| a 10,000-row snapshot from warm rows (namespaces), the engine's share of a frame | 2.1 ms | at most 16 ms | pass | `BenchmarkSnapshot10k/namespaces` |
| a 10,000-row snapshot from warm rows (filter), the engine's share of a frame | 4.6 ms | at most 16 ms | pass | `BenchmarkSnapshot10k/filter` |

- `BenchmarkSnapshot10k`: the benchmark's time per operation.

### P5

Follow a log stream at 10,000 lines per second

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| the slowest log batch on the main goroutine at 10,000 lines/s | 2.39 ms | at most 16 ms | pass | `TestLogsP5` |
| lines per second fed and shown | 9973 lines/s | context | – | `TestLogsP5` |
| heap growth with 10000 kept lines, the scrollback's limit | 13.4 MiB | context | – | `TestLogsP5` |

- `TestLogsP5`: offscreen: Fyne's test driver, the flusher's apply on the caller standing in for the main goroutine; no GPU frame.

### P8

Type into a 5 MB YAML document in the editor, highlighting on

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| the same with a full software paint of a 900×700 window, which overstates a GPU frame | 25.4 ms | context | – | `BenchmarkKeystroke5MB` |
| a keystroke in the 5 MB document, highlighting on: the widget's work without the paint | 0.0152 ms | at most 16 ms | pass | `BenchmarkKeystrokeLayout5MB` |

- `BenchmarkKeystroke5MB`: the benchmark's time per operation.
- `BenchmarkKeystrokeLayout5MB`: the benchmark's time per operation.

### P9

A view stays open on an idle cluster for 10 minutes

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| requests while an open view stays idle for 2s (watchlist) | 0 requests | at most 0 requests | pass | `TestBudgetIdleViewAndWarmReopen/watchlist` |
| requests while an open view stays idle for 2s (list and watch) | 0 requests | at most 0 requests | pass | `TestBudgetIdleViewAndWarmReopen/list_and_watch` |

- `TestBudgetIdleViewAndWarmReopen`: the cache alone over the real client-go stack against the fake API server, counted by the client's counting transport; not 10 minutes.

### P10

Reopen a view within the warm period

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| rows shown after reopening the view in its warm period (watchlist) | 0.0187 ms | at most 100 ms | pass | `TestBudgetIdleViewAndWarmReopen/watchlist` |
| requests of reopening the view in its warm period (watchlist) | 0 requests | at most 0 requests | pass | `TestBudgetIdleViewAndWarmReopen/watchlist` |
| rows shown after reopening the view in its warm period (list and watch) | 0.0151 ms | at most 100 ms | pass | `TestBudgetIdleViewAndWarmReopen/list_and_watch` |
| requests of reopening the view in its warm period (list and watch) | 0 requests | at most 0 requests | pass | `TestBudgetIdleViewAndWarmReopen/list_and_watch` |

- `TestBudgetIdleViewAndWarmReopen`: the cache's handle synced with its rows, no window.

### P11

An agent asks `ui_state`, `resources_find` and `ui_navigate` for an object in a warm cache

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| lists and watches of ui_state, resources_find and ui_navigate on a warm cache | 0 requests | at most 0 requests | pass | `TestShowcaseServiceByClusterIP` |

- `TestShowcaseServiceByClusterIP`: an MCP client over loopback HTTP, the real engine over an in-memory source, counted from a settled cache to a settled cache.

### P12

Metrics: a Pods list is open on a cluster with a metrics-server; a pod's drawer is open; nothing is open

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| pod metrics lists of a namespace-limited user's Pods view in its first list interval (in its namespace, none in all) | 1 request | at most 1 request | pass | `TestBudgetNamespaceLimitedUser/a_RoleBinding_in_the_context's_namespace_alone` |

- `TestBudgetNamespaceLimitedUser`: the engine and the metrics hub against the fake API server, which enforces RBAC and counts reads per namespace.

### P13

Metrics with a Prometheus behind the API server's service proxy and a Metrics Server (the normal case since the fourth slice of G12, 2026-10-12): a Pods list is open; a pod's drawer is open (charts of the default hour) showing CPU or Memory; a chart shows the network or the filesystem; a chart's range changes; the Metrics Server fails; nothing is open

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| Prometheus probes of one detection, the Nodes list's Disk shown | 1 request | at most 1 request | pass | `TestNodesListDiskBudget` |
| Prometheus queries beside the disk query in 3 list polls | 0 requests | at most 0 requests | pass | `TestNodesListDiskBudget` |

- `TestNodesListDiskBudget`: the engine against the fake API server with the Metrics Server's node metrics and a fake Prometheus behind its service proxy, the list interval shortened to 0.7 s.

### P14

The cluster Overview (G12, 2026-10-13) is open on a cluster with a Metrics Server and a Prometheus; its range changes; it closes

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| Prometheus requests of the open Overview in 4 polls: 1 probe, 4 range queries and the root cgroups' 4 | 9 requests | at most 9 requests | pass | `TestClusterBudget` |
| Prometheus requests of a range change | 4 requests | at most 4 requests | pass | `TestClusterBudget` |
| requests in 450 ms after the Overview closed | 0 requests | at most 0 requests | pass | `TestClusterBudget` |

- `TestClusterBudget`: the hub over the real client-go stack against the fake API server and a fake Prometheus (no node-exporter) behind its service proxy, the intervals shortened to 0.2 s.

### P15

A user who may list pods only in two namespaces, named as the cluster's accessible namespaces, opens the Pods view (G3, 2026-10-13, night run); a cluster-admin opens it

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| pod reads of a user limited to two named namespaces (one watch each, none cluster-wide) | 2 requests | at most 2 requests | pass | `TestBudgetAccessibleNamespaces/a_user_limited_to_two_namespaces` |
| requests of that user's open Pods view idle for 2s | 0 requests | at most 0 requests | pass | `TestBudgetAccessibleNamespaces/a_user_limited_to_two_namespaces` |

- `TestBudgetAccessibleNamespaces`: the engine against the fake API server, which enforces RBAC and counts reads per namespace.

### P16

An agent reads an object or a log for the first time in a session, then again (H-82, the owner's option b, 2026-10-14)

| Measurement | Value | Limit | Result | Test |
|---|---:|---|---|---|
| Secrets requests of the window's read of a pod | 0 requests | at most 0 requests | pass | `TestBudgetAgentReadTakesTheSecrets` |
| Secrets requests of an agent's first read of a pod | 1 request | at most 1 request | pass | `TestBudgetAgentReadTakesTheSecrets` |
| Secrets requests of the second read within the warm period | 0 requests | at most 0 requests | pass | `TestBudgetAgentReadTakesTheSecrets` |

- `TestBudgetAgentReadTakesTheSecrets`: the real client-go stack against the fake API server over HTTPS, which streams lists (one watchlist); without streaming lists client-go lists and watches.

## Not measured by make scorecard

- **P3** (Scroll a 10,000-pod list): a real window's frames while scrolling; offscreen tests have no frames (measured by hand in the spike, S1).
- **P6** (Resident memory with three clusters open on their Pods lists): resident memory, to be tracked against Lens Desktop on the same machine; no test measures it.
- **P7** (Cold start to an interactive window): a cold start to a real window; no test starts a window (measured by hand in the spike).

The cluster tier's measurements (kind, Prometheus, the Metrics Server) are not part of a run: they need
`make kind-up`; testing.md records them by hand.
