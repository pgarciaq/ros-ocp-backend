# ADR-0341: Node-pressure signal — notify and withhold consolidation on kubelet pressure

## Status

Accepted (design) — implements [Issue #681](https://github.com/pgarciaq/ros-ocp-backend/issues/681)
(Tier-B narrowed scope, owner greenlight 2026-10-12). Lease-loss observability
was ruled out in [#680](https://github.com/pgarciaq/ros-ocp-backend/issues/680)
(data absent at source); crash-loop coverage already ships via Tier A (#78).

## Phase

Engine / Algorithm (node plugin; cross-repo: operator queries + CSV columns).

## Context (plain English)

A node under Memory/Disk/PID pressure has untrustworthy usage baselines — yet
consolidation logic happily recommends removing neighboring nodes (pushing more
work onto the pressured one) or treats the pressured node's own low usage as
savings headroom. Pressure is a per-node kubelet fact, not a workload fact, so
it belongs on node recs, not container recs.

## Decision

Collect Memory/Disk/PIDPressure per container row (on(node) join via the
CPU-requests carrier — the backend rejects request-less rows, so the join
covers exactly the rows that survive ingest), digest the worst ordinal per
node/day (none 0 < PID 1 < Disk 2 < Memory 3), and when any pressure was
observed in the term window: keep current sizing, attach notification code
**87** (`NODE_PRESSURE`, WARNING), and exclude the node from consolidation
removal (`binaryNodeCountReduction` returns 0, covering single and fleet paths).

## Alternatives considered

- **Node-CSV columns** (first attempt, reverted before push): recon proved the
  backend never reads `cm-openshift-node-usage` for digests — node recs derive
  from container rows — so node-file columns would have been dead bytes.
- **Separate pressure CSV**: rejected — a new file needs new parse/routing
  plumbing; three columns on the existing container CSV reuse everything.
- **Per-condition thresholds/tuning**: rejected — any-observed boolean keeps
  the Tier-A zero-fallback pattern (no env knobs).

## Consequences

- `daily_node_digests.pressure_state` (migration 000213) + code 87 registry
  (migration 000214, catalog, Kruize allowlist).
- Flap-once days notify (deliberate: pressure observed is pressure observed).
- Operators without the new columns parse clean (absent → 0, never reject).
