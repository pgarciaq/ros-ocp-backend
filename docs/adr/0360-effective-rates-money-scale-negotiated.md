# ADR-0360: The `effective_rates` money scale is negotiated per request, and ROS has no float bridge

## Status

Accepted (implements [#461](https://github.com/pgarciaq/ros-ocp-backend/issues/461))

Supersedes the rate-conversion clause of
[ADR-0291](0291-integer-micro-cents-savings-computation.md) ("Koku rates arrive
as `float64` dollars").

## Phase

Cross-repo contract — koku Masu ⇄ ros-ocp-backend cost ingestion (#461)

## Context

`effective_rates` is ROS's only rate source: the savings engine multiplies
pod/node/storage deltas by the cost model's prices for the cluster, and those
prices exist only in koku. The endpoint is internal (service-to-service,
`AllowAny` behind a NetworkPolicy) and predates the integer savings work.

Before #461 the payload mixed scales:

- `rates[]` — the cost model rows verbatim — became integer micro-cents under
  #467 (`a6c53c2b3`, `1 USD = 100_000_000`).
- `configured_rates`, the money columns of `namespace_aggregates`,
  `markup_pct` and the usage-hour columns were still JSON floats: dollars,
  dollars, percent, hours.

That split is the defect. ROS's savings math is `int64` micro-cents
(ADR-0291), so the float fields had to be quantized at the boundary by a
multiplier of 1e8. A quantization step is the one place where a producer-side
mistake is absorbed rather than detected: `0.007` and `700000` differ by eight
orders of magnitude, and both look like plausible savings once applied. When
the engine and the producer are released separately — koku in its own fork,
ROS in this repo, on-prem and SaaS rolling independently — "both sides agree on
the scale" is an assumption that rots between deployments, not a fact.

Two orderings are possible when either side upgrades first, and they cannot be
made symmetric by the producer alone: ROS may run against a Masu that answers
in floats, and Masu may answer a ROS that expects integers. The first is the
dangerous one, because a float payload decodes into a pliant reader.

## Decision

1. **The scale is a request parameter, not an assumption.**
   `GET /api/cost-management/v1/effective_rates/?…&money_scale=microcents`.
   koku validates it against `{dollars, microcents}` and returns **400** for
   anything else, so a typo is loud instead of silently the default.
2. **koku echoes the scale it actually used**, always, in a top-level
   `"money_scale"` field. The reader never infers the scale from value shapes.
3. **`microcents` means integers everywhere the payload carries a number that
   ROS multiplies by:**

   | Field group | `microcents` | `dollars` (default) |
   |---|---|---|
   | `configured_rates.*` | integer micro-cents per the metric's own unit | float dollars |
   | `namespace_aggregates` money (`cost_model_*`, `infrastructure_cost`, `distributed_cost`) | integer micro-cents | float dollars |
   | `namespace_aggregates` hours (`cpu_*_hours`, `mem_*_hours`) | integer **milli-hours**, names unchanged | float hours |
   | `markup_pct` | integer **basis points** (`10%` → `1000`) | float percent |
   | `rates[]` | integer micro-cents (unchanged by the parameter, #467) | integer micro-cents |

   Keeping the hour field names while changing their scale is deliberate: the
   names are already `*_hours`, and renaming them would fork every consumer for
   a unit change. The echo is what makes the reinterpretation safe.
4. **The default stays `dollars`.** koku still serves any reader that predates
   #461, including the deployed pairings. The float branch is compatibility,
   with a named migration end (below), not a second contract.
5. **ROS requires the echo and refuses every other scale by name.** A payload
   whose `money_scale` is not `microcents` — including a Masu that predates the
   parameter and therefore omits it — fails the fetch with
   `koku effective-rates answered money_scale="…"; this build requires
   "microcents" (the Koku/Masu build predates #461 or ignored the parameter)`.
   The integer field types are a second, independent guard: a payload that
   echoes `microcents` but still emits floats fails at `json.Decode` (the decode
   error names `#461` so the operator is not left guessing).
6. **There is no float bridge inside ROS.** A compatibility decoder that
   quantizes legacy float JSON once at the mapper was rejected — see
   alternatives. Refusing is the whole point: `costData = nil` →
   `NotifNoCostData` (**25**) → savings stay `$0`. A cluster with a stale Masu
   shows *unavailable* savings, never wrong ones.
7. **Deploy Masu before ROS.** Under this design that ordering is required, and
   the cost of ignoring it is the visible `NotifNoCostData` window, not
   mispriced data.

## Alternatives Considered

| Alternative | Why not |
|---|---|
| **A. Token-shape compat decoder in ROS** — accept either float or integer and quantize floats once at the mapper | Chosen against. Its failure mode is exactly the defect being fixed: silent 1e8 inflation when the shape detection is wrong, with no error to alert on. It also forces ROS-before-Masu ordering (the harder one to enforce, because Masu is the shared dependency) and leaves two decoders to keep honest. |
| **C. Big-bang: koku switches the default to `microcents`** | Breaks every existing reader at once with no rollback and no way to tell an old reader from a new one. The parameter gives a per-caller switch for the same code. |
| **D. Version the endpoint (`/effective_rates/v2/`)** | The scale is orthogonal to the resource identity: same cluster, same dates, same cost model, different unit. A new path would duplicate the NetworkPolicy, the deployment surfaces and the resolver for a change that a query parameter already expresses — and `rates[]` must stay micro-cents on *both* paths anyway. |
| **E. Infer the scale from value magnitude** (`if all values are integers → microcents`) | Heuristic with a real collision: `node_cost_per_month: 1000` is valid in both scales. Exactly the class of guess this ADR removes. |
| **F. Keep floats and quantize in ROS forever** | The 1e8 multiply is already the boundary; leaving it unverified means every rounding difference between producer and reader is unattributable. #461 exists because the conversion had no owner. |

## Consequences

- **`cost_model_gpu_cost` is deliberately not consumed (#905)**: koku sends
  it (SUM'd, money-scaled), ROS drops it at decode by decision. GPU money
  prices the GPU rate — which is missing, not misfiled (#942: a Tier-B GPU
  rate needs a GPU denominator column from koku; until then GPU savings
  price from configured `gpu_cost_per_month` or report zero). Folding it
  into the CPU/memory numerators would trade a conservative understatement
  for an overstatement — rejected. The exclusion follows the cost model's
  own attribution semantics (direct resource costs stay attributed; only
  unattributable overhead is distributed), and mirrors koku's resource-
  specific `wasted_cost` numerators. On OCP-on-cloud, cloud GPU instance
  spend reaches savings via `infrastructure_raw_cost` regardless.
- ROS and koku can be released in either order *once Masu-first has happened*;
  after that, a ROS rollback to a pre-#461 build against a microcents-capable
  Masu still works, because the default is unchanged.
- koku carries a float branch until every reader sends the parameter. Removing
  `dollars` is a separate, later change gated on the consumer list — not a
  cleanup to be done opportunistically.
- A stale Masu is now an operational event with a fingerprint: notification
  **25** with zero savings on every cluster of that Masu, and the scale string
  in the processor log. That is a diagnosable state; a wrong rate is not.
- The refusal's **25** does not clear itself. Measured on the lab: after the
  Masu upgrade and a successful recalc, the 796 affected rows carried restored
  savings **and** `25` (`{1,9,25}` beside `3,980` cents) — the append-only
  behaviour of [#881](https://github.com/pgarciaq/ros-ocp-backend/issues/881).
  Clearing it is a data operation (`array_remove`), so a mixed-version window
  leaves correct numbers wearing a "no cost data" warning until something
  removes the code.
- `markup_pct` is basis points only under `microcents`. Any future reader must
  read the echo before multiplying; there is no field name that says "basis
  points".
- The float→micro-cent converters in `librobne/types` (`RateMicroCentsPerMCHour`,
  `RateMicroCentsPerGiBHour`, `RateMicroCentsPerGiBMonth`,
  `RateMicroCentsPerDollarMonth`) are no longer on any production path. They
  remain public library API (third-party callers, existing tests); nothing in
  the ROS cost path calls them.
- `namespace_aggregates.cost_model_gpu_cost` is emitted by koku and still not
  decoded by ROS. Whether Tier-B namespace spend should include it (and on
  which denominator) is an open decision —
  [#905](https://github.com/pgarciaq/ros-ocp-backend/issues/905); the
  implementation, whichever way it goes, is #467's ROS half. Nothing here
  depends on the scale.

## Verification

| Behavior | Test | Mutation that must fail it |
|---|---|---|
| A `$0.007` CPU rate arrives as exactly `700000` µ¢, not `699999` | `EffectiveRatesMoneyScaleTest.test_microcents_configured_rates_are_integers` | rounding the cents instead of the micro-cents (`ROUND_HALF_UP` on the wrong quantum) |
| `10%` markup arrives as `1000` basis points, integer | `…test_microcents_markup_is_basis_points` | leaving `markup_pct` on the float branch |
| Aggregate money and hours split into their own integer scales | `…test_microcents_aggregates_split_money_from_hours` | converting hours with `MICRO_CENTS_PER_DOLLAR`, or money with `MILLI_HOURS_PER_HOUR` |
| The default is still `dollars` and says so | `…test_default_scale_is_dollars_and_is_echoed` | defaulting the parameter to `microcents` |
| `rates[]` is unchanged by the scale | `…test_rates_stay_micro_cents_under_both_scales` | applying the scale to `_parse_rates_list`'s output |
| An unknown scale is a 400 | `…test_unknown_money_scale_is_rejected` | falling back to the default on an unrecognized value |
| The response shape decodes with no loss, exact-int | `TestKokuEffectiveRatesContract` | any field reverting to `float64` |
| A missing or `dollars` echo is refused and names `#461` | `TestKokuEffectiveRatesContract_RejectsLegacyMoneyScale` | removing either the echo check or the decode guard |
| A `microcents` echo with float money is refused at decode | `TestKokuEffectiveRatesContract_RejectsFloatMoneyEvenWhenScaleEchoed` | relaxing the wire types to `float64` |
| Tier-B namespace savings are derived in integers from the aggregates | `TestIntegerSavingsMatchesFloatPath_*`, `internal/engine/savings_int_test.go` | reintroducing `EffectiveRateMicroCentsPerMCHour(float64, float64)` on the Tier-B path |

The koku suite ran as `masu.test.api.test_effective_rates` — 34 tests OK.
Mutation check: stashing the implementation fails 6 of the 8 scale tests for
the reason above; the two that still pass are the deliberate controls (explicit
`dollars` equals the default; `rates[]` unchanged).

### Live pairing, measured (2026-11-19)

The tables above prove each side in isolation. The pairing was then exercised
on the SNO lab (arm64), org `1234567`, cluster `cf09ed9d`, 796 recommendation
rows, both images built on the lab hypervisor from the landed commits — koku
`99462739e`, ros-ocp-backend `a0c83385` (containing `53c33270`), tag
`461-202611191058`. ROS was rolled **first**, on purpose, to test the failure
path before the happy one.

| Order executed | Observation | What it proves |
|---|---|---|
| new ROS + pre-#461 Masu (`branch-202611181014`), recalc of 796 rows | 796/796 lost savings and gained **25**; cluster total `7,840,210` → `0` cents; log: `…predates #461; this build only reads money_scale="microcents"` | the refusal is live, and fails safe — no row was priced at 10⁸× |
| Masu → `99462739e`, same recalc, ROS untouched | 796/796 savings restored; total back to **`7,840,210` cents** | the integer path reproduces the float path's total exactly on real data |
| hand computation from the same payload, namespace `cost-onprem` | `32,908,970,883 ÷ 459,941` = `71,550` µ¢/mC-h; `91,359,212,007 ÷ 459,941` = `198,632` µ¢/mC-h (infra + distributed); `10,658,223,469 × 1000 ÷ 769,263` = `13,855,110` µ¢/GiB-h; `celery-beat` 50→25 mC, 204,800→297,636 KiB, 720 h ⇒ `3,980,076,162` µ¢ ⇒ **`3,980` cents**, equal to the stored value | the persisted figure is reproducible from the payload by hand, not just "an integer" |
| payload shape under `?money_scale=microcents` | echo `microcents`; `$0.3` → `30,000,000`; `$200` → `20,000,000,000`; `3.645 h` → `3645`; `rates[]` byte-identical to the `dollars` response (4 rows) | the scale touches money (and hours) only |

One mechanism detail the tests did not predict: a real pre-#461 producer trips
the **decode** guard before the **echo** guard, because it sends
`markup_pct: 0.0` and no `money_scale` at all. Both guards name `#461`, so the
operator sees the same defect either way, but grepping the log for the echo
sentence alone misses this case.

Not covered by this run, and therefore still asserted: a ROS rollback to a
pre-#461 build against a microcents-capable Masu (the `dollars` default is
unchanged, so it should work, but it was not executed), and the per-request cost
of the extra decode pass. Evidence: ros-ocp-backend #461 comment `7007432745`.

## Cross-Repo Dependencies

- koku `koku/masu/api/effective_rates.py` — constants
  (`MICRO_CENTS_PER_DOLLAR`, `MILLI_HOURS_PER_HOUR`,
  `BASIS_POINTS_PER_PERCENT`), `_to_milli_hours` / `_to_basis_points`, the
  `microcents` flag threaded through `_get_cost_snapshot` and
  `_get_namespace_aggregates`, and the request validation/echo.
- koku `koku/masu/test/api/test_effective_rates.py` — `EffectiveRatesMoneyScaleTest`.
- koku `docs/architecture/ros-ocp-integration.md` — the parameter row and the
  scale table.
- ROS `internal/costdata/provider.go` — `MoneyScaleMicroCents`, the int64 wire
  types, the request parameter and the two guards.
- ROS `internal/costdata/{ratecard.go,configured_rates.go}` — copy-only mapper
  and int64 accessors.
- `librobne/types/savings_int.go` — `RateMicroCentsPerMCHourFromCoreHour` (the
  integer replacement for the float converter).
- Deployment: no configuration knob. The ordering is the mechanism — koku/Masu
  first, then ROS (see
  [`docs-site/architecture/cost-integration.md`](../../docs-site/architecture/cost-integration.md#money-scale-461)).

## Related Decisions

- [ADR-0291](0291-integer-micro-cents-savings-computation.md) — integer
  micro-cents in the savings math; its "convert once from `float64` dollars"
  clause is superseded here.
- [ADR-0047](0047-integer-cents-basis-points-millicores.md) — integer cents and
  basis points at the API boundary.
- [ADR-0303](0303-library-extraction-librobne.md) — one money scale from Masu
  HTTP through `RateCard` to `Apply*`.
- ADR-0327 — currency conversion is an API-time concern; this ADR changes the
  unit the numbers travel in, not the currency they are denominated in.
- #467 — `rates[]` passthrough (integer micro-cents on both scales).
