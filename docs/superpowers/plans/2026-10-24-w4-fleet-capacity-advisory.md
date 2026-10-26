# W4 Fleet Capacity Advisory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a ROS-native, history-backed HCP fleet-capacity advisory with per-HC evidence in the current Remote path and a future-compatible collection/engine boundary.

**Architecture:** KMO's ROS-enabled collector emits a bounded, timestamped snapshot of raw management-cluster inputs and optional MCE gauge references through the existing ROS CSV path. `librobne/hcp` computes published-sizing scenarios; ROS stores history, serves RBAC-scoped fleet endpoints, and renders fleet-wide and per-HC views in the existing HCP tab. Local Mode (#138), cross-tenant M3 routing (#620), and additional platform outreach (#398) are not W4 core dependencies.

**Tech Stack:** Go, `librobne`, KMO Prometheus/Kubernetes clients, ROS CSV ingestion, PostgreSQL/pgx, Echo, OpenAPI JSON, React/TypeScript, PatternFly 6, Jest.

**Spec:** `docs/superpowers/specs/2026-10-24-w4-fleet-capacity-advisory-design.md`

## Global Constraints

- No lab-calibrated universal `N`; name the sizing method and assumptions for every displayed scenario.
- Native sizing baselines: 5 vCPU / 18 GiB requests / 75 pods per HA HC; low/medium/high QPS 50/1000/2000; idle usage 2.9 vCPU / 11.1 GiB; increments 9 vCPU / 2.5 GiB per 1000 QPS. Native CPU/memory budgets use eligible worker `Allocatable` as ADR-0334 specifies; pod density uses eligible worker `maxPods`. Capacity is the minimum applicable CPU, memory, and pod limit.
- MCE reference gauges use the inspected MCE worker `status.capacity` model; because native W4 uses allocatable, numeric parity is not expected and both sources stay separately labeled. MCE's average-QPS gauge seeds its total with the 50-QPS floor ((50+Σ)/N — upstream quirk); native uses the plain mean Σ/N and must not replicate the quirk.
- Native results and MCE output gauges are separate labeled sources; never cross-model blend.
- Count all visible HostedCluster objects for occupancy; HC list failure is incomplete, never zero. The MCE average-QPS reference is Ready-HCP-derived.
- **Emission gating:** the W4 file is emitted only where the HyperShift APIs are queryable. `isMissingAPIError` on the HC/HCP list means "not an HCP cluster" — emit **no file** (KMO topology-collector precedent: missing API is not an error); only a real list error with the APIs present is incomplete inventory. A management cluster with zero HCs is a valid subject and emits.
- **Window semantics (owner decision 2026-10-25):** W4 captures **hourly**, beside the SLO/APITax/Nodepool hourly snapshot collectors — not the 15-minute usage quarters — and only the current report window. `observed_at` is the window end, every instant query is evaluated at that single timestamp, and windows whose end is older than one capture interval (backfill/catch-up after downtime) are skipped — Kubernetes inventory cannot be reconstructed for the past. Replay of the same `(org, cluster, observed_at)` is idempotent.
- **Freshness clock (owner decision 2026-10-25):** a snapshot is stale when its age exceeds 2× the effective cadence, effective cadence = max(hourly capture, upload cycle). The snapshot carries `collection_interval_minutes` and `upload_cycle_minutes` so serving never guesses. Chosen over upload-aligned capture, which would coarsen history granularity; two bare collection intervals were rejected because the default 360-minute upload cycle would leave `latest` stale most of the time. Affirmative management pressure suppresses precise added-HC advice; unknown pressure is explicitly `not assessed`.
- **Availability basis:** the illustrative-only warning keys on the hosted control planes' availability (`ControllerAvailabilityPolicy` aggregate: `highly_available`, `mixed`, `single_replica`, `unknown`) — never the management cluster's `controlPlaneTopology`. Mixed or single-replica fleets warn (ADR-0334 rule 2). Compact/shared-infrastructure management clusters (HCP pods on management control-plane nodes, OCP docs §3.2.5) carry the shared-infrastructure caveat on the same status.
- **Average-QPS tri-state:** no Ready HCPs → low-QPS (50) basis; Ready HCPs present but the per-HCP query fails or returns nothing → the `average_qps` scenario is unavailable, never the 50 basis. Rate window is `[2m]` (matches MCE); namespaces come from HostedControlPlane objects, not the W0 label list. Without MCE-configured monitoring the per-HCP `apiserver_request_total` series may be absent — capability-detected.
- **Eligible workers:** default placement — no `hypershift.openshift.io/control-plane` labels/taints and no `HostedCluster.spec.nodeSelector` — means every worker-role node is eligible and `worker_placement_known=true`; the fail-closed `false` is for genuinely ambiguous constraint sets, not the common default. NodePool placement places hosted compute nodes and is irrelevant to the eligible set.
- **Data sources (owner decision 2026-10-25):** the Kubernetes API supplies nodes (allocatable, maxPods, labels, taints), HC/HCP inventory, and pressure conditions — the nodepool/snapshot-inventory/topology-collector precedent, and how MCE itself computes its gauges; Prometheus supplies only the per-HCP QPS rates and the MCE reference gauges. Do not depend on kube-state-metrics allowlists.
- Pressure conditions are evaluated across **all** nodes in the cluster (management control-plane nodes included; owner decision 2026-10-25), not only the eligible worker set.
- **MCE reference staleness:** series absence (addon agent down) already invalidates the reference; a live-but-stuck agent can serve frozen values with advancing scrape timestamps — ROS surfaces "MCE reference may be stale" when reference values are unchanged across the full freshness window while native inputs changed (caution, not suppression).
- **Version floor (owner decision 2026-10-25):** documented floor is OCP management 4.17+ / MCE 2.6+ for compatible matrix pairs; the runtime gate is capability detection. A known `ocp_version` below 4.17 serves with an explicit below-documented-floor warning, never suppression — native output does not depend on MCE version. `mce_version` is best-effort (reading the MultiClusterEngine CR is out of RBAC scope; unknown is acceptable); the MCE reference relies on series presence.
- **Ingest trust (owner decision 2026-10-25):** `observed_at` is operator-supplied — ingest rejects rows dated more than 5 minutes ahead of ingest time (a future-dated row cannot keep `latest` permanently fresh); rejected rows are non-fatal file failures.
- **Retention (owner decision 2026-10-25):** persist history with `ROS_HISTORY_RETENTION_DAYS` (default 90 days) via the core `historyRetainedTables` sweep in `internal/engine/retention.go` — the same mechanism as the SLO/APITax/Nodepool rollup tables. A plugin `RetentionProvider` sweep silently delivers the month cutoff (6 months) instead; do not wire `hcp_capacity_history` that way. The trait-contract refactor that would let plugins own history-class tables is tracked separately in [#701](https://github.com/pgarciaq/ros-ocp-backend/issues/701). No new retention knob without approval.
- Preserve org and cluster RBAC. Self-managed operator-tenant UI is in scope; cross-tenant M3 publication is not.
- Implement Remote collection in the current KMO ROS-enabled path; do not wait for #138 or collect in cost-only mode.
- Regression tests must fail for the targeted mechanism and follow `docs/agents/adversarial-fixtures.md`; migration/RBAC changes need a reviewer to ask how the tests could pass while the bug remains.
- Do not commit or push any repository without explicit approval for that repository and change.

## Review Focus

1. **HyperShift APIs absent vs list error:** absent → no W4 file at all (not an HCP cluster); a real list error with APIs present → incomplete inventory, never count zero. Pin in collector and API tests.
2. **Backfill/window semantics:** historical catch-up windows are skipped; `observed_at` = window end; one eval timestamp joins every instant query. Pin with a fake multi-window pass.
3. **Freshness clock:** staleness is 2× max(capture, upload) from the row's own interval fields — a 6-hour upload cycle must not mark a fresh hourly row stale. Pin in API tests.
4. **Availability basis:** mixed/single-replica hosted fleets warn illustrative-only; the management cluster's `controlPlaneTopology` never drives the warning. Pin in engine tests.
5. **Average-QPS tri-state:** a failed per-HCP query with Ready HCPs present → scenario unavailable, not the 50 basis; no Ready HCPs → 50 basis. Pin in collector and engine tests.
6. **Lifecycle/placement:** occupancy includes progressing, degraded, paused, zero-NodePool, and deleting HCs; MCE average QPS includes Ready HCPs only; default placement makes all worker-role nodes eligible; unknown placement means no native headroom. Pin these rules separately.
7. **Missing, stale, duplicate, or frozen MCE series:** native math remains independent; invalid references are unavailable, never zero-filled or arbitrarily selected. Pin in collector tests.
8. **Pressure true vs unknown:** affirmative pressure withholds precise headroom; unknown pressure is a warning, never silently clear. Pin in engine/API tests.
9. **Tenant/cluster authorization and history:** identical UUIDs in two orgs must not cross-read; history applies the same RBAC as current capacity (`rbac.AddRBACFilter` with `ResourceContainer`, the same helper the HCP surface uses). Pin with adversarial API fixtures.
10. **Latest-per-cluster fan-out:** the unfiltered latest endpoint returns exactly one row per authorized cluster (latest by `observed_at`, deterministic tiebreak); history requires exactly one cluster. Pin with same-timestamp collision fixtures.

---

### Task 1: Amend ADR-0334; reconcile the decision indexes

**Files:**
- Modify: `docs/adr/0334-fleet-admission-headroom-w4.md` — append the approved W4 implementation amendment while preserving the original R5 record.
- Modify: `docs/adr/README.md` — mark ADR-0334 amended and update its summary.
- Modify: `docs-site/architecture/adrs.md` — align the public ADR index summary with the amended decision.
- Modify: `docs/plans/hcp-fleet-optimization.md` — replace stale W4 backlog/MCE-primary status with the approved native-model direction.
- Modify: `docs-site/planned-features/hosted-control-plane-fleet-optimization.md` — align the public W4 status and direction; do not claim shipped implementation.
- Comment: #392 — record the owner-approved design/implementation greenlight and link the plan and #700; preserve the issue body.

**Interfaces:**
- Consumes: approved W4 design.
- Produces: a dated amendment to existing ADR-0334 (no new ADR number), preserving its original R5 rationale and recording the current native-model, MCE-reference, audience, lifecycle, version-floor, cadence/data-sources/emission-gating, pressure-unknown, history, and non-blocking issue decisions.

- [x] **Step 1: Amend ADR-0334 without rewriting its history** — add an `Amendment (2026-10-24, W4 design approval; freshness clock amended 2026-10-25)` section. Explicitly supersede “prefer MCE / do not rebuild a capacity engine” as the current implementation rule; retain MCE as an optional reference. Narrow the customer boundary to cross-tenant M3 delivery; record OCP management 4.17+ / MCE 2.6+ within the official pair matrix, all-visible-HC conservative occupancy, Ready-HCP average-QPS semantics, the effective-cadence freshness clock, the hosted-availability warning basis, hourly capture cadence, the Kubernetes-API-first data-source split, emission gating on HyperShift API presence, pressure-unknown behavior, and history retention.
- [x] **Step 2: Reconcile indexes and roadmaps** — mark ADR-0334 amended in both ADR indexes, and replace stale W4 “not greenlit/MCE-primary” wording in internal/public roadmap pages with “design approved; native engine + MCE reference; implementation plan underway.”
- [x] **Step 3: Verify docs** — run `make docs-lint docs-drift docs-sync-check` and `git diff --check`.
- [x] **Step 4: Correct tracker status** — add the approved W4 status comment to #392 without editing the body.

### Task 2: Native HCP capacity math in `librobne/hcp`

**Files:**
- Create: `librobne/hcp/capacity.go` — define the pure HCP capacity inputs, five scenario calculations, limiting-resource explanation, and headroom result.
- Create: `librobne/hcp/capacity_test.go` — pin formula, units, scenario ordering, and invalid-input behavior.

**Interfaces:**
- `WorkerCapacity { CPUAllocatableMillicores int64; MemoryAllocatableBytes int64; MaxPods int64; PlacementKnown bool }` for the worker set eligible to host HCP control planes.
- `CapacityInputs { Workers WorkerCapacity; CurrentHCCount int64; InventoryComplete bool; ReadyHCPCount int64; AverageReadyHCPQPS float64; ReadyHCPQPSKnown bool; HostedAvailabilityBasis string }`. `HostedAvailabilityBasis` is the hosted control planes' `ControllerAvailabilityPolicy` aggregate (`highly_available`, `mixed`, `single_replica`, `unknown`) — never the management cluster's `controlPlaneTopology`.
- `CalculateCapacity(in CapacityInputs) (CapacityResult, error)` returns `CapacityResult{ModelVersion: "hcp-published-sizing-v1", Scenarios: []CapacityScenario}` in stable scenario order (`request`, `low_qps`, `average_qps`, `medium_qps`, `high_qps`); each scenario has `Name`, `MaxHCs int64`, optional signed `Headroom *int64`, `LimitingResource`, and `Status`. Incomplete inventory or unknown worker placement yields nil headroom on every scenario; `ReadyHCPQPSKnown=false` yields nil headroom on the `average_qps` scenario only; invalid/non-finite inputs return an error.
- Pressure is a separate gate/status, not part of the pure sizing formula. MCE reference values remain outside `CapacityInputs`/native outputs.
- Formula contract: request is `floor(min(eligible worker CPU allocatable / 5 vCPU, eligible worker memory allocatable / 18 GiB, eligible worker maxPods / 75))`; QPS scenarios take the minimum of those request/pod limits and CPU/memory usage limits using idle 2.9 vCPU/11.1 GiB plus 9 vCPU/2.5 GiB per 1000 QPS. Convert bytes to binary GiB for the native published-sizing model. Average-QPS: `ReadyHCPCount=0 && ReadyHCPQPSKnown=true` uses the 50-QPS low basis; `ReadyHCPQPSKnown=false` makes only that scenario unavailable. Native average is the plain mean Σ/N — do not replicate MCE's 50-seeded-total quirk ((50+Σ)/N). `HostedAvailabilityBasis` of `mixed` or `single_replica` marks scenarios illustrative-only (warning status; math unchanged, per ADR-0334 rule 2), with the shared-infrastructure caveat riding the same status.

- [x] **Step 1: Write failing table tests** — pin request/low/average/medium/high formulas, floor behavior, limiting resource, the zero-Ready-HCP average fallback to low QPS, the average-unknown (`ReadyHCPQPSKnown=false`) unavailable case, mixed and single-replica availability warnings, and signed `max-current` headroom.
- [x] **Step 2: Verify red** — `go test ./librobne/hcp -run 'TestCalculateCapacity' -count=1`; failures must identify missing math or wrong limiting resource.
- [x] **Step 3: Implement pure sizing functions** — use the ADR-0334 published baselines, integer-first CPU/memory inputs, and no Kubernetes or Prometheus dependencies.
- [x] **Step 4: Add incomplete/invalid-input tests** — HC inventory incomplete, worker placement unknown, zero eligible workers, negative capacity, NaN/Inf QPS, over-capacity negative signed headroom, and the management-`controlPlaneTopology` input never influencing the availability warning.
- [x] **Step 5: Verify** — `go test ./librobne/hcp ./librobne/engine`.

### Task 3: KMO Remote-mode bounded input snapshot

**Files (koku-metrics-operator repo):**
- Create: `internal/collector/hcp_capacity_collector.go` — query the Kubernetes API via `rest.Config` for node capacity/labels/taints, HC/HCP inventory, and node pressure (nodepool/topology-collector precedent; RBAC already provisioned), and Prometheus for the per-HCP QPS rates and optional MCE references, folded into one bounded observation.
- Create: `internal/collector/hcp_capacity_collector_test.go` — exercise query freshness, duplicate/missing series, lifecycle counts, and unknown pressure.
- Modify: `internal/collector/collector.go` — invoke W4 capture hourly in the existing ROS-enabled collection path, beside the SLO/APITax/Nodepool hourly snapshot collectors, without tying observation time to upload packaging.
- Modify: `internal/controller/costmanagementmetricsconfig_controller.go` — only if needed to pass the existing collection window/config into the new collector; do not add a new CRD field by default.
- Modify: `config/rbac/role.yaml` — only if the collector's required node/HC/HCP reads are not already authorized; keep any added permission least-privilege and review it separately.
- Modify: packaging tests — assert the W4 filename is included in manifest `resource_optimization_files` if the existing prefix handling does not already cover it.

**Interfaces:**
- Consumes: the ROS-enabled CR; the Kubernetes API via `rest.Config` (nodes, HC/HCP inventory, pressure); the Prometheus client (per-HCP QPS rates, MCE gauges).
- Produces: one bounded `ros-openshift-hcp-capacity-<window>.csv` row per **current-window** observation with fields `observed_at` (window end), `mce_sampled_at`, `operator_version`, `ocp_version`, optional `mce_version`, `native_model_version`, `collection_interval_minutes`, `upload_cycle_minutes`, `inventory_complete`, `current_hc_count`, `ready_hcp_count`, `average_ready_hcp_qps`, `ready_hcp_qps_known`, `hosted_availability_basis`, `eligible_worker_cpu_allocatable_millicores`, `eligible_worker_memory_allocatable_bytes`, `eligible_worker_max_pods`, `worker_placement_known`, `pressure_state`, and nullable `mce_{request,low_qps,average_qps,medium_qps,high_qps}_max_hcs`. `cluster_uuid` remains trusted Kafka/report metadata, not a CSV field. Read the MCE version from a narrowly scoped MCE/CSV source when available; otherwise record it as unknown without blocking native output or broadening RBAC. Pressure is `pressured` when node Memory/Disk/PID pressure is observed on any node in the cluster; otherwise `unknown` until signal coverage is verified. Do not invent numeric thresholds.

- [x] **Step 1: Write failing collector tests** — fake the Prometheus/Kubernetes clients; prove all visible HCs count regardless of phase/deletion/NodePool; prove `isMissingAPIError` emits no file while a real list error with the APIs present marks inventory incomplete; prove backfilled windows older than one capture interval are skipped, `observed_at` equals the window end, and every instant query is evaluated at that single timestamp; verify eligible-node selection for the default no-labels/taints/nodeSelector case (all worker-role nodes, `worker_placement_known=true`) and for a genuinely ambiguous constraint set (`false`); prove a failed per-HCP QPS query with Ready HCPs present sets `ready_hcp_qps_known=false` rather than applying the 50 basis; missing/duplicate or timestamp-misaligned MCE series only invalidate the reference; and represent unavailable pressure as unknown across all nodes.
- [x] **Step 2: Verify red** — `go test ./internal/collector -run 'TestHCPCapacity' -count=1` in koku-metrics-operator.
- [x] **Step 3: Implement collection** — use the current ROS collection gate; query documented worker/HC/QPS inputs and optional MCE reference gauges. Emit only where the HyperShift APIs are queryable (`isMissingAPIError` → no file) and only for the current window (skip windows whose end is older than one capture interval); evaluate all instant queries at the window-end timestamp; carry `collection_interval_minutes` and `upload_cycle_minutes` from CR status. Do not couple capture cadence to `upload_cycle`. Capture the hour window at the top of `GenerateReports` alongside `sloHourStart/sloHourEnd` — the 15-minute quarter loop later mutates the shared `TimeSeries` pointer (#644 precedent).
- [x] **Step 4: Add file packaging contract test** — verify the W4 file is manifest-listed under `resource_optimization_files`; no CRD/status or Koku Kafka metadata changes.
- [x] **Step 5: Verify** — `go test ./internal/collector ./internal/packaging ./internal/controller` in koku-metrics-operator.

### Task 4: ROS CSV dispatch, ingestion, and history storage

**Files:**
- Modify: `internal/types/kafkaMsg.go` — add the distinct W4 CSV payload identifier.
- Modify: `internal/utils/utils.go` and `internal/utils/determine_csv_type_test.go` — classify the longest `ros-openshift-hcp-capacity-` prefix without colliding with existing ROS CSV types.
- Modify: `internal/services/parallel_ingest.go` — route the W4 sidecar through typed plugin ingestion and keep malformed advisory input from stalling unrelated recommendations.
- Modify: `internal/services/report_processor.go` — use the existing native CSV plugin dispatcher for the W4 payload.
- Modify: `internal/plugins/hcp/plugin.go` and HCP plugin tests — claim the W4 payload, write history, and update the package doc header (it currently documents "no CSVIngestor").
- Modify: `internal/engine/retention.go` — register `hcp_capacity_history` in the core `historyRetainedTables` list so the unconditional 90-day history sweep owns it (SLO/APITax/Nodepool rollup mechanism; the broader trait refactor is #701).
- Create: `internal/ingestion/hcp_capacity.go` — parse/validate the snapshot and persist the typed history row.
- Create: `internal/ingestion/hcp_capacity_test.go` — pin validation, idempotent replay, and tenant-key behavior.
- Create: `migrations/<next>_hcp_capacity_history.up.sql` and `.down.sql` (000215 at plan time — use the next available number at implementation) — create/drop the monthly-partitioned, org/cluster-scoped history table and indexes; the runtime partition-ensure covers the previous, current, and next two months (a window ending in the prior month can be emitted just after midnight on the 1st).
- Create: `internal/model/hcp_capacity.go` and tests — query latest and ordered history rows by org/cluster after RBAC scoping.

**Interfaces:**
- CSV type/prefix: `hcp-capacity` / `ros-openshift-hcp-capacity-`.
- Storage: one append-only row per `(org_id, cluster_uuid, observed_at)` in partitioned `hcp_capacity_history`; typed columns for raw aggregate inputs, `native_model_version`, cadence fields (`collection_interval_minutes`, `upload_cycle_minutes`), five native maximums/limiting resources, five nullable MCE references, `mce_sampled_at`, current HC count/completeness, average-QPS known flag, hosted availability basis, worker-placement/pressure state, and source versions. The latest endpoint reads the newest history row; retention is the core `historyRetainedTables` sweep at `ROS_HISTORY_RETENTION_DAYS` (default 90 days, about 2,160 hourly rows per cluster). Ingest rejects `observed_at` dated more than 5 minutes ahead of ingest time (non-fatal file failure).
- Replay of the same `(org, cluster, observed_at)` is idempotent; same UUID across orgs remains isolated.

- [x] **Step 1: Write failing parser/storage tests** — valid row, missing/invalid optional MCE references, malformed timestamp/value, incomplete inventory, duplicate replay, identical cluster UUID across orgs, and future-dated `observed_at` rejection (an operator-supplied timestamp cannot keep `latest` permanently fresh).
- [x] **Step 2: Verify red** — targeted `go test ./internal/ingestion ./internal/model ./internal/plugins/hcp -run 'HCPCapacity' -count=1`.
- [x] **Step 3: Implement typed dispatch and persistence** — extend the existing HCP plugin's `CSVIngestor` trait (no `RetentionProvider` for this table: the core history sweep owns retention); add the `parallel_ingest` case, history upsert/partition migration, and the `historyRetainedTables` registration. Malformed W4 files are non-fatal to unrelated container recommendations; transient fetch/database failures retain retry behavior.
- [x] **Step 4: Verify the history path** — migration-backed test inserts two timestamps and proves latest ordering plus RBAC/org-key isolation, and that the core history sweep drops old `hcp_capacity_history` partitions at the history cutoff (regression pin: a plugin month-cutoff sweep must never own this table).
- [x] **Step 5: Verify** — targeted tests, `make test-short`, and `make test` (full testcontainers suite) before the migration is considered complete.

### Task 5: HCP capacity and history API

**Files:**
- Create: `internal/api/handlers_hcp_capacity.go` — expose latest and history GET handlers using authenticated org identity and the existing cluster RBAC model.
- Create: `internal/api/handlers_hcp_capacity_test.go` — prove authorized reads, non-disclosing unauthorized behavior, and cross-org isolation.
- Modify: `internal/model/hcp_capacity.go` — add list/latest/history query methods with stable ordering and filters.
- Modify: `internal/plugins/hcp/plugin.go` — register the two live fleet-capacity routes in `RegisterRoutes` (the plugin owns its URL namespace; routes exist exactly when the plugin is enabled).
- Verify: `internal/api/server.go` — the existing `/recommendations/openshift/hcp/*` wildcard 404 guard already covers both fleet-capacity paths when the hcp plugin is off; no new guard lines (pinned by the Step 1 test). Live routes are never registered here.
- Modify: root `openapi.json` — define both operations, filters, response schemas, nullability, and `x-plugin-required`.
- Modify: `internal/api/openapi_contract_test.go` — assert both paths and response schemas are present and plugin-filtered correctly.
- Modify: `CHANGELOG.md` — record the new W4 API surface and its audience/behavior.

**Interfaces:**
- `GET /recommendations/openshift/hcp/fleet-capacity` returns the latest snapshot **per authorized cluster**: with `filter[cluster_uuid]` absent, one row per cluster (the UI card-list discovery surface); with the filter present, exactly that cluster.
- `GET /recommendations/openshift/hcp/fleet-capacity/history?filter[cluster_uuid]=…&limit=…&offset=…` returns ordered historical snapshots; the single-cluster filter is required (history is a per-cluster trend, matching the GPU time-slicing/VM history patterns).
- Both bind `org_id` from authenticated identity and apply `rbac.AddRBACFilter` with `ResourceContainer` (the same helper the HCP surface uses) before any read; cluster filters accept both `filter[cluster]` and `filter[cluster_uuid]` spellings (existing queryparams alias machinery). Response includes `native_model_version`, five native scenarios, optional same-scenario MCE references, sample age, HC/placement completeness, pressure state, and source versions. Affirmative pressure withholds exact headroom; unknown pressure is explicit. Staleness is age > 2× max(`collection_interval_minutes`, `upload_cycle_minutes`) from the row's own fields; a known `ocp_version` below 4.17 adds a below-documented-floor warning without suppression; MCE references unchanged across the full freshness window while native inputs changed add a "may be stale" caution.

- [x] **Step 1: Write failing API/RBAC/OpenAPI tests** — authorized cluster returns latest/history; unauthorized cluster does not disclose existence; same UUID in another org cannot leak; unfiltered latest returns exactly one row per authorized cluster (same-`observed_at` collision fixtures prove no fan-out) while history without the filter is a 400; both `filter[cluster]` and `filter[cluster_uuid]` spellings resolve identically; absent/unknown pressure and nullable MCE references stay explicit; staleness uses 2× max(capture, upload) from the row's own cadence fields (a 6-hour upload cycle must not mark a fresh hourly row stale); a known below-floor `ocp_version` yields the warning without suppression; the disabled-plugin guard 404s both paths when the hcp plugin is off; static paths do not parse as recommendation IDs.
- [x] **Step 2: Verify red** — `go test ./internal/api -run 'TestHCP.*Capacity' -count=1`.
- [x] **Step 3: Implement model query and handlers** — latest is filter-optional (one row per authorized cluster when absent; latest by `observed_at` with a deterministic tiebreak), history requires exactly one cluster UUID and orders by `observed_at`; apply org+cluster authorization before data reads, set `Cache-Control: no-store` for current/history responses.
- [x] **Step 4: Update OpenAPI/changelog** — document route/query parameters, response schema, nullability, and pressure states.
- [x] **Step 5: Verify** — targeted API/OpenAPI tests and `make test-short`.

### Task 6: HCP UI fleet card and per-HC view

**Files (koku-ui repo):**
- Create: `apps/koku-ui-ros/src/api/ros/hcpCapacity.ts` and tests — fetch latest/history by selected cluster and parse source/pressure state.
- Modify: `apps/koku-ui-ros/src/routes/optimizations/optimizationsTable/optimizationsHcpTable/optimizationsHcpTable.tsx` — show the fleet card only in the Hosted-clusters subview while retaining per-HC recommendations below.
- Create: a focused fleet-capacity panel and tests beside `optimizationsHcpTable.tsx` — render scenario values, history/source freshness, and pressure/inventory cautions without duplicating fleet `+N` per HC.
- Modify: `apps/koku-ui-ros/src/locales/messages.ts` — add translated scenario, source, history, and warning strings.
- Modify the HCCM host only if the current HCP tab wrapper cannot render the panel; no new top-level tab by default.

**Interfaces:**
- Consumes: Task 5 API — unfiltered latest for the card list, filtered latest/history on drill-in — plus the HCP page's existing management/hosted sub-scope and cluster query.
- Produces: fleet-wide scenario panels on the existing Hosted-clusters subview — one per management cluster in the org when no cluster filter is set, narrowed to one when a cluster is selected (owner decision 2026-10-25) — plus the per-HC recommendation table/details below. Render the below-documented-floor, MCE-may-be-stale, and shared-infrastructure cautions alongside pressure/inventory warnings. Do not put the aggregate card on the Management-overhead namespace subview or copy fleet headroom to each HC row.

- [x] **Step 1: Write failing Jest tests** — panel appears only on the Hosted-clusters subview; with no cluster filter it renders one card per management cluster in the org and narrows to one when a cluster is selected; five scenarios and source labels render; unavailable values stay unavailable; pressure true hides exact `+N`; pressure unknown shows a caution; below-documented-floor and MCE-may-be-stale cautions render; per-HC rows do not inherit fleet-wide headroom.
- [x] **Step 2: Verify red** — `npm test --workspace=@koku-ui/koku-ui-ros -- --runInBand optimizationsHcpTable`.
- [x] **Step 3: Implement API client and PatternFly panel** — preserve HCP list URL state, cluster filters, existing term/engine projection, and detail links; add translated copy.
- [x] **Step 4: Verify** — touched Jest suites, `npm run lint --workspace=@koku-ui/koku-ui-ros`, and `npm run type-check --workspace=@koku-ui/koku-ui-ros`.

### Task 7: Cross-repo contracts and release verification

**Files/artifacts:**
- `costmgmt-api-cheatsheet`: endpoint section + Bruno requests
- `iqe-ros-ocp-plugin`: REST coverage for current/history routes
- Koku `koku/masu/external/ros_report_shipper.py`: verify generic forwarding; change only if a contract test demonstrates a gap
- Public/internal docs as identified in Task 1 and OpenAPI contract

- [x] **Step 1: Add/update IQE REST coverage** — identity/org scoping, authorized/unauthorized cluster, current and history response, pressure warning, and MCE-reference absence.
- [x] **Step 2: Update cheat sheet/Bruno** — document both endpoints and query parameters; rebuild generated output using that repository's documented command.
- [x] **Step 3: Verify Koku forwarding** — prove the manifest-listed W4 ROS CSV is forwarded unchanged; no shipper code change if existing pass-through holds.
- [x] **Step 4: Run final checks** — KMO targeted Go tests; ROS `make test-short` and `make test`; ROS UI targeted tests/lint/type-check; IQE targeted suite; `make docs-lint docs-drift docs-sync-check`.
- [x] **Step 5: Report completion** — comment on #392 with per-repository files/tests/follow-ups and completion-table verdicts from `docs/agents/cross-repo-contracts.md`; never edit the issue body.

### robne CLI follow-up (separate, non-blocking scope)

The shared `librobne/hcp` calculation is designed to be CLI-consumable, but the W4 delivery plan does not change `robne recommend` output. The CLI currently supports a fixed plugin allowlist and versioned JSON sibling arrays; W4 CLI support additionally needs the snapshot CSV parser/classifier, plugin selection, a JSON envelope version bump, `diff`/`explain`/`validate` behavior, and the public CLI contract/docs. This is filed as [#700](https://github.com/pgarciaq/ros-ocp-backend/issues/700), a separate child of W4 #392 and related to the closed CLI umbrella #99. It gets its own implementation plan after the W4 CSV/engine contract is stable; it does not block #392 API/UI delivery.

## Explicit non-goals

- No new M3 cross-tenant routing or data-sharing implementation (#620 remains separate).
- No Local Mode implementation in #138; keep the W4 input/engine contract reusable for a later adapter.
- No `robne` CLI CSV/output/envelope implementation in W4 core; see follow-up #700.
- No automatic scaling/remediation, per-HC copy of fleet headroom, or lab-derived universal capacity claim.
- No commits or pushes without explicit approval per repository.
