# ADR-0338: Column descriptors over extractor closures on the recommendation hot path

## Status

Accepted

## Phase

Container recommendation engine correctness and performance
([#602](https://github.com/pgarciaq/ros-ocp-backend/issues/602))

## Context

The decay-weighted percentile walk accepted one extractor per column:

```go
func MultiWeightedPercentileWithExtras(rows []DigestRow, now time.Time,
    halfLifeHours float64, opts *WindowExtraOpts,
    extractors ...func(DigestRow) int64) ([]int64, WindowExtras)
```

Two properties of that signature made it expensive, and neither was visible as a
regression because the cost was constant:

1. **Extractors must be closures.** A percentile is a runtime value, so
   `func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, cfg.CostPercentile) }`
   captures a variable. Go allocates such a closure's context on the heap, and when
   the closure is built inside a helper function it must outlive that frame — so
   every call that assembled an extractor list paid one allocation per capturing
   closure.
2. **`DigestRow` is passed by value, and it is 312 bytes.** Every column of every
   row therefore copied the whole struct onto the argument stack. A 30-row,
   10-column pass copied roughly 94 KB per call.

A CPU profile of the container hot path attributed **~34% of the fused call** to
the four percentile-selector closure frames.

[#602](https://github.com/pgarciaq/ros-ocp-backend/issues/602) required
single-sourcing the post-percentile math (adaptive margin → OOM bump → floor →
limit) across `RecommendCPU`, `RecommendMemory` and the fused
`RecommendCPUAndMemory`. Doing that by extracting shared helpers meant the extractor
lists had to be shared too, and the first implementation did so with closures
living in shared helper functions. That was correct but strictly *worse* than the
code it replaced on the hot path: **6 allocations and 224 B per fused call, against
2 allocations and 160 B before the issue, plus a measurable time regression.**

Two options were evaluated for the extractors:

- **Option A — closures in shared helpers.** Single-sourced, no API change,
  +4 allocations per fused call. This is what shipped first and was measured.
- **Option B — a column descriptor.** A `uint8` naming a `DigestRow` field, with
  `Value(*DigestRow)` reading it in place. The extractor list becomes plain data
  that stays on the stack, and no struct is copied per column.

A third path — replacing the existing closure-based signature outright — was
rejected: those symbols are re-exported through `internal/engine/compat.go`, which
[ADR-0337](0337-compat-bridge-until-consumers-migrate.md) freezes until its external
consumers migrate. Changing them would break consumers mid-migration for no
in-repo gain.

## Decision

Adopt **Option B**: introduce `types.Column` plus
`MultiWeightedPercentileColumns()` and `ColumnWindowOpts`, and use them from the
container package.

- `Column` is a `uint8` with a `ColNone` zero value, so a zero opts struct means
  "don't track this signal" — the role a nil func played in `WindowExtraOpts`.
- Dynamic selectors are resolved *before* the walk by `CPUPercentileColumn` and
  `MemPercentileColumn`. The existing `SelectCPUUsagePercentile` /
  `SelectMemUsagePercentile` are re-implemented as thin wrappers over those
  resolvers, so the percentile-to-column chain still exists exactly once.
- The closure walk is **retained unchanged** for the frozen compat bridge. Two
  walks therefore coexist, which is only acceptable because a differential test
  asserts they return identical values and extras across a matrix of windows,
  half-lives, column lists and option shapes.
- An unknown `Column` reads 0 rather than panicking: a defect should degrade a
  recommendation to its configured floor, not crash a processor batch.

## Consequences

- The fused container path is **~2.2× faster** than the pre-#602 code (4.50 µs →
  2.08 µs, 30-row window, 12 interleaved runs, p=0.000) with the **same 2
  allocations and same 160 B** as before the issue. Selector bodies fell from ~34%
  of the profile to 3.98%, inlined into the walk.
- Recommendation output and every `expl_*` field are byte-for-byte identical to the
  pre-#602 code across 1296 configurations.
- The allocation count is now a **test**, not a benchmark convention:
  `testing.AllocsPerRun` pins the fused and solo paths to 2 allocations and the
  mismatched-window fallback to 4, so a reintroduced closure fails CI.
- The by-value `DigestRow` cost was pre-existing and would have stayed invisible
  indefinitely, since a constant cost never shows up as a regression. **Hot-path
  code should be reviewed for argument-passing cost, not only algorithmic cost.**
- Namespace, node, GPU, PVC, quota and VM recommend paths do not use this walk and
  are unaffected. Adopting `Column` there is available but out of scope.
- The differential test is not ULP-sensitive, so it cannot by itself prove
  float-bit identity; that is established by a 1296-configuration output dump.
  A generative property test would be the way to strengthen it.
- Follow-on: [#618](https://github.com/pgarciaq/ros-ocp-backend/issues/618) — the
  per-row `sync.Map` table lookup in `DecayTableLookup` is now ~27% of what remains
  of the container hot path, so fixing it compounds this win.

## Alternatives rejected

- **Keep closures (Option A).** Correct, and 4 fewer things to explain, but it pays
  a permanent allocation tax on the hottest path in the engine to avoid a change
  contained to one package plus two new functions.
- **Cache extractor lists per config.** Adds a lookup (and an unbounded cache
  keyed by float) to save 64 B.
- **Replace the closure signature outright.** Breaks the frozen compat bridge for
  no in-repo benefit.
- **Revert #602 and revisit later.** Leaves the duplicated post-percentile math and
  the untested-but-shipped drift in place, and keeps the larger pre-existing cost.
