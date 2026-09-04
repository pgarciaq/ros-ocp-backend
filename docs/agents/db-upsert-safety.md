# Database Upsert Safety: Preventing Deadlocks

## The Problem

PostgreSQL `INSERT ON CONFLICT DO UPDATE` acquires row-level locks in the order
rows are encountered. When two concurrent transactions upsert overlapping rows
in different orders, a deadlock occurs (SQLSTATE 40P01).

Go map iteration order is non-deterministic. Any function that iterates a map
to build batch upsert statements is vulnerable:

```go
// DEADLOCK-PRONE: map iteration order is random
for k, v := range grouped {
    batch.Queue("INSERT ... ON CONFLICT ...", k.field1, k.field2, ...)
}
```

## Required Pattern

Every function that upserts rows from a Go map MUST:

1. **Sort keys deterministically** before building the batch — sort order should
   match the table's unique index column order
2. **Wrap the transaction in `withDeadlockRetry`** for defense-in-depth

```go
// CORRECT: deterministic key order prevents circular lock dependencies
entries := make([]entryType, 0, len(grouped))
for k, v := range grouped {
    entries = append(entries, entryType{key: k, val: v})
}
slices.SortFunc(entries, func(a, b entryType) int {
    // Sort by unique index columns in order
    if c := cmpStr(a.key.Field1, b.key.Field1); c != 0 { return c }
    if c := cmpStr(a.key.Field2, b.key.Field2); c != 0 { return c }
    return 0
})

err := withDeadlockRetry("label", func() error {
    tx, err := pool.Begin(ctx)
    // ... batch upserts on sorted entries ...
    return tx.Commit(ctx)
})
```

## Checklist for New Upsert Functions

When adding or modifying any function that does `INSERT ... ON CONFLICT`:

- [ ] Does the function iterate a Go map to build upserts? If yes, sort first
- [ ] Is the sort order consistent with the table's unique index?
- [ ] Is the transaction wrapped with `withDeadlockRetry`?
- [ ] If the function accepts a pre-sorted slice (not a map), is the caller's
      ordering guaranteed to be deterministic?

## Currently Protected Functions

| Function | File | Table |
|---|---|---|
| `upsertContainerDigestsOnSender` | `pipeline_business_hours.go` | `daily_container_digests` |
| `flushGPUStreamGroupsOnSender` | `gpu_stream.go` | `gpu_container_digests` |
| `FlushNodeDigests` | `node_digest.go` | `daily_node_digests` |
| `UpsertGPUDigests` | `pipeline.go` | `gpu_container_digests` |

## When This Rule Triggers

This rule applies when editing files under `internal/ingestion/`. Review any
new `INSERT ... ON CONFLICT` or `batch.Queue` calls against the checklist above.

## Reference

- GitHub issue: https://github.com/pgarciaq/ros-ocp-backend/issues/255
- Helper: `internal/ingestion/deadlock_retry.go` (`withDeadlockRetry`, `isDeadlock`)
- Sort helper: `internal/ingestion/models.go` (`sortDigestKeys`, `cmpStr`)
