# #847 — Offline Sleep Attestation Parity — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner go-ahead 2026-11-13: fold attestation into `LegCoverage` (option 1).

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `ec49ed17`. Tracker: #849 (V-BR8, P3).

## Problem (locked by the issue)

The server scopes #831 attestation to USER workloads (platform rows
`continue` before the gate); the offline reader collapses to one
window-wide boolean over EVERY row, and the advisory kills itself on
false. User evidence rebuilt under the current schedule + one
stale-fingerprint platform row → server advises, offline silent — the
parity break; platform rows only re-attest when their workloads
re-report, so one obsolete row blocks offline recovery indefinitely.

## Mechanism (owner-approved: fold into LegCoverage)

- **Reader** (`librobne/pgdigest/read_sleep_evidence.go`):
  `EvidenceHours` gains `Attested bool` (per-row fp match); the
  window-wide `bool` return is REMOVED.
- **Advisory** (`librobne/advisory/sleep.go`): `LegCoverage` gains
  `Attested bool`; `SleepCandidateWithCoverage` drops the
  `scheduleAttested bool` param; the gate moves INTO the loop at the
  server's position (after the platform `continue`, with the #827
  coverage gates, before the peaks) — user-workload-scoped, returning
  nil on an unattested user workload-day. Nil/missing coverage = no
  attestation info = gate skips (equivalent to the legacy
  `SleepCandidate` wrapper's `true`).
- **CLI** (`cmd/robne/recommend.go`): `coverageFromHoursRows`
  AND-collapses `Attested` per workload-day (matching the server's
  `NOT bool_or(IS DISTINCT FROM)`); the CSV-mode coverage builder sets
  `Attested: true` (inherently attested — split and evaluation share
  the schedule); `fl.scheduleAttested` removed.

Why removing the window-wide kill loses nothing: no rows → empty legs
→ caller guard; stale USER rows → the per-workload gate; stale
PLATFORM rows → correctly ignored (the fix).

## Tests

- **RED (close criterion, end-to-end):** cmd/robne
  `pg_integration_test.go` — 14 days of user evidence under fpA + one
  stale-fingerprint platform row (kube-system) → ReadSleepEvidence →
  coverageFromHoursRows → SleepCandidateWithCoverage → the advisory
  must FIRE. Baseline: the window bool reads false → nil.
- Updated `TestSleepCandidate_UnattestedScheduleIsSilent` (advisory):
  user-unattested pair → nil; attested → fires (the #831 contract at
  workload grain).
- Platform-parity control (advisory): platform-unattested +
  user-attested → fires.
- Reader round-trip extension: per-row Attested against both
  fingerprints (extends TestWriteReadSleepEvidenceHoursRoundTrip).
- **Mutations:** gate reverted to window-wide → RED fails; user-scoping
  dropped (platform rows gate too) → parity control fails; AND-collapse
  dropped → reader test fails.

## Non-goals

The server path (already correct — the fix brings offline TO server
shape); #848 (weekly reclaim); #853 (EXISTS verification).
