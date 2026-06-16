# ADR-0295: Integer-first arithmetic across the native engine

## Status

Accepted

## Phase

Foundational (cross-cutting)

## Context

The legacy Kruize pipeline used `float64` everywhere: CSV metrics were parsed as
floats, stored as floats (often inside JSONB blobs), computed as floats, and
serialized as floats in API responses. This caused three categories of problems
at fleet scale:

### 1. Aggregation drift

IEEE 754 double-precision arithmetic is not associative. Summing thousands of
`float64` savings values across a fleet produces different totals depending on
ordering and grouping. For a tenant with 100,000 containers, fleet savings
aggregated by cluster then summed differed from savings aggregated globally —
typically by cents, but occasionally by dollars when intermediate terms spanned
wide magnitudes. Users see different totals in different views with no explanation.

### 2. Representation noise

`float64` cannot represent `0.01` (one cent) exactly. The closest IEEE 754 value
is `0.01000000000000000020816681711721685228163...`. After a chain of multiply →
add → round operations, values like `$12.345000000000001` appeared in API
responses and database columns. The legacy code papered over this with
`math.Round(x*100)/100` at various call sites, but this was inconsistent and
fragile — adding a new arithmetic step could reintroduce noise.

### 3. Performance cost

On modern x86-64 and ARM64 CPUs, integer multiply (`IMUL` / `MUL`) has
deterministic latency (3 cycles) and pipelines predictably. Floating-point
multiply (`MULSD` / `FMUL`) has similar throughput for isolated operations, but
`math.Exp`, `math.Pow`, and division introduce variable-latency transcendental
paths. In the recommendation hot loop — where every digest row passes through
decay weighting, percentile computation, and savings estimation — the cumulative
cost of float64 operations was measurable (see ADR-0288 for the decay weight
case).

### What Kruize got wrong

Kruize's PostgreSQL tables stored metrics as JSONB blobs with string keys and
`float64` values. Every recommendation cycle deserialized the entire history,
performed floating-point arithmetic in the JVM, and wrote float results back
through HTTP. The problem was not PostgreSQL — it was that the application never
committed to a fixed-point representation, so precision errors propagated through
every layer.

## Decision

The native Go engine uses **integer arithmetic as the default representation**
for all quantitative domains, converting to/from external formats only at
ingestion and API boundaries:

| Domain | Internal representation | Scale | Boundary conversion |
|--------|------------------------|-------|---------------------|
| Money | `int64` cents | 1 cent = 1 | `money.USDToCents()` / `money.FormatCents()` |
| Savings computation | `int64` micro-cents | 1 cent = 1,000,000 | `MicroCentsToCents()` at DB write |
| CPU | `int64` millicores | 1 core = 1,000 | CSV parse: `float64 × 1000 → int64` |
| Memory | `int64` bytes or KiB | — | CSV parse: direct or `float64 × 1024 → int64` |
| Ratios / margins | `int64` basis points | 1% = 100, 100% = 10,000 | `MarginScale = 10000` |
| Decay weights | `float64` lookup table | precomputed per half-life | `math.Exp` at table build time only |
| API output | `string` or structured | — | `MoneyAmount{Value: "1.23", Units: "USD"}` |

### Core rules

1. **Parse once, convert once.** CSV floats become `int64` at parse time
   (ADR-0098). No float intermediates survive past the ingestion boundary.

2. **Compute in integers.** Multiplications, additions, and comparisons in the
   recommendation engine use `int64`. Overflow headroom is verified by analysis
   (see ADR-0291 for the worst-case product: `~7.3×10¹⁶`, well within `int64`
   max `~9.2×10¹⁸`).

3. **Convert at the boundary.** `int64` → display format happens once, at API
   serialization or DB write. The `MoneyAmount` struct (ADR-0064) ensures the
   API contract is a string with explicit units, not a bare float.

4. **Where float64 is unavoidable, precompute and cache.** Decay weights require
   `math.Exp` — but the engine computes lookup tables once per distinct half-life
   and indexes by integer age-in-hours thereafter (ADR-0288).

5. **No `math.Round(x*100)/100` pattern.** If you find yourself rounding a
   float64 to fix display, the value should have been integer all along.

## Alternatives considered

### Keep float64 with disciplined rounding

The legacy approach. Even with consistent rounding (`math.Round` at every
arithmetic step), associativity violations remain: `round(a + b) + round(c)` ≠
`round(a + round(b + c))` for `float64`. Rounding conventions are also easy to
forget in new code — the integer representation makes incorrect usage a type
error or obviously wrong magnitude.

### PostgreSQL NUMERIC / DECIMAL

Arbitrary-precision types eliminate representation noise but are 5–10× slower
than `int64` for arithmetic in Go (requires `big.Int` or database round-trips)
and use more storage. The engine's integer scales (cents, millicores, basis
points) provide sufficient precision without the overhead.

### Fixed-point library (e.g. shopspring/decimal)

Adds a dependency and runtime cost for arbitrary decimal arithmetic. The engine's
domains have well-defined scales (cents, millicores, KiB) that map naturally to
`int64` without a generic library. Simpler code, fewer allocations.

## Consequences

- **Deterministic aggregation.** Fleet savings totals are identical regardless of
  grouping order — `Sum(int64)` is associative and commutative.
- **No representation noise in API responses.** Values are formatted from integers
  with exact decimal conversion, never from float intermediates.
- **Type-level correctness.** Mixing cents and dollars, or millicores and cores,
  produces wrong-magnitude results that are immediately obvious in tests — unlike
  float64 where `0.007` and `7.0` are both plausible CPU costs.
- **Performance.** Integer hot paths are faster and more predictable than float64
  paths with transcendental functions (see ADR-0288, ADR-0291).
- **Boundary discipline required.** Every new integration point (CSV parser, Koku
  rate loader, API serializer) must convert at the boundary. This is explicit and
  testable, but requires developers to understand the scale conventions.
- **Overflow awareness.** `int64` has finite range. New formulas must verify that
  worst-case products fit within `9.2×10¹⁸`. In practice, the engine's scales
  leave 2+ orders of magnitude of headroom (ADR-0291).

## Related decisions

| ADR | Scope |
|-----|-------|
| [ADR-0047](0047-integer-cents-basis-points-millicores.md) | Original integer representation decision (money, CPU, ratios) |
| [ADR-0064](0064-money-amount-api-cents-internal.md) | `MoneyAmount` API boundary type |
| [ADR-0098](0098-csv-float-to-int64-parse-time.md) | CSV parse-time float → int64 conversion |
| [ADR-0280](0280-fixed-point-savings-migration-float-to-integer-cents.md) | Savings float → integer cents migration |
| [ADR-0288](0288-decay-weight-lookup-tables.md) | Decay weight lookup tables (eliminating `math.Exp` from hot path) |
| [ADR-0291](0291-integer-micro-cents-savings-computation.md) | Micro-cents savings computation library |

## References

- [`internal/engine/savings_int.go`](../../internal/engine/savings_int.go) — centralized integer savings math
- [`internal/money/format.go`](../../internal/money/format.go) — cents ↔ display conversion
- [`internal/engine/decay_table.go`](../../internal/engine/decay_table.go) — precomputed decay weight tables
- [Why the Native Engine Was Built](../../docs-site/architecture/motivation.md) — architectural rationale
