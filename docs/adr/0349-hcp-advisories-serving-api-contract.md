# ADR-0349: HCP Advisories Serving API — Typed Per-Verdict Contract and the One-Advisory-Per-HC Invariant

## Status

Accepted (design) — implementation tracked in #717

Amended (2026-11-07) — The §3 key was extended with `management_cluster_uuid` ([#763](https://github.com/pgarciaq/ros-ocp-backend/issues/763) / V15-13): api-tax's `hc_cluster_id=''` rows from **two management clusters (farms) in one org** collided on the 3-column key, and the upsert's `DO UPDATE` replaced evidence but not identity — one farm's UUID served with another farm's webhook facts, visible to the wrong farm's #791 scope grant. See the amendment in §3.

## Phase

HCP fleet FinOps — advisory serving (W2 headroom, W3 zombie, W5 api-tax advisories; W8 sleep joins at implementation)

## Context

`hcp_correlation_advisories` has three firing verdicts today (`do_not_add_workers_first` W2, `tune_noisy_webhook` W5, `review_unused_hosted_cluster` W3) and W8 adds `schedule_nonprod_sleep` — but **no API reader**: advisories are visible only through direct database access and a metrics counter. #717 adds the first serving endpoint (`GET /recommendations/openshift/hosted-clusters/advisories`). This ADR records the contract decisions. (Numbering note: 0348 is reserved for the W8 classification ADR per the committed W8 plan.)

## Decision

### 1. Typed per-verdict contract, not a raw evidence surface

Considered: (a) a minimal envelope exposing `n_signals` verbatim as an evidence array; (b) a typed per-verdict `details` object with a `oneOf` schema. **Chose (b).** The API is the primary consumption surface — most users consume the data through the API, not the web UI (owner, 2026-10-30) — and the field vocabulary is **derived from attested repo conventions rather than invented**: `cpu_usage_p50_mc` → `*_cpu_mc` usage fields; `cpu_overcommit_ratio` → `*_ratio`; `current_node_count` → count-style names; `measured_at`/`window_*` → time fields; `cluster_uuid`/`confidence` → identity/meta naming. The full field table lives in the OpenAPI spec (the contract home); the convention anchors become the per-field descriptions.

**Evolution rule:** additive only — new fields may appear; renames and removals are breaking. New verdicts extend the `oneOf`.

### 2. Uniform `_mc` suffix within this endpoint

The repo splits `_mc` (digest usage percentiles: `cpu_usage_p50_mc`) from `_millicores` (capacity/requests: `cpu_request_used_millicores`); strictly followed, one `details` block would mix suffixes. This endpoint uses **`_mc` uniformly** — all advisory evidence is digest-derived. (The one place the conventions conflict; decided rather than inherited.)

### 3. One advisory per (org, HC, window) — invariant, constraint, escape hatch

The table PK is `(org_id, hc_cluster_id, window_start)`; every correlator pass upserts it. With the shipped rules, at-most-one-advisory-per-HC is guaranteed **semantically, not just mechanically**:

- **api-tax writes `hc_cluster_id=''`** (management-cluster-scoped by design — shared-plane evidence names no single HC); it never collides with per-HC rows.
- **Headroom (worker saturation) and zombie (sustained user-idle) have contradictory predicates** — an HC cannot be both.
- **W8's sleep rule is partitioned from zombie by design** (idle-always → zombie; active-in-BH-and-idle-outside → sleep; pinned by a Review Focus test in the W8 plan).

**Constraint on future rules:** any new per-HC rule must be predicate-partitioned from the existing rules, **or** the PK must be extended with the verdict column (a migration plus changes to every write path). Rules that can co-fire on one HC with an equal `window_start` silently clobber — last writer wins in `RunCycle` order (headroom → api-tax → zombie). The endpoint contract documents at-most-one-advisory-per-HC, and a pinned test enforces the upsert semantics.

**Amendment (#763):** the PK is now `(org_id, hc_cluster_id, window_start, management_cluster_uuid)` (migration 000218; every write path's conflict target extended). The §3 invariant as originally written implicitly assumed **one management cluster (farm) per org** — a legitimate multi-farm topology broke it: two farms' api-tax writes both keyed `(org, '', window)`, and the upsert replaced evidence while keeping the first writer's identity (the mixing the V15-13 audit proved). The extension preserves the invariant's substance: for per-HC rows the management cluster is functionally determined by the HC (one farm owns an HC), so at-most-one-advisory-per-(HC, window) still holds semantically; for shared-plane rows (`hc=''`) the management cluster **is** the distinguishing identity, so each farm gets its own row — and the #791 scope filter (`management_cluster_uuid = ANY(...)`) improves from "one farm may see a mixed row" to "each farm sees exactly its own". The escape-hatch clause above was exercised with a different column than it named: the hazard that arrived first was multi-farm identity, not co-firing verdicts. The verdict-column extension remains the (still unused) remedy for co-firing per-HC rules.

Two boundary notes. (1) **Upgrade:** pre-fix shared-plane rows may already be mixed, undetectably (evidence records no originating farm) — migration 000218 purges `hc_cluster_id=''` rows rather than serve a possibly-mixed row to expiry; farms with live evidence re-fire within the hour (the table is fires-only, hourly-refreshed). (2) **HC farm moves:** per-HC enumeration follows fresh association snapshots, and two farms can transiently both claim one HC within the snapshot-freshness window — post-fix that yields two correctly-attributed rows for one HC (a transient, self-expiring deviation from at-most-one-per-HC) where pre-fix it yielded one *mixed* row; strictly better, accepted rather than re-engineered.

### 4. Public enum, nullable identity, no pagination in v1

- `verdict` values are public constants: the four above.
- `hosted_cluster_id` is **nullable** — null on management-cluster-scoped rows (api-tax), with `management_cluster_uuid` carrying the identity.
- No pagination in v1: per-org volume is a handful of rows; a documented cap applies. (The house keyset machinery exists if volume ever justifies it.)

## Consequences

- The OpenAPI spec is the contract home for names/types/descriptions; this ADR records why the contract is shaped so.
- Future correlator rules carry the partition obligation (or extend the PK).
- `evidence` (the raw `n_signals`) may ride alongside `details` for provenance — optional and additive.
- The API-cheatsheet entry and IQE constants land with #717's completion; the koku-ui card child files once the shape ships.

## Related Decisions

- [ADR-0333](0333-unused-hostedcluster-lifecycle-w3.md) — advisory copy constraints the endpoint's content inherits
- [ADR-0339](0339-w3-idle-leg-workload-cpu-primary.md) — the zombie evidence this endpoint serves
- #717 (implementation), #396/#716 (W8), the W8 plan (`docs/superpowers/plans/2026-10-30-w8-nonprod-sleep-advisory.md`)
