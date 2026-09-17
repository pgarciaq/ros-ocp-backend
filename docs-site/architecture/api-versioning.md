# API Versioning Strategy

> **Last verified:** 2026-09-17

## Current Version

All ROS-OCP-Backend API endpoints are served under:

```
/api/cost-management/v1/recommendations/openshift/...
```

The `v1` prefix is part of the path and is shared with the broader Cost Management API surface (Koku).

## Compatibility Policy

### Backward-Compatible Changes (non-breaking)

These changes do NOT require a version bump:

- Adding new optional query parameters
- Adding new fields to JSON response objects
- Adding new endpoints
- Adding new enum values to filter parameters
- Adding new notification codes
- Increasing pagination limits

**Clients MUST ignore unknown fields** (Postel's law). Extra fields in responses are additive and non-breaking.

### Breaking Changes (require version bump or deprecation)

These changes WOULD require a new version:

- Removing or renaming existing response fields
- Changing field types (e.g., string → integer)
- Removing endpoints
- Changing the meaning of existing parameters
- Reducing pagination limits below previously documented values
- Changing error response shapes

### Current Practice

The native engine is response-shape compatible with the legacy (Kruize-era) API with one deliberate exception: [ADR-0292](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0292-digest-based-plot-percentile-bands.md) replaced query-time boxplots (five-number summary: min/q1/median/q3/max) with digest-based percentile bands (`PlotDetails`: p50/p95/p99/max/format) on container and namespace plot endpoints — saving ~90% of database disk by eliminating long-term raw-sample retention. Consumers must read `PlotDetails`, not the legacy boxplot shape (see [kruize-vs-native-comparison.md](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/kruize-vs-native-comparison.md)).

## Deprecation Process

If a breaking change is needed:

1. Document the change in `CHANGELOG.md`
2. Add deprecation notice to the old field/endpoint (keep functional for ≥2 release cycles)
3. Add the new field/endpoint alongside the deprecated one
4. Remove the deprecated item after the deprecation period

## OpenAPI Specification

The authoritative API contract is [`openapi.json`](../openapi.md) at the repository root. It documents:

- All endpoints with request/response schemas
- Query parameter validation (types, enums, limits)
- Error response shapes
- Server base URL (`/api/cost-management/v1`)

The spec is maintained manually and updated whenever endpoints change. The `x-plugin-required` annotation on paths allows dynamic filtering when plugins are disabled.

## Consumer Guidance

- Always check for the presence of optional fields before using them
- Do not hard-code response field lists — new fields may appear at any time
- Use the `meta.count` field for pagination, not response array length
- The `links.next`/`links.previous` fields provide pre-built pagination URLs
