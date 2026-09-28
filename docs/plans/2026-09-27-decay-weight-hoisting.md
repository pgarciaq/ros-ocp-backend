# Decay Table Lookup Hoisting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the per-row `sync.Map` table resolution from all four #618 decay-weighted loops without changing decay values or existing exported APIs.

**Architecture:** Prepare immutable, per-half-life lookup state once for each weighted row walk and reuse it for every row. In node classification, defer preparation until the first row with both positive allocatable CPU and memory so rows that never enter the weighted path retain the old behavior. Keep `DecayWeight` and `DecayTableLookup` signatures and numeric behavior intact; share the existing lazy table cache rather than creating another cache. Because the four consumers span Go packages, the shared evaluator/cache live in `librobne/internal/decay`; `types` retains its existing exported functions as wrappers, and `node`/`pvc` import only the nested module's internal package.

**Tech Stack:** Go 1.26.3 module floor; `librobne` nested Go module; root module vendored copy maintained by `make vendor-librobne-check`; Go benchmarks, pprof, and benchstat.

**Spec:** [GitHub issue #618 scope/design comment](https://github.com/pgarciaq/ros-ocp-backend/issues/618#issuecomment-5850176453).

## Global Constraints

- Optimize all four agreed loops: `MultiWeightedPercentileWithExtras`, `MultiWeightedPercentileColumns`, `classifyNode`, and `computePVCGrowthSlopeWLS`.
- Preserve `DecayWeight(ageHours, halfLifeHours)` and `DecayTableLookup(ageHours, halfLifeHours)` signatures and externally visible behavior.
- Preserve integer-hour rounding, negative-age handling, non-integer `math.Exp`, nonpositive half-life behavior, the table cutoff, and the oversized-table fallback.
- Keep cached tables immutable and safe for concurrent readers; different half-lives must never reuse one another's table.
- Do not add a second global cache or accept any accuracy-for-speed trade-off.
- Keep resolver state stack-local where possible; the existing fused container benchmark is 160 B/op and 2 allocs/op, and must not regress.
- Do not edit the #618 issue body, commit, or push without separate explicit authorization.

## Review Focus

1. **Interleaved distinct integer half-lives** (84h, 168h, 336h) must each use their own table; this catches stale/last-table reuse.
2. **Fractional and negative ages** must preserve the existing split: integer table paths clamp negative rounded ages; non-integer paths retain direct exponential behavior.
3. **Table edge ages** immediately below, at, and above `2 × halfLife` must retain the current rounding and zero-cutoff behavior.
4. **Nonpositive, sub-hour-rounded, and non-integer half-lives** must retain their current return/math paths without loading an integer table.
5. **Oversized integer half-lives and concurrent first use** must retain the direct-`Exp` cutoff and avoid races or cross-table contamination.

---

### Task 1: Add loop benchmarks and record the pre-change baseline

**Files:**
- Create: `librobne/types/decay_weight_bench_test.go`
- Create: `librobne/node/recommend_bench_test.go`
- Create: `librobne/pvc/recommend_bench_test.go`
- Existing benchmark used without modification: `librobne/container/recommend_parity_test.go:497` (`BenchmarkRecommendCPUAndMemory`)

**Interfaces:** Benchmarks call current public/package functions only; no production interface changes.

- [ ] Add `BenchmarkDecayWeight_PerCall` for a warmed 336-hour table and `BenchmarkMultiWeightedPercentileWithExtras_30Rows` using 30 distinct dates and non-constant values. Keep fixture creation outside timed loops, call `b.ReportAllocs`, and assign results to package-level sinks.
- [ ] Add `BenchmarkClassifyNode_30Days` using the existing `makeDigestRow` fixture helper, 30 consecutive daily rows, positive allocatable CPU/memory, and a 336-hour half-life.
- [ ] Add `BenchmarkComputePVCGrowthSlopeWLS_30Days` using 30 distinct, increasing `UsageBytesAvg` values and a 336-hour half-life.
- [ ] Keep all fixture creation outside the timer and assign each result to a package-level sink. The type-level benchmark shape is:

```go
var decayWeightBenchmarkSink float64
var decayPercentileBenchmarkSink int64

func BenchmarkDecayWeight_PerCall(b *testing.B) {
	ages := [...]float64{0, 24, 72, 168, 240, 336, 480, 672}
	DecayWeight(0, 336) // warm the current table before timing
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decayWeightBenchmarkSink = DecayWeight(ages[i%len(ages)], 336)
	}
}

func BenchmarkMultiWeightedPercentileWithExtras_30Rows(b *testing.B) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := make([]DigestRow, 30)
	for i := range rows {
		rows[i] = DigestRow{
			BucketDate:    now.Add(time.Duration(i-29) * 24 * time.Hour),
			CPUUsageP95MC: int64(i + 1),
		}
	}
	extract := func(row DigestRow) int64 { return row.CPUUsageP95MC }
	var result []int64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, _ = MultiWeightedPercentileWithExtras(rows, now, 336, nil, extract)
	}
	if len(result) != 0 {
		decayPercentileBenchmarkSink = result[0]
	}
}
```

The node and PVC benchmark bodies are:

```go
var nodeClassificationBenchmarkSink nodeClassification

func BenchmarkClassifyNode_30Days(b *testing.B) {
	allocCPU, allocMem := ptr64(16000), ptr64(65536)
	days := make([]DigestRow, 30)
	for i := range days {
		day := i + 1
		days[i] = makeDigestRow("bench-node", day,
			8000+int64(day), 10000+int64(day), 30000+int64(day), 40000+int64(day),
			12000, 48000, allocCPU, allocMem)
	}
	endDate := days[len(days)-1].BucketDate
	cfg := defaultRecConfig()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeClassificationBenchmarkSink = classifyNode("bench-node", days, cfg, defaultThresholdSettings, 336, endDate)
	}
}

var pvcGrowthSlopeBenchmarkSink float64

func BenchmarkComputePVCGrowthSlopeWLS_30Days(b *testing.B) {
	digests := make([]PVCDigestRow, 30)
	for i := range digests {
		digests[i].UsageBytesAvg = int64(i+1) * (1 << 20)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pvcGrowthSlopeBenchmarkSink = computePVCGrowthSlopeWLS(digests, 336)
	}
}
```

The benchmark files import `testing`; the type benchmark file also imports `time` and the node benchmark file uses the existing package-level fixture helpers. All fixture creation stays outside timed loops.
- [ ] Run the benchmarks before production edits:

```bash
go test -C librobne ./types ./container ./node ./pvc -run '^$' \
  -bench '^(BenchmarkDecayWeight_PerCall|BenchmarkMultiWeightedPercentileWithExtras_30Rows|BenchmarkRecommendCPUAndMemory|BenchmarkClassifyNode_30Days|BenchmarkComputePVCGrowthSlopeWLS_30Days)$' \
  -benchmem -count=10 | tee /tmp/opencode/618-before.txt
```

Expected: successful benchmark run with timings, bytes/op, and allocs/op recorded for all five paths. Preserve this report for the post-change comparison.

### Task 2: Add and test the per-invocation decay evaluator

**Files:**
- Create: `librobne/internal/decay/decay.go`
- Create: `librobne/types/decay_weight_test.go`
- Modify: `librobne/types/decay_table.go`
- Modify: `librobne/types/decay.go`
- Add resolved-path benchmark to: `librobne/types/decay_weight_bench_test.go`

**Interfaces:** Internal package interface: `decay.NewEvaluator(halfLifeHours float64) decay.Evaluator` and `(decay.Evaluator).Weight(ageHours float64) float64`, plus `decay.Weight` and `decay.TableLookup` for the existing `types` wrappers. These exports are restricted by Go's `internal` package rule to the nested `librobne` module. The evaluator stores scalar half-life state and, only for an in-range integer half-life, a reference to the already-built immutable table.

- [ ] Write `TestDecayWeightEvaluator_MatchesLegacyBehavior` first. Use a test-only frozen reference for the pre-#618 branch/order and a fixture matrix covering 84h/168h/336h, zero and negative half-lives, a positive sub-hour half-life that rounds to zero, 167.3h, negative/fractional ages, the rounded cutoff boundary, and an integer half-life above the table-size cap. Compare `math.Float64bits` so a changed floating-point evaluation order cannot pass on a tolerance.
- [ ] Keep the test oracle independent of the new evaluator and the cache. It reproduces the old table-entry arithmetic (`k := -math.Ln2 / float64(hlInt)`, then `math.Exp(k * float64(ageInt))`), uses the pre-#618 table limit as a test literal (`100_000`), and applies the old branch order:

```go
func legacyDecayWeight(ageHours, halfLifeHours float64) float64 {
	if halfLifeHours <= 0 {
		return 1
	}
	ageInt := int(math.Round(ageHours))
	hlInt := int(math.Round(halfLifeHours))
	if hlInt <= 0 {
		return 1
	}
	if float64(hlInt) != halfLifeHours {
		return math.Exp(-ageHours * math.Ln2 / halfLifeHours)
	}
	if ageInt < 0 {
		ageInt = 0
	}
	maxAge := hlInt * 2
	if maxAge > 100_000 {
		if ageInt > maxAge {
			return 0
		}
		return math.Exp(-math.Ln2 * float64(ageInt) / float64(hlInt))
	}
	if ageInt > maxAge {
		return 0
	}
	k := -math.Ln2 / float64(hlInt)
	return math.Exp(k * float64(ageInt))
}
```

For each fixture, compare both `decay.NewEvaluator(tc.halfLife).Weight(tc.age)` and `DecayWeight(tc.age, tc.halfLife)` to this oracle with `math.Float64bits`. Use cutoff fixtures around rounded ages 336 for a 168-hour half-life and 100,002 for a 50,001-hour half-life.
- [ ] Write `TestDecayWeightEvaluator_AlternatingHalfLives` with distinct expected results at the same age for 84h, 168h, and 336h, alternating calls so reuse of the wrong table fails.
- [ ] Write a concurrent evaluator test using a previously unused in-range integer half-life and multiple goroutines; each goroutine prepares and reads the evaluator and compares against the frozen reference.
- [ ] Run the new targeted tests before adding the implementation and confirm the failure is specifically that the evaluator API is not yet implemented.
- [ ] Extract/share the existing lazy table-construction/cache retrieval without changing table contents or arithmetic order. Implement the candidate value evaluator so table resolution occurs at construction, while nonpositive, non-integer, and oversized paths preserve their old behavior.
- [ ] Keep `DecayWeight` and `DecayTableLookup` signatures and behavior unchanged. Retain the legacy single-value branch/order in internal `decay.Weight`; preserve direct-call performance using the focused comparison. Only the four row loops use the prepared evaluator. Add `BenchmarkDecayWeightEvaluator_Resolved` with evaluator construction outside the timed loop, then compare direct per-call and resolved weights in the same process.
- [ ] Run:

```bash
go test -C librobne ./types -run '^TestDecayWeightEvaluator_' -count=1
go test -C librobne -race ./types -run '^TestDecayWeightEvaluator_' -count=1
```

Expected: both commands pass; the race run reports no data races.

### Task 3: Hoist table resolution in both percentile walks

**Files:**
- Modify: `librobne/internal/decay/decay.go` (shared evaluator/cache interface)
- Modify: `librobne/types/decay.go`
- Modify: `librobne/types/decay_columns.go`
- Test coverage: `librobne/types/column_test.go` and the new `librobne/types/decay_weight_test.go`

**Interfaces:** Both loops construct one `decay.Evaluator` from `halfLifeHours` after existing empty-input early returns and call its `Weight(ageHours)` method per row. No public signatures change.

- [ ] Change `MultiWeightedPercentileWithExtras` and `MultiWeightedPercentileColumns` to prepare one evaluator before their row loops and use it for every row.
- [ ] The loop transformation is limited to resolving the weight function before iteration and replacing the in-loop call; retain surrounding order and calculations:

```go
decay := decaypkg.NewEvaluator(halfLifeHours)
for i, row := range rows {
	// existing idle/trend/date calculations stay in their current order
	ageHours := now.Sub(row.BucketDate).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	w := decay.Weight(ageHours)
	// existing aggregation continues unchanged
}
```

- [ ] Preserve each loop's existing age calculation/clamp, extractor order, weighted-sum order, trend/idle calculations, and early returns.
- [ ] Run exact evaluator tests and the existing closure-vs-column differential suite:

```bash
go test -C librobne ./types -run '^(TestDecayWeightEvaluator_|TestMultiWeightedPercentileColumns)' -count=1
```

Expected: direct decay edge cases pass and the two percentile implementations remain output-identical.

### Task 4: Hoist table resolution in node and PVC loops

**Files:**
- Modify: `librobne/internal/decay/decay.go`
- Modify: `librobne/node/recommend.go`
- Modify: `librobne/pvc/recommend.go`
- Benchmark coverage: `librobne/node/recommend_bench_test.go`, `librobne/pvc/recommend_bench_test.go`
- Existing behavior tests: `librobne/node/recommend_test.go`, `librobne/pvc/recommend_test.go`

**Interfaces:** `classifyNode` prepares one `decay.Evaluator` for its `halfLifeHours` immediately before the first row that reaches the existing allocatable/weight branch; it does not construct one if no row reaches that branch. `computePVCGrowthSlopeWLS` prepares one evaluator before iterating digests; its existing `n < 2` and nonpositive-half-life OLS branches remain unchanged.

- [ ] In `classifyNode`, prepare the evaluator once immediately before the first reachable per-day weight calculation and replace only the per-day `types.DecayWeight` call. Preserve the previous no-weight-call behavior if no row has both positive allocatable CPU and memory.
- [ ] In `computePVCGrowthSlopeWLS`, prepare the evaluator once before the digest loop and replace only the per-digest `types.DecayWeight` call.
- [ ] In PVC, do not construct the evaluator in `computePVCGrowthSlope`'s OLS branch; it belongs only in `computePVCGrowthSlopeWLS`, after the current caller checks `n >= 2` and `halfLife > 0`.
- [ ] Add a node regression test with an accepted extreme half-life and no weight-bearing row; verify it does not panic. Mutation-check eager evaluator construction and vary fixtures across missing, zero, CPU-only, and memory-only allocatable data.
- [ ] Run existing node decay-spike and PVC growth-trend tests plus both new benchmarks' package tests:

```bash
go test -C librobne ./node ./pvc -run 'Decay|GrowthTrend' -count=1
```

Expected: existing recommendation assertions pass without changes; the benchmark functions compile and exercise 30 distinct rows/days.

### Task 5: Vendor sync, rationale record, measurement, and full verification

**Files:**
- Generated production-source sync only under `vendor/github.com/redhatinsights/ros-ocp-backend/librobne/`
- Generated production-source sync for `vendor/github.com/redhatinsights/ros-ocp-backend/librobne/internal/decay/`
- Modify: `docs/adr/0288-decay-weight-lookup-tables.md` with a dated implementation follow-up and corrected current source references
- Modify: `CHANGELOG.md` under `## [Unreleased]` / `### Performance` if the before/after benchmark confirms a material measured improvement
- Generated: `docs-site/changelog.md` via `make docs-generate` from the root changelog
- GitHub comment only: issue #618 (description stays unchanged)

**Interfaces:** The vendor tree must exactly match `go mod vendor` output for `./librobne`; tests and `librobne/go.mod` remain only in the nested module.

- [ ] Run `go mod vendor` through the repository check and inspect the resulting diff; keep only the expected production Go files under the vendored librobne tree:

```bash
GIT_INDEX_FILE=/tmp/opencode/618-vendor-check.index git read-tree HEAD
GIT_INDEX_FILE=/tmp/opencode/618-vendor-check.index git add -- vendor/github.com/redhatinsights/ros-ocp-backend/librobne
GIT_INDEX_FILE=/tmp/opencode/618-vendor-check.index make vendor-librobne-check
```

The temporary index lets the repository's `git diff`-based check pass on the generated, intentionally uncommitted vendor subtree without staging anything in the user's real index. Preserve the generated `librobne` production sources that `go mod vendor` produces; the current vendor snapshot also lacks some committed #602 source updates.

- [ ] Run after-change benchmarks on the same host and Go version, preserving output:

```bash
go test -C librobne ./types ./container ./node ./pvc -run '^$' \
  -bench '^(BenchmarkDecayWeight_PerCall|BenchmarkDecayWeightEvaluator_Resolved|BenchmarkMultiWeightedPercentileWithExtras_30Rows|BenchmarkRecommendCPUAndMemory|BenchmarkClassifyNode_30Days|BenchmarkComputePVCGrowthSlopeWLS_30Days)$' \
  -benchmem -count=10 | tee /tmp/opencode/618-after.txt
go install golang.org/x/perf/cmd/benchstat@latest
benchstat /tmp/opencode/618-before.txt /tmp/opencode/618-after-ultimate.txt
```

Expected: compare measured results rather than promise a fixed percentage. The fused container path must not exceed its baseline allocations (160 B/op, 2 allocs/op). If a relevant path has no measurable improvement or regresses, report the data and revisit the design before claiming the optimization successful.

- [ ] Append a concise ADR-0288 implementation follow-up recording the resolved-per-loop design, unchanged accuracy/API contract, post-change measurements, the internal evaluator package, and the corrected current source references. Do not update a historical performance audit or create a new ADR/index entry.
- [ ] If benchstat shows a statistically significant improvement in the end-to-end container, node, or PVC benchmark without allocation regression, add a factual #618 entry under `CHANGELOG.md` → `## [Unreleased]` → `### Performance`, using only measured results and stating that outputs/accuracy are unchanged. Then run `make docs-generate` to refresh the generated `docs-site/changelog.md`; inspect the full diff for unrelated generated-page churn. If end-to-end measurements do not confirm an improvement, do not publish a performance claim and report that result before treating the issue as resolved.

- [ ] Inspect all four row-loop bodies to verify evaluator construction is outside each loop and search for residual per-row `DecayWeight` calls in those functions. Capture a post-change CPU profile for the fused container benchmark and verify `sync.Map.Load` no longer scales with digest-row count; one resolution per loop invocation may remain visible.
- [ ] Run repository verification:

```bash
make test-short
go test -C librobne -race -count=1 ./types ./container ./node ./pvc
make lint
make build
make docs-lint docs-drift docs-sync-check
GIT_INDEX_FILE=/tmp/opencode/618-vendor-check.index make vendor-librobne-check
```

Expected: every command exits 0. Report any failures by command and package; do not infer success from partial test runs.

- [ ] Mutation-check the distinct-half-life and fallback tests by temporarily changing the evaluator to reuse a different half-life's table and to route a non-integer half-life through the table path. Confirm the failures identify wrong decay weights, restore only the deliberate mutations, and rerun the targeted tests. The three distinct half-life fixtures must remain in the committed test.
- [ ] Add an implementation-results comment to [#618](https://github.com/pgarciaq/ros-ocp-backend/issues/618) listing the files changed, measured benchmark comparison, tests run, and any deviations from the planned evaluator representation. Do not edit the issue body.
- [ ] Stop without committing or pushing; the repository approval gate requires separate explicit authorization for shipment.

## Scope Dismissals

- OpenAPI, Bruno, API cheat sheet, UI/E2E, and public docs: no API/UI contract or customer-visible behavior changes; decay math remains exact relative to the existing implementation.
- New ADR and ADR index updates: no new ADR; record this implementation follow-up in the existing ADR-0288 instead.
- Historical performance audit: not updated; retain the pre-optimization report as a historical record and put current before/after evidence in ADR-0288 and the #618 implementation comment.
- Public decay documentation: the public formula, configuration, and accuracy contract do not change. The internal implementation-detail follow-up does not need a parallel public architecture-page edit; `docs-site/changelog.md` is regenerated from `CHANGELOG.md` only if end-to-end results earn a performance entry.
