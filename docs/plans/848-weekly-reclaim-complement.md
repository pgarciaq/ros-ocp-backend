# #848 — Weekly Sleep Reclaim Complement — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-13: the complement formula + the optional
> docs-site clarifying clause.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `d4d1caec`. Tracker: #849 (V-BR9, P3 — the last finding).

## Problem (locked by the issue)

Both reclaim computations use `len(Days) × (24h − window)` — non-business
hours on SCHEDULED days only, omitting non-scheduled days entirely,
although those hours ARE part of the proposed sleep window (weekend
activity is evaluated as a veto). Mon–Fri 09:00–17:00 reports 80h
instead of 128h (168 − 40); two workers 160 instead of 256
node-hours/week — a 37.5% underestimate.

## Mechanism

The weekly complement — `(7×24h − len(Days) × window)` — in both
implementations (`internal/engine/correlate/sleep.go` server,
`librobne/advisory/sleep.go` offline).

Correctness: each scheduled day contributes exactly `window` BH hours
(overnight wrap attributes to the scheduled day's shift; identical
per-day windows can never overlap); the complement is always ≥ 0
(the API rejects equal start/end, so window < 24h); empty day list →
168h (no business hours → sleep always, off leg gates safety);
timezone-independent (per-local-day attribution; the 168h week is
nominal, as under the old formula — an estimate).

## Tests

The corrected expectations are the REDs (they fail on current code):
- Offline `TestSleepCandidate_DeclaredAndBHSignatureFires`: 160 → 256.
- Offline `TestWeeklyOutsideHours_MidnightCrossing`: 80 → 128
  (extended to a small table: weekday same-day, weekday overnight,
  7-day, empty-days).
- Server `TestSleepCandidate_DeclaredAndBHSignatureFires`: signal
  `reclaim_node_hours_per_week:160` → `:256`.

Mutations: revert either site's formula → its assertions fail.

## Docs

- ADR-0348 §7 already describes the correct semantics — no change.
- docs-site HCP page: one clarifying clause ("including non-scheduled
  days — weekends are part of the sleep window").
- CHANGELOG Fixed entry.

## Non-goals

DST-week precision (the estimate is nominal, pre-existing character);
in-flight advisory staleness (the #760 lifecycle bound); #853.
