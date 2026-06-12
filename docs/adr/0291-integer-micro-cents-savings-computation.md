# ADR-0291: Integer micro-cents savings computation

## Status

Accepted

## Phase

Performance (P1-1)

## Context

Monthly savings estimates were computed independently in seven engine modules
(`savings.go`, `node_savings.go`, `pvc_savings.go`, `vm_savings.go`,
`gpu_recommender.go`, `recommend_quota.go`, `recommend_cluster_quota.go`).
Each duplicated the same float64 pattern:

1. Convert millicores/KiB/bytes to cores/GiB via floating division
2. Multiply by dollar rates from Koku (`float64`)
3. Multiply by `730` hours/month
4. `math.Round(total*100)/100` before `money.USDToCents`

Floating-point intermediate steps are slower than integer math on modern CPUs
and accumulate rounding error when thousands of recommendations are aggregated
for fleet savings ([ADR-0280](0280-fixed-point-savings-migration-float-to-integer-cents.md),
[ADR-0047](0047-integer-cents-basis-points-millicores.md)).

The native engine performance audit (P1-1 / M3) identified this as the remaining
float64 hot path in the recommendation produce phase, after margin computation
moved to integer basis points ([`MarginScale = 10000`](../engine/margin_scaled.go)).

## Decision

Centralize savings math in `internal/engine/savings_int.go` using **micro-cents**
as the internal fixed-point unit:

- **Scale:** `MicroCentsPerDollar = 100_000_000` (8 decimal places below dollars;
  1 cent = 1_000_000 micro-cents)
- **Rate conversion at boundary:** Koku rates arrive as `float64` dollars.
  Convert once to `int64` micro-cent rates when loading (`RateMicroCentsPerMCHour`,
  `RateMicroCentsPerGiBHour`, `RateMicroCentsPerGiBMonth`, `RateMicroCentsPerDollarMonth`)
- **Hot path:** all deltas (millicores, KiB, bytes, vCPU, GiB) and savings
  products use signed `int64` arithmetic
- **Output conversion once:** `MicroCentsToCents` rounds to integer cents for DB
  persistence; `MicroCentsToDollars` for the few API paths that still expose
  `float64` USD (VM savings helper)

Shared helpers cover CPU, memory, storage, flat monthly, MIG fraction, and
quota-tighten savings. VM power-off scaling reuses `MarginScale` basis points
(`ScaleMicroCentsByBasisPoints`).

## Overflow analysis

Worst-case product from the audit:

`1_000_000 mc × 100_000_000 µ¢/mc-hr × 730 hr ≈ 7.3×10¹⁶`

This fits comfortably in `int64` (max ≈ 9.2×10¹⁸). Replica counts and namespace
aggregates in production stay orders of magnitude below this bound.

## Alternatives considered

### Keep per-module float64 math

Duplicates logic, slower, and drifts from the integer-cents storage model.

### Compute directly in cents (2 decimal places)

Insufficient precision when dividing namespace aggregate costs by usage hours
before multiplying by small millicore deltas. Micro-cents avoid an extra
rounding step mid-formula.

### `NUMERIC` / `big.Int`

Correct but slower and unnecessary given the overflow headroom of `int64`.

## Consequences

- Seven savings modules delegate to one tested integer library
- API and DB formats unchanged (`estimated_*_savings_cents` integer columns;
  `MoneyAmount` in API responses)
- Negative savings (under-provisioned recommendations) work naturally with signed
  `int64`; quota tighten clamps to zero
- New savings categories must use `savings_int.go` helpers, not ad-hoc float math

## Related decisions

- [ADR-0047](0047-integer-cents-basis-points-millicores.md): Integer cents at API boundary
- [ADR-0280](0280-fixed-point-savings-migration-float-to-integer-cents.md): Cents storage migration
- [ADR-0182](0182-monthly-savings-730-hours.md): 730 hours/month constant
- [ADR-0290](0290-max-daily-p95-for-idle-classification.md): Prior performance-audit integer migration

## References

- [`internal/engine/savings_int.go`](../../internal/engine/savings_int.go)
- [`internal/engine/savings_int_test.go`](../../internal/engine/savings_int_test.go)
- [`docs/performance/native-engine-audit-2026-06.md`](../performance/native-engine-audit-2026-06.md) (P1-1)
