# ADR-0300: Remove fixed workload_type allowlist

## Status

Accepted

## Phase

Phase 15

## Context

The native engine path validated `workload_type` filter parameters against a hardcoded
six-type allowlist: `daemonset`, `deployment`, `deploymentconfig`, `replicaset`,
`replicationcontroller`, `statefulset`. This mirrored the PostgreSQL
`sorted_workloadtype` enum originally created in migration 0014.

This design broke for customers using CRD-based workload controllers (e.g. WebLogic
`Domain`, ArgoCD `Application`, Strimzi `KafkaNodePool`) because:

1. The koku-metrics-operator recording rule already collects arbitrary `owner_kind`
   values from Prometheus `kube_pod_owner` metrics.
2. The ingestion pipeline in ros-ocp-backend already stores these values in the
   `workloads` table without issue (the TEXT column accepted anything).
3. The API layer then rejected queries filtering on those types, making the data
   invisible to users.

The idle detection settings API also had a separate allowlist
(`validIdleWorkloadTypes`) with mixed-case Kubernetes kinds (Deployment, StatefulSet,
etc.), which similarly prevented excluding CRD-based workload types from idle
detection.

Jira: COST-7274

## Decision

Remove both workload_type allowlists and replace with format-only validation:

- Non-empty string
- Maximum 63 characters (Kubernetes name length convention)
- No whitespace
- Reject sentinel value `<none>`

Migrate the `workloads.workload_type` column from `sorted_workloadtype` enum to
`TEXT` to match the runtime behavior.

Retain the well-known workload type constants in `internal/types/workload/workload.go`
as convenience values for tests and default configurations, but do not use them as
an exhaustive gate.

## Consequences

### Positive

- API accepts any valid Kubernetes owner kind string, making CRD-managed workloads
  visible in the UI and filterable.
- Idle detection exclusions can reference arbitrary workload types.
- The recording rule, ingestion, engine, and API are now consistently type-agnostic.

### Negative

- UI will need a dynamic dropdown (populated from actual data) instead of a
  hardcoded picklist — separate frontend PR required.
- Savings estimates for CRD-based types use `pod_count_avg` fallback since
  `desired_replicas` is not available for custom controllers without HPA.

### Neutral

- The legacy Kruize aggregator path (`internal/utils/aggregator.go`) is unaffected
  and will be cleaned up separately.
- Engine recommendation logic is already type-agnostic — no changes needed there.
