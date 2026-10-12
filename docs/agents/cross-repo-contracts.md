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

## Completion reports

### 2026-10-12 — Node-pressure signal, Tier B narrowed (GH #681)

Two-repo change (operator + backend), branch `pgarciaq-rosocp-superpowers-phase17`
in both. Operator joins `kube_node_status_condition` (Memory/Disk/PID, status
true) onto container identity via the CPU-requests carrier on(node), emitting
`node_pressure_{memory,disk,pid}` container columns (1 when pressured in window,
empty otherwise); backend parses (optional, absent stays 0), digests the worst
ordinal per node/day (`PressureOrdinal`: memory 3 > disk 2 > pid 1) into
`daily_node_digests.pressure_state` (migration `000213`), and notifies with
code **87** `NODE_PRESSURE` (WARNING) (migration `000214`) while excluding
pressured nodes from consolidation removal. B1 as build work withdrawn (Tier A
gate already covers labeled mgmt-plane namespaces); lease-loss out per #680.

| Surface | Verdict |
|---|---|
| Operator collection/RBAC/status (`koku-metrics-operator`) | **Complete.** Queries `ros:node_pressure_{memory,disk,pid}` + container columns (appended after `sm_active_avg`; existing adjacency untouched). No RBAC/CRD change. Collector suite green. |
| Ingest forwarding, masu (`koku/`) | **No change — verified.** Zero hits for `node_pressure`, `pressure_state`, `PressureState` under `koku/koku/masu/` and `koku/koku/`. |
| UI tabs, detail pages (`koku-ui-ros`) | **No change — verified.** `notification_codes?: number[]` (`recommendations.ts:95,944`); badge text resolves via catalog, so 87 flows automatically. |
| Cheat sheet + Bruno (`costmgmt-api-cheatsheet`) | **Follow-up (that repo).** Node codes line ends at 77 (also missing 79/84/85/86); needs 87 row + HTML/PDF rebuild per its Makefile. No new endpoint (Bruno: nothing to add). |
| IQE REST coverage (`iqe-ros-ocp-plugin`) | **No change — verified.** Node tests assert notification format only (`test_node_utilization.py:188-193`); no exact-equality code set for nodes (unlike containers). |
| Chart E2E (`cost-onprem-chart` `tests/suites/ros/`) | **Follow-up, explicitly gated.** Same fixture-first rule as Tier A. |
| Cross-repo questions (`~/dev/koku/AGENTS.md`) | **No change.** No auth, consumer, or topology change. |

### 2026-10-11 — Container crash-loop quality gate, Tier A (GH #78 spec, #677 exhibit)

Two-repo change (operator + backend), branch `pgarciaq-rosocp-superpowers-phase17`
in both. Operator emits `restart_count` (`ros:restart_count_container_sum`;
koku-metrics-operator `1d8ef04`); backend parses (`ae0eba80`), digests
`RestartCountSum` via migration `000211` (`87f9a47c`), and withholds
cost-engine downsize at threshold 3 (default) with notification code **86**
`UNSTABLE_CONTAINER` (WARNING) (`c15a0f53` + API-surface completion below).
Code note: the plan doc's 48 was reassigned to 86 — 48 is `VM_CRASH_LOOP`
(`internal/notifications/names.go:53`) and every code below 70 belongs to the
VM series (Task 4 ruling; plan doc amended). No Tier B (excluded by design §6).

Backend proof: `make test-short` exit 0 (76 packages ok, zero FAIL);
`librobne/engine/crashloop_fixture_e2e_test.go` drives the committed package
`librobne/engine/testdata/crashloop/` (168-row CSV + `manifest.json` +
`crash-fixture.tar.gz`) through CSV load → daily digests → engine → codes:
crash-a cost recs pinned at current with savings 0 and code 86; clean-b
downsizes (200→58mc, 524288→150733KiB) with no 86; oom-c cost held with codes
3+86 and performance mem upsized (1048576→1255032KiB, OOM bump intact).
`robne validate` ok (168 rows); `robne recommend --plugins container
--format json` reproduces the same hold/downsize numbers (container CLI DTOs
deliberately omit notification codes — `output.go` BH-sibling rule — so codes
are asserted in-engine and via the API catalog, not CLI stdout).

| Surface | Verdict |
|---|---|
| Operator collection/RBAC/status (`koku-metrics-operator`) | **Complete.** Query `ros:restart_count_container_sum` (`internal/collector/queries.go:118`), `restart_count` CSV column (`internal/collector/types.go:567,876,917`). No RBAC/CRD change (existing Prometheus query path). `go test -count=1 ./internal/collector/` ok (33s). |
| Ingest forwarding, masu (`koku/`) | **No change — verified.** Zero hits for `oom_count`, `restart_count`, or ROS container CSV names under `koku/koku/masu/` and `koku/koku/`; the pipeline never parses these columns. |
| UI tabs, detail pages (`koku-ui-ros`) | **No change — verified.** `notification_codes?: number[]` (`apps/koku-ui-ros/src/api/ros/recommendations.ts:95,944`); no per-code map in `locales/messages.ts` — badge text resolves via the `notification-codes` catalog, so code 86 flows automatically. |
| Cheat sheet + Bruno (`costmgmt-api-cheatsheet`) | **Follow-up (that repo).** Container codes table (`costmgmt-api-cheatsheet.adoc:842`) ends at 77; needs an `\| 86 \| WARNING \| Container restarted repeatedly…downsize withheld` row + HTML/PDF rebuild per its Makefile. No new endpoint. |
| IQE REST coverage (`iqe-ros-ocp-plugin`) | **Follow-up (that repo).** `CONTAINER_NOTIFICATION_CODES` (`iqe_ros_ocp/fixtures/constants.py:12`) lacks 86; catalog assertions fail against the new backend until updated. |
| Chart E2E (`cost-onprem-chart` `tests/suites/ros/`) | **Follow-up, explicitly gated.** Fixture-data-first: the committed `librobne/engine/testdata/crashloop/` package is input for a future crash-loop E2E (cf. `test_container_ingestion.py` pattern). |
| Cross-repo questions (`~/dev/koku/AGENTS.md`) | **No change.** No auth, consumer, or topology change. |

Incidental repair in this commit: `NotificationsToShow` also allowlists node
codes 84/85 (`INFO` per `mapping.go:105-106`) — the staleness guard
`TestNotificationsToShowCoversDefinitions` failed on clean HEAD for 84/85/86;
the 86 entry is this task's API-surface work, 84/85 restore the invariant.

Tracked follow-ups left for owners (not this task's scope):
- `docs-site/api-reference/notification-codes.md` (Class A, `Last verified:
  2026-09-20`): example still shows `"count": 82` and codes through 83 —
  truth is now 85 definitions through code 86. Add the 86 entry (plus 84/85
  from #670/#671), refresh the count, bump `Last verified`, run
  `make docs-lint docs-drift docs-sync-check`.
- `docs/architecture/notification-codes.md`: header still claims 82 codes;
  container rule table (line ~149) needs the 86 row
  (`restart_count_sum >= 3` in term window → downsize withheld).
