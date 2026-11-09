# Adversarial review v16 — v15 remediation

**Date:** 2026-11-09  
**Reviewer:** GPT-6.1 Sol; source review and locally executed probes, not a security certification.  
**Verdict:** **CONCERNS — the remediation still has demonstrated failures.**  
**Findings:** 6 P2, 1 P3; no P1 demonstrated in this pass.  
**State of every finding:** **verified mechanism; no fix applied.**

During audit execution, no GitHub findings were filed, no commits or pushes made, and no cluster mutation performed. Reproduction programs below are defect-observation probes: their passing assertions mean the documented defective behavior was observed, **not** that the product is fixed.

**Owner-authorized publication:** the initial report was committed as `ce4b25a1648104adeaf3f187cd558c84ed0ea6ac`, then seven issues were filed under [V16 tracker #833](https://github.com/pgarciaq/ros-ocp-backend/issues/833). The links below record that subsequent publication; the reviewed source commits and audit observations are unchanged. All seven issues remain open, with no fixes applied. V16-04 / #831 is a risk-reassessment question, not a newly discovered boundary or an approved redesign. The completed V15 tracker #780 remains closed.

## 1. Scope and exact revisions

Every repository was on `pgarciaq-rosocp-superpowers-phase17`. HEADs were recorded before inspection and rechecked afterwards.

| Repository | V15 baseline | V16 reviewed HEAD | Enumerated committed delta |
|---|---|---|---|
| ros-ocp-backend | `3061a76f64c4db30756f4e7a1d48f6814276618e` | `da5b76c8faeedcc93a1097081d4fb2b9489eb7d6` | 249 files; +14,543 / -1,801 |
| koku | `926c76feec25b209a0b46fabe58ec2fe4f621040` | `22cebac022bcdd5eb85b6a2024b28e82f3f80852` | 10 files; +691 / -82 |
| koku-metrics-operator | `290c05155ca611a84939fa210539ec193d8ed792` | `fad195d91105d778df5184bd33f1d1ea1595963f` | 5 files; +937 / -34 |
| nise | `e2e1b9b2771d401cfdd78ab3d85407dec1ffb2de` | `0a12a20118e23158788fecb6e792cd90296dced5` | 10 files; +1,721 / -188 |

Baselines for KMO and Nise came from the committed V15 report, not conversational recollection. Deltas were enumerated independently using `git diff --stat` and `git diff --name-only <baseline>..HEAD`. The ROS count includes documentation and vendored first-party copies; it is **not** an independently-read-line coverage claim. Review depth was risk-based, concentrated on authorization helper/caller interactions, new evidence production and consumption, ingestion state, operator recovery, and Nise identity and aggregation changes. No exhaustive whole-repository assurance is implied.

Koku's existing `docker-compose.yml` modification and untracked CSV/screenshots were excluded and left untouched. ROS's pre-existing local planning files and `.openchamber/` were also excluded. Other tracked working trees remained clean.

### Branch integration caveat

Closing a tracker does not establish that every fix is on the requested review branch. The #764/#765 completion record names KMO commit `8799bf17`; #768 names `942d796b` and `546b5c5b` on a side branch. All three `git merge-base --is-ancestor <fix> fad195d9` checks returned false. Their changed production files are absent from the requested KMO baseline-to-HEAD delta. This audit therefore does **not** attest those three remediations on phase17. Their original defects are not counted again as new V16 findings, and their side branches were not silently substituted for the requested HEAD.

### Known-work exclusions

The #780 closing record, relevant per-finding completion comments, and current open-issue bodies were checked. Already tracked mechanisms—including #803/#804, #809/#811/#813, #812, #814/#815/#820, #821, #822, #824/#825 and the other listed follow-ups—are not counted as new findings. In particular, the per-file stream fragment-flush hazard is #822, not a new V16 issue. Retained advisory rows until expiry are also not relabeled as a new finding.

The remaining tag oracle below uses a newly verified route not covered by #752's passing PVC oracle regression. Sleep findings use the **new** evidence table and direct-off-hours path, with fresh raw-input reproductions rather than restating the deleted subtraction. Nise's PVC finding preserves both namespace-qualified pod identities and demonstrates a different remaining lookup collision; it is not the separately tracked same-named-VM defect #821.

## 2. Cluster inventory boundary

The supplied `ACCESS.md` and on-prem operations guidance were read without printing credentials. The documented local kubeconfig exists. A read-only `oc --kubeconfig <documented-path> --request-timeout=10s whoami` failed with `context deadline exceeded`. Cluster attempts stopped; VPN/tunnel confirmation was requested. There were no retries through alternate paths.

**Not live-verified:** node architecture, deployed image tags/digests/commits, feature configuration, or application behavior on the SNO. The document describes arm64; that is a documentary observation, not a node-API verification. No finding below is claimed to have occurred on the deployed installation, and no multi-node or multi-farm live behavior is attested.

## 3. Findings

### V16-01 — Node-utilization warnings still reveal another cluster's tag-key existence

**Tracking issue:** [#828](https://github.com/pgarciaq/ros-ocp-backend/issues/828).

**Repository / severity:** ROS / **P2**, limited same-org confidentiality oracle.  
**State:** verified mechanism; no fix applied.  
**Provenance:** incomplete caller coverage in the #752 tag-warning scoping remediation; fresh endpoint reproduction at the V16 HEAD.

**Location against `da5b76c8faee`:**

- `internal/api/handlers_node_utilization.go:551` passes `nil` to `attachTagWarningsToNodeUtil`.
- `internal/api/tag_warnings.go:99–109` restricts the data-derived catalog only when `allowedClusters != nil`.
- `internal/api/tag_warnings.go:40–50` emits the unknown-key warning according to that catalog.

**Problem:** the node data query may correctly return no authorized rows, while its warnings still use an org-wide data-derived key catalog. A caller restricted to cluster A can distinguish a guessed key existing only on inaccessible cluster B from a nonexistent key.

**Risk / executed proof:** seeded two clusters and a B-only `resolved_tags` key `b-secret-project`; made requests with A-only permissions and no authorized matching data. Both returned HTTP 200, `count:0`, and empty data. The B-only key had **no** unknown-key warning; `no-such-tag` had `not in the known tag catalog` in both warning locations. Appendix A is the executed endpoint-level probe. The existing #752 reader-scope tests pass, including the scoped PVC warning test, so that regression does not cover this caller.

This demonstrates key-existence inference within one org, not cross-org access, tag-value disclosure, an identity-forgery path, or full unauthorized recommendation rows. P2 reflects that bounded impact rather than promoting the finding merely because it is near RBAC.

**Self-contained reproduction handoff:** run Appendix A with the overlay procedure in §5 against the named ROS HEAD. It uses the committed disposable PostgreSQL test helper and the existing legitimate-permission test harness. No application database or external RBAC service is needed.

**Close criteria:** an A-only user must receive the same unknown-key treatment for a B-only key and a nonexistent key on this route; retain authorized-key and scoped-PVC controls; test other remaining callers that pass an unscoped argument without claiming they were all verified by this probe.

**Constraints:** no expanded permission policy, no broad route rewrite, no claims of customer exploitation. Record a new reproduction if reopening #752; do not treat its closed status as evidence this route is safe.

### V16-02 — Direct off-hours medians still authorize sleep during measured overnight work

**Tracking issue:** [#826](https://github.com/pgarciaq/ros-ocp-backend/issues/826).

**Repository / severity:** ROS server + librobne / **P2**, incorrect high-confidence advice.  
**State:** verified mechanism; no fix applied.  
**Provenance:** newly exercised distributions in the #760 replacement evidence model, not the removed all-minus-BH subtraction.

**Location against `da5b76c8faee`:**

- `internal/ingestion/sleep_evidence.go:76–82,114–123` retains each leg's CPU p50 only.
- `internal/engine/correlate/sleep.go:133–141,228–243` sums those container medians and uses the result as the outside-safety gate.
- `librobne/advisory/sleep.go:67–88,186–201` performs the matching offline computation.

**Problem:** measuring the off-hours median directly fixes constant usage but does not establish overnight inactivity. A minority of off-hours samples can contain a sustained nightly job while the median is zero. In addition, the sum of per-container medians is **not** a conservative upper bound on the workload's off-hours activity: containers active at different times can each have zero medians while the workload is active for most of the off-hours window.

**Risk / executed proof, all 14 days and a seven-day 09:00–17:00 UTC schedule:**

1. One workload: 300m during BH, **2000m from 00:00–06:00**, zero during the other ten off-hours. All 24 hourly samples/day are supplied. The new writer stores off-hours p50=0; the actual server cycle creates `schedule_nonprod_sleep`, `confidence:high`, outside=0. The offline evaluator agrees.
2. Two containers of one workload: each active at 2000m for a different seven of the 16 off-hours. The workload is active for **14/16 off-hours**, yet both container medians are zero and the summed evidence is zero. Server and offline still advise sleep.

The raw-CSV server probe exercises `ParseAndDigestCSV` → `hcp_sleep_evidence` → `RunSleepCandidateCycle` → persisted advisory. The separate writer/read-back/offline probe confirms the other evaluator. Controls show the original constant-300m reproduction is now correctly vetoed, while a fully covered BH-active/off-hours-quiet workload correctly fires. Thus this is new evidence about the replacement, not a claim that the original subtraction remains.

**Self-contained reproduction handoff:** Appendix B (`night-burst` and `alternating-containers`) and Appendix C. Defect-observation output is `confidence=high outside=0` in both cases. Fixtures and source are embedded here; no historical temporary audit artifacts are required.

**Close criteria:** the two measured-active raw distributions must not be labeled safe-to-sleep with high confidence; retain the constant-active veto and fully covered quiet positive control; establish and document what the chosen evidence statistic actually proves, without assuming sums of percentiles overestimate simultaneous workload usage.

**Constraints:** advisory-only impact demonstrated; no automatic scale-to-zero or customer outage observed. No substitute percentile, threshold, or storage/performance trade-off is selected by this review.

### V16-03 — Two observed hours per day count as complete sleep evidence

**Tracking issue:** [#827](https://github.com/pgarciaq/ros-ocp-backend/issues/827).

**Repository / severity:** ROS server + librobne / **P2**, unsupported confidence on partial data.  
**State:** verified mechanism; no fix applied.  
**Provenance:** coverage behavior of the newly introduced #760 evidence schema and the carried #761 gate.

**Location against `da5b76c8faee`:**

- `internal/ingestion/sleep_evidence.go:76–82` marks a leg measured as soon as its sample list is nonempty.
- `migrations/000219_hcp_sleep_evidence.up.sql` stores only the two values, not observation coverage.
- `internal/engine/correlate/sleep.go:164–174,206–224` treats any nonzero count of non-NULL container values as coverage.
- `librobne/advisory/sleep.go:77–82,169–182` has the same presence-only test.

**Problem:** the table and evaluators cannot distinguish a fully observed quiet night from one quiet off-hours observation amid a reporting gap. Two present legs are treated as adequate whole-day evidence, even when most of both windows was unobserved.

**Risk / executed proof:** for every day of the 14-day window, provided only a 12:00 observation at 300m and a 20:00 observation at zero—**28 observations instead of 336**. The raw server pipeline persists and emits the high-confidence advisory with outside=0; the offline path does the same. This is not absence of an entire day or stream; the existing missing-stream check is exercised and bypassed by one sample on each side.

No activity during the unobserved 22 hours is asserted: the defect is presenting unknown coverage as a high-confidence sleep-safety observation. The complete-coverage positive control separately shows that silence is not demanded for every candidate.

**Self-contained reproduction handoff:** Appendix B/C `sparse-coverage`. The data contains all 14 dates and only the two specified hours each day.

**Close criteria:** partial evidence must remain distinguishable from adequate coverage through production, storage, and both evaluators; the sparse fixture must not establish high-confidence off-hours inactivity; retain the genuine complete-data positive case. Specify the actual coverage contract rather than inventing a threshold in a test.

**Constraints:** separate from V16-02's statistical loss on **complete** hourly data; no global sample cap, new accuracy compromise, or automatic backfill assumption.

### V16-04 — New sleep advice can be written under a schedule that did not produce its evidence

**Risk-reassessment issue:** [#831](https://github.com/pgarciaq/ros-ocp-backend/issues/831), retaining the explicit prior-disposition context from #760.

**Repository / severity:** ROS / **P2**, wrong-window sleep advice.  
**State:** verified mechanism; no fix applied.  
**Provenance:** concrete unsafe result for the schedule-change boundary acknowledged, but not executed, in #760's completion record.

**Location against `da5b76c8faee`:**

- `internal/ingestion/sleep_evidence.go:55–65` classifies observations using the ingest-time schedule.
- `migrations/000219_hcp_sleep_evidence.up.sql` contains no schedule identity/version for that split.
- `internal/engine/correlate/sleep.go:87–104,318–328` loads the current schedule and date-range evidence independently; it does not validate schedule equivalence or wait for reship completion.

**Problem:** evidence split under an old window remains admissible under a new one. Asynchronous reship is not a gate in this evaluator. A bound on eventual rebuilding does not make interim newly written sleep advice valid.

**Risk / executed proof:** ingested 14 complete days under a seven-day **09:00–21:00** schedule. Samples include 2000m at 18:00–21:00, 300m at 09:00–17:00, zero otherwise. Changed the enabled schedule to **09:00–17:00** without re-ingesting. The actual server cycle writes a **new** high-confidence advisory with outside=0, although the existing raw observations contain activity inside the newly proposed sleep hours. Appendix B executes this against a disposable DB.

This is not the separately recorded lifecycle limitation where an already-issued advisory survives until expiry: the probe starts without an advisory and obtains a new one after changing the schedule. The schedule row is changed with the product schedule persistence helper; source inspection confirms the evaluator has no reship-completion guard.

**Self-contained reproduction handoff:** Appendix B `schedule-change`; Appendix C also demonstrates the stale stored-leg/current-schedule mismatch in the offline evaluator. The server result does not depend on whether an external reship worker is running—the unsafe interval exists until that worker completes.

**Close criteria:** newly issued advice must use evidence attested to its resolved window or remain unknown while evidence is invalidated/rebuilt; exercise end-time, timezone and day-list changes plus the unchanged-schedule positive case; distinguish rebuild scheduling from verified rebuild completion.

**Constraints:** the review selects no invalidation or versioning mechanism. No shared-cluster reship was triggered. Existing BH-detail reship behavior is not proof this separate evaluator is gated.

### V16-05 — KMO recovery deletes legitimate split parts 10 and above

**Tracking issue:** [#829](https://github.com/pgarciaq/ros-ocp-backend/issues/829).

**Repository / severity:** koku-metrics-operator / **P2**, irreversible report loss with successful packaging.  
**State:** verified mechanism; no fix applied.  
**Provenance:** new ambiguity in the #766 staged-recovery supersession code.

**Location against `fad195d91105`:**

- `internal/packaging/packaging.go:338–358` recognizes any numeric suffix as a split part.
- `internal/packaging/packaging.go:609–640` compares recovered entries pairwise under the same UID and deletes entries treated as parts of another.
- `internal/packaging/packaging.go:508–524` really generates multi-digit part names and removes the original after splitting.

**Problem:** when only split parts remain, `report10.csv` is treated as a stale split of `report1.csv`, `report11.csv` as another split of `report1.csv`, etc. The recovery heuristic cannot distinguish an original report from one of its own numbered parts.

**Risk / executed proof:** used the actual `splitFiles` implementation to produce **16 parts** from one CSV. The original was removed by the splitter. Modeled a failure after splitting and before archive completion, then invoked the actual `PackageReports` retry. It returned success, but the resulting tarball contained **nine CSVs**, and the distinct `row-30` marker was absent. Parts 10–16 had been deleted by the new recovery pass.

**Self-contained reproduction handoff:** Appendix D. Uses only isolated temporary directories, the real splitter, packager, and archive-reading helpers committed with #766; no cluster or object storage is required. The low in-memory split byte limit makes this a small reproduction of the normal multi-part naming behavior, not a product configuration change.

**Close criteria:** recovery of at least 16 real split parts must preserve every marker exactly once; original-plus-stale-parts cleanup must still work without classifying a sibling part as the original; cover multi-digit parts and an interrupted split/packaging retry.

**Constraints:** this is a new destructive path in the remediation, not just the old unconditional staging clear. No packaging implementation was changed by the audit.

### V16-06 — Namespace-qualified pod lookup still returns another namespace's same-named PVC

**Tracking issue:** [#830](https://github.com/pgarciaq/ros-ocp-backend/issues/830).

**Repository / severity:** Nise / **P2**, fixture identity/VM disk attribution corruption.  
**State:** verified mechanism; no fix applied.  
**Provenance:** incomplete downstream identity qualification in #775's PVC lookup change.

**Location against `0a12a20118e2`:**

- `nise/generators/ocp/ocp_generator.py:1862–1871` uses `(namespace,pod)` to retrieve a PVC **name**, then scans all volumes using only that PVC name.
- `nise/generators/ocp/ocp_generator.py:1873–1889` consumes the selected claim to populate VM disk capacity.

**Problem:** two namespaces may each legitimately contain a claim named `data`. Qualifying the pod-to-name mapping does not qualify the subsequent claim lookup. The first `data` claim in the volume scan wins regardless of the requested namespace.

**Risk / executed proof:** built the real generator from two namespaces, each with pod `web` and PVC `data`, capacities **1 GiB** and **5 GiB**, on distinct backing PV names. Both namespace-qualified pod identities survived the #775 fix. Nevertheless, requesting namespace B's PVC returned namespace A's claim and 1 GiB VM disk capacity. Reversing namespace declaration order changed both lookups to B's claim and 5 GiB. The existing #775 PVC test uses different claim names (`claim-a`, `claim-b`), so it cannot expose this same-value collision.

**Self-contained reproduction handoff:** Appendix E runs both declaration orders through `OCPGenerator`, `get_specific_pvc_from_pod`, and its actual `get_vm_disk` consumer. No test monkeypatch or VM-name collision is required.

**Close criteria:** same-named PVCs must preserve namespace and capacity attribution in both orders through the lookup and its VM disk consumer; keep distinct-name and same-pod-name controls; do not conflate this with the already tracked same-named-VM issue #821.

**Constraints:** generated test-data corruption is demonstrated, not cross-tenant production data access. No general generator rewrite or unrelated identity expansion is proposed.

### V16-07 — Nise's new insecure-mode warning contradicts the actual Requests TLS setting

**Tracking issue:** [#832](https://github.com/pgarciaq/ros-ocp-backend/issues/832).

**Repository / severity:** Nise / **P3**, false diagnostic and ineffective documented lab override.  
**State:** verified mechanism; no fix applied.  
**Provenance:** new `REQUESTS_VERIFY` handling/comment introduced in #755, not a recurrence of insecure-by-default uploading.

**Location against `0a12a20118e2`:** `nise/report.py:295–305,373–398`.

**Problem:** setting `REQUESTS_VERIFY=false` emits `TLS certificate verification is DISABLED for uploads`, but only controls this warning. The upload still uses Requests' default verification. Requests does not implement that environment variable; its supported CA-bundle environment behavior is different.

**Risk / executed proof:** with the repository's installed Requests **2.34.2**, executed the real upload function with dummy Basic credentials and `REQUESTS_VERIFY=false`, intercepting the Requests HTTP adapter boundary before network I/O. The warning call claimed verification was disabled, while the adapter received **`verify=True`**. An independent `Session.merge_environment_settings` check also returned `verify=True`, and the installed Requests session implementation contains no `REQUESTS_VERIFY` handling.

The security-positive default verification remains intact. The impact is that the claimed explicit lab bypass does not work and the diagnostic falsely reports a security-relevant state; this is not a TLS-bypass vulnerability.

**Self-contained reproduction handoff:** Appendix F invokes the actual uploader and Requests session machinery, not a mock of `requests.post`. It uses only a local temporary payload, dummy credentials and an intercepted adapter; no remote service or secrets are required.

**Close criteria:** the published/diagnosed override behavior and effective transport setting must agree; preserve verified-by-default behavior and private-CA support; exercise the transport boundary rather than asserting only an omitted `verify` kwarg or a warning string.

**Constraints:** no request to weaken TLS defaults; no dependency/CVE attestation; no real credentials transmitted.

## 4. Executed checks and limits

| Check | Observed result | What it establishes |
|---|---|---|
| ROS new server raw-input probes | Six cases pass their observation assertions: four defective scenarios plus two controls | Actual parser, new writer, new table and server advisory cycle reproduce the stated outputs |
| ROS writer/read-back/offline probes | Same six observations | Library path agrees for these cases; not general parity certification |
| ROS new tag-oracle endpoint probe | Observation assertions pass | A-only warning existence oracle on node-utilization |
| ROS existing `TestReaderScope_`, `TestOpenShiftData`, `TestFleetHeatmap_RBAC` selection | Pass, 8.671s | Selected existing RBAC regressions remain green; selection is not the full API suite |
| ROS existing correlator package | Pass, 19.825s | Existing tests do not catch these new raw distributions/coverage changes |
| KMO actual split/recovery probe | 16 parts become nine, success returned, last marker lost | The destructive recovery mechanism is executed |
| KMO existing packaging and collector packages | Pass, 0.706s / 33.520s | Selected tests green despite the uncovered recovery case |
| Nise real PVC/order probe | Both orders demonstrate first-namespace attribution | The remaining same-name collision is executed |
| Nise transport probe | Warning says disabled; adapter receives `verify=True` | Actual Requests option resolution disagrees with the warning |
| Nise three relevant unittest modules | **161 tests pass**, 10.890s | Namespace aggregation, CRQ and generator regressions remain green |
| Koku lowercase metadata helper | Pass for matching/nonmatching lowercase metadata, executing the committed function body | The #776 lookup correction works for that control; not a full Django test run |
| Changed first-party library/vendor copies | Seven changed production files compare byte-for-byte equal | No drift observed for these copies; no dependency-source audit |

The first Nise pytest invocation could not run because pytest was unavailable in the selected environment. The same selected unittest modules were then executed using `uv run --frozen python -m unittest`, producing the 161-test result above. No missing test dependency was silently interpreted as a pass.

**Not executed / not attested:** full ROS `make test`, full Koku Django suite, full operator envtest suite, full Nise suite, scale/throughput/storage benchmarks, mutation tests for every existing regression, live S3, live cluster application behavior, source rollout parity, or every route/configuration combination. Existing Koku development/test databases were not used. No accuracy/speed/storage trade-off is quantified or accepted here. Earlier conversational cost estimates for A/A′ are not measurements produced by this audit.

The seven changed first-party files checked for vendor equality were `advisory/{sleep,headroom,quantile}.go`, `digest/digest.go`, `bhschedule/schedule.go`, and `pgdigest/{read,write}_sleep_evidence.go`. Vendor comparison is expressly **not** a dependency security or FIPS/CVE attestation.

**Handoff verification:** all four Go and both Python programs were extracted back out of this report and executed again. All reproduced the observations shown above, including both positive/negative sleep controls. Thus the handoff is not merely a pointer to session-local probe files. The four repository HEADs were stable on the final check; the only tracked working-tree edit was Koku's pre-existing `docker-compose.yml` change.

## 5. Self-contained reproduction instructions

Use disposable checkouts at the HEADs in §1, with the projects' normal dependencies. Do not switch a shared working tree away from another session's work. ROS test helpers require a local container runtime and create disposable PostgreSQL containers; KMO packaging uses temporary directories only. Nise uses the locked project environment. The snippets depend only on committed product/test code at the specified revisions, not on any files from V15 or this session's scratch directory.

For each Go snippet, save it to an arbitrary scratch directory, then add it **virtually** to its package using a Go overlay. For example, Appendix A is package `api_test`:

```bash
export ROS_ROOT=/absolute/path/to/disposable/ros-ocp-backend
export PROBE=/absolute/path/to/api_probe_test.go
export OVERLAY=/absolute/path/to/api-overlay.json
python3 - <<'PY'
import json, os
from pathlib import Path
virtual = str(Path(os.environ['ROS_ROOT']) / 'internal/api/v16_probe_test.go')
Path(os.environ['OVERLAY']).write_text(json.dumps({'Replace': {virtual: os.environ['PROBE']}}))
PY
cd "$ROS_ROOT"
go test -overlay "$OVERLAY" ./internal/api -run '^TestV16UnscopedTagWarningOracle$' -count=1 -v
```

Use the same overlay procedure for:

- Appendix B: virtual `internal/engine/correlate/v16_probe_test.go`; package `./internal/engine/correlate`; run `^TestV16ServerRawSleepProbes$`.
- Appendix C: virtual `internal/ingestion/v16_probe_test.go`; package `./internal/ingestion`; run `^TestV16SleepEvidenceProbes$`.
- Appendix D: use the KMO root, virtual `internal/packaging/v16_probe_test.go`; package `./internal/packaging`; run `^TestV16RecoveryDeletesDoubleDigitSplitParts$`.

For Appendices E/F, save the Python snippet anywhere and run `uv run --frozen python /absolute/path/to/probe.py` from the disposable Nise checkout. The Python probes perform no external requests.

The complete reproduction programs follow. PASS means **defect observed** where indicated; controls are named separately.

<!-- Embedded reproduction appendices follow. -->

### Appendix A — node-utilization tag oracle

```go
package api_test

import (
 "context"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/labstack/echo/v4"
 "github.com/stretchr/testify/require"
 "github.com/redhatinsights/ros-ocp-backend/internal/api"
 "github.com/redhatinsights/ros-ocp-backend/internal/config"
 database "github.com/redhatinsights/ros-ocp-backend/internal/db"
 "github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestV16UnscopedTagWarningOracle(t *testing.T){
 setupEligibilityTest(t);ctx:=context.Background();seedReaderScopeOrg(t,ctx)
 require.True(t,config.TagsFeatureEnabled(),"warning feature must be enabled, not skipped")
 _,err:=database.GetPool().Exec(ctx,`INSERT INTO org_container_keys
 (org_id,cluster_uuid,namespace,workload,workload_type,container_name,last_reported,resolved_tags)
 VALUES ($1,$2,'ns-rs','wl-rs-b','Deployment','ctr-rs-b',now(),'{"b-secret-project": "x"}')`,testutil.TestOrgID,readerClusterB);require.NoError(t,err)
 schema:=strings.ReplaceAll(testutil.TestOrgID,"-","_")
 _,err=database.GetPool().Exec(ctx,`CREATE SCHEMA IF NOT EXISTS `+schema+`;
 CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocptags_values(uuid uuid primary key,key text not null,value text not null,cluster_ids text[] not null,cluster_aliases text[] not null default '{}',namespaces text[] not null)`);require.NoError(t,err)
 app:=permsApp(readerClusterAPerms(),func(v1 *echo.Group){v1.GET("/v16/node-util",api.GetNodeUtilizationRecs)})
 bodies:=map[string]string{}
 for _,key:=range []string{"b-secret-project","no-such-tag"}{
  req:=httptest.NewRequest(http.MethodGet,fmt.Sprintf("/api/cost-management/v1/v16/node-util?filter[tag:%s]=x",key),nil)
  req.Header.Set("x-rh-identity",makeIdentityHeader(testutil.TestOrgID));rec:=httptest.NewRecorder();app.ServeHTTP(rec,req)
  require.Equal(t,http.StatusOK,rec.Code,rec.Body.String());bodies[key]=rec.Body.String()
  t.Logf("OBSERVED key=%s response=%s",key,rec.Body.String())
 }
 require.NotContains(t,bodies["b-secret-project"],"not in the known tag catalog")
 require.Contains(t,bodies["no-such-tag"],"not in the known tag catalog")
 t.Log("DEFECT OBSERVED: cluster A-only user distinguishes B's private tag key from a nonexistent key")
}
```

### Appendix B — server: raw CSV to persisted advisory

```go
package correlate

import (
 "context"
 "fmt"
 "strings"
 "testing"
 "time"
 "github.com/stretchr/testify/require"
 "github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
 "github.com/redhatinsights/ros-ocp-backend/internal/config"
 "github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
 "github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestV16ServerRawSleepProbes(t *testing.T){
 t.Setenv("ROS_BUSINESS_HOURS_ENABLED","true");config.ResetForTest();t.Cleanup(config.ResetForTest)
 pool:=testutil.SetupTestDB(t);ctx:=context.Background();start,end:=sleepWindow()
 weekdays:=[]string{"monday","tuesday","wednesday","thursday","friday","saturday","sunday"}
 for _,which:=range []string{"night-burst","sparse-coverage","schedule-change","alternating-containers","fully-covered-quiet","constant-active"}{
  t.Run(which,func(t *testing.T){
   org:="v16-server-"+which
   sched:=bhschedule.Schedule{OrgID:org,ClusterUUID:sHC1,Timezone:"UTC",Days:weekdays,StartTime:"09:00",EndTime:"17:00",Enabled:true}
   if which=="schedule-change"{sched.EndTime="21:00"}
   require.NoError(t,bhschedule.UpsertSchedule(ctx,pool,sched))
   var csv strings.Builder
   csv.WriteString("interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count\n")
   for day:=start;day.Before(end);day=day.AddDate(0,0,1){for h:=0;h<24;h++{
    if which=="sparse-coverage"&&h!=12&&h!=20{continue}
    n:=1;if which=="alternating-containers"{n=2}
    for c:=0;c<n;c++{
     cpu:=0.0;if h>=9&&h<17{cpu=0.3};if which=="constant-active"{cpu=0.3}
     if which=="night-burst"&&h<6{cpu=2}
     if which=="schedule-change"&&h>=18&&h<21{cpu=2}
     if which=="alternating-containers"&&(h<9||h>=17){idx:=h;if h>=17{idx=h-8};if(c==0&&idx<7)||(c==1&&idx>=7&&idx<14){cpu=2}}
     ts:=day.Add(time.Duration(h)*time.Hour)
     fmt.Fprintf(&csv,"%s,%s,user-app,pod,web,Deployment,c%d,0.3,3,%.3f,0,1048576,1048576,1048576,1048576,0\n",ts.Format("2006-01-02 15:04:05 +0000 UTC"),ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"),c,cpu)
    }
   }}
   _,err:=ingestion.ParseAndDigestCSV(ctx,pool,strings.NewReader(csv.String()),org,sHC1);require.NoError(t,err)
   if which=="schedule-change"{sched.EndTime="17:00";require.NoError(t,bhschedule.UpsertSchedule(ctx,pool,sched))}
   seedSleepSnapshot(t,ctx,pool,org,sMgmt1,sHC1,hcTierNonProd,time.Now().UTC().Add(-time.Hour))
   seedNodepoolRollup(t,ctx,pool,org,sHC1,2)
   _,err=RunSleepCandidateCycle(ctx,pool);require.NoError(t,err)
   got:=sleepAdvisories(t,ctx,pool,org)
   if which=="constant-active"{require.Empty(t,got);t.Log("CONTROL: constant 300m is correctly vetoed");return}
   require.Len(t,got,1,"expected observation: current server emits advice")
   if which=="fully-covered-quiet"{t.Log("CONTROL: complete quiet off-hours candidate correctly fires");return}
   t.Logf("DEFECT OBSERVED: server case=%s confidence=%v outside=%v",which,got[0]["confidence"],got[0]["outsidePeak"])
  })
 }
}
```

### Appendix C — writer/read-back/offline agreement

```go
package ingestion

import (
 "context"
 "testing"
 "time"
 "github.com/stretchr/testify/require"
 "github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
 "github.com/redhatinsights/ros-ocp-backend/internal/testutil"
 "github.com/redhatinsights/ros-ocp-backend/librobne/advisory"
 libbh "github.com/redhatinsights/ros-ocp-backend/librobne/bhschedule"
 "github.com/redhatinsights/ros-ocp-backend/librobne/pgdigest"
)

func TestV16SleepEvidenceProbes(t *testing.T) {
 pool:=testutil.SetupTestDB(t);ctx:=context.Background()
 now:=time.Date(2026,11,9,0,0,0,0,time.UTC)
 days:=[]string{"monday","tuesday","wednesday","thursday","friday","saturday","sunday"}
 for _,which:=range []string{"night-burst","sparse-coverage","schedule-change","alternating-containers","fully-covered-quiet","constant-active"} {
  t.Run(which,func(t *testing.T){
   org:="v16-"+which
   sched:=bhschedule.Schedule{OrgID:org,ClusterUUID:testutil.TestClusterUUID,Timezone:"UTC",Days:days,StartTime:"09:00",EndTime:"17:00",Enabled:true}
   if which=="schedule-change" {sched.EndTime="21:00"}
   require.NoError(t,bhschedule.UpsertSchedule(ctx,pool,sched))
   cache,err:=bhschedule.LoadSchedules(ctx,pool,org,testutil.TestClusterUUID);require.NoError(t,err)
   var rows []MetricRow
   maxOff:=int64(0)
   for day:=now.AddDate(0,0,-14);day.Before(now);day=day.AddDate(0,0,1){
    for hour:=0;hour<24;hour++ {
     if which=="sparse-coverage" && hour!=12 && hour!=20 {continue}
     containers:=1;if which=="alternating-containers" {containers=2}
     for c:=0;c<containers;c++ {
      cpu:=int64(0);if hour>=9 && hour<17 {cpu=300};if which=="constant-active" {cpu=300}
      if which=="night-burst" && hour>=0 && hour<6 {cpu=2000}
      if which=="schedule-change" && hour>=18 && hour<21 {cpu=2000}
      if which=="alternating-containers" && (hour<9 || hour>=17) {
       offIndex:=hour;if hour>=17 {offIndex=hour-8}
       if (c==0 && offIndex<7)||(c==1 && offIndex>=7 && offIndex<14){cpu=2000}
      }
      if (hour<9||hour>=17)&&cpu>maxOff {maxOff=cpu}
      cn:="c0";if c==1 {cn="c1"}
      rows=append(rows,MetricRow{IntervalStart:day.Add(time.Duration(hour)*time.Hour),Namespace:"user-app",WorkloadName:"web",WorkloadType:"Deployment",ContainerName:cn,CPUUsageMC:cpu})
     }
    }
   }
   grouped:=GroupCSVRows(rows,org,testutil.TestClusterUUID)
   require.NoError(t,upsertSleepEvidenceOnSender(ctx,pool,grouped,cache))
   if which=="schedule-change" {
    sched.EndTime="17:00";require.NoError(t,bhschedule.UpsertSchedule(ctx,pool,sched))
   }
   bh,off,err:=pgdigest.ReadSleepEvidence(ctx,pool,org,testutil.TestClusterUUID,now.AddDate(0,0,-14),now);require.NoError(t,err)
   ss:=advisory.SleepSums(bh,off)
   adv:=advisory.SleepCandidate("hc",advisory.TierNonProd,ss,libbh.Schedule{Timezone:"UTC",Days:days,StartTime:"09:00",EndTime:"17:00",Enabled:true},advisory.DefaultSleepPolicy(),2,nil,nil,now)
   if which=="constant-active" {require.Nil(t,adv);t.Log("CONTROL: constant 300m is correctly vetoed");return}
   require.NotNil(t,adv,"expected observation: current code emits advice")
   if which=="fully-covered-quiet" {t.Log("CONTROL: complete quiet off-hours candidate correctly fires");return}
   t.Logf("DEFECT OBSERVED: case=%s samples=%d max_off_mc=%d confidence=%s outside=%v",which,len(rows),maxOff,adv.Confidence,adv.Details["outside_bh_peak_cpu_mc"])
  })
 }
}
```

### Appendix D — operator split-part recovery

```go
package packaging

import (
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestV16RecoveryDeletesDoubleDigitSplitParts(t *testing.T) {
 p,cr:=newRecoveryPackager(t,MoveFiles)
 const uid="11111111-1111-4111-8111-111111111111"
 name:=uid+"-ros-openshift-report.csv"
 var b strings.Builder
 b.WriteString("interval_start,interval_end,marker\n")
 for i:=1;i<=30;i++{fmt.Fprintf(&b,"2026-11-01 00:00:00 +0000 UTC,2026-11-01 01:00:00 +0000 UTC,row-%02d\n",i)}
 if err:=os.WriteFile(filepath.Join(p.DirCfg.Staging.Path,name),[]byte(b.String()),0600);err!=nil{t.Fatal(err)}
 info,err:=os.Stat(filepath.Join(p.DirCfg.Staging.Path,name));if err!=nil{t.Fatal(err)}
 p.maxBytes=120
 parts,split,err:=p.splitFiles(p.DirCfg.Staging.Path,[]os.FileInfo{info});if err!=nil{t.Fatal(err)}
 if !split||len(parts)<10{t.Fatalf("need at least ten real split parts, got %d",len(parts))}
 _,err=os.Stat(filepath.Join(p.DirCfg.Staging.Path,name));if !os.IsNotExist(err){t.Fatal("split must delete original")}
 if err:=p.PackageReports(cr);err!=nil{t.Fatal(err)}
 names,_,content:=archiveMembersWithContent(t,p.DirCfg.Upload.Path)
 count:=countCSVNames(names)
 if count>=len(parts){t.Fatalf("expected observation of new recovery defect: before=%d after=%d",len(parts),count)}
 t.Logf("DEFECT OBSERVED: real split parts=%d recovered CSVs=%d row-30-preserved=%v; packaging returned success",len(parts),count,strings.Contains(content,"row-30"))
}
```

### Appendix E — Nise same-name PVC attribution

```python
from datetime import datetime, timedelta
from nise.generators.ocp.ocp_generator import OCPGenerator

for order in [("a", "b"), ("b", "a")]:
    namespaces = {}
    for ns in order:
        cap = 1 if ns == "a" else 5
        namespaces[ns] = {
            "pods": [{"pod_name": "web", "cpu_request": 1, "cpu_limit": 2,
                      "mem_request_gig": 1, "mem_limit_gig": 2}],
            "volumes": [{"volume_name": "pv-" + ns, "volume_request_gig": 10,
                         "volume_claims": [{"volume_claim_name": "data", "pod_name": "web",
                                            "capacity_gig": cap}]}],
        }
    start = datetime(2026, 11, 1)
    g = OCPGenerator(start, start + timedelta(hours=1),
                     {"nodes": [{"node_name": "n1", "cpu_cores": 16, "memory_gig": 64,
                                 "namespaces": namespaces}]},
                     ros_ocp_info=True, constant_values_ros_ocp=True)
    for ns in ["a", "b"]:
        pvc, claim = g.get_specific_pvc_from_pod(namespace=ns, pod_name="web")
        disk = g.get_vm_disk(namespace=ns, pod_name="web", static_report=True)
        print(f"order={order} request_ns={ns} pvc={pvc} returned_ns={claim['namespace']} "
              f"capacity={claim['capacity']} vm_disk_capacity={disk['vc_capacity']}")
    assert g.get_specific_pvc_from_pod(namespace=order[1], pod_name="web")[1]["namespace"] == order[0]
print("DEFECT OBSERVED: equal PVC names select the first namespace's claim and VM disk capacity.")
```

### Appendix F — actual Requests transport option

```python
import os
from tempfile import NamedTemporaryFile
from unittest.mock import patch
import requests
from nise.report import post_payload_to_ingest_service

seen = []
def transport(request, **kw):
    seen.append(kw["verify"])
    response = requests.Response()
    response.status_code = 200
    return response

with NamedTemporaryFile() as payload:
    payload.write(b"dummy local payload")
    payload.flush()
    with patch.dict(os.environ, {"REQUESTS_VERIFY": "false", "INSIGHTS_USER": "dummy",
                                 "INSIGHTS_PASSWORD": "dummy"}, clear=True):
        with patch("requests.adapters.HTTPAdapter.send", side_effect=transport), \
             patch("nise.report.LOG.warning") as warning:
            post_payload_to_ingest_service("https://example.invalid/upload", payload.name, max_retries=0)
            assert warning.called
            print("Warning emitted:", warning.call_args.args[0])
print("requests", requests.__version__, "REQUESTS_VERIFY=false transport verify=", seen)
assert seen == [True]
print("DEFECT OBSERVED: warning says verification DISABLED, transport still receives verify=True.")
```

## Final findings summary

| Finding | Repository | Severity | Verified state | GitHub issue |
|---|---|---|---|---|
| V16-01 — node-utilization tag-warning existence oracle | ros-ocp-backend | P2 | Endpoint reproduced; no fix applied | [#828](https://github.com/pgarciaq/ros-ocp-backend/issues/828) |
| V16-02 — direct off-hours medians hide nightly/alternating activity | ros-ocp-backend / librobne | P2 | Raw server pipeline and offline read-back reproduced; no fix applied | [#826](https://github.com/pgarciaq/ros-ocp-backend/issues/826) |
| V16-03 — sparse observations treated as complete sleep coverage | ros-ocp-backend / librobne | P2 | 28-observation/14-day case reproduced both paths; no fix applied | [#827](https://github.com/pgarciaq/ros-ocp-backend/issues/827) |
| V16-04 — changed schedule consumes old-split sleep evidence | ros-ocp-backend / librobne | P2 | New advisory after window change reproduced; no fix applied; prior risk disposition under reassessment | [#831](https://github.com/pgarciaq/ros-ocp-backend/issues/831) |
| V16-05 — recovery deletes double-digit split parts | koku-metrics-operator | P2 | 16 real parts become nine, success returned; no fix applied | [#829](https://github.com/pgarciaq/ros-ocp-backend/issues/829) |
| V16-06 — same-named PVC returns another namespace's claim | nise | P2 | Real generator and disk consumer, both declaration orders reproduced; no fix applied | [#830](https://github.com/pgarciaq/ros-ocp-backend/issues/830) |
| V16-07 — TLS override warning contradicts effective verification | nise | P3 | Actual uploader/Requests adapter option reproduced; no fix applied | [#832](https://github.com/pgarciaq/ros-ocp-backend/issues/832) |
