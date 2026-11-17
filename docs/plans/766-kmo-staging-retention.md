# #766 KMO Staging Retention — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step. Design dictated by #766's close criteria (owner go-ahead 2026-11-05); implementation in `~/dev/koku/koku-metrics-operator` (fork `pgarciaq`, branch `pgarciaq-rosocp-superpowers-phase17`), centrally tracked in ros-ocp-backend #766.

**Goal:** a failed packaging pass retains every report in a recoverable location; the retry includes already-staged pending reports plus new source reports; staging is removed only after successful finalized packaging.

**Mechanism (verified at HEAD, zero drift since `290c0515`):** two defects. (1) The controller's `packageFiles` resets `PackagingError = ""` before every retry; `moveOrCopyFiles` reads the empty string as "prior success" and CLEARS STAGING — deleting the only copy of reports the failed pass had already moved there. (2) Discovery reads only `os.ReadDir(reports/)` — staged-but-never-packaged files are invisible to the retry. Chain: A moves → B's move fails → retry resets → staging cleared (A destroyed) → discovery sees only B → archive = B only → success reported.

**Fix (the criteria's letter):**
1. Staging cleanup moves to the END of a successful `PackageReports` (after `writeTarball` + `LastSuccessfulPackagingTime`) — staging at pass entry is always pending input; the ambiguous error-signal check is deleted (the controller reset becomes harmless).
2. Discovery unions: `reports/` entries (moved to staging as today) + pre-existing staging entries, through the existing name filter (`*.csv` + `cluster_instance_types` json — already excludes stale `manifest.json`). `ErrNoReports` only when BOTH are empty. Mixed uid prefixes across passes are benign (ROS type matching and manifest are prefix-agnostic — verified).
3. Hardening within the "during archive completion" criterion: `writeTarball` writes to a temp name in upload and renames on completion (atomic archives; a failed attempt leaves no partial final-name tarball).

**Test matrix (all in `internal/packaging/packaging_recovery_test.go`, plain Go, private temp dirs):**
- RED: the audit's scenario — A+B, fault-inject the second move (via the existing `FilesAction` seam), replicate the controller wrapper's reset faithfully, retry with normal moves → assert A's content in the archive + manifest membership. Baseline: A deleted, archive = B only.
- Fault after moves / at archive creation (upload dir 0500): everything staged, archive fails → retry (perms restored) → full set packaged; no partial archive in upload.
- Retry with a NEW report C between passes → archive = A, B, C.
- Order-reversal control (fault the FIRST move → nothing stranded → retry packages both).
- Pass-1 stranded-state assertion (A in staging only — the audit's observed intermediate).

**Constraints:** KMO-only scope; no changes to the upload/trim paths; the controller wrapper unchanged (its reset becomes harmless — verify, don't redesign). Commits wait for owner go-ahead.
