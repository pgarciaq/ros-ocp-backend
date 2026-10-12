# Tier-B narrowed build: node-pressure signal — Design (for greenlight)

> Spec: GH #681 (child of #677; lease observability closed NO in #680).
> Status: DESIGN ONLY — no code written. Mirrors the Tier-A flow: greenlight → subagent-driven execution with per-task review.

## Goal

Surface node-pressure conditions (Memory/Disk/PIDPressure) as cost-explorer
notifications on node recommendations, so consolidation advice never silently
targets pressured nodes.

## Non-goal correction vs #681 as filed

**B1 (mgmt-plane crash notifications) is already shipped by Tier A — do not rebuild it.**
Tier A's gate is namespace-agnostic: it fires on every labeled namespace,
and multicluster-engine/hypershift/hive/OCM namespaces were labeled 2026-10-11.
Any restart_count_sum ≥ 3 in those namespaces already withholds cost downsize
and emits code 86 today. Remaining B1 work is verification-only (Task 1 below),
not code. Lease-loss stays out per #680.

## Architecture (corrected during Task 2 — supersedes the node-CSV sketch below)

Pressure rides the **container CSV**, not the node CSV: recon proved the
backend never reads `cm-openshift-node-usage` for digests (node recs derive
from container rows via `nodeDayAccumulator`). Three `node_pressure_*`
container columns join `kube_node_status_condition` on(node) through the
CPU-requests carrier, reusing the full ingest pipeline end to end. Original
node-CSV sketch retained for history, struck through in intent.

## Architecture (original sketch — superseded, see above)

New `pressure_state` node CSV column (worst observed `kube_node_status_condition`
per node/day) → digest → node-plugin notification + consolidation guard.
Two repos, same pattern as Tier A (operator query+column, backend parse/digest/
migration, engine notify, contract/docs).

## Key design decisions (for greenlight)

1. **Signal: worst-pressure ordinal per node/day** (none < PID < Disk < Memory?
   order by actionability: MemoryPressure dominates — a pressured node's usage
   baseline is untrustworthy for consolidation, same rationale as Tier A).
   Exact ordinal and per-condition vs worst-of TBD against live series shapes
   (9 MemoryPressure series observed; Disk/PID presence unverified — Task 1).
2. **Surface: notification + consolidation guard** (mirror Tier A: do not recommend
   removing/consolidating onto pressured nodes; attach code, keep current).
3. **Scope: all nodes** (pressure is per-node; mgmt-plane vs workload split
   unnecessary — the code explains itself; HCP workers included).
4. **No env knob** (Tier-A zero-fallback pattern; threshold = any pressure
   observed in window — boolean, not tuned).
5. **Code: next free ≥ 87** (verify at build; 83–86 taken). Severity WARNING.

## Review Focus

- Pressure flapping within a day (condition true 1/96 samples): expect notification (any-observed semantics) — deliberate, state it.
- Nodes with no pressure series at all (older operators): expect absent column → parse 0 → no code, never reject.
- Consolidation recs that exclude a pressured node must still explain where its workloads go (no silent drop).

---

### Task 1: Verify live data presence (no code)

- Confirm `kube_node_status_condition` Disk/PIDPressure series exist in Thanos
  (MemoryPressure verified: 9 series, 2026-10-11).
- Confirm mgmt-plane container restart rows flow into ROS CSVs (Tier-A column
  live since 2026-10-11; spot-check one multicluster-engine row).
- **Go/no-go:** if Disk/PID absent, narrow scope to MemoryPressure-only and
  record the ruling; if mgmt-plane crash rows absent, file the ingestion gap
  instead of building on assumption.

### Task 2: Operator pressure query + column (repo: koku-metrics-operator)

- New query key `ros:node_pressure_state` (kube-state-metrics
  `kube_node_status_condition{condition=~'MemoryPressure|DiskPressure|PIDPressure',status='true'}`),
  `query{}` block + struct field + header in node CSV path (mirror the
  oom→restart precedent: QueryMap entry, value name, struct mapstructure tag,
  header position documented in-task).
- Tests: query-shape test (5-assertion style), header/row position tests,
  golden update. TDD + mutation per repo rule.

### Task 3: Backend parse + digest + migration (repo: ros-ocp-backend)

- Parse column (optional, absent→none), digest worst-ordinal per node/day,
  migration `000213` (next number verified at build; current max 000212).
- Helpers + types mirroring `RestartCountSum` plumbing. TDD + mutation
  (ordinal-vs-count confusion fixtures).

### Task 4: Engine notify + consolidation guard

- New WARNING code (next free ≥ 87, verified), notification on node recs when
  pressure observed in window; consolidation logic skips pressured nodes as
  targets (keep current, explain via code).
- Boundary tests (flap-once vs sustained), OOM-style coexistence not applicable;
  adversarial fixtures per repo rule.

### Task 5: Verify + contracts

- Fixture E2E (pressured vs clean nodes), `make test-short`, operator suite,
  `robne` runs, cross-repo report per `docs/agents/cross-repo-contracts.md`,
  docs-site/internal catalog rows + CHANGELOG + notification-code updates
  (learned from Tier A — not deferred this time), IQE constant check.

## Self-review

1. **Spec coverage:** #681 B1 → Task 1 verification (no code); B2 → Tasks 2–4;
   contracts/docs → Task 5 (expanded vs Tier A after the deferred-docs lesson).
2. **Placeholders:** none — every step names exact files/patterns; Task 1's
   scope-narrowing branch is an explicit ruling step, not a TODO.
3. **Type consistency:** pressure ordinal type (int) + helper + code + column
   named consistently on write; reviewer to enforce.
4. **Review Focus:** each line has an owning task above.
