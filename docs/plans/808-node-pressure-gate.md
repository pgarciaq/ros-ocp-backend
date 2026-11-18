# #808 — Node-Pressure Opt-In Gate + Value Filter — Implementation Plan

> The mechanism is verified on the lab (see the issue comments, 2026-11-18):
> the `ros:node_pressure_*` trio is (a) ungated by the opt-in label and
> (b) value-blind — PromQL `and` matches series existence, and the
> `status='true'` condition series always exists (value 0 when healthy),
> so `node_pressure_*` is 1 for every CPU-requested container in every
> namespace, every interval. 6,889 of 6,892 hollow rows carry the
> signature. Fix in the **koku-metrics-operator** repo
> (`~/dev/koku/koku-metrics-operator`, branch
> `pgarciaq-rosocp-superpowers-phase17`).

## Mechanism

Rewrite the three `ros:node_pressure_*` QueryMap entries in
`internal/collector/queries.go`:

1. **The value filter** — `and on(node) (kube_node_status_condition{condition='…',status='true'} == 1)`:
   match on the VALUE 1 (actually pressured), not the series' existence
   (the #681 `and`-shape cannot work for condition metrics).
2. **The opt-in gate** — the standard two-arm form every other container
   query carries, wrapping the (now value-filtered) expression:
   `… * on(namespace) group_left kube_namespace_labels{label_insights_…} or … * on(namespace) group_left() (kube_namespace_labels{label_cost_…} or kube_namespace_labels{label_hypershift_…})`.

`clamp_min(requests, 1)` is kept unchanged — the ROS-side consumer
(`librobne.PressureOrdinal`) treats the cells as `> 0`, so the exact
value is cosmetic (the pre-existing request>1 wart is out of the
demonstrated case, recorded here as a non-goal).

**Golden-surface change (intended):** the opted-in namespace's
`node_pressure_*` cells stop being always-1 and reflect actual
pressure (absent when the node is healthy). Recorded on the issue.

## Tests

The repo's test class for queries is string-level assertion (the mock
Prometheus returns canned results — no PromQL evaluation). The
structural gap that let the trio escape: `TestRosQueriesIncludeHyper-
shiftNamespaces` explicitly SKIPS ungated queries. The new tests:

1. **The structural gate test (the class that would have caught #808):**
   every query in `rosContainerQueries` AND `rosNamespaceQueries` must
   carry an opt-in gate in its assembled QueryString. RED on the
   current tree (the three node-pressure entries fail); future queries
   added without a gate fail automatically.
2. **The value-filter pin:** the three `ros:node_pressure_*` strings
   must contain `status='true'} == 1` and the gate arms.
3. **Lab E2E (close criterion 1):** arm64 build, unique tag, deploy,
   one collection cycle, re-read the CSV — only the opted-in
   namespace's rows; `node_pressure_*` absent on a healthy node.

## Mutations planned

- **M1** (gate dropped from one node-pressure query): test 1 fails.
- **M2** (`== 1` reverted): test 2 fails.

## Deliverables

- `internal/collector/queries.go` — the three rewrites.
- `internal/collector/ros_container_optin_test.go` — tests 1 + 2.
- This plan; the issue comments (mechanism + fix report); CHANGELOG in
  the KMO repo if it keeps one (check).

**Non-goals:** the `clamp_min` value wart; the VM/cluster-wide queries
(ungated by design); any ROS-side change (ingest of empty rows is #808's
belt-and-braces question, belonging to ros-ocp-backend if wanted).
