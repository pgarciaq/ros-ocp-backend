# MachineSet Recommendations (Tier 2) — Implementation Spec

**Status:** Tier 1 aggregation **shipped**; Tier 2a customer demand **confirmed**, design **aligned**, implementation **blocked on KMO MachineSet snapshot/RBAC, collection state, coverage reporting, and retry-safe history identity**; Tier 2b **blocked on catalog**<br>
**Public doc:** [docs-site/planned-features/machineset-recommendations.md](../../docs-site/planned-features/machineset-recommendations.md)  
**Requirements:** [REQ-8c.4–8c.6](../archive/requirements.md)  
**Roadmap:** [machineset-recommendations.md](../../docs-site/planned-features/machineset-recommendations.md), [autoscaler-optimization.md](../../docs-site/planned-features/autoscaler-optimization.md)

---

## Summary

Tier 2 adds advisory **MachineSet node-count recommendations** in a
**`machineset` engine plugin** (Phase 3 Optimize), persists
**`machineset_recommendations`**, exposes **`GET .../machinesets/{name}`** with
history, and replaces runtime `GROUP BY` aggregation on the list endpoint.
Delivery is split into **Tier 2a** (no cloud catalog; requires the KMO
MachineSet snapshot/RBAC dependency) and
**Tier 2b** (catalog-driven instance family/size and cost comparison).

**Shipped today:** [`GetMachineSetRecommendations`](../../internal/api/handlers_machinesets.go)
aggregates `node_recommendations` by `machineset_name` — not the Tier 2 engine.

---

## Architecture overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Tier 1 (shipped)                                │
│  daily_node_digests ──► node plugin ──► node_recommendations            │
│                              │                                          │
│                              │ node_count_reduction (per node)          │
│                              │ notification 76 (NODE_FLEET_CONSOLIDATION)│
└──────────────────────────────┼──────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    Tier 2a — machineset plugin (Phase 3)                │
│                                                                         │
│  Input: MachineSet API snapshot + complete node recommendations         │
│         + daily_node_digests (capacity, utilization)                    │
│                                                                         │
│  Engine:                                                                │
│    1. Group by (org_id, cluster_uuid, machineset_name, term, engine)  │
│    2. Node-count engine (sum node_count_reduction)                     │
│    3. Confidence = min(member confidence_level)                       │
│    4. Heterogeneous fleet detection (capacity variance > 10%)           │
│    5. Planned notification candidates 88–90                           │
│                                                                         │
│  Output: UPSERT machineset_recommendations                              │
│          INSERT machineset_recommendation_history (on change)           │
└──────────────────────────────┼──────────────────────────────────────────┘
                               │
                               ▼ (Tier 2b only)
┌─────────────────────────────────────────────────────────────────────────┐
│                    Tier 2b — instance-type plugin + catalog             │
│                                                                         │
│  cloud_instance_catalog ──► smallest-fit / family migration             │
│                         ──► rec_instance_type, cost comparison          │
└─────────────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  API                                                                    │
│    GET .../machinesets          (list — table-backed after migration)   │
│    GET .../machinesets/{name}   (detail + history)                      │
│    CSV export, keyset pagination, filters, order_by                     │
└─────────────────────────────────────────────────────────────────────────┘
```

| Component | Tier | Package / file (planned) |
|-----------|------|--------------------------|
| Node-count engine | 2a | `internal/plugins/machineset/replica.go` |
| Heterogeneous detection | 2a | `internal/plugins/machineset/heterogeneous.go` |
| Plugin orchestration | 2a | `internal/plugins/machineset/plugin.go` |
| Persistence | 2a | `internal/engine/machineset_rec.go` |
| List handler (table-backed) | 2a | `internal/api/handlers_machinesets.go` |
| Detail handler | 2a | `internal/api/handlers_machineset_detail.go` |
| History writer/reader | 2a | `internal/engine/machineset_rec_history.go` |
| Instance type recommendation | 2b | `internal/plugins/instance-type/` |
| Catalog refresh | 2b | `internal/catalog/` |

---

## Tier 2a — No Catalog (Implementation Spec)

Tier 2a can ship independently of Tier 2b. It reuses Tier 1 fleet consolidation output
(`node_count_reduction` from [`applyInstanceTypeConsolidation`](../../librobne/node/recommend.go))
and does **not** require `cloud_instance_catalog`.

### MachineSet source and readiness gate

MachineSet recommendations require a timestamped snapshot of the
`machine.openshift.io/v1beta1` MachineSet API, collected by
`koku-metrics-operator`. Persist its namespace/name/UID and
`machineset_snapshot_at` with each recommendation/history snapshot. Use
`.spec.replicas` as the desired node-count
baseline; keep `.status.replicas` and `.status.availableReplicas` as distinct
observed/availability signals. Include MachineSet namespace, name, UID, and
collection time. The standard MAO status metrics may be used for history only
after their availability is verified; do not assume a standard
`machine_api_machine_set_spec_replicas` Prometheus series exists.

The KMO data path must add read-only `list` permission only for `machines`
and `machinesets` in `machine.openshift.io` in both its RBAC source and OLM
permissions. Scope API list calls to `openshift-machine-api`; the current
supported IPI scope uses that namespace. The collector uses list operations;
do not grant `get` unless an implementation adds individual-object reads. If
OLM cannot express a namespace-scoped binding, resolve and review the
least-privilege alternative before implementation. The existing best-effort
Machine-to-MachineSet lookup does not have the list permission in checked-in
manifests and can otherwise return an empty mapping.

The KMO report must carry a **cluster-level collection state** even when there
are no MachineSet rows: `available`, `not_applicable` (API/resource absent), or
`incomplete` (read/authorization/coverage failure). Do not treat an incomplete
collection as a successful empty snapshot. Serialize an allowlisted projection
only: MachineSet identity/counts/timestamp and Machine node reference/owner
mapping. Do not export full Machine or MachineSet `providerSpec`, user-data, or
credential references to ROS.

The list API must expose per-cluster collection state in list metadata, keyed
by `cluster_uuid`, so an empty `data` array distinguishes no eligible
recommendation from unsupported/incomplete collection. An unsupported or
incomplete cluster must not suppress valid MachineSet rows from other clusters.
Pin the exact JSON property in OpenAPI and API contract tests before
implementation.

Emit an actionable target only when desired count, observed Machine count,
available count, and distinct member-node inventory agree, and every member
has a fresh eligible node recommendation for the requested term/engine. If
the Machine API is absent, mark MachineSet recommendations not applicable;
if reads fail or counts/coverage disagree, mark the MachineSet data
unavailable/incomplete and withhold its target. Node-level recommendations
continue. Never infer membership from `instance_type` or treat missing rows
as a successful zero-reduction result.

The current documented support scope remains IPI until other topologies are
compatibility-tested. UPI is an installation method, not proof that the
Machine API is present or absent. Bare Metal Operator (`BareMetalHost`)
inventory can describe physical-host readiness and hardware, but it is not a
fallback source for MachineSet membership or desired count.

### 1. Replica count engine

#### Inputs

| Source | Fields used |
|--------|-------------|
| `node_recommendations` | `node`, `machineset_name`, `cluster_uuid`, `term`, `engine`, `node_count_reduction`, `cpu_util_p95`, `mem_util_p95`, `confidence_level`, `data_days`, `estimated_savings_cents`, `classification`, `notification_codes`, `instance_type` |
| `daily_node_digests` | `node_capacity_cpu_cores`, `node_capacity_memory_bytes` (latest row per node in term window) |
| MachineSet API snapshot | namespace/name/UID, `.spec.replicas`, `.status.replicas`, `.status.availableReplicas`, collection timestamp |

Scope: all nodes where `machineset_name IS NOT NULL AND BTRIM(machineset_name) <> ''`
for a given `(org_id, cluster_uuid, term, engine='cost')`.

#### Outputs

| Field | Type | Description |
|-------|------|-------------|
| `current_node_count` | `int` | Distinct current Nodes linked to the MachineSet; must agree with the stable API snapshot |
| `desired_node_count` | `int` | MachineSet `.spec.replicas`, the currently configured desired Machine/node count |
| `observed_machine_count` | `int` | MachineSet `.status.replicas`; counts observed Machine objects |
| `available_machine_count` | `int` | MachineSet `.status.availableReplicas`; counts available Machine objects |
| `recommended_node_count` | `int` | Recommended target for MachineSet `.spec.replicas` |
| `excess_nodes` | `int` | `current_node_count - recommended_node_count` (≥ 0) |

#### Algorithm

```
desired_node_count = MachineSet.spec.replicas
observed_machine_count = MachineSet.status.replicas
available_machine_count = MachineSet.status.availableReplicas
current_node_count = COUNT(DISTINCT member Node linked from Machines)

if desired_node_count != observed_machine_count
   or observed_machine_count != available_machine_count
   or available_machine_count != current_node_count
   or any member lacks fresh eligible node recommendation:
    withhold target; mark MachineSet data unavailable/incomplete

excess           = SUM(node_count_reduction) across member nodes
                    -- only nodes with node_count_reduction > 0 contribute
recommended_raw  = current_node_count - excess
recommended_node_count = MAX(recommended_raw, ROS_MIN_MACHINESET_REPLICAS)
```

**Floor:** `ROS_MIN_MACHINESET_REPLICAS` defaults to **1** for Tier 2a
(never recommend zero nodes). This floor does **not** guarantee HA; availability
depends on topology and workloads. The engine MUST NOT emit
`recommended_node_count = 0`.

**No-reduction case:** When `excess = 0` (all member nodes have
`node_count_reduction = 0` — healthy/utilized, or consolidation blocked by
pod headroom gate), then `recommended_node_count = current_node_count`. Emit
planned notification **90** (`MACHINESET_NO_REDUCTION_RECOMMENDED`) only when
the data gate passes. This means no reduction is recommended under the current
model; it does not claim the MachineSet is globally “optimal.”

**Scale-down case:** When `recommended_node_count < current_node_count`, emit
planned notification **89** (`MACHINESET_SCALE_DOWN_RECOMMENDED`).

#### Relationship to Tier 1 list API (today)

The shipped aggregation in [`handlers_machinesets.go`](../../internal/api/handlers_machinesets.go)
already implements this math at query time:

```sql
COUNT(DISTINCT nr.node) AS current_node_count,
COALESCE(SUM(nr.node_count_reduction), 0) AS excess_nodes,
-- recommended_node_count = current_node_count - excess_nodes (client-side)
```

The shipped Tier 1 `current_node_count` is only the number of nodes with
recommendation rows, not authoritative MachineSet membership. Tier 2a
deliberately changes that pre-release field's meaning to verified linked Node
inventory and adds distinct desired/observed/available counts. Update IQE and
other consumers before public launch. Tier 2a persists the same consolidation
math only after validating the API snapshot and complete member coverage;
list/detail/history do not recompute it from incomplete rows.

#### Pseudocode (Go)

```go
func computeNodeCountRecommendation(snapshot MachineSetSnapshot, members []NodeRec) NodeCountResult {
    currentNodes := len(members)
    // Withhold the target unless API counts and the linked member-node inventory
    // agree and every member has a fresh eligible node recommendation.
    if snapshot.DesiredNodeCount != snapshot.ObservedMachineCount ||
        snapshot.ObservedMachineCount != snapshot.AvailableMachineCount ||
       snapshot.AvailableMachineCount != currentNodes || !allMembersCovered(members) {
        return NodeCountResult{DataStatus: "incomplete"}
    }
    excess := 0
    for _, n := range members {
        if n.NodeCountReduction > 0 {
            excess += n.NodeCountReduction
        }
    }
    recommendedNodes := currentNodes - excess
    minNodes := cfg.MinMachineSetReplicas // default 1; this is not an HA guarantee
    if recommendedNodes < minNodes {
        recommendedNodes = minNodes
    }
    return NodeCountResult{
        DataStatus:           "complete",
        CurrentNodeCount:     currentNodes,
        DesiredNodeCount:     snapshot.DesiredNodeCount,
        ObservedMachineCount: snapshot.ObservedMachineCount,
        AvailableMachineCount: snapshot.AvailableMachineCount,
        RecommendedNodeCount: recommendedNodes,
        ExcessNodes:          currentNodes - recommendedNodes,
    }
}
```

#### Savings rollup

```
estimated_savings_cents = SUM(member.estimated_savings_cents)
```

Only nodes with `node_count_reduction > 0` should have non-zero savings in
Tier 1; the sum matches the list API today.

#### Utilization aggregates

| Field | Formula |
|-------|---------|
| `avg_cpu_utilization_p95` | `AVG(cpu_util_p95)` across members (unweighted; matches Tier 1 list) |
| `avg_memory_utilization_p95` | `AVG(mem_util_p95)` across members |
| `data_days` | `MIN(data_days)` across members |

---

### 2. Database schema — `machineset_recommendations`

Migration: assign the next migration number when implementation begins; no
MachineSet recommendation migration exists yet.

```sql
CREATE TABLE IF NOT EXISTS machineset_recommendations (
    id                          BIGSERIAL PRIMARY KEY,
    org_id                      TEXT NOT NULL,
    cluster_uuid                UUID NOT NULL,
    machineset_namespace        TEXT NOT NULL,
    machineset_name             TEXT NOT NULL,
    machineset_uid              TEXT NOT NULL,
    machineset_snapshot_at      TIMESTAMPTZ NOT NULL,
    recommendation_fingerprint TEXT NOT NULL,
    term                        TEXT NOT NULL DEFAULT 'medium_term',
    engine                      TEXT NOT NULL DEFAULT 'cost',
    current_node_count           INT NOT NULL,
    desired_node_count           INT NOT NULL,
    observed_machine_count       INT NOT NULL,
    available_machine_count      INT NOT NULL,
    recommended_node_count      INT NOT NULL,
    instance_type               TEXT,          -- from node labels; NULL for bare-metal
    estimated_savings_cents     BIGINT NOT NULL DEFAULT 0,
    currency                    TEXT NOT NULL DEFAULT 'USD',
    confidence_level            REAL NOT NULL DEFAULT 0,
    heterogeneous               BOOLEAN NOT NULL DEFAULT false,
    notification_codes          SMALLINT[] NOT NULL DEFAULT '{}',
    member_nodes                TEXT[] NOT NULL DEFAULT '{}',
    avg_cpu_utilization_p95     REAL NOT NULL DEFAULT 0,
    avg_memory_utilization_p95  REAL NOT NULL DEFAULT 0,
    data_days                   INT NOT NULL DEFAULT 0,
    last_reported               TIMESTAMPTZ,   -- max(member last_reported)
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT machineset_recommendations_unique
        UNIQUE (org_id, cluster_uuid, machineset_namespace, machineset_name, term, engine),
    CONSTRAINT machineset_recommendation_counts_nonnegative
        CHECK (current_node_count >= 0 AND desired_node_count >= 0 AND
               observed_machine_count >= 0 AND available_machine_count >= 0 AND
               recommended_node_count >= 1)
);

CREATE INDEX IF NOT EXISTS idx_machineset_recs_org_cluster
    ON machineset_recommendations (org_id, cluster_uuid);

CREATE INDEX IF NOT EXISTS idx_machineset_recs_savings
    ON machineset_recommendations (org_id, estimated_savings_cents DESC);

CREATE INDEX IF NOT EXISTS idx_machineset_recs_name
    ON machineset_recommendations (org_id, machineset_namespace, machineset_name);
```

| Column | Population rule |
|--------|-----------------|
| `instance_type` | `MODE()` or `MAX` of non-empty member `instance_type`; empty string → `NULL` |
| `currency` | From org/cluster cost integration (`resolveListCurrency`) |
| `member_nodes` | Sorted `array_agg(DISTINCT node)` |
| `last_reported` | `MAX(updated_at)` from member `node_recommendations` |
| `term` / `engine` | Same dimensions as `node_recommendations`; one row per term+engine |
| `machineset_snapshot_at` | Source timestamp of the MachineSet API snapshot used by the recommendation |
| `machineset_uid` | Kubernetes UID; detects deletion/recreation under the same name |
| `recommendation_fingerprint` | SHA-256 hex of canonical result fields that trigger history changes; excludes recording timestamps |

No foreign key points from these recommendation snapshots to live Machine or
MachineSet objects: upstream objects can be deleted while recommendation
history remains. Tenant/cluster scoping is enforced in the service query.

**Tier 2b columns (add in separate migration when catalog ships):**

```sql
ALTER TABLE machineset_recommendations
    ADD COLUMN recommended_instance_type TEXT,
    ADD COLUMN instance_type_reason      TEXT,
    ADD COLUMN current_monthly_cost_cents BIGINT,
    ADD COLUMN recommended_monthly_cost_cents BIGINT;
```

Go model: extend [`internal/model/machineset_recommendation.go`](../../internal/model/machineset_recommendation.go).

#### Upsert cadence

Run after `RecommendNodes` completes for a cluster (same batch as node recalc).
Register plugin in [`internal/plugins/plugins.go`](../../internal/plugins/plugins.go)
Phase 3 Optimize group.

```sql
INSERT INTO machineset_recommendations (...)
ON CONFLICT (org_id, cluster_uuid, machineset_namespace, machineset_name, term, engine)
DO UPDATE SET
    machineset_uid = EXCLUDED.machineset_uid,
    machineset_snapshot_at = EXCLUDED.machineset_snapshot_at,
    recommendation_fingerprint = EXCLUDED.recommendation_fingerprint,
    current_node_count = EXCLUDED.current_node_count,
    desired_node_count = EXCLUDED.desired_node_count,
    observed_machine_count = EXCLUDED.observed_machine_count,
    available_machine_count = EXCLUDED.available_machine_count,
    recommended_node_count = EXCLUDED.recommended_node_count,
    ...
    updated_at = NOW();
```

Delete stale rows only after a complete successful snapshot for that cluster
confirms a MachineSet has zero members. Never sweep rows after an unsupported,
failed, stale, or incomplete collection.

---

### 3. Detail endpoint

```
GET /api/cost-management/v1/recommendations/openshift/machinesets/{machineset_name}
```

#### Path parameters

| Param | Required | Notes |
|-------|----------|-------|
| `machineset_name` | Yes | URL-encoded; exact match |

#### Query parameters

| Param | Required | Default | Notes |
|-------|----------|---------|-------|
| `cluster_uuid` | **Yes** when name is ambiguous | — | Required if the org has the same `machineset_name` in multiple clusters. Return `400` with message if omitted and ambiguous. |
| `filter[term]` / `term` | No | `medium_term` | `short_term`, `medium_term`, `long_term` |
| `filter[engine]` / `engine` | No | `cost` | `cost` or `performance` |

#### Response shape (JSON)

Mirror node detail (`GET .../nodes/{node}`) patterns:

```json
{
  "machineset_name": "worker-us-east-1a",
  "machineset_namespace": "openshift-machine-api",
  "machineset_uid": "5b1a8c22-0837-4b61-82b0-ef6d3b842c79",
  "cluster_uuid": "02059694-68ab-4d58-8809-de1e91f1d0e5",
  "cluster_alias": "production",
  "term": "medium_term",
  "engine": "cost",
  "current_node_count": 5,
  "desired_node_count": 5,
  "observed_machine_count": 5,
  "available_machine_count": 5,
  "recommended_node_count": 3,
  "excess_nodes": 2,
  "instance_type": "m5.4xlarge",
  "estimated_monthly_savings": { "value": "1234.56", "unit": "USD" },
  "currency": "USD",
  "confidence_level": 0.72,
  "heterogeneous": false,
  "avg_cpu_utilization_p95": 0.34,
  "avg_memory_utilization_p95": 0.41,
  "data_days": 18,
  "last_reported": "2026-05-28T14:00:00Z",
  "machineset_snapshot_at": "2026-05-28T14:00:00Z",
  "notifications": {
      "89": { "severity": "INFO", "description": "..." }
  },
  "member_nodes": [
    {
      "node": "worker-0",
      "cpu_util_p95": 0.32,
      "mem_util_p95": 0.38,
      "node_count_reduction": 1,
      "classification": "underutilized",
      "confidence_level": 0.85,
      "estimated_monthly_savings": { "value": "617.28", "unit": "USD" }
    }
  ],
  "history": [
    {
      "recorded_at": "2026-05-21T06:00:00Z",
      "machineset_namespace": "openshift-machine-api",
      "machineset_uid": "5b1a8c22-0837-4b61-82b0-ef6d3b842c79",
      "machineset_snapshot_at": "2026-05-21T06:00:00Z",
      "current_node_count": 5,
      "desired_node_count": 5,
      "observed_machine_count": 5,
      "available_machine_count": 5,
      "recommended_node_count": 4,
      "estimated_savings_cents": 95000
    }
  ]
}
```

#### Error responses

| Status | Condition |
|--------|-----------|
| `400` | Missing `cluster_uuid` when ambiguous; invalid `term` |
| `404` | No row for `(org_id, cluster_uuid, machineset_name, term, engine)` |
| `424` | RBAC: cluster not in allowed set (Koku pattern) |

Detail endpoint: **no pagination** (single resource).

---

### 4. History — `machineset_recommendation_history`

Follow [`vm_recommendation_history`](../../migrations/000099_vm_recommendation_history.up.sql)
and `quota_recommendation_history` patterns.

Create this table in the same numbered migration as
`machineset_recommendations`; do not leave a placeholder migration number.

```sql
CREATE TABLE IF NOT EXISTS machineset_recommendation_history (
    id                      BIGSERIAL PRIMARY KEY,
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    machineset_namespace    TEXT NOT NULL,
    machineset_name         TEXT NOT NULL,
    machineset_uid          TEXT NOT NULL,
    term                    TEXT NOT NULL,
    engine                  TEXT NOT NULL,
    current_node_count      INT NOT NULL,
    desired_node_count      INT NOT NULL,
    observed_machine_count  INT NOT NULL,
    available_machine_count INT NOT NULL,
    recommended_node_count  INT NOT NULL,
    machineset_snapshot_at  TIMESTAMPTZ NOT NULL,
    recommendation_fingerprint TEXT NOT NULL,
    estimated_savings_cents BIGINT NOT NULL DEFAULT 0,
    heterogeneous           BOOLEAN NOT NULL DEFAULT false,
    notification_codes      SMALLINT[] NOT NULL DEFAULT '{}',
    recorded_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT machineset_history_counts_nonnegative
        CHECK (current_node_count >= 0 AND desired_node_count >= 0 AND
               observed_machine_count >= 0 AND available_machine_count >= 0 AND
               recommended_node_count >= 1)
);

CREATE INDEX IF NOT EXISTS idx_machineset_rec_history_lookup
    ON machineset_recommendation_history (
        org_id, cluster_uuid, machineset_namespace, machineset_name, term, engine, recorded_at DESC
    );

CREATE UNIQUE INDEX IF NOT EXISTS idx_machineset_history_idempotency
    ON machineset_recommendation_history (
        org_id, cluster_uuid, machineset_namespace, machineset_name, term, engine,
        machineset_uid, machineset_snapshot_at, recommendation_fingerprint
    );
```

#### Write policy

History insertion must be retry-safe. `recorded_at DEFAULT NOW()` alone is not
an idempotency key. Use the source snapshot identity and a deterministic
recommendation fingerprint so replaying the same input does not duplicate a
history point, while a changed recommendation can be recorded. Pin this with
an integration test before the migration is numbered.

Insert a history row when **any** of these change vs. the previous snapshot:

- `recommended_node_count`
- `current_node_count`
- `desired_node_count`
- `observed_machine_count`
- `available_machine_count`
- `estimated_savings_cents` (by more than 5% relative, to avoid noise)
- `heterogeneous` flag
- `notification_codes` set (order-insensitive compare)

Also insert on first creation. Cap detail query at **30** rows (match quota detail).

Retention: housekeeper deletes rows older than `ROS_HISTORY_RETENTION_DAYS`
(default 90), same as other history tables.

---

### 5. MachineSet-level confidence

```
confidence_level = MIN(member.confidence_level) for all member nodes
```

Rationale: if **any** member node has low confidence, the MachineSet
recommendation is uncertain. A single new node with `data_days = 2` drags
the fleet confidence down.

Additionally surface `data_days = MIN(member.data_days)` on the row for UI
“days of data” display.

When `confidence_level < ROS_LOW_CONFIDENCE_THRESHOLD` (default 0.5), also
append notification **1** (`LOW_CONFIDENCE`) to `notification_codes` if not
already present from a member node aggregate.

---

### 6. Heterogeneous fleet detection

Compare capacity across member nodes using digest fields
`node_capacity_cpu_cores` and `node_capacity_memory_bytes` (from
`daily_node_digests`, latest bucket in term window).

#### Algorithm

```
cpu_values  = [capacity_cpu_cores for each member]
mem_values  = [capacity_memory_bytes for each member]

cpu_variance_pct = (MAX(cpu_values) - MIN(cpu_values)) / MAX(cpu_values) * 100
mem_variance_pct = (MAX(mem_values) - MIN(mem_values)) / MAX(mem_values) * 100

heterogeneous = (cpu_variance_pct > THRESHOLD) OR (mem_variance_pct > THRESHOLD)
```

**Threshold:** `ROS_MACHINESET_HETEROGENEITY_THRESHOLD_PCT` default **10**.

When `heterogeneous = true`:

- Set column `heterogeneous = true`
- Append planned notification candidate **88** (`MACHINESET_HETEROGENEOUS_CAPACITY`)
- Do **not** block replica recommendation; warn only

Edge cases:

| Case | Behavior |
|------|----------|
| Single-node MachineSet | `heterogeneous = false` (no comparison) |
| Missing capacity on a member | Skip that node for variance; if < 2 valid capacities, `heterogeneous = false` |
| Bare metal (no `instance_type`) | Heterogeneity still detected from Prometheus capacity |

---

### 7. Notification codes (Tier 2a — planned candidates)

Codes 88–90 are candidate IDs, not registered definitions. Before implementation,
verify uniqueness across backend, UI, IQE, operator, and API-cheatsheet consumers;
then add definitions/migration and update those consumers together.

| Code | Constant | Severity | When |
|------|----------|----------|------|
| **76** | `NODE_FLEET_CONSOLIDATION` | INFO | **Node** rows only (Tier 1, shipped). Not duplicated on MachineSet rows. |
| **88** | `MACHINESET_HETEROGENEOUS_CAPACITY` | WARNING | Member nodes differ in CPU or memory capacity by > 10% |
| **89** | `MACHINESET_SCALE_DOWN_RECOMMENDED` | INFO | `recommended_node_count < current_node_count` |
| **90** | `MACHINESET_NO_REDUCTION_RECOMMENDED` | INFO | `recommended_node_count = current_node_count` with complete eligible data |

Code **4** (PDB caveat) remains a separate notification-only decision; PDB and
placement simulation are not Tier 2a gates. Codes **23** and **24** remain
catalog-related Tier 2b notifications.

#### `recommendation_type` filter mapping

| Filter value | SQL predicate |
|--------------|---------------|
| `scale_down` | `recommended_node_count < current_node_count` |
| `no_reduction` | `recommended_node_count = current_node_count` |
| `heterogeneous_warning` | `heterogeneous = true` OR `88 = ANY(notification_codes)` |

---

### 8. List endpoint — keyset pagination (shipped)

Already implemented in [`handlers_machineset_pagination.go`](../../internal/api/handlers_machineset_pagination.go).

| Param | Behavior |
|-------|----------|
| `limit` | Default 10, max `listoptions.MaxLimit` |
| `offset` | Offset pagination (legacy) |
| `after` | Keyset cursor; sort key `estimated_savings DESC, machineset_name ASC, cluster_uuid ASC` |

**Tier 2a migration:** switch data source from runtime `GROUP BY` to
`machineset_recommendations` table. Keep identical response JSON shape for
the pre-release API. Customer-facing count fields use node terminology:
`current_node_count`, `desired_node_count`, and `recommended_node_count`.
Keep `observed_machine_count` and `available_machine_count` explicitly named
as Machine status counts. The desired field represents the MachineSet
`.spec.replicas` value, a desired Machine/node count rather than
application-pod replicas.

Detail endpoint: no pagination.

---

### 9. CSV export (shipped)

`Accept: text/csv` or `?format=csv` on list endpoint.
[`generateMachineSetRecCSV`](../../internal/api/handlers_machinesets.go) — no
changes required for Tier 2a beyond reading from the persisted table.

---

### 10. Filters (Tier 2a — extend list endpoint)

| Filter | Param | Match |
|--------|-------|-------|
| Cluster | `filter[cluster]` or `cluster_uuid` | Exact UUID |
| MachineSet name | `filter[machineset_name]` | Exact or `*` wildcard → `ILIKE` |
| Recommendation type | `filter[recommendation_type]` | `scale_down`, `no_reduction`, `heterogeneous_warning` |
| Term | `filter[term]` or `term` | `short_term`, `medium_term`, `long_term` |

RBAC: same cluster/node scoping as today in `handlers_machinesets.go`.

---

### 11. `order_by` (Tier 2a — new)

Query param: `order_by` (comma-separated; prefix `-` for DESC).

| Value | Default direction | SQL column |
|-------|-------------------|------------|
| `estimated_savings` | **DESC** (default) | `estimated_savings_cents` |
| `machineset_name` | ASC | `machineset_name` |
| `recommended_node_count` | ASC | `recommended_node_count` |
| `current_node_count` | DESC | `current_node_count` |

Keyset cursor must encode the active sort column when `order_by` is not the
default (extend `MachineSetCursor`).

---

### Tier 2a — Acceptance criteria

| # | Criterion | Verification |
|---|-----------|--------------|
| AC-1 | Node-count formula matches Tier 1 consolidation math when counts are stable and coverage is complete | IQE: same `recommended_node_count` as `current_node_count - excess` for fixture cluster |
| AC-2 | No target is emitted when desired, observed, available, member-node, or recommendation-coverage counts disagree | API/ingestion tests for mismatch, stale, and missing-member cases |
| AC-3 | Floor never below `ROS_MIN_MACHINESET_REPLICAS` (default 1); this does not guarantee HA | Unit test: excess > current still returns ≥ 1 |
| AC-4 | `machineset_recommendations` upserted on recalc | Integration: row exists after `RecommendNodes` batch |
| AC-5 | History writes are retry-safe by source snapshot identity and recommendation fingerprint | Integration: replay same input without duplicate; changed output fingerprint records the new recommendation |
| AC-6 | Detail returns member nodes + 30-day history | API test: `GET .../machinesets/{name}?cluster_uuid=...` |
| AC-7 | Confidence = min(member) | Unit test with mixed confidence members |
| AC-8 | Heterogeneous detection at >10% variance | Unit test: mixed capacities |
| AC-9 | Candidate notifications 88/89/90 emitted after uniqueness is verified | Engine/catalog test matrix |
| AC-10 | List filters `recommendation_type`, `order_by` | API integration tests |
| AC-11 | CSV export unchanged | Existing CSV test passes against table-backed list |
| AC-12 | Machine API absent vs. RBAC/read failure are distinguishable; neither yields a target | KMO/ingestion contract tests |

---

### Tier 2a — Implementation checklist

1. Migration: `machineset_recommendations` + `machineset_recommendation_history`
2. Migration: register planned notification codes 88, 89, 90 after cross-consumer uniqueness verification
3. `internal/plugins/machineset/` — Phase 3 plugin
4. Replica + confidence + heterogeneous logic
5. Retry-safe history writer keyed by source snapshot identity and recommendation fingerprint
6. Wire into recalc batch after `RecommendNodes`
7. `GET .../machinesets/{name}` handler + OpenAPI
8. Switch list API to table-backed rows
9. Add `filter[recommendation_type]`, `order_by`
10. IQE fixtures for detail + history
11. Update [`notification-codes.md`](../architecture/notification-codes.md)

**Effort estimate:** ~2–3 weeks total (Tier 2a + Tier 2b)

---

## Tier 2b — Needs Catalog (Implementation Spec)

Tier 2b adds instance type and family recommendations on top of Tier 2a
node-count guidance. **Blocked on** `cloud_instance_catalog` (REQ-8c.6) and
`instance-type` plugin.

### 1. Instance type recommendation

#### Inputs

| Source | Fields |
|--------|--------|
| `machineset_recommendations` (Tier 2a) | `recommended_node_count`, member utilization |
| `daily_node_digests` | Per-node P95 CPU/memory **usage** (not allocatable) |
| `cloud_instance_catalog` | `vcpus`, `memory_mib`, `family`, `category`, pricing |
| Cluster source | `provider` (AWS/Azure/GCP), `region` |

#### Workload profile (per MachineSet)

```
cpu_need_cores = MAX(member.cpu_usage_p95) × HEADROOM   -- default HEADROOM = 1.20
mem_need_bytes = MAX(member.mem_usage_p95) × HEADROOM
gpu_need       = MAX(member.gpu_count) if any GPU workloads (future)
```

Aggregate across **all** member nodes — peak node drives size (conservative).

#### Recommendation logic

```
current_type = machineset.instance_type (from node labels)
catalog_entry = InstanceCatalog.GetInstanceType(provider, region, current_type)

IF catalog_entry exists:
    recommended_type = smallest_fit(catalog, cpu_need, mem_need, gpu_need)
    -- smallest vCPU+memory in same or better family where vcpus >= cpu_need
    -- AND memory_mib >= mem_need_bytes / (1024*1024)

IF recommended_type.vcpus < current_type.vcpus OR recommended_type.memory < current:
    direction = "downsize"
ELIF recommended_type.vcpus > current_type.vcpus OR recommended_type.memory > current:
    direction = "upsize"
ELSE:
    direction = "unchanged"
```

**Hysteresis:** Only recommend instance type change when monthly savings ≥
`ROS_INSTANCE_CHANGE_MIN_SAVINGS_PCT` (default **20%**). Avoid churn.

**Deprecated/unlisted types:** Follow REQ-8c.6 decision matrix; codes **23**, **24**.

---

### 2. Family migration

When Tier 1 `stranded_resource` is aggregated across members:

| Fleet signal | Recommended family | Example |
|--------------|-------------------|---------|
| CPU-heavy (avg CPU util >> mem util, `stranded_resource = memory`) | Compute-optimized | `m5` → `c5` / `c6i` |
| Memory-heavy (`stranded_resource = cpu`) | Memory-optimized | `m5` → `r5` / `r6i` |
| Balanced | General purpose | stay `m*` |
| GPU workloads | GPU instances | `p3`, `g4`, `g5` |

```
family_recommendation = select_family(
    cpu_to_mem_ratio = sum(cpu_usage) / sum(mem_usage),
    stranded = MODE(member.stranded_resource),
    catalog.categories,
)
```

Family switch only when catalog pricing shows savings ≥ hysteresis threshold.

---

### 3. Cost comparison

```
current_cost_monthly  = current_node_count × instance_hourly_rate(current_type)  × 730
recommended_cost_monthly = recommended_node_count × instance_hourly_rate(recommended_type) × 730
savings_monthly = current_cost_monthly - recommended_cost_monthly
estimated_savings_cents = ROUND(savings_monthly × 100)
```

| Case | `current_cost` | `recommended_cost` | `estimated_savings` |
|------|----------------|--------------------|---------------------|
| Both types in catalog | Known | Known | `current - recommended` |
| Current type unlisted | `NULL` | Known | Capacity-based only; savings NULL |
| Catalog unavailable | Tier 2a replica savings only | — | Sum of node savings |

Pricing source priority:

1. `cloud_instance_catalog` on-demand rate (refreshed daily)
2. Koku `effective_rates` for observed instance types
3. Fallback: replica-only savings (Tier 2a)

---

### 4. Catalog integration — `InstanceCatalog` interface

```go
// internal/catalog/catalog.go

type InstanceSpec struct {
    Provider     string
    Region       string
    InstanceType string
    VCPUs        int64
    MemoryMiB    int64
    Family       string
    Category     string // general, compute_optimized, memory_optimized, gpu
    HourlyRate   float64
    Currency     string
}

type InstanceCatalog interface {
    GetInstanceType(provider, region, instanceType string) (*InstanceSpec, bool)
    SmallestFit(provider, region string, cpuCores, memMiB, gpuCount int64) (*InstanceSpec, bool)
    ListByFamily(provider, region, family string) []InstanceSpec
    Refresh(ctx context.Context) error
    LastRefreshed() time.Time
}
```

#### Data sources (REQ-8c.6)

| Provider | API | Auth |
|----------|-----|------|
| AWS | Bulk Pricing JSON (Tier 1) or `DescribeInstanceTypes` (Tier 2 opt-in) | Public / IAM |
| Azure | Retail Prices API | Public |
| GCP | `machineTypes.list` per zone | Project credentials |

#### Caching

| Layer | TTL | Behavior |
|-------|-----|----------|
| PostgreSQL `cloud_instance_catalog` | Refresh every 24h (`ROS_INSTANCE_CATALOG_REFRESH_HOURS`) | Upsert on refresh |
| In-memory `sync.Map` | Invalidated on refresh | Hot path for engine |
| Fallback | Stale cache up to 48h | Log warning; Tier 2a still works |

```go
// On catalog unavailable:
if !catalog.LastRefreshed().After(time.Now().Add(-48 * time.Hour)) {
    // Skip instance type fields; Tier 2a replica recs unchanged
    rec.RecommendedInstanceType = nil
}
```

---

### Tier 2b — Data flow

```
                    ┌──────────────────────┐
                    │  cloud_instance_catalog │
                    │  (PG + sync.Map cache)  │
                    └──────────┬───────────┘
                               │
daily_node_digests ──►┌────────▼─────────┐     ┌─────────────────────────┐
node_recommendations ─►│ instance-type   │────►│ machineset plugin (2b)  │
                       │ plugin          │     │ merges replica + type   │
                       └─────────────────┘     └───────────┬─────────────┘
                                                           │
                                                           ▼
                                              machineset_recommendations
                                              (+ rec_instance_type, cost cols)
```

---

### Tier 2b — Acceptance criteria

| # | Criterion | Verification |
|---|-----------|--------------|
| AC-B1 | Smallest-fit picks correct catalog entry | Unit test with AWS `m5` family |
| AC-B2 | Family migration `m5`→`c5` for CPU-stranded fleet | Engine fixture |
| AC-B3 | 20% hysteresis suppresses minor changes | Unit test |
| AC-B4 | Unlisted instance type → code 23, no bogus switch | REQ-8c.6 matrix |
| AC-B5 | Catalog down → Tier 2a fields populated, type fields null | Integration with empty catalog |
| AC-B6 | Cost comparison uses 730h/month | Unit test |
| AC-B7 | On-prem without catalog skips Tier 2b gracefully | Deployment test |

**Effort estimate:** (included in Tier 2 total above)

---

## What Tier 2 does **not** include

- **Tier 3 MachineAutoscaler** min/max tuning (REQ-8c.7)
- **PDB/affinity placement simulation** — separate research issues #709 and #710; not a Tier 2a prerequisite
- **Clusters without a usable Machine API** — no MachineSet target; node-level recommendations remain available. BMO inventory is not a fallback.
- **Automated apply** — advisory; see [hpa-vpa-deployment-modes.md](../architecture/hpa-vpa-deployment-modes.md)

---

## References

- [machineset-recommendations.md](../../docs-site/planned-features/machineset-recommendations.md)
- [autoscaler-optimization.md](../../docs-site/planned-features/autoscaler-optimization.md)
- [plugin-phases.md](../architecture/plugin-phases.md) — `machineset` in Phase 3
- [handlers_machinesets.go](../../internal/api/handlers_machinesets.go) — Tier 1 list
- [recommend_nodes.go](../../librobne/node/recommend.go) — `applyInstanceTypeConsolidation`
- [requirements.md § REQ-8c.5](../archive/requirements.md#req-8c5-tier-2--machineset-right-sizing-high--not-implemented)
- [requirements.md § REQ-8c.6](../archive/requirements.md#req-8c6-instance-type-catalog--cloud-api-integration-medium--not-implemented)
- [cost-integration.md](../architecture/cost-integration.md) — `effective_rates`
