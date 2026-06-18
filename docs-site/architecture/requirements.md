# ros-ocp-backend with Superpowers — Requirements Document

> **Current implementation (v4.0+):** The native Go engine does **not** use t-digest. Percentiles are **exact**, computed in Go via `slices.Sort()` on values loaded from **daily digest** rows (hourly metrics aggregated at ingestion). TimescaleDB, `tvondra/tdigest`, and `ROS_USE_TDIGEST` were explored and **not adopted** (Fifteenth / Twenty-first reviews below).

> **Date:** 2026-03-26 (last updated: 2026-05-11)
> **Last triage:** 2026-03-26 — all repos triaged against `main` (autotune: `mvp_demo`). Added REQ-5.6 (cost-aware GPU recs leveraging Koku MIG support). All existing requirements remain valid.
> **Risk review:** 2026-03-26 — 17 architectural risks/gaps identified and resolved. Added: decay workaround for `rollup()`, separate hypertables per metric domain, OOM column, continuous aggregate refresh policy, recommendation history hypertable, RBAC/pagination for new endpoints, Koku cost integration, Go-side CSV validation, Kafka partitioning by cluster, UI pending notes, recommendation quality metrics, per-capability feature flags.
> **Second review:** 2026-03-26 — 14 additional issues resolved. Fixed: cluster_uuid TEXT→UUID, dual-model percentile output (cost+perf in single pass), customer-defined percentile parameters, Phase 1→3 dependency (CA DDL in Phase 2), shadow mode (Go exact vs PL/pgSQL t-digest), business hours dropped (decay+percentile sufficient), namespace recommendation coverage (REQ-1.13), NFR-2 memory consumers, §18 oom_count, compression/retention for all hypertables, on-prem /ingest auth, OQ#2/#11 marked resolved, recommendation_sets PK with term+engine, dollar savings precision (rates + markup + distributed cost note), continuous aggregates for GPU/PVC/VM.
> **Node-level recommendations:** 2026-03-26 — Added Phase 8c (Node & MachineSet Recommendations, Weeks 14–20). Tier 1: node utilization visibility (underutilized, overcommitted, stranded resources) using existing `cost:node_*` data. Tier 2: MachineSet right-sizing (instance type, replica count). Tier 3: MachineAutoscaler optimization (saturated, idle, flapping). New schemas: `ros_node_metrics` hypertable, `daily_node_digests` CA, `node_recommendations`, `machineset_recommendations`. New API for Tier 1 node utilization: endpoints under **`/nodes`** (and future **`/machinesets`** for Tier 2/3). **GPU time-slicing** recommendations are **not** node-utilization APIs — they are served at **`GET /recommendations/openshift/gpu/timeslicing`** (not `/nodes`). New operator queries: ~6. Instance type catalog for cross-cloud right-sizing. 11 new REQs (8c.1–8c.11).
> **Third review:** 2026-03-27 — 19 issues resolved (4 critical, 5 significant, 6 complexity reductions, 4 minor).
> **Fourth review (user feedback):** 2026-03-27 — Fixed: C1 (TimescaleDB Toolkit package claims corrected — requires Timescale's packagecloud repo, not standard OS repos; added container image evaluation: Crunchy PGO 6.0 + tvondra/tdigest recommended for OpenShift), S1 (AWS instance catalog: tiered approach because Koku IAM policy lacks `ec2:Describe*` — Tier 1 uses public AWS Bulk Pricing JSON (no auth), Tier 2 optional `EC2.DescribeInstanceTypes` if customer adds permission), L5 (ephemeral storage: removed incorrect "OCP 4.19+" qualifier — cadvisor gap persists through OCP 4.21, the latest release). Key changes: TimescaleDB Toolkit availability verified (v1.22.0, Oct 2025) with `tvondra/tdigest` fallback plan added. Node request sum queries added (C2). R15 updated with 1-engineer+Claude staffing model. Staleness detection added (REQ-10.7). Instance type catalog rewritten to use tiered approach (AWS Bulk Pricing JSON public API + optional EC2 API, Azure public Retail Prices API, GCP machineTypes API) instead of embedded binary. Fleet-level cross-cluster endpoint added. PDB-awareness added as notification on MachineSet recs. mTLS for on-prem `/ingest`. QoS class recommendations deferred (implicit from CPU/memory recs). Multi-timescale merge REQ-4.5 removed (redundant with decay). Quality metrics simplified to OOM rate + recommendation stability + adoption detection. Confidence bounds deferred to post-MVP. Ephemeral storage scoped to OCP 4.21 latest, still unreliable. Node.js recs made informational. Notification code reference table defined. Phase 8c operator dependency explicitly noted.
> **Fifth review (consistency):** 2026-03-29 — 16 findings resolved. Fixed: F1 (all SQL updated from Toolkit `approx_percentile`/`rollup`/`mean` to tvondra/tdigest `tdigest_percentile()` + DBaaS `percentile_cont()` fallback note for EDB/Crunchy managed), F2 (`recommend_cpu()` return signature now includes `namespace`+`workload` for unique container identification), F3 (QoS removed from Gantt chart, API response, backward compat table), F4 (`recommendation_quality` schema aligned with simplified REQ-10.6: dropped `accuracy_score`/`savings_realized_pct`, added `stability_pct`/`adoption_detected`), F5 (notification codes unified to `UPPER_SNAKE_CASE` matching DB `notification_code_definitions` table throughout), F6 (added `recommendation_applied_at` and `stale` columns to `recommendation_sets` ALTER TABLE), F7 (added `oom_count_sum` to `daily_container_digests` continuous aggregate for `recommend_memory()`), F8 (ephemeral storage env var description corrected), F9 (unified `recommend_all_workloads()` naming), F10 (AWS catalog refresh unified to daily, matching AWS daily price notifications), F11 (REQ-10.8 phase corrected to 10 in traceability matrix), F12 (added REQ-7.6 fleet-summary and REQ-1.14 notification-codes with traceability rows), F13 (added `ROS_ENABLE_SHADOW_MODE` to NFR-5), F14 (added mTLS cross-reference in REQ-10.3 on-prem section), F15 (`ros_metrics` column order unified: identifiers → OOM → CPU → memory), F16 (`CREATE EXTENSION` in §18 changed from `timescaledb_toolkit` to `tdigest`).
> **Sixth review (platform compatibility):** 2026-03-29 — Added Supported PostgreSQL Platforms matrix (§2) with 12-row compatibility table covering Crunchy PGO, Timescale Cloud, Azure, Aiven, CloudNativePG (upstream), ODF CloudNativePG, AWS RDS, Google Cloud SQL, AlloyDB, EDB BigAnimal, bare metal, and Red Hat sclorg images. Changed `ROS_USE_TDIGEST` default from `true` to `auto` (startup probes `pg_extension` for tvondra/tdigest; `true`=force, `false`=force fallback). Fixed stale "TimescaleDB + Toolkit" reference in §2 architecture diagram → "TimescaleDB + tvondra/tdigest". Updated REQ-10.4 deployment manifests to reference Crunchy PGO `PostgresCluster` for on-prem. Clarified why Red Hat's sclorg PostgreSQL images, `timescaledb_toolkit`, and ODF CloudNativePG operator are not suitable (ODF CNPG is NooBaa-internal only, no TimescaleDB; TimescaleDB requires `shared_preload_libraries`, incompatible with CNPG 1.27's dynamic `extension_control_path` loading). Added Timescale Cloud as recommended managed DBaaS for AWS/GCP (where RDS/Cloud SQL lack TimescaleDB). Explained auto-detection mechanism: probe → log → expose via `/status`.
> **Seventh review (consistency):** 2026-03-29 — 5 findings resolved. G1: Timescale Cloud tvondra/tdigest column corrected from "YES (via Toolkit)" to "YES (native)". G2: REQ-3.3 SQL example replaced stale `approx_percentile()`/`rollup()` with `tdigest_percentile()`. G3: §18 `recommend_cpu()`, `recommend_memory()`, `detect_idle()` return signatures corrected from `container_id TEXT` to `out_namespace TEXT, out_workload TEXT, out_container_name TEXT`. G4: Removed `ROS_TIMESCALEDB_ENABLED` env var (TimescaleDB is mandatory, no toggle). G5: Removed `ROS_USE_NATIVE_CPU_REC` and `ROS_USE_NATIVE_MEM_REC` env vars (no Kruize fallback exists in the new binary), updated feature-flag prose to reference actual flags (`ROS_USE_TDIGEST`, `ROS_USE_OOM_FEEDBACK`, `ROS_ENABLE_GPU_RECS`, `ROS_ENABLE_VM_RECS`).
> **Eighth review (feature inventory):** 2026-03-29 — Added comprehensive Feature Inventory table between ToC and §1. 60 features cataloged across 10 categories (Infrastructure & Pipeline, Container Recommendations, GPU, Tier 1 New Recs, Replica Count & Cost, Tier 2 New Recs, VM, Node/MachineSet, JVM/Quarkus, Quality/Lifecycle, Bug Fixes). Each feature has description, phase, REQ cross-references, operator dependency flag, status (Active/Deferred/Removed), and clarifications. Two deferred features (F20: confidence bounds, F29: QoS class), one removed (multi-timescale merge), one deferred-low (F25: multi-GPU). All 87 active REQs accounted for.
> **Ninth review (legacy comparison):** 2026-03-29 — Added "vs Legacy" column to all Feature Inventory tables, comparing each feature against the current ros-ocp-backend (`main`) + Kruize autotune (`mvp_demo`) pipeline. Classification: Net-new (39), Enhanced (13), Bug fix (5), Deferred (3). Added "Legacy Features Not Carried Forward" section documenting 8 legacy capabilities and their disposition: box plots (recoverable via t-digest), performance profiles (replaced by function params), experiment lifecycle (eliminated), Kruize PostgreSQL (eliminated), `/updateResults` batching (eliminated), variation % (preserved, computed from relational columns), CSV export (preserved), unit conversion (preserved), RBAC (preserved with fixes). Added "Legacy Comparison Summary" statistics table. **No legacy feature is lost** — all preserved capabilities carried forward with equivalent or better implementation.
> **Tenth review (comprehensive risk):** 2026-03-29 — 15 risks/gaps resolved. R-1 CRITICAL: Added explicit MAX columns to continuous aggregates (t-digest p100 ≠ exact max). R-3: Added `recommendation_profiles` table + `box_plot()` convenience function. G-1→NFR-6: Connection pooling (pgxpool + PgBouncer). G-2→NFR-7: Backup/DR strategy (pgBackRest). G-3→Q3 RESOLVED: Single PG instance with PgBouncer isolation. G-5: box_plot() in §18. G-6→NFR-8: CA schema migration procedure. M-3→NFR-9: On-prem auth strategy (token v1, mTLS future). M-6→R19: Unleash-controlled Kafka routing (single active consumer). R18: AWS Bulk Pricing streaming JSON strategy. R20/R21: Memory p100 fix and recommendation_profiles documented as risk resolutions. New env vars added to NFR-5.
> **Eleventh review (operator dependency audit):** 2026-04-05 — Audited operator impact claims across Feature Inventory, Section 16, and traceability matrix. Fixed: F8 "Two-binary deployment" Operator? corrected from Yes to No (operator is unaffected by server-side binary deployment — it just uploads to the ingress endpoint). F2/REQ-2.3 "Integer types" operator row struck through in Section 16 and traceability matrix corrected to No (conversion happens in ros-ocp-backend at CSV parse time per earlier decision). F27/REQ-6.3 "PVC right-sizing" Operator? corrected from Yes to No — ros-ocp-backend reads the existing `cm-openshift-storage-usage-YYYYMM.csv` directly instead of duplicating PVC data in a new ROS CSV; SaaS requires only a Koku `kafka_msg_handler.py` routing change. Added future optimization note to REQ-8b.1: the ~11 `cost:vm_*` and 12 `ros:vm_*` operator queries overlap significantly; post-MVP, unify VM data collection at 15-min ROS granularity and let Koku aggregate to hourly. Corrected stale "new operator (integer CSV)" in deployment model table. **Major correction:** F9 "On-prem ingestion (no Kafka)" removed — the cost-onprem Helm chart deploys AMQ Streams (Kafka), so on-prem uses the same Kafka + S3 ingestion path as SaaS. No custom `/ingest` endpoint, mTLS, or `ROS_INGEST_TOKEN` needed. Deployment model table fixed: old binary on-prem was "N/A (SaaS only today)" → corrected to "Kafka consumer (same as SaaS)". REQ-10.3 on-prem flow rewritten to match SaaS. NFR-9 updated to reflect same-as-SaaS auth (no custom endpoint auth needed). Corrected SaaS ingestion routing: both binaries in different consumer groups, Unleash flag evaluated per `org_id` for gradual rollout. Codebase column corrected: fork of existing ros-ocp-backend, not rewrite. Red Hat sclorg PG images updated from "NO" to "Possible" with `rpm --nodeps` workaround. REQ-1.8: "Min Hours" → "Min Data Required" with partial-data thresholds + "Decay Half-Life" column. REQ-1.9: added "Recommendation returned?" column (INFO_NOT_ENOUGH_DATA suppresses, others accompany).
> **Twelfth review (codebase verification):** 2026-04-05 — REQ-5.6: Verified Koku MIG dual-path gap against koku, koku-ui, koku-metrics-operator codebases. Note rewritten with precise detail: two Trino SQL templates (`monthly_cost_gpu.sql` all_labels omits MIG labels; `reporting_ocp_gpu_summary_p_usage_only.sql` omits MIG columns) are the only gap — Parquet processor, Hive table, OCPGpuSummaryP model, API endpoints, and UI all support MIG. PriceList models confirmed not wired to API. Operator MIG branch `cost-7178-mig-metrics` confirmed not merged. REQ-8c.6: Added deprecated/unlisted instance type handling — decision matrix for 6 scenarios (in-catalog × right-sized), two new notifications (`INFO_INSTANCE_TYPE_NOT_IN_CATALOG`, `INFO_INSTANCE_TYPE_DEPRECATED`), cost comparison for unlisted types (`current_cost = NULL`). Key principle: never recommend a switch solely because current type is not in catalog. NFR-1: Removed stale `/ingest`, mTLS, `ROS_INGEST_TOKEN`, advisory lock references from on-prem section — replaced with same-as-SaaS Kafka path.
> **Thirteenth review (open questions resolution):** 2026-04-05 — Resolved 16 of 18 remaining open questions (OQ#1,4–10,12–20), leaving only OQ#2 and OQ#11 (previously resolved). Fixed 5 stale references: (1) REQ-6.3 PVC on-prem bullet still said `/ingest`→ corrected to S3+Kafka. (2) NFR-5 env table `ROS_INGEST_TOKEN`/`ROS_INGEST_MTLS_ENABLED` struck through as removed. (3) NFR-6 pool description dropped `/ingest handlers`. (4) R10 resolution updated from "on-prem advisory lock" to same-as-SaaS Kafka partitioning. (5) Migration strategy §20 and R19 resolution corrected from "single active consumer" to "dual consumer groups with per-org_id Unleash routing" matching the deployment model table. OQ resolutions: #1=fail-closed 424 matching Koku pattern, add `ROS_RBAC_TIMEOUT`. #4=DEFERRED (deployment decision). #5=dual-write (already specified in §20). #6=limit ≠ request, `limit_multiplier=1.0` default. #7=10% GPU threshold matching DCGM/AWS convention. #8=global idle threshold, no per-namespace. #9=defer VPA+HPA combined, HPA-managed workloads get informational notifications only. #10=JVM detection reliable enough for informational recs. #12=min/max/avg replica count. #13=hybrid SQL/Go confirmed. #14=full shadow comparison on all clusters. #15=golang-migrate. #16=40% VM hysteresis. #17=informational IOPS only. #18=uniform threshold for MVP. #19=12 VM queries acceptable. #20=Phase 8b if catalog available from 8c.
> **Fourteenth review (completeness):** 2026-04-05 — 13 completeness gaps addressed. Added: (1) OpenAPI spec as source of truth requirement (§2, new subsection with error response format and status code catalog). (2) RBAC 403→424 consistency fix (REQ-0.1, Phase 0 principles aligned with Koku's 424 pattern per OQ#1 resolution). (3) Kafka offset semantics on failure (REQ-10.3, 5-row error handling table matching Koku's `listen_for_messages` pattern: manual commit, seek-back for infra errors, commit for bad data, no DLQ). (4) PG-down buffering bound resolved (NFR-3: pause Kafka consumer instead of in-memory buffer — Kafka retains messages on broker, no OOM risk). (5) Org deletion / GDPR story (NFR-9a: Sources destroy events trigger deletion, stale org cleanup matching Koku's `remove_stale_tenants`). (6) Kubernetes probes specified (NFR-4: `/healthz` liveness, `/readyz` readiness, startup probe with 60s budget). (7) Circuit breakers for external APIs (NFR-2a: RBAC, Koku, AWS/Azure/GCP catalogs with timeouts and fallbacks). (8) Container resource requests/limits (REQ-10.5: 100m/1000m CPU, 256Mi/512Mi memory, GOMEMLIMIT, automaxprocs). (9) GPU env var inconsistency fixed (`ROS_GPU_IDLE_THRESHOLD`→`ROS_GPU_UNDERUTIL_THRESHOLD` in OQ#7). (10) Notification seed list completed (codes 22–24: HPA_ACTIVE, INSTANCE_TYPE_NOT_IN_CATALOG, INSTANCE_TYPE_DEPRECATED). (11) Keyset pagination note added for large orgs (§17, post-MVP `after` cursor parameter). (12) Existing schema baseline reference added (§18 Modified Tables: links to ros-ocp-backend golang-migrate migrations). (13) Operational notes section added (§24: alerting rules/runbooks, distributed tracing, operator delivery ordering — deferred to implementation). NFR-4 expanded with concrete Prometheus metric catalog (14 metrics).
> **Fifteenth review (TimescaleDB removal):** 2026-04-05 — Major architectural shift: eliminated TimescaleDB and `tvondra/tdigest` dependency entirely after confirming AWS RDS (used for SaaS) does not support TimescaleDB extensions. Git tag `v1.0-timescaledb` preserves the prior design. **New architecture:** Go binary performs in-memory aggregation and exact percentile computation (`slices.Sort()` on `[]int64`) from S3 CSVs during ingestion, upserting pre-computed daily digest rows into plain PostgreSQL partitioned tables via `INSERT ... ON CONFLICT DO UPDATE`. No raw metrics stored in PostgreSQL (CSVs remain in S3). Changes across entire document: §2 Platform matrix rewritten for PostgreSQL 13+ with no extensions (AWS RDS, Crunchy PGO, any managed PG). §18 Schema: all hypertables replaced with `PARTITION BY RANGE (bucket_date)` tables (`daily_container_digests`, `daily_vm_digests`, `daily_gpu_digests`, `daily_pvc_digests`, `recommendation_history`, `recommendation_quality`). All continuous aggregates removed. REQ-2.1 rewritten as Go-side daily digest pipeline. REQ-2.2 multi-tenancy via composite PK `org_id` (no `compress_segmentby`). REQ-2.6 retention via `DROP TABLE` partition. REQ-2.7 create partitioned tables (not CAs). REQ-3.1/3.2/3.3 rewritten: PL/pgSQL functions read pre-computed percentile columns from digest tables using CASE statements. Feature table: F1→"Daily digest metrics pipeline", F4→"Go-side exact percentiles", F6→"Daily digest tables". NFR-5 env vars: `ROS_TDIGEST_DELTA` and `ROS_USE_TDIGEST` struck through. NFR-7 Backup simplified (standard PG). NFR-8 rewritten as `ALTER TABLE` migration (no CA drop+create). Risk table: R2/R11/R12/R20 resolved. OQ#3/4 resolved. Traceability matrix: REQ names updated. Storage estimate: ~3 GB for 50K containers × 91d (down from ~6 GB with raw hypertables). Deployment model: existing plain PostgreSQL instances sufficient.
> **Sixteenth review (BIGINT-everywhere):** 2026-03-26 — Type simplification: unified all numeric metric columns to `BIGINT` in PostgreSQL, matching `int64` in Go. Previously, CPU columns used `INT` (4 bytes) and memory columns used `BIGINT` (8 bytes). The mixed-type schema saved ~10% storage but introduced complexity: developers had to decide which type per column, the Go `database/sql` driver silently narrowed `int64` → `INT` (masking potential overflow), and two mental models were needed. **Decision:** accept ~10% storage overhead (~46 GB at 8M containers/91d) for uniform types end-to-end — zero type decisions, zero narrowing, zero overflow risk. The column name suffix (`_mc`, `_kib`, `_pct`, `_count`) conveys the *unit*, not the storage type. Changes: REQ-2.3 rewritten with BIGINT-everywhere design decision callout. F2 updated. §2 architecture note added. §18 Schema: all `INT` metric columns → `BIGINT` across `daily_container_digests`, `daily_vm_digests`, `daily_gpu_digests`, `daily_pvc_digests`, `daily_hpa_digests`, `daily_node_digests`, `recommendation_quality`, `recommendation_sets` ALTER TABLE, `vm_recommendations`, `node_recommendations`, `machineset_recommendations`, `cloud_instance_catalog`. PL/pgSQL function signatures and `::INTEGER` casts → `::BIGINT`. CSV column type annotations in REQ-8b.2/8c.2 updated. `SMALLINT[]` retained for `notification_codes`; `REAL` retained for percentages/ratios.
> **Twenty-first review (Go recommendation engine):** 2026-03-26 — Major architectural shift: replaced PL/pgSQL recommendation functions with **Go-side "read once, compute N terms" pattern**. Rationale: customer-defined recommendation periods (each customer chooses their own 3 term windows from 1–90 days, e.g., one customer uses 3d/20d/60d while another uses 10d/30d/90d) cannot be efficiently served by PL/pgSQL functions that scan daily digest rows N times (once per term). The Go binary reads the maximum window once per cluster from `daily_container_digests` (single batch query), computes all customer-defined terms from the same in-memory buffer (decay weighting, percentile selection, margin, trend), and writes all recommendation results via `COPY FROM`. This eliminates redundant scans and achieves ~20-30ms per cluster for all terms combined. Technology comparison: Thanos (wrong tool — Prometheus query engine, not batch analytics), TimescaleDB (continuous aggregates don't help because decay weighting requires per-row scanning, t-digest rollup is unweighted, and it's not available on AWS RDS), pre-computed rollup tables (can't pre-compute for arbitrary customer-defined windows). Plain PostgreSQL 16 + Go is sufficient. Changes: F5 rewritten (Go recommendation engine replaces PL/pgSQL), F6/F7/F14 updated for customer-defined terms, Phase 1 header changed from "Hybrid Go + PL/pgSQL" to "Go Recommendation Engine", REQ-1.8 rewritten for customer-defined terms, REQ-3.2 rewritten for Go-side computation, REQ-3.3/3.4 updated, REQ-10.3 pipeline step 6 updated, architecture decision callout updated to v4.0-go-engine.
>
> **Twentieth review (final cleanup):** 2026-03-26 — Fixed 2 critical issues, 12 stale references, 3 medium issues. C1 CRITICAL: REQ-4.2 specified IQR-CV `(p75-p25)/median` requiring p25/p75 columns not in digest schema, but `recommend_cpu()` SQL already used `(p95-p50)/mean` (tail-spread CV) which works from stored columns — aligned REQ-4.2, REQ-1.5, F13 terminology to match implementation; added `p_max_margin` (1.50) to SQL function for upper clamp. C2 CRITICAL: `p999` alias in `recommend_cpu()` claimed "p99.9" but only p99 is stored (p99.9 meaningless on ~96 samples/day) — renamed to `p99` throughout SQL and REQ-4.6. H1-H12: Fixed 12 stale t-digest/continuous-aggregate references in ToC (Phase 3 title), F16, F17, F20, REQ-1.7, REQ-1.12, REQ-8b.2, REQ-8c.3, REQ-8c.5, OQ#13, OQ#15, traceability matrix. M1: R4 risk resolution rewritten from `add_continuous_aggregate_policy()` to `pg_partman`/Go ingestion cycle. M2: OQ#15 "continuous aggregates" → "partitions". M3: Phase 2 compat table "integer CSV" clarified to "float CSV — Go converts at parse time".
> **Nineteenth review (PG version target):** 2026-03-26 — Originally upgraded minimum PostgreSQL version from 13+ to 17+. Subsequently revised to **16+** (v5.0-pg16) because PG 17 is not yet certified by Red Hat on OpenShift (Crunchy PGO, RHEL images). No PG 17-specific SQL feature is required — the design uses only `INSERT ... ON CONFLICT DO UPDATE`, declarative partitioning, `COPY FROM`, and `gen_random_uuid()`, all available since PG 10+. PG 16 is already deployed: Koku SaaS runs PG 16, cost-onprem-chart ships PG 16. SaaS ros-ocp-backend must upgrade from PG 13 (end-of-standard-support) to PG 16. PG 17 benefits (improved `MERGE`, better vacuum on partitioned tables, `COPY WITH (ON_ERROR)`) are nice-to-haves for a future upgrade when Red Hat certifies PG 17.
> **Eighteenth review (pg_partman):** 2026-04-05 — Added `pg_partman` (v5.x) as the preferred partition management extension. `pg_partman` is supported on AWS RDS (since PG 12.5), Crunchy PGO, Azure, GCP Cloud SQL, Aiven, and bare metal — the same platforms already listed in §2. It automates partition pre-creation and retention-based dropping, eliminating custom Go partition management code. Changes: §2 platform note updated from "no extensions required" to "optional `pg_partman`". Architecture diagram, vision bullet, §18 schema header, F1/F4 feature table clarifications, REQ-2.6 retention strategy, NFR-8 partition management section — all updated to prefer `pg_partman` with Go-managed fallback for environments where it's unavailable. NFR-6 GABI compatibility note updated. `pg_partman` setup (`CREATE EXTENSION`, `partman.create_parent()`) managed via `golang-migrate`. Maintenance via `pg_cron` (SaaS/RDS) or Go background goroutine (on-prem).
> **Seventeenth review (risk mitigation):** 2026-04-05 — 8 post-refactor issues fixed (2 critical, 3 high, 3 medium). C1: SQL bug in `recommend_cpu()` trend CTE — `dp.bucket` corrected to `dp.bucket_date::timestamp`. C2 CRITICAL: Late-arriving data percentile merge — pre-computed percentiles cannot be mathematically merged via `ON CONFLICT DO UPDATE`; added explicit strategy: re-download all S3 CSVs for the affected day and recompute from full sample; conservative MAX-based fallback if originals unavailable; new Prometheus counter `ros_ingestion_late_data_fallback_total`. H1: Replaced 3 stale "continuous aggregate" body-text references with "daily digest table" equivalents (REQ-3.4, §18 comment, NFR-3). H2: Replaced stale `ros_continuous_aggregate_refresh_duration_seconds` metric with `ros_ingestion_late_data_fallback_total`. H3: Struck through `ROS_COMPRESSION_POLICY_INTERVAL` (no TimescaleDB chunks), replaced `ROS_RETENTION_POLICY_INTERVAL` with explicit `ROS_DIGEST_RETENTION_DAYS` (45d) and `ROS_REC_HISTORY_RETENTION_DAYS` (90d), fixed shadow mode description (was "exact vs t-digest", now "new vs old binary comparison"). M1: NFR-6 GABI compatibility — removed `INT` from type list (BIGINT-everywhere). M2: All PL/pgSQL function `p_start`/`p_end` parameters changed from `TIMESTAMPTZ` to `DATE` to match `bucket_date DATE` column type (8 function signatures updated). M3: Partition auto-creation strategy made explicit — Go creates current+next month partitions at startup and proactively near month boundaries (PostgreSQL does not auto-create RANGE partitions; `pg_partman` excluded per no-extensions policy).
> **Phase 6 correctness audit:** 2026-04-16 — Two data-path correctness gaps closed during final Phase 6 audit. (1) **Container memory P60/P98/P99 parity** (migration 000035): `ComputeContainerDigest()` always computed P60/P98/P99 for memory request and memory usage, but the pipeline discarded them before INSERT — only P50/P95/Max were stored in `daily_container_digests`. This created an asymmetry with `daily_namespace_digests` (which stored all percentiles since migration 000031). Any future configuration change to `ROS_MEM_PERCENTILE=P98` would silently use `0` for containers. Fix: 6 new `BIGINT` columns added to `daily_container_digests`, `ContainerDigestResult` struct extended, pipeline INSERT/UPDATE and recommendation SELECT/Scan updated. (2) **Namespace memory trend slope notification**: `RecommendMemory()` computes `TrendSlope` for all workload types, but `EvaluateNamespaceNotifications()` was discarding it. Idle detection is correctly excluded (10mc threshold is meaningless at namespace scale; truly idle namespaces produce no digests). However, memory trend slope *is* meaningful: a namespace aggregate growing > 500 KiB/day signals a capacity concern. Fix: added `MemTrendSlope float64` to `NamespaceRec`, populated from `memRec.TrendSlope`, and `EvaluateNamespaceNotifications()` now emits `NotifMemoryTrendingUp` when threshold exceeded. Namespace threshold is 5× the container threshold (100 KiB/day → 500 KiB/day) because aggregate metrics naturally have larger absolute swings. CPU trend slope remains computed but unused (no `NotifCPUTrendingUp` code exists for either containers or namespaces).
> **Upstream bug fixes and infra (2026-04-17):** Merged PRs: RHINENG-20862 (percentage sorting columns for recommendations), Konflux reference updates, Kruize 0.8.3 release, RHINENG-26044 (cluster alias sanitization fix), RHINENG-25560 (Go 1.25 upgrade + dependency updates), 12 critical robustness bug fixes.
> **Phases 0–3 implementation (2026-04-25 – 2026-04-28):** Built the native Go recommendation engine from scratch. Phase 0: robustness fixes to existing codebase. Phase 1: daily digest schema, test infrastructure (testcontainers + golang-migrate), CSV parsing, digest computation (M1–M4); native Go recommendation engine with relational API (M5–M8); CSV export, current values, stale detection, HTTP integration tests; Kruize vs Native comparison tool. Phase 2: API fallback handlers, container_id migration, scale benchmarks. Phase 3: notification_codes persistence and mapping, native engine fixes, date-dependent test fixes. Phase 4 plan drafted.
> **Phase 4 — OOM feedback and quality (2026-04-28 – 2026-04-29):** OOM bump for memory recommendations. Recommendation quality writer with 4 metrics (stability, adoption, OOM rate, rec age). Safety clamps, tuple filter, WorkloadType in quality keys. Auto-create digest partitions. E2E test for OOM pipeline. Legacy GORM query compatibility with native engine rows. Always return notification_codes/notifications in API. IQE verification with nise scope fixes.
> **Phase 5 — History, boxplots, retention (2026-04-29 – 2026-04-30):** Recommendation history tracking (partitioned tables). Query-time boxplots from raw `container_usage_samples`. Separate retention policies for history/quality tables. Native detail response aligned to Kruize-compatible UI shape. Container image updated to ubi10/go-toolset:1.25.
> **Phase 6 — Namespace recommendations (2026-04-30 – 2026-05-01):** Native namespace recommendations with boxplots and OpenAPI update. Critical audit: fix write bugs, add integration tests. Namespace P60/P98/P99 percentiles. Container memory P60/P98/P99 parity (migration 000035). Namespace memory trend slope notification. Legacy Kruize fallback removed. List API response aligned with Kruize format. Limit variation, integer percentages, whole MiB rounding. Migration renumbering (000024–000037).
> **Custom timeframes and savings (2026-05-01):** Custom timeframe settings API (`GET/PUT/DELETE .../settings/terms`) with edge case tests. Replica count (`pod_count_min`/`max`/`avg`) added to recommendation pipeline. Estimated monthly savings (CPU + memory cost model rates + infrastructure + markup + distributed costs). Integration tests for savings. `gpu_distributed` effective_rates SQL gap fixed in Koku.
> **Historical tracking and quality API (2026-05-01):** `GET .../history` and `GET .../quality` endpoints with pagination, filtering, and CSV export. `cluster_uuid` columns migrated from TEXT to UUID. Separate retention policy for history and quality tables.
> **GPU recommendations (2026-05-01 – 2026-05-03):** Full GPU recommendation engine: DCGM profiling metric classification (idle/underutilized/memory-bound/compute-bound/well-utilized/no-profiling). MIG profile selection (A100/A30/H100/H200/B100/B200). Two-tier GPU support (Turing+ full profiling, Volta/Pascal frame-buffer only). GPU ingestion pipeline (`gpu_container_digests` table) wired end-to-end. GPU savings estimation from Koku cost data (`effective_rates` endpoint). API filters (`has_gpu`, `gpu_model`, `gpu_classification`). OpenAPI documentation for GPU query parameters.
> **GPU time-slicing (2026-05-03 – 2026-05-04):** Node-level time-slicing recommendations (`GET /recommendations/openshift/gpu/timeslicing`; historically `/recommendations/openshift/nodes`) with pagination and RBAC. Lightweight **`GET /recommendations/openshift/gpu`** summary (counts + links to timeslicing/MIG listings). Container-level time-slicing cross-reference (`time_slicing_node`, `time_slicing_replicas`). Per-container time-slicing dollar savings (`estimated_monthly_timeslicing_savings_usd`). `gpu_container_digests` added to retention sweep. `gpu_model_name` added to unique constraint. Explicit `no_profiling` GPU classification. GPU audit fixes (migration test, OpenAPI enum, rate dedup).
> **Staleness, idle, adoption, fleet summary (2026-05-04 – 2026-05-05):** Stale data detection (`?stale=` API filter, configurable threshold, stale cleanup sweep (delete after N days), `NotifStaleData`). Idle/abandoned workload detection (CPU+memory idle < 10mc/10MiB, zero-usage abandoned, 100% savings estimate). Adoption detection (compares current requests to prior rec with 5% tolerance). Fleet summary endpoint (`GET .../fleet-summary`). Feature docs consolidated into repository.
> **PVC right-sizing (2026-05-05 – 2026-05-06):** PVC right-sizing recommendations (F27). Classifications: oversized (< 20% usage), near-full (> 85%), orphaned (zero usage for `min_trend_days`, default 2), healthy. Growth trend projection via linear regression. `GET /recommendations/openshift/pvcs` API endpoint. Notification codes 20 (orphaned), 29 (oversized), 30 (near-full).
> **Snapshot staleness detection (2026-05-06 – 2026-05-09):** Snapshot staleness design doc and file routing architecture. Classification rules (orphaned, stale, never-restored, redundant, managed, active). Unified settings API with env-var locking. Snapshot recommendation removal policy. End-to-end implementation: ingestion, classification, API (`GET .../snapshots`, `GET|PUT .../settings/snapshot`). Retention sweep for snapshot inventory. Notification codes 31–35. Critical filename substring convention documented.
> **Node right-sizing and feature enablement (2026-05-10):** (1) **Node/namespace recs enabled by default**: Removed `ROS_ENABLE_NODE_RECS` feature gate. Namespace recs unconditional on-prem; cloud gated by Unleash kill switch `rosocp.namespace_disabled`. (2) **Tier 2 operator capacity data**: koku-metrics-operator emits `node_capacity_cpu_cores`/`node_capacity_memory_bytes` in ROS CSVs; parser and digest pipeline wired; `resolveAllocatable()` prefers capacity over request-based fallback. (3) **EMA trend smoothing**: configurable `ROS_NODE_EMA_ALPHA` (default 0.3) applied to daily CPU utilization before linear regression. (4) **EMA imbalance stranded detection**: Replaced two fixed thresholds with single `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` (default 0.6) using EMA-smoothed `|cpu_p95 - mem_p95| / max(cpu_p95, mem_p95)`. (5) **Nise**: ROS CSV generator updated for node capacity columns. (6) **3 correctness bugs fixed** in recommendation engine. (7) **Documentation**: known-issues updated, keyset pagination documented as future improvement.
> **Exact replica count collection (2026-05-10 – 2026-05-11):** (1) **Operator PromQL queries**: Added `ros:desired_replicas` and `ros:available_replicas` to koku-metrics-operator. Queries join `kube_deployment_spec_replicas`/`kube_statefulset_replicas`/`kube_daemonset_status_desired_number_scheduled` (and available equivalents) with per-pod `kube_pod_container_info` via `namespace_workload_pod:kube_pod_owner:relabel` recording rule. (2) **Operator CSV columns**: `desired_replicas`, `available_replicas` added to `rosContainerRow` header and data. (3) **Backend ingestion**: CSV parser, digest computation (`computeReplicaCounts`), pipeline INSERT/UPDATE for `daily_container_digests`. Migration 000055 adds columns to `daily_container_digests` and `recommendation_sets`. (4) **Engine**: `latestReplicaCounts()` selects most recent non-zero values. `replicaCountForSavings()` prefers `DesiredReplicas` over `PodCountAvg` for savings multiplication. (5) **API**: `ReplicaInfo` extended with `Desired`, `Available`, `Source` (`"kube_state_metrics"` or `"derived"`). (6) **Nise**: `desired_replicas`/`available_replicas` columns in ROS CSV generator. (7) **Critical PromQL bug found and fixed during live validation**: Original queries used `(replica_counts) * on(namespace, workload) group_left(container, pod) (pod_info)` which fails with multi-replica workloads ("many-to-many matching not allowed" error). Fixed by swapping operands: `(pod_info) * on(namespace, workload) group_left() (replica_counts)`. Validated against real Prometheus/Thanos on SNO cluster with 27 results matching `oc get deployment` output. (8) **11 new tests** across all three repos covering CSV parsing, savings preference, API source field, pipeline integration, and migration roundtrip.
> **Status:** Draft — Pending Review
> **Parent document:** [ROS OCP Architecture & Performance Analysis](./performance-analysis.md)
> **Scope:** Complete redesign of the ROS OCP remote recommendation pipeline — drop Kruize from the remote path, implement native Go recommendation engine, adopt daily digest tables on plain PostgreSQL for metrics storage, relational columns instead of JSONB, and all identified optimizations, fixes, and new recommendation types.

---

## Table of Contents

- [1. Vision and Goals](#1-vision-and-goals)
- [2. Architecture Overview](#2-architecture-overview)
- [3. Phasing Strategy](#3-phasing-strategy)
- [4. Phase 0: Critical Fixes (Weeks 1–2)](#4-phase-0-critical-fixes-weeks-12)
- [5. Phase 1: Core Recommendation Engine — Go "Read Once, Compute N Terms" (Weeks 3–8)](#5-phase-1-core-recommendation-engine--go-read-once-compute-n-terms-weeks-38)
- [6. Phase 2: Metrics Pipeline — Daily Digests + Integer Types (Weeks 3–10)](#6-phase-2-metrics-pipeline--daily-digests--integer-types-weeks-310)
- [7. Phase 3: Decay Weighting and Custom Timeframes (Weeks 5–12)](#7-phase-3-decay-weighting-and-custom-timeframes-weeks-512)
- [8. Phase 4: Memory Algorithm with OOM Feedback (Weeks 8–14)](#8-phase-4-memory-algorithm-with-oom-feedback-weeks-814)
- [9. Phase 5: GPU Recommendations (Weeks 10–14)](#9-phase-5-gpu-recommendations-weeks-1014)
- [10. Phase 6: New Recommendation Types — Tier 1 (Weeks 8–16)](#10-phase-6-new-recommendation-types--tier-1-weeks-816)
- [11. Phase 7: Replica Count and Total Impact (Weeks 10–14)](#11-phase-7-replica-count-and-total-impact-weeks-1014)
- [12. Phase 8: New Recommendation Types — Tier 2 (Weeks 14–20)](#12-phase-8-new-recommendation-types--tier-2-weeks-1420)
- [12b. Phase 8b: VM Recommendations (Weeks 12–18)](#12b-phase-8b-vm-recommendations-weeks-1218)
- [12c. Phase 8c: Node & MachineSet Recommendations (Weeks 14–20)](#12c-phase-8c-node--machineset-recommendations-weeks-1420)
- [13. Phase 9: JVM/Quarkus Runtime Recommendations (Weeks 16–20)](#13-phase-9-jvmquarkus-runtime-recommendations-weeks-1620)
- [14. Phase 10: Remove Kruize Dependency (Weeks 18–22)](#14-phase-10-remove-kruize-dependency-weeks-1822)
- [15. Performance Targets](#15-performance-targets)
- [16. Operator Changes (Cross-Phase)](#16-operator-changes-cross-phase)
- [17. API Contract Changes](#17-api-contract-changes)
- [18. Database Schema Changes](#18-database-schema-changes)
- [19. Non-Functional Requirements](#19-non-functional-requirements)
- [20. Backward Compatibility and Migration](#20-backward-compatibility-and-migration)
- [21. Repository Map and Subteam Responsibilities](#21-repository-map-and-subteam-responsibilities)
- [22. Testing Strategy](#22-testing-strategy)
- [23. Open Questions (All Resolved)](#23-open-questions-all-resolved)
- [23b. Risk Resolutions](#23b-risk-resolutions)
- [24. Appendix: Requirement Traceability Matrix](#24-appendix-requirement-traceability-matrix)
- [25. Operational Notes (Deferred to Implementation)](#25-operational-notes-deferred-to-implementation)
- [Feature Inventory](#feature-inventory)

---

## Feature Inventory

This table provides a **feature-level** view of the entire project. Each row is a user-visible feature or a critical infrastructure capability. The "REQs" column cross-references every requirement that contributes to the feature. The "Clarifications" column captures key decisions, deferrals, and caveats resolved during review.

### Legend

- **Status**: Active / Deferred / Removed
- **Phase**: The primary phase where the feature ships (may span multiple)
- **REQs**: All requirement IDs that implement the feature
- **Operator?**: Whether the koku-metrics-operator needs changes for this feature
- **vs Legacy**: Comparison with the current ros-ocp-backend + Kruize (`mvp_demo`) pipeline:
  - **Net-new** — Feature does not exist in the legacy pipeline
  - **Enhanced** — Feature exists in legacy but is significantly improved (improvement noted)
  - **Same** — Functionally equivalent to legacy
  - **Bug fix** — Fixes an existing defect in legacy code

### Infrastructure & Pipeline Features

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F1 | **Daily digest metrics pipeline** | Replace `workload_metrics` JSONB with `daily_container_digests` partitioned table. Go computes exact percentiles in memory during CSV ingestion; only pre-aggregated daily digests stored in PostgreSQL. Raw CSVs remain in S3. | 2 | REQ-2.1, REQ-2.2, REQ-2.4, REQ-2.6, REQ-2.7 | No | Active | **YES** | **Net-new** — Legacy uses `workload_metrics` JSONB table (unbounded growth, ~5.7 TB at 50K containers/91d). | PostgreSQL 16+ with optional `pg_partman` for partition lifecycle. Works on AWS RDS, on-prem StatefulSet, any managed PG. |
| F2 | **Integer types pipeline** | `int64` in Go, `BIGINT` in PostgreSQL — one type end-to-end for all numeric metrics. CPU in millicores, memory in KiB, counts as integers. ros-ocp-backend converts float CSV values (cores, bytes) to `int64` at parse time. Eliminates float precision loss and type-choice complexity. ~10% storage overhead vs mixed INT/BIGINT, accepted for simplicity. No operator change required. | 2 | REQ-2.3 | No | Active | **YES** | **Net-new** — Legacy pipeline uses `float64` throughout (CSV, Go structs, Kruize Java `Double`, PostgreSQL `DOUBLE PRECISION`). | CSV format unchanged — operator continues writing Prometheus floats verbatim. Conversion happens in ros-ocp-backend's CSV parser. |
| F3 | **Relational recommendation columns** | Replace `recommendations` JSONB with typed SQL columns in `recommendation_sets`. Zero `json.Unmarshal`, direct GORM scan. | 2 | REQ-2.5 | No | Active | **YES** | **Net-new** — Legacy stores recommendations as opaque `datatypes.JSON` column requiring marshal/unmarshal on every read/write. | Dual-write during migration, then drop JSONB. PK becomes `(org_id, cluster_uuid, namespace, workload, container_name, term, engine)`. |
| F4 | **Go-side exact percentiles** | Exact percentiles computed in Go via `slices.Sort()` on ~96 integer values per container per day during CSV ingestion. Pre-computed percentiles (p50, p60, p95, p98, p99) stored in `daily_container_digests`. Go recommendation engine reads pre-computed values directly — no runtime percentile computation in SQL. | 2, 3 | REQ-3.1, REQ-3.2, REQ-3.5 | No | Active | **YES** | **Net-new** — Legacy Kruize uses `Collections.sort()` on full `ArrayList<Double>` (O(n log n), up to 8,736 elements per 15-day term) for exact percentiles. | No statistical extensions needed. Exact (not approximate) percentiles. Shadow-mode validation (REQ-1.12). |
| F5 | **Go recommendation engine** | All recommendation math (CPU, memory, idle, PVC, namespace, nodes, GPU, JVM, HPA, MachineSet) runs in Go. The "read once, compute N terms" pattern reads the maximum window of daily digests once per cluster (single batch query), computes all customer-defined terms from the same in-memory buffer (decay weighting, percentile selection, margin, trend), and batch-writes all results via `COPY FROM`. | 1, 3 | REQ-1.10, REQ-1.11, REQ-3.2 | No | Active | **YES** | **Net-new** — Legacy requires 4N HTTP round-trips per hour (N containers) to Kruize for `updateResults` + `updateRecommendations`. | Replaces the earlier hybrid PL/pgSQL + Go design. All logic in Go for customer-defined terms efficiency. |
| F6 | **Customer-defined recommendation periods** | Each customer configures their own 3 term windows (1–90 days each), replacing the fixed short/medium/long terms. The Go engine reads the maximum window once and computes all terms from the same in-memory buffer. | 3 | REQ-3.3, REQ-1.8 | No | Active | **YES** | **Net-new** — Legacy Kruize only supports fixed `short_term` (1d) / `medium_term` (7d) / `long_term` (15d) windows. ros-ocp-backend API has `start_date`/`end_date` filters but only on `monitoring_end_time`, not on recommendation computation window. | Defaults: 1d/7d/15d (hardcoded in Go, zero DB cost). Overrides stored in `org_recommendation_terms` table. Percentile models in `recommendation_profiles`. Business hours schedule weighting is implemented separately — see [Business Hours](../plugin-reference/business-hours.md). |
| F7 | **On-demand real-time recommendations** | API-time recommendation via Go computation (~1-5ms) for custom timeframe requests, supplementing batch pre-computation. Go reads daily digests for the requested window and computes recommendations in memory. | 3 | REQ-3.4 | No | Active | **NO** | **Net-new** — Legacy is batch-only (Kruize computes recs on `/updateRecommendations` call, results cached until next poll). | Gated behind `ROS_ENABLE_REALTIME_RECS`. |
| F8 | **Two-binary deployment** | New `ros-ocp-backend-superpowers` is a separate Go module. Old binary untouched (zero regression). External routing via Kafka consumer groups (SaaS) or Helm chart (on-prem). | All | §2, §20 | No | Active | **NO** | **Net-new** — Legacy is a single binary (`ros-ocp-backend`) coupled to Kruize. | On-prem ships only the new binary. SaaS runs both during transition. Operator is unaffected — it just uploads to the ingress endpoint regardless of which binary consumes the data. |
| ~~F9~~ | ~~**On-prem ingestion (no Kafka)**~~ | ~~HTTP `/ingest` endpoint + directory watch. mTLS auth via OpenShift service-serving certs, `ROS_INGEST_TOKEN` fallback. PostgreSQL advisory locks for concurrency.~~ | ~~10~~ | ~~REQ-10.3, NFR-1~~ | ~~No~~ | **Removed** | N/A | ~~**Net-new** — Legacy is SaaS-only.~~ | **Removed:** On-prem deploys AMQ Streams (Kafka) via the cost-onprem chart. The on-prem ingestion path is identical to SaaS (Kafka + S3). No separate `/ingest` endpoint needed. |
| F10 | **Remove Kruize dependency** | Eliminate all Kruize HTTP calls, Kruize PostgreSQL, performance profiles, internal Kafka topic, deployment manifests. | 10 | REQ-10.1, REQ-10.2, REQ-10.3, REQ-10.4, REQ-10.5 | No | Active | **NO** | **Net-new** — Architectural simplification; legacy requires 4 infrastructure services (ros-ocp + Kruize + 2× PostgreSQL). | Kruize remains for `local_monitoring` and HPO — only removed from remote ROS path. |

### Container Recommendation Features

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F11 | **CPU recommendation (fixed algorithm)** | Single consistent algorithm for all CPU levels (no 1-core discontinuity). `cpu_effective = max(usage_max, usage_avg + throttle_avg)`. No per-pod estimation. | 1 | REQ-1.1, REQ-1.2 | No | Active | **YES** | **Enhanced** — Kruize has a piecewise algorithm that switches behavior at 1 CPU core (`CPU_ONE_CORE`): below 1 core it uses `max(cpuMaxValues)`, above it uses `percentile()`. Also estimates `numPods` from `cpuUsageSum/cpuUsageAvg` causing incorrect per-pod splitting. Both bugs removed. | Removes two critical Kruize bugs. |
| F12 | **Cost and Performance models** | Dual-model output per container: cost (p60 CPU / p95 mem) and performance (p98 CPU / p100 mem), both with configurable safety margin. Single-pass computation. | 1 | REQ-1.3, REQ-1.6, REQ-1.7 | No | Active | **YES** | **Enhanced** — Kruize has cost (p60 CPU / p100 mem) and performance (p98 CPU / p100 mem) models, but percentiles are hardcoded in `PercentileConstants`. New system: percentiles are configurable profile parameters, memory cost model uses p95 (not p100), and adds configurable safety margin. | Customer-defined percentiles supported via `recommendation_profiles` (and future profile UI). |
| F13 | **Memory recommendation (basic)** | `max()` instead of p100 sort, adaptive tail-spread CV margin (15-50%), separate request/limit. | 1 | REQ-1.5, REQ-1.6 | No | Active | **YES** | **Enhanced** — Kruize memory uses `usage_percentile + 20% buffer` with `min(percentile, spike+5%)` heuristic, `limit = request` (same value). New system: `max()` aggregation (O(1) vs O(n log n) sort for p100), data-driven adaptive margin via `(p95 - p50) / mean` (15-50% vs fixed 20%), and separate request/limit recommendations. | — |
| F14 | **Customer-defined term support** | Three recommendation terms per customer, each configurable from 1–90 days. Defaults: 1d/7d/15d (hardcoded in Go). Exponential decay weighting per term. Overrides in `org_recommendation_terms` table. | 1 | REQ-1.8 | No | Active | **YES** | **Enhanced** — Kruize has fixed `short_term` (1d) / `medium_term` (7d) / `long_term` (15d) terms. New system: customer-defined windows (any 1–90 day range), configurable decay half-lives, minimum data thresholds scale with window size. | Decay half-lives default: short=none, medium=72h, long=168h. Customers override via `org_recommendation_terms` (future API). |
| F15 | **Notification system** | Structured notification codes (35 defined) with severity levels. Reference table in DB and API endpoint. | 1 | REQ-1.9, REQ-1.14 | No | Active | **YES** | **Enhanced** — Kruize defines ~30 notification codes internally, but ros-ocp-backend filters to only 4 notice codes (`323004`, `323005`, `324003`, `324004`) plus uses `111000` internally. New system: 35 curated codes in DB reference table, all exposed to API via `/notification-codes` endpoint, `UPPER_SNAKE_CASE` naming. | `UPPER_SNAKE_CASE` naming convention. Codes exposed via `/notification-codes` endpoint. |
| F16 | **Namespace recommendations** | Namespace-level CPU/memory recommendations using `daily_namespace_digests` aggregate. Same dual-model, decay, percentile parameterization as containers. | 1 | REQ-1.13 | No | Active | **YES** | **Enhanced** — Legacy has namespace recommendations (gated behind `rosocp.namespace_enabled` Unleash flag), using same Kruize pipeline with separate experiments. New system: relational columns, daily digest aggregates, same algorithmic improvements as containers (no 1-core bug, adaptive margin, decay). | Stored in `namespace_recommendation_sets` with relational columns. |
| F17 | **Shadow-mode validation** | Parallel new Go superpowers engine + legacy Kruize pipeline; reconciliation logs divergences beyond tolerance (1mc / 1 KiB). | 1 | REQ-1.12 | No | Active | **NO** | **Net-new** — No validation mechanism exists in legacy pipeline. | Gated behind `ROS_ENABLE_SHADOW_MODE`. Disable after ≥1 week validation. |
| F18 | **OOM-aware memory recommendations** | OOM event collection, exponential backoff (1.3×/1.6×/2.0×), `oom_floor` in limit calculation. | 4 | REQ-4.1, REQ-4.2, REQ-4.3, REQ-4.6 | Yes (1 new query) | Active | **YES** | **Net-new** — Kruize does not use OOM kill events. Memory recommendation is purely usage-based (percentile + buffer). | Gated behind `ROS_USE_OOM_FEEDBACK`. |
| F19 | **Memory trend detection** | Linear regression on daily mean memory; projects 7 days forward. `WARNING_MEMORY_TRENDING_UP` notification if leak detected. | 4 | REQ-4.4 | No | Active | **YES** | **Net-new** — No trend detection in legacy pipeline. | Triggers notification only (not automatic limit increase). |
| F20 | **Confidence bounds** | Separate lower/upper bounds on recommendations. | — | ~~REQ-1.4~~ | — | **Deferred** | **NO** | **Same (deferred)** — Kruize has `confidence_level` field in schema but it is always `0.0` (never computed). Both systems defer this. | Cost/performance dual-model already provides actionable range. **Future work:** the daily digest schema stores multiple percentiles (p50, p60, p95, p98, p99, max) which could inform confidence bounds, but the statistical methodology for container workload distributions needs design. Revisit when customer demand emerges. |

### GPU Recommendation Features

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F21 | **GPU MIG bin-packing** | Port Kruize algorithm to Go. Smallest MIG profile whose resources ≥ p95 usage. | 5 | REQ-5.1 | No | Active | **YES** | **Enhanced** — Kruize has accelerator recommendations using usage percentiles, MIG detection via Instaslice UUID/profile, and framebuffer-based partitioning. However, Kruize has known bugs: gating logic only recognizes A100/H100/H200, and framebuffer gap logic is incomplete (TODO comments). New system: ported to Go with bug fixes, proper bin-packing against profile lookup table. | Gated behind `ROS_ENABLE_GPU_RECS`. |
| F22 | **B200/RTX PRO GPU support** | Fix gating bug (only A100/H100/H200 recognized). Add B200, RTX PRO 5000/6000 to lookup table. | 5 | REQ-5.2, REQ-5.3 | No | Active | **YES** | **Enhanced** — Kruize `AcceleratorDecisionHandler` has an allowlist gating bug: GPUs not in the hardcoded list (`A100`, `H100`, `H200`) are silently skipped with `328001 accelerator not supported`. New system: data-driven lookup table. | Data-driven lookup table for extensibility. |
| F23 | **GPU workload classification** | Multi-metric DCGM classification (idle, underutilized, memory-bound, compute-bound-underutil, well-utilized, no-profiling) with MIG/time-slicing/deallocation actions and savings. | 5 | REQ-5.4 | No | Active | **YES** | **Net-new** — Kruize does not classify GPU workloads by DCGM profiling metrics. | Thresholds via `ROS_GPU_*` env vars; see [gpu-classification.md](gpu-classification.md). |
| F24 | **Cost-aware GPU recs** | Integrate Koku MIG cost data for current vs recommended GPU profile dollar savings. | 5 | REQ-5.6 | No | Active | **YES** | **Net-new** — Kruize provides no dollar cost integration for GPU recommendations. | Depends on Koku GPU cost API stability. |
| F25 | **Multi-GPU awareness** | Detect containers with multiple GPUs (4-GPU at 25% each). | 5 | REQ-5.5 | Yes | **Deferred** (Low) | **NO** | **Net-new** — Kruize assumes single accelerator per container (comments note this limitation). Both systems defer full multi-GPU support. | Current algorithm assumes 1 GPU/container. **Future work:** requires per-device utilization metrics from operator (`DCGM_FI_DEV_GPU_UTIL` per device UUID), new CSV columns, and bin-packing algorithm across device count × MIG profile. Primarily benefits ML training workloads. Revisit when multi-GPU adoption grows. |
| F25a | **GPU time-slicing recommendations** | Node-level NVIDIA `nvidia.com/gpu.replicas` consolidation guidance. Container-level cross-reference with per-container savings. | 5 | REQ-5.7 | No | Active | **YES** | **Net-new** — No time-slicing analysis in legacy pipeline. | Summary: **`GET /recommendations/openshift/gpu`**. Time-slicing list: **`GET /recommendations/openshift/gpu/timeslicing`**. MIG-focused list: **`GET /recommendations/openshift/gpu/mig`**. |

### New Recommendation Types — Tier 1

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F26 | **Idle/abandoned workload detection** | CPU + memory utilization <1% → idle. Zero usage → abandoned. Estimated savings. | 6 | REQ-6.1 | No | Active | **YES** | **Enhanced** — Kruize has basic CPU idle detection: if derived CPU request ≤1 millicore, emits `NOTICE_CPU_RECORDS_ARE_IDLE` and nullifies CPU recommendation. Memory "idle" is only zero-usage (`NOTICE_MEMORY_RECORDS_ARE_ZERO`). New system: combined CPU+memory threshold, separate idle vs abandoned classification, estimated resource savings, configurable thresholds. | Gated behind `ROS_ENABLE_IDLE_DETECTION` (ON by default). Configurable threshold. |
| F27 | **PVC right-sizing** | Compare PVC usage vs capacity. Flag oversized (>80% unused), near-full (>85%), orphaned (0 usage). Growth trend projection. | 6 | REQ-6.3 | No | Active | **YES** | **Net-new** — No PVC recommendations in legacy pipeline. | No new Prometheus queries, no operator change. Operator already scrapes `cost:persistentvolumeclaim_*` and writes `cm-openshift-storage-usage-YYYYMM.csv`. ros-ocp-backend reads this existing CSV. On-prem: reads from ingested tarball. SaaS: Koku `kafka_msg_handler.py` routing updated to also send storage CSV to ROS consumer. |
| F28 | **Go GOMAXPROCS/GOMEMLIMIT** | Detect Go workloads via `go_info`. Recommend `GOMEMLIMIT = 0.9 × mem_limit` and `GOMAXPROCS = ceil(cpu_limit)`. | 6 | REQ-6.4 | Yes (1 query) | Active | **NO** | **Net-new** — No Go runtime recommendations in legacy pipeline. | — |
| F29 | **QoS class recommendations** | Explicit Guaranteed/Burstable/BestEffort recommendation. | — | ~~REQ-6.2~~ | — | **Deferred** | **NO** | **Net-new (deferred)** — Not in legacy. | Implicit from CPU/memory request/limit values. Revisit if user research demands. |
| F29a | **Snapshot staleness detection** | VolumeSnapshot classification: orphaned, stale, never-restored, redundant, managed, active. Configurable settings with env-var locking. | 6 | REQ-6.5 | No | Active | **YES** | **Net-new** — No snapshot analysis in legacy pipeline. | Settings API with env-var locking for operator-controlled deployments. |
| F29b | **Box plots / five-number summary** | Per-term usage distribution visualization (min, Q1, median, Q3, max) from daily digest data. Available for containers and namespaces. | 3 | REQ-6.6 | No | Active | **YES** | **Net-new** — No usage distribution visualization in legacy pipeline. | Embedded in detail API response. |

### Replica Count & Cost Integration

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F30 | **Replica count collection** | Collect `desired_replicas` / `available_replicas` from deployment/statefulset/daemonset metrics. | 7 | REQ-7.1, REQ-7.2, REQ-7.4 | Yes (2-4 queries) | Active | **YES** | Operator queries `kube_deployment_spec_replicas`, `kube_statefulset_replicas`, `kube_daemonset_status_desired_number_scheduled` (and available equivalents). Backend stores per-digest. API exposes `desired`, `available`, `source` in `ReplicaInfo`. Fallback to pod count when operator columns absent. | Fallback: derive from distinct pod count if operator too old. |
| F31 | **Total impact (resource savings × replicas)** | `per_container_savings × desired_replicas` = total savings in millicores/KiB. | 7 | REQ-7.3 | No | Active | **YES** | **Net-new** — Legacy shows `variation` as percentage change vs current (per-container only). No total-impact-across-replicas calculation exists. | — |
| F32 | **Dollar savings via Koku cost models** | Query Koku `/cost-models/` API for CPU/memory rates + markup. Cache hourly. `estimated_savings_cents` per recommendation. | 7 | REQ-7.5 | No | Active | **YES** | **Net-new** — No dollar cost integration in legacy pipeline. | Graceful degradation: `null` if Koku unreachable or `ROS_ENABLE_COST_INTEGRATION=false`. Distributed costs not captured (secondary benefit). |
| F33 | **Fleet-level summary** | Cross-cluster aggregated savings, adoption rates, top opportunities by org_id. | 7 | REQ-7.6 | No | Active | **YES** | **Net-new** — No fleet-level aggregation in legacy pipeline. | Gated behind `ROS_ENABLE_FLEET_SUMMARY`. |

### New Recommendation Types — Tier 2

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F34 | **HPA optimization** | Saturated HPA (at max), idle HPA (at min), flapping (>10 events/hr), combined VPA+HPA advice. | 8 | REQ-8.1 | Yes (8 queries) | **Deferred** | **NO** | **Net-new** — No HPA analysis in legacy pipeline. | Codes 21–22 reserved; see [Deferred: HPA and VPA](#deferred-hpa-and-vpa-autoscaling). |
| F35 | **Ephemeral storage recommendations** | Right-size ephemeral storage requests/limits. | 8 | REQ-8.2 | Yes (4 queries) | Active (informational only) | **NO** | **Net-new** — No ephemeral storage recommendations in legacy pipeline. | OFF by default (`ROS_ENABLE_EPHEMERAL_STORAGE=false`). cadvisor metrics unreliable through OCP 4.21. Pending upstream fix. |
| F36 | **Node.js heap advisory** | Detect Node.js via `nodejs_version_info`. Emit "set `--max-old-space-size` to 75% of mem limit" notification. | 8 | REQ-8.3 | Yes (1 query) | Active (informational only) | **NO** | **Net-new** — No Node.js runtime recommendations in legacy pipeline. | Weakest recommendation type. OFF by default (`ROS_ENABLE_NODEJS_RECS=false`). No actionable numeric value. |
| F37 | **ResourceQuota recommendations** | Aggregate container recs within namespace vs quota hard limits. Flag over-/under-provisioned quotas. | 8 | REQ-8.4 | Yes (2 queries) | Active | **YES** | **`quota` plugin** — `GET .../quota/`; see [quota-recommendations.md](../features/quota-recommendations.md). | — |
| F37b | **ClusterResourceQuota recommendations** | OpenShift-only: right-size team/tenant CRQ hard limits from `openshift_clusterresourcequota_*` metrics and aggregated namespace quota signals. | 8 | REQ-8.4b | Yes (8+ queries, new CSV) | Active | **YES** | **`cluster-quota` plugin** — `GET .../cluster-quota/`; see [cluster-resource-quota.md](../features/cluster-resource-quota.md). | Requires OCP + openshift-state-metrics. Clusters without CRQs: no rows, no errors. |

### VM Recommendations

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F38 | **VM CPU/memory right-sizing** | OpenShift Virtualization VMs: p95 CPU rounded to whole vCPUs, p95 memory rounded to whole GiB. Guest OS-aware baselines (Windows 2 GiB, Linux 0.5 GiB). 40% hysteresis to avoid restart churn. | 8b | REQ-8b.1, REQ-8b.2, REQ-8b.3, REQ-8b.4, REQ-8b.5, REQ-8b.7, REQ-8b.8 | Yes (12 queries) | Active | **NO** | **Net-new** — No VM recommendations in legacy pipeline. | Controlled by `ROS_ENABLED_PLUGINS`/`ROS_DISABLED_PLUGINS` (enabled by default). VMs identified via `kubevirt_vmi_info`. |
| F39 | **VM disk size & IOPS** | Disk size rec (MAX usage + 30d growth + 25% headroom, round to 10 GiB). IOPS/throughput p95 (informational). | 8b | REQ-8b.4 | Yes (included above) | Active | **NO** | **Net-new** | IOPS informational only (Q17). Actionable storage class recs deferred. |
| F40 | **VM idle detection** | `cpu_p95 < 50mc AND mem_p95 < 512 MiB` → idle VM. | 8b | REQ-8b.4 | No | Active | **NO** | **Net-new** | — |
| F41 | **VM API endpoints** | `/virtual-machines` list + `/:id` detail, same filter/pagination as containers. | 8b | REQ-8b.6 | No | Active | **NO** | **Net-new** | — |
| F42 | **VM instance type recommendation** | If `VirtualMachineInstancetype` resources available, recommend smallest-fit. | 8b | REQ-8b.9 | Yes (1 query) | Active (Low) | **NO** | **Net-new** | Deferred within 8b if instance types not yet widely used. |

### Node & MachineSet Recommendations

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F43 | **Node utilization visibility (Tier 1)** | Underutilized (<30% both CPU+mem), overcommitted (>150% request/allocatable), stranded resources (CPU vs memory imbalance). Per-node p50/p95 utilization, trend slope. | 8c | REQ-8c.1, REQ-8c.2, REQ-8c.2b, REQ-8c.3, REQ-8c.8, REQ-8c.9, REQ-8c.11 | Yes (routing existing + 2 new queries) | Active | **YES** | **Net-new** — Legacy pipeline has node data in CSV (node labels, capacity) but only as a dimension for container recs, not as a recommendation target. | Enabled by default. On-prem = capacity planning; cloud = scale-down. |
| F44 | **MachineSet right-sizing (Tier 2)** | Aggregate utilization across MachineSet nodes. Replica count recommendation (`rec = ceil(current × util / target)`). Instance type recommendation from cloud catalog (smallest-fit). Stranded resource → family switch. PDB notification. | 8c | REQ-8c.4, REQ-8c.5, REQ-8c.6, REQ-8c.11 | Yes (3-5 queries) | Active | **NO** | **Net-new** | Go heuristic (not PL/pgSQL). 20% minimum savings hysteresis. 2-replica HA floor. |
| F45 | **MachineAutoscaler optimization (Tier 3)** | Saturated/idle/flapping/missing autoscaler detection. Suggested min/max adjustments. | 8c | REQ-8c.7 | Yes (2 queries, optional) | Active | **NO** | **Net-new** | Cloud-only (bare metal N/A). |
| F46 | **Cloud instance type catalog** | Live catalog from AWS Bulk Pricing JSON, Azure Retail Prices API, GCP machineTypes API. Daily refresh. In-memory cache. | 8c | REQ-8c.6 | No | Active | **NO** | **Net-new** | AWS Tier 1: public JSON (no auth). Tier 2: optional `ec2:DescribeInstanceTypes` if customer adds IAM perm. |
| F47 | **Node/MachineSet API endpoints** | `/nodes`, `/nodes/:node`, `/machinesets`, `/machinesets/:name`. Filter by utilization, instance type, stranded resource. | 8c | REQ-8c.10 | No | Active | **PARTIAL** | **Net-new** — Node utilization exists (**`GET /recommendations/openshift/nodes`**; deprecated alias **`GET /recommendations/openshift/nodes/utilization`**). GPU time-slicing moved to **`GET /recommendations/openshift/gpu/timeslicing`**. MachineSet list endpoint (**`GET /machinesets`**) shipped as Tier 1 aggregation. Detail endpoint (**`GET /machinesets/{name}`**) and catalog-driven engine are Tier 2. |

### JVM/Quarkus Runtime Recommendations

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F48 | **JVM runtime detection** | Detect Hotspot vs Semeru/OpenJ9 from `jvm_info` or image name heuristic. | 9 | REQ-9.1 | Yes (optional) | Active | **NO** | **Enhanced** — Kruize has `HotspotLayerRecommendationHandler` and `SemeruLayerRecommendationHandler` registered in `LayerRecommendationHandlerRegistry`, but detection relies on the performance profile wiring. New system: proactive detection from `jvm_info` metric or container image name heuristic. | — |
| F49 | **MaxRAMPercentage recommendation** | Recommend heap percentage based on actual `jvm_memory_used_bytes` utilization (if available), or heuristic defaults. | 9 | REQ-9.2 | No | Active | **NO** | **Enhanced** — Kruize `HotspotLayerRecommendationHandler` recommends `MaxRAMPercentage` from memory limit, but uses fixed heuristic (no JVM metrics). New system: data-driven from actual `jvm_memory_used_bytes` when available, heuristic fallback otherwise. | — |
| F50 | **GC policy recommendation** | Data-driven GC selection based on pause metrics. Respects JDK version constraints (ZGC ≥15, Shenandoah ≥12). | 9 | REQ-9.3 | No | Active | **NO** | **Enhanced** — Kruize has GC policy recommendation in `HotspotLayerRecommendationHandler`, but selection logic is heuristic (no pause metric analysis). New system: data-driven from actual GC pause duration/frequency metrics with JDK version gating. | — |
| F51 | **Quarkus thread pool** | `core-threads = max(8, 2 × ceil(cores))`, `queue-size = 2 × core-threads`. Fix `THREADS_PER_CORE=1` undersizing. | 9 | REQ-9.4 | No | Active | **NO** | **Enhanced** — Kruize `QuarkusLayerRecommendationHandler` computes `core-threads` from CPU limit, but uses `THREADS_PER_CORE=1` (should be 2). New system fixes the multiplier and adds `queue-size` with floor of 8. | — |
| F52 | **Semeru consistency** | Use `ceil()` for CPU core rounding (not `round()`). | 9 | REQ-9.5 | No | Active | **NO** | **Bug fix** — Kruize `SemeruLayerRecommendationHandler` uses `round()` for CPU core values, which can round down (e.g., 1.1 → 1). New system uses `ceil()` to ensure the container always has sufficient cores. | — |

### Quality, Observability & Lifecycle

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F53 | **Recommendation quality metrics** | OOM rate after recs, recommendation stability (drift between cycles), adoption detection. Prometheus metrics + `/quality` endpoint. | 10 | REQ-10.6 | No | Active | **YES** | **Net-new** — Legacy has Prometheus metrics (Echo middleware) for request counts/latency, but no recommendation quality metrics (OOM rate, stability, adoption). Kruize has Micrometer logging for notification-level metrics tags but nothing application-facing. | Simplified: dropped `accuracy_score` (needs app-level feedback unavailable). |
| F54 | **Recommendation adoption detection** | Compare current resource requests vs prior recommendation. If within 5% tolerance, mark "likely applied". Track adoption rate per cluster/org. | 10 | REQ-10.7 | No | Active | **YES** | **Net-new** — No adoption detection in legacy pipeline. | `recommendation_applied_at` column + `RECOMMENDATION_APPLIED` notification. |
| F55 | **Recommendation staleness detection** | Flag recs with no new data for >48h. Delete stale recommendations after N days (default 30); archiving to `recommendation_history` before deletion is future work. API `?stale=false` filter. | 10 | REQ-10.8 | No | Active | **YES** | **Net-new** — Legacy has `RecommendationPollIntervalHours` for re-polling cadence and `NeedRecommOnFirstOfMonth` logic, but no explicit staleness flag or API filter. | `stale` column + `STALE_DATA` notification. |
| F56 | **Recommendation history** | Time-series of all past recommendations in `recommendation_history` partitioned table. Retained 90d (old partitions dropped). Fleet API `GET /recommendations/openshift/history` (filter by container/cluster/project). | 10 | R5 (risk resolution), §18 | No | Active | **YES** | **Enhanced** — Legacy has `HistoricalRecommendationSet` and `HistoricalNamespaceRecommendationSet` tables (JSONB, partitioned, upsert on conflict). New system: `recommendation_history` partitioned table with 90d retention and fleet history API (legacy has no history API). | Replaces `historical_recommendation_sets` JSONB table. |

### Critical Bug Fixes (Current Codebase)

| # | Feature | Description | Phase | REQs | Operator? | Status | Impl | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|------|-----------|----------------|
| F57 | **RBAC crash fixes** | Fix nil pointer dereference on HTTP error + `strings.Split` OOB on malformed permission. | 0 | REQ-0.1, REQ-0.2 | No | Active | **YES** | **Bug fix** — Crashes exist in current ros-ocp-backend `main` branch. | Ship as hotfix to current binary. |
| F58 | **API 200-on-error fix** | Return 500 on DB failure instead of 200 with empty data. | 0 | REQ-0.3 | No | Active | **YES** | **Bug fix** — Current API returns HTTP 200 with empty data on database errors. | — |
| F59 | **Kafka resilience fixes** | Type assertion panic, subscribe failure, poison message DLQ, SendMessage retry. | 0 | REQ-0.4, REQ-0.5, REQ-0.7, REQ-0.12 | No | Active | **PARTIAL** | **Bug fix** — Multiple Kafka-related crash and resilience issues in current binary. DLQ (REQ-0.7) not yet implemented. | — |
| F60 | **HTTP timeout + misc fixes** | Client timeouts on all HTTP, GORM `.Where()` bug, date parse error handling, deterministic iteration, log reduction. | 0 | REQ-0.6, REQ-0.8, REQ-0.9, REQ-0.10, REQ-0.11 | No | Active | **YES** | **Bug fix** — Multiple code quality and correctness issues in current binary. | — |

### Multi-timescale merge (Removed)

| # | Feature | Description | Phase | REQs | Operator? | Status | vs Legacy | Clarifications |
|---|---------|-------------|-------|------|-----------|--------|-----------|----------------|
| — | ~~Multi-timescale merge~~ | ~~Separate fast/slow timescales for recommendations.~~ | — | ~~REQ-4.5~~ | — | **Removed** | **Net-new (removed)** — Was proposed as a new feature, then removed as redundant. | Redundant with exponential decay (REQ-3.2). |

### Legacy Features Not Carried Forward

The following features exist in the current ros-ocp-backend + Kruize pipeline but are intentionally **not carried forward** to the new system, along with the rationale for each decision.

| Legacy Feature | Where It Exists | Disposition | Rationale |
|---------------|-----------------|-------------|-----------|
| **Box plots (min, q1, median, q3, max)** | Kruize generates `PlotManager` box-plot data (min/q1/median/q3/max) for CPU and memory per term. ros-ocp-backend strips these on list endpoints but preserves them on detail endpoints. | **Preserved** | The `daily_container_digests` table stores pre-computed percentiles (p50, p95, p99, max). Box-plot values are computed in Go at API response time from the stored percentile columns. Detail endpoint includes box plots by default (same as legacy); list endpoint omits them (same as legacy) with `?include_plots=true` opt-in. No additional storage or schema changes needed. |
| **Kruize performance profiles** | Kruize has `/listPerformanceProfiles` and `/createPerformanceProfile` endpoints. ros-ocp-backend bootstraps with `resource_optimization_openshift.json`. Performance profiles configure recommendation tunables (percentiles, thresholds, layers). | **Preserved** (improved) | The concept is preserved and improved. The Go recommendation engine reads all tunables from `recommendation_profiles` (or equivalent config) — e.g. cost/performance CPU and memory percentiles, margins, decay half-life. Multiple "profiles" can be computed by running the same engine with different parameter sets — e.g., a `conservative` profile (p50 CPU / p90 mem) and an `aggressive` profile (p98 CPU / p100 mem). The API supports `?profile=<name>` query parameter that maps to a named set of parameters stored in a lightweight `recommendation_profiles` table. Default profiles (cost, performance) are pre-populated. Custom profiles are a future CRUD addition on the same mechanism — no engine changes needed. |
| **Kruize experiment lifecycle** | `createExperiment` / `deleteExperiment` — Kruize requires explicit experiment creation per workload, with specific measurement duration, threshold, and performance profile bindings. | **Eliminated** | The experiment abstraction was a Kruize-specific requirement for its Java engine. The new system operates on metrics rows directly — no per-workload registration step is needed. `recommendAllWorkloads()` processes all containers in a cluster in a single Go recommendation pass. |
| **Kruize internal PostgreSQL** | Kruize maintains its own PostgreSQL database (`kruize_results`) with JSONB experiment data, separate from the ros-ocp-backend PostgreSQL. | **Eliminated** | Infrastructure simplification: 2× PostgreSQL → 1× PostgreSQL. All data lives in one database. |
| **`/updateResults` HTTP batching** | ros-ocp-backend sends metrics to Kruize via `POST /updateResults` in batches (4N calls/hour for N containers). | **Eliminated** | Replaced by Go in-memory aggregation + `INSERT ... ON CONFLICT DO UPDATE` directly to PostgreSQL. |
| **Variation as percentage** | Legacy API returns `variation` as a percentage difference between current and recommended values per resource (CPU/memory request/limit). | **Preserved** (computed differently) | The new relational columns store both `current_*` and `rec_*` values. Variation percentage is trivially computed at API time: `((rec - current) / current) × 100`. Not a separate stored field, but functionally equivalent in the API response. |
| **CSV/JSON content negotiation** | Legacy ros-ocp-backend supports `Accept: application/json` (default) and `Accept: text/csv` on the container recommendations list endpoint (returns 406 for namespace CSV). | **Preserved** | The new API supports both `Accept: application/json` and `Accept: text/csv` on all recommendation list endpoints (containers, namespaces, VMs, nodes, MachineSets). With relational columns, CSV export is simpler than legacy (direct SQL column → CSV header mapping, no JSON unmarshal → flatten step). Namespace CSV support is a new improvement over legacy (which returned 406). |
| **Unit conversion (cpu-unit, memory-unit, true-units)** | Legacy API supports `?cpu-unit=cores&memory-unit=GiB&true-units=true` query parameters for response value conversion. | **Preserved** | The new API supports the same query parameters. Integer storage (millicores/KiB) makes conversion to any unit a simple division at API time. |
| **RBAC integration** | Legacy ros-ocp-backend has optional RBAC enforcement via `koku.rbac.RbacService`. | **Preserved** | The new binary includes RBAC support with the bug fixes from F57. |

### Legacy Comparison Summary

| Category | Net-new | Enhanced | Same / Preserved | Bug fix | Deferred | Total |
|----------|---------|----------|-------------------|---------|----------|-------|
| Infrastructure & Pipeline (F1-F10) | 10 | 0 | 0 | 0 | 0 | 10 |
| Container Recommendations (F11-F20) | 4 | 5 | 0 | 0 | 1 | 10 |
| GPU Recommendations (F21-F25) | 2 | 2 | 0 | 0 | 1 | 5 |
| Tier 1 New Recs (F26-F29) | 2 | 1 | 0 | 0 | 1 | 4 |
| Replica Count & Cost (F30-F33) | 4 | 0 | 0 | 0 | 0 | 4 |
| Tier 2 New Recs (F34-F37) | 4 | 0 | 0 | 0 | 0 | 4 |
| VM Recommendations (F38-F42) | 5 | 0 | 0 | 0 | 0 | 5 |
| Node & MachineSet (F43-F47) | 5 | 0 | 0 | 0 | 0 | 5 |
| JVM/Quarkus (F48-F52) | 0 | 4 | 0 | 1 | 0 | 5 |
| Quality & Lifecycle (F53-F56) | 3 | 1 | 0 | 0 | 0 | 4 |
| Bug Fixes (F57-F60) | 0 | 0 | 0 | 4 | 0 | 4 |
| **Total** | **39** | **13** | **0** | **5** | **3** | **60** |

**Key takeaway:** Of 60 features, **39 are net-new** (65%), **13 are enhancements** over legacy Kruize functionality (22%), **5 are bug fixes** to the current codebase (8%), and **3 are deferred** (5%). **No legacy feature is lost.** All preserved capabilities (box plots, performance profiles, variation %, CSV/JSON content negotiation, unit conversion, RBAC) are carried forward with equivalent or better implementation.

---

## 1. Vision and Goals

### Problem Statement

The current ROS OCP recommendation pipeline serializes metrics data 11 times across 4 storage locations, bottlenecked by sequential HTTP calls to a Java-based recommendation engine (Kruize autotune) that reads billions of JSONB rows to compute exact percentiles. At 20,000,000 containers with 91-day retention, the current architecture cannot complete a recommendation cycle within a 24-hour window.

### Vision

Replace the current multi-service architecture with a single Go service ("ros-ocp-backend with superpowers") that:

1. **Eliminates Kruize** from the remote monitoring path
2. **Computes recommendations natively** in Go using exact percentile algorithms on integer types — the "read once, compute N terms" pattern reads the maximum window of daily digests once per cluster and computes all customer-defined terms from the same in-memory buffer
3. **Stores only daily digests in PostgreSQL** — Go computes percentiles in memory during CSV ingestion; raw CSVs remain in S3. PostgreSQL 16+ with optional `pg_partman` extension for automated partition lifecycle.
4. **Stores recommendations as relational columns** instead of opaque JSONB blobs — zero serialization/deserialization
5. **Uses integer types** (millicores/KiB) throughout the pipeline
6. **Adds 10 new recommendation types** not available in the current system
7. **Supports customer-defined recommendation periods** — each customer configures their own 3 term windows (1–90 days each), replacing the fixed short/medium/long terms

### Success Metrics

| Metric | Current | Target | Factor |
|---|---|---|---|
| Ingestion throughput | ~8 containers/sec | ~15,000 containers/sec | ~1,900x |
| Recommendation throughput | ~24 containers/sec | ~60,000 containers/sec | ~2,500x |
| Max containers (1-hour SLA) | ~1,000 | ~5,000,000 | ~5,000x |
| Metrics storage (50K containers, 91d) | 5.7 TB (JSONB) | ~3 GB (daily digest tables) | ~1,900x |
| Application RAM | 350–700 MB (2 services) | 50–100 MB (1 service) | ~5x |
| Infrastructure services | 4 (ros-ocp + Kruize + 2× PostgreSQL) | 2 (ros-ocp + PostgreSQL) | 2x fewer |
| API response latency | 5–47 ms | 0.5–2 ms | ~10x |
| Recommendation types | 6 | 16 | 2.7x |

---

## 2. Architecture Overview

### Current Architecture (to be replaced)

```
Operator → CSV → Kafka → ros-ocp-backend → HTTP /updateResults → Kruize → Kruize PG
                                          → HTTP /updateRecommendations → Kruize → Kruize PG
                                          ← HTTP response → ros-ocp PG → REST API → UI
```

Services: ros-ocp-backend (Go), Kruize autotune (Java), 2× PostgreSQL
Metrics storage: ros-ocp `workload_metrics` (JSONB), Kruize `kruize_results` (JSONB)

### New Architecture

```
Operator → CSV → Kafka → ros-ocp-backend (Go)
                           ├── Download CSV from S3
                           ├── Parse CSV → integer types
                           ├── Compute daily digests in memory (slices.Sort → percentiles)
                           ├── Upsert daily_container_digests (INSERT ... ON CONFLICT DO UPDATE)
                           │
                           ├── recommendAllWorkloads() (Go) ────────────────────┐
                           │   (read digests once, compute N terms, batch write) │
                           │                                                     ▼
                           │                                        ┌──── PostgreSQL (plain) ────────┐
                           │                                        │  Storage only: digest rows    │
                           │                                        │  + recommendation_sets        │
                           │                                        │    (COPY / INSERT upsert)       │
                           │                                        └─────────────────────────────────┘
                           │
                           ├── GPU / JVM / HPA recommendations (Go heuristics)
                           ├── Notifications (Go string building)
                           └── REST API → UI
```

Services: ros-ocp-backend (Go), PostgreSQL 16+ (optional `pg_partman` for automated partition management)
Metrics storage: `daily_*_digests` partitioned tables (pre-aggregated by Go), `recommendation_sets` (recommendations as typed columns)
Recommendation engine: **Go** — all recommendation math (CPU, memory, PVC, idle, namespace, VM, nodes, GPU, JVM, HPA, etc.) runs in Go; PostgreSQL stores digests and results only

### What Is Eliminated

| Component | Current | New |
|---|---|---|
| Kruize autotune (Java service) | Required | **Removed** from remote path |
| Kruize PostgreSQL database | Required | **Removed** |
| `/createExperiment` HTTP call | 1 per new workload | **Removed** |
| `/updateResults` HTTP call | 4N per hour (N containers) | **Removed** |
| `/updateRecommendations` HTTP call | N per cycle | **Removed** |
| `workload_metrics` JSONB table | Growing unbounded | **Replaced** by `daily_*_digests` partitioned tables (pre-aggregated in Go) |
| `recommendations` JSONB columns | Opaque blobs, marshal/unmarshal on every read | **Replaced** by relational columns (zero serialization) |
| Gson serialization/deserialization | Per HTTP call | **Removed** |
| `Collections.sort()` on 8,736+ boxed Doubles | Per recommendation | **Replaced** by Go `slices.Sort[int64]()` on ~96 values during ingestion |

### What Is Added

| Component | Purpose |
|---|---|
| `daily_*_digests` partitioned tables | Pre-aggregated daily digests with pre-computed percentiles, populated by Go during CSV ingestion. Native PostgreSQL RANGE partitioning with monthly retention. |
| Go recommendation functions | `recommendCPU()`, `recommendMemory()`, `detectIdle()`, `recommendPVC()`, `recommendNamespaceQuota()` — core math in process memory after a single digest read |
| `recommendAllWorkloads()` | Batch entry point: one Go pass per cluster computes all CPU + memory + idle + PVC (and related) recs, then batch-writes to PostgreSQL |
| Go recommendation heuristics | GPU MIG bin-packing, JVM/Quarkus tuning, HPA optimization, Go runtime, MachineSet right-sizing — complex branching logic |
| VM recommendation pipeline | `daily_vm_digests` table (populated by Go), `recommendVM()` in Go, VM CSV parser |
| Go orchestration | Kafka → CSV parse → in-memory aggregation → upsert digests → `recommendAllWorkloads()` (containers + VMs) → notification assembly → API serving |

### Deployment Model: Two Separate Binaries

The new ros-ocp-backend with superpowers is a **separate Go binary** forked from the current ros-ocp-backend. The fork lives in a separate branch (or repo) so the original remains untouched during the transition:

| | Old binary (`ros-ocp-backend`) | New binary (`ros-ocp-backend-superpowers`) |
|---|---|---|
| Codebase | Current `ros-ocp-backend` repo, unchanged | Fork of existing `ros-ocp-backend` — reuses Kafka, API, config, metrics infrastructure; replaces Kruize integration with native Go recommendation engine + plain PostgreSQL |
| Dependencies | Kruize HTTP API + Kruize PostgreSQL | PostgreSQL 16+ only (no Kruize, no custom extensions; optional `pg_partman`) |
| SaaS ingestion | Kafka consumer (`hccm.ros.events`) | Kafka consumer (same topic, **different consumer group** — both binaries receive all messages; Unleash flag checked per `org_id` to decide which binary processes each message) |
| On-prem ingestion | Kafka consumer (same as SaaS — AMQ Streams deployed by cost-onprem chart) | Kafka consumer (same as SaaS) |
| Operator requirement | Current operator (float CSV) | Same operator (float CSV); ros-ocp-backend converts to int at parse time. Later phases add new columns (OOM, Go runtime, VM). |

**Routing (per-org_id, not all-or-nothing):**
- **SaaS:** During transition, **both** binaries run simultaneously in different Kafka consumer groups, so both receive every message from `hccm.ros.events`. An **Unleash feature flag** (`ros-ocp.use-superpowers-binary`), evaluated per `org_id`, controls which binary processes each message:
  - Old binary: reads the message, extracts `org_id` from metadata, checks the flag. If flag is **OFF** for this org_id (default), processes the message. If **ON**, skips it (commits offset, moves on).
  - New binary: same check, opposite logic. If flag is **ON** for this org_id, processes. If **OFF**, skips.
  - Exactly one binary processes each message — no double-processing, no dropped messages.
  - Gradual rollout: enable the flag for a small set of org_ids first, monitor, expand. Rollback is per-org_id — disable the flag for a specific org_id and that org's data reverts to the old binary.
  - After full rollout and validation, the old binary is decommissioned.
- **On-prem:** Deploy **only** the new binary. No Kruize, no old ros-ocp-backend. The on-prem Helm chart ships the superpowers binary + PostgreSQL 16+ (already at PG 16 — no database upgrade needed).

**Benefits:**
- Zero risk of regression in the old path — the current binary is untouched.
- Forked codebase retains proven infrastructure (Kafka, API, config) while removing Kruize coupling.
- Independent deployment, scaling, and rollback.
- On-prem gets the superpowers binary from day one (no Kruize to deploy).

### Kruize Scope After This Change

Kruize autotune **remains available** for:
- `local_monitoring` mode (Kruize on customer cluster querying Prometheus directly)
- Experiment management (`local_experiment` mode with HPO)
- Any non-ROS use cases

Kruize is **no longer required** for the remote ROS pipeline.

### Database Requirements

ros-ocp-backend with superpowers requires **PostgreSQL 16+** with no custom extensions. No TimescaleDB, no tvondra/tdigest.

> **Why PostgreSQL 16?** The design uses only standard SQL features available since PG 10+ (declarative partitioning, `INSERT ... ON CONFLICT DO UPDATE`, `COPY FROM`, `gen_random_uuid()`). PG 16 is the baseline because it is already deployed in production: Koku SaaS runs PG 16 and the `cost-onprem-chart` ships PG 16. The current ros-ocp-backend SaaS runs PG 13 (end-of-standard-support on AWS RDS) and must be upgraded to at least PG 16. **PG 17 is not yet supported by Red Hat on OpenShift** (Crunchy PGO / RHEL-certified images) — when it becomes available, it offers incremental benefits: improved `MERGE` command (cleaner upsert alternative), better vacuum on partitioned tables, `COPY WITH (ON_ERROR ignore)`, and incremental sort improvements. These are nice-to-haves, not blockers.

> **Architecture decision (v1.0-timescaledb → v2.0-vanilla-pg → v3.0-pg17 → v4.0-go-engine → v5.0-pg16):** The original design required TimescaleDB for hypertables, continuous aggregates, compression, and t-digest. After analysis at production scale (8M+ containers on SaaS), we determined that: (1) raw metric readings do NOT need to be stored in PostgreSQL — the raw CSVs remain in S3, and the Go binary computes daily digest aggregations in memory during ingestion; (2) daily digest tables with native PostgreSQL partitioning provide equivalent query performance; (3) exact percentiles computed in Go via `slices.Sort()` on ~96 integer values per container per day are faster than approximate t-digest operations; (4) the SaaS deployment uses AWS RDS (via Clowder), which does not support TimescaleDB. The v2.0 design targeted PG 13+; v3.0 targeted PG 17+ for improved `MERGE`, better vacuum, and `COPY WITH (ON_ERROR)`. The v3.0 design used a hybrid Go + PL/pgSQL architecture (PL/pgSQL for math, Go for orchestration). The v4.0 design moves **all recommendation computation to Go** because customer-defined recommendation periods (arbitrary 1–90 day windows per customer) cannot be efficiently served by PL/pgSQL functions — each term would require a separate SQL scan of the same daily digest rows. The Go "read once, compute N terms" pattern reads the maximum window once per cluster, computes all terms in memory, and batch-writes results. The v5.0 revision lowers the minimum PostgreSQL version from 17+ to **16+** because PG 17 is not yet certified by Red Hat on OpenShift, and no PG 17-specific SQL feature is actually required by the design. The TimescaleDB design is preserved at git tag `v1.0-timescaledb`.

**Supported platforms:**

| Platform | Supported? | Notes |
|----------|:----------:|-------|
| **AWS RDS PostgreSQL** (Clowder-provisioned) | **PRIMARY (SaaS)** | Version 16+. Standard Clowder `spec.database`. Upgrade from current PG 13 (`clowdapp.yaml` `version: 13`) to PG 16 (`version: 16`). |
| **cost-onprem PostgreSQL StatefulSet** | **PRIMARY (on-prem)** | Version 16+. Already ships PG 16 — no upgrade needed. |
| **Crunchy PGO** (on OpenShift) | **YES** | PG 16 supported. For customers wanting HA, backups (pgBackRest), PgBouncer. |
| **Azure Database for PostgreSQL** | **YES** | Version 16+. |
| **Google Cloud SQL** | **YES** | Version 16+. |
| **Google AlloyDB** | **YES** | Version 16+. |
| **Aiven for PostgreSQL** | **YES** | Version 16+. |
| **Bare metal / VM** | **YES** | Any PostgreSQL 16+. |

**One optional extension: `pg_partman`** (v5.x) for automated partition creation and retention. Supported on AWS RDS (since PG 12.5), Crunchy PGO, Azure, GCP Cloud SQL, Aiven, and bare metal. `pg_partman` handles monthly partition pre-creation and old partition dropping automatically — eliminating custom Go partition management code. If `pg_partman` is unavailable, the Go binary falls back to manual partition management at startup (see NFR-8). All other features use standard PostgreSQL 16: native partitioning, `INSERT ... ON CONFLICT DO UPDATE`, `COPY FROM`, `gen_random_uuid()`.

> **Type simplification (BIGINT-everywhere):** All numeric metric columns use `BIGINT` in PostgreSQL and `int64` in Go — one integer type end-to-end. This accepts ~10% storage overhead vs a mixed `INT`/`BIGINT` schema (where CPU columns use 4-byte `INT` and memory columns use 8-byte `BIGINT`), in exchange for: (1) zero "which type?" decisions when adding columns — every metric is `BIGINT`, period; (2) no risk of `INT` overflow bugs on edge-case columns (counters, sums, aggregates); (3) a single `[]int64` slice type in Go for all sort/percentile/aggregation helpers — no generics, no duplication, no casts. The column name suffix (`_mc`, `_kib`, `_pct`, `_count`) conveys the *unit*, not the storage type. `SMALLINT[]` is retained for `notification_codes` (code values 1-255), and `REAL` for percentages/ratios.

**Key simplification:** The Go binary handles all metrics aggregation (percentile computation, daily digest rollup) in memory during CSV ingestion, **and** all recommendation computation (decay weighting, margin, trend detection, idle detection) in memory during the recommendation cycle. PostgreSQL stores only pre-computed daily digests and recommendation results — no raw metric readings, no continuous aggregates, no compression policies, no PL/pgSQL recommendation functions.

### API Contract: OpenAPI as Source of Truth

The complete API contract is defined in `openapi-superpowers.json` (OpenAPI 3.1), maintained alongside the Go source code. This file is the **single source of truth** for:
- All endpoint paths, methods, and parameters
- Request/response JSON schemas (including error responses)
- Authentication requirements per endpoint
- Pagination format (`limit`/`offset`/`meta.count`)

The requirements document describes intent and behavior; the OpenAPI spec defines the exact contract. If they diverge, the OpenAPI spec wins. The OpenAPI spec must be reviewed as part of every PR that changes API behavior. Code generation (server stubs, client SDKs, integration tests) should be derived from the OpenAPI spec where possible.

**Error response format** (all endpoints):
```json
{
  "status": 424,
  "code": "RBAC_UNAVAILABLE",
  "message": "RBAC service is unreachable",
  "request_id": "abc-123"
}
```

Standard HTTP status codes: 200 (success), 400 (validation), 403 (forbidden), 404 (not found), 424 (dependency unavailable), 500 (internal error). Feature-disabled endpoints return 404 (not 501) — same as if the endpoint doesn't exist.

---

## 3. Phasing Strategy

Work is organized into 11 phases. Phases 1–3 can overlap. Phases 4–9 can be parallelized where dependencies allow. Phase 8b (VM recommendations) can run in parallel with Phases 8 and 9. Phase 10 is the final cleanup.

```
Week:  1  2  3  4  5  6  7  8  9  10 11 12 13 14 15 16 17 18 19 20 21 22
       ├──┤
       Ph0 (Critical fixes)
          ├────────────────┤
          Phase 1 (Go recommendation engine: read once, compute N terms)
          ├──────────────────────┤
          Phase 2 (daily digests + integer types)
                ├──────────────────┤
                Phase 3 (decay weighting + custom timeframes)
                         ├────────────┤
                         Phase 4 (Memory full: OOM + adaptive + trend)
                               ├──────────┤
                               Phase 5 (GPU)
                         ├──────────────────┤
                         Phase 6 (New recs tier 1: idle, PVC, Go)
                               ├──────────┤
                               Phase 7 (Replica count + total impact)
                                           ├────────────┤
                                           Phase 8 (New recs tier 2: HPA, ephemeral, Node.js, ResourceQuota)
                                     ├──────────────────┤
                                     Phase 8b (VM recs: vCPU, memory, disk, IOPS)
                                           ├──────────────────┤
                                           Phase 8c (Node & MachineSet recs)
                                                 ├──────────┤
                                                 Phase 9 (JVM/Quarkus)
                                                       ├──────────┤
                                                       Phase 10 (Remove Kruize dep)
```

---

## 4. Phase 0: Critical Fixes (Weeks 1–2)

These are crash bugs and correctness issues in the current ros-ocp-backend that must be fixed regardless of architectural direction. They ship as hotfixes to the current codebase.

### REQ-0.1: Fix RBAC nil pointer panic [CRITICAL]

**Source:** Analysis §20.1

**Current behavior:** `rbac.go` dereferences nil `*http.Request` on `http.NewRequest` error, and nil `*http.Response` on `client.Do` error. No HTTP timeout is configured.

**Required changes:**
1. Check `err` from `http.NewRequest` — return `424` (fail closed, matching Koku pattern — see OQ#1 resolution), do not dereference nil.
2. Check `err` from `client.Do` — same handling (424 with structured error body).
3. Move `defer res.Body.Close()` inside the nil check.
4. Configure `http.Client{Timeout: 10 * time.Second}` for RBAC calls.
5. Check `err` from `io.ReadAll(res.Body)` — do not ignore.

**Acceptance criteria:** RBAC service unreachable → ros-ocp-backend returns defined error (not panic). Unit test: mock `http.Client` returning error.

### REQ-0.2: Fix RBAC `strings.Split` panic [CRITICAL]

**Source:** Analysis §20.12

**Current behavior:** `rbac.go` does `strings.Split(acl.Permission, ":")[1]` without length check. Malformed or empty permission string causes index-out-of-range panic.

**Required changes:** Check `len(parts) >= 2` before indexing.

### REQ-0.3: Fix API returns HTTP 200 on DB failure [HIGH]

**Source:** Analysis §20.2

**Current behavior:** `handlers.go` logs DB query error but does not return — continues to build 200 response with empty/stale data.

**Required changes:** After `db.Where(...).Find(...)`, check error, return `500 Internal Server Error` with structured error JSON.

### REQ-0.4: Fix Kafka consumer type assertion panic [HIGH]

**Source:** Analysis §20.3

**Current behavior:** `consumer.go` does `err.(kafka.Error)` without comma-ok form. Non-Kafka errors (network timeout, context cancellation) cause panic.

**Required changes:** Use `kafkaErr, ok := err.(kafka.Error)` — handle non-Kafka errors gracefully.

### REQ-0.5: Fix Kafka subscribe failure handling [HIGH]

**Source:** Analysis §20.4

**Current behavior:** If `consumer.SubscribeTopics()` fails, code continues to `ReadMessage` on an unsubscribed consumer, causing undefined behavior.

**Required changes:** Return fatal error or retry with backoff if subscribe fails.

### REQ-0.6: Add HTTP timeouts [HIGH]

**Source:** Analysis §20.6, §20.7

**Current behavior:** `ReadCSVFromUrl` and `Setup_kruize_performance_profile` use `http.Client{}` with no timeout. Can hang indefinitely.

**Required changes:** Set `Timeout: 30 * time.Second` on all HTTP clients.

### REQ-0.7: Fix poison message infinite redelivery [MEDIUM] — NOT IMPLEMENTED

**Source:** Analysis §20.9

**Current behavior:** On DB error in recommendation poller, handler returns without committing Kafka offset. Message is redelivered infinitely.

**Required changes:** Implement max-retry counter (e.g., 3 attempts). After max retries, log error with full context, commit offset, and optionally produce to a dead-letter topic.

### REQ-0.8: Fix GORM `.Where()` bug in housekeeper [MEDIUM]

**Source:** Analysis §20.11

**Current behavior:** `sourcesCleaner.go` calls `query.Where(...)` but discards the return value. GORM's `.Where()` returns a new `*gorm.DB` — the original is unmodified.

**Required changes:** `query = query.Where(...)`.

### REQ-0.9: Fix `ConvertDateToISO8601` error handling [MEDIUM]

**Source:** Analysis §20.8

**Current behavior:** `time.Parse` error is silently ignored, returns zero time.

**Required changes:** Return `(string, error)` — caller handles parse failures.

### REQ-0.10: Fix non-deterministic iteration order [MEDIUM]

**Source:** Analysis §20.13

**Current behavior:** Aggregator and CSV export iterate over Go maps, producing non-deterministic output order. Complicates testing and debugging.

**Required changes:** Sort keys before iteration in aggregator output and `GenerateCSVRows`.

### REQ-0.11: Reduce Kafka payload logging [MEDIUM]

**Source:** Analysis §20.5

**Current behavior:** Full Kafka message payloads (potentially large) logged at INFO level.

**Required changes:** Log at DEBUG level, or truncate to first 500 bytes at INFO.

### REQ-0.12: Fix `SendMessage` failure reconciliation [MEDIUM]

**Source:** Analysis §20.10

**Current behavior:** If Kafka `SendMessage` fails after DB write, the recommendation trigger is lost with no retry.

**Required changes:** Implement async producer with delivery reports and retry, or reconciliation loop for unprocessed workloads.

### Phase 0 Design Principles (carry forward to new binary)

Phase 0 fixes address bugs in the existing ros-ocp-backend codebase. While the new "superpowers" binary is a fresh Go module, these Phase 0 root causes MUST be codified as architectural requirements for the new binary to prevent recurrence:

1. **No nil pointer dereference:** All HTTP response and error return values must be checked before use. Use Go's `errors.As()` for type assertions.
2. **No unchecked type assertions:** Always use the comma-ok form (`val, ok := x.(Type)`) for interface assertions.
3. **Fail-closed RBAC:** If the RBAC service is unreachable, return 424 Failed Dependency (matching Koku — see OQ#1 resolution), not 200 with empty data.
4. **No 200-on-error:** If a database query fails, the API must return an error status code (500), never 200 with empty/stale data.
5. **Structured error responses:** All errors return JSON with `code`, `message`, and `request_id`.
6. **HTTP client timeouts:** All outbound HTTP clients (RBAC, Koku cost integration) must have explicit `Timeout` configured.
7. **Kafka resilience:** Use `errors.As()` for Kafka errors, handle context cancellation gracefully, implement retry/DLQ for failed sends.
8. **Log levels:** Never log large payloads at INFO. Use DEBUG for payloads, INFO for summaries.

---

## 5. Phase 1: Core Recommendation Engine — Go "Read Once, Compute N Terms" (Weeks 3–8)

Port the CPU and memory recommendation algorithms from Kruize Java to a **Go recommendation engine** using the "read once, compute N terms" pattern:

1. **Read once:** For each cluster, the Go binary executes a single `SELECT` on `daily_container_digests` fetching the maximum window required across all of the customer's configured terms (e.g., if the customer uses 10d/30d/90d, read 90 days of digests).
2. **Compute N terms:** From the same in-memory `[]DigestRow` buffer, compute all customer-defined terms — subsetting the data by date range, applying decay weighting, computing percentiles, margins, trends, and idle detection for each term.
3. **Batch write:** Write all recommendation results for all terms via `COPY FROM` (a single batch write per cluster).

This pattern eliminates redundant I/O: regardless of whether the customer uses 3 terms or 5 terms, the digest data is read exactly once. All recommendation logic (decay weighting, percentile selection, margin computation, trend detection, OOM-aware adjustment) runs in Go, leveraging the pre-computed daily digest columns.

**Phase 1 → Phase 2 dependency:** Phase 2 creates the `daily_container_digests` table and the Go in-memory aggregation pipeline. Phase 1 recommendation code reads from digest tables populated by Phase 2. This means Phase 1 has no dependency on Phase 3 — it works on digest data from day one.

> **Why Go instead of PL/pgSQL?** The v3.0 design used PL/pgSQL functions for recommendation math. Customer-defined recommendation periods (each customer chooses their own 1–90 day windows) make PL/pgSQL inefficient: each term would require a separate PL/pgSQL invocation scanning overlapping ranges of the same daily digest rows. The Go "read once, compute N terms" pattern amortizes the I/O cost across all terms. Additionally: (1) Go is easier to test, debug, and profile than PL/pgSQL; (2) Go's `slices.Sort()` on `[]int64` is faster than PL/pgSQL array operations; (3) all recommendation logic lives in a single codebase instead of split across Go and SQL.

### REQ-1.1: CPU Recommendation — Remove 1-core discontinuity [CRITICAL]

**Source:** Analysis §12, Problem 1

**Current Kruize behavior:** `getCPURequestRecommendation` uses completely different algorithms above and below 1 core. Below 1 core: `max(cpuUsageMax) + max(cpuThrottleMax)`. Above 1 core: `percentile(cpuUsageMax) + percentile(cpuThrottleMax)`.

**Required behavior:** Use a single consistent algorithm for all CPU levels:

```
cpu_effective_per_interval = max(cpuUsageMax, cpuUsageAvg + cpuThrottleAvg)
recommendation = percentile(cpu_effective_values, target) × (1 + safety_margin)
floor(recommendation, 25m)
```

Where `target` = 60th percentile (cost model) or 98th percentile (performance model), and `safety_margin` = 0.15 (15%, configurable).

### REQ-1.2: CPU Recommendation — Remove per-pod estimation [CRITICAL]

**Source:** Analysis §12, Problem 2

**Current Kruize behavior:** Estimates per-pod values via `numPods = cpuUsageSum / cpuUsageAvg`, then divides all metrics by `numPods`. This is mathematically fragile (division by average yields correct count only when all values are equal).

**Required behavior:** Treat each data point as a per-container value (which is what the operator produces). Do not attempt per-pod estimation. For namespace recommendations, use aggregate totals directly.

### REQ-1.3: CPU Recommendation — Cost and Performance models [HIGH]

**Source:** Analysis §12, E.9

**Required behavior:** Produce two recommendation variants per container:

| Model | CPU Percentile (default) | Safety Margin | Use Case |
|---|---|---|---|
| Cost | p60 (configurable) | 15% | Minimize spend, tolerate occasional throttling |
| Performance | p98 (configurable) | 15% | Minimize throttling, higher spend |

Each model produces `request` and `limit` values. Limit = request × limit_multiplier (default 1.0 for Guaranteed QoS, or configurable).

**Customer-defined percentiles (future):** The Go recommendation engine accepts cost and performance percentile parameters via the `recommendation_profiles` table. Defaults are p60/p98 for CPU and p95/p100 for memory. When custom profile support is added, the Go engine reads the customer's profile configuration and applies the percentile values during in-memory recommendation computation. Customer profiles can define any percentile (e.g., "aggressive cost" at p50, "balanced" at p75, "ultra-safe" at p99.5).

### ~~REQ-1.4: CPU Recommendation — Confidence bounds~~ [DEFERRED to post-MVP]

**Status:** Deferred — the cost/performance dual-model output (REQ-1.3, REQ-1.7) already provides an actionable range (p60 for cost savings, p98 for performance safety). Adding separate `lower_bound` / `upper_bound` values on top of this adds API complexity for minimal user benefit. Revisit after MVP when user feedback indicates demand for explicit confidence intervals.

### REQ-1.5: Memory Recommendation — Basic implementation [HIGH]

**Source:** Analysis §16

**Required behavior (initial, before OOM integration):**

```
memory_request = max(memoryUsageMax_values) × (1 + adaptive_margin)
memory_limit = memory_request × limit_multiplier
```

Where `adaptive_margin` = `clamp((p95 - p50) / mean, 0.15, 0.50)` — measuring tail spread relative to average usage from pre-computed daily digest columns. This captures how far peak usage deviates from typical usage, directly relevant for right-sizing headroom. Fixed 0.20 in the initial implementation (before daily digest tables are populated).

**Key fixes from Kruize:**
1. Use `max()` directly instead of sorting 8,736 elements for p100.
2. Do not compute unused MIN values.
3. Do not use JSONObject intermediate representation — work with native Go types.
4. Remove per-pod estimation (same as CPU).
5. Produce separate request and limit values.

### REQ-1.6: Memory Recommendation — Cost and Performance models [HIGH]

| Model | Memory Percentile | Margin | Use Case |
|---|---|---|---|
| Cost | p95 | Adaptive (tail-spread CV, 15–50%) | Minimize waste, accept rare OOM |
| Performance | p100 (max) | Adaptive (tail-spread CV, 15–50%) | Minimize OOM risk |

### REQ-1.7: Recommendation Engine — Dual model output [HIGH]

**Source:** Analysis §12, §16, Appendix B

Every recommendation type must produce both cost and performance model outputs in a single pass. The current Kruize engine builds the filtered result map twice (once per model) — the Go recommendation engine must process the daily digest values once per container and extract both percentile targets from the same data.

### REQ-1.8: Recommendation Engine — Customer-defined term support [HIGH]

**Source:** Analysis E.6, E.9, COST-5691

Support **customer-defined recommendation periods**: each customer (org_id) configures up to 3 term windows, each from 1 to 90 days. This replaces the fixed short_term/medium_term/long_term system.

**Defaults (backwards compatible with legacy):**

| Term | Default Window | Min Data Required | Default Decay Half-Life |
|---|---|---|---|
| term_1 | 1 day | 0.5 hours (30 min) | None |
| term_2 | 7 days | 48 hours (2 days) | 72 hours |
| term_3 | 15 days | 192 hours (8 days) | 168 hours |

**Customer-defined example:** Customer A uses 3d/20d/60d; Customer B uses 10d/30d/90d.

| Customer | term_1 | term_2 | term_3 |
|---|---|---|---|
| Default | 1 day | 7 days | 15 days |
| Customer A | 3 days | 20 days | 60 days |
| Customer B | 10 days | 30 days | 90 days |

**Configuration storage:** Defaults (1d/7d/15d with standard decay rates) are hardcoded in Go as `DefaultTerms` — zero DB cost for the vast majority of customers who never customize. Customers who override their term windows have 3 rows in `org_recommendation_terms` (keyed by `org_id, term_ord`). The Go engine queries this table once per cluster; if no rows exist, it falls back to `DefaultTerms`. Percentile model selection (cost vs performance) remains in the `recommendation_profiles` table and is orthogonal to term windows.

**Minimum data threshold scaling:** `min_data = max(0.5h, window_days × 0.3 × 24h)`. For a 60-day term, the minimum data required is ~18 days (432 hours). This prevents recommendations based on an unrepresentatively small fraction of the window.

**Decay half-life scaling:** Default formula: `half_life = window_days × 0.5 × 24h`. For a 60-day term, the default half-life is 720 hours (30 days). Customers can override the half-life per term. For term_1 ≤ 1 day, no decay is applied (all data is equally recent).

Recommendations are generated with partial data once the minimum threshold is met — they do not require a full window. The `INFO_NOT_ENOUGH_DATA` notification is emitted only when available data is below the minimum threshold for a given term.

**Go "read once, compute N terms" optimization:** The Go engine reads `max(term_1, term_2, term_3)` days of daily digests in a single batch query, then computes all 3 terms from the same in-memory buffer by subsetting the date range and applying each term's decay half-life. This means a customer with 10d/30d/90d terms incurs only the I/O cost of a single 90-day read, not three separate reads.

Custom timeframes (from COST-5691) extend this with user-defined term durations and business hours filtering.

### REQ-1.9: Recommendation Engine — Notification system [HIGH]

**Source:** Analysis E.9

Port the notification system from Kruize. Key notifications:

| Code | Condition | Message | Recommendation returned? |
|---|---|---|---|
| `INFO_NOT_ENOUGH_DATA` | Hours < term min data threshold | "Not enough data for {term} recommendation" | **No** — suppressed for this term only; other terms with sufficient data still return recommendations |
| `IDLE_WORKLOAD` (code 5) | CPU usage < 1m sustained | "Container CPU usage is near zero" | **Yes** — recommendation returned alongside notification; user should consider removing the workload |
| `IDLE_WORKLOAD` (code 5) | Memory usage < 1 MiB sustained | "Container memory usage is near zero" | **Yes** — recommendation returned alongside notification |
| `WARNING_CPU_LIMIT_NOT_SET` | No CPU limit in metrics | "CPU limit is not set" | **Yes** — limits are not inputs to the recommendation algorithm; informational only |
| `WARNING_MEMORY_LIMIT_NOT_SET` | No memory limit in metrics | "Memory limit is not set" | **Yes** — informational only |

Additional notifications are defined per recommendation type in later phases.

### REQ-1.10: Recommendation persistence from Go [HIGH]

**Source:** Analysis E.4, §27, §29

Recommendations are written by the Go recommendation engine (`recommendCPU()`, `recommendMemory()`, etc.) using batch `COPY FROM` / `INSERT ... ON CONFLICT DO UPDATE` into `recommendation_sets` — relational columns only (REQ-2.5), not JSONB. Each row stores one (container, term) combination with typed integer columns for CPU millicores / memory KiB. The Go API layer assembles the nested response format (`short_term`/`medium_term`/`long_term` → `cost`/`performance` → `cpu`/`memory`) from multiple rows — backward-compatible with the current response shape.

### REQ-1.11: Batch recommendation entry point [HIGH]

**Source:** Analysis §29

Implement `recommendAllWorkloads(ctx, orgID, clusterUUID, start, end)` in Go: one digest read for the max term window, then `recommendCPU()`, `recommendMemory()`, and later `detectIdle()`, `recommendPVC()`, `recommendNamespaceQuota()` (and related) over the in-memory buffer, writing results via batch upsert to `recommendation_sets`. The ingestion orchestrator invokes this once per cluster per cycle — no `CALL` into PostgreSQL for recommendation math.

```go
err := engine.RecommendAllWorkloads(ctx, orgID, clusterUUID, start, end)
```

One read of digest rows, all terms computed in process memory, then one batch write.

### REQ-1.12: Shadow-mode validation [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §29

**Scope:** Within the **new binary** (ros-ocp-backend-superpowers), validate the new Go digest-based recommendation engine against a reference path (e.g. legacy binary + Kruize, or a shadow implementation). Two paths run in parallel for a configurable subset of clusters (default: all):

| Path | Computation | Writes to |
|---|---|---|
| **Reference** | Legacy pipeline or alternate Go implementation (exact-percentile or legacy semantics) | `recommendation_sets_shadow` |
| **Production Go engine** | `recommendCPU()` / `recommendMemory()` etc. on pre-computed `daily_container_digests` | `recommendation_sets` (production) |

A reconciliation job compares the two and logs divergences. Any mismatch exceeding rounding tolerance (1 millicore / 1 KiB) triggers a structured warning log with `org_id`, `cluster_uuid`, `container`, `term`, `engine`, and both values.

**This validates:** (1) Go engine correctness against the reference path, (2) decay weighting correctness, (3) digest table completeness. Shadow mode is disabled (`ROS_ENABLE_SHADOW_MODE=false`) after validation confirms the production path matches within tolerance for ≥1 week of production traffic.

### REQ-1.13: Namespace recommendations [HIGH]

**Source:** Analysis §6 (namespace support in ros-ocp-backend)

**Required behavior:** Port namespace-level recommendations following the same pattern as container recommendations:

1. **`daily_namespace_digests` table** — populated by Go during CSV ingestion (aggregating container-level data up to namespace) — provides CPU and memory usage at namespace granularity.
2. **`recommendNamespace()` Go function** — same dual-model (cost/performance) output, same decay weighting, same percentile parameterization as `recommendCPU()` and `recommendMemory()`, but operating on namespace-level aggregates. Reads cost/performance percentile settings from the active recommendation profile.
3. **`namespace_recommendation_sets` relational columns** — convert from JSONB to relational columns (same pattern as `recommendation_sets` in REQ-2.5): `term`, `engine`, `current_cpu_request_millicores`, `rec_cpu_request_millicores`, `current_memory_request_kib`, `rec_memory_request_kib`, `variation_cpu_request_pct`, `variation_memory_request_pct`, `notification_codes`, `confidence_level`. Primary key: `(org_id, cluster_uuid, namespace, term, engine)`.
4. **`recommendAllWorkloads()` batch entry** — invokes `recommendNamespace()` after container and VM recommendations. Results written via `INSERT ... ON CONFLICT DO UPDATE` or `COPY FROM`.
5. **Kafka path** — in the new binary, namespace recommendations are produced alongside container recommendations in the same ingestion cycle (no separate Kafka topic needed). The `ExperimentType`/`PayloadType` field in the Kafka recommendation message distinguishes container vs namespace data.
6. **API** — namespace recommendations use the same API response shape as container recommendations, served from relational columns with zero `json.Unmarshal`.

---

## 6. Phase 2: Metrics Pipeline — Daily Digests + Integer Types (Weeks 3–10)

### REQ-2.1: Daily digest pipeline for metrics [CRITICAL]

**Source:** Analysis §3, §11, §25, §28

**Required behavior:** After parsing CSV data, the Go binary computes daily digest aggregations **in memory** (percentiles via `slices.Sort()` on ~96 integer values per container per day) and upserts the results into `daily_container_digests`. No raw metric readings are stored in PostgreSQL — the CSVs remain in S3.

**Ingestion pipeline (per Kafka message):**

1. Download CSV from S3 presigned URL.
2. Parse CSV rows in Go, converting floats → integers (millicores/KiB) per REQ-2.3.
3. Validate each row: parseable, no NaN/Inf, no invalid negatives. Skip invalid rows with structured error logging.
4. Group valid rows by `(org_id, cluster_uuid, namespace, workload, container_name, date)`.
5. For each group, compute exact percentiles (p50, p60, p95, p98, p99), MAX, mean, OOM count sum, sample count using `slices.Sort()` on `[]int64` (uniform type for both CPU and memory in Go).
6. Upsert digest rows into `daily_container_digests` via `INSERT ... ON CONFLICT DO UPDATE`. If a row already exists for the same `(container, day)` — indicating late-arriving data — re-aggregate from the full sample (see REQ-3.1 late-arriving data handling).

**Why in-memory aggregation works:** Each 15-minute CSV contains ~96 data points per container per day. Even for 10K containers in a single batch, that's 960K integers (~8 MB at 8 bytes each). The Go binary processes one Kafka message at a time, so peak memory for aggregation is bounded by `batch_size × 96 × sizeof(int64)`.

**Go-side CSV validation (before upsert):**
1. Parse each CSV row in Go (already done for type conversion).
2. Validate: parseable as int/float, no NaN/Inf, no negative values where invalid.
3. Convert float→int (millicores/KiB) at this stage.
4. Skip invalid rows with structured error logging (`org_id`, `cluster_uuid`, row number, column, invalid value).

**Acceptance criteria:** 10K containers × 4 intervals ingested and aggregated in < 5 seconds.

### REQ-2.2: Multi-tenancy via org_id [HIGH]

**Source:** Analysis §3

**Required behavior:** All digest tables include `org_id` as a column (not a label — this is SQL, not PromQL). All queries must filter by `org_id` to prevent cross-tenant data access. Partitioned tables use `org_id` as part of the composite primary key to ensure efficient per-tenant query isolation.

### REQ-2.3: Integer types at parse time [HIGH]

**Source:** Analysis §9

**Required behavior:** The operator CSV format is unchanged — it continues writing Prometheus float values verbatim (CPU in cores, memory in bytes). ros-ocp-backend converts these floats to integers at CSV parse time:
1. Parse each CPU column as `float64`, then convert: `int64(math.Round(value × 1000))` → millicores.
2. Parse each memory column as `float64`, then convert: `int64(math.Round(value / 1024))` → KiB.
3. **`int64` everywhere — Go and PostgreSQL.** All integer metric columns use `int64` in Go and `BIGINT` in PostgreSQL. No `int32`, no `INT`. One type end-to-end, zero decisions.

> **Design decision (BIGINT-everywhere):** The original design used `INT` (4 bytes) for CPU columns and `BIGINT` (8 bytes) for memory columns in PostgreSQL, saving ~4 bytes per CPU column per row. At scale (8M containers × 91 days = 728M rows × ~16 INT columns = ~46 GB savings), this is a ~10% storage reduction. However, the INT/BIGINT split introduces complexity: developers must decide which type to use for each new column, the Go `database/sql` driver silently narrows `int64` to `INT` (risking overflow bugs on edge-case columns like counters or sums), and two types means two mental models. **We accept the ~10% storage overhead for uniform types end-to-end.** The naming suffix (`_mc`, `_kib`, `_pct`, `_count`) tells you the *unit*, not the storage type — every numeric metric column is `BIGINT`, period.

**No operator change required.** This eliminates the operator dependency and any deployment ordering concerns. All existing operators work without modification.

### REQ-2.4: Eliminate `workload_metrics` JSONB table [HIGH] — PARTIAL

**Source:** Analysis §3, §25, §27

**Required behavior:** The `workload_metrics.usage_metrics` JSONB column is never read by any code path (verified §27). Actions:
1. Stop writing to the `workload_metrics` table immediately (no migration period needed — data is never read).
2. Drop the table after confirming no external consumers depend on it.
3. Remove the `workload_metrics` partitioning trigger and functions.
4. Remove the `WorkloadMetrics` GORM model and `BatchInsertWorkloadMetrics` function.

### REQ-2.5: Replace `recommendations` JSONB with relational columns [HIGH]

**Source:** Analysis §27

**Required behavior:** Replace the opaque `recommendations` JSONB column in `recommendation_sets` and `namespace_recommendation_sets` with typed relational columns:

```sql
ALTER TABLE recommendation_sets
    ADD COLUMN term TEXT NOT NULL DEFAULT 'short',
    ADD COLUMN engine TEXT NOT NULL DEFAULT 'cost',
    ADD COLUMN current_cpu_request_millicores BIGINT,
    ADD COLUMN current_cpu_limit_millicores BIGINT,
    ADD COLUMN current_memory_request_kib BIGINT,
    ADD COLUMN current_memory_limit_kib BIGINT,
    ADD COLUMN rec_cpu_request_millicores BIGINT,
    ADD COLUMN rec_cpu_limit_millicores BIGINT,
    ADD COLUMN rec_memory_request_kib BIGINT,
    ADD COLUMN rec_memory_limit_kib BIGINT,
    ADD COLUMN variation_cpu_request_pct REAL,
    ADD COLUMN variation_memory_request_pct REAL,
    ADD COLUMN notification_codes SMALLINT[],
    ADD COLUMN confidence_level REAL,
    ADD COLUMN estimated_savings_cents REAL,
    ADD COLUMN recommendation_applied_at TIMESTAMPTZ,  -- REQ-10.7: adoption detection
    ADD COLUMN stale BOOLEAN DEFAULT false;            -- REQ-10.8: staleness flag
```

**Primary key adjustment:** The addition of `term` and `engine` columns means each container has 6 rows (3 terms × 2 engines). The primary key must be updated to include these new fields:

```sql
-- Drop existing PK (exact name depends on current schema)
ALTER TABLE recommendation_sets DROP CONSTRAINT IF EXISTS recommendation_sets_pkey;
-- New composite PK
ALTER TABLE recommendation_sets ADD PRIMARY KEY
    (org_id, cluster_uuid, namespace, workload, container_name, term, engine);
```

This enables clean `INSERT ... ON CONFLICT DO UPDATE` / `COPY FROM` from the Go recommendation engine. At 50K containers × 6 rows = 300K rows — trivial for PostgreSQL. The Go API assembles the nested response format (`short_term`/`medium_term`/`long_term` → `cost`/`performance` → `cpu`/`memory`) by querying all 6 rows for a container and restructuring.

**Migration strategy:**
1. Add new columns alongside existing `recommendations` JSONB (additive, non-breaking).
2. Dual-write: populate both JSONB and relational columns during transition.
3. Switch API reads to use relational columns (eliminate `json.Unmarshal` + `map[string]interface{}` path).
4. After validation, stop writing JSONB. Drop column in subsequent migration.

**Acceptance criteria:** API response generated from relational columns with zero `json.Unmarshal` calls. GORM struct scan directly into typed Go fields.

### REQ-2.6: Partition retention management [MEDIUM]

**Source:** Analysis §7, §28

**Required behavior:** Manage data retention via `pg_partman` (preferred) or Go-managed partition lifecycle:
- Daily digest retention: 45 days (sufficient for all standard terms plus buffer)
- Recommendation history retention: 90 days
- Recommendation quality retention: 90 days

**With `pg_partman` (preferred):** Register each partitioned table with `partman.create_parent()`, set `p_premake` (pre-create months ahead) and `p_retention` (auto-drop old partitions). `pg_partman`'s `run_maintenance()` is called via `pg_cron` (on RDS) or a Go background goroutine (on-prem). This eliminates all custom partition management code.

**Without `pg_partman` (fallback):** The Go binary manages partitions proactively at startup and via background task (see NFR-8).

### REQ-2.7: Create daily digest tables (structure only) [HIGH]

**Source:** Phase dependency resolution (I3)

**Required behavior:** As part of Phase 2 setup, create the `daily_container_digests`, `daily_namespace_digests`, and other digest tables (see §18 for full definitions). These are standard PostgreSQL tables with RANGE partitioning on `bucket_date`. The Go ingestion pipeline populates them with pre-computed percentiles during CSV processing. Phase 1 Go recommendation code reads pre-computed percentile columns from these tables — no runtime percentile computation in SQL.

---

## 7. Phase 3: Decay Weighting and Custom Timeframes (Weeks 5–12)

Phase 3 adds exponential decay weighting in the Go recommendation engine, enables arbitrary date ranges (custom timeframes), and validates recommendation accuracy via shadow mode.

### REQ-3.1: Daily digest tables with pre-computed percentiles [CRITICAL]

**Source:** Analysis §10, §25, §28

**Required behavior:** The Go binary computes exact percentiles during CSV ingestion and stores pre-computed values in `daily_container_digests` and `daily_namespace_digests` tables. The Go recommendation engine reads these pre-computed columns after loading digest rows — no runtime percentile computation in SQL.

**How it works:**
1. Go binary parses CSV rows, groups by `(container, day)`.
2. For each group (~96 integer values per metric per day), sorts via `slices.Sort()` and computes exact percentiles (p50, p60, p95, p98, p99), MAX, mean, OOM count sum, sample count.
3. Upserts into `daily_container_digests` via `INSERT ... ON CONFLICT DO UPDATE` (merging with existing data for the same day if CSV batches arrive in multiple messages).
4. Similarly for namespace-level aggregation into `daily_namespace_digests`.

**Late-arriving data handling:** If a CSV arrives for a past day (e.g., operator was offline), the Go binary must **re-aggregate from scratch** for that day. Pre-computed percentiles cannot be merged — `p95(A ∪ B) ≠ f(p95(A), p95(B))`. The strategy:

1. Go detects that `daily_container_digests` already has a row for this `(container, day)` pair (the `ON CONFLICT` is triggered).
2. Go fetches the S3 keys for all CSV files covering that day from the manifest table (Koku's `costusagereportmanifest` records which files belong to which date range).
3. Go re-downloads and re-parses all CSVs for the affected day, collecting the **full sample** for each container.
4. Go computes exact percentiles on the combined `[]int64` sample and upserts the corrected digest row.

**Why this is acceptable:** Late-arriving data is rare (operator offline, network partition). The re-download cost is bounded: each 15-minute CSV for a single day is ~4 intervals × N containers × ~200 bytes ≈ tens of KB per file. For a typical cluster with 1K containers, re-aggregating one day requires downloading ~96 small files (~20 MB total) — seconds, not minutes.

**Fallback (if S3 originals are unavailable):** If the original CSVs have been deleted from S3 (past retention), the Go binary falls back to a conservative merge: take the MAX of existing and new MAX columns, weighted-average the mean columns by `sample_count`, and **take the higher value** for each percentile column (safe for right-sizing — overestimates rather than underestimates). This fallback is logged as a warning with `ros_ingestion_late_data_fallback_total` counter.

This replaces (superseded design paths that were **not implemented**):
- Hypothetical custom Go t-digest sketch (~300 lines) — rejected in favor of exact `slices.Sort()` on ingestion samples
- Manual serialization/deserialization of digest blobs to `BYTEA`
- End-of-day cron job for digest snapshots
- Any dependency on PostgreSQL extensions for statistical computation

**No library dependencies.** Standard Go `slices.Sort()` on `[]int64`. One type end-to-end: `int64` in Go, `BIGINT` in PostgreSQL, for all numeric metrics (CPU millicores, memory KiB, counts). No narrowing, no type decisions, no PostgreSQL extensions required.

### REQ-3.2: Recommendation computation via Go "read once, compute N terms" [HIGH]

**Source:** Analysis §10, §12 Option C, §28, §29

**Required behavior:** All recommendation computation runs in Go using the "read once, compute N terms" pattern. For each cluster:

1. **Read once:** Single batch query fetches all daily digest rows for the maximum term window:
   ```sql
   SELECT namespace, workload, container_name, bucket_date,
          cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc,
          cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_mean_mc,
          cpu_usage_max_mc, mem_usage_p50_kib, mem_usage_p95_kib,
          mem_usage_p98_kib, mem_usage_p99_kib, mem_usage_mean_kib,
          mem_usage_max_kib, oom_count_sum, sample_count
   FROM daily_container_digests
   WHERE org_id = $1 AND cluster_uuid = $2
     AND bucket_date >= $3 AND bucket_date < $4
   ORDER BY namespace, workload, container_name, bucket_date;
   ```
   For a customer with terms 10d/30d/90d, `$3` = `today - 90 days`, `$4` = `today`. All three terms' data is fetched in one query.

2. **Group by container:** Go groups the result set into a `map[(namespace, workload, container_name)][]DigestRow`.

3. **Compute N terms per container:** For each container's `[]DigestRow` slice, iterate over the customer's configured terms (from `org_recommendation_terms`, or `DefaultTerms` if no overrides):

   ```go
   // Pseudocode for CPU recommendation per container, per term
   func recommendCPU(rows []DigestRow, termDays int, decayHalfLifeHours float64,
       costPct, perfPct float64, minMargin, maxMargin float64) CPURec {
       cutoff := today.AddDate(0, 0, -termDays)
       var weightedCostSum, weightedPerfSum, weightedP50Sum float64
       var weightedP95Sum, weightedP99Sum, weightedMeanSum, weightSum float64
       for _, r := range rows {
           if r.BucketDate.Before(cutoff) { continue }
           age := time.Since(r.BucketDate).Hours()
           w := math.Exp(-age / decayHalfLifeHours)
           costVal := selectPercentile(r, costPct) // picks p50/p60/p95/p98/p99
           perfVal := selectPercentile(r, perfPct)
           weightedCostSum += float64(costVal) * w
           weightedPerfSum += float64(perfVal) * w
           weightedP50Sum  += float64(r.CpuP50) * w
           weightedP95Sum  += float64(r.CpuP95) * w
           weightedP99Sum  += float64(r.CpuP99) * w
           weightedMeanSum += float64(r.CpuMean) * w
           weightSum += w
       }
       costPctW := weightedCostSum / weightSum
       perfPctW := weightedPerfSum / weightSum
       p50w := weightedP50Sum / weightSum
       p95w := weightedP95Sum / weightSum
       p99w := weightedP99Sum / weightSum
       meanw := weightedMeanSum / weightSum
       // Adaptive margin: tail-spread CV clamped to [minMargin, maxMargin]
       margin := math.Min(maxMargin, math.Max(minMargin, 1.0+(p95w-p50w)/meanw))
       // Trend detection: linear regression on daily perf_pct values
       slope := regrSlope(rows, cutoff, perfPct)
       return CPURec{
           CostRequest: max(1, int64(math.Round(costPctW * margin))),
           CostLimit:   max(1, int64(math.Round(p99w * 1.05))),
           PerfRequest: max(1, int64(math.Round(perfPctW * margin))),
           PerfLimit:   max(1, int64(math.Round(p99w * 1.05))),
           IsIdle:      perfPctW < 10,
           CV:          (p95w - p50w) / meanw,
           TrendSlope:  slope,
       }
   }
   ```

4. **Batch write:** All recommendation results for all containers × all terms are collected into a `[]RecommendationRow` and written via `COPY FROM` in a single transaction.

**Memory recommendation** follows the same pattern: `recommendMemory()` uses memory digest columns (`mem_usage_p95_kib`, `mem_usage_max_kib`, etc.), adaptive tail-spread CV margin, and OOM event data (`oom_count_sum`). Both CPU and memory are computed from the same `[]DigestRow` slice — single pass per container.

**Decay weighting** is computed per-row in Go: `weight(day) = exp(-age_hours / half_life_hours)`. For terms ≤ 1 day, the decay half-life is set to `math.MaxFloat64` (effectively no decay). For longer terms, the customer's configured half-life is used (default: `window_days × 0.5 × 24h`).

**Performance characteristics (8M containers, 90-day max window):**
- Single cluster (1K containers): ~20-30ms total (read ~10ms, compute 3 terms ~10ms, write ~5ms)
- Full SaaS (8M containers across ~10K clusters, 10 workers): ~30 seconds
- I/O is amortized: reading 90 days of digests for 3 terms costs the same as reading for 1 term

### REQ-3.3: Custom timeframe and customer-defined term support [HIGH]

**Source:** Analysis §10, COST-5691 (IMPL-PRD99)

**Required behavior:** Two levels of custom time support:

1. **Customer-defined recommendation periods (REQ-1.8):** Each customer configures 3 term windows (1–90 days each) via `org_recommendation_terms` (defaults: 1d/7d/15d hardcoded in Go). The Go recommendation engine uses the "read once, compute N terms" pattern — reading the maximum window once and computing all terms from the same in-memory data.

2. **Ad-hoc custom timeframes (API-time):** The API accepts arbitrary `start_date` / `end_date` parameters for on-demand recommendation computation (REQ-3.4). The Go engine reads the requested window from `daily_container_digests` and computes recommendations in memory, the same way as batch computation.

**Query pattern (used by both batch and on-demand paths):**

```sql
SELECT namespace, workload, container_name, bucket_date,
       cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc,
       cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_mean_mc,
       cpu_usage_max_mc, mem_usage_max_kib, oom_count_sum, sample_count
FROM daily_container_digests
WHERE org_id = $1 AND cluster_uuid = $2
  AND bucket_date >= $start_date AND bucket_date < $end_date
ORDER BY namespace, workload, container_name, bucket_date;
```

**Business hours: implemented.** Schedule-aware sizing for containers and namespaces is available via org/cluster/namespace settings, dual `all_hours` and `business_hours` digest streams, and nested `business_hours` blocks on recommendation detail responses. Dollar savings remain on the `all_hours` row only. See [Business Hours feature guide](../plugin-reference/business-hours.md) and [Business Hours (public)](../plugin-reference/business-hours.md).

### REQ-3.4: Real-time recommendation computation via Go [MEDIUM] — NOT IMPLEMENTED

**Source:** Analysis §25.5, §29

**Required behavior:** Compute recommendations on-demand at API request time using the Go recommendation engine:
1. API handler receives request with `start_date` / `end_date` parameters.
2. Go reads daily digest rows for the requested window from `daily_container_digests` (~1-3ms for a single container, ~5-15ms for a full cluster).
3. Go computes recommendations in memory (same `recommendCPU()` / `recommendMemory()` functions used by the batch path).
4. Return computed recommendation directly to the HTTP response.

This eliminates the recommendation polling loop and provides always-fresh recommendations for any custom timeframe. The batch path runs after each ingestion cycle for pre-computation of the customer's configured terms; the on-demand path supplements it for ad-hoc custom timeframe requests.

### REQ-3.5: Recommendation engine testing and versioning [HIGH] — PARTIAL

**Source:** Analysis §29

**Required behavior:**
- **Unit testing:** Go recommendation functions (`recommendCPU()`, `recommendMemory()`, `detectIdle()`, etc.) are pure functions with well-defined inputs and outputs. Unit tests use table-driven test cases with known digest inputs and expected recommendation outputs. No database required for unit tests.
- **Integration testing:** End-to-end tests seed `daily_container_digests` via `testcontainers-go` (PostgreSQL 16), run the full "read once, compute N terms" pipeline, and verify recommendation results against expected values from the analysis test vectors.
- **Shadow validation:** REQ-1.12 shadow-mode runs the new Go engine alongside the old binary and compares recommendation results.
- **Versioning:** Recommendation algorithm changes are versioned via the Go binary version. Database schema changes are managed via `golang-migrate`.

---

## 8. Phase 4: Memory Algorithm with OOM Feedback (Weeks 8–14)

### REQ-4.1: OOM event collection [HIGH]

**Source:** Analysis §16

**Operator change required:** Add Prometheus query for `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` to operator's ROS query set. New CSV columns: `oom_last_timestamp`, `oom_count` (per container per interval).

**ros-ocp-backend:** Parse OOM columns, aggregate `oom_count_sum` in `daily_container_digests` and/or flag in workload metadata.

### REQ-4.2: Adaptive margin via tail-spread CV [HIGH]

**Source:** Analysis §16

**Required behavior:** Replace fixed 20% margin with adaptive margin based on tail spread:

```
CV = (p95 - p50) / mean
margin_multiplier = clamp(1.0 + CV, 1.15, 1.50)
```

Where `p95`, `p50`, and `mean` are pre-computed daily digest columns (already stored — no new columns needed). This measures how far peak usage (p95) deviates from typical usage (p50), normalized by mean — a coefficient of variation that directly captures workload variability relevant to right-sizing headroom.

For containers with very stable usage (CV < 0.15): 15% margin (multiplier 1.15).
For containers with high variability (CV > 0.50): 50% margin (multiplier 1.50).

> **Why `(p95 - p50) / mean` instead of standard IQR-CV `(p75 - p25) / median`?** Standard IQR-CV measures the spread of the *middle 50%* of data — useful for general statistics but insensitive to tail behavior. Our `(p95 - p50) / mean` captures how far the *tail* is from typical usage, which is exactly what determines how much headroom a container needs above its median request. The daily digest schema stores p50, p95, and mean for both CPU and memory — no additional columns (p25, p75) are required.

### REQ-4.3: OOM exponential backoff [HIGH]

**Source:** Analysis §16

**Required behavior:** When OOM events are detected within the recommendation window:

| OOM Count | Backoff Multiplier |
|---|---|
| 1 | 1.3× |
| 2 | 1.6× |
| 3+ | 2.0× |

Applied to both request and limit recommendations:
```
memory_limit = max(memory_limit_from_percentile, last_oom_limit × backoff_multiplier)
```

### REQ-4.4: Trend detection for memory leaks [MEDIUM]

**Source:** Analysis §16

**Required behavior:** Compute linear regression slope on daily mean memory usage over the last 14 days. If slope is positive and statistically significant (R² > 0.7):
1. Project 7 days forward.
2. If projected value exceeds current recommendation, emit `WARNING_MEMORY_TRENDING_UP` notification with projected value and date.

### ~~REQ-4.5: Multi-timescale merge~~ [REMOVED]

**Status:** Removed — redundant with exponential decay (REQ-3.2). Per-day decay weighting in Go (configurable half-life per term) already handles both fast-reaction and long-term pattern capture. Two separate timescales add implementation complexity without measurable accuracy improvement. Rely solely on decay weighting with configurable half-life.

### REQ-4.6: Separate request and limit recommendations [HIGH]

**Source:** Analysis §16

**Required behavior:**
- **Request:** Based on percentile of usage (p95 cost, p100 performance) with adaptive margin.
- **Limit:** Based on `max(headroom_limit, tail_limit, oom_floor)` where:
  - `headroom_limit = request × 1.5` (configurable)
  - `tail_limit = p99(usage) × 1.1`
  - `oom_floor = last_oom_limit × backoff_multiplier` (if OOM data available)

---

## 9. Phase 5: GPU Recommendations (Weeks 10–14)

### REQ-5.1: Port GPU MIG bin-packing algorithm [HIGH]

**Source:** Analysis §17, Appendix B

**Required behavior:** Port the Kruize GPU recommendation algorithm to Go:
1. Read accelerator metrics (core usage, memory copy, frame buffer usage).
2. Determine GPU model from accelerator metadata.
3. Compute percentile of usage.
4. Select optimal MIG profile via bin-packing (smallest profile whose resources ≥ percentile usage).

### REQ-5.2: Fix B200/RTX PRO gating bug [CRITICAL]

**Source:** Analysis §17

**Current Kruize behavior:** `checkIfModelIsKruizeSupportedMIG` only checks for "A100", "H100", "H200" in model name. B200 and RTX PRO GPUs silently produce no recommendations despite profile data existing.

**Required behavior:** Support all known GPU models: A100, H100, H200, B200, RTX PRO 5000, RTX PRO 6000.

### REQ-5.3: Fix frame buffer gaps [CRITICAL]

**Source:** Analysis §17

**Current Kruize behavior:** `getFrameBufferBasedOnModel` handles 40/80/94/96/141 GB only. B200 (180 GB) and RTX PRO 5000 (48 GB) fall through, returning -1.

**Required behavior:** Add all known frame buffer sizes: 40, 48, 80, 94, 96, 141, 180 GB. Use a data-driven lookup table (not hardcoded if-else chain) for extensibility.

### REQ-5.4: GPU workload classification and underutilization detection [HIGH] — IMPLEMENTED

**Source:** Analysis §17. **Design:** [gpu-classification.md](gpu-classification.md).

**Required behavior:** Classify each GPU workload using DCGM profiling metrics (SM active, tensor pipe active, DRAM active) averaged over the recommendation term window. The system MUST assign one of six utilization classes, each with a distinct recommendation action:

| Class | Meaning | Required action |
|-------|---------|-----------------|
| `idle` | GPU allocated but essentially unused | Recommend deallocation; estimate full GPU savings |
| `underutilized` | Low compute and tensor activity | Recommend MIG partitioning or time-slicing |
| `memory_bound` | High memory bandwidth, low tensor activity | Recommend MIG profile sized to frame-buffer usage |
| `compute_bound_underutil` | Some compute activity but overall underutilized | Time-slicing candidate |
| `well_utilized` | Healthy utilization | No right-sizing action |
| `no_profiling` | Frame-buffer metrics only (Volta/Pascal tier) | FB-based MIG sizing only |

**Notifications:** Emit workload-appropriate GPU notifications (codes 10 underutilized, 26 idle, 27 memory-bound, 28 no-profiling) with dollar savings when Koku cost data is available (REQ-5.6).

**Configuration:** Classification thresholds are configurable via `ROS_GPU_*` environment variables and per-org GPU settings. Threshold defaults and decision-tree ordering: [gpu-classification.md](gpu-classification.md).

### REQ-5.5: Multi-GPU awareness (future) [LOW] — DEFERRED

**Source:** Analysis §17

**Required behavior (deferred):** Detect containers with multiple GPUs via DCGM device count metric. Current algorithm assumes 1 GPU per container. A 4-GPU container at 25% each looks like 1 GPU at 25%, potentially recommending a single small MIG partition instead of "you need 4 GPUs."

### REQ-5.6: Leverage Koku MIG cost data for cost-aware GPU recommendations [MEDIUM] — IMPLEMENTED

**Source:** March 2026 triage — Koku `main` now has MIG GPU cost support

**Context:** Koku has landed MIG-aware cost accounting on the on-prem/self-hosted SQL path (migration `0344`). New fields: `gpu_mode`, `mig_profile`, `mig_slice_count`, `mig_memory_capacity_gb`, `mig_strategy`, `gpu_max_slices`. New API endpoints: `reports/openshift/gpu/` and `reports/openshift/gpu/mig_profiles/`. Monthly GPU cost uses `slices / gpu_max_slices` weighting; unallocated GPU cost distribution uses slice-hours. The operator branch `cost-7178-mig-metrics` adds `mig_instance_id`, `mig_profile`, `mig_slice_count`, `gpu_max_slices` to the cost GPU CSV (not yet merged to `main`).

**Required behavior:** When Koku's GPU cost data is available (via API or shared database), GPU recommendations should include **cost context**:
1. Include estimated monthly cost of the current GPU allocation (full GPU or MIG slice) from Koku data.
2. Include estimated monthly cost of the recommended MIG profile.
3. For underutilization notifications (REQ-5.4), include actual dollar savings: `current_mig_cost - recommended_mig_cost` or `current_gpu_cost` if recommending removal.
4. This cross-references ROS optimization data with Koku cost data — alignment on `cluster_id`, `namespace`, `node`, and `gpu_uuid` / `mig_instance_id`.

**Note — Koku MIG dual-path gap (verified 2026-04-05):** MIG data reaches the Parquet/Hive layer (the post-processor parses `mig_profile`, derives `mig_slice_count`, `gpu_max_slices`, `mig_memory_capacity_mib`), and the `self_hosted_sql/` on-prem path has full MIG support. However, **two Trino SQL templates do not propagate MIG columns** in SaaS: (1) `trino_sql/openshift/cost_model/monthly_cost_gpu.sql` constructs `all_labels` with only `gpu-model`, `gpu-vendor`, `gpu-memory-mib` — MIG labels (`gpu-mode`, `mig-profile`, `mig-slice-count`, `gpu-max-slices`, `mig-strategy`, `mig-memory-mib`) are omitted; (2) `trino_sql/openshift/ui_summary/reporting_ocp_gpu_summary_p_usage_only.sql` reads from the Hive table but does not select MIG columns. The shared PostgreSQL UI summary (`sql/openshift/ui_summary/reporting_ocp_gpu_summary_p.sql`) extracts MIG from `all_labels`, but in SaaS those labels are empty because the Trino cost model SQL didn't include them. Result: `OCPGpuSummaryP` MIG columns are NULL in SaaS. The model, API endpoints (`reports/openshift/gpu/`, `mig_profiles/`), and UI (feature flag `cost-management.koku-ui-hccm.mig`) are all wired and ready — only the two Trino SQL templates need updating. Additionally, `PriceList` / `PriceListCostModelMap` models exist in `cost_models/models.py` but have no API endpoints yet. The operator MIG branch (`cost-7178-mig-metrics`) has not been merged to `main`.

### REQ-5.7: GPU time-slicing recommendations [HIGH] — IMPLEMENTED

**Source:** Implementation sprint 2026-05-03

**Context:** Extends the per-container GPU MIG bin-packing (REQ-5.1) with **node-level time-slicing guidance**. When MIG partitioning is not supported or not cost-effective, NVIDIA time-slicing via `nvidia.com/gpu.replicas` can consolidate multiple low-utilization containers onto fewer physical GPUs.

**Required behavior:**
1. Group GPU containers by node and GPU model from `gpu_container_digests`.
2. Partition containers into time-slicing candidates (SM utilization < threshold, e.g. 30%) and impacted (non-candidate containers sharing the same GPU node).
3. Compute recommended replica count from average candidate SM, DRAM, and frame-buffer utilization.
4. Compute confidence score from average candidate utilization confidence and ratio of candidates to total containers.
5. Compute per-GPU and total estimated monthly dollar savings from Koku cost data (if available).
6. Expose via **`GET /recommendations/openshift/gpu/timeslicing`** API endpoint with pagination and RBAC (canonical GPU time-slicing path). **`GET /recommendations/openshift/gpu`** returns aggregate GPU listing counts and links. **`GET /recommendations/openshift/gpu/mig`** lists containers with non-`full_gpu` MIG recommendations.
7. Cross-reference at container level: `time_slicing_node` and `time_slicing_replicas` fields in the container recommendation detail response.
8. Per-container `estimated_monthly_timeslicing_savings_usd` field.

**Known limitation — MIG + time-slicing combined:** The engine treats MIG partition recommendations and time-slicing as mutually exclusive (`partitionContainers` excludes MIG workloads from time-slicing candidates). NVIDIA hardware can combine MIG instances with time-slicing on each instance; modeling that interaction is **not implemented** and is a deferred enhancement (see `docs/known-issues.md`).

**Code:** `internal/engine/gpu_timeslicing.go` — `ComputeNodeTimeslicingRec()`, `partitionContainers()`, `computeReplicas()`.

### GPU deferred items (beyond REQ-5.5)

Documented in [known-issues.md § GPU: Deferred / Future Work](../known-issues.md#gpu-deferred-future-work). Summary:

| # | Item | Consumer | Why deferred |
|---|------|----------|--------------|
| 1 | **Node GPU count** (`node_gpu_count` from node allocatable `nvidia.com/gpu`) | Node GPU savings; Tier 2 MachineSet GPU-aware consolidation | Informational-only until Tier 2 MachineSet + GPU-aware node consolidation engine exist |
| 2 | **Multi-GPU container consolidation** (REQ-5.5 / F25) | ML training pods with 4–8 GPU requests and partial utilization | Niche (<5% of GPU workloads); per-device UUID collection not in operator; 1-GPU/container covers inference |
| 3 | **MIG list SQL pagination** (`GET .../gpu/mig`) | 10k+ GPU container fleets | In-memory filter/sort/paginate is <50ms today; SQL page keys or materialized MIG table deferred until scale demands it |

---

## 10. Phase 6: New Recommendation Types — Tier 1 (Weeks 8–16)

These require zero or minimal new operator queries and have the highest impact.

### REQ-6.1: Idle workload detection [HIGH]

**Source:** Analysis §23.1

**Required:** Zero new Prometheus queries — uses existing `cpu_usage_container_avg/max` and `memory_usage_container_avg/max` data.

**Algorithm:**
1. Compute `cpu_utilization = avg(cpu_usage) / avg(cpu_request)` over recommendation window.
2. If `cpu_utilization < 0.01` (1%) AND `memory_utilization < 0.01`:
   - Classify as **idle**.
   - Emit `IDLE_WORKLOAD` (code 5) with estimated savings: `sum(cpu_request + memory_request) × unit_cost`.
3. If `cpu_usage == 0` AND `memory_usage == 0` for all intervals:
   - Classify as **abandoned**.
   - Emit `WARNING_WORKLOAD_ABANDONED`.
4. Configurable threshold (default 1% of request).

### ~~REQ-6.2: QoS class recommendations~~ [DEFERRED — implicit from CPU/memory recs]

**Status:** Demoted/deferred — QoS class is entirely determined by the relationship between request and limit values, which our CPU and memory recommendations already produce. Explicitly recommending a QoS class adds noise without adding value: if we recommend `request == limit`, the user gets Guaranteed implicitly. If we recommend `request < limit`, the user gets Burstable implicitly. A separate "QoS recommendation" restates what the CPU/memory recommendations already say. Revisit only if user research shows demand for explicit QoS guidance.

### REQ-6.3: PVC right-sizing [HIGH]

**Source:** Analysis §23.2

**Required:** Zero new queries if existing `cost:` PVC queries are routed to ROS pipeline. One optional new query for inode usage.

**Algorithm:**
1. Compare `persistentvolumeclaim_usage_bytes` vs `persistentvolumeclaim_capacity_bytes`.
2. If `usage / capacity < 0.20` sustained: recommend smaller PVC.
3. If `usage / capacity > 0.85` sustained: warn about capacity risk.
4. If `usage == 0` for all intervals: flag as orphaned PVC.
5. Compute growth trend (linear regression on daily usage means) and project time to capacity exhaustion.

**No operator change required.** The operator already scrapes `cost:persistentvolumeclaim_capacity_bytes`, `cost:persistentvolumeclaim_request_bytes`, and `cost:persistentvolumeclaim_usage_bytes` and writes them to `cm-openshift-storage-usage-YYYYMM.csv` (listed in `manifest.files` for Koku). ros-ocp-backend reads this existing CSV directly — no data duplication needed:
- **Both SaaS and on-prem:** Koku's `ROSReportShipper` extracts the storage CSV from the tarball, uploads it to S3, and includes its path in the Kafka message to `hccm.ros.events`. ros-ocp-backend reads the CSV from S3 — same flow as pod usage CSVs. The only Koku change needed is a routing update in `kafka_msg_handler.py` to include the storage CSV in the ROS Kafka message (server-side change in the `koku` repo, not an operator change).

### REQ-6.4: Go GOMAXPROCS/GOMEMLIMIT [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §23.4

**Required:** One new Prometheus query: `go_info{namespace!=""}` (standard Go Prometheus client metric).

**Algorithm:**
1. Detect Go workloads via `go_info` metric presence.
2. Read container CPU limit from existing metrics.
3. If Go version ≥ 1.19 and no `GOMEMLIMIT` set: recommend `GOMEMLIMIT = container_memory_limit × 0.9`.
4. If container CPU limit is fractional (e.g., 2.5 cores): recommend `GOMAXPROCS = ceil(cpu_limit)` and note that `uber-go/automaxprocs` library handles this automatically.
5. Estimate performance impact: "Setting GOMAXPROCS to match CPU quota can improve performance by 2–10x for CPU-bound Go workloads."

### REQ-6.5: Snapshot staleness detection [HIGH] — IMPLEMENTED

**Source:** Implementation sprint 2026-05-06

**Context:** VolumeSnapshots in OpenShift accumulate over time and can become orphaned, stale, or redundant, consuming storage without value. This feature classifies snapshot health and flags actionable findings.

**Required behavior:**
1. Ingest VolumeSnapshot inventory from the operator's `cm-openshift-snapshot-YYYYMM.csv` (existing CSV type routed to ros-ocp-backend).
2. Classify each snapshot into one of: `orphaned` (source PVC deleted), `stale` (age > configurable threshold, default 30d), `never_restored` (no restore event recorded), `redundant` (superseded by newer snapshot of same PVC), `managed` (owned by a backup tool like Velero/OADP), `active` (recently created and in use).
3. Persist classifications to `snapshot_recommendations` table.
4. Expose via `GET /recommendations/openshift/snapshots` API endpoint with pagination, filtering by classification.
5. Configurable settings via `GET|PUT /recommendations/openshift/settings/snapshot` (stale threshold, classification toggles), with env-var locking for operator-controlled deployments.
6. Notification codes: 31 (orphaned), 32 (never-used), 33 (redundant), 34 (stale), 35 (managed).
7. Reconciliation: remove stale recommendation rows for snapshots no longer in inventory.

**Code:** `internal/engine/snapshot_classify.go`, `internal/engine/snapshot_settings.go`, `internal/ingestion/snapshot.go`, `internal/api/handlers_snapshot.go`, `internal/api/handlers_snapshot_settings.go`.

### REQ-6.6: Box plots / five-number summary visualization [HIGH] — IMPLEMENTED

**Source:** Implementation sprint 2026-04-28

**Context:** Provides visual context for why a recommendation was chosen. Users can see the distribution of their workload's CPU/memory usage over the recommendation window, overlaid with the recommended request/limit values.

**Required behavior:**
1. For each container or namespace recommendation, compute a five-number summary (min, Q1, median, Q3, max) from daily digest percentile data over the active term window.
2. Expose box plot data in the detail API response for both containers (`/recommendations/openshift/:id`) and namespaces (`/recommendations/openshift/namespace/:id`).
3. Support per-term box plots (short/medium/long term windows produce different distributions).
4. Respect customer-defined term windows from `org_recommendation_terms`.

**Code:** `internal/model/boxplot.go` — `AssembleBoxplots()`, `AssembleNamespaceBoxplots()`.

---

## 11. Phase 7: Replica Count and Total Impact (Weeks 10–14)

### REQ-7.1: Collect desired replica counts [HIGH] — IMPLEMENTED

**Source:** Analysis §26

**Operator changes:** Two unified PromQL queries (`ros:desired_replicas`, `ros:available_replicas`) in `koku-metrics-operator/internal/collector/queries.go`. Each query:
1. Computes workload-level replica count by unifying deployment/statefulset/daemonset metrics via `label_replace`
2. Filters by ROS namespace opt-in labels (`insights_cost_management_optimizations` or `cost_management_optimizations`)
3. Broadcasts to per-pod container rows via join on `kube_pod_container_info` + `namespace_workload_pod:kube_pod_owner:relabel` recording rule

**Critical implementation note:** The pod-info side (many-per-workload) must be the LEFT operand with `group_left()`, and the replica-count side (one-per-workload) must be the RIGHT operand. Using the reverse order causes "many-to-many matching not allowed" errors with multi-replica workloads. This was caught and fixed during live Prometheus validation on an SNO cluster.

**Validated against:** Real Thanos Querier on SNO cluster (OpenShift). Both queries returned 27 correct results for the `cost-onprem` namespace, matching `oc get deployment` output exactly. The `namespace_workload_pod:kube_pod_owner:relabel` recording rule is a standard OpenShift monitoring recording rule (confirmed present).

New CSV columns: `desired_replicas`, `available_replicas` (per workload row).

**Code:** `koku-metrics-operator/internal/collector/queries.go` (PromQL), `types.go` (CSV struct), `test_files/test_data/desired-replicas` and `available-replicas` (test data).

### REQ-7.2: Store and expose replica count [HIGH]

**Source:** Analysis §26

**Database change:** Add `desired_replicas` and `available_replicas` columns to `workloads` table (or a new `workload_replica_history` table for time-series replica data).

**API change:** Include in recommendation response:
```json
{
  "replicas": {
    "desired": 5,
    "available": 5
  }
}
```

### REQ-7.3: Compute and expose total savings [HIGH]

**Source:** Analysis §26

**API change:** For each resource recommendation, compute and include:
```json
{
  "cpu": {
    "current_request": { "amount": 500, "format": "millicores" },
    "recommended_request": { "amount": 200, "format": "millicores" },
    "per_container_savings": { "amount": 300, "format": "millicores" },
    "total_savings": { "amount": 1500, "format": "millicores" }
  }
}
```

Where `total_savings = per_container_savings × desired_replicas`.

### REQ-7.4: Fallback replica count from pod data [MEDIUM]

**Source:** Analysis §26

**Required behavior:** If operator does not provide replica count (older operator version), derive approximate count from distinct pods per (namespace, workload, container) in the CSV data. Mark as `"replica_source": "derived"` in the API response to indicate reduced accuracy.

### REQ-7.5: Koku cost data integration for dollar savings [HIGH]

**Source:** Risk review Q8

**Required behavior:** Integrate with Koku's cost model API to convert resource savings into estimated dollar savings.

**Architecture:** New Go module `internal/costdata/` that:
1. Queries Koku's `/api/cost-management/v1/cost-models/` REST API filtered by provider UUID (cluster).
2. Caches cost rates per cluster_uuid in memory (refresh interval: `ROS_COST_CACHE_TTL`, default 1 hour).
3. Cost model data is small (~1 KB per cluster) and changes rarely — hourly cache refresh is sufficient.

**Cost rate extraction:**
- CPU cost per millicore-hour: from `cpu_core_usage_per_hour` rate (sum of Infrastructure + Supplementary rates)
- Memory cost per KiB-hour: from `memory_gb_usage_per_hour` rate, converted to KiB (`rate_per_gb / (1024 × 1024)`)
- GPU cost per GPU-hour: from Koku's MIG cost data (REQ-5.6)
- Markup percentage: from the cost model's `markup.value` field

**Precision notes:**
- Koku's cost model rates include direct Infrastructure and Supplementary rates per resource-hour, plus a markup percentage. These are the **actionable** cost numbers that cluster admins control.
- Koku also applies **distributed costs** (platform overhead, worker unallocated, storage/network/GPU overhead) proportionally by usage to user projects. Reducing a container's usage by X% also reduces its share of distributed costs by ~X%, but this indirect effect is not captured in the direct rate calculation. For maximum accuracy, the savings estimate uses `rate × (1 + markup_pct/100)` which accounts for the direct cost + markup. The distributed cost savings are a secondary benefit that the customer will see in their cost reports after applying the recommendation.
- If Koku adds a "blended cost per millicore-hour" API in the future (including distributed overhead), use that for even higher precision.

**Dollar savings computation:**
```
cpu_savings_mc = (current_request - rec_request)
mem_savings_kib = (current_request - rec_request)
monthly_hours = 730
markup_factor = 1 + (markup_pct / 100)
cpu_dollar_savings = cpu_savings_mc × cpu_rate_per_mc_hour × markup_factor × monthly_hours × replicas
mem_dollar_savings = mem_savings_kib × mem_rate_per_kib_hour × markup_factor × monthly_hours × replicas
total_monthly_savings_usd = cpu_dollar_savings + mem_dollar_savings
```

**Storage:** `estimated_savings_cents` is stored as a REAL column in `recommendation_sets` (see REQ-2.5). It is recomputed on each recommendation cycle and also refreshed when the cost rate cache is updated. This means savings values stay current even if the customer changes their cost model rates.

**API change:** Add `estimated_savings_cents` to recommendation response (per container and in summary endpoint). The `/summary` endpoint aggregates total savings across all workloads in a cluster.

**Graceful degradation:** If Koku API is unreachable or `ROS_ENABLE_COST_INTEGRATION=false`, dollar savings fields are `null` — resource-based savings (millicores, KiB) are always present.

**Authentication:** Use the same `x-rh-identity` header (from the Kafka message metadata or service account).

### REQ-7.6: Fleet-level summary [HIGH] — IMPLEMENTED

**Source:** Implementation sprint 2026-05-04

**Context:** Provides a cross-cluster aggregated view of optimization status for an entire organization. Executives and platform teams need a single endpoint to assess total savings potential and adoption progress.

**Required behavior:**
1. Aggregate recommendations across all clusters for a given `org_id`.
2. Return: total estimated monthly savings (USD), total workloads with recommendations, adoption rate (% of recs applied), top opportunities (highest savings workloads).
3. Expose via `GET /recommendations/openshift/fleet-summary` API endpoint.
4. RBAC-filtered: only includes clusters the requesting user has access to.

**Code:** `internal/api/handlers_fleet.go` — `GetFleetSummary()`.

---

## 12. Phase 8: New Recommendation Types — Tier 2 (Weeks 14–20)

### REQ-8.1: HPA optimization [HIGH] — DEFERRED

**Status:** Planned future work — not implemented. Notification codes **21** (`HPA_SATURATED`) and **22** (`HPA_ACTIVE`) are reserved. See [Deferred: HPA and VPA autoscaling](#deferred-hpa-and-vpa-autoscaling).

**Source:** Analysis §23.3

**Required:** 8 new Prometheus queries:
```promql
kube_horizontalpodautoscaler_spec_min_replicas
kube_horizontalpodautoscaler_spec_max_replicas
kube_horizontalpodautoscaler_spec_target_metric
kube_horizontalpodautoscaler_status_current_replicas
kube_horizontalpodautoscaler_status_desired_replicas
kube_horizontalpodautoscaler_status_condition{condition="ScalingLimited",status="true"}
kube_horizontalpodautoscaler_status_target_metric
kube_horizontalpodautoscaler_labels
```

**Algorithm:**
1. If `current_replicas == max_replicas` sustained (>80% of window): recommend increasing `maxReplicas` (HPA is saturated).
2. If `current_replicas == min_replicas` sustained (>95% of window): recommend decreasing `minReplicas` (HPA never scales up).
3. If scaling limited condition is frequently true: diagnose bottleneck.
4. If VPA recommendation × min_replicas > current total: suggest coordinated VPA+HPA adjustment.
5. Detect HPA flapping: >10 scale events per hour.

### Deferred: HPA and VPA autoscaling

**Status:** Explicitly **deferred** — requirements remain in this document; implementation is planned but not built. Do not treat as shipping functionality.

| Domain | Requirement | Planned plugin | Phase | Notification codes | Operator data |
|--------|-------------|----------------|-------|-------------------|---------------|
| **HPA** | REQ-8.1 | `hpa` (Phase 2 Enrich) | 8 | **21** `HPA_SATURATED`, **22** `HPA_ACTIVE` | 8 new HPA Prometheus queries (see REQ-8.1) |
| **VPA** | (policy recommendations; see [plugin-phases.md](plugin-phases.md)) | `vpa` (Phase 2 Enrich) | 8 | TBD at implementation | `kube_verticalpodautoscaler_*` metrics (operator TBD) |

**HPA scope (when implemented):** Detect HPA-managed workloads; emit informational notifications for saturation (code 21) and active HPA management (code 22, suppressing replica-count advice per OQ#9). Per REQ-8.1 algorithm: min/max replica tuning, scaling-limited diagnosis, flapping detection. Combined VPA+HPA coordinated optimization remains **deferred** until upstream in-place pod vertical scaling stabilizes (OQ#9).

**VPA scope (when implemented):** Advisory VPA `updateMode` and resource-policy recommendations derived from container CPU/memory rightsizing output. Does not replace container plugin sizing — enriches it for workloads with an active VPA CR.

**Architecture:** Both are planned as **separate Phase 2 Enrich plugins** that depend on container recommendations, not new CSV ingestors. See [plugin-phases.md](plugin-phases.md) and [plugin-architecture.md](plugin-architecture.md).

**Deployment and automation:** HPA/VPA recommendations are advisory in all shipped modes (SaaS, on-prem, fleet). ROS does not ship an in-product actuator; customers may automate apply today via external tools (Ansible, SonataFlow, GitOps, CronJobs) that read the REST API, with safety gates (PDB, maintenance windows, canary, confidence, rollback). VPA `updateMode: Off` enables dual-advisor validation against ROS container rightsizing without auto-apply. See [hpa-vpa-deployment-modes.md](hpa-vpa-deployment-modes.md).

### REQ-8.2: Ephemeral storage recommendations [LOW — informational only, pending upstream fix] — NOT IMPLEMENTED

**Source:** Analysis §23.6

**Required:** 4 new Prometheus queries for ephemeral storage requests, limits, and usage.

**Algorithm:** Same pattern as memory — compare usage vs request/limit, recommend right-sized values.

**Scope:** Gated behind `ROS_ENABLE_EPHEMERAL_STORAGE=false` (OFF by default). **Fundamental limitation:** As of OpenShift 4.21 (the latest release, March 2026, based on Kubernetes 1.34), there is no reliable pod-level ephemeral storage usage metric. The `container_fs_usage_bytes` metric has documented unreliability with containerd runtimes (cAdvisor issue [#2785](https://github.com/google/cadvisor/issues/2785), still open). Red Hat KB [#6993297](https://access.redhat.com/solutions/6993297) confirms no dedicated metric exists for per-pod ephemeral storage consumption. This is NOT version-gated — it's a fundamental cadvisor/containerd gap that persists across all current OCP versions. Recommendations based on unreliable metrics could cause pod evictions. Keep OFF by default; re-evaluate when upstream cadvisor fixes land. When enabled, treat output as informational only — never auto-apply.

### REQ-8.3: Node.js heap recommendations [LOW — informational only] — NOT IMPLEMENTED

**Source:** Analysis §23.7

**Required:** 1 new query: `nodejs_version_info` (if available — many Node.js apps don't expose this metric).

**Algorithm:** Purely informational — the operator cannot reliably detect `--max-old-space-size` from Prometheus metrics alone (it's a process argument, not a metric). The recommendation is a **generic best-practice notification**, not a data-driven recommendation:
1. Detect Node.js workload via `nodejs_version_info` metric (if exposed).
2. Emit informational notification: "Node.js detected. Recommend setting `--max-old-space-size` to 75% of container memory limit to prevent V8 OOM."
3. No actionable numeric recommendation — this is advisory only.

**Note:** This is the weakest recommendation type in the system. Gate behind `ROS_ENABLE_NODEJS_RECS=false` (OFF by default).

### REQ-8.4: ResourceQuota recommendations [MEDIUM] — IMPLEMENTED (namespace ResourceQuota)

**Source:** Analysis §23.8. **Design:** [quota-recommendations.md](../features/quota-recommendations.md).

**Shipped:** Phase 1 `quota` plugin (priority 35). Ingests hard/used limits from ROS namespace
CSV into `daily_namespace_digests`; compares against `recommendation_sets` container sums;
persists `quota_recommendation_sets`; exposes `GET /api/cost-management/v1/recommendations/openshift/quota/`.

**Algorithm (implemented):**
1. Sum container `term=medium` / `engine=cost` request/limit recommendations per namespace.
2. Utilization = max(quota used, container sums) vs hard limits (per resource).
3. `tighten` when recommended hard &lt; current hard; `raise` when utilization ≥ high-risk threshold (default 90%, configurable via settings/env).
4. Risk bands: high ≥ 90%, medium ≥ 70%, low otherwise (defaults; overridable per org).

**Future (namespace quota):** Storage/pod quota resources, per-quota object identity (multiple ResourceQuotas per namespace).

### REQ-8.4b: ClusterResourceQuota recommendations [MEDIUM] — IMPLEMENTED

**Source:** Analysis §23.8, extension of REQ-8.4. **Design:** [cluster-resource-quota.md](../features/cluster-resource-quota.md).

**Shipped:** Phase 1 `cluster-quota` plugin (priority 36). Operator emits
`ros-openshift-cluster-quota-*.csv`; ROS ingests `daily_cluster_quota_digests`, runs
`RunClusterQuotaRecommendations`, persists `cluster_quota_recommendation_sets`; exposes
`GET /api/cost-management/v1/recommendations/openshift/cluster-quota/` and
`GET/PUT/DELETE .../settings/cluster-quota`.

**Algorithm (implemented):**

1. Load latest CRQ hard/used per `cluster_quota_name` from digests.
2. Aggregate namespace `quota_recommendation_sets` cluster-wide for v1 recommended-hard sums.
3. Reuse namespace quota classification (`tighten` / `raise` / `optimal` / `none`, risk bands).
4. Utilization: max(CRQ used, recommended sums) vs CRQ hard per resource.
5. Optional monthly savings on `tighten` when cost integration is enabled.

**Backward compatibility:** Clusters without CRQs produce zero rows without error.

**Still future (v1 gaps):** Per-CRQ namespace membership for recommended-hard sums; selector labels
in API; storage/pod/object-count CRQ resources; overlapping CRQ deduplication in fleet totals.

---

## 12b. Phase 8b: VM Recommendations (Weeks 12–18)

This phase adds right-sizing recommendations for OpenShift Virtualization virtual machines — a completely new recommendation category. It spans the operator (new Prometheus queries), the metrics pipeline (new daily digest table), and the recommendation engine (Go `recommendVM()` integrated into `recommendAllWorkloads()`). Phase 8b can be developed in parallel with Phases 8 and 9.

**Source:** Analysis §30

**Why:** VM sprawl (idle/oversized VMs) is the #1 cost problem in virtualization environments (Flexera, Densify report 20-40% of VMs are idle or oversized). OpenShift Virtualization adoption is growing rapidly, and current ROS has zero VM awareness.

### REQ-8b.1: Operator — VM ROS Prometheus queries [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §30.4

**Required behavior:** Add ~12 new `ros:vm_*`-prefixed Prometheus queries to `koku-metrics-operator`:

| Query | Prometheus Series | Purpose |
|---|---|---|
| `ros:vm_cpu_usage_cores` | `rate(kubevirt_vmi_cpu_usage_seconds_total[5m])` | CPU utilization |
| `ros:vm_cpu_request_cores` | `kubevirt_vm_resource_requests{resource='cpu'}` | Current CPU allocation |
| `ros:vm_cpu_limit_cores` | `kubevirt_vm_resource_limits{resource='cpu'}` | Current CPU limit |
| `ros:vm_memory_usage_bytes` | `kubevirt_vmi_memory_used_bytes` | Memory utilization |
| `ros:vm_memory_available_bytes` | `kubevirt_vmi_memory_available_bytes` | Memory headroom |
| `ros:vm_memory_request_bytes` | `kubevirt_vm_resource_requests{resource='memory'}` | Current memory allocation |
| `ros:vm_disk_read_iops` | `rate(kubevirt_vmi_storage_iops_read_total[5m])` | **NEW:** Disk read IOPS |
| `ros:vm_disk_write_iops` | `rate(kubevirt_vmi_storage_iops_write_total[5m])` | **NEW:** Disk write IOPS |
| `ros:vm_disk_read_bytes_per_sec` | `rate(kubevirt_vmi_storage_read_traffic_bytes_total[5m])` | **NEW:** Disk read throughput |
| `ros:vm_disk_write_bytes_per_sec` | `rate(kubevirt_vmi_storage_write_traffic_bytes_total[5m])` | **NEW:** Disk write throughput |
| `ros:vm_disk_allocated_bytes` | `kubevirt_vm_disk_allocated_size_bytes` | Current disk allocation |
| `ros:vm_info` | `kubevirt_vmi_info{phase='running'}` | VM metadata (OS, instance_type) |

All queries MUST filter by `kubevirt_vmi_info{phase='running'}` to exclude stopped VMs.

**Output CSV:** `ros-openshift-vm-usage-<YYYYMM>.csv` with columns for all metrics above, plus `vm_name`, `namespace`, `node`, `vm_instance_type`, `vm_os`, `guest_os_name`, `guest_os_arch`.

**Future optimization (not in scope):** The operator currently also collects ~11 `cost:vm_*` queries at 60-min granularity for Koku cost calculations (`cm-openshift-vm-usage-YYYYMM.csv`). There is significant overlap — 7 of the 12 `ros:vm_*` queries scrape the same Prometheus series. A future cross-repo optimization could unify VM data collection: the operator collects once at 15-min (ROS granularity), and Koku aggregates to hourly during ingestion, eliminating the duplicate `cost:vm_*` queries. This requires coordinated changes in koku-metrics-operator, koku, and ros-ocp-backend, so it is deferred to post-MVP.

### REQ-8b.2: VM daily digest table [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §30.6

**Required behavior:** Create `daily_vm_digests` partitioned table (§18). VM CSV data is parsed in Go, aggregated in memory (exact percentiles via `slices.Sort()`), and upserted into `daily_vm_digests` — same pattern as container digests. No raw VM metric readings are stored in PostgreSQL.

**CSV columns parsed:** `ts`, `org_id`, `cluster_uuid`, `namespace`, `vm_name`, `node`, `cpu_usage_mc` (BIGINT), `cpu_request_mc` (BIGINT), `cpu_limit_mc` (BIGINT), `mem_usage_kib` (BIGINT), `mem_available_kib` (BIGINT), `mem_request_kib` (BIGINT), `mem_limit_kib` (BIGINT), `disk_read_iops` (BIGINT), `disk_write_iops` (BIGINT), `disk_read_bytes_sec` (BIGINT), `disk_write_bytes_sec` (BIGINT), `disk_allocated_bytes` (BIGINT), `vm_instance_type` (TEXT), `vm_os` (TEXT), `guest_os_name` (TEXT), `guest_os_arch` (TEXT).

**Daily digest table:** Daily pre-computed percentiles for CPU, memory, disk IOPS, disk throughput, plus MAX for disk allocation and requests.

**Integer types:** All numeric metrics stored as BIGINT — `int64` end-to-end, consistent with the container pipeline (see REQ-2.3).

### REQ-8b.3: CSV ingestion — VM parser [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §30.9

**Required behavior:**
1. Detect VM CSV files by filename pattern `ros-openshift-vm-usage-*.csv`.
2. Parse VM-specific columns; convert CPU cores to millicores, memory bytes to KiB.
3. Compute daily digests in Go memory and upsert into `daily_vm_digests`.
4. Do NOT route VM CSVs through the container pipeline (separate parser, separate table).

### REQ-8b.4: Go — `recommendVM()` [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §30.7

**Required behavior:** Implement `recommendVM()` in Go using the same "read once, compute N terms" pattern as containers: batch-read `daily_vm_digests` for the cluster and window, compute recommendations in memory, then persist to `vm_recommendations` via `INSERT ... ON CONFLICT DO UPDATE`.

1. **CPU recommendation:** p95 with adaptive margin, rounded UP to whole vCPUs (minimum 1 vCPU).
2. **Memory recommendation:** p95 with 20% minimum margin, rounded UP to whole GiB (minimum 1 GiB). Guest OS-aware baseline: Windows 2 GiB, Linux 0.5 GiB.
3. **Disk size recommendation:** MAX usage + 30-day growth projection (linear regression on daily usage means in Go), 25% headroom, rounded to nearest 10 GiB.
4. **Disk IOPS (informational):** p95 read + write IOPS and throughput.
5. **Idle VM detection:** `cpu_p95 < 50mc AND mem_p95 < 512 MiB` → flag as idle.
6. **Hysteresis:** Only recommend downsizing if ≥40% oversized (VM restart cost).

**Output:** VM name, current/recommended vCPUs, current/recommended GiB memory, disk size recommendation, IOPS profile, idle/oversized flags.

### REQ-8b.5: Batch entry point — extend `recommendAllWorkloads()` [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §30.8

**Required behavior:** Extend `recommendAllWorkloads()` to add the VM recommendation step after container recommendations. VM recommendations stored in `vm_recommendations` table via `INSERT ... ON CONFLICT DO UPDATE`.

### REQ-8b.6: API — VM recommendation endpoint [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §30.9

**Required behavior:**
1. Add `vm_recommendations` section to the recommendation API response.
2. Support filtering by `vm_name`, `namespace`, `cluster_uuid`.
3. Return structured response: `current` (vCPUs, GiB, disk), `recommended` (vCPUs, GiB, disk), `iops_profile` (read/write p95), `flags` (idle, oversized, abandoned).
4. Support the same timeframe parameters as container recommendations.

### REQ-8b.7: ros-ocp-backend — VM workload type [MEDIUM] — NOT IMPLEMENTED

**Source:** Analysis §30

**Required behavior:**
1. Add `VirtualMachine` to the `WorkloadType` enum.
2. Update `aggregator.go` whitelist to accept VM workload type.
3. Route VM data to the VM-specific parser and digest table (not the container pipeline).

### REQ-8b.8: Operator — VM detection heuristic [LOW] — NOT IMPLEMENTED

**Source:** Analysis §30

**Required behavior:** VMs MUST be identified by the presence of `kubevirt_vmi_info{phase='running'}` joining on `name` and `namespace`. Do NOT rely on pod labels — use the KubeVirt-native metric directly.

### REQ-8b.9: Instance type recommendation (optional, Phase 2) [LOW] — NOT IMPLEMENTED

**Source:** Analysis §30.5

**Required behavior:** If `VirtualMachineInstancetype` resources are available in the cluster, recommend the smallest-fit instance type for each VM. Requires 1 new operator query: `kubevirt_vm_instance_type_info`.

---

## 12c. Phase 8c: Node & MachineSet Recommendations (Weeks 14–20)

**Operator dependency:** Phase 8c requires the new operator to ship 2 new `ros:node_requests_*` queries (REQ-8c.2b) and 3–5 MachineSet queries (REQ-8c.4). The operator changes must be completed and deployed BEFORE Phase 8c backend work can be validated end-to-end. Plan operator changes in weeks 12–14 (overlap with Phase 8 completion) so that node CSV data is available by week 14 when backend Phase 8c development starts. Without the operator changes, Phase 8c development can proceed using mock CSV data for unit/integration testing, but end-to-end validation requires real operator data.

This phase adds infrastructure-level right-sizing recommendations — the counterpart to container-level recommendations. While container recs tell developers "your pod is oversized," node/MachineSet recs tell cluster admins "your cluster has excess capacity" or "your instance types are wrong for your workload mix." Industry data (Flexera, Densify) shows 20-40% of cloud compute is wasted at the node/VM layer — often more than at the container layer. Phase 8c can be developed in parallel with Phases 8, 8b, and 9.

**Key design decision:** In OpenShift, the actionable unit for node management is the **MachineSet** (a group of identical machines), not the individual node. Cluster admins change instance types, replica counts, and autoscaler bounds at the MachineSet level. Individual node recommendations are informational; MachineSet recommendations are actionable.

**Source:** Industry analysis — Kubecost, Cast AI, Spot.io, Densify all provide node-level recommendations as a core FinOps feature.

### Tier Structure

| Tier | Scope | New Queries | Effort | Value |
|---|---|---|---|---|
| Tier 1 | Node utilization visibility | 0 (uses existing `cost:node_*`) | Low | High |
| Tier 2 | MachineSet right-sizing | 3–5 | Moderate | Very High |
| Tier 3 | MachineAutoscaler optimization | 2–3 | Moderate | High (cloud only) |

### Cloud vs On-prem Applicability

| | Cloud OpenShift (ROSA/ARO/OCP-on-cloud) | On-prem OpenShift (bare metal) |
|---|---|---|
| **Node utilization (Tier 1)** | Actionable — informs scale-down decisions | Actionable — capacity planning |
| **MachineSet right-sizing (Tier 2)** | Actionable — change instance type / replica count | Informational — "you'd need X if re-provisioning" |
| **MachineAutoscaler (Tier 3)** | Actionable — adjust min/max bounds | N/A (no cloud autoscaler) |

### REQ-8c.1: Operator — Node ROS Prometheus queries [HIGH]

**Required behavior:** Most node metrics are already collected by the operator via `cost:node_*` queries. Route the following existing metrics to the ROS pipeline (or add `ros:` aliases):

| Query | Prometheus Series | Status |
|---|---|---|
| `ros:node_cpu_allocatable` | `kube_node_status_allocatable{resource="cpu"}` | Reuse existing `cost:` query |
| `ros:node_memory_allocatable` | `kube_node_status_allocatable{resource="memory"}` | Reuse existing `cost:` query |
| `ros:node_cpu_capacity` | `kube_node_status_capacity{resource="cpu"}` | Reuse existing `cost:` query |
| `ros:node_memory_capacity` | `kube_node_status_capacity{resource="memory"}` | Reuse existing `cost:` query |
| `ros:node_cpu_usage` | `instance:node_cpu_utilisation:rate5m` or `node_cpu_seconds_total` | Reuse existing |
| `ros:node_memory_usage` | `node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes` | Reuse existing |
| `ros:node_pod_count` | `kubelet_running_pods` or `kube_node_status_allocatable{resource="pods"}` | Reuse existing |
| `ros:node_labels` | `kube_node_labels` | **NEW** — needed for MachineSet mapping and instance type |

**New CSV:** `ros-openshift-node-usage-<YYYYMM>.csv` with columns: `timestamp`, `node`, `cpu_allocatable_mc`, `cpu_capacity_mc`, `cpu_usage_mc`, `memory_allocatable_kib`, `memory_capacity_kib`, `memory_usage_kib`, `pod_count`, `pod_capacity`, `instance_type`, `machineset_name`, `region`, `zone`.

**Node-to-MachineSet mapping:** Extracted from `kube_node_labels` via the `label_machine_openshift_io_machine_set` label. If the label is absent (bare-metal, manually provisioned), `machineset_name` is `NULL` and Tier 2/3 recommendations are skipped for that node.

**Instance type extraction:** From `kube_node_labels` via the `label_node_kubernetes_io_instance_type` label (e.g., `m5.4xlarge`, `Standard_D8s_v3`, `n2-standard-8`).

### REQ-8c.2: Node daily digest table [HIGH]

**Required behavior:** Create `daily_node_digests` partitioned table (see §18). Node CSV data is parsed in Go, aggregated in memory (exact percentiles via `slices.Sort()`), and upserted into `daily_node_digests` — same pattern as container and VM digests. No raw node metric readings are stored in PostgreSQL.

**CSV columns parsed:** `ts`, `org_id`, `cluster_uuid`, `node`, `cpu_allocatable_mc` (BIGINT), `cpu_capacity_mc` (BIGINT), `memory_allocatable_kib` (BIGINT), `memory_capacity_kib` (BIGINT), `cpu_usage_mc` (BIGINT), `memory_usage_kib` (BIGINT), `pod_count` (BIGINT), `pod_capacity` (BIGINT), `cpu_requests_sum_mc` (BIGINT), `memory_requests_sum_kib` (BIGINT), `instance_type` (TEXT), `machineset_name` (TEXT), `region` (TEXT), `zone` (TEXT).

**Digest columns (pre-computed in Go):** `cpu_usage_p50_mc`, `cpu_usage_p95_mc`, `mem_usage_p50_kib`, `mem_usage_p95_kib`, `max_cpu_allocatable_mc`, `max_mem_allocatable_kib`, `max_cpu_requests_mc`, `max_mem_requests_kib`, `max_pod_count`, `instance_type`, `machineset_name`, `sample_count`.

### REQ-8c.2b: Operator — Node request sum queries [HIGH]

**Required:** 2 new Prometheus queries to populate `cpu_requests_sum_mc` and `memory_requests_sum_kib` in the node CSV:

```promql
ros:node_requests_cpu_cores:
  sum by (node) (kube_pod_container_resource_requests{resource="cpu"})

ros:node_requests_memory_bytes:
  sum by (node) (kube_pod_container_resource_requests{resource="memory"})
```

These are lightweight gauge-based queries against kube-state-metrics (no `rate()` needed). Written to node CSV alongside existing node capacity/usage data.

**Note:** Without these queries, the overcommit detection in Tier 1 (REQ-8c.3) cannot compare `sum(pod_requests)` vs `allocatable`. This is a blocking dependency for overcommit detection.

### REQ-8c.3: Tier 1 — Node utilization visibility [HIGH]

**Required:** Existing `cost:node_*` data routed to ROS, plus 2 new request-sum queries (REQ-8c.2b).

**Go function:** `recommendNodes()` — processes all nodes for a cluster in one invocation: batch-read `daily_node_digests`, compute Tier 1 signals in memory, then persist.

**Algorithm:**

1. **Underutilized node detection:**
   - `avg(cpu_usage / cpu_allocatable) < 0.30` AND `avg(memory_usage / memory_allocatable) < 0.30` sustained over 7 days
   - Emit `NODE_UNDERUTILIZED` (code 11) with estimated waste: `(1 - utilization) × allocatable_resources`
   - Configurable threshold: `ROS_NODE_UNDERUTIL_THRESHOLD` (default 0.30)

2. **Overcommitted node detection:**
   - `sum(pod_cpu_requests) > cpu_allocatable × 1.5` — CPU overcommit ratio > 150%
   - Emit `WARNING_NODE_OVERCOMMITTED` with overcommit ratio and risk level
   - Note: Moderate overcommit (100-150%) is normal and expected in Kubernetes; only flag extreme cases

3. **Stranded resources detection (EMA-smoothed imbalance):**
   - Per-day normalized imbalance: `|cpu_p95 - mem_p95| / max(cpu_p95, mem_p95)`
   - Series smoothed with EMA (alpha = `ROS_NODE_EMA_ALPHA`, default 0.3) to dampen transient spikes
   - If final smoothed imbalance exceeds `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` (default 0.6), the lower-utilized resource is flagged as stranded
   - Relative metric — works across low-utilization and high-utilization nodes without fixed absolute thresholds
   - Emit `STRANDED_RESOURCES` (code 13) with recommendation: "Consider CPU-optimized instances" or "Consider memory-optimized instances"
   - This is the highest-impact Tier 1 insight — it directly informs instance type selection

4. **Per-node utilization summary:**
   - `cpu_util_p50`, `cpu_util_p95`, `mem_util_p50`, `mem_util_p95` (from daily digest tables)
   - `pod_scheduling_headroom = (pod_capacity - max_pod_count) / pod_capacity`
   - Trend: linear regression on daily mean usage (computed in Go) — capacity planning signal

**Output:** Stored in `node_recommendations` table via `INSERT ... ON CONFLICT DO UPDATE`.

### REQ-8c.4: Operator — MachineSet Prometheus queries [HIGH] — NOT IMPLEMENTED

**Required:** 3–5 new Prometheus queries:

```promql
ros:machineset_replicas:
  max by (namespace, name) (machine_api_machine_set_status_replicas{namespace="openshift-machine-api"})

ros:machineset_available_replicas:
  max by (namespace, name) (machine_api_machine_set_status_available_replicas{namespace="openshift-machine-api"})

ros:machineset_desired_replicas:
  max by (namespace, name) (machine_api_machine_set_spec_replicas{namespace="openshift-machine-api"})
```

Optional (Tier 3):
```promql
ros:machineautoscaler_min:
  max by (namespace, name) (machine_autoscaler_min_replicas)

ros:machineautoscaler_max:
  max by (namespace, name) (machine_autoscaler_max_replicas)
```

**New CSV columns** (appended to node CSV or separate `ros-openshift-machineset-<YYYYMM>.csv`): `machineset_name`, `machineset_replicas`, `machineset_available_replicas`, `machineset_desired_replicas`, `autoscaler_min`, `autoscaler_max`.

### REQ-8c.5: Tier 2 — MachineSet right-sizing [HIGH] — NOT IMPLEMENTED

**Required:** MachineSet Prometheus queries (REQ-8c.4) + instance type catalog (REQ-8c.6).

**Algorithm (Go heuristic):** MachineSet recommendations involve cross-node aggregation and instance type catalog lookups — implemented in Go alongside Tier 1 node logic.

1. **Group nodes by MachineSet** (from `machineset_name` column in `daily_node_digests`).
2. **Aggregate utilization across all nodes in the MachineSet:**
   - `machineset_cpu_util = sum(cpu_usage across nodes) / sum(cpu_allocatable across nodes)`
   - `machineset_mem_util = sum(memory_usage) / sum(memory_allocatable)`
   - Use p95 from daily digest tables for peak utilization.
3. **Replica count recommendation:**
   - If `machineset_cpu_util_p95 < 0.50` sustained: recommend fewer replicas.
   - Formula: `rec_replicas = ceil(current_replicas × max(cpu_util_p95, mem_util_p95) / target_util)` where `target_util = 0.70` (configurable).
   - Minimum: 2 replicas for HA (configurable `ROS_MIN_MACHINESET_REPLICAS`).
4. **Instance type recommendation (cloud only):**
   - Read current instance type from node labels.
   - Compute per-node resource need: `cpu_need = p95(cpu_usage) × 1.2` (20% headroom), same for memory.
   - Lookup the smallest instance type in the catalog whose vCPUs ≥ `cpu_need` and memory ≥ `mem_need`.
   - If the recommended type differs from current AND is ≥1 size smaller: recommend the change.
   - Hysteresis: only recommend downsizing if savings ≥ 20% (avoid churn).
5. **Stranded resource fix:**
   - If Tier 1 detected stranded resources, Tier 2 recommends switching to the appropriate instance family:
     - CPU-bound + memory stranded → CPU-optimized family (e.g., `c5` instead of `m5`)
     - Memory-bound + CPU stranded → memory-optimized family (e.g., `r5` instead of `m5`)
6. **PDB awareness (notification, not algorithmic):**
   - Query `kube_poddisruptionbudget_status_pod_disruptions_allowed` (available via kube-state-metrics) to detect PDBs affecting pods on the MachineSet's nodes.
   - If PDBs are present, emit `PDB_CAVEAT` (code 4): "N PodDisruptionBudgets affect workloads on nodes in this MachineSet — manual review recommended before scaling down."
   - PDB data does NOT algorithmically change the replica count recommendation (mapping PDB → pods → nodes → MachineSets is a multi-hop join that risks incorrect recommendations). Instead, the notification alerts the operator to review manually.
   - **Known limitation:** PDB-aware replica count adjustment is deferred. PDB compliance is complex (namespace-scoped PDBs vs cluster-scoped MachineSets, multiple PDBs per node) and incorrect enforcement could prevent valid scale-down operations.

### REQ-8c.6: Instance type catalog — Cloud API integration [MEDIUM] — NOT IMPLEMENTED

**Required behavior:** Maintain a live catalog of cloud instance types for AWS, Azure, and GCP, populated via cloud APIs rather than an embedded static file. This ensures the catalog is always up-to-date and covers all instance types including custom/new ones.

**Database table:**

```sql
CREATE TABLE cloud_instance_catalog (
    provider TEXT NOT NULL,           -- 'AWS', 'Azure', 'GCP'
    instance_type TEXT NOT NULL,      -- 'm5.4xlarge', 'Standard_D4s_v3', 'n2-standard-4'
    vcpus BIGINT NOT NULL,
    memory_mib BIGINT NOT NULL,
    family TEXT,                       -- 'm5', 'Dsv3', 'n2'
    category TEXT,                     -- 'general', 'compute_optimized', 'memory_optimized', 'gpu'
    generation TEXT,                   -- '5', 'v3', etc.
    last_refreshed TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, instance_type)
);
```

**Data sources (tiered approach — no embedded static catalog in binary):**

**IAM permission constraint (AWS):** Koku's current IAM policy template for AWS Sources includes only `s3:Get*`, `s3:List*`, `cur:DescribeReportDefinitions`, `organizations:List*`, `organizations:Describe*`, and `iam:ListAccountAliases`. It does **NOT** include `ec2:Describe*`. Calling `EC2.DescribeInstanceTypes` would require customers to update their IAM role — significant friction for existing customers with hundreds of accounts. Therefore, AWS uses a tiered approach:

| Provider | Tier 1 (default, no new permissions) | Tier 2 (optional, customer opts in) | Notes |
|----------|--------------------------------------|--------------------------------------|-------|
| **AWS** | **AWS Bulk Pricing JSON** (public, no auth): `https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/current/region_index.json` → per-region JSON files. Parse `instanceType`, `vcpu`, `memory` from `product.attributes`. Files are large (~2-5 GB for us-east-1) but only need to extract ~600 instance type rows per region. **Refreshed daily** (configurable via `ROS_INSTANCE_CATALOG_REFRESH_HOURS`, default 24) by a background goroutine that downloads, extracts instance specs, and upserts into `cloud_instance_catalog`. AWS publishes daily aggregated price list notifications for changes. This covers ALL instance types including those the customer doesn't use. | If customer adds `ec2:DescribeInstanceTypes` to their IAM role, use the EC2 API directly (paginated, `VCpuInfo.DefaultVCpus`, `MemoryInfo.SizeInMiB`). Faster and more structured than parsing bulk pricing JSON. Opt-in: detected automatically — if `ec2:DescribeInstanceTypes` succeeds, prefer it; if `AccessDeniedException`, fall back to Tier 1 silently. | CUR data (via Koku) only contains instance types the customer **currently uses** — insufficient for recommending alternatives. Tier 1 or Tier 2 is required for full coverage. |
| **Azure** | **Azure Retail Prices API** (public, no auth): `https://prices.azure.com/api/retail/prices?$filter=serviceName eq 'Virtual Machines'`. Parse `armSkuName`, vCPU/memory from `productName`. | N/A — Tier 1 is already public. | Free, no credentials needed. |
| **GCP** | Compute Engine `machineTypes.list`: `GET compute.googleapis.com/compute/v1/projects/{project}/zones/{zone}/machineTypes`. | N/A — Koku already has GCP project credentials via Sources. | Returns `guestCpus` and `memoryMb` directly. Requires iterating zones. |

**Refresh strategy:**
1. Background goroutine refreshes the catalog every `ROS_INSTANCE_CATALOG_REFRESH_HOURS` (default 24 hours).
2. On startup, if catalog is empty or stale (>48 hours), force refresh before processing.
3. Cache in-memory (`sync.Map`) for MachineSet right-sizing lookups (hot path).
4. If API call fails, use cached data. Log warning but don't block recommendations.

**On-prem:** On-prem deployments may have no cloud APIs available. In this case, the catalog is populated only from Koku data (which has instance types from cost reports). If no Koku integration is configured, instance type recommendations are skipped (`instance_type = NULL` in the recommendation).

**Scope:** The catalog covers ALL instance types seen in billing data (via Koku) or available via public APIs — no arbitrary top-50 limit.

**Deprecated / unlisted instance types:**

Customers may be running on instance types that are no longer listed in cloud pricing APIs (e.g., AWS `m4.xlarge`, previous-generation types) or custom/dedicated host types not in the public catalog. The recommendation engine must handle this gracefully:

1. **Right-sizing is always based on actual node capacity from operator metrics** (Prometheus `kube_node_status_capacity` or equivalent), **not** from a catalog lookup. The catalog is consulted only when suggesting alternatives.

2. **Decision matrix:**

| Current instance in catalog? | Node right-sized? | Behavior |
|---|---|---|
| Yes | Yes | Recommend: **keep current**. No action needed. |
| Yes | No (over-provisioned) | Recommend: smallest-fit alternative from catalog. |
| Yes | No (under-provisioned) | Recommend: next-size-up alternative from catalog. |
| **No** (deprecated/unlisted) | **Yes** | Recommend: **keep current**. Emit `INFO_INSTANCE_TYPE_NOT_IN_CATALOG` notification: "Instance type '{type}' is not in the current cloud catalog (may be deprecated or custom). No resizing needed based on utilization." **Do NOT recommend a switch.** |
| **No** (deprecated/unlisted) | No (over-provisioned) | Recommend: smallest-fit alternative from catalog. Emit `INFO_INSTANCE_TYPE_DEPRECATED` notification: "Current instance type '{type}' is deprecated or not in the cloud catalog. Consider migrating to '{recommended_type}' which also right-sizes the node." |
| **No** (deprecated/unlisted) | No (under-provisioned) | Recommend: next-size-up from catalog. Same `INFO_INSTANCE_TYPE_DEPRECATED` notification. |

3. **How to detect "not in catalog":** After catalog refresh, look up the node's `instance_type` label (from `node_labels` or `kube_node_labels`). If no match in `cloud_instance_catalog`, the instance type is unlisted. The node's actual vCPU and memory are still known from Prometheus metrics.

4. **Cost comparison for unlisted types:** If the current instance type has no catalog entry, the cost comparison in the recommendation shows `current_cost = NULL` (unknown) and `recommended_cost = <value>`. The API response includes a `cost_savings` field only when both values are known. For unlisted types, the recommendation is based purely on capacity right-sizing, not cost savings.

### REQ-8c.7: Tier 3 — MachineAutoscaler optimization [MEDIUM] — NOT IMPLEMENTED

**Required:** MachineAutoscaler queries (REQ-8c.4, optional set).

**Algorithm (Go heuristic):**

1. **Saturated autoscaler:**
   - If `current_replicas == max_replicas` sustained >80% of window: recommend increasing `maxReplicas`.
   - Emit `WARNING_AUTOSCALER_SATURATED` with suggested new max: `current_max × 1.5` or `current_max + (current_max - min)`.

2. **Idle autoscaler:**
   - If `current_replicas == min_replicas` sustained >95% of window: recommend decreasing `minReplicas`.
   - Emit autoscaler idle signal (code **75** reserved for future `minReplicas` tuning). Code **15** is **`NODE_IDLE`** (node idle/zombie detection, migration **000121**). Autoscaler codes **14**, **16**, **17** remain Tier 3.

3. **Missing autoscaler:**
   - If a MachineSet has no MachineAutoscaler AND utilization varies >50% between daily peak and trough: recommend enabling autoscaling.
   - Emit `AUTOSCALER_RECOMMENDED` (code 17).

4. **Flapping detection:**
   - Count scale events (replica count changes) per day. If >5 scale events/day sustained: suggest widening the stabilization window or adjusting target utilization.
   - Emit `WARNING_AUTOSCALER_FLAPPING`.

### REQ-8c.8: Go — `recommendNodes()` [HIGH]

**Required behavior:** Tier 1 node utilization analysis runs in Go. After batch-reading `daily_node_digests` for the cluster and window (same "read once, compute" pattern as containers), `recommendNodes()` evaluates underutilization, overcommit, stranded resources, and per-node utilization summaries, then persists rows to `node_recommendations`.

**Logical outputs** (mapped to `node_recommendations` columns): `node`; `cpu_util_p50` / `cpu_util_p95`; `mem_util_p50` / `mem_util_p95`; `cpu_overcommit_ratio`; `is_underutilized` / `is_overcommitted`; `stranded_resource` (`'cpu'`, `'memory'`, or NULL); `trend_slope` (from linear regression on daily means in Go).

Tier 2 and Tier 3 (MachineSet right-sizing, autoscaler) also run in **Go** — they require cross-node aggregation, instance type catalog lookups, and MachineAutoscaler logic.

### REQ-8c.9: Batch entry point — extend `recommendAllWorkloads()` [HIGH]

**Required behavior:** Invoke `recommendNodes()` from the same Go batch pipeline as container recommendations (e.g. within or immediately after `recommendAllWorkloads()`). Node recommendations run **after** container recommendations (since container request sums inform overcommit detection). MachineSet recommendations (Tier 2/3) run in the same Go batch after Tier 1 node rows are written.

### REQ-8c.10: API — Node & MachineSet recommendation endpoints [HIGH] — PARTIAL

**Required behavior:**

1. Add new endpoints (same RBAC, pagination patterns as container endpoints):

| Method | Path | Purpose | Tier |
|---|---|---|---|
| GET | `/api/cost-management/v1/recommendations/openshift/nodes` | Node utilization recommendations (all nodes in cluster) | 1 |
| GET | `/api/cost-management/v1/recommendations/openshift/nodes/:node` | Individual node detail | 1 |
| GET | `/api/cost-management/v1/recommendations/openshift/machinesets` | MachineSet right-sizing recommendations | 2 |
| GET | `/api/cost-management/v1/recommendations/openshift/machinesets/:name` | Individual MachineSet detail | 2 |

2. Support filtering by `cluster_uuid`, `node`, `machineset_name`, `instance_type`, `is_underutilized`, `is_overcommitted`, `stranded_resource`.

### REQ-8c.11: Database — Node and MachineSet recommendation tables [HIGH] — PARTIAL

```sql
CREATE TABLE node_recommendations (
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    node                    TEXT NOT NULL,
    cpu_util_p50            REAL,
    cpu_util_p95            REAL,
    mem_util_p50            REAL,
    mem_util_p95            REAL,
    cpu_overcommit_ratio    REAL,
    is_underutilized        BOOLEAN,
    is_overcommitted        BOOLEAN,
    stranded_resource       TEXT,       -- 'cpu', 'memory', or NULL
    pod_count               BIGINT,
    pod_capacity            BIGINT,
    instance_type           TEXT,
    machineset_name         TEXT,
    trend_slope             REAL,
    notification_codes      SMALLINT[],
    updated_at              TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (org_id, cluster_uuid, node)
);

CREATE TABLE machineset_recommendations (
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    machineset_name         TEXT NOT NULL,
    current_instance_type   TEXT,
    rec_instance_type       TEXT,       -- NULL if no change recommended
    current_replicas        BIGINT,
    rec_replicas            BIGINT,     -- NULL if no change recommended
    cpu_util_p95            REAL,       -- aggregate across all nodes
    mem_util_p95            REAL,
    autoscaler_min          BIGINT,     -- NULL if no autoscaler
    autoscaler_max          BIGINT,
    rec_autoscaler_min      BIGINT,     -- NULL if no change
    rec_autoscaler_max      BIGINT,
    is_saturated            BOOLEAN,
    is_idle                 BOOLEAN,
    is_flapping             BOOLEAN,
    savings_vcpus           BIGINT,     -- total vCPU reduction
    savings_memory_gib      BIGINT,     -- total memory GiB reduction
    estimated_savings_cents REAL, -- from Koku cost data
    notification_codes      SMALLINT[],
    updated_at              TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (org_id, cluster_uuid, machineset_name)
);
```

### Node Recommendation Response Format

```json
{
  "data": [{
    "cluster_uuid": "...",
    "cluster_alias": "...",
    "node": "ip-10-0-1-42.ec2.internal",
    "instance_type": "m5.4xlarge",
    "machineset_name": "worker-us-east-1a",
    "utilization": {
      "cpu_p50_percent": 22.5,
      "cpu_p95_percent": 45.2,
      "memory_p50_percent": 18.1,
      "memory_p95_percent": 35.8
    },
    "scheduling": {
      "pod_count": 42,
      "pod_capacity": 110,
      "cpu_overcommit_ratio": 1.2
    },
    "flags": {
      "is_underutilized": true,
      "is_overcommitted": false,
      "stranded_resource": "memory"
    },
    "trend": {
      "cpu_slope": 0.002,
      "direction": "stable"
    },
    "notifications": {
      "NODE_UNDERUTILIZED": { "type": "notice", "message": "Node CPU and memory utilization below 30% sustained" },
      "NODE_STRANDED_MEMORY": { "type": "notice", "message": "CPU at 45% but memory at 18% — consider CPU-optimized instances" }
    }
  }]
}
```

### MachineSet Recommendation Response Format

```json
{
  "data": [{
    "cluster_uuid": "...",
    "cluster_alias": "...",
    "machineset_name": "worker-us-east-1a",
    "current": {
      "instance_type": "m5.4xlarge",
      "replicas": 6,
      "total_vcpus": 96,
      "total_memory_gib": 384
    },
    "recommended": {
      "instance_type": "m5.2xlarge",
      "replicas": 5,
      "total_vcpus": 40,
      "total_memory_gib": 160
    },
    "utilization": {
      "cpu_p95_percent": 35.2,
      "memory_p95_percent": 28.4
    },
    "autoscaler": {
      "current_min": 3,
      "current_max": 10,
      "recommended_min": 2,
      "recommended_max": 8,
      "is_saturated": false,
      "is_idle": true,
      "is_flapping": false
    },
    "savings": {
      "vcpu_savings": 56,
      "memory_savings_gib": 224,
      "estimated_savings_cents": 1247.50
    },
    "notifications": {
      "AUTOSCALER_IDLE": { "type": "notice", "message": "MachineAutoscaler never scales up — consider reducing minReplicas" }
    }
  }]
}
```

---

## 13. Phase 9: JVM/Quarkus Runtime Recommendations (Weeks 16–20)

### REQ-9.1: JVM runtime detection [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §18

**Required behavior:** Detect JVM workloads via one of:
1. Prometheus metric presence (`jvm_info`, `jvm_memory_used_bytes`).
2. `go_info` absence + `java`/`jdk`/`jvm` in image name (fallback heuristic).

Detect JVM vendor: Hotspot vs Semeru/OpenJ9 from `jvm_info` labels.

### REQ-9.2: MaxRAMPercentage recommendation [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §18

**Required behavior:**
1. Read actual heap usage from `jvm_memory_used_bytes` (if available).
2. Compute `heap_utilization = max(jvm_heap_used) / container_memory_limit`.
3. Recommend `MaxRAMPercentage`:
   - If heap utilization data available: `max(heap_utilization + 0.10, 0.50) × 100` (at least 50%, with 10% headroom over peak).
   - If no heap data: container ≤ 512MB → 50%, container > 512MB → 80% (Kruize defaults, preserved).

### REQ-9.3: GC policy recommendation [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §18

**Required behavior:**
1. If GC pause metrics available (`jvm_gc_pause_seconds_max`):
   - Pause > 200ms sustained → recommend low-latency GC (ZGC ≥ JDK 15, Shenandoah ≥ JDK 12, G1 otherwise).
   - Pause < 50ms → current GC is fine.
2. If no GC metrics:
   - Use heuristic: ≤ 2 cores → SerialGC; 2–4 cores → ParallelGC or G1; > 4 cores → G1 or ZGC.
3. Respect JDK version constraints: ZGC requires JDK 15+, Shenandoah requires JDK 12+.

### REQ-9.4: Quarkus thread pool recommendation [HIGH] — NOT IMPLEMENTED

**Source:** Analysis §18

**Required behavior:**
1. Detect Quarkus via Prometheus metric or image heuristic.
2. Recommend `quarkus.thread-pool.core-threads = max(8, 2 × ceil(cpu_cores))` (matches Quarkus default formula).
3. Recommend `quarkus.thread-pool.queue-size = 2 × core-threads`.
4. **Do not** use `THREADS_PER_CORE = 1` — this undersizes by 50%+ vs Quarkus defaults.

### REQ-9.5: Semeru consistency [MEDIUM] — NOT IMPLEMENTED

**Source:** Analysis §18

**Required behavior:** Use `ceil()` for CPU core rounding (not `round()`), consistent with Hotspot handler. A 1.3-core container should get 2-core-based recommendations for both JVM vendors.

---

## 14. Phase 10: Remove Kruize Dependency (Weeks 18–22)

### REQ-10.1: Remove Kruize API calls [HIGH] — NOT IMPLEMENTED

**Required behavior:** Remove all code paths that call Kruize endpoints:
- `/createExperiment`
- `/updateResults`
- `/updateRecommendations`
- `/updateExperiment`
- `/createPerformanceProfile`
- `/listPerformanceProfiles`

Remove: `internal/utils/kruize/` directory, `internal/types/kruizePayload/` directory, Kruize-related configuration variables (`KRUIZE_URL`, `KRUIZE_HOST`, `KRUIZE_PORT`, etc.).

### REQ-10.2: Remove internal recommendation Kafka topic [MEDIUM] — NOT IMPLEMENTED

**Required behavior:** The `rosocp.kruize.recommendations` topic was an internal coordination mechanism between the report processor and recommendation poller. With native Go computation, recommendations are computed synchronously during ingestion (or on-demand at API time). Remove the producer, consumer, and topic configuration.

### REQ-10.3: Simplify ingestion pipeline [HIGH] — PARTIAL

**Required behavior:** The new ingestion flow is:

**SaaS (Kafka):**
1. Consume Kafka message from `hccm.ros.events`.
2. Download and parse CSV.
3. Validate in Go → compute daily digest aggregations in memory → upsert into `daily_container_digests`.
4. Daily digests are immediately available for the Go recommendation engine.
5. Upsert workload metadata in PostgreSQL (relational columns).
6. Go "read once, compute N terms": read max window of digests for cluster, compute all customer-defined terms in memory, batch-write recommendation results via `COPY FROM`.
7. Optionally: serve on-demand recommendations for ad-hoc custom timeframes at API time.
8. Commit Kafka offset.

**On-prem (same Kafka path):**

The cost-onprem chart deploys AMQ Streams (Kafka). The on-prem flow is identical to SaaS: Koku's listener receives the operator tarball, `ROSReportShipper` uploads ROS CSVs to S3 (NooBaa/ODF) and sends a Kafka message to `hccm.ros.events`. ros-ocp-backend-superpowers consumes from the same topic. Steps 1–8 are the same as SaaS above.

No Kruize HTTP calls. No recommendation polling loop.

**Kafka offset and error handling** (matching Koku's `listen_for_messages` pattern):

Offsets are committed **manually** (`enable.auto.commit: false`). Error behavior depends on the failure type:

| Failure | Behavior | Offset | Rationale |
|---|---|---|---|
| Invalid JSON / unmarshal / validation | Log error, **commit offset** (drop message) | Advanced | Bad data cannot be fixed by retrying. Koku does the same for `ReportProcessorError`. |
| S3 download failure (network, 404, partial) | Log error, **seek back** to current offset, sleep `ROS_RETRY_SECONDS` (default 10s) | Not advanced | Transient infra issue. Koku's `rewind_consumer_to_retry()` pattern. |
| PostgreSQL error (upsert, connection) | Log error, **seek back**, reset DB connection, sleep | Not advanced | DB may be temporarily down. Matches Koku's `OperationalError`/`InterfaceError` handling. |
| Go recommendation computation failure | Log error, **commit offset** | Advanced | Metrics are already stored; recommendations can be recomputed on next cycle. Don't block the queue. |
| Unknown / unexpected error | Log error at ERROR level, **do not commit** | Not advanced | Conservative — will retry. Matches Koku's catch-all. |

**No dead-letter queue (DLQ):** Neither Koku nor the current ros-ocp-backend use a DLQ. Retryable errors use seek-back with sleep; non-retryable errors are committed (dropped) with logging. A DLQ could be added post-MVP if operational experience shows a need, but the seek-back pattern has proven sufficient in production for Koku.

**Consumer lag monitoring:** Expose `ros_kafka_consumer_lag` Prometheus gauge (messages behind head of partition). Alert if lag exceeds threshold (e.g., 1000 messages for >10 minutes) — indicates processing bottleneck or repeated seek-back.

### REQ-10.4: Remove Kruize from deployment manifests [MEDIUM] — NOT IMPLEMENTED

**Required behavior:** Update Helm charts, docker-compose files, and deployment configurations to remove:
- Kruize container/pod
- Kruize PostgreSQL database
- Kruize service/route
- Kruize performance profile ConfigMap
- Kruize-related environment variables

And add (if not already present from Phase 2):
- **On-prem:** The `cost-onprem-chart` already ships PostgreSQL 16 — no database upgrade needed.
- **SaaS:** The Clowder-provisioned RDS instance must be upgraded from version 13 to at least **version 16** (`clowdapp.yaml` `database.version: 16`). AWS RDS supports PG 16. This aligns with Koku SaaS (already on PG 16).

### REQ-10.5: Update health checks and container resources [LOW] — PARTIAL

> **⚠️ Implementation status:** `/healthz` and `/readyz` endpoints described below are **NOT implemented**. The current binary exposes only `/status` (simple 200 OK) and `/metrics` (Prometheus). Kubernetes liveness/readiness probes in production use `/status`. The dedicated probe endpoints with goroutine/deadlock/connectivity checks remain a future enhancement.

**Required behavior:** Remove Kruize health check from ros-ocp-backend startup validation (`Setup_kruize_performance_profile` becomes unnecessary). Replace with `/healthz`, `/readyz` probe endpoints (NFR-4).

**Container resource recommendations** (baseline, tune in production):

| | Requests | Limits | Rationale |
|---|---|---|---|
| **CPU** | 100m | 1000m (1 core) | Go binary is bursty: idle while waiting for Kafka, CPU-intensive during CSV parse + COPY FROM batch. `GOMAXPROCS=2` recommended. |
| **Memory** | 256Mi | 512Mi | CSV streaming + `COPY FROM` staging + API response assembly. NFR-2 bounds all memory consumers. No large in-memory caches. |

These are starting points — adjust based on production profiling. The Go binary should set `GOMEMLIMIT=450MiB` (90% of memory limit) to enable soft memory limit and avoid unnecessary GC pressure while preventing OOM kills. `GOMAXPROCS` should be set via `uber-go/automaxprocs` (reads cgroup CPU quota automatically).

### REQ-10.6: Recommendation quality metrics [MEDIUM] (simplified)

**Source:** Risk review Q16 (simplified per third review)

**Required behavior:** Implement lightweight quality tracking without requiring application-level feedback:

1. **OOM rate:** Count OOM events (from `oom_count_sum` in `daily_container_digests`) for containers that have active recommendations. High OOM rate after recommendations suggests under-sizing. Metric: `ros_recommendation_oom_rate` (Prometheus gauge).
2. **Recommendation stability:** Track value drift between consecutive recommendation cycles. If `|new_rec - old_rec| / old_rec > 0.20` for >50% of containers, the algorithm may be unstable. Metric: `ros_recommendation_stability` (Prometheus gauge).
3. **Store:** Write simplified quality metrics to `recommendation_quality` partitioned table (retained for 90d, old partitions dropped). Drop `accuracy_score` column — it requires knowing whether the user actually applied the recommendation and what happened afterward, which we cannot measure without application-level feedback.
4. **Expose:** Via Prometheus metrics and the `/recommendations/openshift/quality` API endpoint.

### REQ-10.7: Recommendation adoption detection [MEDIUM]

**Source:** Third review (L3)

**Required behavior:** Detect when users apply recommendations by monitoring resource request/limit changes:

1. **Compare:** On each ingestion cycle, compare `current_period_request_mc` with `previous_period_request_mc` (from the prior data point for the same container).
2. **Detect:** If `abs(current - previous) > threshold` (default: 10% change) AND `abs(current - our_recommendation) / our_recommendation < tolerance` (default: 5%), mark recommendation as **"likely applied"**.
3. **Record:** Store `recommendation_applied_at TIMESTAMPTZ` on the `recommendation_sets` row. Emit `RECOMMENDATION_APPLIED` (code 6) notification.
4. **Report:** Expose adoption rate per cluster and per org via `/recommendations/openshift/quality` endpoint and Prometheus metric `ros_recommendation_adoption_rate`.

**Value:** Enables measuring actual adoption rates, identifying teams that act on recommendations, and tuning algorithms based on real-world feedback loops.

### REQ-10.8: Recommendation staleness detection [MEDIUM]

**Source:** Third review (C4)

**Required behavior:** Detect and surface stale recommendations:

1. **Threshold:** If no new metrics data has been received for a container for > 48 hours (configurable via `ROS_STALE_DATA_THRESHOLD_HOURS`), mark the recommendation as stale.
2. **Notification:** Emit `STALE_DATA` (code 2) on the recommendation response. API response includes `"stale": true` flag and `"last_reported"` timestamp.
3. **Cleanup:** Recommendations stale for > 30 days (configurable via `ROS_STALE_CLEANUP_DAYS`) are deleted from `recommendation_sets` during the retention sweep. Archiving to `recommendation_history` before deletion is future work (see `docs-site/features/history-and-quality.md`).
4. **API behavior:** Stale recommendations are still returned by default but can be excluded via `?stale=false` query parameter.

---

## 15. Performance Targets

All targets are structural estimates from the performance model in Analysis §25. They must be validated with benchmarks after implementation.

| Metric | Current | Target | Validation Method |
|---|---|---|---|
| Ingestion: time per container per interval | ~125 ms | ~0.064 ms | Benchmark with 10K containers |
| Recommendation: time per container (amortized) | ~42 ms | ~0.004 ms (Go batch) | Benchmark with 50K containers via `recommendAllWorkloads()` |
| Recommendation: time per cluster (50K containers) | ~35 min | ~0.2 sec (single `recommendAllWorkloads()` run) | Benchmark |
| Max containers per hour (1 worker) | ~1,000 | ~500,000 | Load test |
| Max containers per hour (10 workers) | ~1,000 | ~5,000,000 | Load test |
| Metrics storage (50K containers, 91d) | ~5.7 TB | ~3 GB (daily digest tables) | Measure after 91d run |
| Application RAM (steady state) | ~350–700 MB | ~50–100 MB | Monitor in staging |
| API p50 latency (pre-computed rec) | ~5–20 ms | ~0.3–0.5 ms (typed column read) | Load test |
| API p50 latency (on-demand rec) | N/A (batch only) | ~0.5–2 ms (Go on-demand computation) | Load test |
| API p99 latency | ~50–200 ms | ~5–10 ms | Load test |
| Data round-trips per cluster (rec step) | 2 × N_containers | 1 batched digest read + 1 `COPY` write | Structural |

---

## 16. Operator Changes (Cross-Phase)

All operator changes are in the `koku-metrics-operator` repository. Each is a separate PR.

| Phase | Change | New Queries | New CSV Columns | Priority |
|---|---|---|---|---|
| ~~2~~ | ~~Integer types (millicores/KiB)~~ | ~~0~~ | ~~Modified existing columns~~ | ~~High~~ — **Removed:** conversion now happens in ros-ocp-backend at CSV parse time; no operator change needed (REQ-2.3). |
| 4 | OOM event collection | 1 | `oom_last_timestamp`, `oom_count` | High |
| ~~6~~ | ~~Include existing PVC data in ROS CSV~~ | ~~0~~ | ~~PVC columns in ROS CSV~~ | ~~High~~ — **Removed:** ros-ocp-backend reads the existing `cm-openshift-storage-usage-*.csv` directly; no operator change needed. SaaS requires a Koku routing change (`kafka_msg_handler.py`). |
| 6 | Go runtime detection | 1 | `go_version`, `go_maxprocs` | High |
| 7 | Replica count | 2–4 | `desired_replicas`, `available_replicas` | High |
| 8 | HPA metrics | 8 | HPA spec/status columns | High |
| 8 | Ephemeral storage | 4 | `ephemeral_storage_*` columns | Medium |
| 8 | ResourceQuota | 2 | N/A (namespace-level) | Medium |
| 8 | Node.js detection | 1 | `nodejs_version` | Medium |
| 9 | JVM detection (if not in CSV) | 1 | `jvm_vendor`, `jdk_version` | Medium |
| 8b | VM CPU/memory ROS queries | 6 | Reuse existing `kubevirt_*` series at ROS granularity | High |
| 8b | VM disk IOPS/throughput | 4 | `disk_read_iops`, `disk_write_iops`, `disk_read_bytes_sec`, `disk_write_bytes_sec` | High |
| 8b | VM disk allocation + info | 2 | Reuse existing `kubevirt_*` series | High |
| 8c | Node labels (ROS routing) | 1 | `instance_type`, `machineset_name`, `region`, `zone` | High |
| 8c | Node CPU/memory usage (ROS routing) | 0 (reuse `cost:node_*`) | Reuse existing series at ROS granularity | High |
| 8c | MachineSet replica counts | 3 | `machineset_replicas`, `machineset_available`, `machineset_desired` | High |
| 8c | MachineAutoscaler bounds (Tier 3) | 2 | `autoscaler_min`, `autoscaler_max` | Medium |
| — | Python/.NET detection | 2 | `python_version`, `dotnet_version` | Low |

**Total new queries:** ~40 (on top of existing ~73). Budget increase: ~55%.

---

## 17. API Contract Changes

### Backward-Compatible Additions

The following fields are **added** to the existing API response format without breaking existing consumers:

```json
{
  "data": [{
    "cluster_alias": "...",
    "cluster_uuid": "...",
    "container": "...",
    "id": "...",
    "project": "...",
    "workload": "...",
    "workload_type": "...",
    "source_id": "...",
    "last_reported": "...",

    "replicas": {                              // NEW (Phase 7)
      "desired": 5,
      "available": 5,
      "source": "kube_state_metrics"           // or "derived"
    },

    "recommendations": {
      "short_term": {
        "duration_in_hours": 24,
        "monitoring_start_time": "...",
        "monitoring_end_time": "...",

        "cost": {
          "cpu": {
            "request": { "amount": 200, "format": "millicores" },
            "limit": { "amount": 200, "format": "millicores" },
            "current_request": { "amount": 500, "format": "millicores" },  // NEW
            "per_container_savings": { "amount": 300, "format": "millicores" },  // NEW
            "total_savings": { "amount": 1500, "format": "millicores" }    // NEW
          },
          "memory": {
            "request": { "amount": 256, "format": "MiB" },
            "limit": { "amount": 384, "format": "MiB" },
            "current_request": { "amount": 512, "format": "MiB" },        // NEW
            "per_container_savings": { "amount": 256, "format": "MiB" },   // NEW
            "total_savings": { "amount": 1280, "format": "MiB" }          // NEW
          }
        },
        "performance": { "...same structure..." },

        "notifications": {
          "120001": { "type": "info", "message": "..." },
          "IDLE_WORKLOAD": { "type": "notice", "message": "..." },          // NEW
          "GPU_UNDERUTILIZED": { "type": "notice", "message": "..." },      // NEW
          "MEMORY_TRENDING_UP": { "type": "warning", "message": "..." }     // NEW
        }
      }
    },

    "additional_recommendations": {             // NEW (Phases 6, 8, 9)
      "idle_detection": {
        "is_idle": false,
        "is_abandoned": false,
        "estimated_savings": { "cpu_millicores": 0, "memory_mib": 0 }
      },
      "gpu": {
        "current_model": "NVIDIA A100",
        "recommended_profile": "MIG 1g.5gb",
        "underutilized": false,
        "estimated_savings_percent": 71
      },
      "runtime": {                              // NEW (Phase 9)
        "detected": "hotspot",
        "jdk_version": 21,
        "recommendations": {
          "MaxRAMPercentage": { "current": null, "recommended": "75.0" },
          "GCPolicy": { "current": "G1", "recommended": "ZGC" },
          "GOMAXPROCS": null,
          "quarkus.thread-pool.core-threads": null
        }
      },
      "pvc": [{                                 // NEW (Phase 6)
        "pvc_name": "data-vol",
        "capacity_gib": 100,
        "usage_gib": 15,
        "utilization_percent": 15,
        "recommendation": "Reduce to 20 GiB",
        "growth_trend": "stable"
      }],
      "hpa": {                                  // NEW (Phase 8)
        "current_min": 2,
        "current_max": 10,
        "recommended_min": 1,
        "recommended_max": 8,
        "scaling_limited_percent": 0,
        "flapping": false
      }
    }
  }]
}
```

### New Endpoints

All new endpoints follow the same patterns as existing recommendation endpoints:

**RBAC:** Same `cost-management:ros:*:read` permission as existing container recommendation endpoints. No new RBAC resources required — the existing ros-ocp-backend RBAC middleware applies uniformly to all `/recommendations/openshift/` paths.

**Pagination:** All list endpoints support `limit` (default 10, max 1000), `offset`, `sort_by`, `order_by` (asc/desc). Response includes `meta.count` for total results, consistent with existing endpoint behavior.

**Performance note for large orgs:** `limit`/`offset` pagination degrades at high offsets (`OFFSET 50000` requires scanning 50K rows). For MVP, this is acceptable — most customers have <5K containers and the recommendation_sets table is indexed on the common filter/sort columns. **Post-MVP**, if customers with >50K containers report slow pagination, migrate to **keyset (cursor) pagination**: instead of `offset`, the client passes the last row's sort key (e.g., `?after=<last_workload_id>&limit=100`), and the query uses `WHERE workload_id > ?` which is index-seekable regardless of depth. The OpenAPI spec should reserve the `after` query parameter now (ignored by the initial implementation) to avoid a breaking API change later.

| Method | Path | Purpose | Phase |
|---|---|---|---|
| GET | `/api/cost-management/v1/recommendations/openshift/summary` | Cluster-wide savings summary (aggregated total_savings across all workloads) | 7 |
| GET | `/api/cost-management/v1/recommendations/openshift/history` | Fleet recommendation history (filter by container, cluster, project, workload, term, engine) | 5 (Q5) |
| GET | `/api/cost-management/v1/recommendations/openshift/virtual-machines` | VM right-sizing recommendations | 8b |
| GET | `/api/cost-management/v1/recommendations/openshift/virtual-machines/:id` | Individual VM recommendation detail | 8b |
| GET | `/api/cost-management/v1/recommendations/openshift/nodes` | Node utilization recommendations (Tier 1) | 8c |
| GET | `/api/cost-management/v1/recommendations/openshift/nodes/:node` | Individual node recommendation detail | 8c |
| GET | `/api/cost-management/v1/recommendations/openshift/machinesets` | MachineSet right-sizing + autoscaler recommendations (Tier 2+3) | 8c |
| GET | `/api/cost-management/v1/recommendations/openshift/machinesets/:name` | Individual MachineSet recommendation detail | 8c |
| GET | `/api/cost-management/v1/recommendations/openshift/quality` | Recommendation quality metrics (OOM rate, stability, adoption rate) | 10 |
| GET | `/api/cost-management/v1/recommendations/openshift/fleet-summary` | Cross-cluster aggregated savings, adoption rates, top opportunities by org_id (REQ-7.6) | 7 |
| GET | `/api/cost-management/v1/recommendations/openshift/notification-codes` | Reference table of all notification codes with severity and description (REQ-1.14) | 1 |

### VM Recommendation Response Format (Phase 8b)

```json
{
  "data": [{
    "cluster_uuid": "...",
    "cluster_alias": "...",
    "namespace": "...",
    "vm_name": "my-database-vm",
    "vm_os": "linux",
    "guest_os_name": "rhel",
    "vm_instance_type": "u1.medium",
    "last_reported": "...",

    "current": {
      "vcpus": 8,
      "memory_gib": 32,
      "disk_gib": 200
    },
    "recommended": {
      "vcpus": 4,
      "memory_gib": 16,
      "disk_gib": 200
    },
    "iops_profile": {
      "read_p95": 450,
      "write_p95": 120,
      "throughput_read_mbs": 85.2,
      "throughput_write_mbs": 22.1
    },
    "flags": {
      "is_idle": false,
      "is_oversized": true,
      "is_abandoned": false
    },
    "utilization": {
      "cpu_p95_percent": 28.5,
      "memory_p95_percent": 42.1
    },
    "savings": {
      "vcpu_savings": 4,
      "memory_savings_gib": 16,
      "disk_savings_gib": 0
    }
  }]
}
```

### Node Recommendation Response Format (Phase 8c)

See [REQ-8c.10](#req-8c10-api--node--machineset-recommendation-endpoints-high) for full response schema.

Filters: `cluster_uuid`, `node`, `machineset_name`, `instance_type`, `is_underutilized` (bool), `is_overcommitted` (bool), `stranded_resource` (`cpu`|`memory`).

### MachineSet Recommendation Response Format (Phase 8c)

See [REQ-8c.10](#req-8c10-api--node--machineset-recommendation-endpoints-high) for full response schema.

Filters: `cluster_uuid`, `machineset_name`, `current_instance_type`, `is_saturated` (bool), `is_idle` (bool), `is_flapping` (bool).

---

## 18. Database Schema Changes

### New Tables

```sql
-- PostgreSQL 16+. Optional: CREATE EXTENSION IF NOT EXISTS pg_partman;
-- pg_partman automates partition creation and retention (supported on AWS RDS, Crunchy PGO, etc.).

-- Phase 2: Daily container digest table (populated by Go during CSV ingestion)
-- The Go binary parses each CSV batch, computes exact percentiles via slices.Sort()
-- on ~96 integer values per container per day, and upserts into this table.
-- No raw metric readings are stored in PostgreSQL — CSVs remain in S3.
CREATE TABLE daily_container_digests (
    bucket_date         DATE NOT NULL,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    workload            TEXT NOT NULL,
    workload_type       TEXT NOT NULL,
    container_name      TEXT NOT NULL,
    -- Pre-computed percentiles (exact, computed in Go)
    -- All numeric metric columns are BIGINT (int64 end-to-end, see REQ-2.3)
    cpu_request_p50_mc  BIGINT,
    cpu_request_p60_mc  BIGINT,    -- cost profile
    cpu_request_p95_mc  BIGINT,
    cpu_request_p98_mc  BIGINT,    -- performance profile
    cpu_request_p99_mc  BIGINT,
    cpu_usage_p50_mc    BIGINT,
    cpu_usage_p60_mc    BIGINT,
    cpu_usage_p95_mc    BIGINT,
    cpu_usage_p98_mc    BIGINT,
    cpu_usage_p99_mc    BIGINT,
    cpu_usage_max_mc    BIGINT,    -- exact MAX for performance p100
    cpu_throttle_p95_mc BIGINT,
    cpu_throttle_max_mc BIGINT,
    memory_request_p50_kib  BIGINT,
    memory_request_p95_kib  BIGINT,
    memory_usage_p50_kib    BIGINT,
    memory_usage_p95_kib    BIGINT,
    memory_usage_max_kib    BIGINT,   -- exact MAX for performance p100
    memory_rss_p95_kib      BIGINT,
    memory_rss_max_kib      BIGINT,   -- exact MAX for OOM avoidance
    -- Aggregates
    oom_count_sum       BIGINT DEFAULT 0,
    cpu_usage_mean_mc   BIGINT,        -- for trend analysis (linear regression)
    memory_usage_mean_kib BIGINT,      -- for trend analysis
    sample_count        BIGINT,        -- readings in this day (expect ~96)
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, bucket_date)
) PARTITION BY RANGE (bucket_date);
-- Partitions: monthly, created by golang-migrate or Go auto-partition function.
-- Retention: DROP old partitions when bucket_date < now() - 45 days.

-- Phase 8b: Daily VM digest table (populated by Go during CSV ingestion)
CREATE TABLE daily_vm_digests (
    bucket_date         DATE NOT NULL,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    vm_name             TEXT NOT NULL,
    cpu_usage_p95_mc    BIGINT,
    cpu_usage_max_mc    BIGINT,
    mem_usage_p95_kib   BIGINT,
    mem_usage_max_kib   BIGINT,
    disk_iops_read_p95  BIGINT,
    disk_iops_write_p95 BIGINT,
    disk_throughput_read_p95_mbs  REAL,
    disk_throughput_write_p95_mbs REAL,
    max_cpu_request_mc  BIGINT,
    max_mem_request_kib BIGINT,
    max_disk_allocated_bytes BIGINT,
    sample_count        BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, vm_name, bucket_date)
) PARTITION BY RANGE (bucket_date);

-- Phase 5: Daily GPU digest table (low volume — GPU containers only)
CREATE TABLE daily_gpu_digests (
    bucket_date         DATE NOT NULL,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    workload            TEXT NOT NULL,
    container_name      TEXT NOT NULL,
    gpu_util_p50_pct    REAL,
    gpu_util_p95_pct    REAL,
    gpu_mem_p95_pct     REAL,
    gpu_fb_used_p95_mib BIGINT,
    max_fb_total_mib    BIGINT,
    gpu_model           TEXT,
    mig_profile         TEXT,
    sample_count        BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, bucket_date)
) PARTITION BY RANGE (bucket_date);

-- Phase 6: Daily PVC digest table
CREATE TABLE daily_pvc_digests (
    bucket_date         DATE NOT NULL,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    pvc_name            TEXT NOT NULL,
    usage_p95_kib       BIGINT,
    usage_max_kib       BIGINT,
    max_capacity_kib    BIGINT,
    max_inode_pct       REAL,
    sample_count        BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, pvc_name, bucket_date)
) PARTITION BY RANGE (bucket_date);

-- Phase 8a: Daily HPA digest table (counts/booleans, not distributions)
CREATE TABLE daily_hpa_digests (
    bucket_date         DATE NOT NULL,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    hpa_name            TEXT NOT NULL,
    target_workload     TEXT,
    min_replicas        BIGINT,
    max_replicas        BIGINT,
    avg_current_replicas REAL,
    max_current_replicas BIGINT,
    min_current_replicas BIGINT,
    times_at_max        BIGINT,        -- count of intervals at maxReplicas
    times_scaling_limited BIGINT,      -- count of intervals with scaling_limited=true
    sample_count        BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, hpa_name, bucket_date)
) PARTITION BY RANGE (bucket_date);

-- Phase 3 (v4.0): Recommendation engine is entirely Go — no PL/pgSQL or SQL functions.
-- Pattern: read daily digest rows once per cluster/window, compute all terms/engines in memory, then persist.
-- Go entry points (see REQ-3.2): recommendCPU(), recommendMemory(), detectIdle(), recommendPVC(),
-- recommendNamespaceQuota(), recommendVM(), recommendNodes(), recommendAllWorkloads().
-- SQL is limited to SELECT (load digests), INSERT/UPDATE (results), and golang-migrate DDL.

-- (VM raw metrics are NOT stored — daily_vm_digests is populated by Go during ingestion)

-- Phase 8b: VM recommendations table
CREATE TABLE vm_recommendations (
    org_id           TEXT NOT NULL,
    cluster_uuid     UUID NOT NULL,
    vm_name          TEXT NOT NULL,
    namespace        TEXT NOT NULL,
    current_vcpus    BIGINT,
    rec_vcpus        BIGINT,
    cpu_util_p95     REAL,
    current_mem_gib  BIGINT,
    rec_mem_gib      BIGINT,
    mem_util_p95     REAL,
    disk_allocated_gib BIGINT,
    rec_disk_gib     BIGINT,
    disk_growth_trend TEXT,
    iops_read_p95    BIGINT,
    iops_write_p95   BIGINT,
    throughput_read_mbs REAL,
    throughput_write_mbs REAL,
    is_idle          BOOLEAN,
    is_oversized     BOOLEAN,
    updated_at       TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (org_id, cluster_uuid, vm_name)
);

-- Phase 8b: VM recommendations — Go recommendVM() (see REQ-8b.4). Batch orchestration: recommendAllWorkloads().

-- (No raw metric tables — GPU, PVC, HPA, VM metric readings are NOT stored in PostgreSQL.
--  The Go binary computes daily digests in memory during CSV ingestion and upserts
--  into the daily_*_digests tables defined above. Raw CSVs remain in S3.)

-- Phase 9: Runtime detection (regular table — updated on each ingestion)
CREATE TABLE workload_runtime_info (
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    workload            TEXT NOT NULL,
    container_name      TEXT NOT NULL,
    runtime_type        TEXT,        -- 'hotspot', 'openj9', 'go', 'nodejs', 'python', 'dotnet', NULL
    runtime_version     TEXT,        -- JDK version, Go version, Node version, etc.
    detected_at         TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name)
);

-- (recommendation_history is defined in §18 New Tables section above as a partitioned table)

-- Notification code reference table (populated on schema initialization)
CREATE TABLE notification_code_definitions (
    code SMALLINT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    severity TEXT NOT NULL DEFAULT 'INFO',  -- INFO, WARNING, CRITICAL
    description TEXT NOT NULL
);
INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (1, 'LOW_CONFIDENCE', 'WARNING', 'Less than 4 days of data available for this workload'),
    (2, 'STALE_DATA', 'WARNING', 'No new metrics data received for more than 48 hours'),
    (3, 'OOM_DETECTED', 'CRITICAL', 'OOM kill events detected within the analysis window'),
    (4, 'PDB_CAVEAT', 'WARNING', 'PodDisruptionBudgets affect workloads on this MachineSet — review before scaling'),
    (5, 'IDLE_WORKLOAD', 'INFO', 'Workload uses less than 1% of requested resources'),
    (6, 'RECOMMENDATION_APPLIED', 'INFO', 'Resource change detected matching a previous recommendation'),
    (7, 'NEW_WORKLOAD', 'INFO', 'Less than 24 hours of data — recommendation may be unstable'),
    (8, 'ABANDONED_WORKLOAD', 'WARNING', 'Workload has zero usage for more than 72 hours'),
    (9, 'MEMORY_TRENDING_UP', 'WARNING', 'Memory usage trend suggests capacity risk within 30 days'),
    (10, 'GPU_UNDERUTILIZED', 'INFO', 'GPU utilization below threshold — consider MIG or smaller profile'),
    (11, 'NODE_UNDERUTILIZED', 'INFO', 'Node resources underutilized — consider consolidation'),
    (12, 'NODE_OVERCOMMITTED', 'WARNING', 'Node request overcommit ratio exceeds threshold'),
    (13, 'STRANDED_RESOURCES', 'INFO', 'Imbalanced CPU/memory utilization — consider different instance family'),
    (14, 'AUTOSCALER_SATURATED', 'WARNING', 'MachineAutoscaler at maxReplicas sustained — consider increasing'),
    (15, 'AUTOSCALER_IDLE', 'INFO', 'MachineAutoscaler at minReplicas sustained — consider decreasing'),
    (16, 'AUTOSCALER_FLAPPING', 'WARNING', 'Frequent scale events — widen stabilization window'),
    (17, 'AUTOSCALER_RECOMMENDED', 'INFO', 'MachineSet has variable load but no autoscaler configured'),
    (18, 'VM_IDLE', 'WARNING', 'Virtual machine has near-zero utilization'),
    (19, 'VM_OVERSIZED', 'INFO', 'Virtual machine allocated resources exceed usage by resize threshold'),
    (20, 'PVC_ORPHANED', 'WARNING', 'PVC has zero usage across all intervals'),
    (21, 'HPA_SATURATED', 'WARNING', 'HPA at maxReplicas sustained — scaling is bottlenecked'),
    (22, 'HPA_ACTIVE', 'INFO', 'Workload is managed by an HPA — replica count recommendations suppressed (see OQ#9)'),
    (23, 'INSTANCE_TYPE_NOT_IN_CATALOG', 'INFO', 'Current instance type is not in the cloud catalog (may be deprecated or custom) — no resizing needed (see REQ-8c.6)'),
    (24, 'INSTANCE_TYPE_DEPRECATED', 'INFO', 'Current instance type is deprecated or not in the cloud catalog — consider migrating to the recommended type (see REQ-8c.6)');
-- Go code uses these codes as constants (internal/notifications/codes.go).
-- API exposes them via GET /recommendations/openshift/notification-codes/.

-- Performance profiles (replaces Kruize /listPerformanceProfiles)
CREATE TABLE recommendation_profiles (
    name TEXT PRIMARY KEY,              -- 'cost', 'performance', or customer-defined
    description TEXT,
    cpu_percentile DOUBLE PRECISION NOT NULL,    -- e.g. 0.60 (cost) or 0.98 (performance)
    mem_percentile DOUBLE PRECISION NOT NULL,    -- e.g. 0.95 (cost) or 1.0 (performance)
    safety_margin DOUBLE PRECISION NOT NULL DEFAULT 0.15,
    decay_halflife_hours DOUBLE PRECISION NOT NULL DEFAULT 168,
    is_default BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT now()
);
INSERT INTO recommendation_profiles (name, description, cpu_percentile, mem_percentile, safety_margin, decay_halflife_hours, is_default) VALUES
    ('cost', 'Minimize resource spend, tolerate occasional throttling', 0.60, 0.95, 0.15, 168, true),
    ('performance', 'Minimize throttling and OOM risk, higher spend', 0.98, 1.0, 0.15, 168, true);
-- Custom profiles (future CRUD): INSERT INTO recommendation_profiles VALUES ('conservative', ..., 0.50, 0.90, 0.20, false);
-- API: GET /recommendations/openshift?profile=conservative → passes profile params to the Go recommendation engine.

-- Customer-defined term window overrides (REQ-1.8)
-- Empty for customers using defaults (1d/7d/15d) — Go uses hardcoded DefaultTerms.
-- Only populated when a customer explicitly customizes their term windows.
CREATE TABLE org_recommendation_terms (
    org_id              TEXT     NOT NULL,
    term_ord            SMALLINT NOT NULL CHECK (term_ord BETWEEN 1 AND 3),
    window_days         SMALLINT NOT NULL CHECK (window_days BETWEEN 1 AND 90),
    decay_halflife_hours REAL,   -- NULL = use default formula: window_days × 12
    PRIMARY KEY (org_id, term_ord)
);
-- Example: Customer wants 10d/30d/90d instead of 1d/7d/15d:
-- INSERT INTO org_recommendation_terms VALUES ('org7654321', 1, 10, NULL);
-- INSERT INTO org_recommendation_terms VALUES ('org7654321', 2, 30, NULL);
-- INSERT INTO org_recommendation_terms VALUES ('org7654321', 3, 90, NULL);
--
-- Go engine logic:
--   rows = SELECT term_ord, window_days, decay_halflife_hours
--          FROM org_recommendation_terms WHERE org_id = $1 ORDER BY term_ord;
--   if len(rows) == 0 { return DefaultTerms }  // 1d/7d/15d — zero DB cost for 99% of customers

-- Recommendation quality metrics (partitioned, monthly)
CREATE TABLE recommendation_quality (
    measured_at                     TIMESTAMPTZ NOT NULL,
    org_id                          TEXT NOT NULL,
    cluster_uuid                    UUID NOT NULL,
    namespace                       TEXT NOT NULL,
    workload                        TEXT NOT NULL,
    container_name                  TEXT NOT NULL,
    oom_events_after_rec            BIGINT,
    stability_pct                   REAL,
    adoption_detected               BOOLEAN DEFAULT false,
    recommendation_age_hours        BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, measured_at)
) PARTITION BY RANGE (measured_at);
-- Partitions: monthly. Retention: DROP partitions older than 90 days.

-- (Node raw metrics are NOT stored — daily_node_digests is populated by Go during ingestion)

-- Phase 8c: Daily node digest table (populated by Go during CSV ingestion)
CREATE TABLE daily_node_digests (
    bucket_date             DATE NOT NULL,
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    node                    TEXT NOT NULL,
    cpu_usage_p50_mc        BIGINT,
    cpu_usage_p95_mc        BIGINT,
    mem_usage_p50_kib       BIGINT,
    mem_usage_p95_kib       BIGINT,
    max_cpu_allocatable_mc  BIGINT,
    max_mem_allocatable_kib BIGINT,
    max_cpu_requests_mc     BIGINT,
    max_mem_requests_kib    BIGINT,
    max_pod_count           BIGINT,
    instance_type           TEXT,
    machineset_name         TEXT,
    sample_count            BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, node, bucket_date)
) PARTITION BY RANGE (bucket_date);

-- Phase 8c: Node recommendations table (Tier 1 — utilization visibility)
CREATE TABLE node_recommendations (
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    node                    TEXT NOT NULL,
    cpu_util_p50            REAL,
    cpu_util_p95            REAL,
    mem_util_p50            REAL,
    mem_util_p95            REAL,
    cpu_overcommit_ratio    REAL,
    is_underutilized        BOOLEAN,
    is_overcommitted        BOOLEAN,
    stranded_resource       TEXT,       -- 'cpu', 'memory', or NULL
    pod_count               BIGINT,
    pod_capacity            BIGINT,
    instance_type           TEXT,
    machineset_name         TEXT,
    trend_slope             REAL,
    notification_codes      SMALLINT[],
    updated_at              TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (org_id, cluster_uuid, node)
);

-- Phase 8c: MachineSet recommendations table (Tier 2+3)
CREATE TABLE machineset_recommendations (
    org_id                      TEXT NOT NULL,
    cluster_uuid                UUID NOT NULL,
    machineset_name             TEXT NOT NULL,
    current_instance_type       TEXT,
    rec_instance_type           TEXT,
    current_replicas            BIGINT,
    rec_replicas                BIGINT,
    cpu_util_p95                REAL,
    mem_util_p95                REAL,
    autoscaler_min              BIGINT,
    autoscaler_max              BIGINT,
    rec_autoscaler_min          BIGINT,
    rec_autoscaler_max          BIGINT,
    is_saturated                BOOLEAN,
    is_idle                     BOOLEAN,
    is_flapping                 BOOLEAN,
    savings_vcpus               BIGINT,
    savings_memory_gib          BIGINT,
    estimated_savings_cents REAL,
    notification_codes          SMALLINT[],
    updated_at                  TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (org_id, cluster_uuid, machineset_name)
);

-- Phase 8c: Node recommendations (Tier 1) — Go recommendNodes(); reads daily_node_digests (REQ-8c.3, REQ-8c.8).
```

### Modified Tables

**Baseline schema reference:** The `ALTER TABLE` statements below assume the existing ros-ocp-backend schema as defined in the current `golang-migrate` migrations at [`ros-ocp-backend/internal/model/migrations/`](https://github.com/RedHatInsights/ros-ocp-backend/tree/main/internal/model/migrations). Key existing tables: `recommendation_sets` (PK: `org_id, cluster_uuid, namespace, workload, container_name`; has `recommendations JSONB`), `workload_metrics`, `workloads`, `historical_recommendation_sets` (partitioned). The new binary continues using `golang-migrate` for schema management; the migrations below are appended to the existing sequence.

```sql
-- Phase 2: Relational columns for recommendations (see REQ-2.5 for full list)
-- All numeric metric columns are BIGINT (int64 end-to-end, see REQ-2.3)
ALTER TABLE recommendation_sets
    ADD COLUMN term TEXT NOT NULL DEFAULT 'short',
    ADD COLUMN engine TEXT NOT NULL DEFAULT 'cost',
    ADD COLUMN current_cpu_request_millicores BIGINT,
    ADD COLUMN current_cpu_limit_millicores BIGINT,
    ADD COLUMN current_memory_request_kib BIGINT,
    ADD COLUMN current_memory_limit_kib BIGINT,
    ADD COLUMN rec_cpu_request_millicores BIGINT,
    ADD COLUMN rec_cpu_limit_millicores BIGINT,
    ADD COLUMN rec_memory_request_kib BIGINT,
    ADD COLUMN rec_memory_limit_kib BIGINT,
    ADD COLUMN variation_cpu_request_pct REAL,
    ADD COLUMN variation_memory_request_pct REAL,
    ADD COLUMN notification_codes SMALLINT[],
    ADD COLUMN confidence_level REAL,
    ADD COLUMN estimated_savings_cents REAL,
    ADD COLUMN recommendation_applied_at TIMESTAMPTZ,  -- REQ-10.7: adoption detection timestamp
    ADD COLUMN stale BOOLEAN DEFAULT false;            -- REQ-10.8: staleness flag

-- Update PK to include term + engine (6 rows per container: 3 terms × 2 engines)
ALTER TABLE recommendation_sets DROP CONSTRAINT IF EXISTS recommendation_sets_pkey;
ALTER TABLE recommendation_sets ADD PRIMARY KEY
    (org_id, cluster_uuid, namespace, workload, container_name, term, engine);

-- Phase 7: Replica count on workloads
ALTER TABLE workloads ADD COLUMN desired_replicas BIGINT;
ALTER TABLE workloads ADD COLUMN available_replicas BIGINT;
ALTER TABLE workloads ADD COLUMN replica_source TEXT;  -- 'kube_state_metrics' or 'derived'
```

### Tables and Columns to Deprecate

```sql
-- Phase 2 (immediate): Drop workload_metrics table
--   (usage_metrics JSONB is never read — verified in §27)
DROP TABLE IF EXISTS workload_metrics;

-- Phase 2 (after validation): Drop recommendations JSONB column
--   (replaced by relational columns above)
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS recommendations;
ALTER TABLE namespace_recommendation_sets DROP COLUMN IF EXISTS recommendations;

-- Phase 10: Remove historical JSONB tables (replaced by recommendation_history partitioned table)
DROP TABLE IF EXISTS historical_recommendation_sets;
DROP TABLE IF EXISTS historical_namespace_recommendation_sets;
-- Associated partitioning triggers and functions
-- NOTE: recommendation_history partitioned table (see above) provides time-series history
-- with partition retention (90d), replacing both tables.
```

---

## 19. Non-Functional Requirements

### NFR-1: Concurrency Safety

All shared data structures (in-memory digest cache, workload metadata cache) must use appropriate synchronization. **Never** use `synchronized (new Object())` (the Kruize anti-pattern from §13). Use Go's `sync.RWMutex`, `sync.Map`, or channel-based patterns.

**Cluster isolation (dual-mode):**

- **SaaS (Kafka available):** Use Kafka consumer group partitioning by `cluster_uuid` hash. Each Kafka partition handles a fixed set of clusters, ensuring only one consumer processes a given cluster at a time — eliminating write races on `recommendation_sets`. During rebalance windows, recommendation persistence uses `INSERT ... ON CONFLICT DO UPDATE`, which is atomic (last-writer-wins, deterministic for same input).

- **On-prem (same Kafka path):** The cost-onprem chart deploys AMQ Streams (Kafka), so the on-prem ingestion path is identical to SaaS — Kafka consumer group partitioning by `cluster_uuid` hash provides the same cluster isolation guarantees. As a defense-in-depth measure (e.g., during consumer group rebalances), the same upsert pattern applies. On-prem deployments are typically single-instance, so contention is rare, but the Kafka partitioning and conflict-resolution mechanisms ensure correctness regardless of replica count.

### NFR-2: Bounded Memory

With Go computing digests in memory during CSV ingestion and storing only pre-aggregated results in PostgreSQL, the Go service has controlled memory usage. The primary memory consumers are:

- **CSV parse buffers** — bounded by `ROS_COPY_BATCH_SIZE` (default 5000 rows × ~200 bytes ≈ 1 MB per batch).
- **`COPY FROM` staging** — `pgx` streams to PostgreSQL; memory is bounded by batch size, not file size.
- **API response assembly** — bounded by pagination (`limit` default 10, max 1000). No unbounded result sets.
- **Go recommendation heuristics working memory** — GPU MIG lookup table (~10 KB), JVM detection rules, HPA analysis. All O(1) per workload.
- **Koku cost rate cache** — ~1 KB per cluster, refreshed hourly. At 1000 clusters: ~1 MB.
- **No unbounded maps** (avoid Kruize §19.2, §19.3 patterns).
- **Stream CSV processing** — do not load entire file into memory. Parse row-by-row, batch into `COPY FROM` buffers.

### NFR-2a: Circuit Breakers for External APIs

> **⚠️ Implementation status:** Circuit breakers are **NOT implemented**. External HTTP calls (RBAC, Koku cost API) use simple timeout-based `http.Client` with no breaker pattern. Failures are logged and result in degraded responses (no savings, 403) but do not trigger open/half-open state transitions.

All outbound HTTP calls to external services must use a circuit breaker pattern (e.g., `sony/gobreaker` or equivalent) to prevent cascading failures:

| External Service | Timeout | Circuit Breaker Settings | Fallback |
|---|---|---|---|
| **RBAC** | `ROS_RBAC_TIMEOUT` (5s) | Open after 5 consecutive failures, half-open after 30s | Return 424 (see OQ#1) |
| **Koku cost API** | 10s | Open after 3 consecutive failures, half-open after 60s | Serve recommendations without cost data (`estimated_savings_cents = NULL`) |
| **AWS Bulk Pricing API** | 30s (large download) | Open after 2 failures, half-open after 300s | Use cached catalog; log warning |
| **Azure/GCP catalog APIs** | 10s | Open after 3 failures, half-open after 60s | Use cached catalog; log warning |

Circuit breaker state transitions are logged and exposed via Prometheus counter `ros_circuit_breaker_state_transitions_total{service, from_state, to_state}`.

### NFR-3: Graceful Degradation

> **⚠️ Implementation status:** Consumer pause on PG down is **NOT implemented**. The current binary exits fatally (`log.Fatalf`) when the database is unreachable at startup. During runtime, individual message processing errors are logged and the message is committed (skip-and-continue). There is no circuit-breaker or pause/resume mechanism.

- If PostgreSQL is temporarily unavailable for digest writes: **pause the Kafka consumer** (stop calling `ReadMessage`) rather than buffering in memory. This creates natural backpressure — Kafka retains the messages on the broker. Resume consuming when the DB health check passes. This avoids OOM risk from unbounded in-memory buffering. The seek-back pattern (REQ-10.3 error handling) handles individual message failures; consumer pause handles sustained DB outages. Max pause duration before the consumer is considered stuck: `max.poll.interval.ms` (default 18 minutes, matching Koku). If exceeded, the consumer group rebalances — acceptable, as the DB outage affects all replicas.
- If digest upsert fails for a batch: serve recommendations from last successful digest data, log errors, retry on next Kafka message.
- If operator sends old-format CSV (no integer types, no replica count): parse as float, convert to integer at parse time, derive replicas from pod count, mark reduced accuracy in API.

### NFR-4: Observability

- **Prometheus metrics** (concrete catalog):
  - `ros_ingestion_messages_total{status}` — counter: Kafka messages processed (labels: `success`, `invalid`, `error`)
  - `ros_ingestion_rows_total` — counter: CSV rows inserted via COPY FROM
  - `ros_ingestion_duration_seconds{phase}` — histogram: per-message processing time (labels: `download`, `parse`, `copy`, `total`)
  - `ros_recommendation_compute_duration_seconds{cluster_uuid}` — histogram: `recommendAllWorkloads()` execution time per cluster
  - `ros_recommendation_compute_total{status}` — counter: recommendation batch runs (labels: `success`, `error`)
  - `ros_api_request_duration_seconds{method,path,status}` — histogram: HTTP request latency
  - `ros_api_requests_total{method,path,status}` — counter: HTTP requests
  - `ros_db_pool_connections{pool,state}` — gauge: connection pool utilization (pools: `ingestion`, `compute`, `api`; states: `active`, `idle`, `waiting`)
  - `ros_kafka_consumer_lag` — gauge: messages behind head of partition
  - `ros_circuit_breaker_state_transitions_total{service,from_state,to_state}` — counter: circuit breaker transitions (NFR-2a)
  - `ros_recommendation_oom_rate` — gauge: OOM rate for containers with active recs (REQ-10.6)
  - `ros_recommendation_stability` — gauge: recommendation value drift (REQ-10.6)
  - `ros_recommendation_adoption_rate` — gauge: applied recommendation percentage (REQ-10.7)
  - `ros_ingestion_late_data_fallback_total` — counter: late-arriving data batches that used conservative merge fallback (S3 originals unavailable)
- Structured logging with `org_id`, `cluster_uuid`, `workload`, `container` context.
- Health endpoint: `/status` must check PostgreSQL connectivity.
- **Kubernetes probes** (separate endpoints for different health semantics):
  - **Liveness** (`/healthz`): Process is alive and not deadlocked. Check: goroutine count < threshold (default 10,000), no stuck mutexes. Returns 200 if healthy, 503 if unhealthy. If this fails, Kubernetes restarts the pod.
  - **Readiness** (`/readyz`): Service can handle traffic. Check: PostgreSQL primary is reachable (ping), read replica is reachable (if configured), Kafka consumer is connected (group has active assignment). Returns 200 if all checks pass. If this fails, Kubernetes removes the pod from the Service (API requests stop routing to it, but Kafka consumption continues — readiness only affects HTTP traffic).
  - **Startup** (`/healthz` with `startupProbe` config): Same as liveness, but with a longer `failureThreshold × periodSeconds` to allow for golang-migrate execution. Recommended: `failureThreshold: 30, periodSeconds: 2` (60s startup budget).

### NFR-5: Configuration

All thresholds, percentile targets, safety margins, and half-life values must be configurable via environment variables with sensible defaults:

| Variable | Default | Purpose |
|---|---|---|
| `ROS_CPU_COST_PERCENTILE` | 60 | Cost model CPU percentile |
| `ROS_CPU_PERF_PERCENTILE` | 98 | Performance model CPU percentile |
| `ROS_MEM_COST_PERCENTILE` | 95 | Cost model memory percentile |
| `ROS_MEM_PERF_PERCENTILE` | 100 | Performance model memory percentile |
| `ROS_SAFETY_MARGIN` | 0.15 | Default safety margin |
| `ROS_MEM_MARGIN_MIN` | 0.15 | Min adaptive memory margin |
| `ROS_MEM_MARGIN_MAX` | 0.50 | Max adaptive memory margin |
| `ROS_IDLE_CPU_THRESHOLD` | 0.01 | Idle detection CPU threshold |
| `ROS_GPU_IDLE_THRESHOLD` | 0.02 | SM active below this → idle (2%) |
| `ROS_GPU_UNDERUTILIZED_SM_THRESHOLD` | 0.25 | SM active below this → underutilized (25%) |
| `ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD` | 0.15 | Tensor active below this → underutilized (15%) |
| `ROS_GPU_MEMBOUND_DRAM_THRESHOLD` | 0.60 | DRAM active above this → memory-bound (60%) |
| `ROS_GPU_MEMBOUND_TENSOR_THRESHOLD` | 0.15 | Tensor active below this (with high DRAM) → memory-bound |
| `ROS_GPU_FB_HEADROOM_FACTOR` | 1.20 | Frame-buffer headroom for MIG profile selection |
| ~~`ROS_TDIGEST_DELTA`~~ | ~~200~~ | ~~Removed (v2.0) — no t-digest; exact percentiles computed in Go~~ |
| `ROS_DECAY_HALFLIFE_MEDIUM` | 72 | Medium-term decay half-life (hours) |
| `ROS_DECAY_HALFLIFE_LONG` | 168 | Long-term decay half-life (hours) |
| `ROS_COPY_BATCH_SIZE` | 5000 | Rows per COPY FROM batch |
| ~~`ROS_COMPRESSION_POLICY_INTERVAL`~~ | ~~2 days~~ | ~~Removed (v2.0) — no TimescaleDB chunks; plain PostgreSQL partitions~~ |
| `ROS_DIGEST_RETENTION_DAYS` | 45 | Drop daily digest partitions older than this (REQ-2.6) |
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Drop recommendation history and quality partitions older than this (REQ-2.6) |
| `ROS_ENABLE_SHADOW_MODE` | false | Shadow mode: run new binary alongside old binary, compare recommendation outputs per-container and log divergence (REQ-1.12) |
| `ROS_ENABLE_REALTIME_RECS` | false | Enable on-demand recommendation computation |
| ~~`ROS_USE_TDIGEST`~~ | ~~auto~~ | ~~Removed (v2.0) — no t-digest; exact percentiles computed in Go~~ |
| `ROS_USE_OOM_FEEDBACK` | false | OOM-aware memory recs (enable after validation) |
| `ROS_ENABLE_GPU_RECS` | false | GPU recommendations |
| ~~`ROS_ENABLE_VM_RECS`~~ | ~~false~~ | Removed; VM plugin now controlled by `ROS_ENABLED_PLUGINS`/`ROS_DISABLED_PLUGINS` |
| `ROS_ENABLE_IDLE_DETECTION` | true | Idle workload detection |
| `ROS_ENABLE_COST_INTEGRATION` | false | Koku cost data for dollar savings |
| `ROS_KOKU_API_URL` | (empty) | Koku API base URL for cost data integration |
| `ROS_COST_CACHE_TTL` | 3600 | Cost rate cache TTL in seconds |
| `ROS_NODE_UNDERUTIL_THRESHOLD` | 0.30 | Node underutilization threshold (Tier 1) |
| `ROS_NODE_OVERCOMMIT_THRESHOLD` | 1.50 | Node overcommit ratio threshold (Tier 1) |
| `ROS_MACHINESET_TARGET_UTIL` | 0.70 | MachineSet target utilization for replica sizing |
| `ROS_MIN_MACHINESET_REPLICAS` | 2 | Minimum replica count for HA |
| `ROS_STALE_DATA_THRESHOLD_HOURS` | 48 | Mark recommendation stale after this many hours without data |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Delete stale recommendations older than N days |
| `ROS_ENABLE_EPHEMERAL_STORAGE` | false | Ephemeral storage recs (informational only — cadvisor metrics unreliable through OCP 4.21) |
| `ROS_ENABLE_NODEJS_RECS` | false | Node.js heap informational recs (OFF by default) |
| `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` | 0.6 | EMA-smoothed normalized imbalance threshold for stranded resource detection |
| `ROS_NODE_EMA_ALPHA` | 0.3 | EMA smoothing alpha for trend slope and stranded imbalance (higher = less smoothing) |
| ~~`ROS_ENABLE_NODE_RECS`~~ | _(removed)_ | Node recommendations now enabled unconditionally |
| `ROS_ENABLE_FLEET_SUMMARY` | false | Fleet-level cross-cluster summary endpoint |
| `ROS_INSTANCE_CATALOG_REFRESH_HOURS` | 24 | Cloud instance type catalog refresh interval |
| ~~`ROS_INGEST_TOKEN`~~ | ~~(empty)~~ | ~~Removed — on-prem uses same Kafka + S3 path as SaaS (see NFR-9)~~ |
| ~~`ROS_INGEST_MTLS_ENABLED`~~ | ~~false~~ | ~~Removed — no custom `/ingest` endpoint exists (see NFR-9)~~ |
| `ROS_DB_POOL_INGESTION_MAX` | 5 | Max connections for COPY FROM ingestion on primary (NFR-6) |
| `ROS_DB_POOL_COMPUTE_MAX` | 3 | Max connections for Go recommendation batch runs on primary (NFR-6) |
| `ROS_DB_POOL_API_MAX` | 10 | Max connections for API queries on read replica (NFR-6) |
| `ROS_DB_REPLICA_DSN` | (empty) | Read replica connection string; if unset, API queries use primary (NFR-6) |
| `ROS_DB_CONN_MAX_LIFETIME` | 1800 | Connection max lifetime in seconds (NFR-6) |

**Per-capability feature flags:** Each flag above controls a specific capability independently for fine-grained rollback. If OOM feedback causes issues in production, disable `ROS_USE_OOM_FEEDBACK` without touching CPU recommendations. The recommendation orchestrator checks each flag before invoking the corresponding engine.

### NFR-6: Database Connection Pooling

The Go binary manages three distinct connection workloads against two PostgreSQL endpoints:

**Primary (read-write):**
1. **COPY FROM ingestion** (long-running, high-throughput) — uses `pgx` native driver for streaming `COPY FROM STDIN`.
2. **Go recommendation computation** (medium-duration, batch) — runs `recommendAllWorkloads()` and related Go engines during ingestion cycles (digest reads + result upserts via SQL).

**Read replica (read-only):**
3. **API queries and recommendation reads** (short, low-latency) — uses GORM with `pgx` backend. This is the same read replica used by [GABI](https://github.com/app-sre/gabi) (Go Auditable DB Interface) for SRE/developer ad-hoc SQL access.

**Required strategy:**

- **Separate connection pools to separate endpoints:** Configure `pgx` with three pools:
  - Primary ingestion pool: `ROS_DB_POOL_INGESTION_MAX` (default 5) — bounded by concurrent Kafka consumer goroutines. COPY FROM holds a connection for the duration of the batch.
  - Primary compute pool: `ROS_DB_POOL_COMPUTE_MAX` (default 3) — bounded by concurrent recommendation batch runs. Each batch holds a connection for the duration of computation (~seconds per cluster).
  - Read replica API pool: `ROS_DB_POOL_API_MAX` (default 10) — bounded by concurrent API requests. Short-lived connections (~1-5ms per query). Reduced from 20 because the read replica is shared with GABI, which manages its own connection pool.
- **Read replica DSN:** `ROS_DB_REPLICA_DSN` configures the read replica endpoint. If unset, API queries fall back to the primary (useful for dev/test).
- **GABI coexistence:** [GABI](https://github.com/app-sre/gabi) connects to the same read replica independently (its own `pgx` pool, ~5-10 connections). Total read replica connection budget should account for both ros-ocp-backend API pool (10) + GABI pool (~10) + headroom. PgBouncer on the replica is the right place to enforce limits.
- **GABI compatibility (verified):** GABI is a pass-through SQL proxy — it sends SQL text to PostgreSQL and string-encodes all results. All tables use standard PostgreSQL types (`BIGINT`, `TIMESTAMPTZ`, `DATE`, `TEXT`, `UUID`, `REAL`, `BOOLEAN`, etc.) that GABI handles natively. SREs can run ad-hoc diagnostic queries against daily digest tables and recommendation tables through GABI with full SOC-2 audit logging. The only extension (`pg_partman`) is a DDL-management tool that doesn't affect query behavior — the read replica is a standard PostgreSQL instance managed by Crunchy PGO streaming replication.
- **PgBouncer sidecar (on-prem):** Crunchy PGO natively supports a PgBouncer sidecar (`spec.proxy.pgBouncer`). Enable it on the **read replica** — this is where connection multiplexing matters most (GABI + ros-ocp-backend API + potential future consumers). On the primary, PgBouncer is optional since only ros-ocp-backend writes to it.
- **Connection lifetime:** `ROS_DB_CONN_MAX_LIFETIME` (default 30 minutes) — prevent stale connections after PostgreSQL restarts or failovers.
- **Health checks:** All pools must validate connections before use (`pgx` `BeforeAcquire` callback).

| Variable | Default | Purpose |
|---|---|---|
| `ROS_DB_POOL_INGESTION_MAX` | 5 | Max connections for COPY FROM ingestion (primary) |
| `ROS_DB_POOL_COMPUTE_MAX` | 3 | Max connections for Go recommendation batch runs (primary) |
| `ROS_DB_POOL_API_MAX` | 10 | Max connections for API queries (read replica) |
| `ROS_DB_REPLICA_DSN` | (empty) | Read replica connection string; if unset, API queries use primary |
| `ROS_DB_CONN_MAX_LIFETIME` | 1800 | Connection max lifetime (seconds) |

### NFR-7: Backup and Disaster Recovery

Standard PostgreSQL backup strategies apply. The only optional extension (`pg_partman`) stores metadata in `partman` schema tables — included automatically by `pg_dump`.

**On-prem (Crunchy PGO):**
- Crunchy PGO provides automated pgBackRest backups via `spec.backups.pgbackrest`:
  - Full backup: weekly (configurable via `spec.backups.pgbackrest.repos[0].schedules.full`)
  - Incremental: daily
  - WAL archiving: continuous (enables point-in-time recovery)
- **Restore:** Standard Crunchy PGO restore from backup (`pgBackRest restore`). Partitioned tables and schema restore normally.
- **Note:** RTO/RPO targets are defined by the customer's SRE team. The Helm chart should expose `pgBackRest` configuration in `values.yaml` for customer customization.

**SaaS (managed DBaaS / AWS RDS):**
- AWS RDS, Azure Database for PostgreSQL, and other managed services provide automated point-in-time recovery.
- No additional backup configuration needed — the managed service handles it.

**Key consideration:** The `recommendation_history` partitioned table retains 90 days of historical recommendations. If this data is lost, it cannot be reconstructed from current metrics (only the most recent recommendation can be recomputed). Backup strategy must ensure this table is included.

### NFR-8: Daily Digest Table Schema Migration

Daily digest tables are standard PostgreSQL partitioned tables managed by `golang-migrate`. Schema changes use standard `ALTER TABLE` DDL:

**Adding a column:**
```sql
ALTER TABLE daily_container_digests ADD COLUMN new_metric_mc BIGINT;
```

**Adding a new percentile column:**
1. Add the column via `golang-migrate` migration: `ALTER TABLE daily_container_digests ADD COLUMN cpu_usage_p90_mc BIGINT;`
2. Update the Go ingestion code to compute and populate the new percentile during CSV processing.
3. Update the Go recommendation engine to read the new digest column when computing recommendations.
4. New data automatically includes the new column; historical rows have `NULL` until re-ingested.

**Backfilling historical data:** If a new column must be populated for historical dates, trigger a re-ingestion of S3 CSVs for the affected date range. The Go ingestion pipeline uses `INSERT ... ON CONFLICT DO UPDATE`, so re-processing is idempotent.

**Partition management — preferred: `pg_partman` (v5.x).**

`pg_partman` is supported on AWS RDS (since PG 12.5), Crunchy PGO, Azure, GCP Cloud SQL, Aiven, and bare metal. It automates partition creation and retention:

```sql
CREATE EXTENSION IF NOT EXISTS pg_partman;

-- Register each partitioned table (run once per table via golang-migrate)
SELECT partman.create_parent(
    p_parent_table := 'public.daily_container_digests',
    p_control := 'bucket_date',
    p_interval := '1 month',
    p_premake := 2,                      -- pre-create 2 months ahead
    p_start_partition := '2026-01-01'    -- first partition
);

-- Set retention (auto-drop partitions older than this)
UPDATE partman.part_config
SET retention = '45 days', retention_keep_table = false
WHERE parent_table = 'public.daily_container_digests';

-- For recommendation_history: 90 days retention
-- For recommendation_quality: 90 days retention
```

Maintenance (creating new partitions, dropping expired ones) runs via:
- **SaaS (AWS RDS):** `pg_cron` extension calls `partman.run_maintenance()` hourly.
- **On-prem (Crunchy PGO):** Go background goroutine calls `SELECT partman.run_maintenance()` every hour.

**Fallback (if `pg_partman` is unavailable):** The Go binary detects the absence of `pg_partman` at startup (query `pg_extension`) and manages partitions manually:
1. **At startup:** Check all digest and history tables. For each, ensure partitions exist for the current month and the next 2 months. Create missing partitions via `CREATE TABLE IF NOT EXISTS <table>_YYYYMM PARTITION OF <table> FOR VALUES FROM ('YYYY-MM-01') TO ('YYYY-MM+1-01')`.
2. **Hourly background task:** Pre-create future partitions, drop old partitions past retention.
3. The fallback is a simplified reimplementation of what `pg_partman` does — use `pg_partman` wherever available to avoid maintaining this code.

**When `golang-migrate` is needed:**
- Adding/removing/renaming columns on digest or recommendation tables
- Adding new tables or indexes
- **N/A (v4.0):** Recommendation algorithms are versioned in the Go binary, not via `CREATE OR REPLACE FUNCTION` in migrations
- `CREATE EXTENSION pg_partman` and initial `partman.create_parent()` calls for each table
- NOT needed for: partition creation/drop (handled by `pg_partman` or Go fallback at runtime)

### NFR-9a: Org Data Deletion

When a customer org is offboarded, all ROS data for that `org_id` must be deleted. This matches Koku's pattern where `Tenant.delete()` drops the entire tenant schema (`auto_drop_schema = True`), and a `remove_stale_tenants` Celery task cleans up tenants with no sources older than 2 weeks.

**ROS deletion strategy:**

1. **Trigger:** Listen for `Application.destroy` events on the Platform Sources Kafka topic (matching ros-ocp-backend's existing `sourcesListener` in `housekeeper/sourcesCleaner.go`). When all sources for an `org_id` are destroyed, schedule deletion.
2. **Deletion scope:** `DELETE FROM daily_container_digests WHERE org_id = ?`, `DELETE FROM recommendation_sets WHERE org_id = ?`, and all other tables with `org_id`. PostgreSQL partition pruning ensures efficient deletion when `bucket_date` ranges are included.
3. **Stale org cleanup:** Periodic background task (daily) scans for `org_id` values in `daily_container_digests` that have no corresponding active sources (matching Koku's `remove_stale_tenants` pattern). Delete data for orgs with no sources and no new data for >14 days.
4. **S3 / object storage:** ROS does not persist data in S3 — CSVs are read from Koku's S3 bucket and consumed. No S3 cleanup needed on the ROS side.
5. **Kafka:** No per-org Kafka cleanup needed — messages are consumed and offsets advanced. Topic retention handles old messages.
6. **Backups:** pgBackRest backups may contain deleted org data until the backup retention window expires. This is acceptable — Koku has the same behavior. For GDPR erasure requests, the backup retention window (default 14 days) defines the maximum time deleted data could be restored.

### NFR-9: On-prem Ingestion — Same as SaaS

~~**Previous plan:** Custom HTTP `/ingest` endpoint with mTLS/token auth.~~

**Updated:** The cost-onprem Helm chart deploys AMQ Streams (Kafka) and S3-compatible storage (NooBaa/ODF). The on-prem data flow is **identical** to SaaS:

1. Operator uploads tarball → Koku listener (ingress endpoint).
2. Koku's `ROSReportShipper` extracts ROS CSVs, uploads to S3, sends Kafka message to `hccm.ros.events`.
3. ros-ocp-backend-superpowers consumes from Kafka (same consumer code as SaaS).

No custom `/ingest` endpoint, no mTLS, no `ROS_INGEST_TOKEN` needed. Authentication is handled by Kafka ACLs and S3 credentials, same as SaaS. The `ROS_INGEST_MTLS_ENABLED` and `ROS_INGEST_TOKEN` env vars are removed.

---

## 20. Backward Compatibility and Migration

### Phase-by-Phase Compatibility

| Phase | Old Operator? | Old UI? | Migration Required? |
|---|---|---|---|
| 0 (fixes) | Yes | Yes | No |
| 1 (Go engine) | Yes | Yes | No — same API response format |
| 2 (Daily Digests) | Yes (float CSV — Go converts to integer at parse time) | Yes | Daily digest partitioned tables via golang-migrate |
| 3 (Recommendations) | Yes | Yes | Go recommendation engine and recommendation tables (DDL via golang-migrate) |
| 4 (OOM) | Needs new query | Yes | New CSV columns; graceful if absent |
| 5 (GPU) | Yes | **UI PENDING:** GPU notifications, underutilization alerts | No |
| 6 (idle, PVC, Go) | Some need new queries | **UI PENDING:** `additional_recommendations` section (idle, PVC, Go runtime) | New fields in API response |
| 7 (replicas) | Needs new queries | **UI PENDING:** `replicas` badge, `total_savings` column in recommendation table | New columns; fallback if absent |
| 8 (HPA, ephemeral, Node.js, ResourceQuota) | Needs new queries | **UI PENDING:** HPA optimization panel, ephemeral storage recs, Node.js heap recs, ResourceQuota advisor | New fields |
| 8b (VM) | Needs new queries | **UI PENDING:** VM recommendations page (`/recommendations/openshift/virtual-machines`), VM idle/oversized badges, IOPS profile display | New endpoints + fields |
| 8c (Node/MachineSet) | Needs new queries | **UI PENDING:** Node utilization page (`/recommendations/openshift/nodes`), MachineSet right-sizing page (`/recommendations/openshift/machinesets`), autoscaler alerts | New endpoints + fields |
| 9 (JVM/Quarkus) | Needs new queries | **UI PENDING:** JVM tuning panel (MaxRAMPercentage, GC policy, Quarkus threads) | New fields |
| 10 (remove Kruize) | N/A | No UI changes | Remove Kruize infra |

**UI work summary:** UI changes are required for Phases 5–9, 8b, and 8c. These are additive — existing UI remains functional. New UI features should be gated behind the same feature flags as the backend capabilities (e.g., GPU recs UI only shown when `ROS_ENABLE_GPU_RECS=true`). UI specifications are outside the scope of this backend requirements document but must be coordinated before each phase reaches production.

### Migration Strategy — Two-Binary Deployment

**Key decision:** Old operator → old binary (ros-ocp-backend + Kruize, completely unchanged). New operator → new binary (ros-ocp-backend-superpowers). Two separate binaries, two separate deployments.

**Routing:**
- **SaaS:** Both binaries run simultaneously in **different consumer groups**, so both receive every message from `hccm.ros.events`. An Unleash feature flag (`ros-ocp.use-superpowers-binary`), evaluated **per `org_id`**, controls which binary processes each message: the old binary processes when the flag is OFF (default), the new binary processes when ON. Exactly one binary processes each message — no double-processing, no dropped messages. Rollback is per-org_id — disable the flag and that org's data reverts to the old binary.
- **On-prem:** Only the new binary is deployed. The Helm chart does not include Kruize or the old ros-ocp-backend.

1. **Two-binary deployment:** The old ros-ocp-backend binary is completely untouched — zero regression risk. The new superpowers binary is a fresh Go module with clean architecture. Routing is external (deployment configuration), not internal (code branching).
2. **Per-capability feature flags** (see §22b, Risk Resolution R17):
   - `ROS_USE_OOM_FEEDBACK`, `ROS_ENABLE_GPU_RECS`, `ROS_ENABLED_PLUGINS`/`ROS_DISABLED_PLUGINS`, etc.
   - Each flag controls a specific capability independently for fine-grained rollback. No Kruize fallback exists — the new binary is self-contained.
3. **Dual-write period:** During Phase 2, write daily digests to both `workload_metrics` (JSONB) and the new digest tables. After validation, stop JSONB writes and drop the table (safe — `workload_metrics` is never read, per §27).
4. **JSONB → relational migration:** During Phase 2, write recommendations to both JSONB column and new relational columns. Switch API reads to relational columns. After validation, drop JSONB column.
5. **Digest backfill:** Not required for new data — Go computes digests during ingestion. Optionally, backfill from existing `workload_metrics` JSONB by re-processing historical S3 CSVs through the ingestion pipeline (idempotent via `INSERT ... ON CONFLICT DO UPDATE`).
6. **API compatibility:** There is no v2 — it's v1, extended. We never break backward compatibility. All new fields are additive. Existing fields retain their exact meaning, format, and default units. The API response structure matches v1 exactly (nested `amount`/`format` pairs for CPU and memory, `recommendation_engines.cost`/`performance` structure, numeric-keyed notification maps) — it's assembled from relational columns instead of JSONB, but the JSON shape is identical. **Internal storage uses millicores/KiB for efficiency, but the API layer converts to v1 default units** before responding. V1 defaults: CPU = cores (all endpoints), memory = bytes (list endpoints), memory = MiB (detail endpoints). The `cpu-unit` and `memory-unit` query parameters allow clients to override these defaults. The `cpu-unit`, `memory-unit`, and `true-units` query parameters remain standard (not deprecated) on all container and namespace endpoints with the same defaults as v1. **Legacy namespace routes are preserved as deprecated paths:** `GET /openshift/namespace/recommendations` and `GET /recommendations/openshift/namespace/{recommendation-id}` remain functional and route to the same handlers as the new canonical paths (`/recommendations/openshift/namespaces` and `/recommendations/openshift/namespaces/{recommendation-id}`). All `exclude[*]` and `filter[exact:*]` query parameters from v1 are supported on both legacy and canonical namespace endpoints. See `openapi-superpowers.json` for the full endpoint inventory.

---

## 21. Repository Map and Subteam Responsibilities

This design spans multiple repositories in the Koku ecosystem. Each repository is owned by a different subteam. This section defines **what changes** each repo needs, **when** those changes are needed, and **what the owning subteam should know**.

### Repositories In Scope

| Repository | Language | Owning Subteam | Phases Affected | Branch |
|---|---|---|---|---|
| `ros-ocp-backend` | Go 1.24 | ROS Backend | 0–10 (all) | `pgarciaq-rosocp-superpowers` |
| `koku` | Python/Django | Cost Management Backend | 7 (cost integration) | `pgarciaq-rosocp-superpowers` |
| `koku-ui` | TypeScript/React | Frontend | 3 (custom timeframe UI) | `pgarciaq-rosocp-superpowers` |
| `koku-metrics-operator` | Go 1.25 | Operator | 5 (GPU), 8b (VM), 8c (Node) | `pgarciaq-rosocp-superpowers` |
| `nise` | Python | Test Data | 2–3 (data generation) | `pgarciaq-rosocp-superpowers` |
| `cost-onprem-chart` | Helm/YAML | On-prem Deployment | 10 (remove Kruize) | `pgarciaq-rosocp-superpowers` |
| `iqe-ros-ocp-plugin` | Python/pytest | QE | 1–10 (IQE tests) | Coordinated after each phase |
| `iqe-cost-management-plugin` | Python/pytest | QE | 1–3 (ROS fixtures) | Coordinated after each phase |

### Repositories NOT In Scope

| Repository | Reason |
|---|---|
| `autotune` (Kruize) | Being replaced, not modified. Phase 10 removes it from deployment manifests. |

### Per-Repository Change Details

#### `ros-ocp-backend` — ROS Backend Team (Primary)

This is where ~90% of the implementation work happens. The superpowers engine is a **new Go binary** (`ros-ocp-backend-superpowers`) built within the same repository, deployed alongside the existing binary during the transition period (shadow mode). Key changes:

- **Phase 0:** Bug fixes to existing codebase (RBAC panics, Kafka error handling, HTTP timeouts).
- **Phase 1:** New recommendation engine package (`internal/engine/`): CPU/memory algorithms, percentile computation, dual-model output (cost/performance), customer-defined terms, notification system. New persistence layer writing to relational columns instead of JSONB.
- **Phase 2:** Daily digest pipeline: CSV ingestion → Go in-memory percentile computation → `daily_container_digests` table. New golang-migrate migrations (schema 22+). Replace `workload_metrics` JSONB with typed digest tables.
- **Phase 3:** Decay weighting, custom timeframe query parameters (`start_date`/`end_date`), `org_recommendation_terms` table, "read once, compute N terms" batch entry point.
- **Phase 10:** Remove Kruize HTTP client, drop `rosocp.kruize.recommendations` Kafka topic, simplify ingestion pipeline.

**Current stack:** Go 1.24, Echo v4, GORM, golang-migrate, confluent-kafka-go, Unleash, PostgreSQL 13 (upgrading to 16). ClowdApp with 4 deployments + cron job. 21 existing migrations.

#### `koku` — Cost Management Backend Team

No changes needed for Phases 0–3. First involvement is **Phase 7** (dollar savings):

- **Phase 7:** Expose an internal API endpoint (or extend existing `/reports/openshift/costs/`) that ros-ocp-backend can query to retrieve per-container cost rates (CPU $/core-hour, memory $/GiB-hour) and markup. The superpowers engine uses this to compute `estimated_savings_cents`. Circuit breaker pattern: if Koku is unreachable, savings field degrades to `null`.
- **Awareness:** The ROS API (`/recommendations/openshift`) will return richer data (new fields like `daily_digest_metadata`, `engine_version`, custom timeframe parameters). These are additive — no breaking changes to shared API paths.

#### `koku-ui` — Frontend Team

Can begin work in **Phase 3** once the API contract is defined, in parallel with backend implementation:

- **Phase 3:** Add custom timeframe controls to the Optimizations page. The API will accept `start_date` and `end_date` query parameters on `/recommendations/openshift`. The frontend needs date picker / duration selector UI components that pass these parameters. The default behavior (no date params = standard 1d/7d/15d terms) is unchanged.
- **Phase 7+:** Display `estimated_savings_cents` when available (field is `null` when cost model is not configured or Koku is unreachable).
- **Phase 8b+:** New recommendation types (VM, node, MachineSet) will need new UI views or tabs.

#### `koku-metrics-operator` — Operator Team

No changes needed for Phases 0–3. First involvement is **Phase 5** (GPU):

- **Phase 5:** Add NVIDIA GPU Prometheus queries for ROS (utilization, memory, MIG slices). The `cost-7178-mig-metrics` branch has existing work toward this.
- **Phase 8b:** Add VM-related Prometheus queries (vCPU usage, memory, guest OS info from KubeVirt CRs).
- **Phase 8c:** Add node-level Prometheus queries (allocatable/capacity, request sums) and MachineSet queries.
- **Output format:** Operator produces CSV files in tar.gz payloads with `manifest.json`. New metric types add new CSV files to the payload — the manifest `files` array is extended, not replaced.

#### `nise` — Test Data Team

May need updates starting in **Phase 2–3**:

- **Phase 2–3:** If daily digest ingestion changes how test data is consumed, nise static YAMLs and OCP report generation may need corresponding updates. At minimum, new nise datasets for custom timeframe testing (30d, 60d, 90d duration data).
- **Phase 4+:** OOM event data, GPU metrics, PVC usage, Go runtime metrics — nise needs to generate these for IQE tests.

#### `cost-onprem-chart` — On-prem Deployment Team

No changes until **Phase 10**:

- **Phase 10:** Remove Kruize deployment from the Helm chart. Update ros-ocp-backend deployment to use the superpowers binary. Update health checks, resource limits, and service configuration.
- **Pre-Phase 10:** The two-binary deployment model (shadow mode) may require temporary chart updates if on-prem wants to opt-in to the new engine early.

#### IQE Repositories — QE Team

IQE test updates are coordinated **after** each backend phase ships to staging. See §22 of the [TDD Test Plan](./test-plan.md) for the detailed IQE integration test plan (~63 test changes across all phases).

- **Phase 1–2:** Remove 3 Kruize direct tests, update ~10 response format tests.
- **Phase 3:** Add 5 custom timeframe tests, un-skip ~17 namespace filter tests.
- **Phase 4+:** New tests per phase (OOM, GPU, PVC, replicas, savings, HPA, VM, node).

### Implementation Coordination Timeline

```
Phase 0 (Weeks 1–2):    ros-ocp-backend only
Phase 1 (Weeks 3–8):    ros-ocp-backend only
Phase 2 (Weeks 3–10):   ros-ocp-backend + nise (test data)
Phase 3 (Weeks 5–12):   ros-ocp-backend + koku-ui (parallel) + nise
                         IQE repos (after staging deploy)
Phase 5 (Weeks 10–14):  ros-ocp-backend + koku-metrics-operator (GPU queries)
Phase 7 (Weeks 10–14):  ros-ocp-backend + koku (cost API integration)
Phase 8b (Weeks 12–18): ros-ocp-backend + koku-metrics-operator (VM queries)
Phase 8c (Weeks 14–20): ros-ocp-backend + koku-metrics-operator (Node queries)
Phase 10 (Weeks 18–22): ros-ocp-backend + cost-onprem-chart (remove Kruize)
```

---

## 22. Testing Strategy

### Unit Tests

| Component | Coverage Target | Key Tests |
|---|---|---|
| CPU recommendation engine | 95% | 1-core boundary, percentile accuracy, safety margin, decay weighting |
| Memory recommendation engine | 95% | OOM backoff, adaptive margin, trend detection, separate request/limit |
| GPU recommendation engine | 90% | All GPU models, MIG profiles, underutilization threshold |
| Go percentile computation | 95% | `slices.Sort()` on `[]int64`, exact percentile selection by index, daily digest upsert, `INSERT ... ON CONFLICT DO UPDATE` idempotency |
| Idle/PVC detection | 90% | Threshold edge cases, zero-usage detection |
| Node utilization engine | 90% | Underutilized/overcommitted thresholds, stranded resource detection, trend slope |
| MachineSet right-sizing | 85% | Replica count formula, instance type catalog lookup, hysteresis, bare-metal skip |
| MachineAutoscaler analysis | 85% | Saturated/idle/flapping detection, missing autoscaler handling |
| CSV parsing | 90% | Integer types, float fallback, missing columns, malformed data |
| COPY FROM ingestion | 80% | Batch sizing, integer conversion, error handling, null columns |

### Integration Tests

| Test | Environment | Validates |
|---|---|---|
| End-to-end ingestion | Docker Compose (ros-ocp + PostgreSQL) | CSV → Go aggregation → daily digest upsert → Go recommendation engine → relational upsert |
| API response format | Docker Compose | Backward compatibility with existing UI expectations |
| Operator compatibility | Mock CSV generator | Old-format CSV, new-format CSV, mixed |
| Custom timeframes | Docker Compose | Digest merge over custom date range with decay weighting |

### Performance Tests

| Test | Scale | Target |
|---|---|---|
| Ingestion throughput | 10K containers, 4 intervals | < 5 seconds total |
| Recommendation computation | 10K containers, 91-day digests | < 2 seconds total |
| API latency | 1000 concurrent requests | p99 < 10 ms |
| Memory footprint | 50K containers steady state | < 100 MB RSS |

### Regression Tests

Maintain a golden dataset (generated with nise) that produces known recommendations. After each algorithm change, verify recommendations match expected values within tolerance (±5% for CPU/memory amounts, exact match for notification types).

---

## 23. Open Questions (All Resolved)

All questions have been resolved (18 RESOLVED, 1 DEFERRED to deployment planning). See resolutions inline:

| # | Question | Options | Decision Needed By |
|---|---|---|---|
| 1 | ~~RBAC fail-open or fail-closed when service unreachable?~~ | **RESOLVED** — Fail-closed, matching both Koku and ros-ocp-backend patterns. **Koku** returns HTTP 424 (Failed Dependency) with a JSON error body when RBAC is unreachable or returns ≥500, logged via `RBAC_CONNECTION_ERROR_COUNTER`. **ros-ocp-backend** intends 403 but has a nil-dereference bug on transport errors (effectively 500). The new binary should match **Koku's 424 pattern**: catch RBAC connection errors, return 424 with structured error body, increment a Prometheus counter. Add `ROS_RBAC_TIMEOUT` (default 5s) for the HTTP client. Cache successful RBAC responses (30s TTL, matching Koku's `RBAC_CACHE_TTL`). Org-admins bypass RBAC (matching Koku). When RBAC is disabled (`RBAC_ENABLE=false`), skip the middleware entirely (matching current ros-ocp-backend). | ~~Phase 0~~ |
| 2 | ~~Feature flag for native engine rollout?~~ | **RESOLVED** — Per-capability feature flags (R17, NFR-5). Each capability has an independent toggle. | ~~Phase 1~~ |
| 3 | ~~Database topology?~~ | **RESOLVED:** Single primary + read replica. Primary handles writes only (daily digest upserts, Go recommendation batch writes). Read replica handles all reads: ros-ocp-backend API queries + [GABI](https://github.com/app-sre/gabi) ad-hoc SQL access (SRE/developer auditable queries). PgBouncer sidecar on the read replica (Crunchy PGO `spec.proxy.pgBouncer`) multiplexes connections from both consumers. Crunchy PGO manages replication via `spec.instances[1]` (standard PG streaming replication). Single pgBackRest backup covers both. | ~~Phase 2~~ |
| 4 | ~~TimescaleDB Cloud vs self-managed?~~ | **RESOLVED (v2.0):** No longer applicable — architecture uses plain PostgreSQL 16+ with optional `pg_partman`. SaaS uses AWS RDS. On-prem uses Crunchy PGO or any PostgreSQL instance. No extension decisions needed. | ~~Resolved~~ |
| 5 | ~~JSONB → relational: atomic migration or dual-write period?~~ | **RESOLVED** — Dual-write. This is already specified in the migration strategy (§20, item 4): during Phase 2, write recommendations to both JSONB and new relational columns simultaneously. Switch API reads to relational columns. After validation, drop JSONB. The dual-write approach is safer because: (1) instant rollback — switch reads back to JSONB; (2) validation — compare JSONB vs relational outputs for consistency; (3) zero-downtime — no migration window needed. The `workload_metrics` JSONB table is never read (§27), so it's purely write-side, but `recommendation_sets` JSONB columns are actively read by the API — dual-write there is critical. | ~~Phase 2~~ |
| 6 | ~~Memory limit = request default recommendation?~~ | **RESOLVED** — No, limit ≠ request by default. The document already specifies separate request and limit values for memory (REQ-1.5, REQ-1.6): `memory_request = max(usage) × (1 + adaptive_margin)` and `memory_limit = memory_request × limit_multiplier`. The `limit_multiplier` defaults to 1.0 (Guaranteed QoS), but the **cost model** uses p95 for requests while the **performance model** uses p100 (max) — so the cost model naturally produces `request < limit` for workloads with usage spikes. The `limit_multiplier` is configurable per profile (REQ-1.15). For customers who want Burstable QoS (the common case), a `limit_multiplier` of 1.2–1.5 is appropriate. Default: **1.0** (Guaranteed) for safety — customers can tune via profiles. | ~~Phase 4~~ |
| 7 | ~~GPU underutilization threshold?~~ | **RESOLVED** — **Multi-metric DCGM classification tree** (not a single utilization percentage). Implementation uses SM active, tensor pipe active, and DRAM active with six workload classes and distinct actions (see REQ-5.4, [gpu-classification.md](gpu-classification.md)). Rationale: a single 10% threshold conflates memory-bound inference, idle allocation, and compute-underutilized training jobs; DCGM profiling metrics disambiguate these patterns. Thresholds configurable via `ROS_GPU_*` env vars (NFR-5 table). | ~~Phase 5~~ |
| 8 | ~~Idle detection threshold configurable per namespace?~~ | **RESOLVED** — **No, global only** for MVP. A single `ROS_IDLE_CPU_THRESHOLD` (default 1m) and `ROS_IDLE_MEMORY_THRESHOLD` (default 1 MiB) apply uniformly to all namespaces. Per-namespace thresholds would require a configuration table, a management API, and UI — unnecessary complexity for the first release. If post-MVP feedback shows specific namespaces need different thresholds (e.g., monitoring agents with legitimately low CPU), the global threshold can be tuned, or per-namespace overrides can be added to `recommendation_profiles`. | ~~Phase 6~~ |
| 9 | ~~HPA optimization: is combined VPA+HPA a product priority?~~ | **RESOLVED** — **No, defer.** VPA+HPA combined optimization is a complex problem with known conflicts: VPA adjusts resource requests per pod, while HPA adjusts replica count based on utilization — changing requests shifts the utilization ratio HPA observes, creating feedback loops. Kubernetes upstream is working on "In-place Pod Vertical Scaling" (KEP-1287, beta in 1.33) which will eventually make VPA+HPA safer, but it's not yet stable. **For MVP:** (1) Detect HPA-managed workloads via `horizontalpodautoscaler` metadata and emit an `INFO_HPA_ACTIVE` notification. (2) Still produce resource recommendations (users may want to right-size the per-pod resources even under HPA). (3) Do NOT produce replica count recommendations for HPA-managed workloads — HPA already controls that. (4) Phase 8a HPA-specific features (REQ-8a.*) remain scoped to detecting HPA saturation and providing informational notifications, not overriding HPA behavior. | ~~Phase 8~~ |
| 10 | ~~JVM/Quarkus detection reliability on target clusters?~~ | **RESOLVED** — **Reliable enough for informational recommendations.** JVM detection uses two complementary signals: (1) **Container image labels** (from `kube_pod_container_info`): image names containing `java`, `jboss`, `wildfly`, `quarkus`, `spring-boot`, `openjdk` — this catches ~80% of JVM workloads. (2) **JVM metrics** (from JMX exporter or Prometheus JVM client): `jvm_memory_bytes_used`, `jvm_gc_*`, `jvm_threads_*` — these are definitive when present but require the workload to export them. Detection is best-effort: if neither signal is available, the workload is treated as generic (no JVM-specific recommendation). False positives are harmless (the recommendation says "if this is a JVM workload, consider setting -Xmx to {value}" — purely informational, gated behind `ROS_ENABLE_NODEJS_RECS` equivalent for JVM). False negatives mean we miss some JVM workloads but produce no harm. No staging measurement needed — the algorithm is self-correcting. | ~~Phase 9~~ |
| 11 | ~~Operator query budget: ~22 new queries acceptable?~~ | **RESOLVED** — Single atomic operator update (R13). Old operator → old binary, new operator → new binary. Budget increase (~47%) is acceptable since the new operator ships as a new version. | ~~Cross-phase~~ |
| 12 | ~~Replica count: avg or max for HPA workloads?~~ | **RESOLVED** — **All three: min, max, and avg.** Store and expose `replica_count_min`, `replica_count_max`, and `replica_count_avg` in the recommendation. Rationale: (1) **avg** shows the typical steady-state, useful for cost estimation ("most of the time you need N replicas"). (2) **max** shows peak demand, useful for capacity planning ("you need at least N replicas to handle peaks without degradation"). (3) **min** shows the floor, useful for evaluating total recommendation impact and understanding how much scale-down is safe. The cost impact calculation should use `avg × per_pod_cost` for "typical monthly cost" and `max × per_pod_cost` for "peak capacity cost." For non-HPA workloads with fixed replica counts, min = max = avg = current count, and the recommendation is "consider reducing to N" or "consider increasing to N." | ~~Phase 7~~ |
| 13 | ~~PL/pgSQL vs Go boundary: team comfortable with SQL rec logic?~~ | **SUPERSEDED (v4.0):** **All-Go recommendation engine.** Weighted percentiles, decay, margins, trend/idle detection, and persistence orchestration run in Go (read-once / compute-N-terms); PostgreSQL stores digests and results (`SELECT` / `INSERT` / `UPDATE` / `DELETE` only). *(Earlier draft: hybrid PL/pgSQL + Go with server-side `recommend_cpu`-style functions.)* | ~~Phase 1/3~~ |
| 14 | ~~Shadow-mode: full comparison or canary subset?~~ | **RESOLVED** — **Full comparison on all clusters.** Shadow mode runs the new Go recommendation engine alongside the old Kruize pipeline and compares outputs. This should run on all clusters, not a subset, because: (1) Per-cluster Go recommendation batches are bounded and fast relative to the ingestion interval. (2) A canary subset could miss edge cases (specific workload patterns, unusual cluster configurations). (3) The comparison is read-only — shadow results are logged/metricked but never served to users. (4) The per-org_id Unleash routing already provides the gradual rollout mechanism for the *serving* path — shadow mode validates the *computation* path. Run shadow on all clusters during Phase 1, compare outputs via structured logs and a `recommendation_shadow_diff` Prometheus histogram, then proceed to Phase 3 production serving with confidence. | ~~Phase 1~~ |
| 15 | ~~SQL function versioning: golang-migrate or app startup?~~ | **SUPERSEDED (v4.0):** **golang-migrate for schema; Go binary for algorithms.** Tables, indexes, and partitions use golang-migrate. Recommendation logic is versioned with the Go service (image/git). Same DDL rationale applies: single source of truth, `down` migrations for rollback, run once per deploy. *(Earlier draft: PL/pgSQL `CREATE OR REPLACE FUNCTION` in migrations.)* | ~~Phase 3~~ |
| 16 | ~~VM resize hysteresis: 40% threshold appropriate?~~ | **RESOLVED** — **Yes, 40% default, configurable via `ROS_VM_RESIZE_HYSTERESIS`**. VM resizing requires a VM restart (live migration does not resize vCPU/memory), so the threshold must be conservative to avoid churn. 40% means the recommendation only triggers when the VM is at least 40% oversized or undersized relative to observed usage. This avoids "resize by 1 vCPU" noise. For comparison: Azure Advisor uses 45% for VM right-sizing, AWS Compute Optimizer uses a similar "significant" threshold. 25% would be too aggressive for VMs that restart on resize. | ~~Phase 8b~~ |
| 17 | ~~VM IOPS: actionable storage class recs or informational only?~~ | **RESOLVED** — **Informational only.** Storage class recommendations require a catalog of available storage classes per cluster, which varies widely (Ceph, NFS, local, cloud provider EBS/PD, etc.). Building a storage class catalog is out of scope for MVP. The recommendation should be: emit `INFO_VM_STORAGE_IOPS_HIGH` notification when VM disk I/O consistently exceeds the underlying storage class's rated IOPS (if known from PV annotations), with text like "Consider a higher-performance storage class for disk '{disk_name}'." No specific storage class is suggested. | ~~Phase 8b~~ |
| 18 | ~~VM idle threshold: guest OS-aware (Windows baseline differs)?~~ | **RESOLVED** — **No, uniform threshold for MVP.** Use a single `ROS_VM_IDLE_MEMORY_THRESHOLD` (default 0.5 GiB). Windows VMs typically idle at 1.5-2 GiB, but detecting guest OS from Kubernetes metrics is unreliable (requires `guest-agent` reporting via KubeVirt labels, which not all VMs have). A uniform 0.5 GiB threshold will produce some false "not idle" for Windows VMs — acceptable for MVP since the recommendation is informational. Post-MVP, if KubeVirt's `guestosinfo` is reliably populated, add OS-aware thresholds. | ~~Phase 8b~~ |
| 19 | ~~Operator query budget: ~12 additional VM queries acceptable?~~ | **RESOLVED** — **Yes, acceptable.** The ~12 VM queries add ~1.8s to the hourly reconciliation cycle (at ~150ms/query avg), negligible vs. the 60-minute interval. The operator already runs ~73 queries + ~22 from earlier phases = ~95 total; adding 12 brings it to ~107. Prometheus/Thanos can handle this load — each query scans a small time window (1 hour) over a bounded number of VMs per cluster. The operator team can phase these alongside backend Phase 8b delivery. | ~~Phase 8b~~ |
| 20 | ~~VM instance type recommendation: Phase 8b or defer?~~ | **RESOLVED** — **Phase 8b, if instance type catalog is available from Phase 8c.** VM instance type recommendations (e.g., "this VM is using 2 vCPU but only needs 1 — consider m6i.large → m6i.medium") depend on the cloud instance type catalog (REQ-8c.6). Phase 8c (Node/MachineSet, weeks 14-20) builds the catalog. If Phase 8c delivers before 8b is complete, wire VM instance type recs into 8b. If 8c is delayed, defer VM instance type recs to a follow-up — the core VM right-sizing (vCPU, memory, IOPS notifications) stands on its own without instance type mapping. | ~~Phase 8b~~ |

---

## 23b. Risk Resolutions

Risks identified during the 2026-03-26 review, with their resolutions.

| # | Risk / Gap | Resolution | Affected REQs |
|---|---|---|---|
| R1 | `rollup()` does not support weighted merge for exponential decay | **v4.0:** Decay-weighted merge runs in Go over daily digest rows loaded once per window (~1–3% approximation vs. a theoretical continuous merge, within ±5% tolerance). *(Legacy note: earlier draft used per-day SQL CTEs.)* | REQ-3.2 |
| R2 | Missing storage for GPU, PVC, HPA, OOM, runtime metrics | Separate daily digest tables per metric domain: `daily_gpu_digests`, `daily_pvc_digests`. Regular table for runtime info. OOM count included in `daily_container_digests`. | REQ-2.1, §18 |
| R3 | `container_recommendations` vs `recommendation_sets` naming inconsistency | Standardized on `recommendation_sets` (existing table name). All references fixed. | REQ-1.10, REQ-1.11, §18 |
| R4 | No daily digest freshness guarantee specified | Daily digests are upserted by Go during each ingestion cycle. `pg_partman` manages partition creation and retention. Short-term recs use the most recent digest rows — staleness bounded by the Kafka ingestion interval (typically ≤1 hour). | REQ-3.1 |
| R5 | Dropping historical tables with no replacement | Added `recommendation_history` partitioned table (time-series of all past recs), retained 90d (old partitions dropped). | §18, REQ-10.3 |
| R6 | No RBAC for new endpoints | Same `cost-management:ros:*:read` permission as existing endpoints. No new RBAC resources needed. | §17 |
| R7 | No pagination for new endpoints | All list endpoints: `limit`/`offset`/`sort_by`/`order_by`, matching existing patterns. | §17 |
| R8 | No dollar savings integration (except GPU) | New Go module `internal/costdata/` queries Koku REST API, caches hourly. `estimated_savings_cents` in response. | REQ-7.5 (new) |
| R9 | COPY FROM partial failure on invalid rows | Go-side validation before COPY FROM: parse, validate types, skip invalid rows with structured logging. COPY never sees bad data. | REQ-2.1 |
| R10 | Concurrent recommendation writes (race conditions) | Both SaaS and on-prem: Kafka consumer group partitioning by `cluster_uuid` hash ensures one consumer per cluster. `INSERT ... ON CONFLICT DO UPDATE` as defense-in-depth during rebalances. | NFR-1 |
| R11 | ~~Continuous aggregate limitations~~ | **Resolved (v2.0):** No continuous aggregates — daily digest tables are standard PostgreSQL partitioned tables, fully alterable with `ALTER TABLE`. No schema migration complexity. | — |
| R12 | ~~TimescaleDB availability in production~~ | **Resolved (v2.0):** No TimescaleDB needed — plain PostgreSQL 16+ (AWS RDS compatible). | — |
| R13 | Operator changes span multiple phases | **Resolved:** Single atomic operator update. Old operator → old binary (ros-ocp-backend + Kruize). New operator → new binary (ros-ocp-backend-superpowers). Two separate binaries, external routing. On-prem: new binary only. **Operator owned by separate team.** Adding ~40 new PromQL queries to the existing ~73 increases query load by ~55%. At ~150ms/query average, this adds ~6 seconds to the hourly reconciliation cycle — negligible vs. the 60-minute interval. New queries can be phased alongside backend phases (operator team implements queries for each phase as needed, not all at once). | §2, §20 |
| R14 | UI changes needed across phases 5-9 | Added "UI PENDING" notes per phase with specific features needed. UI gated behind same feature flags as backend. | §20 |
| R15 | 22-week timeline with tight dependencies | **Staffing:** 1 senior engineer + Claude Opus 4.6+ (AI pair programmer). Claude accelerates mechanical work ~3-5x, algorithm implementation ~2x, integration/debugging ~1.5x. **Velocity checkpoint:** Track progress through Phases 0-2 (weeks 1-10). If on track by week 10, remaining phases are lower-risk. If behind, defer Phases 8c and 9. | — |
| R16 | No recommendation quality metric | Simplified: OOM rate + recommendation stability (value drift between cycles) + adoption detection (REQ-10.7). Dropped `accuracy_score` (requires application-level feedback unavailable in this architecture). | REQ-10.6 (updated), REQ-10.7 (new), §18 |
| R17 | No granular rollback plan | Per-capability feature flags (10 flags). Each controls one capability independently. Disable OOM feedback without touching CPU recs, etc. | NFR-5, §20 |
| R18 | AWS Bulk Pricing JSON is ~2 GB uncompressed | Use a streaming JSON parser (Go `encoding/json.Decoder` with `Token()` API) to avoid loading the entire file into memory. Parse product entries one at a time, extract only `vcpu`, `memory`, `instanceType`, `instanceFamily`. Estimated peak memory: ~50 MB for index structures. Implementation detail deferred to specification/implementation plan. | REQ-8c.6 |
| R19 | Kafka routing during SaaS transition | **Resolved:** Both binaries in separate consumer groups; Unleash flag `ros-ocp.use-superpowers-binary` evaluated per `org_id` determines which binary processes each message. Gradual per-org rollout, instant per-org rollback. | §2, §20 |
| R20 | ~~Memory p100 approximation~~ | **Resolved (v2.0):** No t-digest — all percentiles are exact (computed in Go via `slices.Sort()`). MAX values stored as explicit columns (`memory_usage_max_kib`, `memory_rss_max_kib`). | — |
| R21 | Missing recommendation_profiles table | **Resolved:** Added `recommendation_profiles` table to §18 with `cost` and `performance` seed rows (percentile model selection). Added `org_recommendation_terms` table for customer-defined term window overrides (defaults: 1d/7d/15d hardcoded in Go). | §18, REQ-1.8, REQ-1.15 |

---

## 24. Appendix: Requirement Traceability Matrix

| Requirement | Analysis Section | Phase | Priority | Operator Change? | DB Migration? | API Change? |
|---|---|---|---|---|---|---|
| REQ-0.1 RBAC nil panic | §20.1 | 0 | Critical | No | No | No |
| REQ-0.2 RBAC split panic | §20.12 | 0 | Critical | No | No | No |
| REQ-0.3 API 500 on DB error | §20.2 | 0 | High | No | No | Yes (error format) |
| REQ-0.4 Kafka assertion panic | §20.3 | 0 | High | No | No | No |
| REQ-0.5 Kafka subscribe failure | §20.4 | 0 | High | No | No | No |
| REQ-0.6 HTTP timeouts | §20.6–7 | 0 | High | No | No | No |
| REQ-0.7 Poison message DLQ | §20.9 | 0 | Medium | No | No | No |
| REQ-0.8 GORM Where bug | §20.11 | 0 | Medium | No | No | No |
| REQ-0.9 Date error handling | §20.8 | 0 | Medium | No | No | No |
| REQ-0.10 Deterministic order | §20.13 | 0 | Medium | No | No | No |
| REQ-0.11 Logging reduction | §20.5 | 0 | Medium | No | No | No |
| REQ-0.12 SendMessage retry | §20.10 | 0 | Medium | No | No | No |
| REQ-1.1 CPU: no 1-core split | §12 | 1 | Critical | No | No | No |
| REQ-1.2 CPU: no per-pod est. | §12 | 1 | Critical | No | No | No |
| REQ-1.3 CPU: cost/perf models | §12 | 1 | High | No | No | No |
| ~~REQ-1.4 CPU: confidence bounds~~ | §12 | ~~1~~ | ~~Medium~~ | — | — | — DEFERRED to post-MVP |
| REQ-1.5 Mem: basic impl | §16 | 1 | High | No | No | No |
| REQ-1.6 Mem: cost/perf models | §16 | 1 | High | No | No | No |
| REQ-1.7 Dual model output | §12, §16 | 1 | High | No | No | No |
| REQ-1.8 Term support | E.6, E.9 | 1 | High | No | No | No |
| REQ-1.9 Notifications | E.9 | 1 | High | No | No | No |
| REQ-1.10 Rec persistence (Go + SQL upsert) | E.4, §27, §29 | 1 | High | No | Yes | No |
| REQ-1.11 Batch rec entry point | §29 | 1 | High | No | Yes | No |
| REQ-1.12 Shadow-mode validation | §29 | 1 | High | No | No | No |
| REQ-1.13 Namespace recommendations | §6 | 1 | High | No | Yes | Yes |
| REQ-1.14 Notification-codes endpoint | Fifth review F12 | 1 | Low | No | No | Yes |
| REQ-1.15 Recommendation profiles | Risk review R21 | 1 | Medium | No | Yes | Yes |
| REQ-2.1 Daily digest pipeline | §3, §25, §28 | 2 | Critical | No | Yes | No |
| REQ-2.2 Multi-tenancy (org_id) | §3 | 2 | High | No | No | No |
| REQ-2.3 Integer types | §9 | 2 | High | No | No | No |
| REQ-2.4 Drop workload_metrics | §3, §25, §27 | 2 | High | No | Yes | No |
| REQ-2.5 JSONB → relational cols | §27 | 2 | High | No | Yes | No |
| REQ-2.6 Partition retention | §7, §28 | 2 | Medium | No | No | No |
| REQ-2.7 Partitioned digest DDL (structure) | Phase dep. resolution | 2 | High | No | Yes | No |
| REQ-3.1 Daily digests + pre-computed percentiles | §10, §25, §28 | 3 | Critical | No | Yes | No |
| REQ-3.2 Go recommendation engine | §10, §12, §29 | 3 | High | No | Yes | No |
| REQ-3.3 Custom timeframes | §10, IMPL-PRD99 | 3 | High | No | No | No |
| REQ-3.4 Real-time recs (Go on-demand) | §25.5, §29 | 3 | Medium | No | No | No |
| REQ-3.5 Go engine testing | §29 | 3 | High | No | No | No |
| REQ-4.1 OOM collection | §16 | 4 | High | Yes | No | No |
| REQ-4.2 Adaptive margin | §16 | 4 | High | No | No | No |
| REQ-4.3 OOM backoff | §16 | 4 | High | No | No | No |
| REQ-4.4 Trend detection | §16 | 4 | Medium | No | No | Yes |
| ~~REQ-4.5 Multi-timescale~~ | §16 | ~~4~~ | ~~Medium~~ | — | — | — REMOVED (redundant with decay) |
| REQ-4.6 Separate req/limit | §16 | 4 | High | No | No | Yes |
| REQ-5.1 GPU MIG port | §17 | 5 | High | No | No | No |
| REQ-5.2 B200/RTX PRO fix | §17 | 5 | Critical | No | No | No |
| REQ-5.3 Frame buffer fix | §17 | 5 | Critical | No | No | No |
| REQ-5.4 GPU underutil | §17 | 5 | High | No | No | Yes |
| REQ-5.5 Multi-GPU | §17 | 5 | Low | Yes | No | Yes |
| REQ-5.6 Cost-aware GPU recs | Triage Mar 2026 | 5 | Medium | No | No | Yes (Koku API) |
| REQ-6.1 Idle detection | §23.1 | 6 | High | No | No | Yes |
| ~~REQ-6.2 QoS class~~ | §23.5 | ~~6~~ | ~~Medium~~ | — | — | — DEFERRED (implicit from CPU/memory recs) |
| REQ-6.3 PVC right-sizing | §23.2 | 6 | High | No | No | Yes |
| REQ-6.4 Go GOMAXPROCS | §23.4 | 6 | High | Yes | No | Yes |
| REQ-7.1 Replica queries | §26 | 7 | High | Yes | Yes | No |
| REQ-7.2 Store replicas | §26 | 7 | High | No | Yes | Yes |
| REQ-7.3 Total savings | §26 | 7 | High | No | No | Yes |
| REQ-7.4 Fallback replicas | §26 | 7 | Medium | No | No | No |
| REQ-7.6 Fleet-summary endpoint | Third review | 7 | Medium | No | No | Yes |
| REQ-8.1 HPA optimization | §23.3 | 8 | High | Yes | No | Yes |
| REQ-8.2 Ephemeral storage | §23.6 | 8 | Low (informational, pending cadvisor fix) | Yes | No | Yes |
| REQ-8.3 Node.js heap | §23.7 | 8 | Low (informational only) | Yes | No | Yes |
| REQ-8.4 ResourceQuota | §23.8 | 8 | Medium | Yes | Yes | Yes (namespace + CRQ) |
| REQ-9.1 JVM detection | §18 | 9 | High | Yes (optional) | No | No |
| REQ-9.2 MaxRAMPercentage | §18 | 9 | High | No | No | Yes |
| REQ-9.3 GC policy | §18 | 9 | High | No | No | Yes |
| REQ-9.4 Quarkus threads | §18 | 9 | High | No | No | Yes |
| REQ-9.5 Semeru consistency | §18 | 9 | Medium | No | No | No |
| REQ-8b.1 VM Prometheus queries | §30 | 8b | High | Yes | No | No |
| REQ-8b.2 VM digest table | §30 | 8b | High | No | Yes | No |
| REQ-8b.3 VM CSV parser | §30 | 8b | High | No | No | No |
| REQ-8b.4 recommendVM() | §30 | 8b | High | No | Yes | No |
| REQ-8b.5 recommendAllWorkloads() | §30 | 8b | High | No | Yes | No |
| REQ-8b.6 VM API endpoint | §30 | 8b | High | No | No | Yes |
| REQ-8b.7 VM workload type | §30 | 8b | Medium | No | No | No |
| REQ-8b.8 VM detection heuristic | §30 | 8b | Low | No | No | No |
| REQ-8b.9 Instance type rec | §30 | 8b | Low | Yes | No | Yes |
| REQ-8c.1 Node ROS Prometheus queries | Industry analysis | 8c | High | Yes (routing) | No | No |
| REQ-8c.2 Node digest table | Industry analysis | 8c | High | No | Yes | No |
| REQ-8c.3 Node utilization (Tier 1) | Industry analysis | 8c | High | No | Yes | No |
| REQ-8c.4 MachineSet Prometheus queries | Industry analysis | 8c | High | Yes | No | No |
| REQ-8c.5 MachineSet right-sizing (Tier 2) | Industry analysis | 8c | High | No | No | No |
| REQ-8c.6 Instance type catalog | Industry analysis | 8c | Medium | No | No | No |
| REQ-8c.7 MachineAutoscaler opt (Tier 3) | Industry analysis | 8c | Medium | Yes | No | No |
| REQ-8c.8 recommendNodes() (Go) | Industry analysis | 8c | High | No | Yes | No |
| REQ-8c.9 Batch entry point extension | Industry analysis | 8c | High | No | Yes | No |
| REQ-8c.10 Node/MachineSet API endpoints | Industry analysis | 8c | High | No | No | Yes |
| REQ-8c.11 Node/MachineSet rec tables | Industry analysis | 8c | High | No | Yes | No |
| REQ-10.1 Remove Kruize calls | §24 | 10 | High | No | No | No |
| REQ-10.2 Remove rec topic | §24 | 10 | Medium | No | No | No |
| REQ-10.3 Simplify pipeline | §24 | 10 | High | No | No | No |
| REQ-10.4 Remove from manifests | §24 | 10 | Medium | No | No | No |
| REQ-10.5 Update health checks | §24 | 10 | Low | No | No | No |
| REQ-10.6 Rec quality metrics | Risk review Q16 (simplified) | 10 | Medium | No | Yes | Yes |
| REQ-10.7 Adoption detection | Third review L3 | 10 | Medium | No | Yes | Yes |
| REQ-10.8 Staleness detection | Third review C4 | 10 | Medium | No | No | Yes |
| REQ-8c.2b Node request sum queries | Third review C2 | 8c | High | Yes | No | No |
| REQ-7.5 Koku cost integration | Risk review Q8 | 7 | High | No | No | Yes |

---

## 25. Operational Notes (Deferred to Implementation)

The following items are **not missing** from the requirements — they are operational concerns that belong in implementation-phase runbooks, not in a requirements document. Noted here for awareness:

### Alerting Rules and Runbooks

Prometheus alerting rules (e.g., `ROSIngestionLagHigh`, `ROSRecommendationComputeSlow`, `ROSDBConnectionPoolExhausted`) and SRE runbooks are created during deployment, not during requirements. The Prometheus metrics defined in NFR-4 and throughout the requirements (e.g., `ros_kafka_consumer_lag`, `ros_circuit_breaker_state_transitions_total`, `ros_recommendation_oom_rate`, `ros_recommendation_stability`) provide the building blocks. Alerting thresholds depend on production traffic patterns — define them after the first 2 weeks of shadow mode data. **Recommendation:** Create a `docs/runbooks/` directory in the new binary's repo. Each runbook maps an alert to diagnostic steps and remediation. Start with: ingestion lag, recommendation compute timeout, DB connection exhaustion, RBAC circuit breaker open.

### Distributed Tracing

The current ros-ocp-backend has no distributed tracing. Neither does Koku (it uses structured logging with `tracing_id` for request correlation). For MVP, **structured logging with `org_id`, `cluster_uuid`, `request_id` context** (already specified in NFR-4) is sufficient — this matches Koku's pattern and provides the same correlation capability. **Post-MVP**, if cross-service debugging becomes a bottleneck (e.g., tracing a Kafka message through Koku → S3 → ros-ocp-backend → DB), add OpenTelemetry spans. Go has excellent OTel support via `go.opentelemetry.io/otel`. The key propagation point is the Kafka message headers — add a `traceparent` header (W3C Trace Context format) when Koku's `ROSReportShipper` produces the message, and ros-ocp-backend extracts it when consuming.

### Operator Delivery Ordering

The operator and ros-ocp-backend are developed by different teams with different release cadences. The requirements document specifies **what** the operator must send (new CSV columns, new PromQL queries per phase) but not **when** relative to the backend. In practice:
- **Forward compatibility:** The new binary must handle CSVs that are missing new columns (the old operator is still deployed). The CSV parser already handles this — missing columns default to zero/null (REQ-2.3 specifies float-to-int conversion at parse time; the same parse logic handles missing fields gracefully).
- **Backward compatibility:** The old binary (if still active for some orgs during rollout) ignores extra CSV columns it doesn't recognize. The operator adds new columns alongside existing ones.
- **Coordination:** Each backend phase that requires new operator data (marked "Operator Change? Yes" in the traceability matrix) should have a corresponding operator PR merged and released **before** the backend phase is enabled via feature flag. The feature flag acts as the coordination gate — don't enable a Phase 5 GPU feature flag until the operator version with GPU queries is deployed to the target clusters.
