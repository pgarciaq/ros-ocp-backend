# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for ros-ocp-backend.
Each record captures a significant architectural decision, its context, and consequences.

Format follows [Michael Nygard's ADR template](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).

## Index

| Number | Title | Domain | Status |
|--------|-------|--------|--------|
| [0001](0001-native-engine-over-kruize.md) | Use native Go engine over Kruize for production recommendations | Engine / Algorithm | Accepted |
| [0002](0002-exact-go-percentiles-over-timescaledb.md) | Use exact Go percentiles over TimescaleDB/t-digest | Engine / Algorithm | Accepted |
| [0003](0003-read-once-compute-n-terms.md) | Use "read once, compute N terms" over per-term SQL scans | Engine / Algorithm | Accepted |
| [0004](0004-dual-cost-performance-engine-rows.md) | Use dual cost/performance engine rows per term | Engine / Algorithm | Accepted |
| [0005](0005-decay-weighted-average-half-life.md) | Use decay-weighted average with configurable half-life per term | Engine / Algorithm | Accepted |
| [0006](0006-p60-vs-p98-cpu-p95-vs-max-memory.md) | Use P60 (cost) vs P98 (perf) for CPU, P95 vs max for memory | Engine / Algorithm | Accepted |
| [0007](0007-adaptive-margin-p95-p50-over-mean.md) | Use adaptive margin from (P95-P50)/mean clamped 1.15-1.50 | Engine / Algorithm | Accepted |
| [0008](0008-25-millcore-cpu-floor.md) | Use 25 millicore CPU floor | Engine / Algorithm | Accepted |
| [0009](0009-limit-request-times-1-05.md) | Use limit = request × 1.05 for containers | Engine / Algorithm | Accepted |
| [0010](0010-logarithmic-oom-bump-capped-1-60.md) | Use logarithmic OOM bump capped at 1.60× | Engine / Algorithm | Accepted |
| [0011](0011-fixed-idle-thresholds-10mcpu-10mib.md) | Use fixed 10 mCPU / 10 MiB idle thresholds (not env-configurable) | Engine / Algorithm | Accepted |
| [0012](0012-three-state-idle-zombie-active.md) | Use three-state idle/zombie/active classification | Engine / Algorithm | Accepted |
| [0013](0013-idle-classify-inline-during-produce.md) | Classify idle inline during container produce, not as separate plugin | Engine / Algorithm | Accepted |
| [0014](0014-namespace-idle-after-container-gpu-priority-90.md) | Aggregate namespace idle after container+GPU (plugin priority 90) | Engine / Algorithm | Accepted |
| [0015](0015-node-target-utilization-80-vs-55.md) | Use node target utilization 80% (cost) vs 55% (performance) | Engine / Algorithm | Accepted |
| [0016](0016-cost-consolidation-any-performance-2x-headroom.md) | Use cost-engine consolidation on any underutilization; performance only at 2× headroom | Engine / Algorithm | Accepted |
| [0017](0017-ema-smoothed-imbalance-stranded-resources.md) | Use EMA-smoothed imbalance for stranded resource detection (α=0.3, threshold 0.6) | Engine / Algorithm | Accepted |
| [0018](0018-operator-node-allocatable-over-fallback.md) | Prefer operator node_allocatable over 0.93× request fallback | Engine / Algorithm | Accepted |
| [0019](0019-multi-metric-gpu-tree.md) | Use multi-metric GPU tree (SM, tensor, DRAM) not single SM threshold | Engine / Algorithm | Accepted |
| [0020](0020-p98-fb-times-1-20-mig-profile.md) | Use P98 FB × 1.20 headroom for MIG profile selection | Engine / Algorithm | Accepted |
| [0021](0021-exclude-idle-memory-bound-mig-from-timeslicing.md) | Exclude idle/memory-bound/MIG workloads from time-slicing candidates | Engine / Algorithm | Accepted |
| [0022](0022-timeslicing-replicas-clamp-2-8-majority-rule.md) | Clamp time-slicing replicas to [2, 8] with majority ≥50% candidate rule | Engine / Algorithm | Accepted |
| [0023](0023-gpu-confidence-data-volume-burst-penalty.md) | Use GPU confidence from data volume + burst penalty | Engine / Algorithm | Accepted |
| [0024](0024-external-yaml-gpu-catalog.md) | Use external YAML GPU catalog over hardcoded model tables | Engine / Algorithm | Accepted |
| [0025](0025-pvc-thresholds-20-oversized-85-near-full.md) | Use PVC thresholds 20% oversized / 85% near-full with min trend days | Engine / Algorithm | Accepted |
| [0026](0026-pvc-size-max-usage-times-2-floor-1gib.md) | Recommend PVC size as max(usage_max×2, 1 GiB) | Engine / Algorithm | Accepted |
| [0027](0027-pvc-longer-terms-zero-decay.md) | Use longer PVC terms (7/30/90d) with zero decay | Engine / Algorithm | Accepted |
| [0028](0028-quota-engine-container-cost-medium-term.md) | Fix quota engine to container cost/medium_term aggregates | Engine / Algorithm | Accepted |
| [0029](0029-quota-headroom-10-percent-70-90-risk-bands.md) | Use 10% headroom and 70/90% risk bands for quota/CRQ | Engine / Algorithm | Accepted |
| [0030](0030-quota-after-container-crq-after-namespace.md) | Run quota after container recs; CRQ after namespace quota | Engine / Algorithm | Accepted |
| [0031](0031-snapshot-priority-ordered-rules.md) | Use snapshot priority-ordered rules (orphan > managed > redundant > stale > never-restored) | Engine / Algorithm | Accepted |
| [0032](0032-snapshot-restoresize-for-cost.md) | Use restoreSize for snapshot cost, not CSI byte metrics | Engine / Algorithm | Accepted |
| [0033](0033-vm-p95-p99-whole-units-downsize-hysteresis.md) | Use VM P95/P99 + whole vCPU/GiB sizing with downsize hysteresis | Engine / Algorithm | Accepted |
| [0034](0034-normalize-vm-gpu-devices-child-table.md) | Normalize vm_gpu_devices JSONB to child table | Engine / Algorithm | Accepted |
| [0035](0035-business-hours-nested-block.md) | Use business-hours as nested block, not separate API rows | Engine / Algorithm | Accepted |
| [0036](0036-business-hours-container-namespace-only.md) | Scope business hours to container+namespace only | Engine / Algorithm | Accepted |
| [0037](0037-adoption-detection-5-percent-tolerance.md) | Use adoption detection at 5% request tolerance | Engine / Algorithm | Accepted |
| [0038](0038-notification-code-bitmap-1-63.md) | Use notification code bitmap (1–63) for deduplication | Engine / Algorithm | Accepted |
| [0039](0039-notification-codes-smallint-array.md) | Persist notification codes as SMALLINT[], not JSONB | Engine / Algorithm | Accepted |
| [0040](0040-allow-negative-savings.md) | Allow negative savings (cost to implement) | Engine / Algorithm | Accepted |
| [0041](0041-savings-on-all-hours-row-only.md) | Use savings on all_hours row only; BH affects sizing not dollars | Engine / Algorithm | Accepted |
| [0042](0042-desired-replicas-over-pod-count-avg.md) | Use desired_replicas over pod_count_avg for savings multiplication | Engine / Algorithm | Accepted |
| [0043](0043-instance-type-consolidation-level-3.md) | Use instance-type consolidation Level 3 when instance_type present | Engine / Algorithm | Accepted |
| [0044](0044-linear-regression-trend-2-day-minimum.md) | Use linear regression trend with ≥2-day minimum | Engine / Algorithm | Accepted |
| [0045](0045-daily-digest-tables-not-raw-metrics.md) | Use daily digest tables, not raw metrics in PostgreSQL | Data Model | Accepted |
| [0046](0046-bigint-for-all-metric-columns.md) | Use BIGINT for all metric columns end-to-end | Data Model | Accepted |
| [0047](0047-integer-cents-basis-points-millicores.md) | Use integer cents / basis points / millicores, not floats | Data Model | Accepted |
| [0048](0048-relational-columns-not-jsonb-blobs.md) | Use relational columns on recommendation_sets, not JSONB blobs | Data Model | Accepted |
| [0049](0049-term-engine-workload-type-in-pks.md) | Include term, engine, and workload_type in PKs | Data Model | Accepted |
| [0050](0050-uuid-v5-deterministic-recommendation-ids.md) | Use UUID v5 deterministic recommendation IDs | Data Model | Accepted |
| [0051](0051-org-id-on-every-detail-lookup.md) | Require org_id on every detail lookup despite deterministic IDs | Data Model | Accepted |
| [0052](0052-org-container-keys-denormalized-index.md) | Use org_container_keys denormalized index for list pagination | Data Model | Accepted |
| [0053](0053-split-list-query-keys-and-detail.md) | Split list query: keys table for identity, detail table for rec state | Data Model | Accepted |
| [0054](0054-resolved-tags-jsonb-on-keys-table.md) | Store resolved_tags JSONB on keys table | Data Model | Accepted |
| [0055](0055-query-time-boxplots-from-samples.md) | Use query-time boxplots from container_usage_samples | Data Model | Accepted |
| [0056](0056-boxplot-6h-and-daily-buckets.md) | Use 6-hour buckets (short term) and daily buckets (medium/long) | Data Model | Accepted |
| [0057](0057-allowlisted-bucket-sql-expressions.md) | Use allowlisted bucket SQL expressions (BucketGranularity) | Data Model | Accepted |
| [0058](0058-partition-by-usage-start-month.md) | Partition usage/history/quality by usage_start / month | Data Model | Accepted |
| [0059](0059-auto-create-partitions-in-go.md) | Auto-create partitions at first write in Go, not pg_partman | Data Model | Accepted |
| [0060](0060-separate-recommendation-history.md) | Separate recommendation_history from live recommendation_sets | Data Model | Accepted |
| [0061](0061-dual-engine-rows-for-nodes.md) | Use dual engine rows for nodes (term, engine) PK | Data Model | Accepted |
| [0062](0062-analytics-incomplete-flag-on-failure.md) | Mark clusters analytics_incomplete when history/quality fails | Data Model | Accepted |
| [0063](0063-centralized-migrations-with-plugin-headers.md) | Centralize migrations in one numbered directory with plugin headers | Data Model | Accepted |
| [0064](0064-money-amount-api-cents-internal.md) | Use MoneyAmount (value+units) in API while storing cents internally | Data Model | Accepted |
| [0065](0065-kruize-compatible-json-shape.md) | Preserve Kruize-compatible list/detail JSON shape for UI | API Design | Accepted |
| [0066](0066-keyset-after-cursor-pagination.md) | Use keyset (after cursor) pagination over deep offset | API Design | Accepted |
| [0067](0067-base64url-json-cursors.md) | Encode cursors as base64url JSON | API Design | Accepted |
| [0068](0068-filter-project-canonical-namespace-alias.md) | Use filter[project] as canonical namespace alias | API Design | Accepted |
| [0069](0069-filter-term-normalized.md) | Use filter[term] normalized to short_term/medium_term/long_term | API Design | Accepted |
| [0070](0070-engine-filter-dual-engine-resources-only.md) | Use filter[engine]=cost|performance only on dual-engine resources | API Design | Accepted |
| [0071](0071-exclude-gpu-from-savings-summary.md) | Exclude GPU savings from savings-summary fleet total | API Design | Accepted |
| [0072](0072-exclude-quota-from-fleet-savings.md) | Exclude quota/CRQ from fleet savings to avoid double-count | API Design | Accepted |
| [0073](0073-dynamic-openapi-x-plugin-required.md) | Use dynamic OpenAPI filtered by x-plugin-required | API Design | Accepted |
| [0074](0074-manual-openapi-contract-tests.md) | Use manual OpenAPI + contract tests, not code-first codegen | API Design | Accepted |
| [0075](0075-gzip-responses-over-1kb.md) | Use gzip for responses >1KB | API Design | Accepted |
| [0076](0076-request-scoped-enrichment-cache.md) | Use request-scoped enrichment cache for cost rates | API Design | Accepted |
| [0077](0077-notification-codes-catalog-endpoint.md) | Use GET /notification-codes public catalog | API Design | Accepted |
| [0078](0078-nested-node-list-medium-term-cost.md) | Use nested node list with medium-term cost row for shared classification | API Design | Accepted |
| [0079](0079-gpu-node-pagination-sql-triples.md) | Push GPU/node pagination into SQL triple expansion | API Design | Accepted |
| [0080](0080-csv-export-format-param.md) | Use CSV export via format=csv on list endpoints | API Design | Accepted |
| [0081](0081-meta-currency-propagation.md) | Use meta.currency + per-object currency propagation | API Design | Accepted |
| [0082](0082-recalculate-savings-async-202.md) | Use internal POST /recalculate-savings async 202 | API Design | Accepted |
| [0083](0083-capabilities-endpoint-locked-settings.md) | Use capabilities endpoint listing locked settings fields | API Design | Accepted |
| [0084](0084-three-tier-settings-precedence.md) | Use three-tier settings precedence: env lock → DB → default | API Design | Accepted |
| [0085](0085-threshold-cache-ttl-60s-async-recalc.md) | Use per-org threshold cache TTL 60s with async recalc on PUT | API Design | Accepted |
| [0086](0086-single-flight-threshold-recalc.md) | Use single-flight coalescing per (org_id, recommendation_type) on recalc | API Design | Accepted |
| [0087](0087-namespace-memory-trend-5x-container.md) | Use namespace memory trend threshold 5× container (500 KiB/day) | API Design | Accepted |
| [0088](0088-kafka-s3-pipeline-both-modes.md) | Use Kafka + S3 pipeline for on-prem and SaaS (no custom /ingest) | Ingestion | Accepted |
| [0089](0089-manual-kafka-commit-after-success.md) | Use manual Kafka commit after successful processing | Ingestion | Accepted |
| [0090](0090-dlq-after-5-retries.md) | Use DLQ after 5 transient retries with forensic headers | Ingestion | Accepted |
| [0091](0091-incremental-digest-flush-streaming.md) | Use incremental digest flush during streaming CSV parse | Ingestion | Accepted |
| [0092](0092-ingest-statement-timeout-120s.md) | Use separate ingest statement timeout (120s) via SET LOCAL | Ingestion | Accepted |
| [0093](0093-chunked-pgx-batches-500.md) | Use chunked pgx batches (max 500 queued) | Ingestion | Accepted |
| [0094](0094-split-transactions-50k-rows.md) | Use split transactions above 50k rows per phase | Ingestion | Accepted |
| [0095](0095-csv-type-longest-prefix-first.md) | Use DetermineCSVType longest-prefix-first + contains fallback | Ingestion | Accepted |
| [0096](0096-strict-analytics-mode-optional.md) | Use strict analytics mode optional (ROS_INGEST_STRICT_ANALYTICS) | Ingestion | Accepted |
| [0097](0097-csv-contract-test-operator-headers.md) | Use CSV contract test against operator headers | Ingestion | Accepted |
| [0098](0098-csv-float-to-int64-parse-time.md) | Convert CSV floats to int64 at parse time with NaN/Inf rejection | Ingestion | Accepted |
| [0099](0099-compile-time-in-process-plugins.md) | Use compile-time in-process plugins over gRPC/Wasm/.so | Plugins | Accepted |
| [0100](0100-trait-interfaces-for-plugins.md) | Use trait interfaces (CSVIngestor, IngestHook, APIProvider, …) | Plugins | Accepted |
| [0101](0101-ingest-hook-metric-rows-not-db-reread.md) | Pass []MetricRow to IngestHook, not DB re-read | Plugins | Accepted |
| [0102](0102-ingest-hook-failures-non-fatal.md) | Treat IngestHook failures as non-fatal | Plugins | Accepted |
| [0103](0103-phased-execution-produce-enrich-optimize.md) | Use phased execution (Produce/Enrich/Optimize) with priority ordering | Plugins | Accepted |
| [0104](0104-kruize-mutually-exclusive-native.md) | Make Kruize mutually exclusive with native plugins | Plugins | Accepted |
| [0105](0105-container-handlers-in-core.md) | Keep container handlers in core; plugins register domain routes | Plugins | Accepted |
| [0106](0106-gpu-api-enricher-on-container.md) | Use GPU as APIEnricher on container responses | Plugins | Accepted |
| [0107](0107-retention-provider-per-plugin.md) | Use RetentionProvider per plugin with core fallback slice | Plugins | Accepted |
| [0108](0108-term-provider-per-plugin.md) | Use TermProvider per plugin with different default windows | Plugins | Accepted |
| [0109](0109-vm-plugin-feature-gate.md) | Gate VM plugin with ROS_ENABLE_VM_RECS | Plugins | Accepted |
| [0110](0110-example-plugin-trait-checklist.md) | Use _example plugin as compile-time trait checklist | Plugins | Accepted |
| [0111](0111-rates-from-koku-masu.md) | Source all rates from Koku Masu effective_rates | Cost / Savings | Accepted |
| [0112](0112-bounded-lru-ttl-cost-cache.md) | Use bounded LRU+TTL cost cache (max 1000 entries) | Cost / Savings | Accepted |
| [0113](0113-nil-cost-provider-when-masu-unavailable.md) | Use NilCostDataProvider when Masu unavailable | Cost / Savings | Accepted |
| [0114](0114-notif-no-cost-data-container-node-pvc.md) | Emit notification code 25 (NotifNoCostData) on container/node/PVC, not GPU | Cost / Savings | Accepted |
| [0115](0115-gpu-mig-idle-persist-timeslicing-read-time.md) | Persist GPU MIG/idle savings; compute time-slicing at read time | Cost / Savings | Accepted |
| [0116](0116-snapshot-cost-fallback-chain.md) | Use snapshot cost chain: Settings → env → effective_rates → $0.05 default | Cost / Savings | Accepted |
| [0117](0117-savings-include-all-cost-types.md) | Include infrastructure + supplementary + distributed costs in savings | Cost / Savings | Accepted |
| [0118](0118-invalidate-cost-cache-on-settings-change.md) | Invalidate cost cache on threshold settings change | Cost / Savings | Accepted |
| [0119](0119-tags-source-db-on-prem.md) | Use on-prem DB join to Koku tag tables (ROS_TAGS_SOURCE=db) | Tags | Accepted |
| [0120](0120-saas-http-push-tag-sync.md) | Use SaaS HTTP push full-replace sync | Tags | Accepted |
| [0121](0121-koku-and-legacy-tag-filter-syntax.md) | Support Koku-style filter[tag:key] and legacy tag=key:value | Tags | Accepted |
| [0122](0122-tags-enabled-by-default.md) | Default ROS_TAGS_ENABLED=true after stabilization | Tags | Accepted |
| [0123](0123-sa-tokenreview-allowlist-internal.md) | Use SA TokenReview allowlist for internal endpoints | Tags | Accepted |
| [0124](0124-koku-reship-ros-rebuild-bh.md) | Trigger Koku reship_ros to rebuild BH digests from S3 | Reship / Business Hours | Accepted |
| [0125](0125-single-flight-trailing-reship.md) | Use single-flight lock + trailing reship on concurrent schedule edits | Reship / Business Hours | Accepted |
| [0126](0126-forward-only-fallback-reship-failure.md) | Use forward-only fallback when reship fails after max retries | Reship / Business Hours | Accepted |
| [0127](0127-dual-digest-schedule-type-column.md) | Store dual digest streams (schedule_type=all_hours|business_hours) | Reship / Business Hours | Accepted |
| [0128](0128-unify-gorm-pgxpool-stdlib.md) | Unify GORM and pgxpool via stdlib.OpenDBFromPool | Deployment / Ops | Accepted |
| [0129](0129-multi-mode-cobra-binary.md) | Use separate processes (api, processor, housekeeper, poller) from one binary | Deployment / Ops | Accepted |
| [0130](0130-shallow-readyz-default.md) | Use shallow /readyz by default; optional deep checks | Deployment / Ops | Accepted |
| [0131](0131-housekeeper-batched-pk-deletes.md) | Use housekeeper batched PK deletes (5000 rows) for source cleanup | Deployment / Ops | Accepted |
| [0132](0132-retention-policies-per-table.md) | Use retention: 6mo digests, 90d history, 30d stale recs, 48h snapshot inventory | Deployment / Ops | Accepted |
| [0133](0133-structured-logging-zerolog.md) | Use structured logging with org_id, cluster_uuid, request_id | Deployment / Ops | Accepted |
| [0134](0134-postgresql-16-target.md) | Use PostgreSQL 16 target | Deployment / Ops | Accepted |
| [0135](0135-centralized-viper-config.md) | Centralize config in internal/config.Config (Viper) | Deployment / Ops | Accepted |
| [0136](0136-operational-runbooks-adversarial-review.md) | Use operational runbooks + adversarial review as first-class docs | Deployment / Ops | Accepted |
| [0137](0137-migration-lint-concurrently-template.md) | Use migration lint + CONCURRENTLY job template for large-table indexes | Deployment / Ops | Accepted |
| [0138](0138-mkdocs-public-site-separate.md) | Use MkDocs public site separate from internal docs | Deployment / Ops | Accepted |
| [0139](0139-testcontainers-pg16-integration.md) | Use testcontainers PostgreSQL 16 + golang-migrate for integration tests | Testing | Accepted |
| [0140](0140-kruize-vs-native-comparison-tool.md) | Use Kruize vs Native comparison tool for algorithm validation | Testing | Accepted |
| [0141](0141-openapi-contract-tests-all-plugins.md) | Use OpenAPI contract tests on every plugin endpoint | Testing | Accepted |
| [0142](0142-csv-contract-test-operator-columns.md) | Use CSV contract test tied to operator column headers | Testing | Accepted |
| [0143](0143-dry-run-sql-org-id-assertion.md) | Use dry-run SQL tests asserting org_id on detail queries | Testing | Accepted |
| [0144](0144-colocated-domain-tests.md) | Keep domain tests colocated; add wiring tests per plugin extraction | Testing | Accepted |
| [0145](0145-deny-private-networks-csv-fetch.md) | Deny private networks on CSV URL fetch unless development | Security | Accepted |
| [0146](0146-csv-url-allowlist-non-dev.md) | Require explicit CSV URL allowlist in non-dev | Security | Accepted |
| [0147](0147-escape-ilike-wildcards.md) | Escape ILIKE wildcards in filter values | Security | Accepted |
| [0148](0148-redact-kafka-poison-payloads.md) | Redact Kafka poison payloads in logs by default | Security | Accepted |
| [0149](0149-block-dev-token-outside-development.md) | Block ROS_TAGS_DEV_TOKEN outside development | Security | Accepted |
| [0150](0150-validate-sa-allowlist-at-startup.md) | Validate empty SA allowlist blocks api-mode tag auth in prod | Security | Accepted |
| [0151](0151-rbac-fail-closed-cache-60s.md) | Use RBAC fail-closed with in-memory cache (60s TTL) | Security | Accepted |
| [0152](0152-cap-history-filter-cardinality.md) | Cap history filter param cardinality (5 values per param) | Security | Accepted |
| [0153](0153-kafka-category-ros-filter.md) | Consume hccm.ros.events with category=ros filter | Kafka | Accepted |
| [0154](0154-partition-scoped-worker-pool.md) | Use partition-scoped worker pool with ordering preserved per partition | Kafka | Accepted |
| [0155](0155-retry-counter-x-retry-count-header.md) | Use retry counter via X-Retry-Count header on requeue | Kafka | Accepted |
| [0156](0156-sources-destroy-events-cleanup.md) | Use Sources destroy events for tenant cleanup | Kafka | Accepted |
| [0157](0157-ros-enabled-plugins-replaces-native-flag.md) | Replace ROS_USE_NATIVE_ENGINE with ROS_ENABLED_PLUGINS + Kruize exclusivity | Configuration | Accepted |
| [0158](0158-enabled-or-disabled-plugins-env.md) | Use ROS_ENABLED_PLUGINS allowlist OR ROS_DISABLED_PLUGINS blocklist | Configuration | Accepted |
| [0159](0159-per-plugin-term-env-vars.md) | Use per-plugin term env vars ROS_TERMS_<PLUGIN>_<TERM>_* | Configuration | Accepted |
| [0160](0160-savings-estimates-kill-switch.md) | Use ROS_SAVINGS_ESTIMATES_ENABLED global kill-switch | Configuration | Accepted |
| [0161](0161-staleness-threshold-hours-alias.md) | Use ROS_STALENESS_THRESHOLD_HOURS=48 with alias | Configuration | Accepted |
| [0162](0162-housekeeper-graceful-shutdown.md) | Use housekeeper graceful shutdown with configurable grace period | Configuration | Accepted |
| [0163](0163-deprecate-kruize-plugin.md) | Deprecate and remove the Kruize plugin | Plugins | Accepted |
