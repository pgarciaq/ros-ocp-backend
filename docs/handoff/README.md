# Engineering Hand-Off Package — the `pgarciaq/ros-ocp-backend` fork

> **What this is:** the review package for Engineering colleagues taking
> over the fork that replaces `RedHatInsights/ros-ocp-backend`. Written
> 2026-11-14, at branch `pgarciaq-rosocp-superpowers-phase17` (commit
> `189e4201` and successors). Every claim is verified against a named
> source or explicitly labeled as an estimate.

## The one-paragraph version

Since March 2026 this fork rewrote ROS-OCP's recommendation engine from
the Kruize-derived pipeline into a native Go engine (exact percentile
math over staged PostgreSQL digests), built a serving API with keyset
pagination, term/engine projection, and tenant isolation, added the
robne offline CLI for parity, and contributed the supporting changes to
the sibling repos (nise, koku, koku-metrics-operator, the IQE plugins).
The work carries **348 ADRs, 222 migrations, 18 adversarial review
records**, and a test corpus of **83 packages green against real
PostgreSQL** — with the final integration review's 9 findings (1 P1
tenant-isolation blocker, 5 P2, 3 P3) all remediated and CI-verified
(tracker #849, closed 2026-11-14).

## Two audiences, two reading orders

**ROS engineers** — deciding whether replacing the legacy backend with
the native engine is the right call:

1. [The native engine case](native-engine-case.md) — what it is, why,
   the correctness evidence, the honest limits.
2. [Per-area summary](per-area-summary.md) — the fork's delta per
   subsystem, with the gotchas to probe.
3. [Known-issues register](known-issues-register.md) — what is
   consciously accepted or deferred.

**Cost Management (Koku) engineers** — reviewing the contributions to
your repos:

1. [Cross-repo contributions](cross-repo-contributions.md) — every
   fork-branch ahead of upstream in nise, koku, koku-metrics-operator,
   the IQE plugins, and the ported chart fixes; what each does and its
   review/CI status.
2. [Audit ledger](audit-ledger.md) — the scrutiny record, including the
   findings in *your* repos' changes.

Everyone: the [per-area summary](per-area-summary.md) and the
[known-issues register](known-issues-register.md) are the shared
context.

## Headline numbers (all verified 2026-11-14)

| Metric | Value | Source |
|---|---|---|
| Commits since 2026-03-01 (ros-ocp-backend) | 1,934 | `git log --since=2026-03-01` |
| ADRs | 348 (0001–0356) | `docs/adr/` |
| Migrations | 222 up (+ downs) | `schema_migrations` |
| Adversarial review records | 16 reviews + 1 residue adjudication (17 files) | `docs/audit/` |
| Integration-review findings remediated | 9 of 9 (tracker #849) | #840–#848 |
| Test packages green | 83 ok / 0 FAIL locally; CI runs 77 ok + 21 no-test packages | local `make test` logs; CI run 43505014874 |
| Scale-validated | up to ~84K containers (direct-to-MinIO mode; #518 tracks the live-like rerun) | `docs-site/operations/scale-benchmark-report.md` |
| Org-scale serving plan | identity-index-backed at 50K identities | #853 (EXPLAIN ANALYZE) |

## The fork strategy (owner-set, 2026-10-12)

The `pgarciaq/ros-ocp-backend` fork replaces upstream; Engineering
colleagues review the fork's changes since inception directly (this
package). No upstream PRs for ros-ocp-backend or iqe-ros-ocp-plugin.
Downstream contributions (koku, nise, iqe-cost-management-plugin, the
cheatsheet) follow those repos' normal processes. The
koku-metrics-operator future is a soft-split into
costmanagement-metrics-operator (koku queries) and
resourceoptimization-operator / robne-operator (ROS queries) generated
from one repo.
