# #467 Koku effective_rates rates passthrough — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step. Implementation in `~/dev/koku/koku` (fork `pgarciaq`, branch `pgarciaq-rosocp-superpowers-phase17`, baseline `22cebac02`); centrally tracked in ros-ocp-backend #467. **The endpoint is fork-only** — `masu/api/effective_rates.py` exists on no upstream line (`project-koku/koku` main and `gitlab` main both lack it), so this is a fork-line change with no upstream review gate. koku-only; the ROS consumer is a separate, later issue.

**Goal:** the cluster cost snapshot stops being a lossy second copy of the cost model. It carries the cost model's rows as-is (tiered **or** tag), keeps the applied-spend aggregates that only this endpoint has, and never emits a `$0` scalar for a metric the model prices by tag.

**Mechanism (verified at `22cebac02`):**
1. `_parse_rates_list` (54-73) keeps `tiered_rates[0].value` per metric and drops `tag_rates` entirely; it seeds every metric with `{"infrastructure": 0.0, "supplementary": 0.0}` (67-68), so a tag-only metric surfaces as a fake zero.
2. `_get_configured_rates` (87-117) reads `cost_model.rates` directly — a parser that diverges from price lists.
3. `_get_namespace_aggregates` (120-155) has no GPU cost column, and the endpoint never passes a date into rate resolution.
4. **Point 4 is already available:** `CostModelDBAccessor(schema, provider_uuid, price_list_effective_on)` (`masu/database/cost_model_db_accessor.py:22`) applies the `disable_price_list` Unleash kill-switch in `__init__` and exposes `effective_rates` / `markup` / `distribution_info`. Using it is wiring, not invention.

**Fix:**
1. Resolve rates through `CostModelDBAccessor(schema, provider_uuid, price_list_effective_on=parsed_start)` and add a `rates` array to the response: each row as the cost model stores it (`metric`, `cost_type`, `name`/`custom_name`, `tiered_rates` **or** `tag_rates` with `tag_key` + `tag_values[]`). No per-metric dump as source of truth.
2. `configured_rates` stays as **compatibility** for the current ROS reader: flatten tiered-only rows; **omit** metrics that have only tag rates instead of emitting `0.0`. Today ROS resolves a missing key and a `0.0` identically, so this is not a reader-visible regression; it stops the endpoint from asserting "no rate" where tags hold one.
3. Aggregates: add `SUM(COALESCE(cost_model_gpu_cost, 0))` as observed spend (never a derived list price).
4. Money in the new `rates[]` field is **integer micro-cents** per the issue's point 7; `configured_rates` keeps its legacy float scale until #461 converts the whole payload. Document the two-scale window in the module docstring — it is the one sharp edge of landing #467 before #461.

**Recon still owed before the diff (premise checks, in this order):**
- Shape of `PriceList.rates` vs `CostModel.rates` (the accessor's `effective_rates` returns either `{}` or the list; confirm a tag-rate row survives the accessor path, since `price_list` explicitly skips `tag_key` rows and defers to `effective_rates`).
- The exact key names `CostModelSerializer` emits per rate row (`name` vs `custom_name`) — mirror them, do not invent.
- What "no price list covers `start_date`" does to a live response (`{}` → empty `rates` and empty `configured_rates`). That is documented PRD behaviour of the accessor, but it is a visible change for price-list users and must be stated in the issue comment, not discovered by a customer.

**Test matrix (`koku/masu/test/api/test_effective_rates.py`, `MasuTestCase` harness — needs the Postgres tenant DB):**
- RED: a cost model with a **tag_rates-only** metric (the GPU shape) → `rates[]` contains the tag row and `configured_rates` omits the metric. Baseline: no `rates` key, `0.0` scalar.
- RED: GPU spend in the summary → `namespace_aggregates.<ns>` carries `cost_model_gpu_cost`. Baseline: absent.
- Control: tiered-only model → `rates[]` row matches `configured_rates` value (micro-cents vs dollars explicitly asserted), so the two representations cannot silently disagree.
- Control: no cost model for the provider → `rates` empty/omitted, no fake `"USD"`, 200 preserved.
- Green-must-stay: #771's customer-scoped resolution tests, 400/404 paths, and the `disable_price_list` flag path.

**Out of scope:** public cost-models OpenAPI; `exchange_rate`/`user_currency` (#462); any ROS mapper change (#461's ROS half); MIG fraction arithmetic (ROS reads tags locally and applies `slice_count / max_slices`).

**Constraints:** additive to the ROS-visible contract; no drive-by refactors; commits wait for owner go-ahead. koku's working tree carries an unrelated `docker-compose.yml` edit and stray CSVs — leave them untouched and out of the commit.
