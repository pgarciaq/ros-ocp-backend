# Architecture Decision Records

> **Last verified:** 2026-08-05

Architecture Decision Records (ADRs) document significant design choices in
ros-ocp-backend: the problem context, the decision made, and its consequences.
They preserve the reasoning behind the native recommendation engine, plugin
architecture, ingestion pipeline, API contracts, and operational policies so
future contributors can understand *why* the system works the way it does.

The project maintains **334 ADRs** in the repository under `docs/adr/`. Each
record follows [Michael Nygard's ADR template](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).

## Full ADR index

Browse the complete, searchable index (all numbers, titles, domains, phases, and
statuses) in the repository:

**[docs/adr/README.md](https://github.com/pgarciaq/ros-ocp-backend/tree/{{ git_branch }}/docs/adr)**

Individual ADRs are linked from that index, for example:

`https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0001-native-engine-over-kruize.md`

!!! tip "Contributing"
    When you change architectural code paths, check whether an existing ADR
    needs a status update or a new ADR should be created. CI runs an advisory
    reminder when files listed in
    [`.github/architectural-paths.txt`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/.github/architectural-paths.txt)
    are modified.

## Highlighted ADRs

These records are especially useful when onboarding or tracing cross-cutting
behavior. Each link opens the full ADR on GitHub.

| ADR | Title | Why it matters |
|-----|-------|----------------|
| [0001](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0001-native-engine-over-kruize.md) | Use native Go engine over Kruize | Foundational shift to in-process Go recommendations |
| [0003](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0003-read-once-compute-n-terms.md) | Read once, compute N terms | Core performance model for percentile-based sizing |
| [0045](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0045-daily-digest-tables-not-raw-metrics.md) | Daily digest tables, not raw metrics | PostgreSQL data model for recommendation inputs |
| [0066](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0066-keyset-after-cursor-pagination.md) | Keyset (after cursor) pagination | List API pagination contract used across resource types |
| [0088](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0088-kafka-s3-pipeline-both-modes.md) | Kafka + S3 pipeline for on-prem and SaaS | Ingestion architecture shared by deployment modes |
| [0093](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0093-chunked-pgx-batches-500.md) | Chunked pgx batches (max 500) | Memory-bounded batch flushing for ingest pipelines |
| [0099](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0099-compile-time-in-process-plugins.md) | Compile-time in-process plugins | Plugin system: no gRPC/Wasm dynamic loading |
| [0128](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0128-unify-gorm-pgxpool-stdlib.md) | Unify GORM and pgxpool via stdlib | Single connection pool eliminates exhaustion bugs |
| [0299](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0299-adopt-pgxpool-for-high-throughput-ingest.md) | Adopt pgxpool for high-throughput ingest | Why pgx was introduced; GORM vs pgx comparison; full-replacement analysis |
| [0103](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0103-phased-execution-produce-enrich-optimize.md) | Phased execution (Produce/Enrich/Optimize) | How recommendation plugins run in priority order |
| [0138](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0138-mkdocs-public-site-separate.md) | MkDocs public site separate from internal docs | Why this developer site exists alongside `docs/` |
| [0163](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0163-deprecate-kruize-plugin.md) | Deprecate and remove the Kruize plugin | Kruize removal and native-only production path |
| [0259](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0259-synchronous-ingest-time-engine-replaces-kruize-experiment-lifecycle.md) | Synchronous ingest-time engine | Replaces Kruize experiment lifecycle with ingest-time compute |
| [0262](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0262-shadow-mode-native-engine-explicitly-rejected.md) | Shadow-mode native engine rejected | Explicit rejection of dual-run validation approach |
| [0287](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0287-operator-14-day-prometheus-lookback-integration-boundary.md) | Operator 14-day Prometheus lookback | Integration boundary with koku-metrics-operator |
| [0288](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0288-decay-weight-lookup-tables.md) | Precomputed decay weight lookup tables | Eliminates per-row `math.Exp` in digest hot path |
| [0289](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0289-defer-org-metadata-refresh-end-of-reconcile.md) | Defer org metadata refresh to end of reconcile | Cuts redundant full-org scans during streaming ingest (M1) |
| [0290](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0290-max-daily-p95-for-idle-classification.md) | Max-of-daily-P95 for idle classification | O(N) idle check; conservative vs exact window P95 (Q4) |
| [0291](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0291-integer-micro-cents-savings-computation.md) | Integer micro-cents savings computation | Unified fixed-point savings math; eliminates float64 hot path (P1-1) |
| [0292](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0292-digest-based-plot-percentile-bands.md) | Digest-based percentile-band plots | Replaces query-time sample boxplots; separate `ROS_SAMPLE_RETENTION_DAYS` (E-2) |
| [0293](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0293-engine-only-notification-deduplication.md) | Engine-only notification emission | Detail: per-engine maps only; list: `notification_codes` array (A-2) |
| [0294](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0294-slim-list-contract.md) | Slim list contract | List DTOs omit plots; default `short_term` cost; skip enrichment at `limit=1` (S4, H-4) |
| [0295](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0295-integer-first-architecture.md) | Integer-first arithmetic | `int64` everywhere (cents, millicores, basis points, micro-cents); `float64` only at boundaries; umbrella for ADRs 0047/0064/0098/0280/0288/0291 |
| [0325](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0325-stdlib-csv-streaming-over-dataframe-library.md) | stdlib CSV streaming over DataFrame library | Why the native engine uses `encoding/csv` with `ReuseRecord=true` instead of go-gota's DataFrame or `ScanCSV` |
| [0298](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0298-composite-key-sweep-stale-detection.md) | Composite-key sweep for stale detection | Post-reconcile sweep marks rows stale when composite key changes; complements cluster-level staleness (ADR-0224) |
| [0318](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0318-horizontal-scaling-kafka-consumer-groups.md) | Horizontal scaling via Kafka consumer groups | Partition affinity + idempotent DB ops enable coordination-free scaling |
| [0319](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0319-postgresql-only-validated-100k.md) | PostgreSQL-only architecture validated at 100K | PostgreSQL handles 100K containers at 3.5 GB without Redis/Timescale/Trino |
| [0320](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0320-db-pool-arithmetic-primary-scaling-constraint.md) | DB pool arithmetic as primary scaling constraint | `ManifestDownloadWorkers x KafkaWorkers <= DBMaxConns - 2` invariant |
| [0321](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0321-raise-default-dbmaxconns-5-to-10.md) | Raise default DBMaxConns from 5 to 10 | Fix pool constraint violation with default worker settings |
| [0322](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0322-temporary-dual-write-kruize-robne-saas-migration.md) | Temporary dual-write for Kruize/robne SaaS migration | Phased SaaS-only dual engine execution for migration validation |
| [0323](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0323-unified-category-across-all-resource-types.md) | Unified category across all resource types | Single `category` field replaces boolean classifications; supersedes ADR-0307 |
| [0326](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0326-calendar-accurate-monthly-hours.md) | Calendar-accurate monthly hours | Replace fixed 730 hours/month with `HoursInMonth(year, month)` for savings extrapolation; supersedes ADR-0182 |
| [0328](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0328-hcp-cluster-topology-detection-w0.md) | HCP topology detection (W0) | Signals + hybrid operator→backend classify; suppress/annotate |
| [0329](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0329-ros-auto-include-hypershift-hcp-namespaces.md) | Auto-include HCP namespaces in ROS | Operator collects HyperShift CP ns without manual opt label |
| [0330](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0330-hcp-audience-visibility-rh-vs-customer.md) | HCP RH vs customer visibility | Both RH-internal full path and customer advisory |
| [0331](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0331-management-cp-rightsizing-filters-and-guardrails.md) | Management CP rightsizing (W1) | Label filters + strict CP guardrails |
| [0332](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0332-thin-cross-plane-causality-w2.md) | Thin cross-plane causality (W2) | Hosted API pain ↔ management CP; go-with-caveats |
| [0333](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0333-unused-hostedcluster-lifecycle-w3.md) | Unused HostedCluster FinOps (W3) | Idle hosted + CP still on → delete/review; not pausedUntil |
| [0334](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0334-fleet-admission-headroom-w4.md) | Fleet admission headroom (W4) | Docs/MCE packing math — not lab-calibrated universal N |
| [0335](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0335-api-tax-operator-webhook-w5.md) | Operator / webhook API tax (W5) | Thin top-N digest + webhook rollups; both planes |

## Domains covered

The index groups ADRs by domain, including:

- **Engine / Algorithm** — percentiles, idle detection, GPU/MIG, node consolidation, PVC sizing
- **Data Model** — digests, partitions, recommendation keys, integer money types
- **API Design** — pagination, filters, settings precedence, notification codes
- **Ingestion** — Kafka commits, CSV parsing, manifest gating, reship triggers
- **Plugins** — trait interfaces, phased execution, feature gates
- **Cost / Savings** — Masu rates, fleet summary caches, savings methodology
- **Security** — RBAC, SSRF protection, entitlement middleware
- **Deployment / Ops** — retention, migrations, observability, runbooks

See the [full index](https://github.com/pgarciaq/ros-ocp-backend/tree/{{ git_branch }}/docs/adr) for the complete table.
