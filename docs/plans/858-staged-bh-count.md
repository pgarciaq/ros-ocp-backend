# #858 — Staged-Path BH Sample-Count Accounting — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner decisions 2026-11-14: implement now (queue jump over #836 per the
> reachability finding); **pre-count + keep the streaming check**.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `257938e3`. Tracker: #857 (staged-path item, jumped ahead of #836).

## The verified mechanism (recon)

- `stageDigestGroups` stages `queued` rows (every input row →
  `groupedAll`; rows in an enabled BH schedule ALSO → `groupedBH` — a
  second staged row), but the bookkeeping adds
  `b.stagedSamples += b.memSamples` — the INPUT count. With BH
  enabled: `read` (actual staged rows) > `stagedSamples` → the #758
  integrity check hard-fails at finalize, AFTER digest chunks have
  committed (partial-commit shape) — deterministic on every retry.
- **Reachability:** BH groups count toward the staging threshold
  (`digestGroupCount = len(all) + len(bh)`), so the DEFAULT 8,192-group
  threshold crosses at ~4,096 container-days with BH — large-cluster
  shapes the benchmarks ran. Production-relevant, not knob-conditional.

## Mechanism (owner-approved)

1. **Accounting:** `b.stagedSamples += queued` (the actual staged
   rows); `memSamples` keeps its separate role (the 1M input-sample
   threshold).
2. **Pre-count:** a `SELECT count(*)` on the staging table for the
   manifest BEFORE the streaming finalize — staging loss fails before
   any digest commit. The streaming check STAYS as defense-in-depth
   (it catches loss occurring during finalize, which a pre-count
   cannot).

## Tests

- **RED (the committed repro):** a small staging threshold (knob=2) +
  BH enabled + rows in the BH window → staging fires → finalize fails
  with `staged sample count mismatch` on the current code. After the
  fix: succeeds, with correct all-hours AND business-hours digests.
- **Partial-commit property:** force a mismatch (delete a staged row
  post-staging) → finalize must fail with ZERO digest rows committed
  (the pre-count's property — the current code commits chunks first).
- **BH correctness control:** the BH digest values themselves.
- **Mutations:** the accounting reverted (+= memSamples) → the RED
  fails; the pre-count dropped → the partial-commit test fails.

## Non-goals

#836 (retry duplication); #803; the stream paths (fixed in #822).
