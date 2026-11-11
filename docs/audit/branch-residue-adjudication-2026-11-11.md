# Phase17 Branch Residue Adjudication (GPT 6.1 Sol)

**Date:** 2026-11-11
**Reviewer:** GPT 6.1 Sol (fresh-context subagent), dispatched with reconciliation instructions.
**Question answered:** does any pre-rebase phase-branch commit (`pgarciaq-rosocp-superpowers-phase0`–`phase15`) represent work whose content is genuinely ABSENT from phase17 @ `3757cc635c3eb5779b81dca460acbc9795ae06d8`?
**Companion artifact:** `branch-review-phase17-2026-11-11.md` (the integration review).

**Verdict: no missing runtime implementation found, but the residue is not empty. Significant OpenAPI content was not carried forward.**

| Classification | Subjects |
|---|---:|
| EVOLVED-PRESENT, without identified missing content | 69 |
| EVOLVED-PRESENT implementation **and ABSENT OpenAPI portions** | 108 |
| ABSENT OpenAPI content in specification-focused commits | 8 |
| UNRESOLVABLE | 0 |
| **Total** | **185** |

The mixed classification is intentional: calling an entire feature absent would be wrong, but calling its original specification work present would also be wrong. No files were modified; no test suites were run.

## 1. Set construction

For every local `pgarciaq-rosocp-superpowers-phase*` branch, `git cherry pgarciaq-rosocp-superpowers-phase17 <branch>` `+` entries were collected, resolved to subjects, and deduplicated by exact subject across branches.

| Branch suffix | Unequivalent commits | Subjects outside phase15 |
|---|---:|---:|
| phase0 | 3 | 1 |
| phase1 | 5 | 1 |
| phase2 | 6 | 1 |
| phase3 | 7 | 1 |
| phase4 | 9 | 2 |
| phase5 | 11 | 2 |
| phase6 | 23 | 1 |
| phase7-plugins | 37 | 2 |
| phase8 | 37 | 2 |
| phase9 | 47 | 0 |
| phase10 | 51 | 0 |
| phase11 | 67 | 0 |
| phase12 | 124 | 0 |
| phase13 | 130 | 0 |
| phase14 | 138 | 0 |
| phase15 | 181 | 0 |
| phase16 | 0 | 0 |

**The sets are largely, but not completely, cumulative.** Every phase14 subject appears in phase15, but four earlier subjects do not:

| SHA | Subject |
|---|---|
| `5652fe5112fd` | Address PR review: add missing tests, remove plan doc |
| `f4ec21451d9f` | COST-5691: Make legacy GORM query compatible with native engine rows |
| `1b19e1d8e507` | Add canonical namespace recommendations route to native engine path |
| `ce0acc8a1d37` | Fix P2 batch 5: error handling and API response consistency |

The correct union is **185 subjects** (not 181), representing **229 distinct original SHAs** including different pre-/post-rebase versions of the same subject.

**Reconciliation technique:** 173 subjects have same-subject commits reachable from phase17; their actual added/deleted content was compared, not just subjects. Most substantive differences are omitted `openapi.json` changes; remaining differences include upstream incorporation, dependency regeneration, and later evolution. The 12 unmatched subjects were reconciled directly against current files. Duplicate-subject SHA variants were checked — notably, the larger robustness variant `c6f8857c6c8b` is accounted for by reachable `5ca31ead` plus `490434f0`; its remaining HTTP-client difference evolved into the dedicated bounded CSV client.

## 2. Reconciliation table

**Legend:** **P** — EVOLVED-PRESENT; **P+A** — implementation present, specified OpenAPI portion absent; **A** — original specification deliverable absent or incomplete. `→ SHA` identifies a phase17-reachable counterpart whose patch was compared. Schema/parameter omissions refer to the current `openapi.json`.

### Foundations, engines, and plugin transition

| Original SHA | Subject | Class | Evidence |
|---|---|---|---|
| `66ed571047da` | Fix 12 critical robustness bugs | P | → `490434f0`; larger original variant also incorporated through `5ca31ead`. CSV HTTP handling subsequently evolved. |
| `5652fe5112fd` | PR review: missing tests, remove plan | P | → `937d010c`; original DB-error, Sources-listener, and poison-message tests also exist in current test files. |
| `a14d38538bfc` | Remaining PR review comments | P | → `4e0056a5`; bare-HTTP CSV exception superseded by dedicated bounded CSV client. |
| `2cbc4649b7b7` | Daily digest schema and computation, M1–M4 | P | → `8658b776`; substantive implementation carried forward. |
| `61cf9d5aa196` | Native recommendation engine, M5–M8 | P | → `589a7e4a`; CSV reader remains, with evolved download implementation. |
| `7d56acfe4ccd` | Phase2 fallback handlers and container migration | P | → `ff8478bb`; nonmatching patch line is an obsolete route comment. |
| `1229ade8bd59` | Notification persistence and engine fixes | P | → `b635ea26`; startup-call naming/context evolved. |
| `22ac022311c9` | OOM memory bump | P | → `ca5eb803`; added/deleted content matches. |
| `71a0fd2bea63` | History, boxplots, retention | P | → `d6dea817`; engine-switch context evolved. |
| `cf3e5ea9d686` | UBI10 Go toolset; remove deprecated go get | P | → `20bc2a00`; base-version context differs, intent retained and subsequently upgraded. |
| `8a7bb14f4455` | Native namespace recommendations | P | → `54cf8586`; namespace route variant also carried forward. |
| `1b2c3d27599e` | Phase6 audit, tests, migration 000034 | P+A | → `1bd99379`; namespace format/error-response specification portions absent. |
| `5c18924e0eea` | HTTP-client and OpenAPI review alignment | P+A | → `b91687ea`; HTTP handling evolved; documented detail errors/payload portions missing. |
| `fca3931c90ea` | Estimated monthly savings pipeline | P | → `b3458b0b`; current counterpart contains the substantive additions. |
| `9c1c76a8f8be` | Snapshot staleness detection | P | → `f7cc5644`; original USD field evolved into `estimated_monthly_cost: MoneyAmount`. |
| `83cd9f6ff5a9` | Namespace default-on and kill switch | P | → `89d79dda`; intermediate switch intentionally superseded by subsequent removal/plugin control. |
| `9ab49a2f4563` | Node sizing, thresholds, EMA | P | → `458eb073`; substantive patch content matches. |
| `8e569d7c9173` | Remove namespace flag gating | P | → `710064a5`; removal retained; later deprecated-variable cleanup reinforces it. |
| `ec66cf617466` | IDOR, SSRF, fleet RBAC | P | → `712d4efb`; CSV-specific implementation subsequently hardened. |
| `da9ca6704d11` | Logging, Kafka shutdown, CSV streaming | P | → `79a34661`; substantive content matches. |
| `2324e84f434e` | GPU summary endpoint | P | → `36713d9d`; substantive content matches. |
| `550ba6301ac4` | Configurable DB pool and acquire timeout | P | → `22504717`; substantive content matches. |
| `dc96f9e40cb1` | Graceful shutdown and error propagation | P | → `5357b55f`; differences concern renamed signatures/calls. |
| `4a2b822f9a13` | Plugin switch replaces UseNativeEngine | P | → `0a19e40c`; substantive content matches. |
| `360dd09275f4` | Plugin-disable proof test | P | → `a2f8dbd4`; substantive content matches. |
| `2cce1604e3cb` | GPU thresholds into Config | P | → `44ae325f`; substantive content matches. |
| `89ac4330fa76` | Plugin fallback, retention, env bridge | P | → `7faaec9a`; excluded dependency differences include an actual corrupt go.mod conflict fragment, not missing functionality. |
| `d3e55d3d4d5e` | Disabled-plugin 404 and startup logging | P | → `ddc7c376`; substantive content matches. |
| `46480cb6f4ad` | OpenAPI mismatches #64–#83 | P+A | → `e5000b26`; portions of the original error/schema corrections missing. |
| `01d436165d8f` | Full DetailResponse OpenAPI schema | A | No reachable full counterpart. `DetailRecommendations`, `ResourceConfig`, `ResourcePair`, `ResourceValue`, `NamespaceDetailResponse`, and unified term/engine schemas are absent. Plot portion was later reimplemented. |
| `bfd4b3f1e342` | os.Exit cleanup, RBAC test, locked_fields | P | → `ff066287`; substantive content matches. |
| `b5060c3874e2` | P3 test anti-patterns | P | → `e197d638`; residual difference is empty-line/closing-brace context. |
| `c1d60a8954e5` | godotenv support | P | → `3232a79f`; dependency present in current go.mod; vendor implementation carried forward. |
| `4b37b27353b7` | Clowder PostgreSQL 13→16 | P | Current `clowdapp.yaml:259` specifies `version: 16`. |
| `fb85da8cc852` | CodeQL v3 actions | P | → `0dc4d116`; current workflow evolved to CodeQL v4, checkout v4. |
| `2a50d7c30eb7` | Dead code and naming cleanup | P | → `f6bc7f08`; remaining difference is deletion of an already-removed package line. |
| `b8afede5dad6` | Business-hours kill switch | P | → `c5ba330f`; substantive content matches. |

### Savings, quotas, VM features, and settings

| Original SHA | Subject | Class | Evidence |
|---|---|---|---|
| `58aba12a4cb4` | Savings, currency, dual node engines, fleet | P+A | → `52bd3eb7`; node filters/sorts and associated typed specification portions missing. |
| `e5d7255f5911` | Threshold Settings API | P+A | → `42df42af`; threshold request/response schemas incomplete. |
| `5bacc116ce66` | "Testing & Quality Assurance page" | P | Misleading subject: actual patch adds goleak, snapshot tests, threshold-cache tests/metrics — all present. Public page independently exists via `156e6a2c`. |
| `36906334d936` | Shared outbound HTTP transport | P | → `b4871dbd`; substantive content matches. |
| `66c83bbcc65d` | REST/GPU pgx tuning variables | P | → `d1262388`; substantive content matches. |
| `380f8c00f70b` | Startup health, exclusivity, structured savings | P+A | → `113e5ba2`; detail savings specification incomplete despite later MoneyAmount replacement. |
| `063aff9f3359` | Repair go.mod and regenerate go.sum | P | Current go.mod has no duplicate module entries/conflict markers; required dependencies and populated go.sum exist. |
| `84b742f43421` | Unified savings and tag filtering | P+A | → `0cfd59e3`; tag/filter and response-shape specification portions missing. |
| `65b11ac19804` | Savings tag grouping and warnings | P+A | → `433a8c8b`; grouping response alternatives and warning fields missing. |
| `e681f85eac7e` | Idle-detection contracts and OpenAPI | P+A | → `3d1d9b31`; idle filters/grouping and associated contract details missing. |
| `7432e2beab6c` | Savings/spec/route/migration/race test fixes | P+A | → `6f4a669e`; namespace pagination/sorting specification portions missing. |
| `d9beb3c28dd2` | Quota unit tests | P | → `31dbf6e3`; substantive content matches. |
| `fed0fbb06fbd` | Quota spec, cheatsheet, Bruno | P+A | → `37c272b3`; original quota parameter/schema coverage missing from spec. |
| `e840ec2195d8` | Quota settings, three-tier resolution | P+A | → `967470c3`; typed settings and error contracts missing. |
| `f3eedd06cb7b` | ClusterResourceQuota recommendations | P+A | → `a41e4777`; runtime present; current CRQ spec remains largely generic. |
| `55e9cc51c959` | VM history table and endpoint | P | → `01cd50de`; substantive content matches. |
| `5948ab28aceb` | DELETE, VM settings, settings_locked spec | P+A | → `213733e9`; some later settings corrections exist, but original lock/schema coverage remains incomplete. |
| `75c70444cd03` | Comprehensive entity/settings OpenAPI | P+A | → `96a504dd`; substantial original typed VM/GPU/node/quota specification content absent. |
| `450281ab9985` | VM cpu_adaptive_margin_enabled | P+A | → `8c709c00`; GET field survives inline, but original typed PUT/update contract does not. |
| `b1cfbdcae2c3` | Complete VM test-plan checklist | P | → `9adc0845`; history-retention field survives in current inline VM settings schema. |
| `95dd7665377b` | Dedicated settings endpoints; deprecate alias | P+A | → `d22337a3`; routes present; original deprecation/schema/request documentation incomplete. |
| `b76d7ba6eb61` | Network and vGPU implementation reconciliation | P+A | → `e7c48e96`; typed network/settings/metadata contract portions missing. |
| `b2e849f8ca53` | is_network_bound and guest_os filters | P+A | → `951bf61d`; those VM parameters missing from spec. |
| `4e7fcbe53446` | Sequential/random VM disk I/O profiling | P+A | → `0cf9e4bf`; `VMIOProfile` specification absent. |
| `6ba47495b360` | VM placement, correlation, NUMA | P+A | → `4ebc513b`; typed settings/metadata specification missing. |
| `5b4e8f2a6850` | VM savings from Koku rates | P+A | → `84fc5f4e`; VM item/savings specification absent, not merely an old schema-name difference. |
| `64efe2fe28eb` | VM savings in fleet and idle costs | P+A | → `b288b7d4`; savings-summary engine/by-plugin contract missing. |
| `52a85e183733` | VM power-off scheduling | P+A | → `222faff7`; typed power-schedule/settings/metadata contract missing. |
| `6cb5a4c99b05` | VM Network QoS notifications | P+A | → `c7a4f2bf`; typed network-QoS settings contract missing. |
| `bd5c85db93d6` | VM storage-tiering notifications | P+A | → `9adbaf76`; typed storage-tiering settings contract missing. |
| `1a34465a3856` | Migration/config/API hardening | P+A | → `3c0f5170`; typed NUMA placement setting missing from specification. |

### Phase12 alignment and API completeness

| Original SHA | Subject | Class | Evidence |
|---|---|---|---|
| `cbd7ed608f74` | Phase12 node/namespace documentation | P+A | → `dbf8191c`; node idle-state filter/classification specification missing. |
| `26f0f648a3c5` | Populate node instance_type | P+A | → `e124b546`; detail documents it, but original list-row field coverage is absent. |
| `323504655201` | GPU summary count semantics | P+A | → `eb66eeb3`; runtime/docs evolved; original time-slicing cluster parameter coverage missing. |
| `16cf970a0015` | Multi-term PVC detail | P+A | → `148502d2`; detail parameters and typed response missing. |
| `606ad938097b` | PVC pod/workload context | P+A | → `506e75dc`; `mounted_by` absent from PVC item specification. |
| `9d2aa30aa677` | Quota dependencies and API specification | P+A | → `035d6577`; quota sorting/detail contract incomplete. |
| `3a730cd7f2db` | CRQ namespace/project/crq filters | A | Current CRQ list documents only tag parameter; original namespace/project/crq aliases are missing. |
| `1100f18147ca` | CRQ history schema | A | `ClusterQuotaRecommendationHistoryEntry` and typed detail `history` contract absent. |
| `f8a6e7ca658e` | Namespace recommendation history | P+A | → `d3d383af`; endpoint exists, but parameter/typed-response contract missing. |
| `d9d52129b9bc` | Node detail, recalc, consolidation | P+A | → `f3f7fdd1`; portions of node term/engine/list/detail contract absent. |
| `b5ec399a062f` | Storage vm_name ingestion and PVC API | P+A | → `ee40d624`; PVC/related MachineSet contract additions incomplete. |
| `8a29ecf638b9` | Namespace quota storage/pods | P+A | → `ea1caa40`; runtime present; namespace stale-filter specification additions missing. |
| `c5fa2af7448d` | Quota storage/pods capacity_freed | P+A | → `a1ea26ab`; associated quota-name/sorting contract additions missing. |
| `1f31083e7675` | Quota/CRQ savings recalculation | P+A | → `0e715d8b`; related snapshot-summary/recalculation contract additions incomplete. |
| `41f708b12861` | MachineSets, snapshot summary, codes spec | P | → `54be24a9`; substantive patch content matches. |
| `6f3bb2bfcc00` | CRQ group_by[cluster] | P+A | → `48fa6b7a`; grouping parameter and typed grouped count absent. |
| `36ab9c414d4f` | CRQ storage/pods schemas | P+A | → `7400785e`; resource-value/utilization schemas absent. |
| `0bc4912cff24` | CRQ feature page and order tests | P+A | → `c801abbe`; `filter[cluster_resource_quota]` specification absent. |
| `b62a84e472eb` | Snapshot inventory_fresh_hours schema | A | Missing from current GET, PUT request, and PUT response specification; actual API/tests still support it. |
| `5745baaebe30` | CRQ object-count behavior specification | A | Object-count exclusions/risk semantics absent from CRQ OpenAPI entries. Equivalent public feature prose survives, but not the specification deliverable. |
| `08852d937fbe` | Full plugin coverage in OpenAPI introduction | A | Current introduction omits quota, VM, CRQ, idle-detection coverage added by this commit. |
| `3d0c339a5bf2` | Container keyset and engine/term specification | P | → `9efbdd9b`; substantive content matches. |
| `5978651e33f1` | Quota none enums and contract tests | P+A | → `bfc3b426`; explicit `none` enum documentation missing. |
| `90289e6605e8` | Quality engine filter and dual-engine docs | P | → `16885936`; substantive content matches. |
| `5ff6e4a14571` | Idle/zombie improvements | P+A | → `9ae0d15a`; GPU idle/namespace projection and related specification portions missing. |
| `cfed1735badc` | Container/namespace percentile settings spec | P+A | → `da3a393b`; typed sizing settings and PUT bodies missing. |
| `9665bf7860c5` | Effective business-hours endpoint | P | → `d183c373`; substantive content matches. |
| `94684f5b8f1c` | Node filter spec/history-doc correction | P+A | → `83da49ce`; original node filter parameter coverage missing. |
| `20166e5e7a23` | Node alignment tests and RBAC spec | P+A | → `0afc9509`; original node/MachineSet error-contract coverage incomplete. |
| `8f8ce4cebc8f` | History/quality gauges, index, degradation | P+A | → `c5f41f3c`; `QualityRow.engine` specification missing. |
| `22fa585dc658` | Canonical filter[project] | P+A | → `515751e2`; VM and CRQ project parameters absent from spec. |
| `c7c438d61abb` | VM savings sort and CSV specification | A | VM list/history lack original format parameter and `text/csv` response contracts; sorting parameters also absent. |
| `0da670a7aeca` | GPU MIG filters/docs/OpenAPI alignment | P+A | → `0c21080b`; MIG cluster/project/format contract incomplete. |
| `083307e94ba6` | GPU time-slicing CSV specification/test | P+A | → `141a7469`; format parameter and CSV response contract absent. |
| `66d4849d8f70` | Quota CSV and API/engine tests | P+A | → `66a2ec14`; original quota/PVC filter/export contract portions missing. |
| `3359457906c9` | Snapshot summary recommendation-type filter | P+A | → `97267632`; summary parameter/schema coverage incomplete. |
| `dcfff947a69a` | Tag docs/tests/OpenAPI parameters | P+A | → `c034b30a`; original tag parameter coverage missing on several plugin routes. |
| `29b2345d7b82` | Keyset pagination and PVC confidence | P+A | → `07a486c1`; PVC cursor/confidence/pagination-meta specification missing. |
| `2272bbec6e29` | Quota docs and retention alignment | P+A | → `ce728e03`; CRQ CSV format/response documentation absent. |
| `04e8ed63cca3` | Node/GPU confidence_level | P+A | → `d45c7276`; corresponding confidence fields/term schema missing. |
| `b31cfd42156d` | Unified savings shapes and meta.currency | P+A | → `28babc65`; several pagination/money/meta contracts remain incomplete. |
| `2693125dc5be` | SavingsObject→MoneyAmount | P+A | → `8edc2db7`; MoneyAmount exists, but original endpoint/property wiring was not fully preserved. |
| `420240684981` | Snapshot summary namespace alias | P+A | → `afff873f`; summary group-by parameter/alias contract absent. |
| `3074bf901d22` | Snapshot money, keyset, CSV | P+A | → `1e1a82d7`; money partly survives; cursor/sort/export contract absent. |
| `7a27bea15ec8` | MachineSet audit/time-slicing savings docs | P+A | → `a7073633`; MachineSet export/parameter specification incomplete. |
| `897e22798e22` | Business-hours docs/spec alignment | P | → `45298ec0`; substantive patch content matches. |
| `b675c44fe898` | Fleet by_plugin MoneyAmount | P+A | → `ec2c13a8`; typed by-plugin savings response absent. |
| `9366a0162635` | analytics_incomplete specification | A | Neither `analytics_incomplete` nor `analytics_incomplete_at` occurs in current spec; runtime/public documentation retains the behavior. |

### Hardening, performance, and projection

| Original SHA | Subject | Class | Evidence |
|---|---|---|---|
| `4aeba84d0a5f` | Security/API hardening configuration | P | → `322dde5d`; substantive content matches. |
| `ac4c95d90a6b` | Readiness, pagination, recalc guards | P | → `fa8321e9`; substantive content matches. |
| `11f0ee775b57` | Adversarial findings #42/#43/etc. | P+A | → `31c91d97`; `ingest_hooks_failed` fields absent from spec. |
| `b5d462084aee` | Specification sync and ingest-hook fields | P+A | → `45b0adc7`; extensive original contract additions absent, including cursor/error/schema coverage. |
| `e3ad8e95f2f3` | Document adversarial fixes | P | → `69b86061`; substantive content matches. |
| `d150058fa251` | Remove phantom AWS SDK v1 | P | → `9f2e25e5`; dependency/vendor removal carried forward; generated lockfile evolved. |
| `201a230faae1` | SPARSE_DATA code 77 | P+A | → `6ea62b60`; code exists; sizing-threshold specification absent. |
| `577511786ca6` | Informational findings #70–#74 | P+A | → `b4c8a000`; original entitlement/error-response specification incomplete. |
| `287182b2547b` | Adversarial v4 findings #81–#84 | P+A | → `6e380193`; original differentiated forbidden-response contracts incomplete. |
| `dc59c8ab2823` | Connection budget and DB_POOL_SIZE | P | → `c12a2ed4`; old default context differs, intended configuration retained. |
| `2f3d9fd599ec` | Runtime /healthz checks | P | → `57cbae2f`; current docs explicitly classify health probes as non-OpenAPI routes. |
| `2217a7e7ab6a` | Digest plots and sample retention | P | → `a0c1569f`; plot schemas evolved into current `NativePlot` family through `14e968ad`. |
| `9b034998c957` | Engine-level-only notifications | P+A | → `bbe05278`; slim list/engine notification schemas absent. |
| `51418601c106` | Slim namespace DTO/count-only shortcut | P+A | → `2e605f8d`; slim list/projection schemas and parameters missing. |
| `eebf82fef6d4` | Node SQL/postaggregate ordering docs | P | → `4ea81070`; substantive content matches. |
| `6f61ba1e0615` | Time-slicing history/backfill specification | P+A | → `63e7c6f0`; internal backfill exclusion is documented, but public history typing/parameters remain missing. |
| `26ba26fbf055` | include=explanation detail support | P+A | → `5495a820`; include parameters and explanation schemas missing on multiple routes. |
| `56fb95853a57` | Remove deprecated engine/VM/namespace flags | P | → `9b437785`; removals retained; obsolete specification references are gone. |
| `ee3f5b98c824` | Ten container gap findings | P+A | → `6acac4b3`; list/explanation specification additions absent. |
| `36dd16cd50d1` | Final three container gap findings | P+A | → `be842577`; list/detail schema additions absent. |
| `c3e63ad975d6` | Namespace OpenAPI parameters | P+A | → `41c63372`; original alias-route filter/sort coverage absent. |
| `17ec3e286a2e` | Namespace detail idle_state | P+A | → `cc534591`; typed namespace-detail idle classification absent. |
| `7b0d910863cd` | PVC cross-source alignment audit | P+A | → `97ec402e`; PVC explanation schema/wiring absent. |
| `8afb4e1ba2ab` | Namespace savings sorting | P+A | → `d1889a98`; original namespace sorting specification absent. |
| `b35c6eab57c1` | Namespace pagination/sort/cursor fixes | P+A | → `5d77e997`; corrected namespace list/detail specification not preserved. |
| `ba5d5d8834c7` | Term/engine projection | P+A | → `5e4f867c`; projection parameters missing on several documented detail routes. |
| `a0d45c775b99` | PVC/snapshot grouping filters | P+A | → `aa7c9199`; grouping/filter/count contracts missing. |
| `00bd05a564de` | Deterministic recommendation IDs | P+A | → `da78e571`; ID contracts absent from multiple plugin schemas. |
| `c4c79a0b0ad5` | Namespace tags/PVC last_reported | P+A | → `ec2f305e`; corresponding list schema fields absent. |
| `13aa8d41276c` | Remove workload-type allowlist | P | → `0f73ea5b`; implementation and evolved specification retained. |
| `dded70bf8415` | Dynamic /workload-types endpoint | P | → `75f70078`; substantive content matches. |
| `e844f79ca030` | Pin local PostgreSQL16 | P | Current `scripts/docker-compose.yml:33,43` specifies postgres:16. |
| `b6b18c392ba2` | VM estimated_monthly_savings naming | P+A | → `abdcaa48`; runtime rename retained, typed VM item/sort contract absent. |
| `b6e4bbc0152d` | GPU savings/term/classification API gaps | P+A | → `a9d2312d`; parts of term/classification/money wiring missing from specification. |

### Visual Insights and later phase15 work

| Original SHA | Subject | Class | Evidence |
|---|---|---|---|
| `b10f8359156f` | Visual Insights spec/changelog/docs | P+A | → `f8d21c60`; OOM timeline typing/parameters and related contract portions absent. |
| `d90a1804f8d6` | Align GPU summary time-slicing count | P+A | → `5537f62f`; runtime change retained; original actionable-count explanation absent from summary schema. |
| `0951087e35c1` | Add golang-lru/v2 | P | → `afa9a9e0`; dependency/vendor implementation retained. |
| `d994f8778993` | Snapshot age histogram | P+A | → `5bc7aa0b`; endpoint present, original histogram parameters/schema incomplete. |
| `a3d64aff4bc9` | VM per-day I/O metrics, #9 | P+A | → `a080a064`; runtime fields retained; `VMDailyDigestItem` typing absent. |
| `fcc90158c40a` | Snapshot cost-by-type, #19 | P+A | → `69cf0eae`; endpoint retained; original typed aggregation response absent. |
| `43b8d6cfc23f` | Business-hours plot overlays | P | → `3151393d`; current term schemas contain `business_hours_plots` using evolved `NativePlot`. |
| `e4c956c9d122` | Replace hand-rolled LRUs | P | → `6b9f5a16`; substantive implementation retained; dependency declaration evolved. |
| `04d6200128d5` | Category response/filter support | P+A | → `3a95de35`; original namespace/list category contracts incomplete. |
| `f3daeaa679e9` | MIG SQL keyset pagination | P | → `0a8cb21f`; intermediate approximate/enrichment model superseded by persisted MIG implementation. |
| `78ec545eb17d` | GPU total_fb_mib | P+A | → `8fd110e4`; original container GPU field absent from specification; BH-specific field is not an equivalent replacement. |
| `18a73e1001ca` | Quota headroom trend | P+A | → `1e053257`; endpoint retained; typed trend parameters/response absent. |
| `dc27812ca72f` | Node daily utilization digests | P+A | → `d6e498b3`; current detail exposes generic object rows, not original typed digest contract. |
| `e020c2c9ce97` | Fleet heatmap and cache | P+A | → `1988ae47`; original `FleetHeatmapNode.idle_state` contract absent. |
| `256f922bc5a2` | Node hourly utilization | P+A | → `6af3d81f`; route parameter renamed, but current replacement remains an untyped placeholder. |
| `8a4b594ce39e` | Container abandoned→zombie migration | P | → `b10d443f`; substantive content matches. |
| `b5fcaff6b013` | Replica-count optimization | P+A | → `db40e758`; replica optimization/settings specification missing. |
| `ff652b431937` | Node data_days_available | P+A | → `457c2c65`; original meta field absent from specification. |
| `2929c78d0be9` | Node/GPU/VM group_by | P+A | → `e88424ae`; grouping parameters missing from corresponding spec entries. |
| `0c0ca63a2e62` | PVC/VM quality specification/docs | P | → `e4af0201`; substantive content matches. |
| `17f20a788358` | MIG/snapshot quality specification/docs | P | → `cfdf0c08`; substantive content matches. |
| `e34de65da71a` | min_data_days list metadata | P+A | → `c73dc1d0`; corresponding meta fields missing from multiple schemas. |
| `25fd2ae60a1f` | Node daily peak CPU/memory fields | P+A | → `9149aad4`; original typed digest fields missing. |
| `08251be99b37` | Node request-vs-usage fields, #23 | P+A | → `b6e94ca9`; original typed digest request fields missing. |
| `2ef31d1e0ed2` | Persist MIG recommendations, #102 | P+A | → `9f4520ef`; persistence retained; original workload-filter contract absent. |
| `48f3d6f1ba35` | History explanations/term enum, #49 | P+A | → `d73eda47`; 21 explanation fields absent; current HistoryRow still documents short/medium/long. |
| `6308209cd617` | Node GPU count/multi-GPU awareness | P+A | → `acaa3d75`; detail node count partly restored, original MIG `gpu_count` contract missing. |
| `f592841ff1b3` | Fix55 unit-test failures | P+A | → `4a29bf49`; test changes retained; original intended typed specification repairs not fully retained. Historical JSON corruption itself should not be replayed. |
| `2fee3786213f` | Per-org rate limiting, #37 | P | → `21806f0e`; implementation matches; #37 completion report describes the original implementation. |
| `0dac0dbf9731` | Low-severity adversarial findings | P | → `46924401`; substantive content matches. |
| `ac257f68775f` | Performance quick-win docs/tests/spec | P+A | → `ca9b6362`; original date/bucket parameter constraints absent from specification. |
| `2ef316bbd093` | Timeouts, date caps, CSV sanitization | P+A | → `3837d961`; runtime guards retained; original date-cap contracts absent. |
| `f4ec21451d9f` | Legacy GORM/native-row compatibility | P | → `cf4aca17`; evolved counterpart adds further handling; original difference is indentation context. |
| `1b19e1d8e507` | Canonical native namespace route | P | → `fff164ec`; counterpart contains original additions plus further route wiring. |
| `ce0acc8a1d37` | P2 error/API consistency batch | P | → `b652add3`; counterpart contains original additions plus further changes. |

## 3. Tracker cross-checks and verdict

Several historical subject numbers refer to upstream issues or review findings, **not current fork issues** (e.g., historical "#70", "#473", "#482" are unrelated to fork issues of those numbers) and were not used as evidence. Relevant fork reports (#23, #37, #49, #72, #84, #102) often cite pre-rebase SHAs; their implementations map to the reachable counterparts above.

### ABSENT list: specification-focused commits

| Commit | Missing deliverable | Follow-up |
|---|---|---|
| `01d436165d8f` | Complete detail/namespace/resource/engine schema coverage | Reconcile current native and compat contracts under **#605**; do not blindly replace compat schemas. |
| `3a730cd7f2db` | CRQ namespace/project/crq filter contracts | Restore current supported parameters under **#90/#605**. |
| `1100f18147ca` | CRQ typed history/detail contract | Document current response and history rows under **#90/#605**. |
| `b62a84e472eb` | Snapshot inventory_fresh_hours GET/PUT contract | Restore field, bounds, and default from current settings implementation/tests under **#90**. |
| `5745baaebe30` | CRQ object-count semantics in OpenAPI | Port current policy from implementation and surviving feature documentation under **#90**. |
| `08852d937fbe` | Accurate plugin-coverage introduction | Refresh against `internal/plugins/plugins.go` under **#90**. |
| `c7c438d61abb` | VM sorting and CSV contracts | Document current supported sorts and list/history CSV behavior under **#90/#605**. |
| `9366a0162635` | Analytics-incomplete response fields | Document current fields and applicability under **#90/#605**. |

The **108 P+A rows** identify additional absent specification portions within otherwise-preserved work; their follow-up is the same: reconcile the named contract portion against current code, principally through **#90** and **#605** — not replay the historical patches wholesale.

### Existing ledger explains the broad gap

- **#90** remains open for complete OpenAPI request/response/filter maintenance.
- **#605** explicitly records that native response shapes are absent from the specification and block generated-client work.
- Closed **#570** restored public endpoint documentation, not complete machine-readable schemas.
- Closed **#572** restored route coverage and reverse route/spec checks, not response-shape completeness.
- Independent confirmation: `internal/api/openapi_handler.go` reads the root specification and filters paths; it does not generate the missing schemas dynamically.

**Runtime ABSENT list: empty. Specification ABSENT list: nonempty, as enumerated. UNRESOLVABLE list: empty.**

## 4. Confidence

- **185/185 subjects adjudicated with direct Git or current-file evidence; no inference-only rows.**
- **173/185 — 93.5%:** reachable counterpart plus actual patch comparison.
- **12/185 — 6.5%:** unmatched subjects reconciled directly against current files.
- Semantic evolution was checked where straightforward key comparisons would mislead: MoneyAmount, native percentile plots, renamed routes, deprecated flags, and operational-route exclusions.

**High confidence that the phase-branch residue contains no missing runtime implementation. High confidence that substantial original OpenAPI work is absent.** This is a content reconciliation — not a claim that retained implementations are behaviorally correct or test-passing.
