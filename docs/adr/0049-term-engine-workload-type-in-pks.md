# ADR-0049: Include term, engine, and workload_type in PKs

## Status

Accepted

## Context

Narrower PKs caused silent upsert collisions across Deployment vs StatefulSet with same name.

## Decision

PK includes `(org_id, cluster_uuid, namespace, workload_name, workload_type, container, term, engine)`.

## Consequences

No collisions. Wider PK. Correct upsert semantics for all workload types.

## References

- [internal/ingestion/pipeline.go](internal/ingestion/pipeline.go)
