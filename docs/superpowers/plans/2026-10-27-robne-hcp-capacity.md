# robne CLI HCP Fleet-Capacity Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `robne recommend/validate/diff/explain` understand the W4 HCP fleet-capacity snapshot CSV, producing the same advisory as the service from the same shared math.

**Architecture:** Relocate the 25-column CSV parser from `internal/ingestion` to `librobne/csv` (single source for backend + CLI); extract the serve-time derivation rules from the API handler into `librobne/hcp/serve.go` (characterization-golden-guarded); wire a new `hcp` CLI plugin through the existing entity machinery with envelope v12; compute per-observation scenarios via `CalculateCapacity` at read time.

**Tech Stack:** Go 1.26, librobne (CGO-free nested module, vendored into the root module), cobra-style CLI in `cmd/robne`.

**Spec:** `docs/superpowers/specs/2026-10-26-robne-hcp-capacity-design.md` (normative; round references are provenance) + ADR-0345. Plan review round 9 (fresh-eyes, code-verified) applied 2026-10-27.

## Global Constraints

- librobne stays CGO-free and dependency-light; `make robne` must keep building with `CGO_ENABLED=0`.
- The 25-column header is the KMO-writer ↔ backend-reader ↔ CLI-reader triple contract; any change coordinates all sides. Header (exact order): `observed_at,mce_sampled_at,operator_version,ocp_version,mce_version,native_model_version,collection_interval_minutes,upload_cycle_minutes,inventory_complete,current_hc_count,ready_hcp_count,average_ready_hcp_qps,ready_hcp_qps_known,hosted_availability_basis,eligible_worker_cpu_allocatable_millicores,eligible_worker_memory_allocatable_bytes,eligible_worker_max_pods,worker_placement_known,pressure_state,mce_request_max_hcs,mce_low_qps_max_hcs,mce_average_qps_max_hcs,mce_medium_qps_max_hcs,mce_high_qps_max_hcs,shared_infrastructure`.
- Backend ingestion behavior must not change: parser move + reason-returning API extension only; ingestion keeps count-based metrics; all `internal/ingestion` tests stay green.
- API handler behavior must not change: characterization goldens land before the serve-seam refactor and stay byte-identical after.
- Canonical plugin names are backend names: `hcp`, `cluster-quota`. No aliases. The renames (`cluster_quota`→`cluster-quota` flag/YAML spelling; JSON root `recommendations`→`container_recommendations`) are pre-release breaks in this issue's window: no version bump, goldens rewritten, migration note in spec + public docs. JSON sibling keys stay stable snake_case (`cluster_quota_recommendations` unchanged; new sibling `hcp_recommendations`).
- Every librobne change requires `go mod vendor` in the root module before internal/ or cmd/ can see it.
- `make lint` (golangci-lint; use `bin/golangci-lint` at 2.15.0+ — a stale cached binary passes files CI rejects) and `make test-short` must pass at every task boundary.

## Review Focus

The spec implies these input classes; each is pinned by the named test in its owning task (spec silence is not permission to break on them):

1. **Recollected window** — two rows sharing `observed_at` with changed `eligible_worker_*`: series keeps the LAST (backend last-write-wins). Pinned by `TestAssembleCapacitySeries_RecollectLastWins` (Task 4).
2. **Clock-skewed payload** — every row future-dated beyond the 5-minute tolerance with `--plugins hcp` explicit: the error names clock skew, never "no capacity snapshot". Pinned by `TestCompute_HCPCapacityAllSkewDroppedDiagnosis` (Task 5).
3. **Month-boundary pair** — `ros-openshift-hcp-capacity-202609.csv` + `-202610.csv` in one directory: one merged newest-first series. Pinned by `TestAssembleCapacitySeries_MonthBoundaryMerge` (Task 4).
4. **Poisoned capacity CSV in a mixed payload** — header-mismatched capacity file beside valid container CSVs: warn to stderr, continue, container recs unaffected. Pinned by `TestLoad_CapacityPoisonMixed` (Task 1).
5. **Business hours + capacity** — BH-enabled run with capacity rows emits envelope v12 carrying both siblings (version number AND sibling presence). Pinned by `TestEnvelopeVersion_BHAndCapacity` (Task 3, version) plus a BH+capacity full-envelope golden added to Task 4's parity test family (round-9 N8: a version-number unit test cannot pin sibling presence).

---

### Task 1: Shared capacity parser in `librobne/csv` + Load wiring

**Files:**
- Create: `librobne/csv/hcp_capacity.go`, `librobne/csv/hcp_capacity_test.go`
- Modify: `librobne/csv/load.go` (LoadResult fields, parseCSVReader case, finishLoad), `librobne/csv/detect.go:31` (ClassifyFilename prefix), `librobne/csv/load.go` or a new `warnings.go` for `LoadWarning`
- Modify: `internal/ingestion/hcp_capacity.go` (delete local parser, import shared), `internal/ingestion/hcp_capacity_test.go` (parse tests move; DB round-trip stays)
- Modify: `vendor/github.com/redhatinsights/ros-ocp-backend/librobne/csv/` (via `go mod vendor`)

**Interfaces:**
- Consumes: the parser body currently in `internal/ingestion/hcp_capacity.go` (25-column strict header, strict booleans, cadence bounds `1..86400`, model-version match, enum validation).
- Produces (later tasks rely on exactly these):
  - `type HCPCapacityRow struct` — as today (inputs + MCE refs; NO cluster field), moved verbatim.
  - `type RowSkip struct { Index int; ObservedAt string; Reason string }`
  - `func ParseHCPCapacityRows(r io.Reader) (rows []HCPCapacityRow, skips []RowSkip, err error)` — time-INDEPENDENT validation only; header mismatch still returns err.
  - `func HCPCapacitySkewCheck(rows []HCPCapacityRow, now time.Time) (kept []HCPCapacityRow, dropped []RowSkip)` — the >5-min future-skew rule, extracted.
  - `type LoadWarning struct { File, Kind, Reason string }`; `LoadResult.HCPCapacityRows []HCPCapacityRow`; `LoadResult.Warnings []LoadWarning`.
  - `KindHCPCapacity` added to the existing `Kind` **iota const block** in `librobne/csv/detect.go` (round-9 B2: `Kind` is `type Kind int` — a string literal will not compile); `ClassifyFilename` matches prefix `ros-openshift-hcp-capacity-` (no existing rule shadows it; mirror the `ros-openshift-hcp-capacity-` entry in `DetermineCSVType`, `internal/utils/utils.go`).

- [ ] **Step 1: Write the failing parser tests** — move the case table from `internal/ingestion/hcp_capacity_test.go` (TestParseHCPCapacityRows_Valid/SkipMalformed/HeaderMismatch) to `librobne/csv/hcp_capacity_test.go`, adapting: no `now` parameter (future-skew cases move out), and `skips` now carries reasons — assert each skip's `Reason` is non-empty and the case's intent (e.g., `"cadence out of bounds"`). Add `TestHCPCapacitySkewCheck` pinning: a row 10 minutes in the future drops with a skew reason; a row 3 minutes ahead stays; a past row stays.
- [ ] **Step 2: Run to verify red** — `cd librobne && go test ./csv/ -run 'TestParseHCPCapacityRows|TestHCPCapacitySkewCheck' -count=1`. Expected: FAIL, undefined symbols.
- [ ] **Step 3: Implement** — move the parser; split `parseHCPCapacityRecord` into time-independent validation (returns `(HCPCapacityRow, RowSkip, bool)`) and remove the skew branch into `HCPCapacitySkewCheck`. Fix the stale "24-column"/"24-field" comment strings to 25 (design N6).
- [ ] **Step 4: Write the failing Load tests** — `TestLoad_CapacityRows` (dir with a valid capacity CSV → `HCPCapacityRows` populated, no error), `TestLoad_CapacityPoisonMixed` (valid container CSV + header-mismatched capacity CSV → `Warnings` gains one entry, container rows present, NO error), `TestLoad_CapacityOnlyPoisonedAuto` (only a poisoned capacity CSV → Load succeeds via the new `hasCapacityAttempt` path with a warning, zero rows), `TestLoad_CapacityOnlyStandalone` (only a valid capacity CSV → `hasROS()` true).
- [ ] **Step 5: Verify red, implement, verify green** — `parseCSVReader` gains the `KindHCPCapacity` case converting file-level errors into `LoadWarning` + nil error (poison contract: never abort the run); `mergePart` appends rows and warnings; `finishLoad` succeeds on rows OR capacity warnings (`hasCapacityAttempt` helper — capacity only, no behavior change for other kinds; a header-only capacity CSV with zero data rows counts as an attempt, matching the snapshot precedent at `load.go:53-59` — round-9 N5); `KindHCPCapacity` stays OUT of the `failLoadOnMissingColumns` set.
- [ ] **Step 6: Rewire ingestion** — `internal/ingestion/hcp_capacity.go` deletes its local parser, calls `csv.ParseHCPCapacityRows` + `csv.HCPCapacitySkewCheck(rows, time.Now().UTC())`, keeps metrics (skipped count = len(skips)+len(dropped)) and logging unchanged. Run `go mod vendor` then `go test ./internal/ingestion/ -run 'TestParseHCPCapacity|TestProcessHCPCapacityCSV' -count=1` — DB round-trip green.
- [ ] **Step 7: Full check + commit** — `make lint && make test-short`; commit `feat(csv): shared HCP capacity parser and Load wiring (#700)`.

### Task 2: Serve-time derivation seam in `librobne/hcp`

**Files:**
- Create: `internal/api/handlers_hcp_capacity_golden_test.go` (characterization), `librobne/hcp/serve.go`, `librobne/hcp/serve_test.go`
- Modify: `internal/api/handlers_hcp_capacity.go` (refactor to the seam), `vendor/...` (resync)

**Interfaces:**
- Produces:
  - `type ServeObservation struct` — DB-free view: `ObservedAt, MCESampledAt time.Time(*); OperatorVersion, OCPVersion, MCEVersion, NativeModelVersion string; CollectionMinutes, UploadMinutes int64; InventoryComplete, QPSKnown bool; CurrentHC, ReadyHC int64; QPS *float64; Basis, Pressure string; SharedInfrastructure bool; Workers WorkerCapacity` (placement lives in `Workers.PlacementKnown` — round-9 W7, no duplicate top-level field); `MCERequest, MCELow, MCEAverage, MCEMedium, MCEHigh *int64; Scenarios []CapacityScenario` (the existing `librobne/hcp` type).
  - `func FreshnessWindow(collectionMinutes, uploadMinutes int64) time.Duration` (2× max).
  - `func BelowVersionFloor(ocpVersion string) bool` (4.17 floor; unparseable/empty never warns).
  - `func ScenarioStatus(basis string) string` (`illustrative_only` for mixed/single_replica, else `ok`).
  - `func DeriveHeadroom(o ServeObservation, scenario string, maxHCs int64, limiting string) *int64` (round-9 B4: the scenario name is required — the average+unknown-QPS nil rule cannot be expressed without it) — nil on empty limiting, incomplete inventory, unknown placement (`o.Workers.PlacementKnown`), pressure, or (scenario == `average_qps` + `!o.QPSKnown`); else signed max−current.
  - `func DeriveWarnings(o ServeObservation, now time.Time, mceMayBeStale *bool) ServeWarnings` (round-9 W1: the stale-caution input comes from `MCEMayBeStaleFromSeries` in both consumers — the API maps its DB window query, the CLI its in-payload series — so it is a parameter, not derived) — `{Stale, BelowVersionFloor, Pressured, PressureUnknown, SharedInfrastructure bool; MCEMayBeStale *bool}`.
  - `func MCEMayBeStaleFromSeries(series []ServeObservation) bool` — series is newest-first; window = FreshnessWindow of series[0] (relative to series[0], NOT `--now`); len<2 → false; true iff all in-window priors share series[0]'s five MCE refs while any native input differs. **Supersedes spec §2's `MCEStalePoint` sketch and its `MaxHCs`-only scenario shape** (round-9 W9) — build `[]ServeObservation`, not both.
  - Mapper in `internal/api`: `func snapshotToServeObservation(s model.HCPCapacitySnapshot) hcp.ServeObservation` (trivial field copies).

- [ ] **Step 1: Characterization goldens FIRST** — `TestHCPCapacityHandler_Characterization` table: pressured (headroom nil on all five), pressure_unknown, stale (age > 2× cadence), below-floor 4.16.x, frozen-MCE series, incomplete-inventory (headroom nil). Seed rows via the existing `api_test` helpers (`seedHCPCapRow` etc.) and **capture the raw HTTP response body** through the existing echo harness (round-9 W2a: `hcpCapacityRowResponse` is unexported in package `api` and unreachable from the helpers' `api_test` package). Pin the non-time-derived fields per case — scenario values, headroom, statuses, warnings, MCE refs (round-9 W2b: the handler calls `time.Now()` internally, so `sample_age_seconds`/`stale` cannot be byte-compared; seed `observed_at` relative to `time.Now()` and assert the remaining fields, the existing `TestHCPCapacityRow_ServeTimeSemantics` pattern). These must PASS against the CURRENT handler — characterization, not TDD.
- [ ] **Step 2: Verify green against current code; commit the goldens** (`test: characterization goldens for HCP capacity serve semantics (#700)`).
- [ ] **Step 3: Failing seam tests** — `librobne/hcp/serve_test.go` mirrors the derivation rules with hand-computed values (round-9 B3: window **2h** for 60/60 cadence — 2×max(60,60); the 12h figure belongs to the API test's 60/360 fixture; hand-check each nil case; MCE-frozen series from three observations with differing QPS → true; identical native inputs → false).
- [ ] **Step 4: Implement `serve.go`; refactor the handler** to map `model.HCPCapacitySnapshot` → `ServeObservation` and build the response from the rule functions (the DB fetch for the MCE window feeds `MCEMayBeStaleFromSeries` with mapped observations). CLI DTO stays unchanged this task.
- [ ] **Step 5: Verify** — `go mod vendor && go test ./internal/api/ -run 'TestHCPCapacity|TestOpenAPI_HCPCapacity' -count=1` — characterization goldens unchanged on the pinned fields, plus `make test-short`.
- [ ] **Step 6: Commit** `refactor(hcp): shared serve-time derivation seam (#700)`.

### Task 3: Envelope ladder, plugin registry, pre-release renames

**Files:**
- Modify: `cmd/robne/output.go` (envelopeVersion ladder, consts, recommendJSON root tag), `cmd/robne/config.go:30` (knownPlugins, YAML keys), `cmd/robne/plugins.go`, `cmd/robne/recommend.go`, `cmd/robne/explain.go`, `cmd/robne/diff.go` (slice label), `cmd/robne/version.go` (`envelopeCapability`/`jsonEnvelopeMax` live here, not output.go), `cmd/robne/robne.yaml.sample`, all tests/testdata under `cmd/robne/`
- Modify: `docs/plans/robne-cli-spec.md`, `cmd/robne/README.md`, public robne docs (spec §, examples, migration note)

**Interfaces:**
- Produces: `recommendJSONVersionWithHCPCapacity = 12`; `knownPlugins`/`stdoutEntityPlugins`/`enabledPlugins`/`allShippedPlugins` contain `hcp` and `cluster-quota` (not `cluster_quota`); JSON root key `container_recommendations`; sibling keys unchanged (`cluster_quota_recommendations` stays); `writeRecs` dispatch NOT yet gained `hcp` (Task 5). **`pruneMissingFilePlugins` gains `case "hcp"` (rows-present check on `len(loaded.HCPCapacityRows) > 0`) in THIS task** (round-9 B1: `allShippedPlugins()` is the default resolved set and the prune `default:` arm appends unknown plugins — without the case here, every default run between Task 3 and Task 4 emits v12 with no capacity sibling, breaking the existing v≤11 goldens and the spec's v12-when-sibling-present rule). Intermediate commits keep default runs at v≤11; the `requireExplicitFilePlugins` case lands in Task 4 with the compute path.

- [ ] **Step 1: Failing envelope tests** — `TestEnvelopeVersion_BHAndCapacity` (plugins hcp+bh → 12), `TestEnvelopeVersion_PlainBHUnchanged` (bh+node → 11; bh only → 10 — pins the fall-through equivalence), `TestEnvelopeVersion_CapacityOnly` (hcp → 12), `TestEnvelopeVersion_CapacityWithRowsPruned` (auto run, no capacity rows: hcp pruned → v stays ≤11). Round-9 W5: there is no existing v1–v11 combination TABLE — version assertions are scattered `"version":N` checks across `output_test.go`, `recommend_test.go` (incl. `TestRecommend_BusinessHoursDefaultAllJSONVersion11`), and `version_test.go`; extend those in place (the BH default-JSON-v11 test must stay green through Task 3) or consolidate them into one new table.
- [ ] **Step 2: Verify red; restructure `envelopeVersion`** into the fall-through ladder (no early returns): entity ladder → BH block assigns v10/v11 → capacity assigns v12 last (design round-8 C1 option 2). Update `envelopeCapability`, `jsonEnvelopeMax`, `version` Long text.
- [ ] **Step 3: Registry + renames** — add `hcp` to the four plugin maps; rename `cluster_quota`→`cluster-quota` across `knownPlugins`, `stdoutEntityPlugins`, `envelopeCapability`, plugins.go cases, output.go error strings, explain.go help + identity key, recommend.go validate/Path A, version.go table + Long, config.go YAML keys (`allowedYAMLKeys`, `reservedYAMLKeys`), `robne.yaml.sample`, all tests/testdata (grep-verify: `grep -rn 'cluster_quota' cmd/robne/ | grep -v _test.go` returns only `cluster_quota_recommendations` JSON-tag hits). Root key rename `recommendations`→`container_recommendations` in output.go tag + diff.go slice label + every golden. Round-9 N6 — two easily-missed sites: `fileConfig.ClusterQuota`'s yaml tag `yaml:"cluster_quota"` (config.go — renaming `allowedYAMLKeys` without the tag silently drops the block) and `rejectRecommendEnvelopeInput`'s probe tag `json:"recommendations"` (explain.go, exercised by `TestRunExplain_RejectsRecommendEnvelope`).
- [ ] **Step 4: Verify** — `go test ./cmd/robne/ -count=1` (all goldens rewritten and green); `grep -rn '"recommendations"' cmd/robne/` empty; docs updated with the one-line migration note.
- [ ] **Step 5: Commit** `feat(robne): envelope v12 ladder, hcp plugin registry, pre-release renames (#700)`.

### Task 4: Capacity compute path (`recommend`)

**Files:**
- Create: `cmd/robne/recommend_hcp_capacity.go`, `cmd/robne/recommend_hcp_capacity_test.go`, `cmd/robne/testdata/hcp-capacity/` (fixtures below)
- Modify: `cmd/robne/recommend.go` (recommendResult field, compute wiring, Path A rejection), `cmd/robne/ratecard.go:106` (resolveClusterIDFromLoad manifest slot), `cmd/robne/output.go` (sibling serialization)

**Interfaces:**
- Consumes: Task 1 (`LoadResult.HCPCapacityRows`, `HCPCapacitySkewCheck`, `RowSkip`), Task 2 (`ServeObservation`, rule functions, `MCEMayBeStaleFromSeries`), Task 3 (registry).
- Produces:
  - `func resolveCapacityNow(flag string, cfg fileConfig) time.Time` — `--now` > YAML `now` > `time.Now().UTC()`; never calls `parseNow`.
  - `func assembleCapacitySeries(rows []csv.HCPCapacityRow, now time.Time) (series []csv.HCPCapacityRow, skewDrops []csv.RowSkip)` — skew-check → stable sort newest-first by `ObservedAt` → dedup by `observed_at` keeping the LAST of the tied group in read order.
  - `func capacityRowToServe(row csv.HCPCapacityRow, res hcp.CapacityResult) hcp.ServeObservation`
  - `type HCPCapacityOut struct` — CLI DTO mirroring the service row shape (`cluster_id, observed_at, native_model_version, source_versions, cadences, sample_age_seconds, flags, counts, qps, basis, pressure, eligible_workers, scenarios [name/max_hcs/limiting_resource/headroom/status], mce_references, warnings`); field naming consistency with existing siblings is the builder's call per the spec.
  - `recommendResult.HCPCapacity []HCPCapacityOut`.

**Fixtures (plan attachments — review-2 W5; real hcp-mgmt rows 2026-10-26, identity-free by contract):**

`testdata/hcp-capacity/ros-openshift-hcp-capacity-202610.csv`:
```csv
observed_at,mce_sampled_at,operator_version,ocp_version,mce_version,native_model_version,collection_interval_minutes,upload_cycle_minutes,inventory_complete,current_hc_count,ready_hcp_count,average_ready_hcp_qps,ready_hcp_qps_known,hosted_availability_basis,eligible_worker_cpu_allocatable_millicores,eligible_worker_memory_allocatable_bytes,eligible_worker_max_pods,worker_placement_known,pressure_state,mce_request_max_hcs,mce_low_qps_max_hcs,mce_average_qps_max_hcs,mce_medium_qps_max_hcs,mce_high_qps_max_hcs,shared_infrastructure
2026-10-26T21:59:59Z,2026-10-26T21:59:59Z,4726441c2accc574778840f1b9d09ee0bd5c6f64,4.22.17,,hcp-published-sizing-v1,60,60,true,3,3,19.800645154088865,true,single_replica,46500,147688947712,750,true,unknown,8,8,8,4,2,true
2026-10-26T20:59:59Z,2026-10-26T20:59:59Z,4726441c2accc574778840f1b9d09ee0bd5c6f64,4.22.17,,hcp-published-sizing-v1,60,60,true,3,3,19.5189589937284,true,single_replica,46500,147688947712,750,true,unknown,8,8,8,4,2,true
2026-10-26T19:59:59Z,2026-10-26T19:59:59Z,4726441c2accc574778840f1b9d09ee0bd5c6f64,4.22.17,,hcp-published-sizing-v1,60,60,true,3,3,19.481572567239073,true,single_replica,46500,147688947712,750,true,unknown,8,8,8,4,2,true
```
`testdata/hcp-capacity/manifest.json`: `{"cluster_id":"11111111-1111-1111-1111-111111111111","cr_status":{"topology":{"controlPlaneTopology":"HighlyAvailable","hostedClusterCount":3}}}`
Recollect pair (separate dir `testdata/hcp-capacity-recollect/`): the 21:59 row twice — second occurrence with `eligible_worker_cpu_allocatable_millicores` = `55500` — expected series keeps 55500.
Expected pinned values (hand-verified against `CalculateCapacity` and the live SNO API): scenarios request/low/average = 7 `memory_request`, medium = 3 `cpu_usage`, high = 2 `cpu_usage`; headroom +4/+4/+4/0/−1; status all `illustrative_only`; warnings: `pressure_unknown`, `shared_infrastructure`, `mce_may_be_stale` (the in-window prior 20:59 row carries QPS 19.5190 vs series[0]'s 19.8006 while MCE refs stay frozen at 8/8/8/4/2 — window is 2h relative to series[0], so the 19:59 row at the exact boundary is irrelevant either way; round-9 N1) with `--now 2026-10-26T22:30:00Z`.

- [ ] **Step 1: Failing series tests** — `TestAssembleCapacitySeries_RecollectLastWins`, `TestAssembleCapacitySeries_MonthBoundaryMerge` (two files, Sep row older → one newest-first series), `TestAssembleCapacitySeries_SortOrder` (unsorted input rows → newest-first).
- [ ] **Step 2: Verify red; implement `assembleCapacitySeries`; verify green.**
- [ ] **Step 3: Failing compute tests** — `TestRecommend_HCPCapacityParity` (full run on `testdata/hcp-capacity/` with `--now 2026-10-26T22:30:00Z --format json`: pins the scenario/warning values above, sibling `hcp_recommendations` full 3-row series newest-first, envelope v12, cluster label `11111111-…`); `TestRecommend_HCPCapacityClock` (no `--now`, capacity-only input: succeeds — `parseNow` never invoked); `TestRecommend_HCPCapacityUnknownCluster` (no manifest, no YAML: label `unknown`, envelope cluster_id empty).
- [ ] **Step 4: Verify red; implement** — `recommend_hcp_capacity.go` (clock, series, mapper, DTO). Wiring: `requireExplicitFilePlugins` gains the `hcp` case (zero-rows-loaded → the missing-snapshot message) in THIS task (round-9 W6: without it, Task 4's commit leaves explicit `hcp` on a container-only payload silently emitting v12 with no sibling); the compute function returns the all-skew-dropped diagnosis error when hcp is explicit AND series empty AND `len(skewDrops) > 0` (the mechanism lives here, not in `requireExplicitFilePlugins`, which runs pre-compute via `applyFilePlugins` — round-9 B1/W6); `loadFiles` skips `parseNow` when no digest-backed entity is loaded, with `out.now` = `resolveCapacityNow` result (round-9 W8: `recommend.go:728` calls `parseNow` unconditionally and capacity-only input has no `interval_end` — without this, capacity-only runs hard-error); a bad YAML `now` is a hard error, same as `parseNow` (round-9 N11); `Load` warnings print to stderr right after Load in recommend/validate/explain (round-9 W4 — spec-locked, previously unimplemented); extend `resolveClusterIDFromLoad` (manifest fills the csvID slot when rows carry none; YAML-override and disagreement semantics unchanged, disagreement error reworded to say manifest — round-9 N3, current message says "CSV cluster_id"); capacity rows carry no cluster id by contract, so the manifest-vs-row tripwire is vacuous for capacity — the existing container-rows hard error is untouched (round-9 N2); Path A: explicit `hcp` → friendly error, auto → `dropPlugin` (supersedes the spec's `pruneEmptyPathAPlugins` wording — round-9 N7, snapshot precedent); output.go serializes the sibling.
- [ ] **Step 5: Verify green; `make lint && make test-short`; commit** `feat(robne): HCP capacity compute path and hcp_recommendations sibling (#700)`.

### Task 5: Table/CSV rendering + wrong-payload UX

**Files:**
- Modify: `cmd/robne/output.go` (`writeHCPCapacityCSV`, `writeHCPCapacityTable`, `writeRecs` cases, one-entity error list), `cmd/robne/plugins.go` (`requireExplicitFilePlugins` hcp case)
- Test: `cmd/robne/recommend_hcp_capacity_test.go`

**Interfaces:**
- Consumes: Task 4's `recommendResult.HCPCapacity` (latest = `series[0]`).
- Produces: table/CSV render the LATEST observation's five scenarios as rows; column sets are the builder's call per the spec.

- [ ] **Step 1: Failing tests** — `TestWriteRecs_HCPCapacityTable` (latest only — assert the 21:59 row's five scenario lines, not the 19:59/20:59 ones); `TestWriteRecs_HCPCapacityCSV` (header + five rows); `TestRequireExplicitPlugins_MissingCapacity` (`--plugins hcp` on a container-only payload → nonzero exit, message contains "capacity snapshots are emitted by HyperShift management clusters"); `TestCompute_HCPCapacityAllSkewDroppedDiagnosis` (all rows future-dated → nonzero exit, message contains "future-dated" and "clock skew", NOT "no capacity snapshot"); `TestRecommend_CapacityOnlyPoisonedExplicit` (capacity-only poisoned CSV + explicit `hcp` → warnings printed to stderr, then the friendly nonzero exit — the fourth poison-matrix cell, round-9 W4). **Mechanism (round-9 fix):** `requireExplicitFilePlugins` runs at plugin-resolution time via `applyFilePlugins(plugins, explicit, loaded)` and sees only `LoadResult` — it cannot observe compute-time skew drops. It keeps its existing role (zero-rows-loaded → the missing-snapshot message). The skew diagnosis lives in the Task-4 compute path: `recommend_hcp_capacity.go` returns the error when hcp is explicit AND the assembled series is empty AND `len(skewDrops) > 0` (rows were loaded; compute rejected them all).
- [ ] **Step 2: Verify red; implement; verify green.** `make lint && make test-short`; commit `feat(robne): HCP capacity table/CSV rendering and wrong-payload diagnosis (#700)`.

### Task 6: `validate` / `diff` / `explain`

**Files:**
- Modify: `cmd/robne/recommend.go` (validate section), `cmd/robne/diff.go`, `cmd/robne/explain.go`
- Test: `cmd/robne/recommend_hcp_capacity_test.go`, `cmd/robne/diff_test.go`, `cmd/robne/explain_test.go`

**Interfaces:**
- Consumes: Task 4 DTO + series; Task 1 `RowSkip`.
- Produces: `recommendJSON.HCPRecommendations *[]HCPCapacityOut` (this task owns the envelope struct field; Task 4 owned the sibling serialization — round-9 N9) + `diffSlice("hcp_recommendations", left.Latest, right.Latest, hcpCapacityKey)` over the one-row latest slices keyed by the `observed_at` string; explain flags `--scenario` (required for hcp; one of the five names) and `--observed-at` (optional RFC3339, default latest).

- [ ] **Step 1: Failing validate tests** — `TestValidate_HCPCapacity` (valid fixture: `hcp_capacity_rows: 3` line; a fixture with one bad row: `skipped row <observed_at|#n>: <reason>` line; pg input rejected as today).
- [ ] **Step 2: Failing diff tests** — `TestDiff_HCPCapacity` (two envelopes whose latest rows differ in `eligible_worker_cpu…` → scenario maxima deltas reported; **latest rows with DIFFERENT `observed_at` — the normal month-over-month case — must still compare latest-vs-latest, not report added/removed rows**: round-9 W3, pre-select `series[0]` from each side into one-row slices before `diffSlice`, per the spec's locked latest-per-side semantics; missing-side → per existing diff contract); `TestDiff_EnvelopeVersionMismatch` (v11 vs v12 exits 2 — existing rule, now pinned for capacity).
- [ ] **Step 3: Failing explain tests** — `TestExplain_HCPCapacity` (`--plugins hcp --scenario medium_qps`: source (eligible worker capacity, occupancy 3, QPS basis single_replica), assumption (published-sizing v1 baselines), limiting resource `cpu_usage`); `TestExplain_HCPCapacitySelectors` (`--observed-at 2026-10-26T20:59:59Z` selects that row; bad scenario name → error; `--schedule business_hours` rejected for hcp).
- [ ] **Step 4: Verify red; implement; verify green.** `make lint && make test-short`; commit `feat(robne): validate/diff/explain for HCP capacity (#700)`.

### Task 7: Spec, docs, final checks

**Files:**
- Modify: `docs/plans/robne-cli-spec.md` (envelope § v12 + `hcp_recommendations`; §472 mixing list +`hcp`; BH § v12-when-BH+hcp; new HCP-capacity subsection: contract, plugin, sibling shape, clock rule, selectors; migration note for the two renames), `cmd/robne/README.md`, `docs-site` public robne docs (offline-trust note incl. consequence per round-8 N3)

- [ ] **Step 1: Update docs per the design's review-6 T8 checklist.**
- [ ] **Step 2: Final checks** — `make lint`, `make test-short`, `make docs-lint docs-drift docs-sync-check`, `git diff --check`; grep-verify no rename stragglers.
- [ ] **Step 3: Commit** `docs(robne): HCP capacity spec and public docs (#700)`.

---

## Fixture provenance

The three 2026-10-26 rows are real hcp-mgmt observations extracted from the SNO's `costonprem_ros.hcp_capacity_history` (query recorded in the session ledger), carrying no cluster identity by CSV contract; the manifest fixture supplies a synthetic UUID. The recollect pair is synthetic (same `observed_at`, changed `eligible_worker_cpu_allocatable_millicores` 46500→55500) per design round-8 W1. Expected values hand-verified against `CalculateCapacity` and cross-checked against the live API response captured 2026-10-26.
