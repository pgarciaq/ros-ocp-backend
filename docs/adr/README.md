# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for ros-ocp-backend.
Each record captures a significant architectural decision, its context, and consequences.

Format follows [Michael Nygard's ADR template](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).

## Index

| Number | Title | Domain | Phase | Status |
|--------|-------|--------|-------|--------|
| [0001](0001-native-engine-over-kruize.md) | Use native Go engine over Kruize for production recommendations | Engine / Algorithm | 1 | Accepted |
| [0002](0002-exact-go-percentiles-over-timescaledb.md) | Use exact Go percentiles over TimescaleDB/t-digest | Engine / Algorithm | 1 | Accepted |
| [0003](0003-read-once-compute-n-terms.md) | Use "read once, compute N terms" over per-term SQL scans | Engine / Algorithm | 1 | Accepted |
| [0004](0004-dual-cost-performance-engine-rows.md) | Use dual cost/performance engine rows per term | Engine / Algorithm | 2 | Accepted |
| [0005](0005-decay-weighted-average-half-life.md) | Use decay-weighted average with configurable half-life per term | Engine / Algorithm | 1–2 | Accepted |
| [0006](0006-p60-vs-p98-cpu-p95-vs-max-memory.md) | Use P60 (cost) vs P98 (perf) for CPU, P95 vs max for memory | Engine / Algorithm | 2 | Accepted |
| [0007](0007-adaptive-margin-p95-p50-over-mean.md) | Use adaptive margin from (P95-P50)/mean clamped 1.15-1.50 | Engine / Algorithm | 1–2 | Accepted |
| [0008](0008-25-millcore-cpu-floor.md) | Use 25 millicore CPU floor | Engine / Algorithm | 1–2 | Accepted |
| [0009](0009-limit-request-times-1-05.md) | Use limit = request × 1.05 for containers | Engine / Algorithm | 1–2 | Accepted |
| [0010](0010-logarithmic-oom-bump-capped-1-60.md) | Use logarithmic OOM bump capped at 1.60× | Engine / Algorithm | 4 | Accepted |
| [0011](0011-fixed-idle-thresholds-10mcpu-10mib.md) | Use fixed 10 mCPU / 10 MiB idle thresholds (not env-configurable) | Engine / Algorithm | 1–9 | Accepted |
| [0012](0012-three-state-idle-zombie-active.md) | Use three-state idle/zombie/active classification | Engine / Algorithm | 9 | Accepted |
| [0013](0013-idle-classify-inline-during-produce.md) | Classify idle inline during container produce, not as separate plugin | Engine / Algorithm | 1–9 | Accepted |
| [0014](0014-namespace-idle-after-container-gpu-priority-90.md) | Aggregate namespace idle after container+GPU (plugin priority 90) | Engine / Algorithm | 6 | Accepted |
| [0015](0015-node-target-utilization-80-vs-55.md) | Use node target utilization 80% (cost) vs 55% (performance) | Engine / Algorithm | 10 | Accepted |
| [0016](0016-cost-consolidation-any-performance-2x-headroom.md) | Use cost-engine consolidation on any underutilization; performance only at 2× headroom | Engine / Algorithm | 10 | Accepted |
| [0017](0017-ema-smoothed-imbalance-stranded-resources.md) | Use EMA-smoothed imbalance for stranded resource detection (α=0.3, threshold 0.6) | Engine / Algorithm | 10 | Accepted |
| [0018](0018-operator-node-allocatable-over-fallback.md) | Prefer operator node_allocatable over 0.93× request fallback | Engine / Algorithm | 10 | Accepted |
| [0019](0019-multi-metric-gpu-tree.md) | Use multi-metric GPU tree (SM, tensor, DRAM) not single SM threshold | Engine / Algorithm | 7 | Accepted |
| [0020](0020-p98-fb-times-1-20-mig-profile.md) | Use P98 FB × 1.20 headroom for MIG profile selection | Engine / Algorithm | 7 | Accepted |
| [0021](0021-exclude-idle-memory-bound-mig-from-timeslicing.md) | Exclude idle/memory-bound/MIG workloads from time-slicing candidates | Engine / Algorithm | 7 | Accepted |
| [0022](0022-timeslicing-replicas-clamp-2-8-majority-rule.md) | Clamp time-slicing replicas to [2, 8] with majority ≥50% candidate rule | Engine / Algorithm | 7 | Accepted |
| [0023](0023-gpu-confidence-data-volume-burst-penalty.md) | Use GPU confidence from data volume + burst penalty | Engine / Algorithm | 7 | Accepted |
| [0024](0024-external-yaml-gpu-catalog.md) | Use external YAML GPU catalog over hardcoded model tables | Engine / Algorithm | 7 | Accepted |
| [0025](0025-pvc-thresholds-20-oversized-85-near-full.md) | Use PVC thresholds 20% oversized / 85% near-full with min trend days | Engine / Algorithm | 7 | Accepted |
| [0026](0026-pvc-size-max-usage-times-2-floor-1gib.md) | Recommend PVC size as max(usage_max×2, 1 GiB) | Engine / Algorithm | 7 | Accepted |
| [0027](0027-pvc-longer-terms-zero-decay.md) | Use longer PVC terms (7/30/90d) with zero decay | Engine / Algorithm | 7 | Accepted |
| [0028](0028-quota-engine-container-cost-medium-term.md) | Fix quota engine to container cost/medium_term aggregates | Engine / Algorithm | 10 | Accepted |
| [0029](0029-quota-headroom-10-percent-70-90-risk-bands.md) | Use 10% headroom and 70/90% risk bands for quota/CRQ | Engine / Algorithm | 10 | Accepted |
| [0030](0030-quota-after-container-crq-after-namespace.md) | Run quota after container recs; CRQ after namespace quota | Engine / Algorithm | 10 | Accepted |
| [0031](0031-snapshot-priority-ordered-rules.md) | Use snapshot priority-ordered rules (orphan > managed > redundant > stale > never-restored) | Engine / Algorithm | 10 | Accepted |
| [0032](0032-snapshot-restoresize-for-cost.md) | Use restoreSize for snapshot cost, not CSI byte metrics | Engine / Algorithm | 10 | Accepted |
| [0033](0033-vm-p95-p99-whole-units-downsize-hysteresis.md) | Use VM P95/P99 + whole vCPU/GiB sizing with downsize hysteresis | Engine / Algorithm | 7 | Accepted |
| [0034](0034-normalize-vm-gpu-devices-child-table.md) | Normalize vm_gpu_devices JSONB to child table | Engine / Algorithm | 7 | Accepted |
| [0035](0035-business-hours-nested-block.md) | Use business-hours as nested block, not separate API rows | Engine / Algorithm | 9 | Accepted |
| [0036](0036-business-hours-container-namespace-only.md) | Scope business hours to container+namespace only | Engine / Algorithm | 9 | Accepted |
| [0037](0037-adoption-detection-5-percent-tolerance.md) | Use adoption detection at 5% request tolerance | Engine / Algorithm | 4 | Accepted |
| [0038](0038-notification-code-bitmap-1-63.md) | Use notification code bitmap (1–63) for deduplication; persist as SMALLINT[] (incorporates former ADR-0039) | Engine / Algorithm | 4 | Accepted |
| [0040](0040-allow-negative-savings.md) | Allow negative savings (cost to implement) | Engine / Algorithm | 7 | Accepted |
| [0041](0041-savings-on-all-hours-row-only.md) | Use savings on all_hours row only; BH affects sizing not dollars | Engine / Algorithm | 9 | Accepted |
| [0042](0042-desired-replicas-over-pod-count-avg.md) | Use desired_replicas over pod_count_avg for savings multiplication | Engine / Algorithm | 7 | Accepted |
| [0043](0043-instance-type-consolidation-level-3.md) | Use instance-type consolidation Level 3 when instance_type present | Engine / Algorithm | 10 | Accepted |
| [0044](0044-linear-regression-trend-2-day-minimum.md) | Use linear regression trend with ≥2-day minimum | Engine / Algorithm | 7 | Accepted |
| [0045](0045-daily-digest-tables-not-raw-metrics.md) | Use daily digest tables, not raw metrics in PostgreSQL | Data Model | 1 | Accepted |
| [0046](0046-bigint-for-all-metric-columns.md) | Use BIGINT for all metric columns end-to-end | Data Model | 1 | Accepted |
| [0047](0047-integer-cents-basis-points-millicores.md) | Use integer cents / basis points / millicores, not floats | Data Model | 1 | Accepted |
| [0048](0048-relational-columns-not-jsonb-blobs.md) | Use relational columns on recommendation_sets, not JSONB blobs | Data Model | 1 | Accepted |
| [0049](0049-term-engine-workload-type-in-pks.md) | Include term, engine, and workload_type in PKs | Data Model | 1–2 | Accepted |
| [0050](0050-uuid-v5-deterministic-recommendation-ids.md) | Use UUID v5 deterministic recommendation IDs | Data Model | 1 | Accepted |
| [0051](0051-org-id-on-every-detail-lookup.md) | Require org_id on every detail lookup despite deterministic IDs | Data Model | 1 | Accepted |
| [0052](0052-org-container-keys-denormalized-index.md) | Use org_container_keys denormalized index for list pagination | Data Model | 6 | Accepted |
| [0053](0053-split-list-query-keys-and-detail.md) | Split list query: keys table for identity, detail table for rec state | Data Model | 6 | Accepted |
| [0054](0054-resolved-tags-jsonb-on-keys-table.md) | Store resolved_tags JSONB on keys table | Data Model | 11 | Accepted |
| [0055](0055-query-time-boxplots-from-samples.md) | Use query-time boxplots from container_usage_samples | Data Model | 5 | Accepted |
| [0056](0056-boxplot-6h-and-daily-buckets.md) | Use 6-hour buckets (short term) and daily buckets (medium/long) | Data Model | 5 | Accepted |
| [0057](0057-allowlisted-bucket-sql-expressions.md) | Use allowlisted bucket SQL expressions (BucketGranularity) | Data Model | 5 | Accepted |
| [0058](0058-partition-by-usage-start-month.md) | Partition usage/history/quality by usage_start / month | Data Model | 1 | Accepted |
| [0059](0059-auto-create-partitions-in-go.md) | Auto-create partitions at first write in Go, not pg_partman | Data Model | 1 | Accepted |
| [0060](0060-separate-recommendation-history.md) | Separate recommendation_history from live recommendation_sets | Data Model | 5 | Accepted |
| [0061](0061-dual-engine-rows-for-nodes.md) | Use dual engine rows for nodes (term, engine) PK | Data Model | 2 | Accepted |
| [0062](0062-analytics-incomplete-flag-on-failure.md) | Mark clusters analytics_incomplete when history/quality fails | Data Model | 4–5 | Accepted |
| [0063](0063-centralized-migrations-with-plugin-headers.md) | Centralize migrations in one numbered directory with plugin headers | Data Model | 1 | Accepted |
| [0064](0064-money-amount-api-cents-internal.md) | Use MoneyAmount (value+units) in API while storing cents internally | Data Model | 2 | Accepted |
| [0065](0065-kruize-compatible-json-shape.md) | Preserve Kruize-compatible list/detail JSON shape for UI | API Design | 2 | Accepted |
| [0066](0066-keyset-after-cursor-pagination.md) | Use keyset (after cursor) pagination with base64url JSON cursors (incorporates former ADR-0067) | API Design | 8 | Accepted |
| [0068](0068-filter-project-canonical-namespace-alias.md) | Use filter[project] as canonical namespace alias | API Design | 2 | Accepted |
| [0069](0069-filter-term-normalized.md) | Use filter[term] normalized to short_term/medium_term/long_term | API Design | 2–3 | Accepted |
| [0070](0070-engine-filter-dual-engine-resources-only.md) | Use filter[engine]=cost|performance only on dual-engine resources | API Design | Accepted |
| [0071](0071-exclude-gpu-from-savings-summary.md) | Exclude GPU savings from savings-summary fleet total | API Design | 7 | Accepted |
| [0072](0072-exclude-quota-from-fleet-savings.md) | Exclude quota/CRQ from fleet savings to avoid double-count | API Design | 6–7 | Accepted |
| [0073](0073-dynamic-openapi-x-plugin-required.md) | Use dynamic OpenAPI filtered by x-plugin-required | API Design | 2–7 | Accepted |
| [0074](0074-manual-openapi-contract-tests.md) | Use manual OpenAPI + contract tests on all plugin routes (incorporates former ADR-0141) | API Design | 12 | Accepted |
| [0075](0075-gzip-responses-over-1kb.md) | Use gzip for responses >1KB | API Design | 2 | Accepted |
| [0076](0076-request-scoped-enrichment-cache.md) | Use request-scoped enrichment cache for cost rates | API Design | 8 | Accepted |
| [0077](0077-notification-codes-catalog-endpoint.md) | Use GET /notification-codes public catalog | API Design | 4 | Accepted |
| [0078](0078-nested-node-list-medium-term-cost.md) | Use nested node list with medium-term cost row for shared classification | API Design | 7 | Accepted |
| [0079](0079-gpu-node-pagination-sql-triples.md) | Push GPU/node pagination into SQL triple expansion | API Design | 7 | Accepted |
| [0080](0080-csv-export-format-param.md) | Use CSV export via format=csv on list endpoints | API Design | 6 | Accepted |
| [0081](0081-meta-currency-propagation.md) | Use meta.currency + per-object currency propagation | API Design | 7 | Accepted |
| [0082](0082-recalculate-savings-async-202.md) | Use internal POST /recalculate-savings async 202 | API Design | 7 | Accepted |
| [0083](0083-capabilities-endpoint-locked-settings.md) | Use capabilities endpoint listing locked settings fields | API Design | 3 | Accepted |
| [0084](0084-three-tier-settings-precedence.md) | Use three-tier settings precedence: env lock → DB → default | API Design | 3 | Accepted |
| [0085](0085-threshold-cache-ttl-60s-async-recalc.md) | Use per-org threshold cache TTL 60s with async recalc on PUT | API Design | 3 | Accepted |
| [0086](0086-single-flight-threshold-recalc.md) | Use single-flight coalescing per (org_id, recommendation_type) on recalc | API Design | 8 | Accepted |
| [0087](0087-namespace-memory-trend-5x-container.md) | Use namespace memory trend threshold 5× container (500 KiB/day) | API Design | 3 | Accepted |
| [0088](0088-kafka-s3-pipeline-both-modes.md) | Use Kafka + S3 pipeline for on-prem and SaaS (no custom /ingest) | Ingestion | Pre-0–1 | Accepted |
| [0089](0089-manual-kafka-commit-after-success.md) | Use manual Kafka commit after successful processing | Ingestion | Pre-0–1 | Accepted |
| [0090](0090-dlq-after-5-retries.md) | Use DLQ after 5 transient retries with forensic headers | Ingestion | Pre-0–1 | Accepted |
| [0091](0091-incremental-digest-flush-streaming.md) | Use incremental digest flush during streaming CSV parse | Ingestion | 1 | Accepted |
| [0092](0092-ingest-statement-timeout-120s.md) | Use separate ingest statement timeout (120s) via SET LOCAL | Ingestion | 1 | Accepted |
| [0093](0093-chunked-pgx-batches-500.md) | Use chunked pgx batches (max 500 queued) | Ingestion | 1 | Accepted |
| [0094](0094-split-transactions-50k-rows.md) | Use split transactions above 50k rows per phase | Ingestion | 1 | Accepted |
| [0095](0095-csv-type-longest-prefix-first.md) | Use DetermineCSVType longest-prefix-first + contains fallback | Ingestion | 1 | Accepted |
| [0096](0096-strict-analytics-mode-optional.md) | Use strict analytics mode optional (ROS_INGEST_STRICT_ANALYTICS) | Ingestion | 1–4 | Accepted |
| [0098](0098-csv-float-to-int64-parse-time.md) | Convert CSV floats to int64 at parse time with NaN/Inf rejection | Ingestion | 1 | Accepted |
| [0099](0099-compile-time-in-process-plugins.md) | Use compile-time in-process plugins over gRPC/Wasm/.so | Plugins | 1 | Accepted |
| [0100](0100-trait-interfaces-for-plugins.md) | Use trait interfaces (CSVIngestor, IngestHook, APIProvider, …) | Plugins | 1 | Accepted |
| [0101](0101-ingest-hook-metric-rows-not-db-reread.md) | Pass []MetricRow to IngestHook, not DB re-read | Plugins | 1 | Accepted |
| [0102](0102-ingest-hook-failures-non-fatal.md) | Treat IngestHook failures as non-fatal | Plugins | 1 | Accepted |
| [0103](0103-phased-execution-produce-enrich-optimize.md) | Use phased execution (Produce/Enrich/Optimize) with priority ordering | Plugins | 1 | Accepted |
| [0104](0104-kruize-mutually-exclusive-native.md) | Make Kruize mutually exclusive with native plugins | Plugins | 1 | Accepted |
| [0105](0105-container-handlers-in-core.md) | Keep container handlers in core; plugins register domain routes | Plugins | 1 | Accepted |
| [0106](0106-gpu-api-enricher-on-container.md) | Use GPU as APIEnricher on container responses | Plugins | 7 | Accepted |
| [0107](0107-retention-provider-per-plugin.md) | Use RetentionProvider per plugin with core fallback slice | Plugins | 1–12 | Accepted |
| [0108](0108-term-provider-per-plugin.md) | Use TermProvider per plugin with different default windows | Plugins | 1–3 | Accepted |
| [0109](0109-vm-plugin-feature-gate.md) | Gate VM plugin with ROS_ENABLE_VM_RECS | Plugins | 7 | Superseded by [ADR-0157](0157-ros-enabled-plugins-replaces-native-flag.md) |
| [0110](0110-example-plugin-trait-checklist.md) | Use _example plugin as compile-time trait checklist | Plugins | 1–12 | Accepted |
| [0111](0111-rates-from-koku-masu.md) | Source all rates from Koku Masu effective_rates | Cost / Savings | 7 | Accepted |
| [0112](0112-bounded-lru-ttl-cost-cache.md) | Use bounded LRU+TTL cost cache (max 1000 entries) | Cost / Savings | 7–8 | Accepted |
| [0113](0113-nil-cost-provider-when-masu-unavailable.md) | Use NilCostDataProvider when Masu unavailable | Cost / Savings | 7 | Accepted |
| [0114](0114-notif-no-cost-data-container-node-pvc.md) | Emit notification code 25 (NotifNoCostData) on container/node/PVC, not GPU | Cost / Savings | 7 | Accepted |
| [0115](0115-gpu-mig-idle-persist-timeslicing-read-time.md) | Persist GPU MIG/idle savings; compute time-slicing at read time | Cost / Savings | 7 | Accepted |
| [0116](0116-snapshot-cost-fallback-chain.md) | Use snapshot cost chain: Settings → env → effective_rates → $0.05 default | Cost / Savings | 10 | Accepted |
| [0117](0117-savings-include-all-cost-types.md) | Include infrastructure + supplementary + distributed costs in savings | Cost / Savings | 7 | Accepted |
| [0118](0118-invalidate-cost-cache-on-settings-change.md) | Invalidate cost cache on threshold settings change | Cost / Savings | 7–8 | Accepted |
| [0119](0119-tags-source-db-on-prem.md) | Use on-prem DB join to Koku tag tables (ROS_TAGS_SOURCE=db) | Tags | 11 | Accepted |
| [0120](0120-saas-http-push-tag-sync.md) | Use SaaS HTTP push full-replace sync | Tags | 11 | Accepted |
| [0122](0122-tags-enabled-by-default.md) | Default ROS_TAGS_ENABLED=true after stabilization | Tags | 11 | Accepted |
| [0123](0123-sa-tokenreview-allowlist-internal.md) | Use SA TokenReview allowlist for internal endpoints | Tags | 12 | Accepted |
| [0124](0124-koku-reship-ros-rebuild-bh.md) | Trigger Koku reship_ros to rebuild BH digests from S3 | Reship / Business Hours | 9 | Accepted |
| [0125](0125-single-flight-trailing-reship.md) | Use single-flight lock + trailing reship on concurrent schedule edits | Reship / Business Hours | 9 | Accepted |
| [0126](0126-forward-only-fallback-reship-failure.md) | Use forward-only fallback when reship fails after max retries | Reship / Business Hours | 9 | Accepted |
| [0127](0127-dual-digest-schedule-type-column.md) | Store dual digest streams (schedule_type=all_hours|business_hours) | Reship / Business Hours | Accepted |
| [0128](0128-unify-gorm-pgxpool-stdlib.md) | Unify GORM and pgxpool via stdlib.OpenDBFromPool (incorporates former ADR-0268 phase-1 dual pool) | Deployment / Ops | 8 | Accepted |
| [0129](0129-multi-mode-cobra-binary.md) | Use separate processes (api, processor, housekeeper, poller) from one binary | Deployment / Ops | Pre-0–1 | Accepted |
| [0130](0130-shallow-readyz-default.md) | Use shallow /readyz by default; optional deep checks | Deployment / Ops | Pre-0–12 | Accepted |
| [0131](0131-housekeeper-batched-pk-deletes.md) | Use housekeeper batched PK deletes (5000 rows) for source cleanup | Deployment / Ops | Pre-0–12 | Accepted |
| [0132](0132-retention-policies-per-table.md) | Use retention: 6mo digests, 90d history, 30d stale recs, 48h snapshot inventory | Deployment / Ops | 5–12 | Accepted |
| [0133](0133-structured-logging-zerolog.md) | Use structured logging with logrus (org_id, cluster_uuid, request_id) | Deployment / Ops | Pre-0–12 | Accepted |
| [0134](0134-postgresql-16-target.md) | Use PostgreSQL 16 target | Deployment / Ops | Pre-0 | Accepted |
| [0135](0135-centralized-viper-config.md) | Centralize config in internal/config.Config (Viper) | Deployment / Ops | Pre-0–12 | Accepted |
| [0136](0136-operational-runbooks-adversarial-review.md) | Operational runbooks + adversarial review governance loop (incorporates former ADR-0286) | Deployment / Ops | 12 | Accepted |
| [0137](0137-migration-lint-concurrently-template.md) | Use migration lint + CONCURRENTLY job template for large-table indexes | Deployment / Ops | 12 | Accepted |
| [0138](0138-mkdocs-public-site-separate.md) | Use MkDocs public site separate from internal docs | Deployment / Ops | 12 | Accepted |
| [0140](0140-kruize-vs-native-comparison-tool.md) | Use Kruize vs Native comparison tool for algorithm validation | Testing | 1–12 | Accepted |
| [0142](0142-csv-contract-test-operator-columns.md) | Use CSV contract test tied to operator column headers (incorporates former ADR-0097) | Testing | 1–12 | Accepted |
| [0143](0143-dry-run-sql-org-id-assertion.md) | Use dry-run SQL tests asserting org_id on detail queries | Testing | 12 | Accepted |
| [0144](0144-colocated-domain-tests.md) | Keep domain tests colocated; add wiring tests per plugin extraction | Testing | 12 | Accepted |
| [0145](0145-deny-private-networks-csv-fetch.md) | Deny private networks on CSV URL fetch unless development | Security | 12 | Accepted |
| [0146](0146-csv-url-allowlist-non-dev.md) | Require explicit CSV URL allowlist in non-dev | Security | 12 | Accepted |
| [0147](0147-escape-ilike-wildcards.md) | Escape ILIKE wildcards in filter values | Security | 12 | Accepted |
| [0148](0148-redact-kafka-poison-payloads.md) | Redact Kafka poison payloads in logs by default | Security | 12 | Accepted |
| [0149](0149-block-dev-token-outside-development.md) | Block ROS_TAGS_DEV_TOKEN outside development | Security | 12 | Accepted |
| [0150](0150-validate-sa-allowlist-at-startup.md) | Validate empty SA allowlist blocks api-mode tag auth in prod | Security | 12 | Accepted |
| [0151](0151-rbac-fail-closed-cache-60s.md) | Use RBAC fail-closed with in-memory cache (60s TTL) | Security | 12 | Accepted |
| [0152](0152-cap-history-filter-cardinality.md) | Cap history filter param cardinality (5 values per param) | Security | 12 | Accepted |
| [0153](0153-kafka-category-ros-filter.md) | Consume hccm.ros.events with category=ros filter | Kafka | Pre-0–1 | Accepted |
| [0154](0154-partition-scoped-worker-pool.md) | Use partition-scoped worker pool with ordering preserved per partition | Kafka | Pre-0–1 | Accepted |
| [0155](0155-retry-counter-x-retry-count-header.md) | Use retry counter via X-Retry-Count header on requeue | Kafka | Pre-0–1 | Accepted |
| [0156](0156-sources-destroy-events-cleanup.md) | Use Sources destroy events for tenant cleanup | Kafka | Pre-0–12 | Accepted |
| [0157](0157-ros-enabled-plugins-replaces-native-flag.md) | Replace ROS_USE_NATIVE_ENGINE with ROS_ENABLED_PLUGINS + Kruize exclusivity | Configuration | 1 | Accepted |
| [0158](0158-enabled-or-disabled-plugins-env.md) | Use ROS_ENABLED_PLUGINS allowlist OR ROS_DISABLED_PLUGINS blocklist | Configuration | 1 | Accepted |
| [0159](0159-per-plugin-term-env-vars.md) | Use per-plugin term env vars ROS_TERMS_<PLUGIN>_<TERM>_* | Configuration | 3 | Accepted |
| [0160](0160-savings-estimates-kill-switch.md) | Use ROS_SAVINGS_ESTIMATES_ENABLED global kill-switch | Configuration | 7 | Accepted |
| [0161](0161-staleness-threshold-hours-alias.md) | Use ROS_STALENESS_THRESHOLD_HOURS=48 with alias | Configuration | 11 | Accepted |
| [0162](0162-housekeeper-graceful-shutdown.md) | Use housekeeper graceful shutdown with configurable grace period | Configuration | 12 | Accepted |
| [0163](0163-deprecate-kruize-plugin.md) | Deprecate and remove the Kruize plugin | Plugins | 1–12 | Accepted |
| [0164](0164-sparse-data-notification-orthogonal-to-low-confidence.md) | Add SPARSE_DATA notification orthogonal to LOW_CONFIDENCE | Engine / Algorithm | 4 | Accepted |
| [0165](0165-defer-recommendations-for-synthesized-manifests.md) | Defer recommendations for synthesized manifests until quiet period expires | Ingestion | 10 | Accepted |
| [0166](0166-report-file-status-manifest-gating.md) | Per-file report_file_status tracking with manifest completeness gating | Ingestion | 10 | Accepted |
| [0167](0167-cost-management-entitlement-middleware.md) | Cost-management entitlement middleware (defense-in-depth) | Security | 12 | Accepted |
| [0168](0168-disabled-plugin-route-guards.md) | Disabled plugin route guards before Echo catch-all | API Design | 12 | Accepted |
| [0169](0169-allowlisted-native-sql-query-fragments.md) | Allowlisted native SQL query fragments | Security | 12 | Accepted |
| [0170](0170-machineset-tier1-aggregation.md) | MachineSet Tier-1 aggregation over node recommendations | API Design | 11 | Accepted |
| [0171](0171-streaming-recommendation-batches.md) | Streaming recommendation batches for memory bounding | Engine / Algorithm | 8 | Accepted |
| [0172](0172-dual-path-idle-classification.md) | Dual-path idle classification (authoritative vs legacy fallback) | Engine / Algorithm | 9 | Accepted |
| [0173](0173-tenant-configurable-idle-detection.md) | Tenant-configurable idle detection supersedes fixed thresholds | Engine / Algorithm | 9 | Accepted |
| [0174](0174-fleet-summary-idle-via-notification-codes.md) | Fleet summary counts idle via notification codes, not idle_state column | API Design | 6 | Accepted |
| [0175](0175-idle-api-surfaces-waste-not-savings.md) | Idle API surfaces waste (not savings) with terminate guidance | API Design | 9 | Accepted |
| [0176](0176-namespace-idle-aggregation-rules.md) | Namespace idle aggregation rules | Engine / Algorithm | 6 | Accepted |
| [0177](0177-node-idle-separate-from-container.md) | Node idle classification is separate from container logic | Engine / Algorithm | 9 | Accepted |
| [0178](0178-container-confidence-data-days-over-window.md) | Container confidence is dataDays/windowDays (unlike GPU tiers) | Engine / Algorithm | 4 | Accepted |
| [0179](0179-recommendation-quality-stability-formula.md) | Recommendation quality stability formula | Engine / Algorithm | 4 | Accepted |
| [0180](0180-analytics-write-ordering-strict-mode.md) | Analytics write ordering (recommendations-first vs strict mode) | Data Model | 4–5 | Accepted |
| [0181](0181-adoption-detection-all-term-engine-rows.md) | Adoption detection marks all term/engine rows and emits code 6 | Engine / Algorithm | 4 | Accepted |
| [0182](0182-monthly-savings-730-hours.md) | Monthly savings extrapolation uses 730 hours constant | Cost / Savings | 7 | Superseded by [0326](0326-calendar-accurate-monthly-hours.md) |
| [0183](0183-separate-estimated-waste-cents.md) | Separate estimated_waste_cents for idle workloads | Cost / Savings | 9 | Accepted |
| [0184](0184-fleet-vs-savings-summary-endpoint-split.md) | Fleet-summary vs savings-summary endpoint split | API Design | 6 | Accepted |
| [0185](0185-fleet-savings-lru-cache-rbac-keys.md) | Fleet/savings summary LRU cache with RBAC-scoped keys | Cost / Savings | 6–8 | Accepted |
| [0186](0186-per-cluster-threshold-hash-skip.md) | Per-cluster threshold hash skip for recalculation efficiency | API Design | 3–8 | Accepted |
| [0187](0187-savings-only-recalc-vs-full-threshold-recalc.md) | Savings-only recalc vs full threshold recalc | Cost / Savings | 7–8 | Accepted |
| [0188](0188-list-query-keys-pagination-refilter-detail.md) | List query splits identity on keys table, re-filters on recommendation_sets | Data Model | 6–8 | Accepted |
| [0189](0189-precomputed-org-recommendation-stats.md) | Pre-computed org_recommendation_stats for list counts | Data Model | 6 | Accepted |
| [0190](0190-keyset-cursor-tie-breaker-tuples-per-resource-type.md) | Keyset cursor tie-breaker tuples per resource type | API Design | 8 | Accepted |
| [0191](0191-filter-modes-include-exact-exclude.md) | Filter modes — include (ILIKE), exact, and exclude with different SQL semantics | Security | 6–12 | Accepted |
| [0192](0192-echo-route-registration-order-middleware-layering.md) | Echo route registration order and middleware layering | API Design | 2–12 | Accepted |
| [0193](0193-no-public-api-rate-limiting-body-limits-internal-sync.md) | No public API rate limiting; body limits on internal sync only | Security | 12 | Accepted |
| [0194](0194-node-consolidation-precedence-pod-scheduling-gate.md) | Node consolidation precedence and pod-scheduling gate | Engine / Algorithm | 10 | Accepted |
| [0195](0195-pg-advisory-xact-lock-node-recommendation-writes.md) | pg_advisory_xact_lock for node recommendation writes | Deployment / Ops | 10 | Accepted |
| [0196](0196-mig-profile-selection-full-gpu-escape-hatch.md) | MIG profile selection full_gpu escape hatch | Engine / Algorithm | 7 | Accepted |
| [0197](0197-vm-sub-features-guest-agent-power-schedule-storage-tiering.md) | VM sub-features — guest-agent confidence, power schedule, storage tiering | Engine / Algorithm | 7 | Accepted |
| [0198](0198-gpu-time-slicing-notification-code-36-savings-formula.md) | GPU time-slicing notification code 36 and savings formula | Engine / Algorithm | 7 | Accepted |
| [0199](0199-synthesized-manifest-id-namespace-uuid-scope-key.md) | Synthesized manifest ID namespace UUID and scope key derivation | Ingestion | 10 | Accepted |
| [0200](0200-kafka-consumer-session-tuning-slow-csv-processing.md) | Kafka consumer session tuning for slow CSV processing | Kafka | Pre-0–1 | Accepted |
| [0201](0201-cluster-ingest-hooks-failed-degradation-flag.md) | Cluster ingest_hooks_failed degradation flag | Ingestion | 1–12 | Accepted |
| [0202](0202-ros-partitioned-parent-registry-extensible-retention-ddl.md) | ros_partitioned_parent_registry for extensible retention DDL | Deployment / Ops | 8–12 | Accepted |
| [0203](0203-retention-side-effects-beyond-partition-drop.md) | Retention side effects beyond partition drop | Deployment / Ops | 8–12 | Accepted |
| [0204](0204-continuous-hour-decay-vs-calendar-day-windows.md) | Continuous-hour decay vs calendar-day windows | Engine / Algorithm | 1–3 | Accepted |
| [0205](0205-rbac-filter-intersection-semantics.md) | RBAC filter intersection semantics | Security | 12 | Accepted |
| [0206](0206-explain-audit-query-harness-list-performance.md) | Explain-audit query harness for list performance | Testing | 12 | Accepted |
| [0207](0207-stdlib-json-encoding-api-responses.md) | Stdlib JSON encoding for API responses (not jsoniter/sonic) | API Design | 2–12 | Accepted |
| [0208](0208-settings-scope-org-wide-only-except-business-hours.md) | Settings scope is org-wide only (not hierarchical) except business hours | API Design | 3 | Accepted |
| [0209](0209-dual-idle-threshold-surfaces-container-vs-idle-detection.md) | Dual idle-threshold surfaces (container sizing vs idle_detection) | Engine / Algorithm | 9–11 | Accepted |
| [0210](0210-per-feature-opt-out-under-global-ros-settings-locked.md) | Per-feature opt-out under global ROS_SETTINGS_LOCKED | Configuration | 11 | Accepted |
| [0211](0211-parallel-settings-domains-domain-specific-storage-shapes.md) | Parallel settings domains with domain-specific storage shapes | API Design | 11 | Accepted |
| [0212](0212-threshold-validation-allowlist-cross-field-rules.md) | Threshold validation via allowlist and cross-field rules | API Design | 3–11 | Accepted |
| [0213](0213-init-threshold-defaults-env-into-package-defaults-at-startup.md) | InitThresholdDefaults copies env into process-wide defaults at startup | Configuration | 3 | Accepted |
| [0214](0214-idle-settings-put-fans-out-async-recalc-five-types.md) | Idle settings PUT fans out async recalc to five recommendation types | API Design | 9–11 | Accepted |
| [0215](0215-delete-settings-restores-compiled-defaults.md) | DELETE settings restores compiled defaults (not empty/disabled) | API Design | 3 | Accepted |
| [0216](0216-business-hours-pending-marker-stub-rows.md) | Business hours pending-marker stub rows (not customer schedules) | Reship / Business Hours | 9 | Accepted |
| [0217](0217-disabled-cluster-override-blocks-org-bh-inheritance.md) | Disabled cluster override blocks org BH inheritance for digests | Reship / Business Hours | 9 | Accepted |
| [0218](0218-org-level-reship-single-flight-trailing-batch-coalescing.md) | Org-level reship single-flight with trailing batch coalescing | Reship / Business Hours | 9 | Accepted |
| [0219](0219-reship-background-poller-retries-pending-clusters.md) | Reship background poller retries pending clusters | Reship / Business Hours | 9 | Accepted |
| [0220](0220-bh-put-triggers-reship-threshold-put-triggers-recalc.md) | BH PUT triggers reship; threshold PUT triggers recalc (not reship) | Reship / Business Hours | 9 | Accepted |
| [0221](0221-notifications-recomputed-each-produce-run-not-dismissable.md) | Notifications recomputed each produce run (not dismissable entities) | Engine / Algorithm | 4 | Accepted |
| [0222](0222-notification-dual-source-db-seed-and-go-definitions.md) | Notification dual source — DB seed and Go definitions map | API Design | 4 | Accepted |
| [0223](0223-plugin-filtered-notification-catalog-subsets.md) | Plugin-filtered notification catalog subsets | API Design | 4–7 | Accepted |
| [0224](0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md) | Stale marking precedence — last_reported_at overrides digest age | Engine / Algorithm | 11 | Accepted |
| [0225](0225-filter-stale-tri-state-semantics.md) | filter[stale] tri-state semantics | API Design | 11 | Accepted |
| [0226](0226-tag-sync-full-replace-per-org.md) | Tag sync is full-replace per org (not incremental) | Tags | 11 | Accepted |
| [0227](0227-ros-tags-enabled-master-gate-silently-disables-tag-filters.md) | ROS_TAGS_ENABLED master gate silently disables tag filters | Tags | 11 | Accepted |
| [0228](0228-effective-rates-cache-key-org-cluster-only.md) | Effective rates cache key is org+cluster only (date range excluded) | Cost / Savings | 7–8 | Accepted |
| [0229](0229-container-savings-effective-rates-from-namespace-aggregates.md) | Container savings derive effective rates from namespace aggregates | Cost / Savings | 7 | Accepted |
| [0230](0230-snapshot-inventory-append-only-freshness-window.md) | Snapshot inventory append-only with freshness window for classification | Engine / Algorithm | 10 | Accepted |
| [0231](0231-snapshot-cost-static-rate-per-gib-month.md) | Snapshot cost uses static $/GiB/month (not Koku effective rates) | Cost / Savings | 10 | Accepted |
| [0232](0232-managed-backup-detection-label-prefix-map.md) | Managed backup detection via label prefix map | Engine / Algorithm | 10 | Accepted |
| [0233](0233-cluster-upsert-on-every-kafka-payload.md) | Cluster upsert on every Kafka payload before file processing | Ingestion | Pre-0–1 | Accepted |
| [0234](0234-no-soft-delete-cluster-state.md) | No soft-delete cluster state — cleanup via Sources destroy events | Data Model | Pre-0–12 | Accepted |
| [0235](0235-two-layer-reship-concurrency.md) | Two-layer reship concurrency — org coalescing plus per-cluster advisory lock | Reship / Business Hours | 9 | Accepted |
| [0236](0236-large-table-index-strategy-concurrently-pre-step.md) | Large-table index strategy — manual CONCURRENTLY pre-step for production | Deployment / Ops | 12 | Accepted |
| [0237](0237-runtime-partition-pre-creation-current-next-month.md) | Runtime partition pre-creation (current + next month) | Deployment / Ops | 1–8 | Accepted |
| [0238](0238-environment-variable-catalog-by-subsystem.md) | Environment variable catalog organized by subsystem | Configuration | 12 | Accepted |
| [0239](0239-feature-toggles-vs-plugin-toggles.md) | Feature toggles vs plugin toggles distinction | Configuration | 1–12 | Accepted |
| [0240](0240-connection-pool-timeout-tuning-surface.md) | Connection pool and timeout tuning surface | Deployment / Ops | 8 | Accepted |
| [0241](0241-deprecated-alias-env-vars-backward-compat.md) | Deprecated alias env vars maintained for backward compatibility | Configuration | 1–12 | Accepted |
| [0242](0242-rosocp-prometheus-metric-naming-convention.md) | rosocp_ Prometheus metric naming convention | Deployment / Ops | Pre-0–12 | Accepted |
| [0243](0243-high-cardinality-analytics-incomplete-labels.md) | High-cardinality labels on analytics_incomplete metric | Deployment / Ops | 4–12 | Accepted |
| [0244](0244-request-correlation-echo-request-id.md) | Request correlation via Echo request_id (no OpenTelemetry) | Deployment / Ops | Pre-0–12 | Accepted |
| [0245](0245-plugin-init-registration-order-undefined.md) | Plugin init() registration order intentionally undefined | Plugins | 1 | Accepted |
| [0246](0246-boot-fatal-csv-type-collision.md) | Boot fatal on CSV type collision between plugins | Plugins | 1 | Accepted |
| [0247](0247-apiproviders-bypass-kruize-exclusivity.md) | APIProviders() bypasses Kruize exclusivity for namespace routes | Plugins | 6 | Accepted |
| [0248](0248-v1-only-api-no-v2-migration-policy.md) | v1-only API namespace with no v2 migration policy | API Design | 2 | Accepted |
| [0249](0249-advisory-openapi-changelog-ci-non-blocking.md) | Advisory OpenAPI changelog CI is non-blocking | Testing | 12 | Accepted |
| [0250](0250-pagination-meta-contract.md) | Pagination meta contract — count, limit, offset, has_next, next link | API Design | 6–8 | Accepted |
| [0251](0251-quota-max-used-container-rec-sum.md) | Quota recommendations use max(quota_used, container_rec_sum) utilization signal | Engine / Algorithm | 10 | Accepted |
| [0252](0252-storage-pods-object-count-quotas-used-only.md) | Storage/pods/object-count quotas use used metrics only (not container sums) | Engine / Algorithm | 10 | Accepted |
| [0253](0253-pvc-four-way-classification-healthy-orphaned.md) | PVC four-way classification including healthy and orphaned | Engine / Algorithm | 7 | Accepted |
| [0254](0254-pvc-growth-projection-decay-weighted-slope.md) | PVC growth projection via decay-weighted slope | Engine / Algorithm | 7 | Accepted |
| [0255](0255-org-container-keys-refresh-deletes-stale.md) | org_container_keys refresh deletes stale keys on ingest | Data Model | 6 | Accepted |
| [0256](0256-dual-tag-filter-syntax-legacy-koku.md) | Dual tag filter syntax — legacy and Koku-style (incorporates former ADR-0121) | Tags | 11 | Accepted |
| [0257](0257-stale-tag-sync-warning-on-list-responses.md) | Stale tag sync warning on list responses | Tags | 11 | Accepted |
| [0258](0258-recommendation-profiles-seeded-unused-percentiles-in-settings.md) | recommendation_profiles seeded but unused; custom profiles deferred (incorporates former ADR-0284) | Data Model | 1–3 | Accepted |
| [0259](0259-synchronous-ingest-time-engine-replaces-kruize-experiment-lifecycle.md) | Replace Kruize experiment lifecycle with synchronous ingest-time engine | Engine / Algorithm | 0–1 | Accepted |
| [0260](0260-per-container-recommendation-granularity-operator-csv-grain.md) | Per-container recommendation granularity matching operator CSV grain | Data Model | 1 | Accepted |
| [0261](0261-three-terms-short-medium-long-kruize-aligned-defaults.md) | Three terms (short/medium/long) with Kruize-aligned defaults (1d/7d/15d) | Engine / Algorithm | 1–3 | Accepted |
| [0262](0262-shadow-mode-native-engine-explicitly-rejected.md) | Shadow-mode native engine explicitly rejected | Deployment / Ops | 1–2 | Accepted |
| [0263](0263-stop-writing-workload-metrics-in-native-mode.md) | Stop writing workload_metrics in native mode | Data Model | 1–2 | Accepted |
| [0264](0264-kruize-era-legacy-table-background-deletion.md) | Kruize-era legacy table background deletion strategy | Deployment / Ops | 7–8 | Accepted |
| [0265](0265-operator-csv-column-contract-optional-columns-partial-upgrade.md) | Operator CSV column contract — optional columns and partial-upgrade tolerance | Ingestion | 4–11 | Accepted |
| [0266](0266-go-language-choice-inherited-kafka-integration-service.md) | Go language choice for native engine integration (vs Java/Kruize rewrite) | Deployment / Ops | Pre-0 | Accepted |
| [0267](0267-echo-framework-inherited-pre-existing-service.md) | Echo framework inherited from pre-existing service | API Design | Pre-0 | Accepted |
| [0269](0269-testcontainers-over-docker-compose-test-isolation.md) | testcontainers PostgreSQL 16 + golang-migrate over docker-compose (incorporates former ADR-0139) | Testing | 1 | Accepted |
| [0270](0270-on-demand-api-time-recommendations-deferred.md) | On-demand API-time recommendations deferred (ROS_ENABLE_REALTIME_RECS) | API Design | 1–3 | Accepted |
| [0271](0271-recommendation-history-boxplots-deferred-phase4-to-phase5.md) | Recommendation history and boxplots deferred from phase 4 to phase 5 | Data Model | 4–5 | Accepted |
| [0272](0272-detail-response-typed-struct-replaces-adhoc-json-maps.md) | DetailResponse typed struct replaces ad-hoc JSON maps | API Design | 5 | Accepted |
| [0273](0273-subquery-pagination-replacing-row-multiplier.md) | Subquery pagination replacing row-multiplier approach | API Design | 6–7 | Accepted |
| [0274](0274-remove-rh-accounts-join-direct-org-id-filtering.md) | Remove rh_accounts join — direct org_id filtering on recommendation tables | Data Model | 8–9 | Accepted |
| [0275](0275-quality-metrics-container-only-internal-not-primary-ui.md) | Quality metrics are container-only and internal (not primary UI surface) | API Design | 4–5 | Superseded by [ADR-0310](0310-quality-metrics-generalization-separate-tables-per-entity.md) |
| [0276](0276-hpa-vpa-recommendations-deferred-advisory-only.md) | HPA/VPA recommendations deferred — advisory automation model only | Engine / Algorithm | 7+ | Accepted |
| [0277](0277-local-hybrid-on-cluster-engine-deferred-central-only-v1.md) | Local/hybrid on-cluster engine deferred — central processing only for v1 | Deployment / Ops | 7+ | Accepted |
| [0278](0278-machineset-tier2-engine-deferred-scope-criteria.md) | MachineSet Tier-2 engine deferred — scope criteria documented | Engine / Algorithm | 11–12 | Accepted |
| [0279](0279-namespace-recommendations-unleash-killswitch-then-default-on.md) | Namespace recommendations: Unleash kill-switch then default-on | Plugins | 6 | Accepted |
| [0280](0280-fixed-point-savings-migration-float-to-integer-cents.md) | Fixed-point savings migration (float → integer cents) | Cost / Savings | 9–12 | Accepted |
| [0281](0281-jsonb-vs-normalized-columns-when-each-appropriate.md) | JSONB vs normalized columns — when each is appropriate | Data Model | 7–8 | Accepted |
| [0282](0282-cgo-confluent-kafka-go-test-isolation-strategy.md) | CGO dependency via confluent-kafka-go — test isolation strategy | Ingestion | 0 | Accepted |
| [0283](0283-synchronous-rest-api-no-websocket-sse-recommendation-updates.md) | Synchronous REST API — no WebSocket/SSE for recommendation updates | API Design | Pre-0 through 12 | Accepted |
| [0285](0285-phase-branch-merge-order-migration-renumbering.md) | Phase branch merge order and migration renumbering | Deployment / Ops | 4–6 | Accepted |
| [0287](0287-operator-14-day-prometheus-lookback-integration-boundary.md) | Operator 14-day Prometheus lookback as integration boundary | Ingestion | Cross-repo (koku-metrics-operator) | Accepted |
| [0288](0288-decay-weight-lookup-tables.md) | Precomputed decay weight lookup tables | Engine / Algorithm | Performance | Accepted |
| [0289](0289-defer-org-metadata-refresh-end-of-reconcile.md) | Defer org metadata refresh to end of reconcile cycle | Performance / Ingestion | Performance | Accepted |
| [0290](0290-max-daily-p95-for-idle-classification.md) | Max-of-daily-P95 for idle classification | Engine / Algorithm | Performance | Accepted |
| [0291](0291-integer-micro-cents-savings-computation.md) | Integer micro-cents savings computation | Engine / Algorithm | Performance | Accepted |
| [0292](0292-digest-based-plot-percentile-bands.md) | Digest-based percentile-band plots with separate sample retention | Data Model | Performance | Accepted |
| [0293](0293-engine-only-notification-deduplication.md) | Engine-only notification emission in container/namespace detail JSON | API Design | Performance | Accepted |
| [0294](0294-slim-list-contract.md) | Slim list contract for container and namespace recommendations | API Design | Performance | Accepted |
| [0295](0295-integer-first-architecture.md) | Integer-first arithmetic across the native engine | Engine / Algorithm | Foundational | Accepted |
| [0296](0296-recommendation-explanation-factors-typed-columns.md) | Store recommendation explanation factors as typed columns | Data Model / API Design | 13 | Accepted |
| [0297](0297-gpu-time-slicing-recommendation-persistence.md) | Persist node GPU time-slicing recommendations at ingest | Data Model / Engine | 13 | Accepted |
| [0298](0298-composite-key-sweep-stale-detection.md) | Composite-key sweep for stale detection | Engine / Algorithm | 14 | Accepted |
| [0299](0299-adopt-pgxpool-for-high-throughput-ingest.md) | Adopt pgxpool for high-throughput ingest alongside GORM | Data Access / Ingestion | Foundational | Accepted |
| [0300](0300-remove-fixed-workload-type-allowlist.md) | Remove fixed workload_type allowlist — accept any string | API Design / Validation | 15 | Accepted |
| [0301](0301-visual-insights-dashboard.md) | Visual Insights Dashboard | API Design / Frontend | 16+ | Proposed |
| [0302](0302-oom-timeline-endpoint.md) | OOM Timeline Endpoint | API Design | 17 | Accepted |
| [0303](0303-library-extraction-librobne.md) | Library Extraction of the Native Engine (librobne) | Engine / Architecture | P4b | Accepted |
| [0304](0304-utilization-gauge-metric-and-thresholds.md) | Utilization Gauges Use Peak Usage with Settings-Derived Color Thresholds | API Design / Frontend | 16+ | Accepted |
| [0305](0305-robne-cli-standalone-binary.md) | robne CLI as Standalone Binary Separate from ros-ocp-backend | Deployment / Ops | Future | Proposed |
| [0306](0306-stale-filter-org-container-keys-column.md) | Add `is_stale` column to `org_container_keys` for unified query path | Performance / Data Model | 15+ | Accepted |
| [0307](0307-recommendation-categories.md) | Recommendation Categories (undersized/oversized/optimized) | Data Model / API Design | 15+ | Accepted |
| [0308](0308-auto-lower-heavy-api-timeout-saas.md) | Auto-lower heavy API statement timeout in SaaS mode | Performance / Operations | 15+ | Accepted |
| [0309](0309-replica-count-optimization-phase1.md) | Replica Count Optimization — Phase 1 | Engine / Algorithm | 15+ | Accepted |
| [0310](0310-quality-metrics-generalization-separate-tables-per-entity.md) | Quality metrics generalization — separate tables per entity type | Data Model / API Design | 17 | Accepted |
| [0311](0311-reject-pool-gzip-writer-echo-middleware.md) | Reject pool gzip writer echo middleware | API Design | 17 | Rejected |
| [0312](0312-reject-streaming-json-list-responses.md) | Reject streaming JSON list responses | API Design | 17 | Rejected |
| [0313](0313-reject-factor-out-rh-accounts-subquery.md) | Reject factor out rh_accounts subquery | Data Model | 17 | Rejected |
| [0314](0314-reject-eliminate-get-clusters-for-org-query.md) | Reject eliminate get-clusters-for-org query | Data Model | 17 | Rejected |
| [0315](0315-reject-push-full-rbac-filter-into-sql.md) | Reject push full RBAC filter into SQL | Security | 17 | Rejected |
| [0316](0316-defer-rate-limiter-mutex-sharding.md) | Defer rate limiter mutex sharding | API Design | 17 | Deferred |
| [0317](0317-reject-prometheus-gauge-rate-limiter-map.md) | Reject Prometheus gauge rate limiter map | Deployment / Ops | 17 | Rejected |
| [0318](0318-horizontal-scaling-kafka-consumer-groups.md) | Horizontal scaling via Kafka consumer groups | Kafka / Deployment | 100K benchmark | Accepted |
| [0319](0319-postgresql-only-validated-100k.md) | PostgreSQL-only architecture validated at 100K containers | Deployment / Ops | 100K benchmark | Accepted |
| [0320](0320-db-pool-arithmetic-primary-scaling-constraint.md) | DB connection pool arithmetic as primary scaling constraint | Deployment / Ops | 100K benchmark | Accepted |
| [0321](0321-raise-default-dbmaxconns-5-to-10.md) | Raise default DBMaxConns from 5 to 10 | Deployment / Ops | 100K benchmark | Accepted |
| [0322](0322-temporary-dual-write-kruize-robne-saas-migration.md) | Temporary dual-write for Kruize/robne SaaS migration | Engine / Deployment | — | Accepted |
| [0323](0323-unified-category-across-all-resource-types.md) | Unified category classification across all resource types | Data Model / API Design | — | Accepted |
| [0324](0324-vm-pvc-companion-csv-for-shared-storage-detection.md) | VM PVC companion CSV for shared storage detection | Data Model / Ingestion | — | Accepted |
| [0325](0325-stdlib-csv-streaming-over-dataframe-library.md) | Use stdlib encoding/csv streaming over DataFrame library (go-gota) | Ingestion | Foundational | Accepted |
| [0326](0326-calendar-accurate-monthly-hours.md) | Calendar-accurate monthly hours for savings extrapolation | Cost / Savings | — | Accepted |
| [0327](0327-api-time-currency-conversion-over-storage.md) | API-time currency conversion over stored-currency duplication | Cost / API | — | Accepted |
| [0328](0328-hcp-cluster-topology-detection-w0.md) | HCP cluster topology detection (W0) | HCP / Fleet | — | Accepted |
| [0329](0329-ros-auto-include-hypershift-hcp-namespaces.md) | Auto-include HyperShift HCP namespaces in ROS queries | HCP / Operator | — | Accepted |
| [0330](0330-hcp-audience-visibility-rh-vs-customer.md) | HCP audience visibility — RH-internal and customer paths | HCP / Product | — | Accepted |
| [0331](0331-management-cp-rightsizing-filters-and-guardrails.md) | Management CP rightsizing — filters and guardrails (W1) | HCP / Engine | — | Accepted |
| [0332](0332-thin-cross-plane-causality-w2.md) | Thin cross-plane causality (W2) — hosted API pain vs management CP | HCP / Fleet | — | Accepted |
| [0333](0333-unused-hostedcluster-lifecycle-w3.md) | Unused HostedCluster (“zombie”) FinOps (W3) | HCP / Fleet | — | Accepted |
| [0334](0334-fleet-admission-headroom-w4.md) | Fleet admission headroom — N more HostedClusters (W4) | HCP / Fleet | — | Accepted |
| [0335](0335-api-tax-operator-webhook-w5.md) | Operator / webhook API tax recommendations (W5) | HCP / Fleet | — | Accepted |
