# VM Multi-File Payload Digest Batching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** VM CSV files within one payload are digested in a single pass, so a day split across files merges instead of clobbering; #707's parity divergence closes and the VM legs of the parity comparison go green.

**Architecture:** The report processor currently dispatches each VM-family file (usage / gpu-device / pvc) to an independent `ProcessVMCSV` call whose digest upsert is full-replacement — same-day rows across files are lost to last-write-wins. The fix batches all VM-family files of one manifest into one digest pass (shared accumulators across per-file readers), preserving per-file manifest state (markFileProcessing/Done) and the cross-payload replacement semantics (KMO's monthly accumulator re-sends a fuller month; replacement across payloads is correct and stays).

**Tech Stack:** Go 1.26, internal/ingestion (pgx upserts), librobne (unchanged — the CLI already digests a directory in one pass).

**Issue:** #707 (root cause pinned in its comments; the original gate-divergence framing is corrected there). ADR: none — the batch-within-payload / replace-across-payloads decision lives in the issue and the code comment (single-entity ingestion fix, not a shared-contract change).

## Global Constraints

- **Cross-payload replacement semantics are preserved.** Two manifests re-sending overlapping days must still end with the later payload's digests (the KMO monthly-accumulator model). Batching is strictly within one manifest's file set.
- **Per-file manifest state is preserved.** Each VM file still gets its own markFileProcessing / markFileDone (or permanent-error handling); the #650 fail-safe and retry semantics must not change. A file that fails mid-batch must not silently pass its batch-mates.
- **The VM-family never-fails companions contract stays** (gpu-device / pvc degrade, never gate).
- **robne is unchanged** — it already merges a directory in one pass; the fix makes the server match. The shared ≥20 agent-sample percentile minimum must end up behaving identically on both paths (verify the constants are literally the same value; single-source the constant if they are not already shared).
- `make lint` (golangci-lint ≥2.15.0, `bin/golangci-lint`) and `make test-short` green at every task boundary; ingestion tests unchanged in behavior except where this fix is the point.

## Review Focus

Input classes the fix must handle; each pinned by the named test in its owning task:

1. **Day split across two usage files** — 77 rows (00:00–19:00) + 19 rows (19:15–23:45) for the same (vm, day): the merged digest has 96 samples, 96 agent samples, and computed `mem_available` percentiles. Pinned by `TestProcessVMCSV_MultiFileDayMerges` (Task 2; fails pre-fix with 19 samples and NULL percentiles — the exact #707 evidence).
2. **Genuinely partial day** — all files of the payload sum to < 20 agent samples for a day: percentiles are withheld (NULL) — and the same input through `librobne/csv.DailyVMDigests` withholds identically. Pinned by `TestVMAgentMinimumSharedBothPaths` (Task 4).
3. **Cross-payload replacement** — a second manifest re-sending the same (vm, day) with fuller data replaces the first payload's digest (no accumulation). Pinned by `TestProcessVMCSV_SecondPayloadReplaces` (Task 3).
4. **Mixed VM-family multi-file payload** — usage split across two files plus gpu-device and pvc companions: all companions processed, digest intact. Pinned by `TestProcessVMCSV_MultiFileMixedFamily` (Task 3).
5. **Mid-batch file failure** — the second of two usage files fails to download/parse: the first file's state is not corrupted, the failed file takes the existing permanent-error path, and batch-mates are marked per their own outcomes. Pinned by `TestProcessVMCSV_BatchFileFailureIsolation` (Task 3).

## Tasks

### Task 1: Recon + spec line + constants check

- [ ] Recon the dispatch loop that feeds `parallel_ingest.go`'s per-file switch (where the manifest's file list iterates) — identify the batching point that can collect VM-family file URLs while preserving per-file state transitions.
- [ ] Verify the two agent-sample minimums are the same literal value (`minAgentSamplesForPercentile` in `internal/ingestion/vm_digest_builder.go`, `vmMinAgentSamplesForPercentile` in `librobne/csv/digest_vm.go`); if duplicated, single-source the constant where both can import it.
- [ ] Spec line (`docs/plans/robne-cli-spec.md`, Entity CSVs vs ingest section): "multiple VM usage files in one payload are one digest pass on both paths (#707) — same-day rows merge; a later payload's re-sent month replaces."
- [ ] `make docs-lint docs-drift docs-sync-check`.

### Task 2: Detector test, RED

- [ ] Write `TestProcessVMCSV_MultiFileDayMerges` (Review Focus 1) against the server ingest path: two VM usage CSVs splitting one day 77/19 with agent values on every row → assert the stored digest has 96 samples, 96 agent samples, non-NULL `mem_available_p50/p95`. Verify it FAILS pre-fix for the #707 mechanism (last file's 19-sample digest replaces; 19 < 20 → NULL percentiles), reading the failure to confirm the mechanism.

### Task 3: The fix — payload-level VM batching

- [ ] Batch VM-family files at the dispatch layer: collect the manifest's VM-family file URLs, process them through one digest pass (shared accumulators across per-file readers — `ProcessVMCSV` refactored to accept multiple readers or an aggregate row callback), with per-file markFileProcessing/Done preserved and per-file error isolation (Review Focus 5).
- [ ] Task 2 GREEN. New tests: `TestProcessVMCSV_SecondPayloadReplaces` (Review Focus 3), `TestProcessVMCSV_MultiFileMixedFamily` (Review Focus 4), `TestProcessVMCSV_BatchFileFailureIsolation` (Review Focus 5).
- [ ] `docs/agents/db-upsert-safety.md`: apply the checklist to the changed upsert path; add/update the VM row in *Currently Protected Functions* if the function set changed.
- [ ] `make test-short`; `make lint`.

### Task 4: Shared-minimum pin (both paths)

- [ ] `TestVMAgentMinimumSharedBothPaths` (Review Focus 2): a day with < 20 agent samples across all files → server digest percentiles NULL **and** `librobne/csv.DailyVMDigests` withholds identically for the same rows.
- [ ] If Task 1 found the constants duplicated, this test pins the single-sourced value.

### Task 5: Changelog + public page check

- [ ] `docs-site/changelog.md` [Unreleased] fix entry: VM recommendations from multi-file payloads no longer degrade (same-day rows previously lost to per-file last-write-wins).
- [ ] Check `docs-site/features/virtual-machines.md` for any upload/file-structure language; add one line only if it says something now untrue.
- [ ] `make docs-lint docs-drift docs-sync-check`.

### Task 6: Parity re-run — the acceptance gate

- [ ] Run the parity validation per `.cursor/skills/server-robne-parity/SKILL.md` with the #707 fixture payload (the 77/19 split — `.superpowers/tmp/parity-20261027/payloads/vm-13d` regenerated or reused) plus the standing set; the VM legs green (the 25/6 diffs gone), all other legs stay green.
- [ ] Record the run on #707 and post the completion report; the 10/10 gate this issue owns (inherited from #706) is met.

## Fixture Provenance

- **The 77/19 split:** the #707 evidence itself — cluster `a6d781ac`, VM `multi-gpu-mixed-vm-01`, Oct 25 split at 19:15 by nise's row-limit file split; every row carries `memory_available_kib`. Server-side failure values on record: 19 samples, NULL percentiles, recommendation 65 GiB (usage branch) vs robne's 37 GiB (agent branch). The Go test reconstructs the split deterministically.
- **Cross-payload case:** the KMO monthly-accumulator model — the same payload re-sent with a fuller month must replace (do not accumulate). Derived from the vm_upsert ON CONFLICT DO UPDATE semantics, which are correct across payloads and must survive the batching.

## Non-goals

- No change to the latest-day guest-agent gate or the ≥20 agent-sample minimum's value (both already shared semantics; only their constancy across paths is pinned).
- No consolidation of the two digest builders into one implementation (a #705-pattern candidate, deliberately out of scope; only the constant is single-sourced if duplicated).
- No robne changes (already correct).
- No merge-on-upsert semantics (ruled out: double-counts KMO monthly re-sends).
