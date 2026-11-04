# #757 VM Companion Deferral — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:test-driven-development governs every step. Owner locked the design 2026-11-04: **Option A + orphan-not-done, default retry ladder, no knob changes.**

**Goal:** a fresh VM/day with usage plus PVC/GPU companions persists the parent and every real attachment before marking dependent files done; companions whose usage never arrives are NOT treated as successful persistence (transient error → existing retry ladder → DLQ, per #806's evidence-gated future).

**Architecture:** `VMDigestBatch.AddFile` buffers companion rows per file (parse now, no DB); `Flush` writes parents (existing order: all-hours → BH → hourly), then applies each companion file in AddFile order — same per-file replacement semantics as today (delete-then-insert per digest), just later in time. Each buffered companion carries a post-Flush orphan status. `processVMFileBatch` marks succeeded files done EXCEPT orphaned companions, then returns a transient error naming missing parents (drives the existing ladder; classified transient explicitly, not via the fallback-true).

**Recon-verified facts:**
- Mechanism at HEAD = filed: companions ingest immediately (lookup → warn-skip → nil), usage accumulates to Flush; even usage-first ordering loses. Zero drift on all four files.
- `AddFile`'s only production caller: `parallel_ingest.go:213`. `IngestVMGPUDeviceCSV`/`IngestVMPVCCSV` called only from `AddFile` (verify cmd/librobne before restructuring).
- `NewVMDigestBatch` already loads `bhCache` when BH enabled — the deferred GPU BH accumulation reuses it (mirrors `IngestVMGPUDeviceCSV`'s own cache load).
- PVC: `finalizeVMPVCGroups(acc) → map[VMDigestKey][]IngestPVCDigest`; `BatchLookupVMDigestIDs` (batched); `UpsertVMPVCDigests` delete-then-insert. No schedule variants.
- GPU: acc → `applyVMGPUDeviceAcc → digestMap` → per-schedule `upsertVMGPUDevicesForSchedule` (per-key `LookupVMDigestID`, warn-skip) → `UpsertVMGPUDevices` delete-then-insert. BH variant via schedule type.
- Parents: `UpsertDailyVMDigests` = `ON CONFLICT (natural key) DO UPDATE` — digest_id stable across replacement.
- `isTransientKafkaProcessingError` defaults unknown errors to transient; the orphan error gets explicit classification anyway.
- Services scaffolding: `model.EnsureReportFileExpectations` / `GetReportFileStatus` / `MarkReportFileDone`; `report_file_tracker_test.go` shows patterns; `ReadCSVBodyFromUrl` is http (httptest server).
- Fixtures: `vmSplitDayFile`, PVC CSV columns (`interval_start,...,pvc_name,disk_capacity_bytes,volume_mode`), `vmGPUDeviceCSVHeader()`.

## Global Constraints
- Per-file companion semantics preserved (NO cross-file merging — today's per-file delete-then-insert replacement).
- The three existing batch regressions (MultiFileDayMerges, SecondPayloadReplaces, BatchFileFailureIsolation) must stay green, semantics unchanged.
- No new config knobs; no ladder changes (#806 owns the time-scale follow-up).
- Failure between parent persistence and attachment: Flush returns error → nothing marked done → idempotent retry.
- BH dimension: companions accumulate BH rows only when the batch's cache produces BH digests (mirror today's gating).
- Commits wait for owner go-ahead. Push: fork, branch `pgarciaq-rosocp-superpowers-phase17`.

## Review Focus
1. Same-manifest attach regardless of order (T1/T2) — the filed bug.
2. Orphan not-done + transient error (T5/T7b) — the criterion.
3. Replay + replacement idempotency (T3/T4/T7c).
4. Between-persistence failure leaves nothing done (T6).
5. Per-file replacement semantics preserved (no cross-file companion merging).
6. BH schedule variants covered.

---

### Task 1: RED regression suite
**Files:** `internal/ingestion/vm_batch_companion_test.go` (new), `internal/services/parallel_ingest_companion_test.go` (new)

- T1 usage-first: usage + PVC + GPU for a NEW VM/day → Flush → parent=1, PVC rows>0, GPU rows>0. **RED on baseline** (pvc=0, gpu=0 — the audit's exact numbers).
- T2 companion-first: same assertions, companions added before usage. **RED on baseline.**
- T4 later fuller payload: batch2 replaces usage → parent replaced, attachments still present. **RED on baseline** (attachment assertion).
- T3 batch-level replay (second batch, same files): counts stable — replacement-semantics guard (GREEN both; the *done-marking* replay is T7c).
- T5 orphan: companion for never-seen VM → handle `.Orphaned()` true, missing names the key; attached companions not orphaned. Written against the new API (compile-RED on baseline is expected; T1/T2/T4 carry the behavioral RED).
- T6 between-persistence failure: `companionFlushHook` (package var, nil in prod) returns error → Flush errors after parents written → attachments absent. GREEN-tier guard.
- T7 services-level `processVMFileBatch` (httptest CSV server + manifest expectations): (a) all files done AND rows present — **RED on baseline** (done but rows absent); (b) orphan companion not done + transient error, usage file done; (c) replay: second call skips all, returns clean.
- Run → RED: T1/T2/T4/T7a fail with the filed numbers; controls pass. Ledger the output.

### Task 2: Implementation
- `vmCompanionFile` (exported handle) + per-file buffers on the batch; `AddFile` returns `(kind, *VMCompanionFile, error)`; companions parse+accumulate, no DB.
- `Flush` tail: `flushCompanions` — per file in order: PVC via extracted `attachVMPVCGroups` (missing []VMDigestKey), GPU per schedule via extracted `attachVMGPUDevicesForSchedule`; missing keys → file orphaned + missing list; attach otherwise.
- Keep `IngestVMPVCCSV`/`IngestVMGPUDeviceCSV` as wrappers over the extracted helpers (verify cmd/librobne callers first).
- `processVMFileBatch`: per-file handles; mark done except orphaned; orphan presence → typed transient error (explicit classifier entry in `kafka_processing_errors.go`).
- Update the three existing tests' AddFile call sites if needed (mechanical; assertions unchanged).
- GREEN; full ingestion + services packages.

### Task 3: Mutations + full verification
- Mutation (a): revert companion deferral in AddFile (immediate ingest) → T1/T2/T7a fail. Mutation (b): orphan → done (drop the not-done branch) → T7b fails. Restore.
- `make lint`, `make test` full suite (headline).

### Task 4: Fresh-context review (most capable model) → one fix pass if needed.

### Task 5: Close-out
- CHANGELOG entry; completion report with close-criteria mapping, verified/asserted, #806 cross-reference; commit on owner go-ahead.
