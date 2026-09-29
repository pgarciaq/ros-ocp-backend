# Cross-repo contracts (2026-09-29, #638 lesson)

Adjacent repositories are first-class deliverables, not dismissals. When a
feature/fix touches a surface another repo consumes or documents, evaluate
each row and either complete it or record a tracked follow-up — never wave
it away with "adjacent repo" or "backend-only".

## Rules

1. **Verify, then dismiss.** A dismissal needs a named reason tied to evidence
   ("no new API fields were added", "existing suite X covers this flow at
   lines …"), never a location ("adjacent repo", "backend-only"). "Backend-only"
   is banned as an E2E/IQE rationale unless you name the covering suite.
2. **Skill-table rows default to do.** If a completion-report row names an
   artifact that exists anywhere in the workspace, produce the update or file
   the tracker. Absence must be verified (list the directory), not assumed.

## Where things live

| Surface | Repo | What goes there |
|---|---|---|
| Operator collection/RBAC/status | `~/dev/koku/koku-metrics-operator/` | Queries, ClusterRole, CRD bases, controller tests |
| Ingest forwarding (masu) | `~/dev/koku/koku/` (`koku/masu/`) | Ship/enrich paths; verify, don't assume pass-through |
| UI tabs, detail pages | `~/dev/koku/koku-ui/` (`koku-ui-ros`) | Tracked via pointer issues; implementation lands there |
| Cheat sheet + Bruno | `~/dev/koku/costmgmt-api-cheatsheet/` | Endpoint subsections + `.bru` requests; rebuild HTML/PDF per its Makefile |
| IQE REST coverage | `~/dev/koku/iqe-ros-ocp-plugin/` | Endpoint/filter entries in the REST suites |
| Chart E2E | `~/dev/koku/cost-onprem-chart/` (`tests/suites/ros/`) | Needs fixture data first; gate explicitly |
| Cross-repo questions | `~/dev/koku/AGENTS.md` | Ecosystem hub (auth, consumers, topology) |

## Scope discipline

One branch name across repos per phase (`pgarciaq-rosocp-superpowers-phase17`
pattern). Commits/pushes are per-repo and need explicit scope each time —
"commit and push" without a qualifier covers every repo holding changes, so
confirm scope rather than assuming ros-ocp-backend-only.
