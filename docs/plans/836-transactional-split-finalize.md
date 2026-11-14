# #836 — Transactional Split-Archive Finalize (KMO) — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner instruction 2026-11-14: tackle now despite the KMO classification
> (tracker #857's deferred section). The mechanism per the issue's sketch:
> temp names + atomic set rename into upload/.

**Code repo:** ~/dev/koku/koku-metrics-operator, branch
`pgarciaq-rosocp-superpowers-phase17` (post the #829-era merges).
**Plan/campaign tracker:** ros-ocp-backend #857 (the KMO section's first item).

## The verified mechanism (recon, posted to #836)

The split branch of `PackageReports` calls `writeTarball` per part; a
failure partway leaves already-finalized tarballs in upload/. The
uploader (`filesToUpload` + the upload loop) uploads EVERYTHING in
upload/ (no prune/dedup; removal only on 202), and the retry packages
under a fresh timestamp + manifest UUID — both sets upload. Server-side
effects are mostly idempotent (keyed upserts); the worst concrete
effect is a duplicate cross-day history row (severity resolves toward
P3 — an exactly-once violation).

## Mechanism

**All-or-nothing finalize:** the split branch writes each part under
a temp name in a `upload/.partial/` subdirectory (invisible to the
flat `GetFiles` listing), and renames the completed set into upload/
only after every part finalized. A failure partway cleans the partial
directory (and a compensation loop removes any already-renamed finals
if a rename itself fails — a tiny window, defended). The upload
directory therefore never contains a partial split set.

## Tests

- **RED (the duplication repro):** a split packaging forced to fail
  partway (inject: pre-create a directory at the k+1-th part's path →
  os.Create fails), then a retry on the same staged inputs — the
  upload directory must contain ONLY the new full set. Baseline: the
  old partial set's tarballs are still present (and would upload).
- **Uploader assertion:** `filesToUpload`-equivalent listing of the
  post-retry upload dir returns only the new set.
- **Exactly-once tooling (close criterion 3):** extend the recovery
  test helpers to assert archive-set membership ACROSS multiple
  tarballs (the current `archiveMembersWithContent` requires exactly
  one archive).
- **Mutations:** the finalize reverted to direct writes → the RED
  fails; the compensation dropped → the rename-window test fails (if
  constructible; otherwise documented as defensive).

## Non-goals

The uploader's prune/dedup behavior beyond the partial-set fix (the
transactional finalize removes the partial-set class entirely); the
server-side idempotency surface (assessed, posted); #811/#809/#808.
