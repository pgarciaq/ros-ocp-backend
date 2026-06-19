# ADR-0299: Adopt pgxpool for high-throughput ingest alongside GORM

## Status

Accepted

## Context

The upstream `ros-ocp-backend` (main branch) uses **pure GORM** — a single `*gorm.DB` global backed by `gorm.io/driver/postgres` (which uses pgx/v5 as its underlying driver, but only indirectly through the standard `database/sql` interface). All database access — reads and writes — goes through GORM's ORM layer.

The native engine work introduced a high-throughput ingest pipeline that must process 10k–50k+ digest rows per Kafka manifest within a 120-second statement timeout (see [ADR-0092](0092-ingest-statement-timeout-120s.md)). These rows arrive as streaming CSV files from the koku-metrics-operator; each manifest may contain pod usage, storage, GPU, namespace labels, and ROS-specific container data that must be upserted into daily digest tables with `ON CONFLICT DO UPDATE ... RETURNING` semantics.

### Why GORM cannot satisfy ingest requirements

1. **One round-trip per row**: GORM's `db.Create()` / `db.Save()` issues one `INSERT` statement per call. For 10k rows, that's 10k network round-trips — each with connection acquisition, query parsing, and result scanning overhead.

2. **No batch pipeline API**: GORM has no equivalent of PostgreSQL's extended query protocol pipelining. Each statement is sent, flushed, and awaited independently.

3. **No COPY protocol support**: PostgreSQL's `COPY FROM` binary protocol is the fastest possible insert path (zero parsing overhead on the server). GORM provides no access to it.

4. **Reflection-based scanning**: GORM uses `reflect.Value` for struct field population on every row. On hot paths processing thousands of rows per second, this creates GC pressure from per-row allocations.

5. **No `ON CONFLICT ... RETURNING` control**: GORM's `Clauses(clause.OnConflict{...})` generates upsert SQL but does not expose fine-grained control over `RETURNING` clauses needed to track which rows were inserted vs updated for downstream analytics.

6. **Implicit transactions**: GORM wraps individual creates in implicit transactions. Bulk operations require manual `db.Session(&gorm.Session{CreateBatchSize: N})` which still issues multiple INSERT statements (just batched into groups), not pipelined.

## Decision

Introduce `pgxpool` as a direct dependency for **ingest and bulk-write paths** while retaining GORM for **API read handlers**.

The ingest pipeline acquires connections from a shared `pgxpool.Pool` and uses:
- `pgx.Batch` / `SendBatch` for pipelining N upsert statements in a single network round-trip
- `pgx.CopyFrom` for binary bulk loading where COPY semantics apply (e.g., raw metric rows without conflict handling)
- Explicit `pgtype` mappings for type-safe parameter binding without reflection
- Connection-scoped prepared statements reused across batch iterations

GORM continues to serve all list/detail API handlers, migration tooling, and model definitions via `stdlib.OpenDBFromPool` on the same underlying pool (see [ADR-0128](0128-unify-gorm-pgxpool-stdlib.md)).

## Why pgx Over GORM for Ingest (Detailed Comparison)

| Dimension | GORM | pgx (direct) |
|-----------|------|--------------|
| **Batching** | Multiple INSERT statements, one `Exec` per batch group | `SendBatch`: pipeline N statements in one network flush |
| **COPY protocol** | Not supported | `CopyFrom`: binary wire-level bulk load |
| **Type handling** | Reflection-based, `reflect.Value` per field | Explicit `pgtype` registration, zero-reflect Scan targets |
| **Prepared statements** | Per-transaction or per-session; GORM re-prepares across calls | Connection-level cache; reuse across batch iterations |
| **Memory profile** | Per-row `reflect.Value` + interface boxing allocations | Stack-allocated scan targets; batch buffer reuse |
| **Wire protocol** | Standard `database/sql` simple/extended query | Direct extended query protocol; parse/bind/execute separation |
| **Upsert control** | `clause.OnConflict` with limited RETURNING support | Raw SQL with full `ON CONFLICT DO UPDATE ... RETURNING` |
| **Error granularity** | Wrapped in GORM error types | Direct `pgconn.PgError` with constraint name, position |
| **Connection lifecycle** | Managed by `database/sql` pool (acquire per query) | Explicit acquire/release on pool; batch holds one conn |

### Quantitative justification

- **Row-by-row GORM**: On a 10k-row manifest, each `db.Create()` incurs ~1ms network round-trip (local) to ~5ms (cross-AZ). Total: 10–50 seconds for INSERT alone, leaving no budget for conflict resolution, analytics hooks, or partition management within the 120s timeout.

- **pgx batches (chunked at 500)**: 20 batches × 1 round-trip each = ~20ms network time. Total ingest including conflict handling: 2–5 seconds for 10k rows.

- **UXSNO benchmark** ([benchmark report](https://pgarciaq.github.io/ros-ocp-backend/operations/benchmark-report/)): Ingest of ~876 containers completes streaming+digest phase in ~43 seconds. With per-row ORM calls, this would exceed 120s on manifests with >2000 containers — common in production clusters.

## Why Keep GORM for API Reads

Removing GORM entirely is not justified for the read path:

1. **60+ model files** with struct tags, associations, and hooks already defined. These provide correct serialization, eager loading (`Preload`), and scoped queries for list/detail handlers.

2. **Pagination and filtering**: List handlers use GORM's query builder for dynamic `WHERE` clause construction from user-supplied filters. The builder prevents SQL injection by construction for dynamic column references ([ADR-0169](0169-allowlisted-native-sql-query-fragments.md)).

3. **Migration tooling**: `AutoMigrate` and GORM's schema inspection accelerate development iteration on model changes. Migration files reference GORM models for type safety.

4. **Performance is not the bottleneck**: API read queries return single-digit millisecond results for detail lookups and <100ms for paginated lists. ORM overhead (~50µs per query) is negligible relative to PostgreSQL execution time.

5. **Risk/reward**: Rewriting all read handlers to raw pgx would risk introducing SQL injection vectors, pagination bugs, and association-loading regressions for zero measurable latency improvement.

## Full GORM Replacement Analysis

### Arguments for full replacement (eliminate GORM entirely)

- **Single query style**: All database access uses the same pgx idioms — no context-switching between ORM and raw SQL for developers.
- **No abstraction leaks**: GORM occasionally generates unexpected SQL (implicit JOINs, suboptimal ORDER BY handling). Raw pgx gives full control.
- **Smaller dependency tree**: GORM pulls in ~15 transitive dependencies. pgx alone is lighter.
- **Full SQL control**: Complex queries (recursive CTEs, window functions, lateral joins) are cleaner in raw SQL than forced through ORM query builders.
- **Eliminates dual-maintenance**: No need to understand two query patterns or debug GORM-specific behaviors.
- **pgx ecosystem**: Libraries like `pgxscan` or `scany` provide struct scanning without full ORM overhead, offering a middle ground.

### Arguments against full replacement

- **Massive rewrite effort**: ~60 model files, all list/detail handlers, settings handlers, notification catalog, tag sync — estimated 4–6 weeks of focused migration work with high regression risk.
- **SQL injection surface**: GORM's query builder prevents injection by construction for dynamic filters. Raw SQL with user-supplied filter values requires careful parameterization and the allowlist system ([ADR-0169](0169-allowlisted-native-sql-query-fragments.md)) — more surface area for mistakes.
- **Migration tooling loss**: No equivalent of `AutoMigrate` in pgx. Would need to adopt a standalone migration tool (already done via `golang-migrate`, but lose model-to-schema coherence checks).
- **Diminishing returns**: API reads are not bottlenecked on ORM overhead. Measured overhead is ~50µs per query on paths that take 5–50ms total.
- **Team familiarity**: Existing contributors know GORM idioms. A full migration changes the entire codebase's query style simultaneously.

### Recommendation

**Not worth doing now.** The unified pool ([ADR-0128](0128-unify-gorm-pgxpool-stdlib.md)) eliminated the operational pain of dual connection management. The separation is clean: pgx owns ingest (write-heavy, latency-critical), GORM owns API reads (convenience-heavy, not latency-critical).

Incrementally migrate hot read paths to raw SQL only if GORM becomes a **measurable** bottleneck — for example, high-cardinality list queries with complex tag joins where GORM generates suboptimal plans. Each such migration should be targeted and benchmarked.

A full GORM removal would be justified only if:
- Binary size constraints require dropping the dependency (unlikely — GORM adds ~5MB)
- Compilation speed becomes a blocker (GORM adds ~8s to cold builds)
- A security audit requires eliminating GORM's use of `reflect` (extreme scenario)
- The team decides to adopt a different framework that conflicts with GORM

None of these conditions currently hold.

## Alternatives Considered

### Use GORM's CreateInBatches with large batch sizes

`db.CreateInBatches(rows, 1000)` splits rows into groups and issues one INSERT per group. This reduces round-trips from N to N/1000, but:
- Still no pipelining (each batch INSERT is sent and awaited independently)
- No COPY protocol
- No `ON CONFLICT ... RETURNING` fine-grained control
- Reflection overhead remains per-row
- Measured at ~10x slower than pgx batches for 10k-row manifests

### Use sqlx instead of pgx

`sqlx` provides struct scanning and named parameters on top of `database/sql`. However:
- No batch pipeline API (still one query per `Exec`)
- No COPY protocol
- No connection-level prepared statement caching
- Adds a dependency without solving the core performance problem

### Use raw database/sql with pgx stdlib adapter

The `pgx/v5/stdlib` package exposes pgx through `database/sql`. This works for simple queries but:
- `database/sql` has no batch API
- No COPY protocol exposure
- Connection pooling is managed by `database/sql` (less configurable than pgxpool)
- Loses pgx's explicit type system

### Write a custom batch layer on top of GORM

Build a wrapper that collects GORM model instances and manually constructs multi-row INSERT SQL:
- Reimplements what pgx already provides
- Fragile: must track GORM struct tags, column names, type mappings
- No COPY protocol
- Maintenance burden with no ecosystem support

## Consequences

- **Two query styles coexist**: Ingest code uses pgx idioms (explicit SQL, batch queuing, typed parameters). API code uses GORM idioms (model structs, query builder, preloading). Clear separation by package makes this manageable.
- **Developer onboarding**: New contributors must understand both pgx and GORM. Mitigated by package-level documentation and the clear boundary (ingest vs API).
- **Pool unification**: Both pgx and GORM share a single `pgxpool.Pool` via `stdlib.OpenDBFromPool` ([ADR-0128](0128-unify-gorm-pgxpool-stdlib.md)), eliminating operational complexity.
- **Testing**: Ingest tests use pgxpool directly against testcontainers PostgreSQL. API tests continue using GORM test helpers. No conflict.

## Related Decisions

- [ADR-0092](0092-ingest-statement-timeout-120s.md): Statement timeout that makes per-row inserts infeasible
- [ADR-0093](0093-chunked-pgx-batches-500.md): Chunked batch sizing rationale
- [ADR-0128](0128-unify-gorm-pgxpool-stdlib.md): Pool unification that solved dual-pool operational pain
- [ADR-0169](0169-allowlisted-native-sql-query-fragments.md): SQL fragment allowlist that GORM provides by construction
- [ADR-0171](0171-streaming-recommendation-batches.md): Streaming batches for memory-bounded processing

## References

- [pgx documentation: Batch](https://pkg.go.dev/github.com/jackc/pgx/v5#Batch)
- [pgx documentation: CopyFrom](https://pkg.go.dev/github.com/jackc/pgx/v5#Conn.CopyFrom)
- [UXSNO Benchmark Report](https://pgarciaq.github.io/ros-ocp-backend/operations/benchmark-report/)
- [internal/db/db.go](../../internal/db/db.go)
