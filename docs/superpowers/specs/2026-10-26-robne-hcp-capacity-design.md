# robne CLI HCP fleet-capacity support — design

Date: 2026-10-26
Issue: [#700](https://github.com/pgarciaq/ros-ocp-backend/issues/700) (child of #392; not #701, which is RetentionProvider)
Status: design for owner review (review rounds 2–7 applied 2026-10-26)

## Intent and why

The W4 fleet-capacity advisory (#392) is live end-to-end: the operator
(koku-metrics-operator) emits a bounded hourly snapshot CSV, the ROS backend
ingests it, computes the five published-sizing scenarios via
`librobne/hcp.CalculateCapacity`, and serves them through the API and UI.
#700 gives operators and support engineers the same advisory **offline**
with `robne`, from the same shared math and the same CSV contract — no
cluster access required.

The canonical workflows (owner-stated):

1. **Disconnected mode:** KMO with `upload_toggle: false` accumulates
   payload tarballs on its PVC; someone copies a tarball (or the reports
   directory) to a machine with the robne CLI.
2. **Fake data:** nise generates a payload fed to robne (nise-side
   generator tracked in #702, not this design).

A possible future workflow — robne querying Prometheus directly, skipping
the CSV step — is explicitly not supported today; this design keeps
"gather" (CSV) separable from "compute" so a future Prometheus gatherer
can feed the same `CapacityInputs`.

## Goals

- `robne recommend` on a management-cluster payload produces the
  fleet-capacity advisory as a new JSON sibling, with CLI table/CSV
  rendering, matching the shared `librobne/hcp` fixture values
  scenario-by-scenario.
- `robne validate`, `robne diff`, and `robne explain` understand the
  capacity entity.
- One shared CSV parser and one shared math library across backend
  ingestion and CLI — no duplicated validation rules or formulas.
- CLI plugin naming aligned with the backend service: canonical names are
  backend **plugin** names (`hcp`, `cluster-quota`), single spelling, no
  aliases (review-2: owner-directed; kills the `cluster_quota`
  divergence by renaming, not aliasing).

## Non-goals

- No second capacity formula: the CLI calls `librobne/hcp` (issue
  non-goal, hard).
- No multi-payload batching (process N packages in one run). Both W4
  capacity and HCP recommendations are computable from the **management
  cluster payload alone** — verified: all 25 CSV columns are
  management-side, HCP container rows and topology ride the same payload,
  KMO emission gating (C1) means hosted-cluster payloads never contain a
  capacity CSV (`collectHCPCapacityHourly` returns nil when the
  HyperShift APIs are absent), and the live SNO deployment produces the
  advisory from hcp-mgmt's payload only. Hosted-cluster payloads serve
  their own workloads' recommendations. Batching can be filed later if a
  real need appears.
- No `postgres://` recompute for capacity in this issue: the pg path
  recomputes from stored digests; capacity's source-of-truth inputs are
  not digests. `recommend` Path A rejects the capacity plugin as it does
  `snapshot` (explicit → friendly error, auto → dropped); `validate`
  already rejects pg inputs globally (review-2 N2 correction: the gap
  was never in `validate`).
- No nise generator (open as #702; no new issue needed — review-2 W5),
  no backend/API/UI changes beyond the locked shared-logic moves below,
  no Local Mode operator work, no cross-tenant M3 routing (issue
  non-goals).
- No back-compat for the pre-release renames in this issue's breaking
  window (owner-stated): `cluster_quota` → `cluster-quota` (plugin
  name strings) and the envelope root key `recommendations` →
  `container_recommendations` (review-5), each with its
  tests/testdata. robne has no releases and no external JSON
  consumers (verified review-5: no fork releases, no robne usage in
  the IQE fork, cheatsheet, or koku-ui), so both rename in place with
  no version bump. The JSON sibling key stays
  `cluster_quota_recommendations` (review-3: stable snake_case keys —
  a tag rename would turn working jq pipelines into silent-null
  pipelines; breakage stays at the flag layer where failures are
  loud). Go identifiers (`ClusterQuotaRow`, `KindClusterQuota`,
  `ParseClusterQuotaRows`) and DB/digest names are unchanged.

## Anti-goals

- Per-HC rows carrying fleet headroom (W4 rule: fleet numbers live in the
  fleet entity only).
- Absent MCE references rendered as zero — absence stays absence.
- Affirmative pressure advertising exact headroom — withheld, as in the
  service.

## Locked decisions (owner, 2026-10-26; review rounds marked inline)

| Decision | Value |
|---|---|
| Plugin name (canonical, review-2) | `hcp` — the backend **plugin** name (`HCPPlugin.Name()`; that plugin claims the `hcp-capacity` CSV type and serves the fleet-capacity routes). Single token: there is no hyphen/underscore variant to diverge. CLI `hcp` selects the capacity advisory only; HCP-namespace container rows keep riding under `container` + manifest guardrails |
| `cluster_quota` rename (review-2, amended review-3) | Renamed to `cluster-quota` everywhere a plugin name appears: `knownPlugins`, `stdoutEntityPlugins`, `envelopeCapability`, error/help strings, explain identity key, spec + public docs, tests/testdata. No alias, no back-compat. The JSON sibling key and diff sibling key stay `cluster_quota_recommendations` (stable snake_case rule, next row). Go identifiers and DB names stay |
| JSON sibling | `hcp_recommendations` (review-4: follows the house `*_recommendations` pattern — all 16 existing sibling keys share the suffix, so no new exception is created) |
| Root key rename (review-5) | `recommendations` → `container_recommendations` — the last non-mirroring key goes (cluster-quota spelling stays per review-3). Pre-release break, rides #700's window; no version bump, goldens rewritten, one-line migration note in spec + public docs |
| JSON key stability (review-3, amended review-5) | After #700's window, sibling keys are stable snake_case and never renamed for spelling — the `*_recommendations` suffix stays (type marker, plurality carrier, BH disambiguation). Post-#700 mapping is mechanical `<entity>_recommendations` with one documented exception: `cluster-quota` → `cluster_quota_recommendations`. The plugin↔key mapping is documented in the spec, not derived by rule |
| JSON envelope | Version **12** (`recommendJSONVersionWithHCPCapacity`), additive sibling `hcp_recommendations` — prior-version consumers ignore unknown siblings (established pattern: v2 namespace … v11 BH-plugins). **v12 applies whenever the capacity sibling is present, independent of business hours** (builder-check #3: today's BH early-return pins the envelope at 10/11; capacity rides above it so the ladder stays monotonic — BH+capacity emits v12 carrying both siblings) |
| Capacity clock (review-2, C1) | `--now` flag, else YAML `now`, else **wall clock** (`time.Now().UTC()`). Never max `interval_end` (capacity rows have none — `parseNow` hard-errors on capacity-only input) and never max `observed_at` (would pin the latest sample age to zero, so nothing is ever stale). Wall clock matches the service; goldens pin `--now` for reproducibility |
| Output granularity | **Full observation series** in the JSON sibling (the monthly CSV accumulates hourly rows — KMO `ros-openshift-hcp-capacity-YYYYMM.csv`, idempotent per-window upsert); table/CSV output shows the latest observation only (review-2 N4: no series flag; JSON carries the series) |
| `diff` semantics | Compare the **latest observation** on each side: scenario maxima, limiting resource, headroom changes, plus input changes (worker capacity, HC count, QPS, basis, pressure). Row key is `observed_at`. Cluster mismatch is a meta delta under the existing contract (`writeMeta("cluster_id", …)`), not a new error (review-2 W4). Same-version envelopes required (existing hard error — a v11-vs-v12 diff exits 2) |
| `explain` selectors (review-2, N1) | `--scenario` (required, one of the five scenario names) + `--observed-at` (optional RFC3339, default latest). All other identity flags rejected via the existing `explainIdentitySets` machinery. `--schedule business_hours` rejected for `hcp` (joins the pvc/quota/cluster-quota/snapshot list) |
| Single cluster per input (review-2, W2; amended review-6) | robne's existing phase-1 rule stands: every file in one directory/tarball belongs to one cluster. All capacity CSVs in the input (KMO multi-file reports; month-boundary pairs; overlapping payloads merged in one dir) merge into one series attributed to the manifest cluster: files processed in sorted filename order, stable sort newest-first, dedup by `observed_at` keeping the LAST occurrence in read order — matches backend last-write-wins convergence. Duplicates arise only from overlapping inputs or KMO recollect-with-changed-values (KMO dedups by full row string, so a changed recollect appends) |
| Manifest identity + cross-check (review-2, W6; precedence review-7) | Sibling cluster label = manifest `cluster_id`, else YAML `cluster_uuid`, else `"unknown"` — but YAML overrides when both are set, and manifest-vs-YAML disagreement is the existing hard error (reworded to say manifest). Manifest-vs-row mismatch stays a stderr warning (misattribution tripwire for hand-copied payloads) |
| Warn-and-skip plumbing (review-2, W1) | `LoadResult` gains `Warnings []LoadWarning{File, Kind, Reason}`; capacity file-level poison (header mismatch, read error, all-rows-unparseable) becomes a warning, never an error. `mergePart` appends; `finishLoad` succeeds on rows OR capacity warnings; recommend/validate/explain print warnings to stderr right after `Load` (explain inherits this via `executeRecommend`; diff never Loads — it compares envelopes whose row-level warnings are already baked in). Row-level skips stay in `RowsSkipped`; compute-time skew drops increment `SkippedRows` (documented: `Load` has no clock) |
| Serve-time sharing seam (review-2, C3) | New `librobne/hcp/serve.go` with a DB-free `ServeObservation` view + rule functions (freshness window, below-floor, status, headroom nil rules incl. pressure, warnings, `MCEMayBeStaleFromSeries`). Shared = rules, not bytes: the API handler and the CLI keep their own JSON DTOs and map into the shared view via thin mappers. Characterization goldens on current handler output land BEFORE the refactor; the refactor keeps them byte-identical |
| Backend scope reread (review-2, C3) | Locked: #700's "no backend changes" means **no new backend surfaces**; parser + derivation moves into `librobne` are allowed as behavior-preserving refactors with goldens green. This does not block #392 (W4 is live; the refactor lands after, guarded by the goldens) |
| Data parity CLI-vs-service (review-2, W3) | Same accumulated monthly CSV + same math + same frozen-reference rule ⇒ same scenarios. Residual divergences only: (1) the clock (wall vs `--now`) for age/staleness/skew; (2) month-boundary truncation (service retains 90 days, payload carries its month(s)) |
| Offline trust (review-2, N7) | Hand-copied CSVs are unauthenticated input — no integrity check exists (vs the backend's authenticated upload path). Stated in constraints and public docs, not mitigated further |
| nise | Follow-up #702 (already open), out of scope here; no new issue (review-2 W5) |
| Multi-payload | Non-goal (see above); wrong-payload UX defined below |
| Wrong-payload, explicit plugin (builder-check #5) | `--plugins hcp` on a payload without a capacity CSV → **nonzero exit with the friendly message** (consistent with `requireExplicitFilePlugins` for every other entity — script-safe); auto-detection stays silent; the sibling is omitted, not emitted empty |

## The how

### 1. Parser relocation (the load-bearing refactor)

The 25-column parser (strict header, future-skew tolerance, model-version
match, cadence bounds, strict booleans, skip-with-counters) currently
lives in `internal/ingestion/hcp_capacity.go`. Move it to
`librobne/csv` as `ForEachHCPCapacity` + `ParseHCPCapacityRows` beside the
SLO-parser precedent; the backend ingestion imports it from there and its
tests stay green unchanged (they already pin the validation semantics).
`ClassifyFilename` gains the `ros-openshift-hcp-capacity-` prefix →
`KindHCPCapacity` (backend `DetermineCSVType` parity — the prefix strings
must match exactly; verified against KMO
`rosHCPCapacityFilePrefix` + `TestHCPCapacityRow_Contract`, same 25
columns in the same order).
Fix the stale "24-column"/"24-field" strings to 25 in the move
(review-2 N6; KMO already says contract position 25).

The parsed row struct carries inputs only — scenarios are computed at
read time by every consumer. `vendor/` resync is part of any librobne
change (root module vendors librobne via the `replace` directive).

### 2. Compute and serve-time derivations

**Shared, not duplicated (builder-check #1 + review-2 C3):** the
serve-time derivations (freshness window, below-floor,
`mce_may_be_stale`, scenario status/headroom nil rules,
pressure-withholding) move from `internal/api/handlers_hcp_capacity.go`
into `librobne/hcp/serve.go`, and the API handler is refactored to call
them — one implementation, both consumers, API behavior unchanged. The
pressure-withholding and average-QPS headroom rules are stricter than
`CalculateCapacity`'s own — they live in the shared derivation helper,
not re-derived. Concrete seam:

- `type ServeObservation struct` — DB-free view: observed/sample times,
  four versions, both cadences, inventory/placement/QPS-known flags,
  current/ready counts, `AverageReadyHCPQPS *float64` (nil when
  unknown), availability basis, pressure state, shared-infra flag,
  `Eligible WorkerCapacity`, five MCE refs in scenario order,
  scenarios (`MaxHCs` + `LimitingResource`).
- Rule functions: `FreshnessWindow(coll, upload int64)`,
  `BelowVersionFloor(ocp string)`, `ScenarioStatus(basis string)`,
  `DeriveHeadroom(...) *int64` (nil rules incl. pressure),
  `DeriveWarnings(...)`, `MCEMayBeStaleFromSeries([]MCEStalePoint) bool`
  (MCE refs + native inputs + observed_at per point; `len < 2` → false,
  same as today).
- Mappers (trivial field copies, unit-tested): `model.HCPCapacitySnapshot
  → ServeObservation` in `internal/api`; `csv.HCPCapacityRow +
  CapacityResult → ServeObservation` in `cmd/robne` (new
  `recommend_hcp.go`). API and CLI keep their own JSON DTOs.
- Order of work: characterization goldens on current handler output
  first (pressured / unknown-pressure / stale / below-floor /
  frozen-MCE / incomplete-inventory cases), then the move, goldens
  byte-identical.

Per observation, call `librobne/hcp.CalculateCapacity`; derive at output
time with the capacity clock (locked above): sample age, staleness (2×
the row's own max cadence), below-floor warning, and the
`mce_may_be_stale` caution via the shared series helper over the
in-payload series (same rule as the backend: MCE refs frozen across the
freshness window while native inputs moved). Series assembly (review-6):
gather rows across files in sorted filename order → skew-check →
stable sort newest-first → dedup by `observed_at`, keeping the LAST
occurrence in read order. Latest = series[0], independent of `--now`
(age surfaces via the staleness warning); table/CSV rendering, the
explain default, and diff all consume it. This mirrors the service's
serve-time shape (`warnings`, `mce_references`, scenario
`status`/`headroom` nil rules, pressure withholding).

**Capacity-only runs never call `parseNow`** (review-2 C1): the engine
clock resolves only when digest-backed entities are present.
`resolveCapacityNow` (`--now` > YAML `now` > wall) is the single clock
for age/staleness/skew/mce-window. Tests: capacity-only input without
`--now` succeeds; `--now` goldens pin derivations.

**Time-dependent vs time-independent parsing (builder-check #2a):** the
shared parser separates time-independent row validation (header, types,
enums, cadence bounds — applied in `csv.Load`, which has no clock) from
the future-skew check (a separate function taking `now`, applied by the
CLI at compute time with the capacity clock and by backend ingestion
with wall clock). `Load`'s signature is unchanged (the struct gains
fields; the parser move adds a new exported API); the skew rule stays
single-sourced. Skew drops at compute increment `SkippedRows`.

**Load wiring:** `LoadResult` grows `HCPCapacityRows` (SLO/snapshot
eager-parse precedent) plus `Warnings` (locked above); `hasROS()` counts
capacity rows so a capacity-only input works standalone, and
`finishLoad` succeeds on rows OR capacity warnings (new
`hasCapacityAttempt` helper — gated to capacity only, no behavior
change for other entities). **Poisoned capacity CSV (builder-check #2b
+ review-2 W1):** the `parseCSVReader` capacity case converts
file-level poison into `LoadWarning` entries and returns nil error —
mirroring the backend's load-bearing W4 dispatch contract (a poisoned
capacity file must not gate container recommendations). This
deliberately diverges from the CLI's `failLoadOnMissingColumns=true`
precedent for other kinds (`KindHCPCapacity` stays out of that set);
the service contract wins. Matrix: mixed + poisoned → warn on stderr,
continue, sibling omitted; capacity-only poisoned + explicit `hcp` →
warnings printed, then the friendly nonzero-exit message;
capacity-only poisoned + auto → warnings printed, then "no ROS input"
error. All three cells tested.

### 3. CLI wiring

- `stdoutEntityPlugins` + `resolvedPlugins` + `knownPlugins` gain `hcp`
  (auto-detected by CSV presence, explicit via `--plugins hcp`).
  `pruneMissingFilePlugins` / `requireExplicitFilePlugins` /
  `pruneEmptyPathAPlugins` gain the `hcp` case (rows-present check).
  `enabledPlugins` and `allShippedPlugins()` gain `hcp` too —
  `validatePlugins` rejects anything outside both maps, explain checks
  only the former, and the latter is the default set, so all three
  must move together. `recommendResult` gains `HCPCapacity
  []HCPCapacityOut` (CLI-owned DTO) — the single source all writers,
  diff, and explain read.
- Table/CSV dispatch (review-6 T1): `case "hcp"` in both `writeRecs`
  switches calling new `writeHCPCapacityCSV` / `writeHCPCapacityTable`
  in `output.go`; append `hcp` to the one-entity error-string plugin
  list. Both render the latest observation's five scenarios as rows.
- `cluster_quota` → `cluster-quota` rename in the same change:
  `knownPlugins` key, `stdoutEntityPlugins`, `envelopeCapability`,
  `plugins.go` cases, `output.go` error string, `explain.go` help +
  identity key, `recommend.go` validate/Path A references,
  `version.go` table + Long text, `robne-cli-spec.md`,
  `cmd/robne/README.md`, public robne docs, all tests/testdata,
  `robne.yaml.sample`. YAML surface (`allowedYAMLKeys`,
  `reservedYAMLKeys`) renames the key too — probing hard-errors on
  unknown keys, so old files fail loud. No alias (Review-2:
  owner-stated, no back-compat). Review-3: the JSON
  tag and diff sibling key stay `cluster_quota_recommendations` —
  existing jq pipelines keep working unquoted; only the flag spelling
  changes, which fails loud.)
- Root key rename in the same breaking window (review-5):
  `recommendations` → `container_recommendations` in the
  `recommendJSON` tag, `diff.go` slice label + key-func wiring, all
  goldens/testdata (`golden_envelope_v1.json`,
  `golden_short_cost.json`, …), `robne-cli-spec.md` (incl. the
  "`recommendations` stays container-only" sentence),
  `cmd/robne/README.md`, public robne docs and examples, and any
  `docs-site/testing` recipes. Grep-verify no stragglers; re-check
  the IQE fork for robne usage at build time; one-line
  pre-release-break migration note in spec + public docs.
- `robne recommend --format json` emits envelope v12 with the
  `hcp_recommendations` sibling (full series, newest-first);
  `envelopeVersion` gains the capacity check AFTER the BH override
  (review-2 N5: also update `envelopeCapability`, `jsonEnvelopeMax`,
  `version` Long text, and the version/output/diff goldens).
  Table/CSV formats render the latest observation; the existing
  one-entity-per-stream rule stands, so a mgmt payload (container +
  capacity rows) needs `--plugins hcp` for `--format table|csv`
  (review-2 N4 — documented, no new flag).
- Path A (`--input postgres://`): explicit `hcp` → friendly error
  ("hcp has no digest table; …", snapshot precedent, firing before
  any clock resolution); auto → dropped via `dropPlugin`
  (review-2 N2).
- Cluster identity (review-6 T4): extend `resolveClusterIDFromLoad` —
  the manifest fills the csvID slot when rows carry no ids, then the
  existing `resolveClusterIDs` decides (YAML `cluster_uuid`
  overrides; manifest-vs-YAML disagreement → the same hard error,
  reworded to say manifest). Manifest-vs-row mismatch stays a stderr
  warning (locked above, review-2 W6). No manifest and no YAML →
  `"unknown"` sibling label.
- Wrong-payload UX: a payload with no capacity CSV and `--plugins hcp`
  explicit → clear informational message ("no capacity snapshot in this
  payload; capacity snapshots are emitted by HyperShift management
  clusters"); auto-detection stays silent (a hosted-cluster payload
  simply has no capacity rows).

### 4. `validate` / `diff` / `explain`

- **validate:** the shared parser's validation rules are the contract.
  The parser API is extended to return per-row skip **reasons**
  (builder-check #4: "pure relocation" was overstated — it is a
  relocation plus a reason-returning API extension; backend ingestion
  keeps its count-based behavior, ignoring the reasons, so its tests and
  metrics are unchanged). Validate reports the series parse plus skipped
  rows with reasons, prints `Load` warnings, and notes that skew is
  evaluated at compute with the capacity clock (review-6 T6): print
  `hcp_capacity_rows: <n>` following the `namespace_rows:` pattern,
  plus `skipped row <observed_at|#n>: <reason>` lines (observed_at
  when field 0 parses, else row index). pg inputs rejected
  globally as today (no capacity-specific work).
- **diff:** latest-per-side comparison (locked above); row key
  `observed_at`; exit code and output shape follow the existing diff
  contract. Cluster mismatch rides the existing `cluster_id` meta delta
  (review-2 W4: no new error — document that cross-cluster capacity
  diffs are allowed and flagged). Same-version requirement stated in
  help/docs (v11-vs-v12 exits 2). Implementation (review-6 T2):
  `recommendJSON.HCPRecommendations *[]hcpCapacityOut` plus
  `diffSlice("hcp_recommendations", left.HCPRecommendations,
  right.HCPRecommendations, hcpCapacityKey)` keyed by the
  `observed_at` string; in-sibling duplicates hit the existing
  duplicate-key hard error.
- **explain:** `explain --plugins hcp --scenario <name>
  [--observed-at <RFC3339>]` (locked above, review-2 N1); new
  `--scenario` + `--observed-at` flags wired through
  `explainIdentitySets`/`explainFlagValue`. For the scenario, explain
  the source (snapshot inputs: eligible worker capacity, occupancy,
  QPS basis), the sizing assumption (published-sizing baselines via
  the model version), and the limiting resource; render occupancy
  completeness and pressure state. Help `Long` text gains the
  `hcp: --scenario --observed-at` identity line (review-6 T5).

### 5. Parity fixtures and docs

- The hand-verified Task 2 fixture (`librobne/hcp/capacity_test.go:18`
  — 64 vCPU / 128 GiB / 1500 pods → request/low/avg 7
  `memory_request`, medium 5, high 3 `cpu_usage`; headroom 3/3/3/1/−1)
  plus the **live hcp-mgmt observation captured 2026-10-26**
  (compact topology → `shared_infrastructure`, single-replica basis →
  `illustrative_only`, QPS 16.7, MCE refs 8/8/8/4/2,
  current_hc_count 3, scenarios 7/7/7/3/2 with headroom +4/+4/+4/0/−1).
  Review-2 W5: the full 25-column live row MUST be pasted into the
  build issue as a plan attachment before build (summary values are not
  a fixture); sanitized by replacing the cluster UUID with a fixture
  UUID — versions, QPS, and values are lab data, not secrets. Both pin
  CLI output scenario-by-scenario against the backend ingestion tests.
- nise generator stays in open #702 (review-2 W5: no new issue; #392
  itself tested via librobne unit + ingestion inline-CSV + API tests +
  live SNO — nise was never a W4 input).
- `docs/plans/robne-cli-spec.md` + public robne docs: CSV contract,
  plugin name, output shape/version, examples — plus the offline-trust
  note, the `cluster-quota` rename, the root-key rename, and the
  one-line pre-release-break migration note. Spec checklist
  (review-6 T8): Envelope § (v12 + `hcp_recommendations`), §472
  mixing list (+`hcp`), BH § (v12-when-BH+hcp), one new HCP-capacity
  subsection (contract, plugin, sibling shape, clock rule,
  selectors).

## What is left to the builder

- Exact JSON sibling field layout (mirroring the service's row shape —
  `scenarios`, `warnings`, `mce_references`, source versions — but the
  final field order/naming consistency with existing siblings is a
  builder call).
- Table/CSV column sets for the capacity rendering.
- Diff output detail formatting (follow the existing entity diff style).
- Warning/error message wording (stderr warning texts, friendly
  messages).
- Test placement (cmd/robne vs librobne/csv) per repo convention.

## Constraints

- librobne stays CGO-free and dependency-light (CLI build constraint).
- The KMO writer ↔ backend reader ↔ CLI reader triple must agree on the
  25-column header; any change is a coordinate-all-sides event.
  (Verified review-2 N8: KMO `csvHeader()` +
  `TestHCPCapacityRow_Contract` match the backend list exactly;
  emission gating C1 and monthly accumulation confirmed in
  `hcp_capacity_collector.go`.)
- Backend ingestion behavior must not change (parser move is a pure
  relocation + import swap + N6 string fix; all ingestion tests must
  stay green; handler refactor guarded by new characterization
  goldens).
- Offline trust (review-2 N7): robne consumes unauthenticated,
  hand-copied CSVs. No integrity check is attempted; the public docs
  state the trust assumption. The manifest-vs-row cross-check is a
  tripwire, not authentication.
