# #822 — Stream-Path Flush Fragment Replacement — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner decision 2026-11-14: **option (a)** — the stream paths stop
> honoring `ROS_INGEST_FLUSH_BATCH_SIZE`; the knob remains staged-path-only
> (where its flush is safe). Option (b) (disk staging for the stream paths)
> parked as a follow-up if a real deployment ever OOMs on the stream path.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `cd47d0ad`. Tracker: #857 (queue item 2).

## The verified mechanism (recon, 2026-11-14)

- The close criteria's empirical question: **YES, conditionally** — a
  container-day spans a flush boundary whenever the knob is set finite
  (the threshold check runs after appending the row, so the group whose
  first row crosses it flushes as a fragment; its remaining rows land in
  the next flush). With the default (`math.MaxInt32`), the mid-stream
  flush never fires — **no default-path exposure**.
- The upserts are replacement writes (`DO UPDATE SET x = EXCLUDED.x` —
  verified for digests and `hcp_sleep_evidence`).
- The staged path honors the same knob SAFELY (disk staging, key-
  transition finalize). Only the stream paths (container
  `pipeline_stream.go:178`, namespace `namespace_stream.go:183`) use it
  unsafely.
- The performance guide already documents the knob as a discouraged
  footgun ("lower only if the pod OOMs — unlikely: upstream file caps
  bound memory to ~22–115 MB").

## Mechanism (option a)

Remove the mid-stream flush from both stream paths: the row loop no
longer reads the threshold; `flushDigestGroupBatch` and its call sites
go; everything accumulates and flushes once at end-of-CSV (the default
behavior today). Side effect: `digestBatchesFlushed` stays 0 → the
single-ingest-tx path is eligible more often (an improvement).

## Tests

- **RED:** the knob set to a small finite value (e.g. 2) + a CSV whose
  container-day spans the would-be boundary — the digest must reflect
  ALL samples (p50 over the full set). Baseline: the mid-stream flush
  fires, the second fragment replaces the first → fragment-only p50.
- Namespace twin (same shape at its threshold).
- Controls: default config (no knob) unchanged; the staged path still
  honors the knob (existing staged tests).
- Mutation: restore the mid-stream flush → the RED fails.

## Docs

- The performance guide's knob row: staged-path-only.
- CHANGELOG Fixed entry.
- The issue's severity note: resolves P2-suspected → knob-conditional
  defect (no default-path exposure).

## Non-goals

Option (b) (parked); #836/#803; the staged path.
