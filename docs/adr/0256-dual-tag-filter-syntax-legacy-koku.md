# ADR-0256: Dual tag filter syntax (legacy and Koku-style)

## Status

Accepted

## Context

Tag filtering on list endpoints must support existing ROS clients and Koku/HCCM query conventions ([ADR-0121](0121-koku-and-legacy-tag-filter-syntax.md)). Tag data resolves from DB or HTTP sync ([ADR-0119](0119-tags-source-db-on-prem.md), [ADR-0120](0120-saas-http-push-tag-sync.md)) with JSONB on keys ([ADR-0054](0054-resolved-tags-jsonb-on-keys-table.md)).

[ADR-0226](0226-tag-sync-full-replace-per-org.md) full-replace sync can leave filters stale relative to Koku tag inventory — addressed separately in [ADR-0257](0257-stale-tag-sync-warning-on-list-responses.md).

## Decision

Accept **two equivalent syntaxes**, parsed by `TagFiltersFromParams`:

| Syntax | Example |
|--------|---------|
| Legacy | `?tag=environment:production` |
| Koku-style | `filter[tag:environment]=production,staging` |

Semantics:

- **Multi-value within one key:** OR (match any listed value).
- **Cross-key:** AND (must satisfy all specified tag keys).
- **Wildcard value:** `key:*` matches any value for that key.

Both syntaxes may appear in one request; combined filters AND together.

## Alternatives Considered

### Koku-style only

Breaks on-prem scripts and early ROS integrations.

### Legacy only

Breaks UI parity with cost-management explorer filters.

### SQL ILIKE on raw tag JSON string

Injection and index unfriendly; structured JSONB predicates used instead.

## Consequences

- OpenAPI documents both parameter forms.
- IQE and contract tests cover each syntax path.
- Parser must normalize keys to lowercase/canonical form consistently.

## Related Decisions

- [ADR-0121](0121-koku-and-legacy-tag-filter-syntax.md): Original dual-syntax decision.
- [ADR-0226](0226-tag-sync-full-replace-per-org.md): Tag sync full-replace.
- [ADR-0054](0054-resolved-tags-jsonb-on-keys-table.md): resolved_tags JSONB.

## References

- [internal/api/tag_filters.go](../../internal/api/tag_filters.go)
- [docs/operations/api-query-parameters.md](../operations/api-query-parameters.md)
