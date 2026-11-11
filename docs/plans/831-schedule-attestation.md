# #831 Schedule-Change Risk Remediation — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner decision 2026-11-11 (the issue's close gate): the #760 disposition
> does NOT cover the demonstrated case (NEW advice born wrong after a
> schedule change); remediate under **mechanism (a): schedule-fingerprint
> attestation on evidence rows**. Trade accepted: the sleep advisory goes
> unknown after every schedule change until evidence rebuilds (up to one
> reship/ingest cycle) — silence-over-wrong.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
HEAD past #827 (`3e4ea511`). Tracked as #831 (V16-04, P2).

**Recon facts (verified):**
- The defect is orthogonal to #826/#827: activity at 18:00–21:00 sits in
  the OLD BH leg; off-p95 and coverage gates never see it. The Appendix B
  reproducer (09:00–21:00 → 09:00–17:00 without re-ingest) still writes
  new high-confidence advice with outside=0 on current HEAD — confirmed
  by command as the RED.
- A reship machinery exists (`internal/reship`: pending flags, locks,
  poller, ReshipStatus/Since in the DB, triggered by the BH settings
  handler) but nothing references `hcp_sleep_evidence`, and the evaluator
  never consults the status — the unsafe window is schedule-change →
  reship completion, ungated.
- The writer splits at the CLUSTER-scope schedule (bhschedule.Cache);
  the evaluator resolves the same cluster-scoped row (resolveSleepSchedule)
  — both sides can compute the same fingerprint.
- #760/#826/#827 precedent: evidence-attestation column + unknown-on-
  mismatch + cold-start round + COUNT=COUNT(*) completeness rule.

**Mechanism:**
1. `internal/bhschedule`: one canonical `Fingerprint(timezone, days,
   startMin, endMin)` (sha256 over a canonical string: tz | sorted days |
   zero-padded HH:MM bounds). Both the writer (from the cache's Schedule)
   and the evaluator (from the resolved sleepSchedule) call THIS function.
2. Migration 000222: `hcp_sleep_evidence.schedule_fingerprint TEXT`
   (NULL = pre-fingerprint rows, cold-start round 4 → unattested).
   latestMigrationVersion 221→222.
3. Writer: stamp the split schedule's fingerprint on every row.
4. Server loader/predicate: loadSleepEvidence returns the window's
   attestation (COUNT(fingerprint = current) vs COUNT(*)); any row not
   matching the CURRENT schedule's fingerprint → the workload-day reads
   unknown (the #826 partial-migration rule, one level up). Gate is
   conservative: unknown, never zero-activity.
5. Offline: the CSV mode is inherently attested (legs and evaluation use
   the same current schedule) — unchanged. The DB mode reads the
   fingerprint column; SleepCandidateWithCoverage gains an attestation
   input (mismatch → nil); robne's DB wiring passes it.
6. No reship-status coupling (option (b) rejected as the primary gate:
   the reship→sleep-evidence link is unverified); the fingerprint is
   self-healing on re-ingest regardless of the reship path.

**Tests:**
- RED: the Appendix B schedule-change shape end-to-end (ingest 14 days
  under 09:00–21:00 with 2000m at 18:00–21:00; change to 09:00–17:00;
  run the cycle) → must NOT write advice. Baseline: high-confidence,
  outside=0.
- Change matrix: end-time (the RED), day-list (drop a day), timezone —
  each → unknown.
- Positive: unchanged schedule → fires (the quiet control).
- Recovery: after re-ingest under the new schedule → fires again.
- Cold start: NULL fingerprints → unknown.
- Partial: mixed fingerprints in one window → unknown.
- Offline: DB-mode mismatch → nil; CSV mode unaffected.
- Mutations: gate removed → RED fires; fingerprint ignores end_time →
  the end-time case fires; ANY-match instead of ALL-match → the partial
  test fires. Stash-rerun.

**Docs:** ADR-0348 §5 amendment (the attestation contract: fingerprint,
unknown-on-mismatch, cold-start round 4, the silence-over-wrong trade
with the owner decision); CHANGELOG Fixed entry.

**Non-goals:** no reship-status gate; no advisory invalidation/versioning
beyond new-advice suppression (already-issued advice surviving to expiry
remains the separately recorded #760-accepted lifecycle limitation); no
schedule-change UX changes.
