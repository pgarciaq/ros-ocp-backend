# Database Conventions

Schema design guidelines for ros-ocp-backend PostgreSQL tables. For migration
mechanics, partitioning, and key tables, see [CONTRIBUTING.md — Database](../../CONTRIBUTING.md#database).
For the ERD diagram source, see [docs/database/db-schema](../database/db-schema).

Related architecture docs:

- [requirements.md](requirements.md) — REQ-2.4/2.5 JSONB elimination and relational columns
- [operations/query-performance.md](../operations/query-performance.md) — indexing and list-query patterns
- [migrations/README.md](../../migrations/README.md) — migration best practices

---

## When to Use JSONB vs Normalized Tables

### The Rule

**JSONB is appropriate for:**

- Configuration/settings data (read whole blob by PK, write rarely, small payloads)
- Opaque metadata that is never filtered/queried inside SQL
- Low-cardinality data (few rows per tenant)

**JSONB is NOT appropriate for:**

- Fact/telemetry/time-series data at volume
- Data where you need per-element access, filtering, joining, or indexing
- High-cardinality data that grows with usage (rows × time × entities)

### Decision Matrix

| Question | If YES → | If NO → |
|----------|----------|---------|
| Will SQL queries filter/join on individual JSON fields? | Normalize | JSONB may be OK |
| Will the data grow proportionally to usage volume? | Normalize | JSONB may be OK |
| Do you need indexes on fields inside the JSON? | Normalize | JSONB may be OK |
| Is this read-whole/write-whole by primary key? | JSONB is fine | Consider normalizing |
| Is this ≤ tens of rows per tenant? | JSONB is fine | Consider normalizing |

### Examples in this codebase

| Table | JSONB Column | Verdict | Reasoning |
|-------|-------------|---------|-----------|
| `recommendation_thresholds` | `thresholds` | **Appropriate** | Settings store; ≤9 rows/org; read whole blob by PK; no SQL predicates inside JSON; written only on admin PUT |
| `daily_vm_digests` | `gpu_devices` (removed in 000100) | **Inappropriate → Normalized** | Per-GPU telemetry; grew with VMs × days × devices; needed per-element analysis for notification 54, mixed-idle detection, API detail; replaced with `vm_gpu_device_digests` child table |
| `recommendation_sets` | `notification_codes` (SMALLINT[]) | **Appropriate** | Fixed-size array; read whole; filtered via `@>` operator with array index support |
| `org_container_keys` | `resolved_tags` | **Appropriate** | Metadata blob; read whole per container identity; no SQL-level tag filtering |

See [vm-recommendations.md](../design/vm-recommendations.md) for the `vm_gpu_device_digests` normalization case study.

### Anti-patterns to avoid

1. **JSONB arrays as child tables** — If you find yourself using `jsonb_array_elements()` in queries or needing GIN indexes, normalize into a child table with FK + `ON DELETE CASCADE`.
2. **JSONB for time-series dimensions** — If the JSON grows with each ingestion cycle, it belongs in a proper table with time-based partitioning or retention.
3. **Querying JSONB fields in WHERE clauses** — If you write `WHERE col->>'field' = X`, that field should probably be a column.

### When adding new JSONB columns

Before adding a JSONB column to any table, answer:

1. How many rows will this table have? (If millions → don't use JSONB for per-row variable data)
2. Will you ever need to query/filter by fields inside the JSON? (If yes → normalize)
3. Is the JSON schema stable or will it evolve? (Evolving schemas in JSONB are fine for config; problematic for fact data where you need migrations)
