# Adversarial review v19 — the unified cluster filter, after batch G

**Date:** 2026-11-27
**Reviewer:** AI-assisted (session agent); live probes against the lab bench and
file reads, not a security certification.
**Verdict:** **CONCERNS.** One P2 implementation finding (V19-01), two P3
contract/docs findings (V19-02, V19-03), two informational notes (V19-04,
V19-05). No security finding; no P1.
**Filed:** [#972](https://github.com/pgarciaq/ros-ocp-backend/issues/972)
(V19-01), [#973](https://github.com/pgarciaq/ros-ocp-backend/issues/973)
(V19-02). V19-03 and V19-04 are comments on
[#970](https://github.com/pgarciaq/ros-ocp-backend/issues/970) and
[#966](https://github.com/pgarciaq/ros-ocp-backend/issues/966), the issues they
extend.
**Scope:** the cluster-filter implementation as it stands at `6b78a7b0` — the
seven commits from `2986010c` (#950) through `6b78a7b0` (#968 F1), read in full
where they touch resolution, plus live probes of thirty surfaces on the bench.

## 1. What was reviewed, and from where

| Item | Value |
|---|---|
| ros-ocp-backend HEAD | `6b78a7b0` (`pgarciaq-rosocp-superpowers-phase17`) |
| Commits in scope | `2986010c` #950, `3b2ce106` #952 docs, `1806b8a0` #963/#964/#967/#968, `46d50677` #969 docs, `6b78a7b0` #968 F1 |
| Live image | `ros-ocp-backend:f1-202611272200` (arm64, built from `6b78a7b0`, deployed to `cost-onprem-ros-api`) |
| Bench | org `1234567`, alias `demo-special` = `048db166-…b030`, `demo-bench` = `1f859c07-…f779`, `demo` = 4 clusters, `live947-node` in two clusters (`…9471`, `…9472`) |
| Probes | three scripts: ignored-filter detector (30 surfaces), UUID-spelling probe (21 surfaces), mode matrix (19 surfaces); plus targeted calls recorded below |

The probes were written for this pass and are not committed; every number in
this file is copied from a run against the image above. Counts drift because
the poller keeps writing (history moved 61,244 → 61,256 → 61,262 within the
session); equality and difference legs were always taken in the same run, so
the drift does not change a verdict.

## 2. Method

Personas as required by the adversarial-reviewer skill: the Saboteur (break it
with inputs), the New Hire (understand it in six months), the Security Auditor
(injection, access control, data exposure). The self-review trap was broken by
enumerating **mechanisms** first — `grep` for every handler that mentions the
cluster parameter, bucketed by the expression it builds — rather than reading
the files I already knew. That is what surfaced V19-01: the node detail is the
only handler in the tree that never calls the shared filter accessor.

## 3. Findings

### V19-01 — `/nodes/{node}` drops the documented `filter[...]` spelling, and answers with another cluster's node

**Severity:** P2. Same class as #968 (silently wrong answer on a documented
form), but worse: the answer is a *different cluster's object*, not an empty
page. Filed as #972.
**Location:** `internal/api/handlers_node_detail.go:77-80`.

```go
clusterFilter := strings.TrimSpace(c.QueryParam("cluster_uuid"))
if clusterFilter == "" {
    clusterFilter = strings.TrimSpace(c.QueryParam("cluster"))
}
```

`c.QueryParam` reads the flat spellings only. Every sibling detail route reads
`queryparams.FirstFilter(c, "cluster")` — `filter[cluster]` — as well:
`handlers_node_hourly.go:56`, `handlers_gpu_timeslicing_detail.go:81`,
`handlers_pvc_detail.go:49`, `handlers_quota_detail.go:75`,
`handlers_vm_recs.go:463`, `handlers_vm_history.go:39`. Node detail is alone.

The public parameter table lists `filter[cluster]` as the bracket form of the
Cluster parameter (`docs-site/plugin-reference/query-parameters.md:74`), and
the #969 paragraph tells the caller that `/nodes/{node}` answers `400 … pass a
cluster UUID or a narrower alias` — but that 400 is unreachable through the
documented spelling.

**Executed proof** (`/nodes/live947-node`, the node exists in two clusters):

| request | 200 body `cluster_uuid` | expected |
|---|---|---|
| no filter | `…9471` | the documented no-filter merge |
| `cluster_uuid=…9472` (flat) | `…9472` | `…9472` ✓ |
| `cluster=…9472` (flat) | `…9472` | `…9472` ✓ |
| `filter[cluster]=…9472` (bracket) | **`…9471`** | `…9472` ✗ |
| `filter[cluster]=replay-nomatch-xyz` | `…9471`, HTTP 200 | `404 node not found` ✗ |
| `cluster=replay-nomatch-xyz` (flat) | `404 node not found` | ✓ |
| `cluster=demo` (flat, 4 matches) | `400 … resolves to 4 clusters` | ✓ |
| `filter[cluster]=demo` (bracket) | **HTTP 200, `…9471`** | `400` ✗ |

So a caller who sends the filter in the form the docs name asks for cluster
9472's node and receives 9471's node — the exact failure mode #969 exists to
remove, reachable through the form nobody tested. `filter[exact:cluster]` and
`exclude[cluster]` are ignored there too (the handler never reads them), which
puts this route in the "answers as though unfiltered" family that #970's
paragraph does not list.

**Impact today:** no known consumer sends the bracket form here — koku-ui's
node detail request (`koku-ui-ros/src/api/ros/recommendations.ts:362`) sends
only term and engine, and the replay's node legs used the flat form. The
defect is therefore latent for the UI and live for API consumers following the
docs, which is how #968 shipped too.

**Why it survived:** the batch-G detail tests pass the flat form —
`url.Values{"cluster": {…}}` in `cluster_alias_contract_matrix_test.go:294-310`
and `:373-378`. The test and the docs disagree about the spelling, and nothing
compares them.

**Fix sketch:** read `queryparams.FirstFilter(c, "cluster_uuid")` and
`queryparams.FirstFilter(c, "cluster")` before the flat fallbacks (the pattern
`handlers_gpu_timeslicing_detail.go` already uses), and add the bracket legs to
the existing node-detail tests. One handler, ~6 lines, plus tests.

### V19-02 — `exclude[cluster]` is an exact, case-sensitive alias match; the docs and the code comment say it negates the include membership

**Severity:** P3 (documented-vs-actual mismatch; the current behaviour is
deliberate and tested, the description of it is not). Filed as #973.
**Location:** `internal/api/utils.go:377-382` (`parseClusterParams`),
`docs-site/plugin-reference/query-parameters.md:129-131`, the comment at
`utils.go:369-372`.

The include form is what the docs define the membership to be: an org-scoped,
case-insensitive substring, unioned. Exclude builds

```sql
NOT IN (SELECT cluster_uuid FROM clusters WHERE org_id = ? AND cluster_alias = ?)
```

— exact, case-sensitive, no `ILIKE`, no wrapping. The code comment says
"Exclude negates the whole membership (NOT IN with the **positive condition**)"
and the public page says "`exclude` negates the membership". Neither is what
the code does, and the test asserts the code, not the prose: *"exclude negates
the exact alias membership; it is not an ILIKE"*
(`cluster_alias_contract_batch2_test.go:388-393`).

**Executed proof** — every mode on the four honouring families, same run:

| surface | all | `filter=demo` | `exclude=demo` | `exclude=demo-special` | `exclude=DEMO-SPECIAL` |
|---|---|---|---|---|---|
| container list | 1,786 | **1,281** | **1,786** | 1,756 | **1,786** |
| `/namespace` | 164 | **67** | **164** | 149 | **164** |
| `/history` | 61,256 | **28,782** | **61,256** | 60,296 | **61,256** |
| `/quality/containers` | 12,749 | **5,199** | **12,749** | 12,589 | **12,749** |

Two divergences follow from the code, and neither is anywhere in the docs:

1. `exclude[cluster]=demo` removes **nothing**, although `filter[cluster]=demo`
   selects 1,281 rows on the same route. A caller who reads "exclude negates
   the membership" and passes the same value they filtered on gets the full
   page back.
2. Case matters only for exclude. `filter[cluster]=DEMO-SPECIAL` selects 960
   rows; `exclude[cluster]=DEMO-SPECIAL` removes none. A value copied in a
   different case is a silent no-op — the one failure mode this phase keeps
   finding.

**The decision this needs:** either
(a) keep exact-exclude (defensible — excluding by substring can hide more than
the caller meant) and fix the two prose sites to say *exact alias, case
sensitive*; or
(b) make exclude case-insensitive on the alias (no substring, just
`LOWER(cluster_alias) = LOWER(?)`), so a copied alias always works.

Recommendation: **(b) plus the prose fix.** The include form is case
insensitive by contract, aliases are display strings the UI hands back, and a
silent no-op on an exclude is indistinguishable from "the cluster has no rows".
(b) does not widen what exclude can hide the way a substring would.

### V19-03 — the "surfaces that ignore exact/exclude" enumeration is short by five, and the shortfall is silent

**Severity:** P3 (docs; extends #970, whose owner decision was "scope the docs,
do not implement").

`query-parameters.md:134-141` names six readers plus the two history routes as
the surfaces that ignore `filter[exact:cluster]` / `exclude[cluster]`. Measured
on the same image, **eleven** surfaces ignore them — the six (confirmed), plus:

| surface | all | plain alias | `exact` alias | `exclude` alias |
|---|---|---|---|---|
| `/machinesets` | 43 | 8 | **43** | **43** |
| `/vm` | 884 | 48 | **884** | **884** |
| `/gpu/mig` | 56 | 33 | **56** | **56** |
| `/gpu/timeslicing` | 8 | 3 | **8** | **8** |
| `/fleet-heatmap` | 100 | 10 | **100** | **100** |

(The six readers reproduce #970's table exactly: for example `/nodes` 86 → 10
plain, 86 exact, 86 exclude.) Five more single-object routes also ignore both
modes because they read the plain form only: `/nodes/{node}`,
`/node/{node}/hourly-utilization`, `/vm/detail`, `/pvcs/detail`,
`/quota/detail`, `/cluster-quota/detail`, `/gpu/timeslicing/{node}`,
`/vms/{vm}/history`.

A reader of the current paragraph would conclude that `filter[exact:cluster]`
works on `/vm` — everything the paragraph does not name as an exception reads
as supported. This is the same "docs encode the bug as intent" failure the
recon playbook warns about, one revision later.

**Fix:** one sentence listing the honouring set (container list, `/container`,
`/namespace` + `/namespaces`, `/history`, `/quality/*`) and saying that every
other surface reads the plain form only, with the raw readers' two empty
behaviours as the existing detail. No code change.

### V19-04 — `/savings-summary` ungrouped: confirmed live (upgrades #966)

#966 records that the ungrouped response ignores `filter[cluster]` and says the
behaviour "is read from the code, not observed live". It is now observed, so
the evidence is recorded on the issue rather than duplicated here: no filter,
a UUID, an alias and a no-match alias all answer the same 16,219,909.32 EUR
across 30 clusters, while `group_by[idle_state]` answers 2 groups → 1 → 0 for
the same three values. The cache key has no cluster dimension
(`fleetsummary.GetSavings(orgID, rbacScoped, userPerms, engine, term)`), so
option B in #966 really is a cache change.

### V19-05 — two informational notes

1. **Client validation differs between the resolvers.** The map layer sanitizes
   the alias (`sanitizeParamValue`): `filter[cluster]=demo%` → `400 invalid
   character in cluster value`, a 300-character value → `400 … exceeds max
   length 253`. The Go resolvers (`resolveClusterFilterUUIDs`,
   `clusterFilterPredicate`) do not: the same inputs on `/nodes` and `/pvcs`
   answer `200` with count 0. Nothing is unsafe — every value is a bind
   parameter, and the pattern constructors escape `%`/`_` — but "the same bad
   value is a 400 here and a 200 there" is a contract a client can observe.
2. **`include` and `exact` compose with AND, undocumented.** On all four
   honouring families, repeated include values union correctly
   (`filter[cluster]=demo-special&filter[cluster]=demo-sleep` → 1,146 =
   960 + 186 on `/history`), but mixing the modes intersects:
   `filter[cluster]=demo-sleep&filter[exact:cluster]=demo-special` → **0**,
   where each alone returns rows (186 and 960). The docs say a repeated
   `filter[cluster]` is OR'd and say nothing about mixing modes. Either state
   the AND or union the two sets; today a caller who pins an exact cluster and
   also sends a substring gets an empty page.

## 4. Persona notes that produced no finding

- **Saboteur.** UUID spelling is now consistent: lowercase, UPPERCASE and
  undashed forms return identical counts on all 21 surfaces probed (this was
  v18's V18-01; the #951 canonicalization holds). Combined filters keep their
  argument indices correct (`/pvcs` with cluster + project + pvc + storageclass
  returns the expected 1, a no-match cluster returns 0, no 503s). Argument
  binding, allowlist keys and the org-scoped subquery all hold.
- **Security Auditor.** No injection surface: every client value reaches SQL as
  a bind parameter (`$n` / `?`), including the `containsPattern` output; the
  ADR-0169 allowlist validates key strings built only from constant atoms, so
  no client text can become SQL. The UUID shortcut for unrestricted callers
  (`resolveClusterFilterUUIDs` returns the parsed UUID without an org lookup)
  is safe only because each consuming query carries its own `org_id`
  predicate — verified in `queryHourlyNodeDigests`
  (`WHERE org_id = $1 AND cluster_uuid = $2::uuid`) and the equivalent builders
  behind `/vms/{vm}/history`, `/gpu/timeslicing/history` and
  `/snapshots/summary`. That is a load-bearing invariant worth a comment where
  the shortcut is taken.
- **New Hire.** `hcpCapacityClusterParam`'s comment says the bare, `filter
  [cluster_uuid]` and `filter[cluster]` spellings "all resolve identically
  (existing alias machinery, VM-history precedent)", but the next statement
  rejects any non-UUID with `400 invalid cluster_uuid`. The comment is wrong;
  the behaviour is #965's question. There is also no single place that says
  which of the three resolvers a new surface should use — the recon page
  describes the axes, not the map. A four-line table in
  `docs/agents/api-contract-recon.md` (surface family → resolver → modes it
  reads) would have made V19-01 obvious on first read.

## 5. What this pass did not examine

- **RBAC partial grants live.** The replay and these probes ran as an org
  admin; the intersection is argued from code and from the in-repo fixtures.
  Only IQE crosses the gateway with a real grant (#971, #962).
- **The lookup-failure (`503`) legs.** Unit-only, as recorded in the #968
  completion report.
- **Tag filters combined with the cluster filter** on the raw readers (the
  argument-index arithmetic past the tag clause), and pagination/cursor
  interaction with a union scope.
- **Performance** of the `clusters` subquery per request; not measured here.
- **koku-ui consumers beyond the request builders named above**: I read the
  builder for each affected surface I cite, not the components that call them.

## 6. Verdict

**CONCERNS.** The resolution machinery is sound — parameterized, org-scoped,
RBAC-intersected, and consistent on UUID spelling — and the four families that
honour all three modes do so correctly. The remaining risk is not in the SQL
but in the **wiring and the prose**: one detail handler reads a different
spelling than its siblings and answers with another cluster's object (V19-01),
exclude is described as something it is not (V19-02), and the exception list
in the public contract is half its real size (V19-03). Each is small; together
they are the same failure the phase was opened to remove, and two of the three
are one-line prose or six-line handler changes.
