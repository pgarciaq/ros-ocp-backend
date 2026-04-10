# Native Engine: Notification Compatibility Gap

## Background

The Kruize recommendation engine produces detailed notification objects in its
API responses. These notifications carry numeric codes, human-readable messages,
and severity types. The koku-ui frontend (`koku-ui-ros`) relies on these
notification objects to power several UI features.

## What Kruize Provides

Each recommendation in the Kruize JSON response includes a `notifications` map
keyed by numeric code strings:

```json
{
  "323004": {
    "type": "notice",
    "message": "Workload is optimised wrt CPU Request",
    "code": 323004
  }
}
```

### How ros-ocp-backend Uses Kruize Notifications

| Component | File | Usage |
|-----------|------|-------|
| Validity gate | `internal/utils/kruize/kruize_api.go` | `IsValidRecommendation` checks for code `111000` ("At least one valid recommendation") |
| API response filter | `internal/api/utils.go` | `filterNotifications` strips codes `323004`, `323005`, `324003`, `324004` (optimization notices) from list responses to reduce payload size |
| Response assembly | `internal/api/utils.go` | Remaining notifications forwarded verbatim in the JSONB `recommendations` field |

### How koku-ui Uses Notifications

| Feature | File | Behavior |
|---------|------|----------|
| "Optimized" badge | `apps/koku-ui-ros/src/utils/notifications.ts` | `isIntervalOptimized()` checks whether any notification code in `[323004, 323005, 324003, 324004]` is present — if ALL expected resources are optimized, the row is tagged "optimized" |
| Warning alerts | `apps/koku-ui-ros/src/routes/optimizations/optimizationsBreakdown/` | Displays `<Alert>` components for each notification, using the `message` and `type` fields |
| Notification filtering | `apps/koku-ui-ros/src/utils/notifications.ts` | `filterNotifications()` removes specific codes before display |

## What the Native Engine Provides

The native Go engine has a `notification_codes SMALLINT[]` column in
`recommendation_sets`, populated by `EvaluateNotifications()` in
`internal/engine/notifications.go`. These codes are **integers** (1–24), not the
Kruize 6-digit codes.

However:
- `WriteRecommendations` does **not** currently persist notification codes (the
  column is in the schema but omitted from the INSERT).
- `NativeContainerResult` does **not** include a `notifications` object in the
  JSON response — it only has `notification_codes []int16` at the row level, not
  at the per-term/per-engine level like Kruize.
- The API fallback handlers (`GetRecommendationSetListWithFallback`,
  `GetRecommendationSetWithFallback`) note this limitation in code comments.

## Impact When Native Engine Is Active

| UI Feature | Impact |
|------------|--------|
| "Optimized" badge | **Silent degradation** — never shown because no notification codes are present in the response. Containers won't be marked as optimized even when they are. |
| Warning alerts | **Silent degradation** — no alerts displayed because the `notifications` map is absent. |
| Notification filtering | **No-op** — nothing to filter. |

The UI does **not** crash or show errors — it simply loses the informational
indicators. All other data (recommended values, current values, variations,
cluster/namespace/workload metadata) is fully functional.

## Impact on koku Backend

**None.** The koku backend's "notifications" (`masu/api/notifications.py`,
`koku/notifications.py`) are entirely unrelated — they handle Insights platform
events (missing cost model, stale OCP source) and have no connection to ROS
recommendation notifications.

## Remediation Options

### Option A: Populate and Map Notification Codes (Recommended)

1. Call `EvaluateNotifications()` in `WriteRecommendations` and persist the
   resulting codes in the `notification_codes` column.
2. Add a mapping layer in the API response assembly that converts native integer
   codes to Kruize-compatible notification objects (code, message, type).
3. Include the mapped notifications in `NativeContainerResult.Recommendations`.

**Effort:** Medium. The notification evaluation logic already exists; the gap is
in persisting and mapping the results.

### Option B: Parallel Notification Endpoint

Create a separate API endpoint that returns notification details for a given
container, allowing the UI to fetch them independently.

**Effort:** Medium, but requires UI changes.

### Option C: Accept Degradation

Document that notification indicators are unavailable in the native engine path
and accept the loss until a future phase addresses it.

**Effort:** None (current state).

## Current Decision

Option C (accept degradation) — per user decision. Notifications are not a
priority for the current phase. This document serves as the tracking artifact.
