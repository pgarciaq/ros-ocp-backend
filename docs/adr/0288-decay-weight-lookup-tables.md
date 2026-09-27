# ADR-0288: Precomputed decay weight lookup tables

## Status

Accepted

## Phase

Engine / Algorithm (performance)

## Context

Exponential decay weighting ([ADR-0005](0005-decay-weighted-average-half-life.md),
[ADR-0204](0204-continuous-hour-decay-vs-calendar-day-windows.md)) runs in the
hottest recommendation path: every digest row in every term, engine, and fused
extractor pass through `DecayWeight()` inside `MultiWeightedPercentileWithExtras`.

On a medium-sized cluster (500 containers, medium + long terms, cost + performance
engines), the fused digest walk invoked `math.Exp` tens of thousands of times per
recommendation cycle. `math.Exp` is a transcendental function — cheap in isolation,
but dominant when multiplied by row × term × engine × extractor fan-out.

The native engine performance audit (P0-1 / M4) identified this as the densest
floating-point hot path in the recommend phase.

## Decision

Replace per-row `math.Exp` with **precomputed lookup tables** keyed by integer
half-life hours:

1. `DecayWeight()` and the prepared row-walk evaluator quantize age and whole-number
   half-lives to integer hours (`math.Round`) and use `table[ageInt]`.
2. Tables are built **lazily** on first use per distinct half-life via `sync.Map`
   in `librobne/internal/decay/decay.go`. Each table spans `0 … halfLife×2` hours
   (twice the half-life covers the effective decay window).
3. Non-integer half-lives (e.g. `167.3`) fall back to direct `math.Exp`.
   Negative ages on integer-table paths are clamped to age zero; negative ages
   on the non-integer path retain direct `math.Exp` behavior.
4. When a tenant overrides `window_days` but leaves `decay_halflife_hours` NULL,
   `term_config.go` auto-derives `window_days × 12` hours, producing integer
   half-lives that hit the lookup path.

## Alternatives Considered

### Keep `math.Exp` per row

Simplest code, but leaves P0-1 unresolved. Rejected after audit showed
28k–60k transcendental calls per cycle on representative clusters.

### Single normalized lookup table (fixed half-life)

One table indexed by `(age, window)` with normalized weights would eliminate
`sync.Map` and halflife-keyed tables. Rejected because it would **remove the
`decay_halflife_hours` tuning knob** — tenants and operators rely on per-term
half-life overrides ([configurability](../architecture/configurability.md)).
Distinct integer half-lives (12, 84, 168, 360, 720, …) require distinct curves.

### `go:generate` with embedded static tables

Pre-build tables at compile time and embed as `[]float64` constants. Rejected
because ros-ocp-backend runs as a **batch worker per Kafka payload**, not a
long-lived daemon warming caches across hours. The set of half-lives is bounded
(typically 2–3 per invocation: short=0, medium, long) but not fixed at compile
time — custom tenant windows produce arbitrary integer multiples of 12. Lazy
`sync.Map` construction costs microseconds per distinct half-life; acceptable
for batch invocation. `go:generate` would also require maintaining generated
artifacts for up to 8760 distinct half-life keys.

### Fixed-point or bit-shift approximation of `exp`

Faster than `math.Exp` but adds bespoke numeric code and harder-to-reason-about
error bounds. Lookup tables with exact `math.Exp` at build time are simpler and
testable (`TestDecayWeight_TableLookup_MatchesExp`).

## Consequences

- **Performance:** `math.Exp` eliminated from the hot path for standard integer
  half-lives (plugin defaults and auto-derived `window_days × 12`).
- **Accuracy:** Integer-hour quantization of age introduces at most ~0.2% weight
  error vs continuous-hour `math.Exp` (half-hour rounding on a 168h half-life).
  Negligible for recommendation sizing — well below adaptive margin (15–50%) and
  percentile noise.
- **Memory:** One `[]float64` per distinct half-life seen in-process; tables persist
  for process lifetime (typically one Kafka message batch). Bounded by
  `halfLife × 2 + 1` entries per table.
- **Configurability preserved:** Per-term `decay_halflife_hours` remains fully
  tunable; auto-derive keeps custom `window_days` aligned without manual half-life
  entry.

## Implementation follow-up — #618 (2026-09-27)

The initial implementation resolved the cached table inside `DecayWeight()` for
every digest row. #618 prepares at most one immutable evaluator for each of the
four weighted row walks — `MultiWeightedPercentileWithExtras`,
`MultiWeightedPercentileColumns`, node classification, and PVC weighted least
squares — and reuses it across that walk. The evaluator and the existing
half-life-keyed cache live in `librobne/internal/decay`, which lets the separate
`types`, `node`, and `pvc` packages share the implementation without expanding
the library's public API. Tables, quantization, cutoffs, fallbacks, and the
single-value `DecayWeight()` / `DecayTableLookup()` contracts are unchanged.

On Go 1.26.8 / Intel Core Ultra 7 165H, `benchstat` over 10 samples per version
reported these 30-row/day benchmark changes (`p=0.000` for each end-to-end
path):

| Path | Before | After | Change | Allocations |
|---|---:|---:|---:|---:|
| Fused container recommendation | 3.779 µs | 1.437 µs | −62.0% | 160 B, 2 allocs (unchanged) |
| Node classification | 3.860 µs | 1.534 µs | −60.3% | 960 B, 4 allocs (unchanged) |
| PVC weighted least-squares slope | 1.392 µs | 0.357 µs | −74.4% | 0 B, 0 allocs (unchanged) |
| Closure percentile walk (30 rows) | 2.417 µs | 0.862 µs | −64.3% | 8 B, 1 alloc (unchanged) |

The final fused CPU profile shows `sync.Map.Load` at 0.72% flat / 1.45%
cumulative. One cache resolution per row-walk invocation remains expected; map
work no longer scales with the number of rows. Node classification delays
evaluator construction until the first row with both positive allocatable CPU
and memory, preserving the old behavior when no row reaches the weighted path.
PVC WLS returns immediately on an empty digest slice, preserving its previous
zero result without evaluating decay state.
A separate exact-bit test compares prepared weights and the compatibility
function against the pre-#618 branch and arithmetic order, including distinct
half-lives and fallback boundaries.

## Related Decisions

- [ADR-0005](0005-decay-weighted-average-half-life.md): Decay-weighted average design.
- [ADR-0204](0204-continuous-hour-decay-vs-calendar-day-windows.md): Hour-based age.
- [ADR-0003](0003-read-once-compute-n-terms.md): Fused digest walk context.

## References

- [librobne/types/decay.go](../../librobne/types/decay.go) — `DecayWeight()` and percentile walks
- [librobne/types/decay_table.go](../../librobne/types/decay_table.go) — compatibility lookup and half-life derivation
- [librobne/internal/decay/decay.go](../../librobne/internal/decay/decay.go) — shared evaluator, table build, and lookup
- [internal/engine/term_config.go](../../internal/engine/term_config.go) — auto-derive half-life
- [docs/architecture/decay-weights.md](../architecture/decay-weights.md)
- [docs/performance/native-engine-audit-2026-06.md](../performance/native-engine-audit-2026-06.md) — P0-1
