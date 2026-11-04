# #758 Container Digest Staging — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step. Owner-locked design (2026-11-04): **Option B — hybrid staging**: an in-memory manifest batch (A's performance) with a finite staging threshold; past the threshold, raw samples stage to a per-manifest UNLOGGED table and the finalize computes digests per key streaming. Memory bounded in every mode; lossless by construction.

**Goal:** a manifest's container files covering disjoint portions of one container/day produce the complete daily distribution (sample_count=2 for the audit's two-observation proof), both files done; replay idempotent; later independent payloads still replace.

**Mechanism being fixed (verified at HEAD):** per-file streams each build `map[DigestKey][]metricSample` and upsert at EOF; `pgdigest` upsert is full replacement per key → file 2 replaces file 1 (audit: count=1, p95=100, max=100). Latent adjacent bug: the opt-in `ROS_INGEST_FLUSH_BATCH_SIZE` mode flushes AND CLEARS mid-file — a key straddling a flush boundary loses its earlier samples (same class, within one file). The batch's staging fixes both for batched files; the standalone per-file path keeps today's semantics (scope note).

**Memory rationale (owner discussion, 2026-11-04):** KMO packages at 100MB/file; ~600k samples ≈ 120-200MB in memory per max file TODAY. A pure manifest batch (option A) scales with file count (GBs for big clusters) and voids the knob — rejected. The hybrid bounds in-memory groups at a threshold (the knob if set, else `defaultContainerStageThreshold = 8192` groups ≈ today's per-file ceiling); overflow samples go to disk; finalize streams per key.

## Architecture

- `ingestion.ContainerDigestBatch` (new, `internal/ingestion/container_batch.go`): owns the manifest's shared stream state — `groupedAll`/`groupedBH`, `scheduleCache`, `gpuAccum`/`gpuBHAccum`, `nodeAccum`/`nodeBHAccum`, `nodeClusterSched`, `writeNodeBH`/`writeGPUBH`, row/group totals. Mirrors what `parseAndDigestCSVStream` builds per file today.
  - `NewContainerDigestBatch(ctx, pool, orgID, clusterUUID, manifestID)`: BH cache load + pruning (as the stream does); DELETEs stale staged rows for the manifest (crash-recovery hygiene — unlogged tables truncate on crash anyway, but clean start is cheap and correct for soft failures).
  - `AddFile(ctx, r) (rows []MetricRow, err error)`: streams rows into the shared state (appendGroupedRow / appendBusinessHoursRow / gpu/node accumulator adds). When in-memory group count ≥ the staging threshold: `stageDigestGroups` — INSERT raw samples to the staging table, clear the maps (the knob's memory bound, preserved). Returns rows for hook dispatch (nil today — ParseAndDigestCSV returns none — kept for contract parity).
  - `Flush(ctx)`: finalize. Staged keys: SELECT ... WHERE manifest_id ORDER BY key columns, accumulate per key, MERGE with any in-memory remainder for the same key, compute+upsert in key-chunks (memory bounded); memory-only keys upsert as today; gpu/node accumulators write via the existing paths; single-tx path when nothing staged and totals are eligible (`singleIngestTxEligible`); DELETE the manifest's staged rows.
- Staging table (migration 000217): `container_metric_samples_staging` — UNLOGGED, full DigestKey + metricSample columns + manifest_id, index on manifest_id. Scratch only: no FKs, no models.
- `services.processContainerFileBatch` (parallel_ingest.go): partitions container files out of `remaining` (alongside the VM partition), downloads → `batch.AddFile` per file (bypassing per-file DispatchCSV — justified: the container plugin's IngestCSV returns no rows, hooks no-op on empty; same precedent as #707/#757), per-file error isolation, done-marking after Flush. Transient errors thread through WITHOUT blocking other groups (the #757 F1 lesson, from the start): vm/container/other groups all process; first transient returned.
- Standalone paths unchanged: `ParseAndDigestCSV` / `ProcessCSVToDigests` / `processContainerDigestFallback` keep per-file semantics for CLI/tools/tests/fallback (documented scope note).

## Test matrix

- RED on baseline (existing API — `ParseAndDigestCSV` per file, and services-level through the current per-file path): the audit's proof (08:00=2000m + 09:00=100m → count=1, max=100), reversed order (2000 survives), same-valued control (count must be 2 — collision guard).
- Batch (new API, GREEN-tier): multi-file merge both orders; replay idempotency; later payload replaces; staging threshold=1 (mid-file staging + staged/memory merge correctness); BH stream completeness (schedule fixture per existing BH tests); crash-retry cleanliness (failed Flush → new batch → clean).
- Services: done=2 + count=2 + max=2000; replay skips; later fuller payload replaces; mixed manifest (container + other types + VM files) — all groups process.

## Constraints
- No percentile-merging math — raw samples unioned (criterion: "complete percentiles... not GREATEST").
- Existing suites green: pipeline tests, BH tests, fallback path, ProcessCSVToDigests callers, #757's tests (parallel_ingest threading changes touch the same function).
- Commits wait for owner go-ahead. Push: fork, branch `pgarciaq-rosocp-superpowers-phase17`.
