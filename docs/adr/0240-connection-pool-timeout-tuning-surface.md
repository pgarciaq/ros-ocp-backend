# ADR-0240: Connection pool and timeout tuning surface

## Status

Accepted

## Context

ROS runs multiple processes (API, processor, housekeeper, poller) sharing PostgreSQL via unified pgxpool ([ADR-0128](0128-unify-gorm-pgxpool-stdlib.md)). Ingest workloads run longer SQL than list API queries ([ADR-0092](0092-ingest-statement-timeout-120s.md)). External calls to Masu/Koku for reship and effective rates need bounded HTTP timeouts.

Operators tuning on-prem deployments require documented knobs without reading `config.go` for every deployment profile.

## Decision

Expose and document these primary tuning env vars (defaults in `config.go`, details in `docs/operations/configuration.md`):

| Variable | Purpose |
|----------|---------|
| `ROS_DB_MAX_CONNS` | pgxpool max connections per process |
| `ROS_DB_ACQUIRE_TIMEOUT` | Max wait to acquire a connection from pool |
| `ROS_DB_STATEMENT_TIMEOUT` | Default session `statement_timeout` for API queries |
| `ROS_INGEST_STATEMENT_TIMEOUT` | Longer timeout for ingest transactions (`SET LOCAL` where applicable) |
| `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS` | Outbound HTTP client timeout (Masu, tag sync, reship) |
| `ReshipConcurrency` | Parallel cluster reships within org batch (with per-cluster lock, [ADR-0235](0235-two-layer-reship-concurrency.md)) |

Statement timeout for API paths is applied at session level via pgx `AfterConnect` hook setting `statement_timeout` from config.

## Alternatives Considered

### Single statement timeout for all modes

API queries killed too aggressively or ingest holds connections too long.

### Per-handler timeout in application code only

Does not stop runaway SQL server-side; pool exhaustion still possible.

### Unlimited pool size

Masks connection leaks; risks PostgreSQL `max_connections` exhaustion.

## Consequences

- Misconfigured `ROS_DB_MAX_CONNS` across replicas can exhaust PostgreSQL; document sum across pods.
- Ingest timeout must remain ≥ longest expected digest flush ([ADR-0094](0094-split-transactions-50k-rows.md)).
- HTTP timeout interacts with reship retry poller ([ADR-0219](0219-reship-background-poller-retries-pending-clusters.md)).

## Related Decisions

- [ADR-0128](0128-unify-gorm-pgxpool-stdlib.md): Unified pgxpool.
- [ADR-0092](0092-ingest-statement-timeout-120s.md): Ingest statement timeout.
- [ADR-0238](0238-environment-variable-catalog-by-subsystem.md): Env catalog organization.

## References

- [internal/config/config.go](../../internal/config/config.go)
- [internal/db/pool.go](../../internal/db/pool.go)
- [docs/operations/configuration.md](../operations/configuration.md)
