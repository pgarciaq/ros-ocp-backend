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

1. `DecayWeight()` quantizes `ageHours` and `halfLifeHours` to integer hours
   (`math.Round`) and looks up `table[ageInt]` when half-life is a whole number.
2. Tables are built **lazily** on first use per distinct half-life via `sync.Map`
   in `internal/engine/decay_table.go`. Each table spans `0 … halfLife×2` hours
   (twice the half-life covers the effective decay window).
3. Non-integer half-lives (e.g. `167.3`) and negative ages fall back to direct
   `math.Exp` — preserving exact math for edge configurations.
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

## Related Decisions

- [ADR-0005](0005-decay-weighted-average-half-life.md): Decay-weighted average design.
- [ADR-0204](0204-continuous-hour-decay-vs-calendar-day-windows.md): Hour-based age.
- [ADR-0003](0003-read-once-compute-n-terms.md): Fused digest walk context.

## References

- [internal/engine/decay.go](../../internal/engine/decay.go) — `DecayWeight()`
- [internal/engine/decay_table.go](../../internal/engine/decay_table.go) — table build + lookup
- [internal/engine/term_config.go](../../internal/engine/term_config.go) — auto-derive half-life
- [docs/architecture/decay-weights.md](../architecture/decay-weights.md)
- [docs/performance/native-engine-audit-2026-06.md](../performance/native-engine-audit-2026-06.md) — P0-1
