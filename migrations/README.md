# Database migrations

Apply migrations via `./rosocp db migrate up` (see project docs).

## Index conventions

**All new indexes MUST use `CREATE INDEX CONCURRENTLY`** so migrations do not block writes during zero-downtime deployments.

PostgreSQL does not allow `CONCURRENTLY` inside a transaction block; migration runners that wrap each file in a transaction may require a separate non-transactional migration step—follow the pattern used elsewhere in this repo for concurrent index creation.

Existing migrations that already ran cannot be rewritten retroactively; apply this rule to **new** migrations only.

Migrations **000058–000060** alter tables/functions and do not add secondary indexes; no `CONCURRENTLY` changes were applied there.
