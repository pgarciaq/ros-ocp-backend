# ADR-0226: Tag sync is full-replace per org (not incremental)

## Status

Accepted

## Context

Tag data arrives from Koku as a complete org-level snapshot. Incremental merge would require diffing against existing state and handling partial updates reliably.

## Decision

`SyncOrgTags` resets all `resolved_tags` to `{}` for the org, then applies the incoming payload. Metadata tracked in `org_tag_sync_metadata`.

This is a full-replace operation — not additive.

## Alternatives Considered

### Incremental merge

Requires reliable diff and complex conflict resolution on partial payloads.

### Per-namespace sync

Too many API calls from Koku for large orgs.

## Consequences

- If payload is incomplete (missing namespaces), those namespaces lose tags until next sync.
- Sync is idempotent (same payload = same result).
- Partial network failures during sync can leave tags temporarily empty.
- Tag filter results may be empty between reset and apply phases.

## Related Decisions

- [ADR-0120](0120-saas-http-push-tag-sync.md): SaaS HTTP push sync.
- [ADR-0119](0119-tags-source-db-on-prem.md): On-prem DB join.
- [ADR-0227](0227-ros-tags-enabled-master-gate-silently-disables-tag-filters.md): Tags enabled gate.

## References

- [internal/tags/sync.go](../../internal/tags/sync.go)
