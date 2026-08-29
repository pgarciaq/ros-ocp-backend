# Changelog

All notable API and behavioral changes to ROS-OCP-Backend are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed

- **Namespace list omits `business_hours` ([#497](https://github.com/pgarciaq/ros-ocp-backend/issues/497)):**
  Unfiltered `GET .../namespaces` no longer nests `business_hours` on list
  rows. Slim projection already omitted it. Detail (`GET .../namespaces/{id}`)
  is unchanged. Fat list DTO is not collapsed (ADR-0294).

### Added

- **Integrating librobne docs ([#499](https://github.com/pgarciaq/ros-ocp-backend/issues/499)):**
  Public MkDocs page for the embeddable engine (import path, zero converters,
  `Recommend*` / `Apply*`, import allow/deny). Site:
  https://pgarciaq.github.io/ros-ocp-backend/architecture/librobne/ .
  Go module stays `github.com/redhatinsights/ros-ocp-backend/librobne`.
  Package `doc.go` on core librobne packages. No engine or API change.

- **Peak hours container/namespace utilization ([#496](https://github.com/pgarciaq/ros-ocp-backend/issues/496)):**
  UI-only. Second Peak hours chart on shared utilization (`business_hours_plots`
  + BH request/limit). All-hours charts stay 24×7. No new API fields.

- **Peak hours Visual Insights ([#494](https://github.com/pgarciaq/ros-ocp-backend/issues/494)):**
  Node and VM detail return sibling `daily_digests_business_hours` (same row
  shape as `daily_digests`; omitted when empty; never merged). Timeslicing
  detail nest copies BH SM/DRAM/tensor/FB averages and catalog `total_fb_mib`
  when replica sizing is present. Lists stay all-hours.

- **GPU MIG list container `id` ([#495](https://github.com/pgarciaq/ros-ocp-backend/issues/495)):**
  `GET .../gpu/mig` rows include `id` (same container recommendation id as
  `GET .../recommendations/openshift/{id}`) and `workload_type`. Duplicate
  `id` values across term (and GPU-model) rows for the same container are expected.
  Group-by rows omit `id`. CSV adds `id` and `workload_type`. No nested
  `business_hours` on this list. No `GET .../gpu/mig/{id}` route.

- **robne CLI BH notification codes 79–82 ([#492](https://github.com/pgarciaq/ros-ocp-backend/issues/492)):**
  `robne recommend` JSON BH siblings with sizing set `notification_codes` to
  **79** (node), **80** (GPU container), **81** (GPU timeslicing), or **82** (VM)
  only. All-hours siblings omit the key. Do not copy the engine catalog.
  Envelope stays **11**. Empty BH arrays stay `[]` with no codes. Stdout
  mapping only — rec structs and PG upsert are unchanged.

- **robne CLI business-hours JSON siblings ([#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487)):**
  YAML `business_hours` with explicit `--plugins node|gpu|vm` is allowed.
  JSON envelope **11** when any of those plugins is on with BH. Sibling keys
  `business_hours_node_recommendations`, `business_hours_gpu_recommendations`,
  `business_hours_gpu_timeslicing_recommendations`, and
  `business_hours_vm_recommendations` are arrays (never `null`); omit the key
  when that plugin is off. Container/namespace BH only still uses envelope
  **10**. CLI siblings are full DTOs (idle/abandoned/SKU/GPU/disk) — **not**
  the product thin nest. Dual-write node/GPU/VM BH **digests**; do not upsert
  BH recs (namespace BH rec overwrite removed). GPU/VM weighting is
  drop-or-full; node scales usage when `0 < w < 1`. Path A + explicit plugin +
  empty BH digests is a hard error; implicit default emits `[]`. Files-path
  weekend + explicit plugin succeeds with empty arrays. `explain --schedule
  business_hours` is unlocked for node/gpu/timeslicing/vm (PVC/quota/snapshot
  still error). `csv`/`table` stay JSON-only when BH is on.

- **VM business-hours detail ([#486](https://github.com/pgarciaq/ros-ocp-backend/issues/486)):**
  Ingest dual-writes `daily_vm_digests` (`all_hours` and `business_hours`) using
  the **namespace** schedule (`ProducesBusinessHoursDigests`). Namespace-only
  enablement **does** produce VM BH (like GPU container, unlike node/timeslicing).
  Weight `<= 0` drops the 15-minute sample; otherwise the **full** sample is
  included (drop-or-full — not container `ComputeWeightedDigest`). Produce/list
  VM queries default to `all_hours`. Persist rec tables stay all-hours (no
  `schedule_type`). Nested `business_hours` is on **GET .../vm/detail** only
  (thin nest: vCPU/GiB + reason + code **82** `VM_BH_OFFICE_WINDOW` when sizing
  is present). List, history, CSV, and group-by stay all-hours. Nested
  `notifications` is the Kruize map; parent VM `notifications` stay a JSON
  array. Reason-only insufficient-data blocks omit 82. Disabled schedule omits
  the object. PVC attaches to the all-hours parent only. Guest GPU devices
  dual-write onto the BH parent; nested detail still omits GPU. The VM plugin
  does **not** implement `APIEnricher`. CLI JSON VM BH siblings are [#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487)
  (full `vmOut`, not this product thin nest).

- **GPU timeslicing business-hours detail ([#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491)):**
  `GET .../gpu/timeslicing/{node}` returns all GPU-model × term rows for one node
  (same row shape as the list). Nested `business_hours` is attached there only
  when org ⊕ cluster is enabled (`ProducesNodeBusinessHoursDigests`) and every
  container in the node × GPU model group uses the cluster window. Mixed
  namespace windows omit the nested object. Namespace-only enablement does not
  produce timeslicing BH. List, history, GPU summary `timeslicing.count`,
  backfill, and container `time_slicing_*` stay all-hours. Persist tables stay
  all-hours (no `schedule_type`); BH is recomputed at read time. Replica sizing
  on the nested object emits notification **81** (`GPU_TS_BH_CLUSTER_WINDOW`).
  Reason-only insufficient-data blocks omit 81. Nested BH never includes dollar
  savings. GPU `APIEnricher` stays rates-only. CLI JSON GPU BH siblings are
  [#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487).

- **GPU business-hours container detail ([#485](https://github.com/pgarciaq/ros-ocp-backend/issues/485)):**
  Ingest dual-writes `gpu_container_digests` (`all_hours` and `business_hours`)
  using the **namespace** schedule (`ProducesBusinessHoursDigests`). Weight `<= 0`
  drops the sample; otherwise the full sample is included (no fractional
  min/max/mean). Produce/list GPU queries default to `all_hours`. Nested
  `business_hours` is on **container detail** `gpu.{term}` only (code **80**
  `GPU_BH_OFFICE_WINDOW` when sizing is present). Container list, MIG list, and
  timeslicing **list** stay all-hours. Timeslicing BH detail is [#491](https://github.com/pgarciaq/ros-ocp-backend/issues/491).
  No workload-type Settings API. GPU `APIEnricher` stays rates-only. CLI JSON GPU BH siblings are [#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487).

- **Node business-hours detail ([#484](https://github.com/pgarciaq/ros-ocp-backend/issues/484)):**
  When an org or cluster business-hours schedule is enabled, ingest dual-writes
  `daily_node_digests` (`all_hours` and `business_hours`). Node list stays
  all-hours. Node **detail** nests `business_hours` on each engine with
  cores/GiB sizing and notification **79** (`NODE_BH_NOT_PEAK_SAFE`) when
  sizing is present. Namespace-only schedules do not dual-write node BH.
  `hourly_node_digests` stays all-hours. No `peak_safe` boolean; code 78 is
  not added to the catalog. CLI JSON node BH siblings are [#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487).

- **robne CLI binary identity / envelope capability ([#489](https://github.com/pgarciaq/ros-ocp-backend/issues/489)):**
  `robne version` prints binary identity and the plugin → envelope bump table
  this binary can emit (`json_envelope_max`, then container=1 … snapshot=9,
  `business_hours`=10). JSON recommend `"version"` stays per-run (ADR-0336);
  container-only is still `1`. `business_hours` in the table is the YAML bump,
  not a `--plugins` name. No `--version` flag. `make robne` injects
  `git describe --always --dirty`; `go test` / `go build` stay `devel`.

- **robne CLI other-entity explain ([#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490)):**
  `robne explain` covers namespace, node, GPU MIG, GPU timeslicing, PVC, VM,
  quota, cluster_quota, and snapshot on the same subcommand as container.
  One entity type per run: `--plugins` is exactly one name (omit for container);
  two or more is a hard error. YAML `plugins:` does not select the type.
  Inapplicable identity flags are hard errors. GPU infers MIG vs timeslicing
  from `--container` vs `--node` (no `--kind`; both set is an error).
  `--schedule business_hours` is container, namespace, node, gpu, and vm
  ([#487](https://github.com/pgarciaq/ros-ocp-backend/issues/487)); PVC/quota/snapshot still error. CLI-owned
  snake_case DTOs (`*_bp` / `*_mc` / `*_kib`); no `float32` confidence; PVC
  includes `usage_ratio` and `growth_bytes_per_day` from the rec. Do not json-tag
  engine `Expl`. Do not golden full explain JSON.

- **robne CLI Phase 3 diff / container explain ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)):**
  `robne diff LEFT.json RIGHT.json` compares two [#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470)
  recommend envelopes (files on disk; no Postgres, no re-run). Exit 0 identical,
  1 recs or metadata (`cluster_id` / `now` / `skipped_rows`) differ, 2 unreadable
  JSON / version mismatch / duplicate row keys. Empty sibling vs missing key is
  a delta. Rows match by persist identity, not array index. Keep the numeric
  golden `cmd/robne/testdata/golden_short_cost.json`; CI also diffs
  `golden_envelope_v1.json`. `robne explain` re-runs the engine from the same
  `--input` as `recommend` (CSV / dir / tarball / `postgres://`) and prints one
  container’s snake_case explanation DTO. Recommend JSON stays the list (no
  `Expl`, trend slopes, or `float32` confidence). Envelope `--input` is a hard
  error. Other entity types are [#490](https://github.com/pgarciaq/ros-ocp-backend/issues/490).
  `--schedule` defaults to `all_hours`; `business_hours` requires YAML
  `business_hours.enabled`. No new GitHub Actions workflow (`make test` already
  covers `cmd/robne`).

- **robne CLI business-hours digest filtering ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)):**
  YAML `business_hours:` (flat cluster-wide schedule, not Settings JSON nesting)
  unlocks dual digest streams for **container and namespace**. Omit the key for
  compiled default off; overlay replaces the whole key. When `enabled: true`,
  robne always keeps unweighted `all_hours` and adds `schedule_type=business_hours`
  via `librobne/bhschedule` + weighted `DailyDigests` (`librobne/csv` takes a
  callback and does not import `bhschedule`). JSON `version` 10 siblings
  `business_hours_recommendations` and `business_hours_namespace_recommendations`
  (always arrays, never `null`; empty after weighting is still an array).
  `csv`/`table` is a hard error. Overnight (`end_time < start_time`) is allowed;
  equal start/end is not. Invalid IANA timezone is an error. `--now` is
  decay/staleness only. Files → `--output postgres://` dual-writes both digest
  streams and namespace recs for both `schedule_type`s; container recs stay
  all_hours (`recommendation_sets` has no `schedule_type`). Path A SELECTs the
  matching stream (never re-filters stored `all_hours`); empty BH after prune is
  a hard error. Node/GPU/VM BH is [#483](https://github.com/pgarciaq/ros-ocp-backend/issues/483).
  Not Phase 3 ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)).

- **robne CLI snapshot inventory CSV + default-all plugins ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)):**
  `--plugins snapshot` (or implicit default when a snapshot file is classified)
  parses NISE `ocp_snapshot_inventory` / operator `ros-openshift-snapshot-*` /
  `cm-openshift-snapshot-inventory` (classified **before** blanket
  `cm-openshift-*`) in `librobne/csv`. Hourly rows collapse to latest per
  `(namespace, snapshot_name)` then `ClassifySnapshotInventory` with compiled
  `DefaultSnapshotSettings` and CLI `now` (never wall clock). JSON `version` 9
  sibling `snapshot_recommendations` (always an array, never `null`; empty
  `notification_codes` is `[]`). Files-only: `--output postgres://` does not
  write snapshot tables; Path A plus **explicit** snapshot is a hard error
  (implicit default drops snapshot). YAML `snapshot:` stays reserved. Do not
  wrap `internal/ingestion.ParseSnapshotRows`. **Default `--plugins` is all
  shipped plugins** unless `--plugins` or YAML `plugins:` is set. Implicit
  default **skips** missing dedicated CSVs / empty Path A tables; an explicit
  list **errors**. GPU still needs `accelerator_model_name`. csv/table stay one
  entity per stream — pin `--plugins container` when a container ROS file also
  enables node. Not business hours ([#479](https://github.com/pgarciaq/ros-ocp-backend/issues/479)).
  Not Phase 3 ([#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480)).

- **robne CLI other-entity Path A SELECT ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)):**
  `--input postgres://` SELECTs stored other-entity daily digests
  (`librobne/pgdigest.Read*`) and recomputes recs for listed plugins
  (namespace, node, GPU, PVC, VM, quota, cluster_quota). Nested chain
  matches files: quota/CRQ reconstruct supporting container and quota
  days from the CLI-owned DB even when those plugins are off, then emit
  siblings only for listed plugins. Empty own-table SELECT is an error;
  empty nested inputs yield zero aggregates. `end` is `--now` or
  `MaxAnyDigestDate` (never wall clock). `--apply-schema` stays a hard
  error. Same DB `--output` upserts recs only. YAML entity blocks stay
  reserved (comments-only sample/README fix). Not INSERT ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)).
  Not rec SQL ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)).
  Not snapshot. Not business hours.

- **robne CLI other-entity digest INSERT ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)):**
  `--output postgres://` INSERTs other-entity daily digests into the same
  CLI-owned database as [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)
  (`daily_namespace_digests`, `daily_node_digests`, `gpu_container_digests`,
  `daily_pvc_digests`, `daily_vm_digests` + `vm_gpu_device_digests`,
  `daily_namespace_quota_digests`, `daily_cluster_quota_digests`). Slim LWW
  writers live in `librobne/pgdigest` (`ON CONFLICT` replaces the day; not
  ingest `GREATEST`/`LEAST`). Persist whenever the file load already built
  those days (not plugin-gated); rec upsert stays plugin-gated ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)).
  Quota/CRQ persist every `report_date`, not `Latest*Snapshots`. Monthly
  `PARTITION OF` only for RANGE parents (namespace, node, GPU, PVC);
  quota/CRQ/VM stay heap. Processor ingest merge is unchanged. Path B still
  computes other-entity recs from files. Not SELECT / Path A
  ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)). Not recs.
  Not snapshot. Not business hours.

- **robne CLI other-entity rec upsert ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)):**
  `--output postgres://` upserts native rec rows for shipped 2b plugins
  (namespace, node, GPU MIG + time-slicing, PVC, VM, quota, cluster_quota)
  using the same `migrate.Up()` / `source_id=robne` / `EnsureAccountCluster`
  path as 2a. SQL lives in `librobne/pgrec`; processor wrappers call the same
  writers. GPU persist maps in-memory CLI recs (`WriteGPURecs` /
  `WriteNodeGPUTimeslicingRecs`) and does not re-query Postgres or
  `Apply*Savings` (savings stay null, same as stdout). Stale-term `DELETE`
  matches product (node/PVC/VM/time-slicing). Path B persists whenever
  `--output` is set (empty slices are no-ops). Path A still skips file-only
  plugins until [#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482).
  Not digests ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)).
  Not snapshot. Not business hours.

- **robne CLI ClusterResourceQuota CSVs ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)):**
  `--plugins cluster_quota` (or YAML `plugins`) parses NISE
  `*ocp_ros_cluster_quota.csv` and operator `ros-openshift-cluster-quota-*.csv`
  (classified **before** namespace). Missing CRQ CSV is an error; days with no
  hard limits emit an empty `cluster_quota_recommendations` array (never
  `null`). JSON `version` is 8. Empty `namespaces` sums **all** in-memory
  namespace quota recs (product `QueryNamespaceQuotaAggregateForNamespaces`); a
  non-empty comma list filters by membership (two ResourceQuotas in one
  namespace both count). Missing NS/container ROS still emits CRQ recs from
  used vs hard. DTO matches `quotaOut` grain: hard + recommended CPU
  millicores, memory/storage **bytes**, pods; omit used/utilization. Do not
  convert CRQ memory to KiB (#477). Nested chain computes container then quota
  recs in memory when those CSVs are present (even if those plugins are off);
  do not emit their siblings unless listed. YAML `cluster_quota:` stays
  reserved. Do not call `ApplyClusterQuotaSavings` (unset savings stay JSON
  `null`). `--output postgres://` still persists containers only (stderr
  warning). `--input postgres://` skips cluster_quota (stderr warning) or
  errors if it is the only plugin. Remaining 2b under #472 is none — do not
  close. Not #473. Snapshot is not stubbed.

- **robne CLI namespace quota CSVs ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)):**
  `--plugins quota` (or YAML `plugins`) reads the **same** namespace ROS CSV
  as `--plugins namespace` (`ros-openshift-namespace-*`, `ocp_ros_namespace_usage`).
  Rows without `quota_name` (alias `resource_quota_name`) are not quota snapshots
  (NISE usage-only). Named-quota `*_namespace_sum` columns are ResourceQuota hard
  limits. Missing namespace CSV is an error; no named-quota rows emit an empty
  `quota_recommendations` array (never `null`). JSON `version` is 7. One rec per
  namespace×quota_name (no term/engine). If container ROS is in the load, in-memory
  container recs (term `medium`, engine `cost`) feed aggregates even when
  `--plugins container` is off. YAML `quota:` stays reserved. Do not call
  `ApplyQuotaSavings` (unset savings stay JSON `null`). `--output postgres://`
  still persists containers only (stderr warning). `--input postgres://` skips
  quota (stderr warning) or errors if it is the only plugin. Issue #472 stays
  open for cluster_quota. Not #473.

- **robne CLI VM CSVs ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)):**
  `--plugins vm` (or YAML `plugins`) parses ROS VM usage
  (`ros-openshift-vm-usage-*`, `ocp_ros_vm_usage` — classified **before**
  `ocp_ros_usage`). Optional companions (`ros-openshift-vm-pvc-*` /
  `ocp_ros_vm_pvc`, `ros-openshift-vm-gpu-device-*` / `ocp_ros_vm_gpu_device`)
  degrade if missing or malformed. JSON `version` is 6 with `vm_recommendations`
  (empty arrays, never `null`). Timeslicing is a column on each VM row, not a
  second sibling. Overlay container `terms` (1/7/15) apply until YAML `vm:`
  unlocks. YAML `vm:` stays reserved. `--output postgres://` still persists
  containers only (stderr warning). `--input postgres://` skips vm (stderr
  warning) or errors if it is the only plugin. Do not call product VM savings.
  Issue #472 stays open for cluster_quota. Not #473.

- **robne CLI PVC CSVs ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)):**
  `--plugins pvc` (or YAML `plugins`) parses NISE `*ocp_storage_usage.csv`,
  operator `ros-openshift-storage-*.csv`, and `cm-openshift-storage-usage`
  (classified before blanket `cm-openshift-*` cost-only). Daily digests match
  ingest byte-seconds → bytes. JSON `version` is 5 with `pvc_recommendations`
  (empty arrays, never `null`). One rec per PVC×term (no `engine`). YAML `pvc:`
  stays reserved (compiled defaults). `--output postgres://` still persists
  containers only (stderr warning). `--input postgres://` skips pvc (stderr
  warning) or errors if it is the only plugin. Do not call `ApplyPVCSavings`.
  Issue #472 stays open for cluster_quota. Not #473.

- **robne CLI node and GPU CSVs ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)):**
  `--plugins node` / `gpu` (or YAML `plugins`) aggregate node and GPU daily
  digests from **container ROS rows** (optional allocatable and DCGM columns;
  no new file family). JSON `version` is 3 with `node_recommendations` or 4
  with `gpu_recommendations` plus `gpu_timeslicing_recommendations` (empty
  arrays, never `null`). CSV/table stay one entity per stream; GPU CSV is MIG
  rows (timeslicing is JSON-only). YAML `node:` / `gpu:` stay reserved
  (compiled defaults). `--output postgres://` still persists containers only
  (stderr warning). `--input postgres://` skips node/gpu (stderr warning) or
  errors if they are the only plugins. Later #472 slices added PVC, VM, and quota
  stdout; cluster_quota still open. Not #473.

- **robne CLI namespace CSVs ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)):**
  `--plugins namespace` (or YAML `plugins: [container, namespace]`) parses NISE
  `*ocp_ros_namespace_usage.csv` and operator `ros-openshift-namespace-*.csv`,
  computes namespace recs, and writes sibling `namespace_recommendations` on
  JSON `version` 2. Default `--plugins` is still `container` (v1 envelope).
  CSV/table stay one entity per stream. `--output postgres://` still persists
  containers only (stderr warning). `--input postgres://` skips namespace
  (stderr warning) or errors if namespace is the only plugin. Later #472 slices
  added node/GPU, PVC, VM, and quota stdout; cluster_quota still open. Not #473.

- **robne CLI digest SELECT ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)):**
  Files plus `--output postgres://…` INSERT today’s `all_hours` digests, then
  SELECT `[end − MaxWindowDays, end]` (`end` is `--now` or `max(bucket_date)`),
  then recommend and upsert recs. `--input postgres://…` recomputes from stored
  digests (stdout; optional rec upsert; `--apply-schema` is an error). `validate`
  stays files-only. Same DSN if both `--input` and `--output` are Postgres.
  `librobne/pgdigest` holds the SELECT; the processor `loadDigestRows` wrapper
  keeps the ingest timeout and row cap.

- **robne CLI digest INSERT ([#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463)):**
  `robne recommend --output postgres://…` upserts container `all_hours` rows into
  `daily_container_digests` (monthly partitions, last-write-wins) before the
  existing rec upsert. `librobne/pgdigest` holds the SQL; the processor imports
  it.

- **robne CLI Phase 2a ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)):**
  `robne recommend --output postgres://…` upserts full container recs into a
  dedicated database this CLI owns. The binary embeds `migrations/`.
  `--apply-schema` is required to bootstrap or upgrade; daily cron at head
  omits it. `source_id` is always `robne`; any other `clusters.source_id`
  refuses the write. Stdout still prints.

- **robne CLI Phase 1 ([#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469),
  parent [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)):**
  `make robne` builds `bin/robne`. `robne recommend` / `robne validate` read
  NISE or operator ROS container CSVs (directory, file, or `.tar.gz` with `./`
  stripped), overlay YAML (replace whole top-level keys) and `rate-card.json`
  (merge by cluster id), and write JSON/CSV/table on stdout. No PostgreSQL,
  Kafka, or Masu. Parser lives in `librobne/csv` (operator must not import it).

### Fixed

- **Empty-database migrate no longer dies at 000179
  ([#464](https://github.com/pgarciaq/ros-ocp-backend/issues/464)):**
  `000179` no longer `ALTER`s `node_recommendation_history` (that table was
  never created). `000181` drops the ghost table if a testdb/bench stub left
  it behind. Leftover tests and `scripts/explain-audit/seed.sql` write
  `category` instead of the dropped `is_underutilized` / `is_overcommitted`
  columns. Node persist INSERT now binds `expl_sizing_formula` (`$37`) so
  column count matches VALUES.

### Changed

- **Settings API overnight business-hours windows ([#488](https://github.com/pgarciaq/ros-ocp-backend/issues/488)):**
  PUT allows `end_time` before `start_time` (for example Mon–Fri `22:00`–`06:00`).
  Equal start and end still return `400` (zero-width). Classification stays in
  `InBusinessHours` (half-open `[start, end)` in the IANA zone; post-midnight
  samples belong to the previous calendar day's shift). PUT may include a
  non-fatal wrap warning. No migration and no new Unleash flag.

- **robne JSON stdout envelope ([#470](https://github.com/pgarciaq/ros-ocp-backend/issues/470)):**
  `robne recommend --format json` writes a versioned object (`version`,
  `cluster_id`, `now`, `skipped_rows`, `recommendations`) with snake_case row
  keys matching CSV. Unset `estimated_savings_cents` is JSON `null`. Pin
  `--now` in CI. Do not add `json` tags on `librobne/types.ContainerRec`.
  Phase 3 `robne diff` consumes this envelope.

- **P4+ entity compute in librobne ([#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94)):**
  Namespace, snapshot, node, GPU MIG + timeslicing, VM, PVC, namespace quota,
  and cluster quota recommendation compute live in nested `librobne/` packages.
  Product wrappers still query PostgreSQL, apply savings, and persist.
  GPU catalogs embed from `librobne/gpu/`. No user-facing API change.

- **P4b namespace/snapshot load-then-compute ([#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94)):**
  `RecommendNamespaces` and `ClassifySnapshotInventory` take in-memory rows —
  no `*pgxpool.Pool`. Product wrappers still query PostgreSQL and persist.
  Formerly numbered P2. No user-facing API change.

### Documentation

- **robne CLI spec Phase 2 split ([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)):**
  PostgreSQL is **2a** ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471))
  use case (c): embed `migrations/`, `migrate.Up()` (bootstrap + upgrade, never Down),
  ensure cluster from YAML, native container upsert. Not a live Helm DB. Other
  entity CSVs **2b** ([#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472))
  (namespace + node/GPU + PVC + VM + quota + cluster_quota stdout shipped;
  remaining 2b under #472 is none — do not close),
  entity PG **2c** ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)),
  digest **SELECT** **2d** ([#474](https://github.com/pgarciaq/ros-ocp-backend/issues/474)) (shipped).
  Digest **INSERT** (`pgdigest`) is [#463](https://github.com/pgarciaq/ros-ocp-backend/issues/463) (shipped).

- **robne CLI spec for greenlight ([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)):**
  [`docs/plans/robne-cli-spec.md`](docs/plans/robne-cli-spec.md) is the review
  contract (YAML + user overlay with **replace whole top-level keys**, `--now`
  as decay/staleness clock (term windows stay on latest digest day), NISE vs operator columns, rate-card JSON **merge by cluster
  id** and **replace-not-add** `by_architecture` / `by_model`, Phase 1 stdout
  only, PostgreSQL in **2a** ([#471](https://github.com/pgarciaq/ros-ocp-backend/issues/471)), tarball `./` prefix). Samples:
  `cmd/robne/robne.yaml.sample`, `cmd/robne/rate-card.json.sample`. Public
  overlay manual:
  [`docs-site/features/robne-cli.md`](docs-site/features/robne-cli.md)
  (GitHub Pages Features; planned-features URL is a bookmark stub).
  Trackers:
  [#465](https://github.com/pgarciaq/ros-ocp-backend/issues/465) (NISE headers),
  [#466](https://github.com/pgarciaq/ros-ocp-backend/issues/466) (koku tar `./`).

- **robne CLI public page graduated to Features ([#469](https://github.com/pgarciaq/ros-ocp-backend/issues/469)):**
  [`docs-site/features/robne-cli.md`](docs-site/features/robne-cli.md) is the
  user manual. Spec §3 documents `--now` as the decay/staleness clock (term
  windows stay on latest digest day, same as the processor). Old
  [`docs-site/planned-features/robne-cli.md`](docs-site/planned-features/robne-cli.md)
  is a bookmark stub. Incomplete YAML `sizing:` is an error (copy the sample
  block or omit the key). Unparseable CSV data rows are counted on stderr;
  all-unparseable ROS files error.

- **P3/P4 librobne extract ([#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94)):**
  Container recommendation compute lives in nested module
  `github.com/redhatinsights/ros-ocp-backend/librobne` (`replace => ./librobne`).
  `RecommendWorkloads` takes digest rows and an emit callback — no `*pgxpool.Pool`.
  Product wrappers still load PostgreSQL and call `ApplySavingsEstimates` after emit.
  Identical type aliases (no convert loops). No user-facing API change.
  Other entities moved in P4+ (above).

- **P1b container RateCard ([#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94)):**
  Container `ApplySavingsEstimates` takes an integer `RateCard` plus calendar
  projection hours. Koku `effective_rates` map once per cluster in
  `costdata.ClusterCostDataToRateCard` (Tier B spend; no USD default; no markup
  copy). Savings golden ±1 cent; sizing unchanged. Node/GPU/VM/PVC still use
  `ClusterCostData`.

- **Docs IA — pagination, query performance, scale test plan, known issues:**
  Nav moves (URLs unchanged): API Pagination → API Specification; Query Performance
  → Architecture; Scale Test Plan → Testing. `known-issues` retitled **Known
  Limitations** (slim caveats + GitHub issue links). Full feature-status megadoc
  frozen under Historical → [Feature Status Archive](https://github.com/pgarciaq/ros-ocp-backend/blob/main/docs-site/historical/feature-status-archive.md).
  New tracking issues: [#445](https://github.com/pgarciaq/ros-ocp-backend/issues/445)
  (`rh_accounts` joins), [#446](https://github.com/pgarciaq/ros-ocp-backend/issues/446)
  (fleet savings materialization), [#447](https://github.com/pgarciaq/ros-ocp-backend/issues/447)
  (ROS GPU Optimizations UI).

### Added

- **Plugin traits catalog
  ([#420](https://github.com/pgarciaq/ros-ocp-backend/issues/420)):**
  New public page `architecture/plugin-traits.md` (nav: Architecture → Plugin Traits;
  after Plugin Architecture). Stub at old `plugin-reference/traits/` path redirects.
  listing every trait with role, when to use it, and who implements it, plus
  scaffolder defaults. Linked from Local Development, CONTRIBUTING, architecture
  §4, and `cmd/newplugin` checklist.

- **Plugin scaffolder (`make new-plugin` / `go run ./cmd/newplugin`)
  ([#410](https://github.com/pgarciaq/ros-ocp-backend/issues/410)):**
  Generates `internal/plugins/<name>/{plugin.go,plugin_test.go}` with live
  Plugin + APIProvider + RetentionProvider (other traits commented), appends a
  sorted blank import to `internal/plugins/plugins.go`, and prints a checklist.
  Supports `PHASE`, `PRIORITY`, `TRAITS`, and `DRY_RUN`. See Local Development
  and CONTRIBUTING.

- **All nav-section PDF books
  ([#382](https://github.com/pgarciaq/ros-ocp-backend/issues/382)):**
  `./scripts/docs-pdf.sh all` (or `make docs-pdf-all`) builds one PDF per
  top-level MkDocs nav section under gitignored `dist/pdf/`: getting-started,
  features, planned-features, architecture, testing, plugin-reference, api,
  operations, security, ui-integration. Home is skipped. See CONTRIBUTING.md.

- **PDF print CSS + Architecture/Operations books
  ([#381](https://github.com/pgarciaq/ros-ocp-backend/issues/381)):**
  Hardened `scripts/docs-pdf/styles.scss` so tall diagrams/tables/code can
  break across A4 pages (overrides `mkdocs-to-pdf` `page-break-inside: avoid`
  defaults that previously truncated books). `./scripts/docs-pdf.sh` now also
  builds `architecture` and `operations` (`make docs-pdf-architecture`,
  `make docs-pdf-operations`). Known limitations documented in CONTRIBUTING.md.

- **Local Features PDF book generation
  ([#380](https://github.com/pgarciaq/ros-ocp-backend/issues/380)):**
  `./scripts/docs-pdf.sh features` (or `make docs-pdf-features`) builds
  `dist/pdf/features.pdf` from the Features MkDocs nav section. Mermaid
  diagrams are pre-rendered with `mmdc` to PNG (WeasyPrint-safe; Mermaid SVGs
  often use `foreignObject`); PDF uses `mkdocs-to-pdf` (WeasyPrint) so macros
  still expand. Output and work trees are gitignored (`dist/pdf/`,
  `.docs-pdf-work/`). See CONTRIBUTING.md.
- **Multi-currency savings conversion
  ([#364](https://github.com/pgarciaq/ros-ocp-backend/issues/364)):**
  All savings `MoneyAmount` fields across recommendation list, detail, grouped,
  summary, history, and fleet endpoints are now converted from the stored cost
  model currency to the user's preferred display currency at API response time.
  Two new Koku Masu endpoints (`user_currency/`, `exchange_rate/`) provide the
  user's preference and exchange rate. LRU caches (configurable TTL and size)
  avoid repeated HTTP calls. Graceful fallback: when exchange rates are
  unavailable, amounts are returned in the stored currency with no error.
  Requires a coordinated Koku upgrade for the new Masu endpoints.

- **Per-PVC VM shared storage detection
  ([#359](https://github.com/pgarciaq/ros-ocp-backend/issues/359)):**
  New companion CSV (`ros-openshift-vm-pvc-YYYYMM.csv`) from the operator
  carries per-PVC disk allocation data. `DetectSharedPVCs` now uses actual
  PVC name overlap instead of the previous proxy heuristic (namespace +
  placement profile matching). Falls back to proxy detection for legacy
  operator payloads that lack the companion CSV. New `vm_pvc_digests`
  child table (migration 000180). See [ADR-0324](docs/adr/0324-vm-pvc-companion-csv-for-shared-storage-detection.md).

- **Direct-to-MinIO benchmark mode (`scripts/direct_to_minio.py`)
  ([#268](https://github.com/pgarciaq/ros-ocp-backend/issues/268)):**
  New script that bypasses the Koku listener for ROS processor benchmarks by
  uploading nise-generated CSVs directly to MinIO and publishing Kafka messages.
  Reduces benchmark time from hours (listener-bound) to minutes at 10K+ container
  scale. The listener does not transform ROS CSVs — the script replicates its
  upload and notification behavior directly.

- **Comprehensive benchmark config generator (`scripts/gen_benchmark_config.py`):**
  Python script that generates nise YAML configs covering ALL recommendation
  engines: containers (regular, idle/zombie), GPU (time-slicing, MIG), VMs
  (Linux, Windows, idle, abandoned, GPU), PVCs (oversized, near-full, orphaned),
  snapshots (stale, orphaned), namespace quotas, and cluster quotas. Entity
  counts scale proportionally based on the `--containers` parameter.

### Fixed

- **MoneyAmount.Units now reflects resolved currency in all API responses
  ([#363](https://github.com/pgarciaq/ros-ocp-backend/issues/363)):**
  Previously, `MoneyAmount.Units` was hardcoded to "USD" in 14+ API response
  formatters even when `meta.currency` reported a different currency from the
  cost model. Currency is now threaded from the API handler through the model
  layer so `MoneyAmount.Units` matches the resolved cost-model currency at
  creation time. Enrichment functions also patch `Units` on detail endpoints
  where the cluster UUID is unknown until query time. Affects container,
  namespace, PVC, snapshot, node utilization, quota, and history endpoints.

### Changed

- **Calendar-accurate monthly hours for savings extrapolation
  ([#316](https://github.com/pgarciaq/ros-ocp-backend/issues/316)):**
  Replace the fixed `730` hours/month constant with `HoursInMonth(year, month)`
  which returns `daysInMonth × 24` for the current calendar month. Affects
  container, node, VM, namespace, and quota savings. PVC and snapshot savings
  (which use flat monthly rates) are unaffected. Supersedes
  [ADR-0182](docs/adr/0182-monthly-savings-730-hours.md);
  see [ADR-0326](docs/adr/0326-calendar-accurate-monthly-hours.md).

### Fixed

- Fix snapshot `notification_codes` NULL for active classification ([#317](https://github.com/pgarciaq/ros-ocp-backend/issues/317))

- **`-race` OOM in engine sub-package test binaries:** Moved integration tests
  that depend on `internal/testutil` (testcontainers, Docker SDK) out of
  `internal/engine/pvc/` and `internal/engine/quota/` into the engine root test
  package. This drops pvc test deps from 578→437 and quota from 729→599,
  preventing OOM when compiled with `-race`. The gpu, vm, and snapshot
  sub-packages still import testutil due to tests that require unexported
  function access.

### Changed

- **Unified category classification across containers, namespaces, and nodes
  ([#358](https://github.com/pgarciaq/ros-ocp-backend/issues/358)):**
  Replaced fragmented boolean flags with a single `category` column for all
  resource types. Container and namespace recommendations now include `idle`
  and `zombie` as category values (previously only available via `idle_state`).
  Node recommendations replace `is_underutilized` and `is_overcommitted`
  booleans with a unified `category` column using priority ordering:
  `idle > overcommitted > stranded_cpu > stranded_memory > underutilized > optimized`.
  API filters `filter[idle_state]` (containers/namespaces) and
  `filter[is_underutilized]`/`filter[is_overcommitted]`/`filter[stranded_resource]`
  (nodes) are replaced by `filter[category]`. See ADR-0323.

- **Engine God-package refactoring — Phase 4: Container extraction
  ([#313](https://github.com/pgarciaq/ros-ocp-backend/issues/313)):**
  Extract container-specific recommendation logic into `internal/engine/container/`
  sub-package. Moves savings estimation, quality tracking, recommendation history,
  notifications, replica optimization, and CPU/memory recommendation functions out
  of root engine. Root engine files become thin delegates via `var = container.Func`
  and `compat.go` type aliases, preserving full backward compatibility for external
  callers. The container sub-package imports only from `engine/core` (shared types)
  and `costdata`, avoiding circular dependencies.

- **Engine God-package refactoring — Phase 3
  ([#312](https://github.com/pgarciaq/ros-ocp-backend/issues/312)):**
  Extract five domain sub-packages from root `internal/engine/`: PVC, Namespace,
  Node, Quota, and GPU. Each domain's source and test files move into their own
  sub-package (`engine/pvc/`, `engine/namespace/`, `engine/node/`, `engine/quota/`,
  `engine/gpu/`). Root engine retains orchestrators and cross-domain coordination
  while `compat.go` provides backward-compatible type/function aliases. Fixes a
  `NotificationCodeBitmap` bug where codes > 63 (e.g. `NotifSparseData = 77`) were
  silently dropped by the bitmap-limited `AppendUnique`. GPU threshold settings use
  function-variable wiring pattern to break circular imports between `engine/gpu`
  and root engine.

- **Engine God-package refactoring — Phase 2
  ([#311](https://github.com/pgarciaq/ros-ocp-backend/issues/311)):**
  Extract shared core types and algorithms into `internal/engine/core/`. Moves 15
  files (types, decay, margins, percentiles, trend, idle classification, savings,
  cost rates, explanations, notifications bitmap, category) into a new leaf package
  with zero dependencies on root engine. Root engine provides backward-compatible
  type and function aliases via `compat.go`. Sub-packages (`engine/vm/`,
  `engine/snapshot/`) continue importing root engine (aliases resolve transparently).
  Dependency graph: `engine/core ← engine (root), engine/vm, engine/snapshot`.

- **Engine God-package refactoring — Phase 1
  ([#310](https://github.com/pgarciaq/ros-ocp-backend/issues/310),
  [#307](https://github.com/pgarciaq/ros-ocp-backend/issues/307)):**
  Extracted `internal/engine/vm/` (73 files) and `internal/engine/snapshot/`
  (2 files) into domain-specific sub-packages. The root `engine` package retains
  type aliases for full backward compatibility — no import changes required in
  callers. This is Phase 1 of the multi-phase plan to break up the 245-file
  engine God package identified in adversarial review v10.

- **`ROS_INGEST_FLUSH_BATCH_SIZE` default raised to `math.MaxInt32` (effectively
  disabled) ([#264](https://github.com/pgarciaq/ros-ocp-backend/issues/264)):**
  Intermediate digest flushes are now disabled by default. The flush-and-clear
  mechanism was found to degrade recommendation quality: each flush computed
  percentiles from ~1 sample per group (due to map clearing + blind-overwrite
  upserts), producing meaningless P50/P95/P99 values. Upstream file size caps
  (nise: 100K rows, CMMO: 100 MB) bound in-flight memory to ~22–115 MB, making
  the safety mechanism unnecessary. The change simultaneously improves ingestion
  performance (1 DB flush per file instead of 20 for 10K-container clusters) and
  recommendation accuracy. The env var override remains available for
  memory-constrained environments. See ADR-0091 (revised).

- **`maxPgxBatchQueue` increased from 500 to 2000
  ([#257](https://github.com/pgarciaq/ros-ocp-backend/issues/257)):**
  Both ingestion (`pipeline.go`) and recommendation (`recommend_all.go`) batch
  queue depths raised to reduce database round-trips per flush. Memory impact
  is negligible (~1.4 MiB per batch).

- **`ROS_INGEST_FLUSH_BATCH_SIZE` default increased from 1000 to 5000
  ([#256](https://github.com/pgarciaq/ros-ocp-backend/issues/256)):**
  Superseded by [#264](https://github.com/pgarciaq/ros-ocp-backend/issues/264)
  which raised the default to `math.MaxInt32`.

- **CSV parsers now reuse record buffers
  ([#256](https://github.com/pgarciaq/ros-ocp-backend/issues/256)):**
  All 7 `csv.NewReader` call sites set `ReuseRecord = true`, reducing per-row
  `[]string` allocations during CSV parsing. Safe because all parsers copy field
  values into structs before the next `Read()`.

- **Digest partitions pre-created before manifest processing
  ([#256](https://github.com/pgarciaq/ros-ocp-backend/issues/256)):**
  `EnsureIngestPartitionsForWindow` creates digest, GPU, and node partitions for
  a 3-month window (previous, current, next month) before the file loop, avoiding
  redundant `CREATE TABLE IF NOT EXISTS` during hot-path CSV parsing.

### Removed

- **Vestigial raw usage sample tables dropped
  ([#258](https://github.com/pgarciaq/ros-ocp-backend/issues/258)):**
  `container_usage_samples` and `namespace_usage_samples` tables removed via
  migration 000172. These tables had no remaining read path after ADR-0292
  replaced query-time boxplots with digest-based percentile-band plots.
  `ROS_SAMPLE_RETENTION_DAYS` environment variable removed. Retention sweep,
  source cleanup, and config validation no longer reference these tables.

### Fixed

- **Container recommendation digest query no longer hits `statement_timeout`
  ([#263](https://github.com/pgarciaq/ros-ocp-backend/issues/263)):**
  `RecommendWorkloadsStreaming` now buffers all digest rows in memory via
  `loadDigestRows()` inside a transaction that uses the ingest statement
  timeout (120s) instead of the API default (25s). The transaction is committed
  and the DB connection released before recommendation processing begins,
  eliminating TCP backpressure timeouts on large clusters. A new covering index
  `idx_daily_container_digests_recommend` on the digest ORDER BY columns removes
  the external merge sort that contributed to slow query plans.

- **Won't Fix / Deferred decisions recorded in docs/adr/ (ARV-15,
  [#251](https://github.com/pgarciaq/ros-ocp-backend/issues/251)):**
  Seven architectural decisions (PROF-1, PROF-4, PERF-02, PERF-07, PERF-08,
  PERF-09, PERF-10) marked "Won't Fix" or "Deferred" during performance audits
  now have corresponding ADR entries (0311–0317) with status "Rejected" or
  "Deferred", linking to the audit sections and explaining the rationale.

- **CSV helper function naming consolidated (ARV-16,
  [#252](https://github.com/pgarciaq/ros-ocp-backend/issues/252)):**
  Removed duplicate `optionalInt64Str`, `optionalInt32Str`, `optionalIntPtrStr`
  from `utils.go` — they were byte-for-byte identical to `optionalInt64CSV`,
  `optionalInt32CSV`, `optionalIntCSV` in `csv_helpers.go`. Rewired 27 call
  sites. Added doc comment explaining the `float32` vs `float64` variant
  distinction (precision semantics, not arbitrary duplication).

- **Compile-time column count guard for positional scan (ARV-13,
  [#249](https://github.com/pgarciaq/ros-ocp-backend/issues/249)):**
  `nativeDetailSelect` (~82 SQL columns) and `scanNativeContainerRowsNoSort`
  must stay in lockstep, but mismatches only fail at runtime with a live DB.
  Added `TestNativeDetailSelectColumnCount` that counts comma-separated column
  tokens in the SQL constant and asserts equality — runs without PostgreSQL.

- **computeVariation negative half-integer rounding now tested (ARV-14,
  [#250](https://github.com/pgarciaq/ros-ocp-backend/issues/250)):**
  The negative branch `(diff - current/2) / current` was only exercised by the
  `-50%` case (exact integer, no rounding). Added `{3,2,-33}` and `{6,5,-17}`
  test cases to cover the non-trivial rounding boundary.

- **CI lint for bare SET statement_timeout outside AfterConnect (ARV-17,
  [#253](https://github.com/pgarciaq/ros-ocp-backend/issues/253)):**
  The pool's `AfterConnect` hook uses session-level `SET statement_timeout` to
  establish the default timeout for every connection. If other code accidentally
  uses bare `SET` instead of `SET LOCAL`, it permanently changes the connection's
  timeout for all subsequent queries on that pooled connection (layered-trust
  hazard). Added `TestNoBareSETStatementTimeout` lint test that scans
  `internal/db/` for any bare `SET statement_timeout` outside the `setStatementTimeout`
  function, catching violations at test time before they reach production.

- **sync.Pool scratch buffers capped to prevent GC pressure (ARV-10,
  [#246](https://github.com/pgarciaq/ros-ocp-backend/issues/246)):**
  `cvScratch.spareInner` accumulated cleared maps without limit during burst
  reconciliations (24+ hours of data), and `weightedDigestScratch.pairs` grew
  permanently after processing large payloads. Both persisted in the pool until
  GC eviction, contributing to heap pressure spikes. Added caps: `spareInner` at
  32 entries, `pairs` at 512 capacity (reset to 128 when exceeded).

- **Autovacuum settings relaxed for INSERT-only quality tables (ARV-11,
  [#247](https://github.com/pgarciaq/ros-ocp-backend/issues/247)):**
  Migration 000168 applied `autovacuum_vacuum_scale_factor=0.05` to quality
  partitions that are INSERT-only — vacuum found no dead tuples, causing wasted
  I/O. `autovacuum_analyze_scale_factor=0.02` triggered ANALYZE after every
  reconcile cycle. New migration resets vacuum to default (freeze duty handled by
  `autovacuum_vacuum_insert_scale_factor`) and raises analyze to 0.05.
  Additionally, `ensureEntityQualityPartitions()` now sets reloptions on newly
  created partitions so they don't silently revert to defaults.

- **`sanitizeCSVRow` in-place mutation accepted as safe (ARV-12):**
  `sanitizeCSVRow` mutates the caller's `[]string` slice in-place rather than
  returning a copy. Reviewed and accepted — all call sites pass fresh
  `[]string{...}` literals constructed immediately before the call, so no
  aliasing risk exists. No code change required.

- **Fleet heatmap cache split from fleet summary cache (ARV-9,
  [#245](https://github.com/pgarciaq/ros-ocp-backend/issues/245)):**
  The fleet heatmap LRU cache shared `ROS_FLEET_SUMMARY_CACHE_CAPACITY` with the
  fleet summary cache. Heatmap entries are much larger (~200 bytes × max nodes vs
  ~1 KB for summary entries), so raising the shared config for more summary entries
  disproportionately increased heatmap memory. Added separate
  `ROS_FLEET_HEATMAP_CACHE_CAPACITY` (default 128, down from the shared 256).
  Documented memory implications per `ROS_FLEET_HEATMAP_MAX_NODES` setting.

- **GPU model_name label cardinality capped (ARV-8,
  [#244](https://github.com/pgarciaq/ros-ocp-backend/issues/244)):**
  `rosocp_gpu_model_unrecognized_total` was a `CounterVec` keyed by raw
  (truncated) GPU model strings. Every unique model name created a new
  Prometheus time series that was never garbage-collected, causing unbounded
  memory growth on long-running deployments with GPU diversity. Replaced with a
  plain `Counter` (no labels). Specific model strings are available in
  application logs (`gpu_metadata: unrecognized GPU model`, WARN, once per model
  per process lifetime). Updated Grafana dashboard, monitoring docs, runbooks,
  GPU catalog docs, CONTRIBUTING.md, and docs-site.

- **Namespace fallback: remove LIMIT 500, add TOCTOU retry (ARV-7,
  [#243](https://github.com/pgarciaq/ros-ocp-backend/issues/243)):**
  `getNativeNamespaceByIDFallback()` scanned `DISTINCT (cluster_uuid,
  namespace_name)` with `LIMIT 500`, causing silent 404s for orgs with >500
  cluster×namespace pairs. Removed the LIMIT and scoped the fallback to
  `WHERE namespace_id IS NULL` (only pre-migration rows need the scan). Added
  a TOCTOU retry of the primary indexed lookup after the fallback scan, matching
  the pattern established in `ResolveQuotaKeyByID`.

- **Quota trend and OOM timeline get heavy statement timeout (ARV-6,
  [#242](https://github.com/pgarciaq/ros-ocp-backend/issues/242)):**
  `QueryQuotaTrend` and `QueryOOMTimeline` were omitted from the DB-001
  `WithHeavyStatementTimeout` upgrade. Both query digest tables that grow
  proportionally with cluster uptime and used only the 25s session-level default.
  Wrapped both handlers in `WithHeavyStatementTimeout` and changed the model
  functions to accept `db.QueryRower` (interface satisfied by both `*pgxpool.Pool`
  and `pgx.Tx`).

- **Namespace detail fallback uses positional scan (ARV-5,
  [#241](https://github.com/pgarciaq/ros-ocp-backend/issues/241)):**
  `getNativeNamespaceByIDFallback()` still used GORM `.Find()` with reflection
  to scan `NativeNamespaceRow` (56+ fields), while the primary path used
  positional `.Rows()` + `scanNativeNamespaceRowsNoSort()`. Converted the
  fallback to match, eliminating the last GORM reflection scan in the namespace
  detail path and ensuring column alignment tests cover both code paths.

- **pprof security hardening (ARV-4,
  [#240](https://github.com/pgarciaq/ros-ocp-backend/issues/240)):**
  Removed `pprof.Cmdline` handler (leaks full process argument list). Extracted
  shared `internal/debug` package to eliminate the 5-vs-6 route asymmetry between
  the API server (Echo) and processor/poller (`net/http`). Documented
  `ROS_ENABLE_PPROF` in the operations configuration reference.

- **Category fields now returned in API responses (ARV-1,
  [#237](https://github.com/pgarciaq/ros-ocp-backend/issues/237)):**
  `category`, `category_cpu`, and `category_memory` columns were present in the
  database and Go structs but missing from the native SQL `SELECT` constants
  (`nativeDetailSelect` for containers, `nativeNSSelect` for namespaces). The
  positional scanner skipped them, so API responses always returned empty values.
  Added all three columns to both SELECT constants and updated the four positional
  `Scan` call sites to match. Column alignment tests updated with new sentinel
  values to prevent future regressions.

- **Remove `DEBUG_SAVINGS` log noise from hot API path (ARV-3,
  [#239](https://github.com/pgarciaq/ros-ocp-backend/issues/239)):**
  Two `logrus.Infof("DEBUG_SAVINGS: ...")` calls executed for every container
  recommendation in the list API, producing thousands of log lines per request
  on large tenants and leaking financial data (`savings_cents`) at Info level.
  Removed both lines.

- **Partition DROP lock convoy prevention (ARV-2,
  [#238](https://github.com/pgarciaq/ros-ocp-backend/issues/238)):**
  `SweepPartitionedTables` now wraps each `DROP TABLE` in a transaction with
  `SET LOCAL lock_timeout = '2s'`. Previously, if a concurrent API query held
  `AccessShareLock` on a partition, the DROP would block (up to 25s statement
  timeout) and its pending `AccessExclusiveLock` would queue all subsequent
  reads — a lock convoy. Now the DROP fails fast and retries on the next daily
  sweep. Also switched to `pgx.Identifier{}.Sanitize()` for secure identifier
  quoting.

- **`requireXRHID` defense-in-depth fix:** The identity extraction helper returned
  `nil` error after writing a 401 response (because `c.JSON()` returns nil on
  success). All ~50 handler callers would continue executing past the auth check
  and panic on nil `db.Pool`. Now returns `echo.ErrUnauthorized` so callers stop.
  Mitigated in production by middleware, but this makes each handler self-protecting.
- **`classifySnapshot` nil semantics:** Returned `[]int16{}` (empty non-nil slice)
  for active snapshots instead of `nil` (no notifications). Corrected to return nil.
- **Runtime partition reloptions inheritance:** New monthly partitions created by
  `EnsureHourlyNodeDigestPartitions` / `EnsureHourlyVMDigestPartitions` now receive
  `autovacuum_vacuum_scale_factor=0.05`, `autovacuum_analyze_scale_factor=0.02`,
  and `fillfactor=85` via `ALTER TABLE SET` immediately after creation.

### Added

- **pprof profiling support:** Set `ROS_ENABLE_PPROF=true` to expose Go pprof
  endpoints (`/debug/pprof/`) on the internal Prometheus/metrics port. Gated by
  env var (default off), blocked by security enforcement in production (CM-7).
  Use with `kubectl port-forward` for CPU/memory profiling of live pods.

### Performance

- **Database tuning migrations (audit v3, batch 1):**
  Autovacuum and storage parameter tuning for 9 high-write tables, plus a partial
  index for GPU timeslicing cross-ref lookups.
  - `autovacuum_vacuum_scale_factor=0.05` on all 9 tables (4× more frequent vacuum)
  - `fillfactor=85` on 4 UPSERT/UPDATE-heavy tables (enables HOT updates)
  - Partial index on `recommendation_sets` for `has_gpu=true AND time_slicing_node<>''`
    eliminates full-table scan in `LoadPersistedGPUTimeslicingCrossRefs`
  - ([#195](https://github.com/pgarciaq/ros-ocp-backend/issues/195),
    [#197](https://github.com/pgarciaq/ros-ocp-backend/issues/197),
    [#198](https://github.com/pgarciaq/ros-ocp-backend/issues/198))

- **13 performance quick wins from audit v3:**
  Implemented the highest-ROI optimizations from the native engine performance audit.
  - **Engine/ingestion:** PVC decay weight lookup table eliminates ~135k `math.Exp` calls
    ([#188](https://github.com/pgarciaq/ros-ocp-backend/issues/188));
    integer `hourKey` comparison avoids `time.Date` allocations
    ([#204](https://github.com/pgarciaq/ros-ocp-backend/issues/204));
    pre-allocated VM accumulator slices
    ([#189](https://github.com/pgarciaq/ros-ocp-backend/issues/189));
    `sync.Map` partition DDL cache
    ([#190](https://github.com/pgarciaq/ros-ocp-backend/issues/190));
    capacity hint on GPU writes slice
    ([#191](https://github.com/pgarciaq/ros-ocp-backend/issues/191))
  - **Database writes:** `pgx.Batch` for GPU timeslicing cross-ref UPDATEs
    ([#186](https://github.com/pgarciaq/ros-ocp-backend/issues/186))
    and history INSERTs
    ([#192](https://github.com/pgarciaq/ros-ocp-backend/issues/192));
    dynamic WHERE clause eliminates non-sargable OR pattern
    ([#193](https://github.com/pgarciaq/ros-ocp-backend/issues/193))
  - **API handlers:** Heavy statement timeouts on all expensive endpoints
    (fleet heatmap, snapshot cost-by-type, node/VM hourly, GPU timeslicing history)
    ([#185](https://github.com/pgarciaq/ros-ocp-backend/issues/185),
    [#199](https://github.com/pgarciaq/ros-ocp-backend/issues/199),
    [#209](https://github.com/pgarciaq/ros-ocp-backend/issues/209),
    [#221](https://github.com/pgarciaq/ros-ocp-backend/issues/221));
    hard 90-day date range cap on OOM timeline and quota trend
    (in addition to `MaxLookbackDays`)
    ([#200](https://github.com/pgarciaq/ros-ocp-backend/issues/200),
    [#228](https://github.com/pgarciaq/ros-ocp-backend/issues/228));
    max 20 bucket boundaries
    ([#213](https://github.com/pgarciaq/ros-ocp-backend/issues/213));
    CSV formula-injection sanitization on all 6 CSV export handlers
    ([#203](https://github.com/pgarciaq/ros-ocp-backend/issues/203),
    [#229](https://github.com/pgarciaq/ros-ocp-backend/issues/229))

- **Hourly digest retention via partition DROP (DB-002,
  [#231](https://github.com/pgarciaq/ros-ocp-backend/issues/231)):**
  Switched `hourly_node_digests` and `hourly_vm_digests` retention from
  row-level DELETE to `SweepPartitionedTables` (partition DROP). Eliminates
  per-row WAL entries and dead-tuple vacuum overhead. Monthly granularity keeps
  at most one extra partial month, consistent with all other partitioned tables.

- **Quota recommendation O(1) lookup (PERF-01):**
  Added deterministic `quota_id` (UUID v5) column and index to
  `quota_recommendation_sets`. The quota trend endpoint now resolves quota keys
  via indexed lookup instead of scanning all rows. Existing rows are backfilled
  automatically by the next reconciliation UPSERT (~5 min after migration).
  Includes a retry on the indexed path to handle the TOCTOU window during
  the backfill transition.

- **Integer ceiling for replica optimization (REPLICA-1):**
  Replaced `math.Ceil(float64/float64)` with pure integer arithmetic
  `(a + b - 1) / b` in `ComputeRecommendedReplicas`. Eliminates float64
  conversions, removes the `math` import, and avoids any floating-point
  rounding edge cases. Semantically equivalent for all positive inputs.

- **Integer rounding for variation calculation (DIGEST-2):**
  Replaced `math.Round(float64/float64 * 100)` with pure integer arithmetic
  in `computeVariation`. Uses banker-style `(diff*100 + current/2) / current`
  for positive values (and symmetric for negative). Removes the last `math`
  import from `recommend_all.go`. Semantically equivalent for all inputs.

### Security

- **Graduated production security enforcement ([#168](https://github.com/pgarciaq/ros-ocp-backend/issues/168)):**
  Startup now validates RBAC, DB TLS, Kafka TLS, dev token absence, CSV allowlist,
  and internal endpoint auth. Enforcement follows a three-tier model aligned with
  FedRAMP Shared Responsibility:
  - **None** (`DEVELOPMENT=true`): all checks skipped — zero developer friction
  - **Warn** (on-prem default): findings logged as `SECURITY WARNING`; process continues
  - **Fatal** (Clowder/SaaS or `ROS_SECURITY_ENFORCE=true`): process exits on any violation
  
  Additionally, `DEVELOPMENT=true` + `ACG_CONFIG` (Clowder) always fatals to prevent
  dev mode inside the FedRAMP authorization boundary. New env var: `ROS_SECURITY_ENFORCE`.
  Addresses FedRAMP controls AC-3, SC-8, CM-6, IA-3, SI-10.
  See [`docs/operations/security-enforcement.md`](docs/operations/security-enforcement.md).

- **CSV formula injection sanitization ([#170](https://github.com/pgarciaq/ros-ocp-backend/issues/170)):**
  All CSV export endpoints now sanitize cell values against spreadsheet formula
  injection (CSV injection). Cells starting with `=`, `+`, `-`, `@`, `\t`, or
  `\r` are prefixed with a single quote to prevent formula execution when opened
  in Excel, LibreOffice, or Google Sheets. Applied as defense-in-depth across all
  9 CSV generators (container, namespace, PVC, quota, cluster quota, fleet savings,
  GPU MIG, node GPU, machineset, snapshot recommendations). Reference: OWASP CSV
  Injection, FedRAMP SI-10.

## [1.0.0-phase16] — 2026-07-02 — Phase 16: Multi-GPU Awareness and Node GPU Count

**Branch:** `pgarciaq-rosocp-superpowers-phase16`

### Added

- **Kafka commit-on-panic rationale documented ([#164](https://github.com/pgarciaq/ros-ocp-backend/issues/164)):**
  Both sequential and parallel consumer panic recovery sites now have inline
  documentation explaining why committed (skipped) messages are the correct
  strategy: panics indicate malformed data that would panic again on retry,
  while transient failures surface as errors handled by Kafka redelivery.

- **Rate limiter: configurable TTL, exported sentinel constant, health endpoint bypass test ([#160](https://github.com/pgarciaq/ros-ocp-backend/issues/160), [#163](https://github.com/pgarciaq/ros-ocp-backend/issues/163), [#165](https://github.com/pgarciaq/ros-ocp-backend/issues/165)):**
  Bucket expiry is now configurable via `ROS_API_RATE_LIMIT_EXPIRES_MINUTES`
  (default 5). The `__unknown_org__` sentinel value is exported as
  `middleware.UnknownOrgSentinel` for test consistency. Integration test
  confirms `/healthz`, `/readyz`, `/status` are excluded from rate limiting
  (registered before the v1 middleware group).

- **S3 readiness SSRF filter hardened: private ranges, DNS resolution, https-only ([#159](https://github.com/pgarciaq/ros-ocp-backend/issues/159), [#166](https://github.com/pgarciaq/ros-ocp-backend/issues/166)):**
  `validateS3Endpoint` now: (1) blocks all RFC 1918/4193 private ranges,
  IPv6 loopback/link-local, and link-local multicast addresses; (2) resolves
  hostnames via DNS and validates each resolved IP against the blocklist
  (defeating DNS rebinding); (3) requires `https://` in production — `http://`
  is only allowed when `DEVELOPMENT=true` for local MinIO/LocalStack usage.

- **InBusinessHours overnight schedule respects day boundaries ([#162](https://github.com/pgarciaq/ros-ocp-backend/issues/162)):**
  For overnight schedules (e.g., 22:00–06:00) configured for specific days,
  the post-midnight portion is now correctly attributed to the previous
  calendar day's shift. Previously, a "Monday 22:00–06:00" schedule would
  reject "Tuesday 03:00" because Tuesday wasn't in the allowed days list,
  even though that time is part of Monday's night shift.

- **process*CSVNative functions propagate context ([#161](https://github.com/pgarciaq/ros-ocp-backend/issues/161)):**
  All 6 `process*CSVNative` test-helper functions now accept `ctx context.Context`
  as their first parameter, eliminating hidden `context.Background()` calls.
  This ensures cancellation can propagate through these paths and makes the code
  consistent with the production dispatch path which already threads context.

- **DB singleton uses atomic.Pointer to eliminate data race ([#158](https://github.com/pgarciaq/ros-ocp-backend/issues/158)):**
  Replaced `sync.Once` reassignment in test helpers (`SetForceTestPool`,
  `SuspendForceTestPool`) with `atomic.Pointer[pgxpool.Pool]` and an
  `atomic.Bool` suppression flag. Production hot path (`GetPool`) retains ~1ns
  fast-path cost (single atomic load) while test state transitions are now
  race-free. Previously reassigning `sync.Once` structs was a data race under
  Go's memory model.

- **Fleet heatmap validates `term` parameter ([#154](https://github.com/pgarciaq/ros-ocp-backend/issues/154)):**
  The `filter[term]` parameter is now validated against the `short|medium|long`
  allowlist. Invalid values return HTTP 400 instead of silently producing empty
  results and polluting the LRU cache with bogus keys.

- **Rate limiter uses shared bucket for empty org_id ([#156](https://github.com/pgarciaq/ros-ocp-backend/issues/156)):**
  When identity has no `org_id`, the rate limiter now uses a fixed sentinel key
  (`__unknown_org__`) instead of the client IP. This prevents X-Forwarded-For
  spoofing from bypassing rate limits. Also sets `Echo.IPExtractor` to
  `ExtractIPFromXFFHeader()` for correct proxy-aware IP resolution.

- **HTTP server hardened with full timeout set ([#155](https://github.com/pgarciaq/ros-ocp-backend/issues/155)):**
  Added configurable `ReadTimeout` (default 60s), `WriteTimeout` (default 120s),
  and `IdleTimeout` (default 120s) to the API `http.Server`. Previously only
  `ReadHeaderTimeout` was set, leaving the server vulnerable to slow-loris attacks
  and idle connection accumulation.

- **Ingest path threads ctx for graceful shutdown ([#153](https://github.com/pgarciaq/ros-ocp-backend/issues/153)):**
  All `run*Recommendations` functions now accept a `context.Context` parameter
  propagated from the Kafka handler's shutdown-aware context. Previously, each
  function created `context.Background()`, causing in-flight DB transactions to
  ignore SIGTERM and block pod shutdown beyond `terminationGracePeriodSeconds`.

- **Fleet heatmap cache key includes cluster filter ([#148](https://github.com/pgarciaq/ros-ocp-backend/issues/148)):**
  The `clusterFilter` query parameter is now part of the LRU cache key. Previously,
  filtered and unfiltered requests shared the same cache entry, causing intra-org
  data inconsistency when different users applied different cluster filters.

- **Overnight business hours schedule support ([#149](https://github.com/pgarciaq/ros-ocp-backend/issues/149)):**
  `InBusinessHours` now handles wrap-around schedules (e.g., 22:00–06:00) where
  `startTime > endTime`. Previously, overnight schedules never matched, silently
  classifying all data as outside business hours and overstating savings estimates.

- **DB/Pool singletons use sync.Once ([#150](https://github.com/pgarciaq/ros-ocp-backend/issues/150)):**
  `GetPool()` and `GetDB()` initialization is now protected by `sync.Once`, preventing
  a data race when multiple goroutines (e.g., parallel Kafka workers) call them
  concurrently during startup.

- **KafkaMsg Files/Object_keys length bounded ([#151](https://github.com/pgarciaq/ros-ocp-backend/issues/151)):**
  Added `max=1000` validator tags to `Files` and `Object_keys` slices. Messages
  exceeding this limit are rejected as invalid, preventing resource exhaustion
  from malformed Kafka messages with thousands of file entries.

- **S3 readiness endpoint SSRF validation ([#152](https://github.com/pgarciaq/ros-ocp-backend/issues/152)):**
  The `/readyz` S3 health check now validates `ROS_READINESS_S3_ENDPOINT` against
  restricted addresses (localhost, link-local, metadata endpoint) before making
  requests, consistent with the CSV download SSRF protections.

- **Panic recovery in Kafka worker goroutines ([#147](https://github.com/pgarciaq/ros-ocp-backend/issues/147)):**
  Added `recover()` blocks in both `wrapHandlerWithInFlight` (sequential consumer)
  and the parallel worker goroutines. A panic in a message handler now logs the
  full stack trace, increments `rosocp_kafka_handler_panics_total` Prometheus counter,
  commits the message (poison-message semantics), and allows the goroutine to
  continue processing. This prevents consumer crashes and `WaitGroup` leaks that
  could deadlock graceful shutdown.

- **CloudWatch credentials no longer exposed in process environment ([#146](https://github.com/pgarciaq/ros-ocp-backend/issues/146)):**
  Replaced `os.Setenv("AWS_ACCESS_KEY_ID", ...)` / `os.Setenv("AWS_SECRET_ACCESS_KEY", ...)`
  with direct credential injection via `credentials.NewStaticCredentials` passed through
  the `*aws.Config` parameter to the CloudWatch hook. Credentials are now scoped to the
  AWS session and are not visible in `/proc/self/environ` or inherited by child processes.

- **Per-org API rate limiting ([#37](https://github.com/pgarciaq/ros-ocp-backend/issues/37)):**
  Added opt-in per-organization token bucket rate limiter using Echo's built-in middleware.
  Configured via environment variables: `ROS_API_RATE_LIMIT_ENABLED` (default `false`),
  `ROS_API_RATE_LIMIT_RPM` (default 60), `ROS_API_RATE_LIMIT_BURST` (default 30).
  Returns HTTP 429 with JSON body when exceeded. Prometheus counter
  `rosocp_rate_limited_requests_total` tracks denied requests.

- **Fleet heatmap safety limit ([#144](https://github.com/pgarciaq/ros-ocp-backend/issues/144)):**
  Added configurable max-node cap (`ROS_FLEET_HEATMAP_MAX_NODES`, default 1000) to the
  fleet heatmap endpoint. Queries now include a `LIMIT` clause to prevent unbounded memory
  allocation for large fleets. When the limit is reached, a `meta.warnings` array indicates
  truncation and suggests filtering by cluster.

- **Fleet heatmap scan error tracking ([#145](https://github.com/pgarciaq/ros-ocp-backend/issues/145)):**
  Row scan failures are now counted and reported in `meta.warnings` (e.g., "2 rows could
  not be read") instead of being silently skipped. A Prometheus counter
  (`rosocp_fleet_heatmap_scan_errors_total`) enables alerting on schema drift after migrations.

- **Node GPU count ([#32](https://github.com/pgarciaq/ros-ocp-backend/issues/32)):**
  Added `node_gpu_count` column to `daily_node_digests` table (migration 166).
  Ingestion now parses the `node_allocatable_gpu_count` column from the operator's
  ROS container CSV and stores the maximum value per node-day. The field is exposed
  in node recommendation list and detail API responses as `node_gpu_count`.

- **Multi-GPU container awareness ([#30](https://github.com/pgarciaq/ros-ocp-backend/issues/30)):**
  Added `gpu_count` column to `gpu_container_digests` table (migration 167).
  Ingestion now parses the `gpu_uuid` column from the operator CSV and counts
  distinct GPU UUIDs per container-day to derive `gpu_count`. The GPU MIG
  recommendation engine skips MIG downsizing for containers using multiple GPUs
  (`gpu_count > 1`) and emits notification code 78 (`NotifGPUMultiDevice`).
  `gpu_count` is exposed in GPU MIG recommendation API responses.

## [1.0.0-phase15] — 2026-07-02 — Phase 15: Pagination, Sorting, and Savings Display Fixes

**Branch:** `pgarciaq-rosocp-superpowers-phase15`

### Added

- **Fleet Recommendation History Explorer UI ([#130](https://github.com/pgarciaq/ros-ocp-backend/issues/130)):**
  New "History" top-level tab in the Optimizations page (koku-ui) that displays a
  paginated, sortable, filterable table of all recommendation snapshots across the fleet.
  Filters include cluster, project, workload, container, term, engine, and date range
  (defaults to last 30 days). Supports CSV export via `format=csv` query parameter.
  Advanced `expl_*` explanation columns are hidden by default with a toggle to reveal them.
  Implemented as a Module Federation remote component in koku-ui-ros loaded by koku-ui-hccm.

- **Container Recommendation History Chart ([#49](https://github.com/pgarciaq/ros-ocp-backend/issues/49)):**
  Exposed all 21 `expl_*` explanation columns in `GET /recommendations/openshift/history`
  response. These columns (data days, decay half-life, CPU/memory cost/perf percentiles,
  usage statistics, adaptive margins, trend slopes, OOM counts, floor flags, idle state)
  provide rich context for recommendation drift visualization. Also added these columns
  to CSV export. No migration needed — columns already existed in `recommendation_history`.

### Fixed

- **Term enum bug in history endpoints ([#49](https://github.com/pgarciaq/ros-ocp-backend/issues/49)):**
  `GET /history` and `GET /gpu/timeslicing/history` now normalize incoming `term`
  filter values via `NormalizeRecommendationTermFilter` and emit canonical forms
  (`short_term`, `medium_term`, `long_term`) in responses via `termToAPI()` converters.
  Previously these endpoints accepted/returned raw DB values (`short`, `medium`, `long`)
  which broke frontend filtering that uses ADR-0069 canonical terms.

- **Persist GPU MIG recommendations for full SQL pagination ([#102](https://github.com/pgarciaq/ros-ocp-backend/issues/102)):**
  Replaced the per-request GPU MIG enrichment loop (`gpu_enrichment.go`) with a
  persisted `gpu_mig_recommendation_sets` table populated during the background
  engine cycle. The MIG list handler now reads directly from SQL with full keyset
  pagination, sorting (`cluster`, `namespace`, `confidence`, `gpu_idle_state`,
  `term`), and filtering (`term`, `gpu_idle_state`, `cluster`, `namespace`,
  `workload`). Provides exact `meta.count` via `COUNT(*)`. Grouped queries
  (`group_by[cluster]`, `group_by[project]`) are also SQL-backed. Migration
  `000165` creates the table with composite primary key and keyset indexes.
  Retention sweep added to the GPU plugin.

- **Node Request vs Usage Gap Chart ([#23](https://github.com/pgarciaq/ros-ocp-backend/issues/23)):**
  Visual Insights chart showing the gap between aggregate resource requests and
  actual P95 usage on nodes. Exposes `max_cpu_requests_mc` and `max_mem_requests_kib`
  from `daily_node_digests` on the node detail endpoint. Frontend renders two area
  charts (CPU/Memory) where the shaded gap between the request line and usage line
  highlights overcommitted resources. No migration needed — columns already existed
  since migration 000052.

- **Savings Waterfall Dashboard ([#25](https://github.com/pgarciaq/ros-ocp-backend/issues/25)):**
  Horizontal bar chart in the Efficiency tab showing potential monthly savings
  broken down by optimization category (Container, GPU, Node, PVC, Snapshot, VM).
  Uses the existing `/recommendations/openshift/savings-summary` endpoint's
  `by_plugin` field. Bars are sorted by absolute magnitude, with positive savings
  shown in blue and negative savings in red. Gated behind the
  `ROS_VISUAL_INSIGHTS_ENABLED` Unleash feature toggle. Implemented in
  `koku-ui-ros` as a federated module (`SavingsWaterfallChart`).

- **Per-pod CV for StatefulSet replica confidence ([#116](https://github.com/pgarciaq/ros-ocp-backend/issues/116)):**
  Phase 2 of replica count optimization. Computes the coefficient of variation
  (CV) of per-pod CPU usage across hourly buckets and stores it as `cpu_usage_cv_bp`
  (basis points, 0-10000) on `daily_container_digests`. StatefulSet confidence now
  uses this direct asymmetry measure when available (CV < 15% = high, 15-30% = medium,
  > 30% = low), falling back to the Phase 1 P50/P95 spread heuristic when pod
  identity is unavailable.

- **Max usage columns on daily_node_digests ([#107](https://github.com/pgarciaq/ros-ocp-backend/issues/107)):**
  Added `cpu_usage_max_mc` and `mem_usage_max_kib` columns to `daily_node_digests`.
  These store the highest single hourly aggregate CPU/memory usage for the day,
  complementing the existing P50 and P95 percentiles. Nullable for backward
  compatibility — no backfill required. Exposed on the node detail API
  (`GET /recommendations/openshift/nodes/{node}`) within `daily_digests[]`.

- **Cold-start null state support ([#59](https://github.com/pgarciaq/ros-ocp-backend/issues/59)):**
  Added `min_data_days` field to the response `meta` of all list endpoints
  (containers, namespaces, nodes, node GPU time-slicing, PVCs). The value is
  read from the term configuration for the active term (defaulting to the
  "medium" term when no `filter[term]` is specified). This enables the UI to
  detect cold-start conditions (when `data_days_available < min_data_days`)
  and show an informative empty state instead of a generic "no results" page.
  Added `MinDataDaysForTerm` helper to `internal/engine/term_config.go` with
  unit tests. Updated OpenAPI spec with the new field across all list meta schemas.

- **PVC and VM recommendation quality metrics (Tier 1 of [#117](https://github.com/pgarciaq/ros-ocp-backend/issues/117)):**
  Generalized quality metrics to PVC and VM entity types. New database tables
  `pvc_recommendation_quality` and `vm_recommendation_quality` (partitioned by
  `measured_at`), with automatic partition management and 90-day retention.
  Engine logic computes stability (old vs new recommendation comparison),
  adoption detection (current allocation ≈ old recommendation), and
  entity-specific signals: `days_above_threshold` for PVCs (days where
  usage/capacity > 95%) and `saturation_days` for VMs (days where CPU or
  memory utilization > 95% of allocated). Quality metrics are written after
  each recommendation cycle in both the ingestion and threshold-recalculation
  pipelines. New API endpoints: `GET /quality/containers` (canonical path for
  existing container quality), `GET /quality/pvcs`, `GET /quality/vms` — all
  with pagination, sorting, filtering, RBAC, and CSV export. The existing
  `/quality` endpoint remains as a backward-compatible alias.

- **GPU MIG and Snapshot recommendation quality metrics (Tier 2 of [#117](https://github.com/pgarciaq/ros-ocp-backend/issues/117)):**
  Extended quality metrics to GPU MIG and Snapshot entity types. New database
  tables `gpu_mig_recommendation_quality` and `snapshot_recommendation_quality`
  (partitioned by `measured_at`), with automatic partition management and 90-day
  retention. GPU MIG quality computes binary stability (same/different MIG profile),
  adoption (current profile matches old recommended profile), and contention days
  (days where `sm_active_max` ≥ 95% in GPU digests). Snapshot quality detects
  adoption by tracking snapshots that disappear from inventory after receiving a
  delete/stale recommendation. Quality metrics are written before reconciliation
  deletes adopted rows. New API endpoints: `GET /quality/gpu`,
  `GET /quality/snapshots` — both with pagination, sorting, filtering, RBAC, and
  CSV export.

### Changed

- **Migrate container abandoned detection to `idle_state=zombie` classification:**
  `ClassifyIdleState` now detects zero-usage containers as zombie immediately (early zombie
  path), regardless of the 14-day observation window. `DetectAbandoned()` function removed.
  Notification code 8 (`ABANDONED_WORKLOAD`) removed — zombie containers now emit code 5
  (`IDLE_WORKLOAD`). Fleet summary `abandoned_containers` count now queries
  `idle_state = 'zombie'` instead of `notification_codes @> ARRAY[8]`. Savings recalculation
  reads `idle_state` column instead of checking for code 8. Category assignment (`category`,
  `category_cpu`, `category_memory`) is suppressed for idle/zombie containers (set to NULL).
  ([Issue #86](https://github.com/pgarciaq/ros-ocp-backend/issues/86))

### Added

- **Integration tests for hourly heatmap and VM activity endpoints:**
  Added `handlers_node_hourly_test.go` (6 tests) and `handlers_vm_hourly_test.go`
  (7 tests) covering happy-path response shapes, empty data, missing/invalid
  parameters, custom `days` param, max-days capping, and RBAC cluster filtering
  for `GET /node/{id}/hourly-utilization` and `GET /vm/hourly-activity`.
  Replica optimization response shape was verified as already well-covered by
  existing node recommendations integration tests.
  ([Issue #120](https://github.com/pgarciaq/ros-ocp-backend/issues/120))

- **Quality metrics dashboard UI (Phase 1):** New "Quality" tab on the HCCM
  Optimizations page, gated by `cost-management.koku-ui-hccm.quality-dashboard`
  Unleash flag (default off). Displays recommendation stability (line chart),
  adoption rate (area chart), OOM-after-recommendation events (bar chart), summary
  KPI cards, a sortable data table, and CSV download. Backed by the existing
  `GET /recommendations/openshift/quality` API endpoint.
  ([Issue #50](https://github.com/pgarciaq/ros-ocp-backend/issues/50))

- **Replica count optimization (Phase 1):** New recommendation type that suggests
  an optimal replica count for Deployments and StatefulSets based on per-replica
  P95 resource utilization. Adds three columns to `recommendation_sets`:
  `recommended_replicas`, `replica_confidence`, `replica_explanation`. Target
  utilization is configurable via `ROS_REPLICA_TARGET_UTILIZATION_PCT` env var
  (default 70%, valid range 10–95%). Deployments have a minimum floor of 2
  replicas (HA), StatefulSets min 1. DaemonSets are excluded. Confidence uses
  a P50/P95 CPU spread heuristic (Deployments always high; StatefulSets vary
  by spread). Savings calculation includes both per-replica sizing savings and
  replica reduction savings for freed replicas. Exposed via `replica_optimization`
  object in the container detail API response.
  ([Issue #98](https://github.com/pgarciaq/ros-ocp-backend/issues/98),
  [ADR-0309](../docs/adr/0309-replica-count-optimization-phase1.md))

- **Node utilization cold-start signal (`meta.data_days_available`):** The node
  utilization recommendations endpoint (`GET /recommendations/openshift/nodes`) now
  returns `data_days_available` in the response `meta` object, reporting the number
  of distinct days of `daily_node_digests` data for the queried cluster(s). The UI
  compares this against `min_data_days` from the terms API to distinguish cold-start
  (insufficient data) from genuine "no recommendations" scenarios.
  ([Issue #84](https://github.com/pgarciaq/ros-ocp-backend/issues/84))

- **`group_by` support for Node, GPU, and VM tabs:** Added `group_by[cluster]` to
  Node utilization and GPU time-slicing list endpoints; `group_by[cluster]` and
  `group_by[project]` to GPU MIG; `group_by[cluster]` and `group_by[namespace]`
  to VM recommendations. When a `group_by` parameter is present, the endpoint
  returns aggregated rows with the group key, count, and summed estimated monthly
  savings (where applicable). Follows the existing Storage/Quota group-by pattern.
  ([Issue #112](https://github.com/pgarciaq/ros-ocp-backend/issues/112))

- **VM `node` and `is_power_off_candidate` filters:** Added `filter[node]` (string) and
  `filter[is_power_off_candidate]` (boolean) query parameters to the VM recommendations
  list endpoint. Follows the same parsing patterns as existing boolean filters.
  ([Issue #108](https://github.com/pgarciaq/ros-ocp-backend/issues/108))
- **GPU MIG `workload` filter:** Added `filter[workload]` (multi-value) query parameter
  to the GPU MIG recommendations list endpoint. Uses post-fetch filtering consistent
  with existing `project` and `gpu_idle_state` filters.
  ([Issue #109](https://github.com/pgarciaq/ros-ocp-backend/issues/109))

- **Node hourly utilization heatmap API:** New `GET /recommendations/openshift/node/{id}/hourly-utilization`
  endpoint returns per-hour CPU/memory aggregates and max pod count for a specific node over
  a configurable date range (default 14 days, max 90). Used by the frontend to render
  hour-of-day × day-of-week utilization heatmaps on the node breakdown page. Includes new
  `hourly_node_digests` partitioned table (monthly range partitions on `report_date`), hourly
  aggregation in the node ingestion pipeline (reuses existing `NodeDayAccumulator` per-hour
  buckets), and retention cleanup. Gated by `ROS_VISUAL_INSIGHTS_ENABLED` and
  `ROS_HOURLY_NODE_DIGESTS_ENABLED` (both default true).
  ([Issue #16](https://github.com/pgarciaq/ros-ocp-backend/issues/16))

- **VM hourly activity heatmap API:** New `GET /recommendations/openshift/vm/hourly-activity`
  endpoint returns per-hour CPU/memory/IO aggregates for a specific VM over a configurable
  date range (default 14 days, max 90). Used by the frontend to render hour-of-day ×
  day-of-week activity heatmaps revealing idle periods. Includes new `hourly_vm_digests`
  partitioned table (monthly range partitions on `report_date`), hourly aggregation in
  the VM ingestion pipeline, and retention cleanup. Gated by `ROS_VISUAL_INSIGHTS_ENABLED`
  and `ROS_HOURLY_VM_DIGESTS_ENABLED` (both default true).
  ([Issue #13](https://github.com/pgarciaq/ros-ocp-backend/issues/13))

- **Fleet heatmap API:** New `GET /recommendations/openshift/fleet-heatmap` endpoint
  returns per-node utilization data for fleet-wide heatmap visualization. Each node
  includes a server-computed utilization band (idle/low/moderate/healthy/hot) based
  on the selected metric's p95 value and idle state. Supports `metric` (cpu/memory),
  `filter[term]`, `filter[engine]`, and `filter[cluster]` query params. Includes
  RBAC-aware caching (LRU+TTL, 5 min, 256 entries) with invalidation wired into
  recommendation ingest, savings/threshold recalculations, retention sweeps, sources
  cleanup, and business hours settings. Gated by `ROS_VISUAL_INSIGHTS_ENABLED`.
  ([Issue #24](https://github.com/pgarciaq/ros-ocp-backend/issues/24))

- **Node daily digests in detail API:** The `GET /recommendations/openshift/nodes/{node}`
  endpoint now includes a `daily_digests` array with per-day CPU and memory P50/P95
  utilization values plus allocatable capacities from the `daily_node_digests` table.
  Supports `start_date` and `end_date` query params (default: last 14 days).
  Gated by `ROS_VISUAL_INSIGHTS_ENABLED`.
  ([Issue #20](https://github.com/pgarciaq/ros-ocp-backend/issues/20))

- **GPU VRAM capacity in API responses:** Container detail, MIG recommendation,
  and GPU summary endpoints now include `total_fb_mib` (total VRAM capacity in MiB)
  from the GPU catalog. Populated automatically for all 17 recognized GPU models;
  omitted (null) for unrecognized models. Enables frontend VRAM utilization gauges
  without additional API calls.
  ([Issue #21](https://github.com/pgarciaq/ros-ocp-backend/issues/21))

- **Recommendation categories (undersized/oversized/optimized):** Container and
  namespace recommendations now include `category`, `category_cpu`, and
  `category_memory` fields classifying each recommendation based on variation
  percentage thresholds (±10% dead zone). The conservative rule applies: undersized
  wins when CPU and memory disagree. PVC, VM, and quota endpoints derive `category`
  from their existing classification fields (no new DB columns). New
  `filter[category]=undersized|oversized|optimized` query parameter supported on
  container and namespace list endpoints. Migration 000155 adds the columns and
  partial indexes.
  ([Issue #81](https://github.com/pgarciaq/ros-ocp-backend/issues/81))

- **Business-hours boxplot overlay:** Container and namespace detail endpoints now
  include an optional `business_hours_plots` field alongside the existing `plots`
  field. When business-hours digests exist, the field contains utilization percentile
  data (P50/P95/P99/Max) computed from business-hours schedule only, enabling
  side-by-side comparison on the frontend charts. Omitted when no business-hours
  data is configured.
  ([Issue #18](https://github.com/pgarciaq/ros-ocp-backend/issues/18))

- **Namespace quota headroom trend endpoint:** New
  `GET /recommendations/openshift/quota/:quota-id/trend` returns per-day quota
  hard limit vs actual used values for CPU request (millicores) and memory request
  (bytes). The gap between hard and used represents headroom. Defaults to the last
  30 days. Gated by `ROS_VISUAL_INSIGHTS_ENABLED` and the `quota` plugin.
  ([Issue #14](https://github.com/pgarciaq/ros-ocp-backend/issues/14))

- **Snapshot cost-by-type aggregation endpoint:** New
  `GET /recommendations/openshift/snapshots/cost-by-type` returns snapshot storage
  costs grouped by `recommendation_type` (orphaned, stale, active, etc.) with total
  cost in cents and count. Gated by `ROS_VISUAL_INSIGHTS_ENABLED`.
  ([Issue #19](https://github.com/pgarciaq/ros-ocp-backend/issues/19))

### Changed

- **Auto-detect SaaS mode for heavy API statement timeout:** When Clowder is
  detected (`ACG_CONFIG` set), `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` defaults to
  28000ms instead of 45000ms to stay within the ~30s ingress/gateway budget. On-prem
  deployments retain the 45000ms default. Explicit env var always overrides.
  ([Issue #44](https://github.com/pgarciaq/ros-ocp-backend/issues/44),
  [ADR-0308](../docs/adr/0308-auto-lower-heavy-api-timeout-saas.md))

- **GPU MIG list SQL-backed keyset pagination:** The GPU MIG recommendations
  list endpoint now delegates pagination to SQL (`ListGPUMIGKeysPage` with real
  `limit`, `offset`, and keyset `seek`) instead of fetching all keys in-memory.
  `meta.count` uses `CountGPUMIGKeys` for the total distinct-key count. Pages
  may be under-full after in-memory MIG/idle/term filtering (tracked in #102).
  ([Issue #71](https://github.com/pgarciaq/ros-ocp-backend/issues/71))

- **Consolidated hand-rolled LRU caches onto `hashicorp/golang-lru/v2`:** Replaced
  four custom bounded-LRU+TTL cache implementations (RBAC permissions, cost/effective
  rates, fleet summary, savings summary) with `expirable.NewLRU` from the hashicorp
  library already used by `termConfigCache`. Created a shared generic
  `cache.RemoveByPrefix` helper for prefix-based invalidation. Background expiry
  replaces lazy-on-read expiry. Prometheus metrics renamed from `*_evictions_total`
  to `*_removals_total`; `*_lazy_expiry_total` counters dropped. Grafana dashboard
  and documentation updated accordingly. No API or behavioral changes.
  ([Issue #95](https://github.com/pgarciaq/ros-ocp-backend/issues/95))

### Fixed

- **`filter[stale]` correctness and performance:** The `filter[stale]=true` and
  `filter[stale]=only` query parameters now work correctly and use the fast
  `org_container_keys` keyset pagination path instead of falling back to expensive
  `DISTINCT ON` queries. Previously, `filter[stale]=true` returned the same results
  as the default (broken) and `filter[stale]=only` returned zero rows due to
  contradictory `stale = false AND stale = true` predicates. Added `is_stale` column
  to `org_container_keys` so all stale/non-stale queries use the same optimized path.
  ([Issue #42](https://github.com/pgarciaq/ros-ocp-backend/issues/42),
  [ADR 0306](../docs/adr/0306-stale-filter-org-container-keys-column.md))

- **NULLS LAST consistency across keyset-paginated endpoints:** All `ORDER BY`
  clauses in keyset-paginated endpoints now include `NULLS LAST` for both ASC and
  DESC directions (container, PVC, snapshot, machineset). The shared
  `keysetSeekClause` and all endpoint-specific seek functions now correctly handle
  NULL sort values — when paginating through a column with NULLs (e.g.,
  `estimated_savings_cents`), rows with NULL sort values appear last and the cursor
  correctly advances through the NULL region by tie-breaker only.
  ([Issue #96](https://github.com/pgarciaq/ros-ocp-backend/issues/96))

### Added

- **VM I/O sparkline fields in daily_digests:** The VM detail endpoint now includes
  `disk_read_iops_p95`, `disk_write_iops_p95`, `disk_read_bps_p95`, and
  `disk_write_bps_p95` in each `daily_digests` item. These fields were already
  stored in the database but were dropped during API serialization. Gated by
  `ROS_VISUAL_INSIGHTS_ENABLED`. The frontend renders them as compact sparklines
  on the VM breakdown page.
  ([Issue #9](https://github.com/pgarciaq/ros-ocp-backend/issues/9))

- **Snapshot age distribution histogram endpoint:** New Visual Insights endpoint
  `GET /recommendations/openshift/snapshots/age-distribution` returns a histogram
  of snapshot counts grouped by configurable age buckets (default: <7d, 7-30d,
  30-90d, 90d+). Supports custom `bucket_boundaries` query parameter. Gated by
  `ROS_VISUAL_INSIGHTS_ENABLED`.
  ([Issue #15](https://github.com/pgarciaq/ros-ocp-backend/issues/15))

- **LRU eviction for term config cache:** Replaced the unbounded `map` + `sync.RWMutex`
  term config cache with a bounded LRU from `hashicorp/golang-lru/v2/expirable`. The
  cache now has mode-aware defaults (5 entries on-prem, 1000 SaaS) and supports LRU
  eviction alongside the existing 60s TTL. Configurable via
  `ROS_TERM_CONFIG_CACHE_MAX_ENTRIES`. New Prometheus metrics:
  `rosocp_term_config_cache_{size,hits_total,misses_total,evictions_total}`.
  ([Issue #35](https://github.com/pgarciaq/ros-ocp-backend/issues/35))

### Added (prior)

- **`org_namespace_keys` materialized table for namespace list pagination:** Namespace
  list queries now use a pre-materialized `org_namespace_keys` table for count and page
  selection (same pattern as `org_container_keys` for containers). The keys table is
  refreshed at the end of each namespace recommendation cycle and excludes stale
  namespaces. Tag-based filtering uses a GIN-indexed `resolved_tags` JSONB column.
  The DISTINCT ON fallback path is preserved for stale-only queries.
  ([Issue #33](https://github.com/pgarciaq/ros-ocp-backend/issues/33))
- **Parallel CSV file download within a manifest:** Files in a manifest payload are
  now downloaded and processed concurrently using bounded parallelism, reducing
  wall-clock ingestion time from `N × avg_download_time` to approximately
  `max(download_times)`. Concurrency is configurable via `ROS_MANIFEST_DOWNLOAD_WORKERS`
  (default 2). A startup warning is emitted when the product
  `ManifestDownloadWorkers × KafkaWorkers` exceeds `DBMaxConns - 2`, indicating
  potential connection pool exhaustion.
  ([Issue #41](https://github.com/pgarciaq/ros-ocp-backend/issues/41))

### Added (prior)

- **Kafka consumer lag metric:** New Prometheus gauges `rosocp_kafka_consumer_lag`
  (per-partition, labeled by `topic` and `partition`) and
  `rosocp_kafka_consumer_lag_total` (aggregate per topic) expose how many messages
  are pending processing. Each processor instance reports lag only for its assigned
  partitions, so `sum(rosocp_kafka_consumer_lag)` across replicas gives the correct
  cluster-wide total. Poll interval configurable via `KAFKA_LAG_POLL_INTERVAL_SECONDS`
  (default 30s). Stale partition labels are cleaned on rebalance; gauges reset on
  graceful shutdown.
  ([Issue #34](https://github.com/pgarciaq/ros-ocp-backend/issues/34))
- **OOM timeline endpoint:** New
  `GET /recommendations/openshift/containers/{id}/oom-timeline` returns per-day OOM
  kill counts for a container (sparse — only days with events). Supports optional
  `start_date` / `end_date` query parameters (default: last 6 months). Returns 404
  for unknown containers, 400 for invalid UUIDs or date ranges. Gated by
  `ROS_VISUAL_INSIGHTS_ENABLED` (default `true`). Enables frontend scatter-plot
  visualization of OOM event patterns over time.
  ([Issue #3](https://github.com/pgarciaq/ros-ocp-backend/issues/3),
  [ADR-0302](../docs/adr/0302-oom-timeline-endpoint.md))
- **CPU throttle trend in boxplot API:** Container boxplot responses now include an
  optional `cpuThrottle` field in each `plots_data` bucket. The field contains `p95`,
  `max` (in cores), and `format` ("cores"). Omitted when both values are zero (no
  throttling occurred). Enables frontend area charts showing throttle envelope
  alongside CPU usage. Namespace boxplots are unaffected (no throttle columns in
  `daily_namespace_digests`). New `ThrottlePlotDetails` struct and OpenAPI schema added.
  ([Issue #4](https://github.com/pgarciaq/ros-ocp-backend/issues/4))
- Deterministic recommendation `id` (UUID v5) on list and detail responses for node,
  PVC, quota, cluster quota, snapshot, and VM recommendations. Formulas match
  koku-ui client-side fallbacks (`internal/model/recommendation_ids.go`).
- **GPU MIG endpoint:** `filter[term]` support on `/recommendations/openshift/gpu/mig`
  allows filtering MIG recommendations by term (e.g. `short_term`, `medium_term`).
- **GPU time-slicing endpoint:** Node-level `classification` field added to
  time-slicing list responses, surfacing rightsized/oversized/undersized/idle state.
- **GPU cost rate configuration:** GPU-aware cost rate integration for savings
  estimations on GPU recommendation endpoints.

### Changed

- **GPU summary `timeslicing.count` aligned with list `meta.count`:** The summary
  endpoint now counts actionable persisted recommendations from
  `node_gpu_timeslicing_recommendations` instead of raw GPU-triple groups from
  `gpu_container_digests`. The badge count now matches the number of rows in the
  time-slicing list table. Falls back to the previous triple-count logic when no
  persisted recommendations exist yet (before the first backfill run).
  ([Issue #72](https://github.com/pgarciaq/ros-ocp-backend/issues/72))
- **COST-7274:** Removed fixed six-type `workload_type` allowlist from the API and
  idle detection settings. The `workload_type` filter now accepts any valid Kubernetes
  owner kind string (max 63 chars, no whitespace, non-empty). CRD-based workload
  types (e.g. `domain`, `virtualmachine`, `kafkanodepool`) are now queryable.
  ([ADR-0300](../docs/adr/0300-remove-fixed-workload-type-allowlist.md))
- Migration 000151 converts `workloads.workload_type` column from
  `sorted_workloadtype` enum to `TEXT`.
- **VM endpoint:** Renamed response field `savings` to `estimated_monthly_savings`
  for consistency with all other ROS endpoints (container, namespace, node, PVC,
  quota, cluster-quota). The `order_by` parameter accepts `estimated_monthly_savings`
  as the primary key; `savings` and `savings_amount` remain as deprecated aliases.
- **GPU time-slicing endpoint:** Renamed `total_node_savings` to
  `estimated_monthly_savings` for consistency with all other ROS endpoints.

### Fixed

- **GPU MIG:** `meta.count` now reflects the actual number of filtered entries
  after post-query filters (term, project, tag, idle state) are applied, instead
  of the pre-filter key count from the database.
- Namespace recommendations: cursor seek pagination bugs, sorting by
  `estimated_monthly_savings`, `cpu_util_p95`, `mem_util_p95`, and `pod_count`
- Node recommendations: cursor pagination fixes and `$0` savings display corrections
- Node savings pagination: cursor sort value type mismatch for mixed numeric types

### Documentation

- Added missing docs-site pages to resolve GitHub Pages 404s
- Updated branch references and development status for Phase 15

## [1.0.0-phase14] — Phase 14: Recommendation Explanations & GPU Time-Slicing Persistence

**Branch:** `pgarciaq-rosocp-superpowers-phase14`

### Planned

- Backfill mechanism for existing recommendations without explanation columns
- UI integration for explanation panels in koku-ui (Phase 5)

### Added

- Typed `expl_*` explanation columns for all native-engine recommendation types ([ADR-0296](../docs/adr/0296-recommendation-explanation-factors-typed-columns.md))
- Migration 000146: explanation columns on live and history recommendation tables
- Engine capture: explanation factors embedded on rec structs and persisted at write time
- `?include=explanation` query parameter on detail endpoints (comma-separated list; opt-in)
- OpenAPI schemas for `ContainerExplanation`, `GPUExplanation`, `NodeExplanation`, and related types
- User-facing documentation: [Understanding Your Recommendations](architecture/understanding-recommendations.md)
- [ADR-0296](../docs/adr/0296-recommendation-explanation-factors-typed-columns.md): Store recommendation explanation factors as typed columns (persist engine intermediate values at write time; expose via detail API `explanation` object)
- [ADR-0297](../docs/adr/0297-gpu-time-slicing-recommendation-persistence.md): GPU time-slicing recommendation persistence at ingest
- Implementation plan: [`docs/plans/recommendation-explanations.md`](../docs/plans/recommendation-explanations.md) covering container, namespace, node, GPU, PVC, quota, cluster-quota, and VM recommendation types
- Implementation plan: [`docs/plans/gpu-time-slicing-persistence.md`](../docs/plans/gpu-time-slicing-persistence.md)
- **GPU time-slicing recommendation persistence** ([ADR-0297](../docs/adr/0297-gpu-time-slicing-recommendation-persistence.md)): move node time-slicing from compute-at-read to compute-at-ingest
  - Migration 000145: `node_gpu_timeslicing_recommendations` table and `node_gpu_timeslicing_recommendations_history` history table
  - `ComputeAndPersistNodeGPUTimeSlicingRecs` engine function persists recommendations during ingest
  - GPU time-slicing list API reads from the persisted table with compute-at-read fallback for unmigrated rows
  - `GET /recommendations/openshift/gpu/timeslicing/history` public endpoint for historical time-slicing recommendations
  - `POST /recommendations/openshift/internal/backfill-gpu-timeslicing` admin endpoint to backfill persisted rows
  - 90-day history retention for time-slicing recommendation history
  - Sources cleaner integration for persisted time-slicing and history rows
  - OpenAPI spec updated for new endpoints and response schemas

### Fixed

- **Gap analysis finding #1 — silent zero savings**: When `KOKU_MASU_URL` is not configured (or cost data is unavailable for a namespace), savings fields are now `null` instead of `$0.00`. A `COST_DATA_UNAVAILABLE` notification code is appended to affected recommendations. A startup WARN log is emitted. `NULLS LAST` is applied unconditionally in cursor pagination for consistent sort behavior.
- **Gap analysis finding #10 — CrashLoopBackOff detection**: Documented as a known limitation. The proper fix requires adding `kube_pod_container_status_restarts_total` to the koku-metrics-operator CSV export (cross-repo change).
- **Gap analysis finding #12 — CPU/memory savings breakdown**: Added `estimated_cpu_savings_cents` and `estimated_memory_savings_cents` columns (migration 148) and exposed as `cpu_savings` / `memory_savings` in list and detail API responses alongside the aggregate `estimated_monthly_savings`.
- **Gap analysis findings #3–#16** (10 items):
  - OpenAPI: added `estimated_monthly_savings` to `order_by` enum (#3), `tags` to `RecommendationListItem` (#6), `filter[container]` parameter (#13), filter alias documentation for `cluster_uuid`/`namespace` (#14), `nullable: true` on all explanation pointer fields (#11), aligned list/detail schema drift (#16), verified `exclude[...]`/`filter[exact:...]` already documented (#7)
  - Code: fixed `businessHoursToDetail()` emitting empty `limits` object when only a `Reason` is present (#9)
  - Tests: added `TestGetNativeRecommendationSetList_OrderByEstimatedMonthlySavings` integration test (#8), `TestApplySavingsEstimates_ZeroConfiguredRates` unit test (#15)
- **Memory floor**: Added `MemFloorKiB` (default 4096 KiB = 4 MiB) to prevent 0 KiB memory recommendations when percentile computations decay to zero. Mirrors the existing CPU floor pattern. Configurable via `ROS_CONTAINER_MEM_FLOOR_KIB` / `ROS_NAMESPACE_MEM_FLOOR_KIB` env vars and the Settings API. Adds `expl_mem_floor_applied` explanation factor. Fixes gap analysis finding #2.
- `DetermineCSVType` misclassified cost management CSV files (`cm-openshift-pod-usage-*`) as `PayloadTypeContainer` due to the default fallthrough; the parser then failed with "missing required column: workload". Added `PayloadTypeUnknown` and an early-out rule for `cm-openshift-*` prefixed filenames so they are skipped.
- Container detail API: `desired_replicas` and `available_replicas` were stored in the database but omitted from the `nativeDetailSelect` SQL query, causing the API to return `null` replica counts
- Added DeploymentConfig support to replica count queries (desired/available replicas now reported for DC workloads)
- [ADR-0298](../docs/adr/0298-composite-key-sweep-stale-detection.md): Post-reconcile composite-key sweep (`MarkUnreportedContainersStale`) marks recommendations stale when their composite key no longer matches any current digest data (e.g., `workload_type` change from `deployment` to `statefulset`). Complements the existing cluster-level staleness check from [ADR-0224](../docs/adr/0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md).
- [ADR-0295](../docs/adr/0295-integer-first-architecture.md): Documented the overarching integer-first arithmetic principle across the native engine

### Documentation

- Linked 10 orphaned documentation pages in `mkdocs.yml` (performance analysis, T-Digest feasibility, requirements, test plan, recommendation IDs, database conventions, HPA/VPA modes, query parameters, notification codes, configuration reference)
- Fixed MkDocs macros plugin conflicts: removed Jinja2-style heading ID suffixes in history-and-quality and virtual-machines pages
- Clarified configuration page labels: "Deployment Configuration" vs "Configuration Reference (Operations)"
- Updated testing page with current test counts (~5,400 total across all repos; ~3,100 added by native engine effort)
- Updated `repo_url` in `mkdocs.yml` to point to the current phase branch

### Removed

- `ROS_USE_NATIVE_ENGINE` — removed; native engine is unconditionally active (see [ADR-0157](../docs/adr/0157-ros-enabled-plugins-replaces-native-flag.md))
- `ROS_ENABLE_VM_RECS` — removed; VM plugin controlled by `ROS_ENABLED_PLUGINS`/`ROS_DISABLED_PLUGINS`
- `DISABLE_NAMESPACE_RECOMMENDATION` — removed; was dead code
- **GPU time-slicing read-time fallback:** Removed the fallback in
  `gpu_enrichment.go` that re-computed time-slicing recommendations at API read
  time when persisted cross-references were missing. All GPU time-slicing data is
  now served exclusively from the persisted table populated during ingestion,
  reducing latency for GPU-enriched container list requests.
  ([Issue #27](https://github.com/pgarciaq/ros-ocp-backend/issues/27))

---

## [2026-06-14]

Phase 13 performance, API contract, and hardening work (branch `pgarciaq-rosocp-superpowers-phase13`).

### Added

- Per-phase Prometheus pipeline histograms (`rosocp_pipeline_phase_duration_seconds`) for `download`, `parse_digest`, `write_digests`, `recommend`, `write_recommendations`, `post_process`, and `metadata_refresh`
- End-to-end pipeline duration histogram (`rosocp_pipeline_total_duration_seconds` with `status=success|error`)
- Grafana dashboard panels for total pipeline duration and per-phase heatmap
- `make test-short` for fast local unit tests (`go test -short ./...`, skips Docker/testcontainers)
- Prometheus counter `rosocp_csv_rows_skipped_total{report_type}` for skipped CSV parse rows
- Migration 000144: per-table autovacuum tuning (`fillfactor=85`) for `recommendation_sets` and `container_usage_samples`
- Processor shutdown drain: Kafka consumer waits for in-flight handlers on SIGTERM (`ROS_SHUTDOWN_TIMEOUT_SECONDS`, default 30s)
- `ROS_SAMPLE_RETENTION_DAYS` (default 45) for shorter retention of `container_usage_samples` and `namespace_usage_samples` partitions, independent of digest retention

### Changed

- Savings recalculation uses `pgx.Batch` (chunk 500) instead of per-row UPDATEs; reduces 3,000+ round-trips to ~6 batch sends per cluster (performance audit v2 DB-N1)
- GPU list enrichment scopes digest queries to page containers via `unnest` filter instead of scanning full cluster; reduces GPU-heavy list API p95 by 30–80% (performance audit v2 API-N1)
- Tag sync replaces per-namespace UPDATE loop with single `unnest`-based batch UPDATE; reduces 200+ statements to 2 regardless of namespace count (performance audit v2 DB-N2)
- Namespace CSV ingestion streams rows incrementally (mirrors container `forEachCSVRow` path) instead of materializing the full file in memory; usage samples flush every 1000 rows and digest groups flush at `ROS_INGEST_FLUSH_BATCH_SIZE` (performance audit B-2)
- **Breaking (plots):** Container and namespace detail `plots_data` buckets now expose digest-based percentile bands (`p50`, `p95`, `p99`, `max`) instead of query-time boxplots (`min`, `q1`, `median`, `q3`, `max`). All terms use daily buckets. Update UI chart components accordingly (ADR-0292, performance audit E-2).
- **Breaking (Prometheus):** Removed high-cardinality `org_id`, `cluster_uuid`, `cluster_id`, and `source_id` labels from fleet metrics. Tenant context is now emitted via structured logs at metric call sites. Affected metrics: `rosocp_analytics_incomplete_total` (`error_type` only), `ros_recommendation_stability` / `ros_recommendation_adoption_rate` / `ros_recommendation_oom_rate` (gauges → unlabeled histograms), `ros_reship_in_progress` (per-org/cluster gauge → fleet-wide concurrent gauge), `ros_reship_*` counters/histograms (unlabeled except `reason` on provider resolution failures), `ros_threshold_recalculation_total` / `ros_savings_recalculation_total` (`recommendation_type`, `status` only), coalescing counters (`rosocp_*_coalesced_total`), `ros_ingestion_file_failures_total` (`report_type`, `error_class` only), `rosocp_internal_endpoint_calls_total` (`endpoint`, `sa_name` only). Update Grafana dashboards and alert rules that filter by org/cluster labels.
- Container list API: paginate `org_container_keys` directly for identity/cluster-metadata sorts instead of `DISTINCT ON` over `recommendation_sets` (~1000× faster page selection at 200k+ containers; performance audit M2)
- VM CSV parse errors: per-row logs downgraded to debug; one summary warn per file when rows are skipped

- Container recommendation list API: use slim list DTO (`BuildListResponse`) instead of full detail assembly; preserves list UI fields while omitting plots and duplicate notification nesting (performance audit A-1)
- Namespace recommendation list API: use slim list DTO (`BuildNamespaceListResponse`) with the same default projection (`short_term` cost); detail unchanged (performance audit S4, H-4)
- List handlers skip GPU/business-hours/currency enrichment when `limit <= 1` (count-only badge/summary calls; performance audit H-4)
- **Breaking (notifications):** Container/namespace detail responses emit `notifications` only on `recommendation_engines.<engine>`; top-level and term-level notification maps removed. List rows expose `recommendations.notification_codes` (integer array) instead of `recommendations.notifications`. Update UI to read engine-level maps on detail and codes on list (ADR-0293, performance audit A-2).
- API middleware: parse `x-rh-identity` once in identity middleware and reuse the cost-management entitlement flag in entitlement middleware (performance audit A-4)
- CSV ingest: single-transaction fast path now requires row count ≤ 25,000 and digest group count ≤ 5,000 (was row count ≤ 50,000 only); large single-file manifests fall through to incremental flush sooner (performance audit B-5)
- List API `Collection` responses use generic `Collection[T]` with typed `data` slices instead of `[]interface{}`, avoiding per-item heap boxing (performance audit A-3)
- Container image build strips debug symbols via `-ldflags="-s -w"` (~30% smaller binary; performance audit I-1)

### Fixed

- Retention sweep now deletes aged rows from non-partitioned recommendation tables (`node_recommendations`, `namespace_recommendation_sets`, `pvc_recommendation_sets`) using `ROS_RETENTION_MONTHS`; fleet summary cache is invalidated for affected orgs

### Added

- ADR-0291: Integer micro-cents savings computation — unified `savings_int.go` helpers replace per-module float64 billing math
- ADR-0288: Precomputed decay weight lookup tables — lazy `sync.Map` tables keyed by integer half-life hours replace per-row `math.Exp` in the digest hot path; documents auto-derive (`window_days × 12`) and ~0.2% quantization accuracy
- Public docs: `docs-site/architecture/decay-weights.md` with decay curve charts under `docs-site/architecture/charts/`
- ADRs 0258-0287: Historical phase decisions — Kruize elimination, per-container granularity, three-term architecture, shadow mode rejection, operator CSV contract, framework/language inheritance, phase deferrals, migration strategy, governance process
- ADRs 0208-0257: Comprehensive coverage for settings architecture, business hours reship, notifications model, staleness semantics, tag sync, effective rates, snapshots, reship concurrency, migration strategy, configuration catalog, plugin system details, API contract decisions, quota/PVC algorithms
- ADRs 0172-0207: Comprehensive architecture decision coverage for idle detection, savings methodology, list query performance, threshold recalculation, node/VM/GPU algorithms, API design patterns, Kafka tuning, RBAC semantics, and retention mechanics
- ADR-0166: Per-file report_file_status tracking with manifest completeness gating
- ADR-0167: Cost-management entitlement middleware (defense-in-depth)
- ADR-0168: Disabled plugin route guards before Echo catch-all
- ADR-0169: Allowlisted native SQL query fragments
- ADR-0170: MachineSet Tier-1 aggregation over node recommendations
- ADR-0171: Streaming recommendation batches for memory bounding
- ADR-0165: Defer recommendations for synthesized manifests (quiet-period debouncer)

### Changed

- CSV ingest: digest group buffers store slim `metricSample` values (~120B) instead of full `MetricRow` structs (~456B+heap strings) between incremental flushes, reducing peak in-memory digest grouping by ~5–10× (performance audit B-1)
- Default `ROS_DB_MAX_CONNS` lowered from 10 to 5 to reduce on-prem connection pressure against bundled PostgreSQL (`max_connections=100`); `DB_POOL_SIZE` retained as a deprecated alias
- Recommendation engine: adaptive margin uses integer-only `ComputeAdaptiveMarginScaledDirect` instead of float CV detour per container rec (performance audit M5)
- Savings estimation: unified integer micro-cents computation in `savings_int.go` replaces duplicated float64 math across container, node, PVC, VM, GPU, and quota engines; rates convert once at load, cents written at output (ADR-0291, performance audit P1-1)
- Query performance: remove redundant `rh_accounts` joins for org scoping on `recommendation_quality`, native/legacy container detail, and native namespace detail queries — filter denormalized `org_id` directly (performance audit P1-4)
- CSV ingest: remove per-row Prometheus gauge update for in-memory digest groups; gauge updates only at flush boundaries (performance audit B-4)
- Notification catalog: `GET /recommendations/openshift/notification-codes` returns `Cache-Control: public, max-age=86400` for static in-memory catalog responses (performance audit A-6)
- GPU classification persistence: `StoreGPUClassifications` uses chunked `pgx.Batch` updates (500 per round-trip) instead of per-container `Exec` (performance audit Q6)
- Idle classification: replace window P95 sort with max-of-daily-P95 for container and GPU idle checks — O(N) scan, no sort allocations; conservative bound may classify fewer workloads as idle when single-day spikes exist (ADR-0290, performance audit Q4)
- PVC recommendation persistence: `WritePVCRecommendations` uses chunked `pgx.Batch` upserts (500 per round-trip) instead of per-PVC `Exec`, reducing database latency for large clusters
- Recommendation engine: fuse CPU and memory weighted-percentile passes into a single `RecommendCPUAndMemory` call (~40–50% fewer digest row walks per container-term-engine)
- Recommendation engine: `windowBounds` returns index ranges for zero-copy term window slicing instead of copying `DigestRow` structs
- Org metadata refresh (`org_container_keys`, `org_recommendation_stats`, fleet summary cache invalidation) deferred to once per reconcile cycle via `RefreshOrgMetadata` instead of after every 500-container write batch — 50–90% reduction in recommendation write time for large orgs (ADR-0289)
- Decay weighting: `DecayWeight()` uses precomputed lookup tables for integer half-lives (plugin defaults and auto-derived `window_days × 12`); non-integer half-lives still use `math.Exp`. When a tenant overrides `window_days` but leaves `decay_halflife_hours` NULL, half-life auto-derives as `window_days × 12` hours
- Corrected ADRs 0084, 0161 with status updates reflecting actual implementation scope
- Updated ADRs 0011, 0013, 0038, 0043, 0132 with status updates cross-referencing new ADRs
- Expanded ADRs 0088, 0102, 0112, 0133, 0136, 0139, 0140, 0145, 0151, 0163 with implementation details
- Fixed ADR-0133 logrus/zerolog drift
- Fixed entitlement middleware code comment (ADR-0149 → ADR-0167)
- Added ADR code comments in 5 key architectural files
- Expanded ADRs 0050, 0086, 0112, 0118, 0125, 0135, 0136, 0162 with post-v4.0 hardening context
- Fixed ADR-0086 implementation reference (custom recalcFlight, not x/sync/singleflight)
- Fixed code comment in manifest debouncer to cite ADR-0165 instead of ADR-0050

### Added

- Adversarial due diligence review v5.0: cumulative integration validation of #77–#84 fixes; zero new findings; all 85 review items closed. (`rosocp_savings_summary_cache_size`, evictions, invalidations, lazy expiry) matching fleet cache parity (adversarial review finding #81 resolved).
- OpenAPI reusable `ForbiddenEntitlementOrRBAC` and `ForbiddenEntitlementOrSettingsLocked` response components; all v1 paths now reference shared 403 components (adversarial review finding #83 resolved).
- ADR cross-reference comments on manifest debouncer, config validation, savings/threshold recalc guards, and savings cache (adversarial review finding #84 resolved).

### Fixed

- CI architectural path manifest expanded with debouncer, config validation, and recalc guard files; workflow filters synced (adversarial review finding #82 resolved).
- Startup config validation warnings for internal tags auth without SA allowlist, permissive/empty CORS in production, and org allowlist with auth disabled (`ValidateConfig`; adversarial review finding #67 resolved).
- Savings summary default rollup cached in memory with same TTL/invalidation as fleet summary; metrics `rosocp_savings_summary_cache_hits_total` and `rosocp_savings_summary_cache_misses_total` (adversarial review finding #68 resolved).
- Fleet summary cache: configurable capacity (`ROS_FLEET_SUMMARY_CACHE_CAPACITY`), Prometheus metrics (hits/misses/evictions/invalidations/lazy expiry), LRU lazy-expiry cleanup via `container/list`, and invalidation on threshold settings, business-hours settings, and savings recalculation triggers (adversarial review findings #65, #66, #69 resolved).
- Manifest debouncer: generation counter prevents double-fire when quiet-period timers race with `Stop()`; shutdown stops pending timers and skips callbacks via processor lifecycle and `asyncjobs` hook (adversarial review findings #79, #80 resolved).
- Fleet and savings summary caches invalidate after retention stale-recommendation purge and Sources destroy analytics cleanup (adversarial review finding #77 resolved).
- Async savings/threshold/reship recalc now invalidates fleet and savings summary caches after coalesced work completes, in addition to pre-trigger invalidation, preventing stale cached aggregates during the recalc window (adversarial review finding #78 resolved).
- Defer recommendation engines for synthesized manifest IDs (`synth-*`) until `ROS_SYNTH_MANIFEST_QUIET_PERIOD` (default 30s) expires with no new file registrations; metric `rosocp_manifest_recommendation_deferred_total` (adversarial review finding #61 resolved).
- Single-flight coalescing for savings recalc, reship, and threshold recalc now uses the latest caller parameters on trailing runs (finding #62 resolved).
- Fix IPv6 private address bypass in CSV URL SSRF protection (adversarial review finding #64 resolved).

### Added

- OpenAPI `ForbiddenEntitlement` response component documenting `cost_management` entitlement requirement on all v1 paths (adversarial review finding #70 resolved).
- `// ADR-NNNN` cross-reference comments at key architectural decision points in Go source (adversarial review finding #74 resolved).

### Changed

- CI governance path files (`.github/openapi-paths.txt`, `.github/architectural-paths.txt`) expanded with maintenance comments and broader globs; workflow filters synced (adversarial review finding #71 resolved).
- Pin `govulncheck@v1.1.4` in CI for reproducible vulnerability scans (adversarial review finding #72 resolved).

- Notification code **77** (`SPARSE_DATA`, INFO): fires when `data_days` is at or below `sparse_data_threshold` (default 2) for container, namespace, node, and PVC recommendations — orthogonal to `LOW_CONFIDENCE` (code 1). Configurable via `sparse_data_threshold` on container/namespace Settings API (`ROS_CONTAINER_SPARSE_DATA_THRESHOLD`, `ROS_NAMESPACE_SPARSE_DATA_THRESHOLD`); migration `000143`.
- Internal endpoint audit logging and metric `rosocp_internal_endpoint_calls_total` (labels `endpoint`, `org_id`, `sa_name`); optional org allowlist via `ROS_INTERNAL_ALLOWED_ORGS` (finding #63 resolved mitigated).
- Advisory CI workflow [`.github/workflows/openapi-changelog-check.yml`](../.github/workflows/openapi-changelog-check.yml): warns when API-affecting paths (see [`.github/openapi-paths.txt`](../.github/openapi-paths.txt)) change without `openapi.json` updates, or when Go files change without `CHANGELOG.md` updates (finding #53 resolved).
- Advisory CI workflow [`.github/workflows/adr-reminder.yml`](../.github/workflows/adr-reminder.yml): reminds authors to review or create ADRs when architectural paths change (see [`.github/architectural-paths.txt`](../.github/architectural-paths.txt)) (finding #54 resolved).
- `govulncheck` CI workflow [`.github/workflows/govulncheck.yml`](../.github/workflows/govulncheck.yml) on PRs and weekly schedule (finding #60 resolved).

### Changed

- Remove unused `aws-sdk-go` v1 phantom dependency (adversarial review finding #60 resolved as non-issue); migrate S3 readiness check to `aws-sdk-go-v2`.

- Cost-management entitlement middleware on v1 API routes: rejects requests without `entitlements.cost_management.is_entitled=true` unless `DEVELOPMENT=true` (finding #35 resolved).
- Internal tag endpoint bearer auth in db mode via `ROS_INTERNAL_TAGS_AUTH_REQUIRED` (default `true`) (finding #37 resolved).
- API shutdown context for async threshold/savings recalc and masu reship jobs with 30s drain grace (finding #47 resolved).
- In-memory LRU fleet summary cache with `ROS_FLEET_SUMMARY_CACHE_TTL` (default 300s), invalidated on recommendation ingest (finding #52 resolved).
- Serialized Kafka offset commits when `ROS_KAFKA_PARALLEL=true` via `kafka.CommitMessage` mutex (finding #57 resolved).
- Configurable history default date window `ROS_HISTORY_DEFAULT_DAYS` (default 30) when `start_date`/`end_date` omitted (finding #51 resolved).

- Adversarial due diligence review **v2.0** ([`docs/audits/adversarial-review.md`](../docs/audits/adversarial-review.md)): fresh audit acknowledging v1.6 remediations (#1–#31) and documenting 29 new findings (#32–#60) across ingestion edge cases, GPU fleet-scale performance, auth hardening gaps, and governance.
- SSRF DNS fail-closed in production: unresolved hostnames block CSV fetch when `DEVELOPMENT=false` (adversarial review finding #34 resolved).
- Per-org single-flight coalescing for savings recalculation and business-hours reship with metrics `rosocp_savings_recalc_coalesced_total` and `rosocp_reship_coalesced_total` (finding #36 resolved).
- Bounded LRU cache for RBAC permissions with `ROS_RBAC_CACHE_MAX_ENTRIES` (default 500) and metrics `rosocp_rbac_cache_size`, `rosocp_rbac_cache_removals_total` (finding #40 resolved).
- Architecture Decision Records: 162 ADRs in [`docs/adr/`](../docs/adr/README.md) with index, covering engine, data model, API, ingestion, plugins, cost, tags, deployment, testing, security, Kafka, and configuration decisions (adversarial review finding #30 resolved).
- Bounded LRU cache for masu effective-rates with `ROS_COST_CACHE_MAX_ENTRIES` (default 1000) and metrics `rosocp_cost_cache_size`, `rosocp_cost_cache_removals_total` (finding #29 mitigated).
- Architecture doc for deterministic recommendation IDs and org_id detail-query invariant (finding #27 verified).
- Threshold recalculation single-flight coalescing per `(org_id, recommendation_type)` with metric `rosocp_threshold_recalc_coalesced_total` (findings #11, #28 mitigated).
- Optional deep readiness checks: `ROS_READINESS_CHECK_KAFKA`, `ROS_READINESS_CHECK_S3` (default `false`); S3 bucket settings `ROS_READINESS_S3_*` (finding #17 mitigated).
- `ROS_API_MAX_NODE_RESULTS` (default `1000`) hard cap for node utilization and GPU time-slicing list endpoints (finding #22 mitigated).
- Migration CI lint [`scripts/lint-migrations.sh`](../scripts/lint-migrations.sh), [`docs/operations/large-table-migrations.md`](../docs/operations/large-table-migrations.md), and [`deploy/migrations/concurrent-index-job.yaml`](../deploy/migrations/concurrent-index-job.yaml) (finding #24 mitigated).
- Configurable strict analytics ingestion mode (`ROS_INGEST_STRICT_ANALYTICS`, default `true`): when enabled, history/quality write failures block recommendation persistence and Kafka offset commit (message retried). Set `false` for degraded mode.
- Security hardening env vars: `DEVELOPMENT`, `ROS_API_MAX_OFFSET`, `ROS_CSV_DENY_PRIVATE_NETWORKS`, `ROS_LOG_POISON_PAYLOAD`, `ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS`.
- Startup validation for CSV SSRF allowlist, tag dev token, and tag SA allowlist (`ValidateSecurityConfig`, `ValidateTagAuthConfig`).

### Changed

- History CSV export uses `RECORD_LIMIT_CSV` (default 1000) instead of the paginated list limit (finding #50 resolved).
- Native container detail lookup uses indexed `container_id` only; pre-migration composite-key fallback removed (finding #59 resolved).
- GPU MIG list (`GET /recommendations/openshift/gpu/mig`) uses SQL key pagination (`ListGPUMIGKeysPage`) instead of loading all clusters into memory (finding #48 resolved).
- GPU time-slicing list rejects unsupported `order_by` values and uses SQL triple pagination for JSON and CSV exports; in-memory fleet fallback removed (finding #49 resolved).
- Plugin ingest hook failures set `clusters.ingest_hooks_failed` and expose `ingest_hooks_failed` / `ingest_hooks_failed_at` on container list responses (finding #43 resolved).
- Business-hours settings changes log masu reship cluster count and return a warning when re-ingestion is triggered (finding #46 resolved).
- `ROS_CSV_MAX_BODY_BYTES` default lowered from 512 MiB to 100 MiB (finding #56 resolved).
- CORS middleware restricts origins via `ROS_CORS_ALLOWED_ORIGINS`; production defaults deny cross-origin unless configured (finding #42 resolved).

- `ROS_INGEST_STRICT_ANALYTICS` now defaults to `true` (strict mode). Set `false` explicitly for degraded mode (finding #45 resolved).
- Kafka consumer no longer logs message payload prefixes at DEBUG; metadata only (finding #38 resolved).

- Legacy Kafka messages without `metadata.manifest_id` now receive a deterministic synthesized manifest ID (`synth-` prefix) derived from `(org_id, cluster_uuid, date)` or a payload fingerprint, enabling per-file tracking and recommendation gating. Emits `rosocp_ingest_manifest_id_synthesized_total` and a WARN log when synthesis occurs (adversarial review finding #32 resolved).

- Money formatting (`FormatCentsToAmount`) uses integer cents division instead of float64 to avoid display rounding errors (finding #26 mitigated).
- Container ingestion degraded mode now sets `clusters.analytics_incomplete`, emits structured warnings, and increments `rosocp_analytics_incomplete_total` when history or quality writes fail (adversarial review finding #9 mitigated).
- GORM now shares the pgxpool via `stdlib.OpenDBFromPool`; `ROS_DB_MAX_CONNS` governs all database connections per process (adversarial review finding #7 mitigated).
- ILIKE filter values escape `%`, `_`, and `\` with `ESCAPE '\\'` (finding #13 mitigated).
- CSV URL fetch requires explicit allowlist in non-development mode; private networks denied by default (finding #12 mitigated).
- Poison Kafka message logs redact payload by default; optional `ROS_LOG_POISON_PAYLOAD` for debug preview (finding #20 mitigated).
- Housekeeper handles SIGTERM/SIGINT with configurable grace period (finding #19 mitigated).
- History endpoints enforce `MAXIMUM_COUNT_PER_QUERY_PARAM` on filter params (finding #25 mitigated).
- `offset` query parameter capped at `ROS_API_MAX_OFFSET` (default 10000) (finding #14 mitigated).
- Tag auth: `ROS_TAGS_DEV_TOKEN` blocked outside development; empty SA allowlist blocked in api mode (findings #15, #16 mitigated).
- GPU time-slicing list uses SQL triple pagination (`CountNodeGPUTriples` / `ListNodeGPUTriplesPage`) instead of loading all clusters into memory (finding #22 mitigated).

---

## [2026-06-10]

### Added

- Kafka DLQ (Dead Letter Queue) support: messages that fail processing after 5 retries (configurable via `ROS_KAFKA_MAX_TRANSIENT_RETRIES`) are routed to `hccm.ros.events.dlq` with forensic metadata headers, unblocking the consumer partition.
- New Prometheus metrics: `rosocp_kafka_dlq_messages_total`, `rosocp_kafka_retries_total`
- New configuration: `ROS_KAFKA_MAX_TRANSIENT_RETRIES`, `ROS_KAFKA_DLQ_TOPIC`
- Incremental digest flush during streaming ingest (`ROS_INGEST_FLUSH_BATCH_SIZE`, default 1000) with metrics `rosocp_ingest_groups_in_memory`, `rosocp_ingest_flush_total`, `rosocp_ingest_flush_duration_seconds`
- Ingestion-specific DB statement timeout (`ROS_DB_INGEST_STATEMENT_TIMEOUT`, default 120s) via `SET LOCAL` on batch transactions; API timeout configurable via `ROS_DB_STATEMENT_TIMEOUT` (default 25s)

### Changed

- Kafka commit resilience: consumer commits offsets only after successful processing; transient failures retry with backoff before DLQ routing (findings #1 and #2 from adversarial review).
- Streaming ingest flushes container-day digest groups incrementally instead of holding the full map until EOF (adversarial review finding #8).

### Fixed

- Native list pagination dropping `workload_type` filter on re-join to `org_container_keys`
- `workload_type` and namespace tag list filters on the org-keys pagination path
- Namespace tag filters crashing legacy SQL path when tags enabled
- E2E blockers: terms handler, fleet summary counts, `workload_type` filter
- Large-cluster ingestion no longer hits the 25s global `statement_timeout` on batch upserts (adversarial review finding #21)

---

## [2026-06-01] — Phase 12: API Polish, Savings Unification & Snapshot

### Added

- Unified `MoneyAmount` savings format (`value` + `units`) across all list APIs with `meta.currency` on responses (migrations 000074, 000132–000138)
- `confidence_level` on node, GPU MIG, and GPU time-slicing recommendations (migration 000133)
- Keyset pagination for PVC and snapshot list endpoints with `has_next` / `next_cursor` (migrations 000134, 000139)
- Snapshot summary endpoint with `filter[recommendation_type]`, `group_by[namespace]`, MoneyAmount costs, and CSV export
- Snapshot settings API with `inventory_fresh_hours`, validation, and async recalculation
- `GET /recommendations/openshift/notification-codes` public catalog endpoint
- `GET /recommendations/openshift/machinesets` aggregation endpoint
- `GET /recommendations/openshift/namespaces/{id}/history` namespace recommendation history
- Node detail API with savings recalculation and fleet consolidation notifications
- Namespace quota recommendations extended for storage and pods with `capacity_freed` exposure
- Per-ResourceQuota identity and extended quota resources (storage, pods)
- Cluster-quota savings recalculation; quota and cluster-quota async recalc on settings PUT
- PVC `vm_name` from storage CSV ingestion exposed on PVC recommendation API
- `filter[term]` on container list API; normalized to `short_term` convention across node APIs
- `filter[project]` as canonical namespace filter alias across all ROS list endpoints
- CSV export (`format=csv`) on remaining list endpoints; expanded columns for PVC, VM, and container exports
- Tag filtering on VM, quota, cluster-quota, and history endpoints
- `ROS_TAGS_ENABLED` default changed to `true`
- Batch analytics refactored into explicit history and quality pipeline hooks
- Migration 000140: `report_file_status` for ingestion file tracking

### Changed

- Renamed `SavingsObject` to `MoneyAmount`; fleet savings `by_plugin` values migrated to structured format
- Savings stored as integer cents internally (P1 fixed-point migration path)
- Standardized two-decimal savings display in JSON and CSV (`currency` column alongside numeric value)
- Idle/zombie detection improvements with dual-engine integration tests

### Fixed

- GPU time-slicing pagination limiting expanded recommendations (triple SQL path and standard path)
- Container and namespace keyset pagination overlap producing duplicate rows
- `filter[term]` normalization on PVC list API
- Savings recalculation endpoint `conn busy` error under concurrent load
- Healthy PVC upserts now return empty `notification_codes` array instead of null
- GPU tag filter gating and tag SQL for list queries
- VM notification code descriptions aligned with `mapping.go`
- Migration roundtrip and `ReadOldRecommendations` test failures
- Staleness threshold default to 48h with `ROS_STALE_DATA_THRESHOLD_HOURS` alias
- `filter[stale]` on namespace list with `STALE_DATA` notification on stale rows
- Cluster-quota notification catalog filter codes; object-count wired into CRQ utilization, risk, and blocking

---

## [2026-05-31] — Phase 11: Virtual Machine Recommendations

### Added

- VM recommendations plugin (Preview/Beta): vCPU, memory, and disk right-sizing with notification codes 50–63 (migrations 000089–000107)
- VM monthly savings estimates from Koku `effective_rates`; VM savings in fleet summary with `vm_cost_per_month`
- VM placement recommendations: correlated workload detection, NUMA node memory heuristic, codes 60–63
- VM power-off scheduling recommendation
- VM storage tiering notifications (simplified) and Network QoS notifications (SR-IOV/DPDK suggestions)
- Sequential vs random disk I/O profiling for VMs
- Production-quality vGPU time-slicing recommendations with read-time savings
- VM history endpoint with retention behavior; VM settings API with `settings_locked` and dedicated `/settings/vm` routes
- GPU classification thresholds exposed in VM Settings API; `cpu_adaptive_margin_enabled` setting
- Server-side filters: `is_network_bound`, `guest_os`
- Backward compatibility for partial operator upgrades (GPU columns, `restart_count`, VM preferences)
- Comprehensive notification codes reference (internal + public docs-site)
- Dedicated `/settings/{type}` endpoints; `/settings/thresholds?recommendation_type=` deprecated

### Changed

- n1 network-optimized VM recommendations enabled (active, not deferred)
- vGPU profiles and `gpu_catalog.yaml` MIG profiles aligned with NVIDIA documentation
- OpenAPI spec comprehensively updated for VM, GPU, nodes, quotas, and all settings endpoints

### Fixed

- VM test plan counts and notification matrix coverage
- Unit test failures for partitions, notifications, and migrations
- `TestDefinitionsMatchDB`: sync notification codes 58–59

---

## [2026-05-27] — Phase 10: Quota & ClusterResourceQuota Recommendations

### Added

- Namespace quota recommendation plugin with `GET /recommendations/openshift/quota` list and detail endpoints
- Quota settings API with 3-tier resolution (org → cluster → namespace) at `GET/PUT/DELETE /recommendations/openshift/settings/quota`
- ClusterResourceQuota (CRQ) recommendation plugin with list, detail, and settings endpoints
- CRQ settings API with env-var defaults and 3-tier resolution
- `DetermineCSVType` refactor supporting nise-style filenames; integration test for filename detection
- Comprehensive quota and CRQ unit and integration tests
- OpenAPI spec, API cheatsheet, and Bruno collection entries for quota endpoints

### Fixed

- Quota plugin docs-site alignment, default values, registry messages, redundant hooks
- Quota used columns wired in ingestion with backward-compatible handling for older operator versions

---

## [2026-05-21] — Phase 8–9: Tags, Idle Detection, Business Hours & Performance

### Added

- **Tag filtering**: Koku-aligned `filter[tag:key]` and legacy `tag=key:value` syntax on container, namespace, node, GPU, VM, quota, CRQ, and history list APIs; `meta.warnings` on empty tag-filter results
- Tag sync receiver reading Koku tag tables directly (on-prem) with SA auth, full-replace semantics, and `GET /recommendations/openshift/tags/status` endpoint (migration 000082)
- Fleet savings summary `group_by[tag:key]` for per-tag-value container savings
- `ROS_TAGS_SYNC_MAX_BODY_MIB` env var (replaces undocumented `ROS_TAGS_SYNC_MAX_BODY_BYTES`)
- **Idle/zombie detection**: inline engine classification with DB persistence, settings API, notification codes, and GPU idle/zombie detection (migration 000083)
- Phased plugin execution model (Phase 1 Produce / Phase 2 Enrich / Phase 3 Post-process with barriers)
- **Business hours**: schedule domain logic, org/cluster/namespace settings API (`GET/PUT/DELETE`), dual-stream ingestion and recommendation engine, reship client with single-flight lock and retry poller, IANA timezone support via `tzdata` in container image (migrations 000076–000081)
- Env vars: `ROS_BUSINESS_HOURS_ENABLED`, `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK`, `ROS_RESHIP_POLLER_INTERVAL_SECS`, `ROS_RESHIP_MAX_RETRIES`, `ROS_RESHIP_CONCURRENCY`
- **Threshold settings API** with per-org TTL cache and async recalculation after changes (migration 000073)
- **Keyset pagination** for container and namespace lists with opaque cursors, `has_next`, `next_cursor`, and `after` query parameter (migration 000079+)
- `org_container_keys` table for efficient list pagination at 200k+ containers per org
- Per-phase Prometheus histograms: `rosocp_pipeline_phase_duration_seconds`
- Counters: `rosocp_recommendations_written_total`, `rosocp_ingestion_errors_total`
- Structured logging with `org_id`, `cluster_uuid`, and `request_id`; Echo RequestID middleware
- Operational runbook (`docs/operations/runbooks.md`)
- MkDocs public documentation site (`docs-site/`) with CI deployment from feature branches
- Per-plugin configurable recommendation terms
- Centralized `internal/config.Config` struct replacing scattered `os.Getenv` calls
- Plugin registry with `ROS_ENABLED_PLUGINS` (fatal if both `kruize` and native plugins enabled)
- Gzip middleware for API responses over 1KB
- RBAC permissions in-memory cache with configurable TTL (`ROS_RBAC_CACHE_TTL`)
- Configurable Kafka consumer worker pool (`ROS_KAFKA_PARALLEL`, `ROS_KAFKA_WORKERS`)
- PostgreSQL `statement_timeout` (25s default) on pool and GORM connections
- pgx pool tuning env vars: `ROS_DB_MAX_CONNS`, `ROS_DB_MIN_CONNS`, `ROS_DB_ACQUIRE_TIMEOUT_SECS`, etc.

### Changed

- Recommendation pipeline refactored to streaming architecture (batch-of-500, O(batch) memory)
- All engine/ingestion packages use centralized `internal/logging` package
- API handlers use consistent `hlog` pattern with org_id + request_id context
- GPU filters pushed to SQL for correct pagination (removed in-memory post-query filtering)
- Fixed-point integer storage for savings (cents), GPU digest metrics (basis points), and node sizing (millicores/KiB)
- P0 query rewrites: filter `org_id` directly instead of `rh_accounts` join
- Performance optimizations: fused weighted percentile pass, parallel GPU/node processing, batched business-hours enrichment, request-scoped enrichment cache, eliminated separate COUNT query in list APIs, streaming CSV ingestion

### Fixed

- GPU pagination returning incomplete pages when filters applied post-query (#496)
- `rosocp_partition__missing_error_total` metric name double-underscore (#329)
- Missing ingestion error counter (#330)
- Engine math edge cases: negative savings, zero-division in margin (#256, #262, #263)
- `workload_type` missing from PKs causing silent data collisions (#346, #349)
- Business hours: namespace prune on PUT, reship clearing, cluster-scoped prune logic
- Threshold settings: `locked_fields` null handling, unknown field validation
- Integer cents and fixed-point math for monetary precision

---

## [2026-05-18] — Phase 6–7: Plugin Architecture & Production Hardening

### Added

- Plugin framework: registry, trait interfaces (Producer/Enrich/Retention/APIProvider), and plugins for container, namespace, GPU, node, PVC, snapshot, quota, cluster-quota, VM, and Kruize legacy marker
- `ROS_ENABLED_PLUGINS` replaces `ROS_USE_NATIVE_ENGINE` boolean
- Streaming recommendation pipeline with per-phase metrics and operational runbook
- GPU catalog extracted to YAML with unrecognized model alerting
- Upgrade runbook for Kruize-to-native migration safety
- Contract test for Koku `effective_rates` API
- `.env` file support via godotenv for local development
- Comprehensive developer guide in `CONTRIBUTING.md`
- Clowdapp database version bumped from 13 to 16

### Changed

- `KAFKA_AUTO_COMMIT` default flipped to `false` (manual commit after processing)
- GPU threshold globals replaced with `GPUThresholds` struct
- Background deletion of Kruize-era tables before cluster CASCADE

### Fixed

- P0 security: IDOR, SSRF hardening, fleet RBAC enforcement
- P0 data safety: snapshot reconcile guard, scoped partition creation
- P0/P1 pipeline reliability: transactions, batching, Kafka commits
- P1 API correctness and silent failures (503 on DB errors)
- P2 audit: cascade delete on source removal, node PK completeness, `/readyz` endpoint, Prometheus metrics, probe config, SQL-level pagination for node/GPU handlers
- Dead code and naming issues (#383, #386, #390, #391, #393, #396–#398)
- Migration safety: regex validation for `cluster_uuid::uuid` cast (000041), deadlock prevention between 000058 and `PersistNodeRecommendations`, safe index rebuild for 000045 on large databases
- Reconciliation audit: 52 unmarked P0–P2 issues resolved; 490-issue audit closed

---

## [2026-05-01] — Phase 6: Native Engine Feature Complete (v1.0)

Initial release of the native Go recommendation engine replacing Kruize for production workloads.

### Added

- **Container recommendations**: decay-weighted CPU/memory percentiles with adaptive margin, dual cost/performance outputs, idle detection, trend slope notifications
- **Namespace recommendations**: aggregate boxplots, P60/P98/P99 memory percentiles, memory trend slope notification (migrations 000031–000036)
- **GPU recommendations**: DCGM PROF_ profiling metrics (SM_ACTIVE, PIPE_TENSOR_ACTIVE, DRAM_ACTIVE), workload classification (idle, underutilized, memory_bound, well_utilized), MIG profile selection, Tier 1/Tier 2 model support (migrations 000042–000045)
- **GPU time-slicing**: node-level time-slicing recommendations with pagination, RBAC, and dollar savings (migration 000044)
- **GPU API restructure**: `/gpu/timeslicing`, `/gpu/mig`, `/gpu/summary`; node CPU/memory at `/nodes`
- **Node utilization**: underutilized, overcommitted, stranded resource detection with EMA-smoothed imbalance score; term-based windowing (short/medium/long)
- **Node right-sizing engine** with configurable thresholds and EMA smoothing (migrations 000052–000054)
- **PVC right-sizing**: oversized, near-full, orphaned, growth trend detection (migration 000048, 000064)
- **Snapshot staleness detection** end-to-end with inventory retention sweep
- **Idle/abandoned workload detection** with full savings estimation
- **Adoption detection** (15% tolerance matching)
- **Stale data detection** and lifecycle management
- **Estimated monthly savings** via Koku `effective_rates` integration including distributed costs
- **Replica count** (`pod_count_min/max/avg`) from operator `workload_pod_count` column (migration 000039)
- **Custom term configuration API**: `GET/PUT/DELETE /recommendations/openshift/settings/terms`
- **Unified settings API** with env-var locking and cost model gap analysis
- **Recommendation history and quality tracking** APIs with CSV export (migrations 000030+)
- **Fleet summary endpoint** with savings aggregation
- **Historical tracking**: partitioned `recommendation_history` and `recommendation_quality` tables with separate retention policies
- **Query-time boxplots** from raw `container_usage_samples` and `namespace_usage_samples`
- **Notification codes**: persisted definitions with API mapping (migration 000027); always returned in list/detail responses
- **OOM feedback**: logarithmic memory bump from `oom_count` CSV column (`ROS_OOM_BASE_BUMP`, `ROS_OOM_MAX_BUMP`)
- **Quality metrics**: stability, adoption, OOM rate, recommendation age
- **Kruize vs Native comparison tool** for quantitative algorithm verification
- **CSV export**, current values, stale detection on all container/namespace list endpoints
- **cluster_uuid TEXT → UUID** migration (000041) fixing join operator errors
- **Limit variation**, integer percentages, whole MiB rounding on recommendations
- **Composite indexes** for native list query performance (migration 000061)
- Container image updated to `ubi10/go-toolset:1.25`

### Changed

- Native list API response aligned with Kruize-compatible JSON shape for UI backward compatibility
- Legacy Kruize fallback removed from native recommendation handlers
- Namespace recommendations enabled by default with Unleash kill switch
- Namespace recommendation feature flag gating removed

### Fixed

- Container memory P60/P98/P99 parity: pipeline now stores all percentiles in `daily_container_digests` (migration 000035)
- Namespace memory trend slope notification (previously discarded by evaluator)
- Phase 6 critical audit: write bugs, integration tests, migration 000034
- GPU ingestion pipeline wired end-to-end (digest upsert, query, API enrichment)
- GPU savings: `$0` vs `null` semantics for well-utilized GPUs; org_id prefix mismatch calling Koku
- Short-term recommendations anchored to latest digest date
- Fleet summary query using `medium` term instead of `medium_term`
- Three correctness bugs in recommendation engine
- Migration renumbering conflicts resolved (000023–000043 range consolidated)

---

## [2026-04-30] — Phase 5: History, Boxplots & Retention

### Added

- Recommendation history tracking in partitioned monthly tables
- Raw `container_usage_samples` table for query-time boxplot assembly (exact five-number summaries via `percentile_cont`)
- Retention sweep for digests, samples, history, and quality tables with configurable periods (`ROS_RETENTION_MONTHS`, `ROS_HISTORY_RETENTION_DAYS`)
- Strongly-typed `DetailResponse` struct replacing raw JSON manipulation for Kruize-compatible UI shape

### Changed

- Native detail response matches Kruize-compatible UI JSON shape

### Fixed

- Test failures from Phase 5 DetailResponse shape change

---

## [2026-04-29] — Phase 4: OOM Feedback & Quality Tracking

### Added

- OOM bump for memory recommendations: `bump = 1 + 0.15 × log₂(1 + oom_count)`, capped at 1.60×
- Recommendation quality writer with 4 metrics: stability, adoption, OOM rate, recommendation age
- Auto-create digest partitions on first write
- E2E test for OOM pipeline (cross-repo: operator `oom_count` column, nise test data, backend parser)
- Safety clamps and tuple filter on quality writer keys including `WorkloadType`

### Changed

- Native CSV parser columns aligned with operator/nise output (`cpu_request`, `mem_request`, etc.)
- Legacy GORM query compatible with native engine rows
- API always returns `notification_codes` and `notifications` arrays (never omitted)

### Fixed

- Pipeline ordering: quality writer runs after recommendation write, skipped on read failure
- Compare tool uses operator column names and includes `oom_count`

---

## [2026-04-25] — Phases 1–3: Native Go Recommendation Engine Foundation

### Added

- Native Go recommendation engine with "read once, compute N terms" architecture
- Daily digest schema: `daily_container_digests`, `daily_namespace_digests` with RANGE monthly partitioning (migrations 000025–000028)
- Test infrastructure: testcontainers PostgreSQL 16, golang-migrate, deterministic fixtures
- CSV parsing with float→int64 conversion, NaN/Inf validation, stable row ordering
- Digest computation pipeline: exact percentiles on ~96 int64 values per day
- Decay-weighted percentile recommendations with adaptive margin and 25mc CPU floor
- Dual cost/performance recommendation outputs per term
- Notification code persistence and mapping (Phase 3)
- API fallback handlers, `container_id` migration, scale benchmarks (Phase 2)
- Exclude and exact filter support for container List API
- Percentage sorting columns for recommendations (RHINENG-20862, RHINENG-25638)
- Kruize vs Native Engine comparison tool and documentation

### Changed

- Go upgraded to 1.25; dependencies updated

---

## [2026-04-20] — Phase 0: Critical Robustness Fixes

### Added

- HTTP client timeout (30s default via `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS`) on all outbound calls including Kruize REST
- Dead-letter handling for poison Kafka messages with max retry count
- Context cancellation checks in long-running CSV processing

### Fixed

- RBAC nil pointer panic when permissions service returns error
- API returning HTTP 200 on database failure (now 503)
- Kafka type assertion panics on unexpected message types
- Kafka subscribe failure silently ignored (now exits consumer)
- Non-deterministic CSV row order from map iteration
- GORM insert errors silently swallowed
- Date parse errors returning zero time instead of error
- Kafka payload logged at Info level (moved to Debug with truncation)
- SendMessage failure not propagated to caller

---

## Migration Reference

Operators upgrading from Kruize or earlier native-engine builds should run all migrations through **000140**. Key migration groups:

| Range | Purpose |
|-------|---------|
| 000025–000028 | Daily digests, notification codes, relational recommendation_sets columns |
| 000030–000036 | Usage samples, namespace percentiles, container memory P60/P98/P99 |
| 000039–000045 | Pod count, UUID types, GPU digests, node name on GPU digests |
| 000048–000064 | PVC and snapshot notification codes, PVC term column |
| 000052–000054 | Node digests and node recommendations |
| 000073–000074 | Threshold settings, savings integer cents |
| 000076–000083 | Business hours schema, tag sync metadata, idle state columns |
| 000089–000107 | VM recommendations, enhancements, savings, power schedule |
| 000110–000140 | Namespace schedule type, node idle state, PVC VM name, keyset indexes, savings cents renames, snapshot costs, report file status |

See `docs/operations/upgrade-runbook.md` for Kruize-to-native migration steps and pre-migration `CONCURRENTLY` index guidance for large production databases.
