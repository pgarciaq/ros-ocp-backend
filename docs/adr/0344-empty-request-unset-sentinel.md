# ADR-0344: Empty CSV request cells parse as the unset-zero sentinel — request-less containers ingest and get usage-derived recommendations

## Status

Accepted (design) — implements [Issue #697](https://github.com/pgarciaq/ros-ocp-backend/issues/697)
(root cause of the `open-cluster-management-agent-addon` zero-digest straggler
diagnosed on [#694](https://github.com/pgarciaq/ros-ocp-backend/issues/694)).
Owner-approved direction: when a user sets no requests/limits, the reason is
usually that they don't know what to set — so the recommendation must state
what to set, not go silent.

## Phase

Ingestion / Engine (container plugin; namespaces follow an existing precedent; no operator change).

## Context (plain English)

`librobne/csv/parse.go::parseRecord` parsed `cpu_request_container_avg` and
`memory_request_container_avg` **unguarded**: an empty cell errored in
`coreToMillicores` / `bytesToKiB`, and `ForEachRow` silently bumped the skip
counter. Any container without a CPU **or** memory request therefore never
became a digest row — it vanished from the store, the engine, every
recommendation surface, and the HCP scopes. Live demonstration: KMO ships 420
agent-addon rows per cycle; zero digest rows persist; the processor logged
`ParseCSVRows: skipped 6528–19431 malformed or invalid rows` per cycle.

The namespace parser had already solved the same problem differently: requests
are **optional** there (`parse_namespace.go` guards the cells; empty parses as
0; malformed non-empty values still skip the row), and its tests explicitly pin
`TestParseNamespaceRows_RequestlessRowsParseAsZero`. Request-less namespaces
recommend fine today — recs are computed from **usage** percentiles
(`RecommendCPUAndMemory` over window rows), not from the current request.

## Decision

1. The container parser adopts the namespace convention: request cells are
   **guard-if** — empty parses as the zero **unset sentinel** with usage
   preserved; **malformed non-empty values still error and skip the row**
   (validation is not weakened). Both CPU and memory requests get the
   symmetric treatment; usage/limit/throttle guards unchanged (empty usage
   keeps its zero-interpretation per the crash-loop rationale, ADR-0340).
2. "0 = unset, recommend from usage" becomes the shared cross-surface
   convention. The engine's consumer audit confirms every current-request
   consumer is already safe on 0: `ComputeVariation` returns 0 (`current == 0`
   guard), `ClassifyResource(0)` yields `optimized` — the same label
   request-less namespaces already show — the idle utilization branch requires
   `current > 0 && mem > 0` (no division by zero), savings return 0 for zero
   deltas, the crash-loop gate never downsizes on `current = 0`, and the HCP
   guardrail falls back to its absolute floor (100m / 128MiB) because
   `EffectiveFloor` takes `max(absolute, pct% of current)`. Upsize recs then
   produce negative "savings" — pre-existing engine behavior for every upsize,
   now additionally visible as the cost delta of adding a request.
3. No notification code is added in the fix that lands this ADR. A dedicated
   "no request set" code (88) would bind the DB seed
   (`notification_code_definitions`), the Go definitions map
   (`TestDefinitionsMatchDB`), the public catalog endpoint, and UI display — a
   cross-repo contract deferred until the product asks for it explicitly.
4. Consequence for the HCP workload tripwire: the raw-CSV scan existed because
   skipped request-less rows were blind to the parsed pin check. The fix
   removes that blind spot; the tripwire sees request-less workloads at the
   parsed layer, and previously invisible unpinned workload names will surface
   a live run as *new evidence to review*, not regressions.

## Alternatives Considered

### NULL plumbing end-to-end (the original #697 framing)
Requests become `*int64` through parser, digest aggregation, writers, and
readers. Rejected after the product direction locked: the read layer's
`COALESCE(cpu_request_p50_mc, 0)` already collapses NULL to 0 at the engine
boundary, so the distinction survives only if every reader also changes —
a wide diff chasing a distinction the engine cannot act on. The namespace
parser's shipped 0-sentinel offers the same user outcome with a contained
change. Known trade-off recorded: a genuinely explicit 0 request is
indistinguishable from unset (pathological in practice), and mixed windows dilute
request percentiles toward 0 — both already true of the namespace path today.

### Fix in the operator (emit 0 for missing requests)
Rejected — the CSV is contract-correct; empty cells are the honest
representation of "not set", and the operator must stay untouched
(receiver-side defect).

### Withhold recommendations for request-less containers
Rejected by owner decision — the whole point is to give users an indication of
what requests and limits to set. The engine already produces usage-derived
requests (floors apply; limits follow limit = request × 1.05, ADR-0009 style
multipliers).

## Consequences

- Request-less containers appear as digest rows and recommendations across
  container and HCP surfaces; `agent-addon` closes the #694 straggler on the
  next lab cycle after deploy.
- The skip counter keeps counting only genuinely malformed rows; the residual
  18–19k cycle spikes get their own accounting in a follow-up.
- Forward-only adoption: historical CSVs already ingested are not re-processed
  (backfill deferred as a separate decision; data is retained in S3).
