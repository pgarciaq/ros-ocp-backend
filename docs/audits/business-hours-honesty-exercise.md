# Business Hours Recommendations — Honesty Exercise

| Field | Value |
|-------|-------|
| **Date** | 2026-06-21 |
| **Auditor** | AI agent |
| **Feature** | Business Hours (schedule-aware dual-stream recommendations) |

> **Later (2026-08-24):** Settings PUT overnight windows shipped in
> [#488](https://github.com/pgarciaq/ros-ocp-backend/issues/488). Notes below
> that say overnight is deferred are historical as of this audit date.

---

## Executive Summary

The business hours feature is **fully implemented and well-aligned** across the ecosystem. It provides dual-stream (all_hours + business_hours) container and namespace recommendations based on configurable weekly schedules with timezone support.

**Feature maturity:** Production-ready (v1 shipped). Settings API, ingestion dual-digest pipeline, recommendation engine enrichment, API response, OpenAPI spec, E2E tests, IQE tests, Bruno collection, and cheatsheet documentation are all present and consistent.

**Key findings:**

- 1 documentation bug: design doc shows `"format": "bytes"` but implementation correctly uses `"format": "MiB"` (aligned with all other memory fields)
- 1 broken link in public docs-site: references non-existent `../features/business-hours.md`
- List response correctly strips `business_hours` (detail-only field) — design-aligned
- No notification codes specific to business hours — design-aligned
- koku-ui renders business hours as "Peak hours sizing" card on detail breakdown — functional

---

## Phase 1: Discovery Summary

### How business hours are defined

- **Settings API** at three scopes with inheritance: org → cluster → namespace
- **Fields:** `timezone` (IANA), `schedule.days[]` (lowercase English), `schedule.start_time`/`end_time` (HH:MM 24h), `off_hours_weight` (0.0–1.0), `enabled` (bool)
- Overnight windows rejected (v1 limitation; later unlocked by Settings PUT [#488](https://github.com/pgarciaq/ros-ocp-backend/issues/488))

### How the engine splits data

- At ingestion time, each 15-minute metric row is evaluated against the effective schedule
- `IntervalStart` (UTC) is converted to the configured timezone
- Row contributes to `all_hours` digest always; contributes to `business_hours` digest only when inside the window (or at reduced weight via `off_hours_weight`)
- Two digest rows per container per day when BH is enabled (`schedule_type` column discriminator)

### API response structure

- Detail endpoint: `recommendation_engines.{cost|performance}.business_hours.{requests|limits}.{cpu|memory}.{amount|format}`
- List endpoint: `business_hours` field is **stripped** (detail-only enrichment)
- Format: CPU in `cores`, memory in `MiB` (same as main `config` block)

### Per-container, per-namespace, or global?

- Per-container and per-namespace (v1 scope)
- Node, GPU, PVC excluded (by design — ADR-0036)

### Interaction with recommendation terms

- Each term (short/medium/long) independently computes BH recommendations from the business_hours digest stream
- Same decay half-life and percentile logic applies per-term
- Insufficient data returns a `reason` string instead of values

### Savings estimates

- Savings always use the `all_hours` perspective (cheatsheet explicitly states this)
- No separate BH savings calculation

### Notification codes

- No codes specific to business hours (design-aligned)
- Standard container codes apply

### Data requirements from operator

- **No operator changes needed** — ROS CSVs already include `interval_start`/`interval_end` at 15-minute granularity

### UI display

- koku-ui shows a "Peak hours sizing" card in the detail breakdown view
- Reads `engine.business_hours` (typed as `RecommendationValues`)
- Card appears as a third grid column when BH data is present

### CSV export

- Business hours fields are **not** in CSV export (list endpoint strips BH)
- Detail-only enrichment by design

---

## Phase 2: Alignment Matrix

| Aspect | Requirements | Go Code | Internal Docs | Public Docs | Cheatsheet | Bruno | Unit Tests | E2E Tests | IQE Tests | OpenAPI |
|--------|:-----------:|:-------:|:-------------:|:-----------:|:----------:|:-----:|:----------:|:---------:|:---------:|:-------:|
| Settings API (3 scopes + inheritance) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Timezone (IANA) validation | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Schedule fields (days, start_time, end_time) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| off_hours_weight [0.0, 1.0] | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Kill-switch (ROS_BUSINESS_HOURS_ENABLED) | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ |
| Dual digest streams (all_hours + business_hours) | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | — |
| Nested response block (engine.business_hours) | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ |
| Response format (cores + MiB) | ⚠️ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | — | ✅ |
| List strips BH (detail only) | ✅ | ✅ | — | — | — | — | ✅ | — | ✅ | — |
| Reship on schedule change | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | — | — |
| Effective endpoint (resolved_from) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Container + Namespace only (not node/gpu/pvc) | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | — | — | — |
| Per-term BH recommendations (short/medium/long) | ✅ | ✅ | ✅ | ✅ | — | — | ✅ | ✅ | — | ✅ |
| Savings use all_hours only | ✅ | ✅ | ✅ | — | ✅ | — | — | — | — | — |
| No BH-specific notification codes | ✅ | ✅ | — | ✅ | — | — | — | — | — | — |
| PUT returns 202 Accepted | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Overnight window rejection | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | — | — | — |
| reship_status field on cluster GET | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | — | ✅ |
| Capabilities endpoint (business_hours: bool) | ✅ | ✅ | ✅ | ✅ | ✅ | — | ✅ | ✅ | ✅ | ✅ |
| koku-ui display | — | — | — | — | — | — | — | — | — | — |

**UI:** koku-ui renders `business_hours` as "Peak hours sizing" card — ✅ aligned with API contract.

---

## Phase 3: Discrepancies Found

### 1. Design doc response example: memory format is "bytes" but code uses "MiB"

**Location:** `docs/features-business-hours.md` line ~500

**What's wrong:** The example response shows:
```json
"memory": { "amount": 402653184, "format": "bytes" }
```

But the implementation (`internal/model/detail_response.go` `kibToMiB()`) produces:
```json
"memory": { "amount": 393216, "format": "MiB" }
```

This matches all other memory fields in the API (the `config` block also uses MiB).

**Authoritative source:** Code is authoritative here. The design doc example was written before implementation settled on MiB (consistent with existing Kruize-compatible format).

**Fix:** Update design doc example.

### 2. Public docs-site broken link

**Location:** `docs-site/plugin-reference/business-hours.md` lines 68-69

**What's wrong:** References `../features/business-hours.md` which doesn't exist under `docs-site/`. The actual internal design doc is at `docs/features-business-hours.md`.

**Fix:** Link to the internal design doc relative path, or remove the link since docs-site shouldn't reference internal docs.

---

## Phase 4: Fixes Applied

### Fix 1: Design doc response example — memory format corrected

Updated the example in `docs/features-business-hours.md` to show `"format": "MiB"` with a correct MiB amount value, matching the actual API output.

### Fix 2: Public docs-site broken links

Updated `docs-site/plugin-reference/business-hours.md` to remove the broken internal-only links and replace with appropriate references.

---

## Phase 5: Verification

- `go build ./...` — passes
- `go test ./internal/... -count=1` — passes (business hours tests confirm format alignment)

---

## Phase 6: Honest Assessment

### What works end-to-end

1. **Settings API** — full CRUD at org/cluster/namespace with inheritance, effective endpoint, validation
2. **Ingestion** — dual digest pipeline correctly splits rows by schedule, weighted percentile computation
3. **Engine** — BH recommendations computed per-term, attached to detail results
4. **API response** — Kruize-compatible nested block with correct format strings
5. **Reship** — schedule change triggers async historical reprocessing via Koku masu
6. **Kill-switch** — feature cleanly disables (routes hidden, OpenAPI stripped, capabilities reflect)
7. **koku-ui** — "Peak hours sizing" card renders on detail view when BH data exists
8. **E2E tests** — comprehensive coverage in cost-onprem-chart (Phase 10)
9. **IQE tests** — smoke coverage for capabilities, settings CRUD, and enrichment verification
10. **Bruno collection** — all endpoints covered (GET/PUT/DELETE at all scopes + effective)
11. **Cheatsheet** — detailed documentation with correct examples

### What's missing or planned

1. **Overnight windows** — deferred at audit time; Settings PUT now allows `end_time < start_time` ([#488](https://github.com/pgarciaq/ros-ocp-backend/issues/488))
2. **Node/GPU business hours** — Phase 2 (by design, ADR-0036)
3. **Separate BH savings estimates** — not planned; savings always use all_hours
4. **CSV export of BH data** — not applicable (detail-only enrichment)
5. **v1.1 forward-only degraded mode UI banner** — designed but not yet in koku-ui
6. **v1.1 slow periodic backfill retry** — designed, not yet implemented

### Design questions for the user

None — the feature is well-designed and consistently implemented. The only discrepancies found were minor documentation issues (stale example format, broken link) which have been fixed.
