# Adversarial Due Diligence Review — ROS ecosystem, phase17

## Version & Date

Version: 15 | Date: 2026-11-01 | Reviewer: AI-assisted, GPT-6.1 Sol High

**Verdict: BLOCK deployment readiness pending authorization remediation.**

**Remediation parent:** [#780](https://github.com/pgarciaq/ros-ocp-backend/issues/780).
The 29 finding trackers are linked in the summary and individual findings below;
the remediation index at the end also records their implementation repositories
and priorities.

Fresh Scope A review across eight dimensions. This appends to the historical
[v14 review](adversarial-review-v14-2026-09-06.md); it does not rewrite earlier
findings or reclassify their accepted risks without new evidence.

## Executive Summary

This review validates **29 findings: 5 High, 20 Medium, 4 Low; no Critical
finding demonstrated**. Four High findings are ROS authorization failures:
read-only settings requests mutate state after returning 403; several readers
ignore explicit resource restrictions; settings-only roles gain infrastructure
read access; and heatmap cache hits bypass node restrictions. The fifth High
finding is Nise's unconditional disabling of TLS verification for uploads that
carry Basic credentials or bearer tokens. These are demonstrated mechanisms,
not evidence of exploitation in a deployed environment. No cross-organization
data disclosure or gateway-identity forgery was demonstrated.

Correctness risks concentrate at seams: Kafka partition ordering/commit
watermarks, same-manifest digest fan-in, VM companion dependencies, advisory
evidence and identity, operator row keys and packaging recovery, Koku provider
resolution/snapshots, and generated fixture fidelity. Several green test suites
miss these defects. A real PostgreSQL reproduction combines the identity of one
management cluster with another cluster's API-tax evidence. Raw hourly samples
also produce high-confidence sleep advice for a workload active overnight.
The sleep defects are advisory-only today; this review does not claim automatic
scale-to-zero or an observed customer outage.

There are substantial strengths: streaming bounded downloads, explicit tenant
arguments, SQL parameterization/allowlists in sampled paths, nested-module
testing, structured metrics, image smoke builds, extensive ADRs, and verified
prior remediations. Those controls do not establish readiness while the new
authorization and ingestion failures remain. The earlier docs dedup work also
introduced two demonstrated regressions, explicitly included rather than
excused as reviewer-authored changes.

## Scope, baselines, and trust model

All repositories were on `pgarciaq-rosocp-superpowers-phase17`:

| Repository | Reviewed commit | Scope |
|---|---|---|
| ros-ocp-backend | `3061a76f64c4db30756f4e7a1d48f6814276618e` | Full non-vendor repository in scope; source, tests, config, migrations, CLI/nested library, docs/tooling |
| koku-metrics-operator | `290c05155ca611a84939fa210539ec193d8ed792` | Full non-vendor repository in scope, including committed CRD/RBAC/bundle contracts |
| koku | `926c76feec25b209a0b46fabe58ec2fe4f621040` | Committed delta from `b82cdef2d6405c5e28195f4aaaf0feebd4bf8c10`: 56 files, +3,987/−36, plus necessary callers/callees |
| nise | `e2e1b9b2771d401cfdd78ab3d85407dec1ffb2de` | Full tracked non-vendor application, generators, tests, fixtures, packaging/config |

The Koku base is the merge-base with the locally available `origin/main`; no
claim is made that that remote-tracking ref was freshly fetched. Existing Koku
`docker-compose.yml` edits and untracked CSV/screenshots were excluded and left
untouched. All four HEADs were rechecked after investigation.

`vendor/` was excluded everywhere, including vendored librobne. Dependency
manifests and CI configuration remained in scope; ordinary builds necessarily
link dependencies, which is not an audit of dependency source. Virtual
environments, disposable build outputs, and local artifacts were not treated as
product source. Clean tracked code/config counts were approximately 239,519
lines for ROS, 40,791 for KMO, and 37,215 for Nise. Full Koku has about 421,271
tracked code/config lines, but only its stated delta was reviewed. Earlier
filesystem-based cost estimates that included local artifacts are not coverage
measurements.

Six bounded parallel assignments covered API/auth/model reads, computation and
correlation, pipeline/lifecycle, KMO, Koku delta, and Nise. The coordinator
reviewed CI/governance/docs and independently checked the selected findings,
including rerunning evidence tests. **Full repository scope does not mean an
exhaustive independent read of every line or a security certification.** Deep
inspection was risk-based; uncovered cases are recorded below.

Data flow checked: KMO/Nise CSV and manifests → ingress/Koku shipping → Kafka
and object URLs → ROS parsing/digests → recommendation/advisory persistence →
HTTP reads; Koku separately supplies rates, tags, cost groups, and reship.
ROS uses Go/Echo/pgx/GORM and a nested librobne module; KMO uses Go,
controller-runtime and Prometheus clients; Koku uses Django/DRF, tenant schemas
and Celery; Nise is a Python generator/CLI.

Both SaaS and on-prem contracts were considered, **without live-cluster
inspection or deployment changes**. Legitimate gateway-produced `x-rh-identity`
and authoritative RBAC responses are assumed for user APIs. Internal Masu and
platform service-account cross-org authority were not mislabeled as new public
auth bypasses. Local tests used disposable databases, mock Kafka/HTTP boundaries
or local HTTP servers, never the running development/application database.

## Scorecard

Qualitative judgments, not benchmark scores or guarantees:

| Dimension | Rating | Key gap / evidence |
|---|---|---|
| Security | Blocked | Four reproduced ROS authorization failures; authenticated Nise uploads disable certificate verification |
| Correctness | Needs remediation | Lost/misattributed measurements, stale savings, unsafe advisory predicates and incomplete fixture contracts |
| Auditability | Needs remediation | HTTP 403 after mutation; files/query hours marked successful despite missing data; packaging can report success after loss |
| Operational robustness | Needs remediation | Offset gaps, destructive packaging retry and an unbounded optional Prometheus path |
| Performance | Not load-validated | Query limits/timeouts and streaming controls inspected; no new benchmark/EXPLAIN-backed performance finding or scale assurance |
| Design quality | Mixed | Good explicit plugin contracts, but optional authorization and identity/aggregation boundaries are not consistently enforced |
| Maintainability | Mixed | Broad tests/ADRs, but realistic multi-source/multi-tenant fixtures and generated-doc integration are missing |
| Governance | Controls present, incomplete | Prior fixes and checks exist; phase17 full CI is red and passing docs gates missed new content/link regressions |

## Findings Status Summary

All entries below are **Open — verified mechanism, no fix applied**. Confidence
is high in each stated mechanism; live incidence and full remediation scope are
not established merely by a reproduction. No new issue was filed during audit
execution; subsequent owner-authorized remediation trackers are listed below.

| # | Title | Severity | Primary dimension | Repo | GitHub issue |
|---|---|---|---|---|---|
| V15-01 | Forbidden settings requests still mutate state after 403 | High | Security | ROS | [#751](https://github.com/pgarciaq/ros-ocp-backend/issues/751) |
| V15-02 | Resource readers omit explicit RBAC scope | High | Security | ROS | [#752](https://github.com/pgarciaq/ros-ocp-backend/issues/752) |
| V15-03 | Settings-only permissions become infrastructure read access | High | Security | ROS | [#753](https://github.com/pgarciaq/ros-ocp-backend/issues/753) |
| V15-04 | Heatmap cache ignores node authorization scope | High | Security | ROS | [#754](https://github.com/pgarciaq/ros-ocp-backend/issues/754) |
| V15-05 | Authenticated uploads disable TLS verification | High | Security | Nise | [#755](https://github.com/pgarciaq/ros-ocp-backend/issues/755) |
| V15-06 | Kafka worker mutexes do not preserve partition order | Medium | Operational robustness | ROS | [#756](https://github.com/pgarciaq/ros-ocp-backend/issues/756) |
| V15-07 | VM companions finish before usage parents exist | Medium | Correctness | ROS | [#757](https://github.com/pgarciaq/ros-ocp-backend/issues/757) |
| V15-08 | Split container files overwrite same-day observations | Medium | Correctness | ROS | [#758](https://github.com/pgarciaq/ros-ocp-backend/issues/758) |
| V15-09 | Weighted-digest pool handoff races with reading Max | Medium | Correctness | ROS/librobne | [#759](https://github.com/pgarciaq/ros-ocp-backend/issues/759) |
| V15-10 | Subtracting medians falsely establishes sleep safety | Medium | Correctness | ROS/librobne | [#760](https://github.com/pgarciaq/ros-ocp-backend/issues/760) |
| V15-11 | Night-only workloads are skipped by sleep coverage | Medium | Correctness | ROS/librobne | [#761](https://github.com/pgarciaq/ros-ocp-backend/issues/761) |
| V15-12 | Headroom correlation drops SLO source identity | Medium | Correctness | ROS/librobne | [#762](https://github.com/pgarciaq/ros-ocp-backend/issues/762) |
| V15-13 | API-tax upsert merges different management clusters | Medium | Correctness | ROS | [#763](https://github.com/pgarciaq/ros-ocp-backend/issues/763) |
| V15-14 | Sorted label values collapse workload identities | Medium | Correctness | KMO | [#764](https://github.com/pgarciaq/ros-ocp-backend/issues/764) |
| V15-15 | Namespace quota merge loses measured usage | Medium | Correctness | KMO | [#765](https://github.com/pgarciaq/ros-ocp-backend/issues/765) |
| V15-16 | Packaging retry deletes unarchived staging data | Medium | Operational robustness | KMO | [#766](https://github.com/pgarciaq/ros-ocp-backend/issues/766) |
| V15-17 | HCP capacity queries bypass configured deadlines | Medium | Operational robustness | KMO | [#767](https://github.com/pgarciaq/ros-ocp-backend/issues/767) |
| V15-18 | Namespace opt-in failure skips independent collectors | Medium | Correctness | KMO | [#768](https://github.com/pgarciaq/ros-ocp-backend/issues/768) |
| V15-19 | Savings callback sends provider ID as cluster ID | Medium | Correctness | Koku | [#769](https://github.com/pgarciaq/ros-ocp-backend/issues/769) |
| V15-20 | Global latest-day tag snapshot clears other clusters | Medium | Correctness | Koku ↔ ROS | [#770](https://github.com/pgarciaq/ros-ocp-backend/issues/770) |
| V15-21 | Rates resolver selects another customer's provider | Medium | Correctness | Koku ↔ ROS | [#771](https://github.com/pgarciaq/ros-ocp-backend/issues/771) |
| V15-22 | Packaged Nise reports lose recognized file-family names | Medium | Correctness | Nise ↔ ROS | [#772](https://github.com/pgarciaq/ros-ocp-backend/issues/772) |
| V15-23 | Namespace usage is averaged across pods, not aggregated | Medium | Correctness | Nise | [#773](https://github.com/pgarciaq/ros-ocp-backend/issues/773) |
| V15-24 | Static CRQ normalization silently discards supplied fields | Medium | Correctness | Nise | [#774](https://github.com/pgarciaq/ros-ocp-backend/issues/774) |
| V15-25 | Same pod name in different namespaces overwrites a workload | Medium | Correctness | Nise | [#775](https://github.com/pgarciaq/ros-ocp-backend/issues/775) |
| V15-26 | Manifest reship uses wrong S3 metadata-key casing | Low | Correctness | Koku | [#776](https://github.com/pgarciaq/ros-ocp-backend/issues/776) |
| V15-27 | Docs build overwrites canonical limitations with a stub | Low | Maintainability | ROS | [#777](https://github.com/pgarciaq/ros-ocp-backend/issues/777) |
| V15-28 | Dedup links resolve relative to wrong directories | Low | Maintainability | ROS | [#778](https://github.com/pgarciaq/ros-ocp-backend/issues/778) |
| V15-29 | Calendar-dependent fixtures leave full CI red | Low | Governance | ROS | [#779](https://github.com/pgarciaq/ros-ocp-backend/issues/779) |

Severity follows the requested impact-based definitions. In particular,
same-org resource-boundary disclosure is still a confidentiality failure;
provider mis-selection without demonstrated foreign monetary-data disclosure is
not called a security breach. Advisory-only defects and crash-dependent offset
loss are Medium, not claims of automatic destructive action or a current
fleet-wide outage. Effort below is an estimate, not an approved design.

## Findings Detail

Paths in each location are relative to the named repository. `E/` below means
the session-local evidence directory
`/tmp/opencode/phase17-adversarial-review/`. Evidence files are not installed
tests and are not silently part of repository CI. Captured failure text is
included here so the findings do not depend solely on retaining temporary logs.

### V15-01 — Forbidden settings requests still mutate state after 403

**Tracker:** [#751](https://github.com/pgarciaq/ros-ocp-backend/issues/751).

**High · Security / integrity, auditability · ROS · Effort M**

**Location:** `internal/api/settings_rbac.go:35-42`;
`handlers_snapshot_settings.go:44-73`; 20 guard consumers found by usage grep.

`requireSettingsWrite` returns the result of `c.JSON(403, ...)`, which is nil
when the response write succeeds. Callers stop only on a non-nil error, so the
denied request continues into its mutation. The independently read snapshot
engine DELETE has no second authorization guard.

**Risk / proof:** a legitimate read-only, non-admin caller deleted a seeded
org settings row: `status=403 ... remaining_settings_rows=0`. The coordinator
reproduced this using real Identity/entitlement/RBAC middleware and handler
against disposable PostgreSQL (`E/coordinator-api-repro.log`). Business-hours
pruning/reship fan-out is additionally reachable by source inspection, not
separately executed. HTTP status-only tests wrongly suggest protection.

**Recommendation:** make denial return an explicit non-nil authorization
sentinel or an explicit stop result; test unchanged persistence and absent
side effects across all consumers. Preserve the existing 403 contract. Distinct
from #546's upstream-error semantics and #738/#742's lock/require-all policy.

**Remediation follow-up — 2026-11-01 (uncommitted candidate, not closed):**
the approved #751 repair now preserves denial JSON/write errors and returns a
non-nil authorization error. Local unit, 54 routed PostgreSQL persistence/effect
cases and 34 OpenAPI-operation contracts pass; selecting the original guard
restores the named mutation failures. Independent pre-commit security review
found no production-fix blockers and independently reproduced RED/GREEN. Its
IQE unrelated-403 false-positive finding was addressed by exact ROS JSON
assertions, verified against gateway/lock/HTML fault injection and the real local
HTTP/PostgreSQL contract. IQE, cheat-sheet/Bruno and generated HTML/PDF changes
remain uncommitted in their owning repositories. Full Go tests remain red on
nine original-baseline-reproducible failures (seven compatibility tests and two
#779 retention fixtures). **Isolated SNO verification:** on 2026-11-01, two
actual IQE plugin tests using real non-admin users, a test-only Keycloak instance,
an isolated Envoy gateway and real Insights RBAC failed against the original
guard on the named PUT/DELETE state mutations, then passed against the rebuilt
fixed arm64 image. The test stack used a distinct ROS database, even though it
reused the lab org ID; the shared ROS settings count remained unchanged. This
does not certify the shared SNO's existing realm/gateway, SaaS, or TLS security:
the test reproduced the SNO's permissive JWKS TLS setting. The full-suite
failures and owner-approved commit/push/closure remain outstanding. This
follow-up does not alter historical audit evidence or resolve other authorization
findings.

### V15-02 — Registered resource readers ignore explicit RBAC scope

**Tracker:** [#752](https://github.com/pgarciaq/ros-ocp-backend/issues/752).

**High · Security / confidentiality · ROS · Effort M–L**

**Location:** `internal/api/handlers_pvc.go:84-173`, `handlers_pvc_group.go:36-53`,
`handlers_quota_recs.go:161-237,361-380`,
`handlers_cluster_quota_recs.go:85-228,343-360`,
`handlers_hcp_advisories.go:150-219`, `handlers_oom_timeline.go:19-73` and
`internal/model/oom_timeline.go:38-78`.

Generic RBAC middleware admits recognized permissions and attaches their scope;
it does not filter SQL. These readers use org scoping without consuming explicit
cluster restrictions. A legitimate cluster-A-only role can enumerate cluster B's
storage/quota groups and advisories, and read a known B container's OOM timeline.

**Risk / proof:** actual requests returned B's PVC capacity `987654321` and
savings `123.45 USD`, quota resources, an excluded management-cluster advisory,
and seven OOM events (`E/coordinator-api-repro.log`). No UUID collision, forged
identity or foreign org is needed. Production registrations and complete callees
were checked. PVC/quota/CRQ detail and quota trend have the same omission by
source read but were not individually runtime-seeded.

**Recommendation:** apply server-owned resource scope to list/count/group/CSV
and resolved-ID detail/history paths; test two distinct clusters in one org,
wildcards, empty grants and foreign-org negatives. Do not substitute a client
filter for authorization. This is not the historical alias-join or PVC-key issue.

### V15-03 — Settings-only roles acquire OpenShift data access

**Tracker:** [#753](https://github.com/pgarciaq/ros-ocp-backend/issues/753).

**High · Security / confidentiality · ROS · Effort M**

**Location:** `internal/api/middleware/rbac.go:84-123`;
`handlers_node_recs.go:849-873`; `handlers_fleet.go:27-39`;
`internal/model/recommendation_set_native.go:676-701`;
`internal/rbac/query_builder.go:82-114`;
`internal/model/native_ns_list_keys.go:209-230`.

The middleware now admits settings ACLs, but data helpers treat an absent
OpenShift grant as unrestricted access. Missing child grants can intentionally
inherit an affirmative parent grant; having no OpenShift grant at all is not
such inheritance. Koku's independent permission lattice starts resources empty
and grants inheritance only from a real parent permission.

**Risk / proof:** the same node-hourly request hides B from an A-scoped reader,
but serves B's CPU `617`, memory `123456`, and pod count `42` to a settings-read-only
role. Coordinator reproduction uses the real middleware and database. Wider
native/compat/fleet fan-out is source-verified, not individually reproduced.

**Recommendation:** establish affirmative OpenShift-read eligibility separately
from settings-route admission, preserving intentional cluster→node→project
inheritance. Cover settings-only read/write, wildcard and explicit empty roles.
This differs from a forgotten helper call: the exercised endpoint calls the
helper, and the helper itself grants too much.

### V15-04 — Heatmap cache key omits node authorization scope

**Tracker:** [#754](https://github.com/pgarciaq/ros-ocp-backend/issues/754).

**High · Security / confidentiality · ROS · Effort S–M**

**Location:** `internal/api/handlers_fleet_heatmap.go:136-142,239-250,303-305`;
`handlers_fleet.go:27-39`; `internal/fleetsummary/cache.go:180-194`.

Cache scoping considers cluster restrictions only. A wildcard-cluster role with
an explicit narrow node grant gets the same `org:all` key as an unrestricted
role. A hit returns before the otherwise-correct node filter. Explicit child
restrictions are supported by the existing node-scope helper and Koku's
independent permission inheritance behavior.

**Risk / proof:** after warming through the real handler, the restricted caller
received `secret-node` and `visible-node` (`count=2`); after invalidation, the
identical caller received only `visible-node` (`count=1`). Coordinator rerun:
`E/coordinator-api-repro.log`. Names, topology, utilization and savings can
cross the intra-org node boundary. The org remains in the key.

**Recommendation:** include every authorization dimension affecting the response
or hash its canonical effective visibility scope. Test both warm orders; do not
filter a shared cached object in place. Closed #148 fixed client cluster filters,
not this node-permission dimension.

### V15-05 — Authenticated Nise uploads disable TLS certificate verification

**Tracker:** [#755](https://github.com/pgarciaq/ros-ocp-backend/issues/755).

**High · Security / credential confidentiality and report integrity · Nise · Effort S**

**Location:** `nise/report.py:370-385` (`verify=False` at 376 and 384).

Both Basic-auth and bearer upload branches unconditionally disable server
certificate verification, including for public HTTPS endpoints. OAuth token
acquisition verifies TLS, but the upload carrying the acquired token does not.
No explicit insecure-lab decision is required.

**Risk / proof:** a network-positioned attacker able to substitute a certificate
can receive the password/token and alter reports. The real route function,
mocking only transport, emitted `auth=('test-user','test-password') verify=False`;
the coordinator repeated the probe (`E/coordinator-nise-repro.log`). Actual
interception was not attempted, and no real credentials were sent.

**Recommendation:** verify certificates by default, support a configured CA,
and require explicit warned opt-in for insecure lab use. Test both auth modes.
This is pre-existing code (upstream upload-retry change), but no matching accepted
risk or dedicated current tracker was found. A private-CA lab does not justify
an unconditional insecure SaaS path.

### V15-06 — Kafka partition mutexes do not preserve offset order

**Tracker:** [#756](https://github.com/pgarciaq/ros-ocp-backend/issues/756).

**Medium · Operational robustness / delivery correctness · ROS · Effort M**

**Location:** `internal/kafka/consumer.go:146-201`; `commit.go:18-30`;
`internal/services/report_processor.go:269-278`; default workers in
`internal/config/config.go:799-800`.

Workers dequeue FIFO messages before competing for a per-partition mutex.
Mutual exclusion does not preserve dequeue/offset order. A higher offset can
finish and commit while lower offsets have not run; serialization of commit
calls does not provide a contiguous-completion watermark.

**Risk / proof:** the coordinator observed an inversion `trace=[6 7 8 1 2 9]`.
An independent actual CommitMessage test with an in-process librdkafka mock
broker verified that committing offset 2 stores next-offset 3, acknowledging
unprocessed 0/1 as well (`E/coordinator-kafka-order.log`). Crash/rebalance in that
gap can skip reports; replacement writes can also execute stale-after-fresh.
No live production loss incident is asserted.

**Recommendation:** reader-assigned ordered partition queues with cross-partition
parallelism, or explicit sequencing plus contiguous commits and replacement-write
order. Test a deliberately blocked lower offset, rebalance/shutdown, and actual
handler order. ADR-0154 promises ordering; a global commit mutex is insufficient.

### V15-07 — VM companions are acknowledged before usage parents are persisted

**Tracker:** [#757](https://github.com/pgarciaq/ros-ocp-backend/issues/757).

**Medium · Correctness / silent input loss · ROS · Effort M**

**Location:** `internal/services/parallel_ingest.go:183-240`;
`internal/ingestion/vm_digest_builder.go:648-694`;
`vm_pvc_db.go:124-147`; `vm_gpu_device_db.go:107-125`.

Usage files accumulate until batch Flush; companion AddFile calls query the
database immediately. For a new VM/day, no parent exists even if usage is first.
Companions skip missing parents successfully; Flush then writes the parent and
all files become done, so same-manifest replay does not repair the loss.

**Risk / proof:** actual service-path PostgreSQL tests produced, on both first
attempt and replay, `done=3 parent=1 pvc=0 gpu=0`
(`E/coordinator-pipeline-services.log`). Consumers prefer real PVC/device evidence
but fall back to proxies/aggregates without it. The claim is lost per-attachment
fidelity, not that all aggregate GPU data disappears.

**Recommendation:** merge and persist usage parents first, then ingest companions
and acknowledge dependency completion. Test fresh days, both file orders,
failure/replay and later full replacement. Sorting usage URLs alone is not a fix.
Regression adjacent to #707; its original multi-file usage tests still pass.

### V15-08 — Split container files replace the same day's earlier observations

**Tracker:** [#758](https://github.com/pgarciaq/ros-ocp-backend/issues/758).

**Medium · Correctness / recommendation fidelity · ROS · Effort M–L**

**Location:** `internal/services/parallel_ingest.go:77-122,307-317`;
`internal/ingestion/pipeline_stream.go:175-300`;
`pipeline_business_hours.go:118-130`; `librobne/pgdigest/write.go:159-191`.

Each file builds a separate daily accumulator and replaces the same digest key
on conflict. A manifest containing disjoint portions of one container/day loses
the earlier completed portion even though both files are done. Count-based
producer splits do not guarantee container/day boundaries.

**Risk / proof:** actual service processing of 2000m then 100m observations gave
`manifest done=2 sample_count=1 p95_mc=100`, rather than two samples and the high
point (`E/coordinator-pipeline-services.log`). Lower-level reversed-order tests
still lose a sample; only which value survives changes. Undersizing is possible
when the high segment is overwritten.

**Recommendation:** combine a manifest's observations before its daily replacement,
or use lossless staging. Preserve replay/idempotency and later fuller-payload
replacement. Adding counts or taking maxima of file percentiles cannot reconstruct
the distribution. Distinct from intra-file #264 and scheduling coupling #708.

### V15-09 — Weighted-digest scratch handoff races with reading Max

**Tracker:** [#759](https://github.com/pgarciaq/ros-ocp-backend/issues/759).

**Medium · Correctness / concurrent data corruption · ROS/librobne · Effort S**

**Location:** `librobne/digest/digest.go:118-122,168-177,234`.

`ComputeWeightedDigest` returns its backing buffer to sync.Pool before reading
`pairs[pn-1].v` into Max. A concurrent owner can refill/sort that storage. Other
percentiles were already copied into locals; Max was not. Parallel ingest and
the offline digest builder both call this function, and callers persist maxima.

**Risk / proof:** the coordinator reran the unmodified implementation with a
concurrent overlay test: `WARNING: DATA RACE`, write at line 122 against read at
177 (`E/coordinator-weighted-race.log`). An explicitly separate scheduling-only
Gosched amplifier observed `Max=300` with `P95=400` for constant 400 samples;
that amplifier is not substituted for the original-code race proof. A deployed
wrong persisted row or incidence rate was not observed.

**Recommendation:** copy Max or construct the whole result before relinquishing
buffer ownership, with a concurrent race regression. No accuracy/performance
trade or pool rewrite is needed.

### V15-10 — Subtracting all-hours and BH medians falsely proves safe sleep

**Tracker:** [#760](https://github.com/pgarciaq/ros-ocp-backend/issues/760).

**Medium · Correctness / unsafe advisory · ROS/librobne · Effort M–L**

**Location:** `internal/engine/correlate/sleep.go:127-129,195-206`;
`librobne/advisory/sleep.go:61-84,163-174`.

The rule subtracts sums of p50s as if they were additive usage totals. A workload
using 300m constantly has all-hours p50=300 and BH p50=300, so the computed
off-hours value is zero despite continuous overnight activity. This contradicts
ADR-0348's overnight-safety promise even though its formula records the same
mistake; changing to another percentile does not make subtraction valid.

**Risk / proof:** real raw hourly rows through canonical digest calculators
produced high-confidence `schedule_nonprod_sleep` with
`outside_bh_peak_cpu_mc:0`; the actual server predicate also returned
`match=true known=true`. Coordinator logs: `coordinator-advisory-repro.log` and
`coordinator-server-sleep.log` under E. The seven-day schedule removes weekend
coverage ambiguity. Tests seeding `all=BH+outside` cannot detect the math defect.

**Recommendation:** approve an actual off-hours evidence design with explicit
coverage; until supported, withhold the sleep-safety claim. Update ADR and both
evaluators using raw-sample regressions. Preserve existing all-hours recommendation
persistence and BH-detail contracts. No autonomous shutdown was exercised.

### V15-11 — Night-only workloads are ignored rather than vetoing sleep

**Tracker:** [#761](https://github.com/pgarciaq/ros-ocp-backend/issues/761).

**Medium · Correctness / missing-evidence handling · ROS/librobne · Effort S–M**

**Location:** `internal/engine/correlate/sleep.go:187-191`;
`librobne/advisory/sleep.go:155-159`.

The predicate skips a workload if either stream is missing. A night-only CronJob
normally has all-hours evidence and no positive-weight BH digest; another
workload can establish the day's coverage/activity gates. The night workload is
therefore invisible to the outside-hours safety test.

**Risk / proof:** raw rows produced `{BH:0 All:2000 BHMeasured:false AllMeasured:true}`
for the night job, yet emitted high-confidence sleep advice. Coordinator library
and server-predicate reruns confirmed it. Producers independently omit all-zero-BH
groups, so the input is normal, not malformed. This remains after fixing percentile
subtraction because the workload is skipped before that calculation.

**Recommendation:** establish sufficient evidence per user workload before a
sibling establishes whole-day coverage; missing BH for an all-hours workload is
unknown/a veto, not an ignored row. Add mixed-coverage regressions and coordinate
the conservative gate with V15-10's design.

### V15-12 — Headroom correlation drops independent SLO source identity

**Tracker:** [#762](https://github.com/pgarciaq/ros-ocp-backend/issues/762).

**Medium · Correctness / false or suppressed advisory · ROS/librobne · Effort S**

**Location:** `internal/engine/correlate/evaluate.go:59-66,91-99`;
`librobne/advisory/headroom.go:100-106`; `advisory/quantile.go:151-160`.

Storage and producer contracts preserve histogram source, but the correlator
discards it and chooses latest samples by verb/boundary alone. Non-gating
metrics-server samples can replace Kubernetes samples or mix incompatible
bucket schemas. The owner-locked [#644 source amendment](https://github.com/pgarciaq/ros-ocp-backend/issues/644#issuecomment-5917243306)
requires H to gate on `job=kubernetes`; other jobs are recorded, non-gating.

**Risk / proof:** calm Kubernetes-only input does not fire; adding a coherent
metrics-server source one minute later produces `do_not_add_workers_first`,
`Confidence:high`, p99 `2.98` against threshold `0.3`
(`E/coordinator-advisory-repro.log`). Server source omission is independently
verified by SQL/body read, not a second live-server test.

**Recommendation:** apply the locked source policy before dropping source in
both server and offline assembly, with staggered mixed-schema tests. A later
policy reversal would require coherent per-source aggregation, not newest-boundary
wins.

### V15-13 — API-tax upsert overwrites and misattributes different management clusters

**Tracker:** [#763](https://github.com/pgarciaq/ros-ocp-backend/issues/763).

**Medium · Correctness / identity and evidence integrity · ROS · Effort M**

**Location:** `internal/engine/correlate/apitax.go:144-155`;
`migrations/000203_hcp_correlation_advisories.up.sql:7-20`.

All API-tax rows use an empty hosted-cluster ID, and the conflict key excludes
management cluster. Two farms in one org/hour therefore conflict. The UPDATE
replaces evidence but not management identity, mixing the first farm's UUID
with the second farm's webhook facts. The API serves both fields together.

**Risk / proof:** the coordinator ran the actual writer against fully migrated
disposable PostgreSQL. Two independent management clusters left one row:
`identity=11111111-1111-4111-8111-111111111111 evidence=[webhook:second.example ...]`.
The expected-two-row test failed with `got 1`
(`E/coordinator-apitax-pg.log`). This upgrades the reviewer's initial SQLite
mechanism check to real PostgreSQL evidence; it is still not a deployed incident.

**Recommendation:** define/migrate advisory identity to preserve management scope
and the accepted per-HC/window invariant, update all writer conflict targets,
and test two same-org/same-window farms. Updating the UUID alone still loses a
row. This requires a schema/contract review, not a blind key edit.

### V15-14 — Sorted label values collapse distinct workload identities

**Tracker:** [#764](https://github.com/pgarciaq/ros-ocp-backend/issues/764).

**Medium · Correctness / corrupt attribution · KMO · Effort M**

**Location:** `internal/collector/collector.go:126-137,139-194`;
production VM query caller `vm_ros_queries.go:15-35`.

The composite key sorts label values, destroying their roles. VM alpha in
namespace beta and VM beta in namespace alpha on the same node collapse into
one key. Independent query-result orders can then combine request fields from
one object with usage from the other.

**Risk / proof:** operator tests yield one row with alpha/beta identity,
alpha's request `1000` and beta's usage `900` instead of alpha's `100`.
The canonical ROS parser accepts it (`accepted=1 skipped=0`) and preserves that
misattribution in its digest. Coordinator reran producer and consumer probes:
`E/coordinator-operator-collector.log`, `coordinator-operator-consumer.log`.

**Recommendation:** preserve named positions in an unambiguous composite key and
survey reconstructed-key callers before changing the helper. Test swapped labels,
reversed query orders and instant/range paths. Distinct from fixed #503's GPU join.

### V15-15 — Namespace quota merge drops the usage it intends to retain

**Tracker:** [#765](https://github.com/pgarciaq/ros-ocp-backend/issues/765).

**Medium · Correctness / input loss · KMO · Effort S–M**

**Location:** `internal/collector/namespace_merge.go:19-23,41-58`;
`collector.go:108-113,752`.

The usage map contains `*rosNamespaceRow`, but the merge asserts the non-pointer
type, so the copy never happens. Named quota rows contain no measured usage;
the original namespace row may also be suppressed. ROS requires measured CPU
and memory and skips the resulting row.

**Risk / proof:** a namespace with actual CPU `7` and memory `1024` yields an
empty-usage quota row. The canonical parser accepts the control (`1/0`) but
skips the quota row (`accepted=0 skipped=1`), losing named quota inputs.
Coordinator reran both lexicographic quota/name cases and the consumer. The
demonstrated positional-order case does not depend on V15-14's key collision.

**Recommendation:** copy the real stored row type and derive quota identity
from named fields. Test both name orders and parsed usage/ResourceQuota survival,
not only pivoted hard/used values.

### V15-16 — Packaging retry deletes unarchived staged reports

**Tracker:** [#766](https://github.com/pgarciaq/ros-ocp-backend/issues/766).

**Medium · Operational robustness / data loss · KMO · Effort M**

**Location:** `internal/controller/costmanagementmetricsconfig_controller.go:564-570`;
`internal/packaging/packaging.go:458-501,566-573`.

After one MoveFiles action succeeds and a later move fails, the first report
exists only in staging. The controller resets PackagingError before retrying;
the packager then interprets the cleared value as prior success and deletes
staging. It packages only the remaining source report and records success.

**Risk / proof:** the actual controller wrapper/packager with only the second
move fault-injected lost report A and emitted an archive containing only B.
Coordinator rerun: `E/coordinator-operator-controller.log`. A's original and
an archive copy were both absent; this is not just delayed upload.

**Recommendation:** preserve failure provenance and repackage all pending staged
inputs before deletion/finalization. Removing the reset alone is insufficient
because filesToPackage currently scans new reports only. Cover move and archive
completion failures and recovery.

### V15-17 — HCP capacity queries bypass configured timeouts

**Tracker:** [#767](https://github.com/pgarciaq/ros-ocp-backend/issues/767).

**Medium · Operational robustness / reconcile stall · KMO · Effort S–M**

**Location:** `internal/collector/hcp_capacity_collector.go:523-556,643-664`;
client construction `internal/collector/prometheus.go:114-131`.

The optional synchronous hook uses context.Background for direct Prometheus
queries, bypassing ContextTimeout; its actual HTTP client has no overall timeout.
A response stall can prevent packaging, status updates and requeue, while Ping
health probes stay green. Default reconcile concurrency is one.

**Risk / proof:** a local HTTP server held the real client beyond its schema-valid
10-second budget; completion required the test's external release at roughly
10.15 seconds (`E/coordinator-operator-collector.log`). Production has no such
release/deadline. This is not #729's fixed Kubernetes informer wait.

**Recommendation:** propagate cancellation and a bounded budget through these
per-HCP/reference queries, preserving best-effort degradation. Do not place a
short topology-only deadline on the whole reconcile. A server-side Thanos query
timeout cannot bound every proxy/network response stall.

### V15-18 — No namespace opt-in skips independent collectors and cost VM output

**Tracker:** [#768](https://github.com/pgarciaq/ros-ocp-backend/issues/768).

**Medium · Correctness / persistent missing hourly data · KMO · Effort S**

**Location:** `internal/collector/collector.go:296-340`;
controller sentinel handling `internal/controller/prometheus.go:184-188`.

The no-enabled-namespaces sentinel returns before SLO, API-tax, NodePool and
capacity hooks, despite their explicit opt-in-independent contracts. Deferred
Cost Management VM output is skipped too. The controller treats this sentinel
as query success and advances the hour rather than retrying it.

**Risk / proof:** the full GenerateReports probe changes only namespace opt-in:
disabled gives `cost VM CSV=false`, `NodePool calls=0`; enabled gives `true` and
`1`. Running VM ROS data exists in both cases. Coordinator rerun is in
`E/coordinator-operator-collector.log`. This does not assert every healthy HCP
management cluster lacks opt-in; that prerequisite matters.

**Recommendation:** finish independent hooks/deferred cost output before returning
the container opt-in outcome, or represent outcomes independently. Pin all
independent hook calls and cost VM output with opt-in absent.

### V15-19 — Savings callback sends provider UUID as OpenShift cluster UUID

**Tracker:** [#769](https://github.com/pgarciaq/ros-ocp-backend/issues/769).

**Medium · Correctness / stale monetary estimates · Koku · Effort S**

**Location:** `koku/masu/processor/ros_savings_recalc.py:69-70`;
caller `ocp/ocp_cost_model_cost_updater.py:911-912`.

The callback puts provider_uuid into cluster_uuid, although the updater already
knows a distinct OpenShift cluster ID. ROS accepts the request and uses that
string verbatim for rate-cache invalidation and recommendation filtering; no
provider→cluster translation exists at the consumer.

**Risk / proof:** AST-loading the exact committed notifier emitted provider
`2222…` for actual cluster `bbbb…`. Real caller and ROS handler/selector were
independently read; the coordinator reran the isolated mechanism script
(`E/koku-repro.py`). A 202 can conceal stale savings after a rate update. Existing
tests assert the wrong mapping.

**Recommendation:** send the resolved cluster ID, retain provider ID separately
if needed, and use distinct IDs in producer/consumer contract tests. Not #467's
rate flattening or #452's deployment wiring. No live cost-model update was run.

### V15-20 — Global latest-day tag selection clears other clusters

**Tracker:** [#770](https://github.com/pgarciaq/ros-ocp-backend/issues/770).

**Medium · Correctness / disappearing filtered recommendations · Koku ↔ ROS · Effort M**

**Location:** Koku `koku/masu/processor/ros_tag_sync.py:82-85,108-118`;
ROS `internal/tags/sync.go:94-105,123-150`.

A tenant-wide MAX(usage_start) restricts the entire replacement snapshot to one
date. Valid clusters with staggered same-month uploads disappear from the
payload. ROS intentionally clears all org tags before applying that incomplete
snapshot, and its real query filters then exclude the still-current workloads.

**Risk / proof:** Oct 30/31 fixtures for two clusters emit only the newer cluster
and its `staging` value; a same-day control emits both. The coordinator executed
the committed builder against ORM doubles and separately read the full-replace
SQL and per-provider scheduling. This is an isolated mechanism check, not a live
database sync; the downstream clearing consequence follows from actual SQL.

**Recommendation:** agree snapshot freshness/deletion semantics, then include the
latest applicable evidence for every relevant cluster/namespace. Do not simply
widen the scan and silently resurrect intentionally deleted tags. Add skewed-day
and namespace-gap fixtures. Periodic repeats do not repair the selection rule.

### V15-21 — Effective-rates resolver chooses another customer's provider

**Tracker:** [#771](https://github.com/pgarciaq/ros-ocp-backend/issues/771).

**Medium · Correctness / tenant-qualified identity resolution · Koku ↔ ROS · Effort S**

**Location:** `koku/masu/api/effective_rates.py:36-43,173-183,209-215`.

The endpoint validates the requested org but resolves cluster ID by scanning
shared authentication rows and selecting the first provider without a customer
predicate. Koku explicitly supports the same cluster ID across customers and
reuses matching authentication objects. The requested tenant's cost-model lookup
then receives the foreign provider ID; ROS reship trusts that returned ID.

**Risk / proof:** the exact resolver/view with two providers sharing auth returns
200 for org B but selects A's provider and queries `(orgB, providerA)`.
Coordinator rerun: `E/koku-repro.py`; the prerequisite is independently pinned
by `api/provider/test/test_serializers.py:545-593`. Rates/defaults and historical
BH reship can be wrong/empty. Namespace aggregation remains tenant-scoped;
no foreign monetary-data disclosure is claimed.

**Recommendation:** resolve using both the validated customer and cluster ID;
test shared-auth/colliding-cluster providers with distinct names/rates and the
reship URL. A global uniqueness assumption is contradicted by existing tests.

### V15-22 — Nise packaging erases report-family names

**Tracker:** [#772](https://github.com/pgarciaq/ros-ocp-backend/issues/772).

**Medium · Correctness / silently missing validation evidence · Nise ↔ ROS · Effort M**

**Location:** Nise `nise/report.py:1036-1047,1088-1155`;
ROS `internal/utils/utils.go:486-554`,
`internal/services/parallel_ingest.go:299-304`.

Monthly HCP/NodePool/SLO/API-tax files have correct family names, but packaging
renames them to generic openshift_report.N.csv; container/companion and split
paths also encounter generic naming. Koku forwards the names without restoring
families. Server dispatch marks unknown files done without ingestion. Offline
generic-container header fallback does not recover the HCP families.

**Risk / proof:** a fresh binary built from the pinned ROS source validates 24
capacity rows in the raw monthly directory, but the same generated/extracted
payload fails with `no capacity snapshot in this payload`. The coordinator
reran Nise's network-disabled generation and current-binary validation. Fake
no-advisory tests may pass because evidence disappeared, not because a rule
correctly stayed silent.

**Recommendation:** preserve recognized family names through normal and split
packaging and test manifest routing plus actual consumer row counts. A documented
monthly-container workaround already exists; this finding does not claim the
generic-name problem was wholly unknown. New HCP consequences affect #727/#728.

### V15-23 — Namespace usage averages across pods rather than aggregating them

**Tracker:** [#773](https://github.com/pgarciaq/ros-ocp-backend/issues/773).

**Medium · Correctness / misleading sizing fixtures · Nise · Effort M**

**Location:** `nise/generators/ocp/ocp_generator.py:2392-2408,2432-2453`;
independent KMO namespace query contract `internal/collector/queries.go:197-205`.

Nise sums requests but averages usage across pods and takes a single-pod maximum.
The operator reports time statistics of concurrent namespace sums. The namespace
consumer cannot recover the missing total from the emitted aggregate row.

**Risk / proof:** two constant 0.5-core/1-GiB pods emit namespace usage 0.5 cores/
1 GiB instead of 1 core/2 GiB. A current ROS binary recommends `575m/1205862KiB`
from that file versus `1150m/2411725KiB` from an aggregate-correct control.
Equal-valued pods deliberately defeat MAX/distinct-value masking. Coordinator
generation and current-binary outputs are retained under E.

**Recommendation:** first fix the exact simultaneous-constant case and specify
how time-varying samples are coordinated. Independent maxima cannot generally
reconstruct max(sum over time); do not silently trade statistical accuracy for
a cheap aggregation. Add operator-semantic fixture contracts.

### V15-24 — Static ClusterResourceQuota normalization drops supplied fields

**Tracker:** [#774](https://github.com/pgarciaq/ros-ocp-backend/issues/774).

**Medium · Correctness / false test scenarios · Nise · Effort S**

**Location:** `nise/generators/ocp/ocp_generator.py:1354-1379`,
later row defaults at `949-961`; bundled `ocp_report_cluster_quota.yml:20-40`.

The normalizer copies only CPU/memory fields while the row builder supports
storage, pod/object quotas and namespace membership. Supplied static values
silently disappear and are replaced by invented low-use defaults.

**Risk / proof:** supplied pod hard/used `1000/990` becomes `50/20`; object hard
`2000` becomes `100`; real namespace `demo` becomes `namespace-1,namespace-2`.
The current ROS consumer accepts the file and emits a low-risk tighten result
with pods recommended `22`, not the intended high-utilization case. Coordinator
reran the real generator and rebuilt consumer. Bundled non-default fixtures
exhibit the same normalization loss.

**Recommendation:** preserve every supported field through YAML normalization,
settle byte/GiB aliases, and test full YAML→generator→consumer behavior. Helper-only
row-builder tests bypass the lossy stage.

### V15-25 — Cross-namespace pod-name collisions erase a workload

**Tracker:** [#775](https://github.com/pgarciaq/ros-ocp-backend/issues/775).

**Medium · Correctness / fixture data loss · Nise · Effort M**

**Location:** `nise/generators/ocp/ocp_generator.py:1551-1563,2264-2270,2347-2363`.

Pod and ROS maps key only by pod_name, but Kubernetes uniqueness is namespace-scoped.
The second namespace's same-named pod overwrites the first before cost/ROS row
emission. The real ROS digest identity would preserve the namespace if it received
both workloads.

**Risk / proof:** valid `a/web` and `b/web` produce only `b/web`, 96 rows rather
than 192. Current-binary recommendation output contains namespace B only.
Repeated HCP component names across hosted namespaces are a relevant shape;
absence-based tests can turn falsely green.

**Recommendation:** carry namespace-qualified identity through maps and GPU/PVC
lookups while retaining raw CSV names; test same-name multi-namespace objects
with distinct measurements. Do not work around the bug by renaming fixtures.
Related to namespace-list #254 but distinct from that fixed storage layer.

### V15-26 — Targeted manifest reship mismatches S3 metadata casing

**Tracker:** [#776](https://github.com/pgarciaq/ros-ocp-backend/issues/776).

**Low · Correctness / targeted recovery · Koku · Effort S**

**Location:** `koku/masu/api/reship_ros.py:152-164`, especially 162;
writer `koku/masu/external/ros_report_shipper.py:125-131`.

S3 stores user metadata keys lowercase, but the filter looks up `ManifestId`
rather than `manifestid`. A matching manifest's objects are rejected and the
endpoint returns successful zero-file recovery. The DB manifest-ID value itself
is not the mismatch.

**Risk / proof:** the real filter with `Metadata={"manifestid":"42"}` returns
zero keys; a mixed-case control returns one. The coordinator reran it and
independently checked the [AWS metadata contract](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingMetadata.html#UserMetadata).
The active ROS client uses date-range reship, not manifest_id; ordinary automatic
BH reship is therefore not claimed broken by this finding.

**Recommendation:** normalize/lookup lowercase metadata and add a targeted-manifest
case with standard S3 casing. No live S3 request or compatibility-store casing
observation was performed.

### V15-27 — Docs build overwrites the canonical limitations page with a stub

**Tracker:** [#777](https://github.com/pgarciaq/ros-ocp-backend/issues/777).

**Low · Maintainability / published-content correctness · ROS · Effort S**

**Location:** `scripts/generate-docs.sh:81-94`;
`.github/workflows/docs.yml:48-58`; `docs/known-issues.md:3-15`.

The dedup changed the internal file into a pointer, but the generator still
copies it over the full canonical docs-site page before MkDocs builds. Rewriting
the pointer produces a self-link and deletes the limitations table in the
generated site artifact. This is a new reviewer-authored #748 regression, not
an accepted instance of historical #417.

**Risk / proof:** running the actual generator in an isolated copy exits 0 but
changes the page from 66 lines with limitations to 15 lines without them and a
`known-issues.md` self-link (`E/docs-generation-repro.log`). Existing docs gates
pass. The live published page was not fetched during this local-only review.

**Recommendation:** remove the stale known-issues source-copy stage/comments
while preserving the other intended generators; add a generation-preserves-
canonical-content regression. A raw pre-generation mkdocs check alone would
still miss this pipeline failure.

### V15-28 — Dedup relative links point to nonexistent directories

**Tracker:** [#778](https://github.com/pgarciaq/ros-ocp-backend/issues/778).

**Low · Maintainability / broken maintained navigation · ROS · Effort S**

**Location:** `docs/upgrade-runbook.md:3,14`; `docs/README.md:25,28`;
`docs/features/container-recommendations.md:230`, plus the same pattern in 13
other maintained files (21 occurrences across 16 files total).

Several repoints omit a required `../`, while the upgrade stub has one too many.
For example, docs/README's docs-site/... resolves to docs/docs-site, a feature's
../docs-site resolves there too, and the upgrade pointer escapes the repository.
Some root stubs also mislink the agents policy directory.

**Risk / proof:** `E/docs-link-repro.py` compares newly introduced links since
ec0da9c4 and checks source-parent-relative paths, skipping fenced/external links:
`TOTAL: 21 new broken link occurrences in 16 files`. Exact paths are in its log.
The docs-site-only linter and pair-existence checker both pass, so they never
verified these internal pointers. This is another #748 regression.

**Recommendation:** compute each target relative to the source parent, validate
stub/index/maintained-feature links, and deliberately exclude historical trees
from rewrite scope. Merely checking the target exists somewhere in the repo is
not link-resolution verification.

### V15-29 — Calendar-sensitive integration fixtures leave full CI red

**Tracker:** [#779](https://github.com/pgarciaq/ros-ocp-backend/issues/779).

**Low · Governance / reliable regression signal · ROS · Effort S–M**

**Location:** `internal/api/handlers_quality_subtypes_test.go:174-182`;
`internal/engine/correlate/apitax_test.go:23-24,60-62`;
`internal/engine/retention_history_trait_test.go:51-69,107-127`.

Quality/API-tax fixtures insert previous-month hours while ensuring only a
current/start-month partition. Retention fixtures assume that a two-month-old
monthly partition lies wholly outside 45 days; on November 1, September contains
both old and still-retained dates, so the production whole-partition policy
correctly keeps it and the tests incorrectly require its removal.

**Risk / proof:** full [CI run 41260456220](https://github.com/pgarciaq/ros-ocp-backend/actions/runs/41260456220)
at pinned HEAD failed. The coordinator reproduced all three fixture mechanisms
locally with disposable testcontainers: SQLSTATE 23514 for both hourly cases,
and the two September-partition assertions failing in retention tests.
Logs: `quality-fixture-repro.log`, `apitax-fixture-repro.log`,
`retention-fixture-repro.log` under E. Short tests pass because these DB tests
are skipped. Other compat-list failures also occurred in CI, but their root
cause is **not verified** here and is not attributed to this calendar defect.

**Recommendation:** ensure every fixture timestamp's partition, inject/control
the clock where appropriate, and choose partitions whose entire ranges satisfy
the intended cutoff. Run day/month/retention boundary cases without weakening
assertions or changing correct production retention to satisfy a faulty test.
Distinct from #749's operator envtest midnight flake.

## Verification and evidence ledger

All selected mechanisms were challenged against actual callers/callees or an
independent contract. The coordinator repeated every reviewer slice's selected
evidence probes. Not all proof types have equal breadth:

| Check | Observed result | What it does not prove |
|---|---|---|
| `env GOMAXPROCS=4 GOFLAGS=-p=2 make test-short` | Passed root and nested-module package tests (`E/ros-test-short.log`) | Skipped PostgreSQL integration tests did not run |
| `make docs-lint docs-drift docs-sync-check` | Exit 0 (`E/docs-checks.log`) | Does not validate internal links or canonical-page preservation after generation |
| API middleware/handler probes on isolated PostgreSQL | Eight cases reproduce forbidden writes/reads/cache leakage | Minimal schemas are not a full migration or gateway deployment test |
| Kafka ordering + real CommitMessage/mock broker | Actual offset inversion; higher commit stores next-offset beyond gap | No production crash/rebalance incident observed |
| VM/container manifest probes on migrated PostgreSQL | Missing companions and clobbered same-day samples | Not all entity/file-family combinations tested |
| Canonical weighted-digest `-race` probe | Race detected in original source | Scheduling-amplified numerical output is separate; production incidence unmeasured |
| Sleep/headroom raw-input overlays | Unsafe sleep and non-gating-source false advisory; zombie p95/both-stream control passes | Headroom SQL source omission verified statically, not a full server query run |
| API-tax actual writer on migrated PostgreSQL | One row, first identity plus second evidence | Remediation migration/all-writer compatibility not implemented |
| KMO evidence tests and canonical ROS parser probes | All five selected mechanisms reproduced | Observation tests PASS when they detect bad behavior; not a fixed-code green suite |
| KMO existing targeted tests/selftests | Collector/topology/client tests and bundle/helper checks passed | No full envtest, image build or live rollout |
| Nise full unittest discovery in isolated tracked copy | `Ran 542 tests ... OK` (`E/nise-tests.log`) | Existing tests miss the five reproduced contract/security defects |
| Nise generation + freshly built current ROS CLI | Packaging loses HCP discovery; namespace recommendations differ 2×; quota/pod identity defects survive current consumer | Initial reviewer used an older local binary; coordinator rebuilt current source before confirming these consequences |
| Koku exact committed function bodies via AST | Four mechanisms reproduced with ORM/HTTP/S3 boundary doubles | No Django/PostgreSQL/S3/broker integration suite; decorators/middleware were not exercised by this script |
| Read-only GitHub history/status checks | Prior tracker states and source-policy amendment checked; full CI failure inspected | A closed issue alone is not evidence that its implementation remains correct |

Representative reproduction commands, from the indicated repository:

```bash
# ros-ocp-backend: actual server/API observations; set API_REVIEW_DSN to a NEW
# disposable local database, never an application/development database.
go test -mod=readonly -overlay "$E/api-overlay.json" ./internal/api \
  -run '^TestAPIAdversarial' -count=1 -v
go test -mod=readonly -overlay "$E/pipeline-overlay.json" ./internal/services \
  -run '^TestPipelineReview(VMManifestCompanions|ContainerManifestSplitDay)$' -count=1 -v
go test -mod=readonly -overlay "$E/pipeline-overlay.json" ./internal/kafka \
  -run 'TestPipelineReview(Higher|Partition)' -count=1 -v
go test -mod=readonly -overlay "$E/coordinator-apitax-overlay.json" \
  ./internal/engine/correlate -run '^TestAuditAPITaxClusterIdentity$' -count=1 -v

# ros-ocp-backend/librobne: original implementation, no scheduling amplifier
go test -race -overlay "$E/compute-lib-overlay.json" ./advisory \
  -run '^TestReviewWeightedDigestScratchMaxRace$' -count=1 -v
go test -overlay "$E/compute-lib-overlay.json" ./advisory \
  -run '^TestReview(Sleep|Headroom|Zombie)' -count=1 -v

# koku-metrics-operator
go test -mod=readonly -overlay "$E/operator-overlay.json" ./internal/collector \
  -run '^TestOperatorEvidence' -count=1 -v
go test -mod=readonly -overlay "$E/operator-overlay.json" ./internal/controller \
  -run '^TestOperatorEvidence' -count=1 -v

# Dependency-free committed Koku function mechanism checks
python3 "$E/koku-repro.py"
```

Set `E=/tmp/opencode/phase17-adversarial-review`. Overlays contain checkout-local
absolute paths; adjust them when reproducing elsewhere. Defect-asserting probes
may PASS while expected-good regression probes FAIL on this unfixed HEAD.
Read the named assertions/output, not merely the exit status. These tests have
not been installed as repository regressions or mutation-checked against fixes.

## Prior Findings Status

**Prior remediation sampled, not rediscovered:** RBAC deep-copy/error/truncation/
limiter fixes (#543/#545/#546/#547); safe DSN/CA installation (#544/#549);
atomic bounded malformed-JSON reporting (#553); reship error observability
(#534); CSV skip counters (#504); PVC/quota merge/no-org-rewrite fixes (#505/#508);
image smoke/UBI runtime fixes (#558/#562); and explicit workflow exceptions
(#563). Source and relevant tests were read; this is not a fresh deployment
attestation for every prior issue. Closed tracker state was independently
recorded in `E/prior-issue-status.json`.

Specific positive controls remain important:

- #746/#747: zombie uses p95 and sums both streams; the new control produces
  activity 14m from 7m+7m and correctly vetoes idle classification.
- #707: existing PostgreSQL tests for VM multi-file usage merging, subsequent
  replacement and failure isolation still pass. V15-07 exposes a different gap.
- #729/#739: uncached bounded topology reads and current RBAC coverage/selftests
  are present. V15-17 is the separate unbounded Prometheus hook.
- #502/#503: Nise physical-GPU cardinality and KMO's GPU join fixes remain
  present; their distinct identity issues above are not those fixed bugs.

## Accepted Risks

**Existing accepted/deferred risks — not new findings and not newly accepted by
this review:** v13's trusted-local CLI decompression/cancellation boundaries and
physically implausible integer overflow cases; #511 PVC unique-key tenant
extension; #513 compat cleanup; #725 CLI multi-payload generalizations;
#738/#742 require-all/env-lock policy; #708 ingestion/scheduling coupling;
#461/#462/#467 money/FX/rate-shape work; #452 SaaS integration wiring; #720
MachineSet investigation; #636 deepcopy provenance; #749 operator test flake;
#740's upstream-link trigger. Their owner-defined disposition is unchanged.

## Rejected Hypotheses

**Rejected mechanisms:** unsigned identity as a public bypass despite a trusted
gateway; internal cross-org authority as inherently unauthorized; allowlisted
SQL concatenation as automatically injectable; absent optional API-tax counts
as necessarily malformed; filename-independent offline loading as equivalent
to server dispatch; and a failed VM parser's partial accumulator as a production
write when its caller aborts before Flush. No severity was promoted merely
because multiple reviewers noticed the same class.

## Priority Remediation Order

1. **Immediate — authorization/credential protection:** V15-01, V15-04,
   V15-03, V15-02, V15-05. Start with the small settings-stop/caching fixes,
   but do not let them substitute for the full affirmative-scope audit.
   Allocate roughly S–M each; V15-02's full reader fan-out is M–L. Confirm
   deployed RBAC/auth-mode exposure separately; do not test forbidden writes on
   live customer policy as a reproduction shortcut.
2. **Short-term — delivery and measurement integrity:** V15-06/07/08/09,
   V15-14/15/16/17/18, and V15-19/20/21. Preserve ordering, replay and
   replacement contracts; qualify identifiers before writes/callbacks. Most
   are S–M; lossless multi-file container aggregation is M–L.
3. **Short-term — advisory trust:** V15-10/11/12/13. Consider a conservative
   no-advice behavior until true off-hours evidence is available. V15-10 needs
   an approved math/storage design (M–L), not another percentile substitution;
   V15-13 needs coordinated migration/all-writer tests (M).
4. **Short-term — trustworthy validation inputs:** V15-22/23/24/25, mostly
   S–M. Verify packaged names and numeric/identity semantics at the actual
   consumers; golden headers and helper-only tests are insufficient.
5. **Medium-term / backlog — targeted recovery and governance:** V15-26 (S),
   V15-27/28 (S), V15-29 (S–M). Fix the reviewer-authored docs regressions
   promptly despite Low runtime severity, and restore a reliable full-CI signal.

These are proposed remediation boundaries, not permission to implement, commit,
push, broaden schemas or automatically file issues. A security/tenant/migration
fix requires pre-commit adversarial review and mutation-checked regressions.

## Remaining Gaps and Limitations

- No live SaaS/on-prem verification, deployment/image build, full cluster E2E,
  penetration test, broker rebalance storm, full race suite, vulnerability database
  scan, FIPS validation or license-compliance attestation. Dependency source was
  intentionally excluded. No assurance that every possible auth path is safe.
- No large-org EXPLAIN/ANALYZE, load run, memory profile or measured complexity
  regression. Performance has coverage notes, not a clean production-scale verdict.
- The Koku mechanism tests do not establish framework/ORM/S3 integration behavior;
  follow with disposable two-customer and skewed-summary-date DB contracts.
- Extra API child-project/node restrictions, currency/cache combinations, broad
  model breadth, manifest recommendation phase ordering, other entity split-file
  behavior, and shutdown/cancellation details remain candidates for targeted
  follow-up, not additional validated findings.
- Container compat-list failures in the inspected CI run were not root-caused;
  V15-29 does not explain away that red evidence as one fixture issue.
- The independent #741 chart suite is not a result of this review and was not
  polled or used as proof of correctness. No successful full-stack run is claimed.
- The soft $100 checkpoint was an approximate planning boundary, not an enforced
  provider billing cap. Reliable task-level usage telemetry was unavailable;
  this report does not certify actual spend or the earlier token-cost estimate.

## Current State

| State | Count |
|---|---:|
| New validated findings | 29 |
| Critical | 0 demonstrated |
| High | 5 |
| Medium | 20 |
| Low | 4 |
| Resolved by this review | 0 |
| Newly accepted risks | 0 |
| Open new findings | 29 |

The supporting code/log bundle is retained at
`/tmp/opencode/phase17-adversarial-review/phase17-review-evidence.tar.gz` (session
artifact, not a tracked dependency of the report). The report retains key proof
outputs and exact source locations; retain the bundle before cleaning temporary
files if executable reproduction artifacts are needed later. Audit-owned local
PostgreSQL containers were removed after verification; the coordinator checked
their recorded test-session labels before removing its five testcontainers.

During audit execution, output was limited to the report and temporary evidence.
**No product fixes, commits, pushes, GitHub issue/comment changes, or live-cluster
mutations were made during that review.** Subsequent tracker creation is recorded
separately below.
Pre-existing Koku work remains unchanged; the only intended repository change
from this audit is this report. Historical reviews remain intact.

## Remediation Trackers — Owner-Authorized Follow-up

Coordination parent: [#780 — Track v15 adversarial review remediation across ROS,
KMO, Koku and Nise](https://github.com/pgarciaq/ros-ocp-backend/issues/780).
All 29 findings are native sub-issues there, grouped and ordered by the approved
recommendation. The parent records ownership/coordination rules, unresolved design
gates, verification requirements and explicit risk-disposition/close criteria;
it is not an authorization to implement or a replacement for individual evidence.

After the review, the owner requested one actionable GitHub issue for every
finding, centrally in `pgarciaq/ros-ocp-backend`. All 29 were created on
2026-11-01 and verified remotely as **open**, with exact expected titles,
problem/evidence/reproduction bodies, severity/effort/area/repository labels,
and separate proposed-direction/cross-repository handoff comments. No existing
issue body was overwritten and no fix was implemented by this filing step.

The issues are self-contained: temporary proof paths are explicitly not required
handoff dependencies. Unresolved evidence, schema and freshness design choices
remain approval gates, not silently settled mechanisms. Related work is linked
as coordination, not automatically imposed blocking dependencies.

| Finding | Tracker | Implementation repository | Severity |
|---|---|---|---|
| V15-01 | [#751](https://github.com/pgarciaq/ros-ocp-backend/issues/751) | ros-ocp-backend | P1 |
| V15-02 | [#752](https://github.com/pgarciaq/ros-ocp-backend/issues/752) | ros-ocp-backend | P1 |
| V15-03 | [#753](https://github.com/pgarciaq/ros-ocp-backend/issues/753) | ros-ocp-backend | P1 |
| V15-04 | [#754](https://github.com/pgarciaq/ros-ocp-backend/issues/754) | ros-ocp-backend | P1 |
| V15-05 | [#755](https://github.com/pgarciaq/ros-ocp-backend/issues/755) | nise | P1 |
| V15-06 | [#756](https://github.com/pgarciaq/ros-ocp-backend/issues/756) | ros-ocp-backend | P2 |
| V15-07 | [#757](https://github.com/pgarciaq/ros-ocp-backend/issues/757) | ros-ocp-backend | P2 |
| V15-08 | [#758](https://github.com/pgarciaq/ros-ocp-backend/issues/758) | ros-ocp-backend | P2 |
| V15-09 | [#759](https://github.com/pgarciaq/ros-ocp-backend/issues/759) | ros-ocp-backend | P2 |
| V15-10 | [#760](https://github.com/pgarciaq/ros-ocp-backend/issues/760) | ros-ocp-backend | P2 |
| V15-11 | [#761](https://github.com/pgarciaq/ros-ocp-backend/issues/761) | ros-ocp-backend | P2 |
| V15-12 | [#762](https://github.com/pgarciaq/ros-ocp-backend/issues/762) | ros-ocp-backend | P2 |
| V15-13 | [#763](https://github.com/pgarciaq/ros-ocp-backend/issues/763) | ros-ocp-backend | P2 |
| V15-14 | [#764](https://github.com/pgarciaq/ros-ocp-backend/issues/764) | koku-metrics-operator | P2 |
| V15-15 | [#765](https://github.com/pgarciaq/ros-ocp-backend/issues/765) | koku-metrics-operator | P2 |
| V15-16 | [#766](https://github.com/pgarciaq/ros-ocp-backend/issues/766) | koku-metrics-operator | P2 |
| V15-17 | [#767](https://github.com/pgarciaq/ros-ocp-backend/issues/767) | koku-metrics-operator | P2 |
| V15-18 | [#768](https://github.com/pgarciaq/ros-ocp-backend/issues/768) | koku-metrics-operator | P2 |
| V15-19 | [#769](https://github.com/pgarciaq/ros-ocp-backend/issues/769) | koku | P2 |
| V15-20 | [#770](https://github.com/pgarciaq/ros-ocp-backend/issues/770) | koku | P2 |
| V15-21 | [#771](https://github.com/pgarciaq/ros-ocp-backend/issues/771) | koku | P2 |
| V15-22 | [#772](https://github.com/pgarciaq/ros-ocp-backend/issues/772) | nise | P2 |
| V15-23 | [#773](https://github.com/pgarciaq/ros-ocp-backend/issues/773) | nise | P2 |
| V15-24 | [#774](https://github.com/pgarciaq/ros-ocp-backend/issues/774) | nise | P2 |
| V15-25 | [#775](https://github.com/pgarciaq/ros-ocp-backend/issues/775) | nise | P2 |
| V15-26 | [#776](https://github.com/pgarciaq/ros-ocp-backend/issues/776) | koku | P3 |
| V15-27 | [#777](https://github.com/pgarciaq/ros-ocp-backend/issues/777) | ros-ocp-backend | P3 |
| V15-28 | [#778](https://github.com/pgarciaq/ros-ocp-backend/issues/778) | ros-ocp-backend | P3 |
| V15-29 | [#779](https://github.com/pgarciaq/ros-ocp-backend/issues/779) | ros-ocp-backend | P3 |
