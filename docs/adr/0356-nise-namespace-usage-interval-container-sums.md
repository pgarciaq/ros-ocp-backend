# ADR-0356: Nise Namespace Usage Aggregates the Interval's Container Rows

## Status

Accepted — implemented in #773 (nise `nise/generators/ocp/ocp_generator.py`)

## Phase

Fixture data — nise OCP generator's `ocp_ros_namespace_usage` family

## Context

The operator (and nise's `ocp_ros_namespace_usage`) reports **time statistics of
the concurrent namespace sum**: for one interval, the namespace's CPU/memory
usage is the sum across the pods running then, and `max`/`min` describe that sum
over the interval. ROS ingestion stores those as the namespace digest, and the
namespace recommendation sizes from the observed usage (`internal/model/boxplot.go`
`AssembleNamespaceBoxplots`, `librobne/quota/recommend.go` for quota).

The generator did not produce that. `_aggregate_namespace_data` read the stored
per-pod template values of `ros_data` and then, per namespace:

- averaged usage across pods (`sum(avgs)/len(avgs)`),
- took the **single largest** pod for `max` and the smallest for `min`,
- ignored `start`/`end` entirely, so every fixture exported a perfectly flat
  namespace series no matter what the container rows said,
- computed `cpu_throttle_namespace_min` as `min()` over the per-pod **averages**,
  which is not a minimum of anything.

A namespace consumer cannot recover the missing total from the emitted row. Two
constant 0.5-core/1-GiB pods produced 0.5 cores/1 GiB instead of 1 core/2 GiB, so
every avg/max-based reading understated a multi-pod namespace by its pod count —
the dangerous direction for a headroom/visualization surface.

## Decision

1. **The namespace row is the per-interval aggregate of the container rows
   emitted for that interval.** The ROS pods pass arms an accumulator
   (`_ns_interval_sums`), adds each emitted row's summed columns while it emits,
   and the namespace pass writes one row per accumulated `(interval, namespace)`.
   The aggregation point is the final emitted row, after `by_hour` shaping and
   per-row randomization, so `namespace_avg == sum(container_avg)` holds by
   construction rather than by re-deriving values.
2. **One accumulation per pod per interval.** `_ros_pod_gpu_variants` emits one
   row per distinct `gpu_uuid`; summing every emitted row would multiply a
   multi-GPU pod's CPU/memory by its GPU count. Only the first variant
   accumulates, which matches the pre-fix pod count.
3. **`max`/`min` are sums of the container extremes and are documented as
   bounds.** Without sub-interval samples no other value is computable. The sum
   of maxima is a conservative upper bound on the within-interval namespace
   maximum (it can overstate when pods peak at different moments, never
   understate); the sum of minima is the matching lower bound. The issue's
   prohibition on "replacing max(sum over time) with arbitrary sums of
   independent maxima" is answered explicitly: this is not a time-series
   maximum, it is the only computable bound, and it errs high. Consumer
   evidence: quota recommendations never read `*_max` (`librobne/quota/recommend.go`
   sizes from `*_namespace_hard`/`*_namespace_used` plus request sums and
   headroom basis points); `*_max` feeds the namespace detail boxplot
   (`internal/model/boxplot.go`), and `*_min` is parsed by
   `librobne/csv/parse_namespace.go` and read by nothing.
4. **`cpu_throttle_namespace_min` becomes the sum of the container minima**,
   replacing `min()` over per-pod averages. Same rule as the other columns; the
   old expression was a category error, not a slower approximation.
5. **Requests, limits and quota used/hard are unchanged.** Requests and limits
   already summed container sums; the quota projection reads them exactly as
   before. `namespace_running_pods_*` / `namespace_total_pods_*` also stay as
   they were (all declared pods running) — pod-phase modelling is a separate
   concern, recorded as a non-goal in #773.
6. **Direct namespace-only generation falls back to base values.** Calling
   `generate_data("ocp_ros_namespace_usage")` without a prior pods pass has no
   emitted rows to aggregate; it resolves each pod's base value for the interval
   (`by_hour` applied) and sums, without randomization — per-row randomization
   draws from the global `random` stream and cannot be replayed. The CLI
   (`nise/report.py`) always runs the pods pass first on the same generator, and
   `ROS_OCP_REPORT_TYPE_TO_COLS` orders `ocp_ros_usage` before
   `ocp_ros_namespace_usage`, so the supported path has the accumulator.
7. **Namespace rows are emitted in pod-emission order per interval.** The old
   path iterated a `set` of namespaces, so row order changed with string hashing.
   Content, not order, is the contract; the emitted set is identical.

**Explicit non-goals** (recorded in #773, unchanged here): frozen pod counters,
`memory_rss_usage_namespace_avg` whole-byte rounding, and `mem_usage_gig`
`by_hour` shaping by the ROS pod path.

## Alternatives Considered

- **Keep recomputing from `ros_data` but sum instead of average.** Minimal
  diff, but leaves the flat-series defect (the interval is still ignored) and
  cannot reproduce randomized or `by_hour` values, so the namespace file would
  disagree with the container file it supposedly aggregates. Rejected: the
  contract is exactly that agreement.
- **Sum every emitted row.** Simplest accumulation, but multiplies multi-GPU
  pods by their device count. Rejected; dedupe per pod.
- **Memoize per `(pod, interval)` resolved values instead of accumulating.**
  The reconnaissance harness measured 25.7 MiB for the memo against 6.0 MiB for
  the accumulator on the large fixture (2000 pods × 100 namespaces × 96
  intervals), with no correctness difference. The memo figure was not
  re-measured after the implementation; the accumulator's 6.03 MiB was.
  Rejected on space.
- **An `array('d')` slot per column instead of a Python list.** Measured at
  311 B per entry against the list's 627 B on the same generator (2× smaller,
  ~86 MiB at month scale). Not adopted: it buys headroom only at fixture sizes
  where `report.py`'s own row buffering is two orders of magnitude larger, and
  it forces the pod counter through a float slot. It remains the cheap lever if
  month-scale, many-namespace fixtures ever become normal.
- **Sum a true within-interval maximum by emitting a sub-interval series.**
  Requires changing the CSV granularity and the consumer contract; out of scope
  for a fixture-data fix. The documented bound is the honest version of what the
  current schema can express.

## Consequences

- **Multi-pod namespaces now report their real totals.** Swept over every
  loadable ROS example fixture in-process (50 fixtures, 198 348 namespace
  rows), the new path satisfies `namespace == sum(containers)` for all 12 mapped
  usage/request/throttle columns and for the pod count in every row; the
  baseline fails those invariants on 16 fixtures. Row counts are identical
  (198 348 keys in both states) — no fixture gains or loses a namespace row.
- **Value changes are exactly the intended ones.** 16 of 50 fixtures change
  content, all in usage/`rss`/throttle columns; 15 because the mean became the
  sum (fixed/baseline ratio is exactly the pod count, 2× or 3×), and one
  (`ocp_report_w8_sleep_guest.yml`) because the namespace series now follows the
  container `by_hour` values instead of a stored placeholder. Requests, limits
  and quota values are unchanged in every fixture. The other 34 fixtures are
  identical on every mapped field.
- **The namespace pass no longer rescans pods; the accumulator costs bounded
  memory.** The old pass was O(namespaces × pods × intervals) — it walked every
  pod once per namespace per interval. The new one walks only the accumulated
  entries. Measured end to end on one month of quarter-hour intervals
  (2 976 intervals, 100 namespaces, 400 pods, 1 190 400 container rows,
  297 600 namespace rows), back to back on the same host:

  | | pre-fix | this change |
  |---|---|---|
  | pods pass | 17.8 s | 33.0 s |
  | namespace pass | 43.8 s | 13.9 s |
  | total | 61.6 s | 46.9 s (−24 %) |
  | extra memory | ~0 | 180.6 MiB |

  At the smaller benchmark (2 000 pods, 100 namespaces, 96 intervals) the
  namespace pass still falls from 1.20–2.13 s to 243–280 ms, but end-to-end wall
  clock was inconclusive on this host (one low-load run measured 1973 → 1611 ms;
  re-runs did not reproduce it), so this ADR claims only the month-scale figure.
  The memory cost is the accumulator: 180.6 MiB measured at 297 600
  `(interval, namespace)` entries — **636 B per entry** on the real generator
  (`tracemalloc`, stable across runs) versus ~0 before. An earlier
  "240 B / ~66 MiB" figure came from a synthetic structure holding a single
  value per entry and understated it by ~2.6×. Each entry is 16 accumulated
  numeric slots plus a pod counter, so the ~128 B of payload is dominated by
  Python object overhead. For scale, `report.py` already buffers emitted rows per
  report type up to `--file-row-limit` (default 100 000) before writing and
  clearing the list — a container row measured 2 056 B, so ~196 MiB per report
  type at the default limit — and the largest shipped example fixture needs
  32 724 entries (~21 MiB); a typical fixture needs under 1 MiB. At month scale
  the accumulator is therefore of the same order as a buffer the tool already
  holds per report type, not a new dominant cost. This is a space-for-time
  trade, stated here rather than assumed.
- **Consumer recommendations move only for namespaces.** `robne diff` between
  the pre-fix control (same container rows, namespace columns rewritten to the
  old mean/max/min semantics) and the fixed payload shows container
  recommendations unchanged, quota recommendations unchanged, and the namespace
  recommendation doubled for a two-pod namespace (544 → 1087 mc CPU request,
  1 085 276 → 2 170 553 KiB memory request). That is the issue's expected delta.

## Verification

- `tests/test_namespace_aggregation.py` adds 12 regression tests: sum-vs-mean,
  pod-count fan-out (1.0 → 1.5), order independence, sum-of-max/min, throttle
  min, multi-GPU dedupe, `by_hour` tracking, randomized interval agreement,
  multi-namespace isolation, both direct-generation fallbacks, and one CLI
  integration test that runs `ocp_create_report` and asserts the *written* CSVs
  agree (the path that proves the accumulator is armed and consumed). Verified
  by stashing only the implementation file and re-running the module: all 12
  fail in 0.5 s (872 assertion failures in one run, mostly one `subTest`
  failure per interval), each message naming its mechanism — `0.5 != 1.0`
  (sum-of-pods, the 2-pod fan-out point — so the 1.0 → 1.5 step never appears —
  the multi-GPU pod counted per device, and the CLI-written namespace CSV),
  `0.59395 != 1.13418` (sum-of-max), `1.1113 != 0.0002` (`by_hour` container
  value against the stored flat placeholder), and a set difference for namespace
  isolation and both direct-generation fallbacks.
- Full nise unit suite: 563 tests, OK.
- Fixture sweep over `examples/ros_ocp_e2e/` and `examples/ros_ocp_seeding/`:
  6 fixtures cannot be loaded (4 Jinja templates, 2 pre-existing `pvc`
  `AttributeError`s); the other 50 satisfy the invariant in the fixed state and
  16 violate it in the baseline.
