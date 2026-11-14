# Per-Area Summary — the fork's delta since March 2026

> The fork's changes per subsystem along the data path. Each section:
> what changed, why, the gotchas a reviewer should probe, and the
> evidence pointers. Commit counts per directory are from
> `git log --since=2026-03-01 -- <dir>` (2026-11-14).

## 1. Ingestion & processor (287 commits — `internal/ingestion` + `internal/services`; ingestion alone 177)

CSV payloads from the metrics operator (or Nise in test) are parsed
once, classified per business-hours schedule, and upserted into
range-partitioned hourly digests per entity (container, namespace,
node, PVC, VM, GPU, quota, cluster-quota, snapshot). The processor
(`rosocp`) orchestrates Kafka consumption → staged digest writes →
recommendation computation → serving tables, with per-phase Prometheus
metrics (`rosocp_pipeline_phase_duration_seconds` et al.).

- **Why:** read-once compute-n-terms (ADR-0003) — every term's
  percentile derives from the same staged digests, never re-parsed.
- **Probe:** the upsert-safety rules (`docs/agents/db-upsert-safety.md`)
  — idempotent replays, chunk-grain flushes, partition ensures.
- **Evidence:** #826 (off-leg p95 statistic), #827 (observation
  coverage floors), #829/#836 (multi-tarball retry semantics — #836
  open), the async-jobs lifecycle in `internal/asyncjobs`.

## 2. Native recommendation engine (486 commits — `internal/engine`)

The percentile math and per-entity recommenders: exact CPU/memory
percentiles per term with decay-weighted averaging, dual cost/
performance rows per identity (ADR-0004), adaptive margins
(ADR-0007), the 25m CPU floor (ADR-0008), idle/zombie classification,
GPU and MIG awareness, VM and node recommenders, HCP capacity
advisories, and the correlate rules (§6). `librobne/` is the CGO-free
core the robne CLI shares — the parity anchor.

- **Why:** ADR-0001's founding case; the statistic choices are
  individually ADR'd (0002–0008).
- **Probe:** `librobne` must stay CGO-free (`make robne` builds with
  CGO_ENABLED=0); the server builds with CGO_ENABLED=1 (Kafka client).
- **Evidence:** the engine's test corpus; the term configuration
  (`internal/engine/term_config.go`, per-plugin windows, tenant
  overridable, env-lockable); the idle-classification tests.

## 3. Serving API & list architecture (520 commits — `internal/api`)

The REST surface behind `/api/cost-management/v1/`: 164 handler files
covering list/detail/history/CSV per entity, settings (terms,
thresholds, business hours), savings summaries, fleet endpoints. The
list architecture is keys-table-driven: `org_container_keys` /
`org_namespace_keys` paginate by keyset cursor with sort-alias
translation (`NsAllowedOrderBy` and the container twin), detail-only
filters gate eligibility via a single conjunction-bearing EXISTS
(#841/#842), and term/engine projection rides `filter[term]` /
`filter[engine]` (list projects short+cost by default).

- **Why:** the kruize-compat shape had to keep serving while the
  storage changed underneath — same URLs, native rows.
- **Probe:** tenant isolation — every list/detail predicates `org_id`
  (the #840 P1 was a detail-expansion leak; its regression and the v14
  cluster-alias analysis pin the family); the OpenAPI contract tests
  (`openapi_contract_test.go`); the #853 EXPLAIN guard
  (`eligibility_exists_explain_test.go`) watches the plan shape in CI.
- **Evidence:** #840–#845 + #850/#852 (all closed with reports); the
  docs-site feature pages carry the per-endpoint contracts.

## 4. Entity plugins (`internal/plugins`)

Twelve plugins (container, namespace, node, pvc, quota, cluster-quota,
gpu, vm, slo, snapshot, hcp, kruize) registered via blank imports in
`plugins.go` — individually enable/disable-able via
`ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS`; disabled routes 404.
The kruize plugin is registered but **disabled by default** (the
legacy/rollback path).

- **Probe:** plugin lifecycle integration tests; the mutual-exclusivity
  rule (namespace endpoints stay available in kruize mode).
- **Evidence:** `docs-site/architecture/plugin-architecture.md`; the
  plugin lifecycle tests in `internal/plugins/`.

## 5. Business hours

Org/cluster/namespace-scoped weekly schedules (IANA timezone, day
list, HH:MM window, overnight wrap, off-hours weight) classify every
sample into all-hours vs business-hours digest streams. **Persisted
recommendations derive from the all-hours stream**; the business-hours
view is computed at detail-GET time from the BH digests (the dual
contract — see `docs-site/features/business-hours.md`). The sleep
advisory evaluates against the same classification. The dual-digest product contract lives in
`docs-site/features/business-hours.md`; ADR-0348's amendments record
the evaluation semantics (evidence legs, p95 statistic, coverage
floors, schedule attestation, applicability by classified hours).

- **Probe:** the applicability derivation (#845: classified hours, not
  UTC weekday — timezone-shifted schedules and the overnight
  post-midnight day are pinned by tests); the attestation fingerprint
  (#831) and its user-workload-scoped gate (#847).
- **Evidence:** ADR-0348 + its amendment chain; the BH feature tests
  (`internal/api/handlers_business_hours*_test.go`,
  `internal/ingestion/*business_hours*_test.go`).

## 6. HCP advisories (`internal/engine/correlate`)

The correlator serves advisory verdicts for hosted control planes:
W2 headroom (do-not-add-workers-first), W3 zombie
(review-unused-hosted-cluster), W5 api-tax (tune-noisy-webhook), W8
non-prod sleep (schedule_nonprod_sleep). One advisory per HC per
window (ADR-0349's predicate partition); the sleep rule's full
decision record is ADR-0348 with its amendments (#760 evidence legs,
#761 workload-grain coverage, #826 p95, #827 floors, #831 attestation,
#845 applicability, #847 offline parity, #848 reclaim complement).

- **Probe:** the zombie/sleep partition invariant; the reclaim
  estimate's weekly complement with distinct-day counting (#848's
  review P2 — duplicates are API-legal and must not go negative).
- **Evidence:** ADR-0348/0349; the HCP advisory integration tests; the
  serving endpoint's scope gates (management vs hosted).

## 7. Schema & migrations (195 commits — `migrations/`)

222 up-migrations: the partitioned digest tables, the
recommendation_sets / namespace_recommendation_sets relational models
(8-column natural keys incl. term+engine), the keys tables, the
settings tables, the sleep-evidence and advisory tables, the tag
tables. Every narrowing down-migration leads with a fail-loud guard;
the rollback policy is documented in `migrations/README.md`.

- **Probe:** `vendor-librobne-check` (vendor/ must match ./librobne);
  the populated-downgrade scenarios
  (`internal/engine/migration_down_populated_test.go`, 11 scenarios);
  the roundtrip tests (empty + business-hours shapes).
- **Evidence:** #846 (closed with the policy); the migration count is
  the `schema_migrations` version.

## 8. robne CLI (148 commits with librobne — `cmd/robne`, `librobne/`)

The offline tool: reads Nise/operator CSVs (or stored digests in DB
mode), computes the same recommendations through `librobne`, emits
advisories and JSON — the server-parity instrument. DB mode reads the
same evidence rows; CSV mode is inherently attested. The man pages are
generated (`make man`) with a drift check in CI.

- **Probe:** the parity skill (`.cursor/skills/server-robne-parity/`)
  and the parity tests; #847's workload-grain attestation (the
  coverage input carries it).
- **Evidence:** #847 (closed); the robne manpage drift CI gate.

## 9. Operational surface

docs-site (MkDocs, per-page "Last verified" stamps, `make docs-lint
docs-drift docs-sync-check`), the ADR corpus with its public index,
Prometheus metrics, the upgrade runbook, the SNO/HCP lab runbooks
(internal), CI (golangci-lint, docs gates, image smoke, publish,
vendor drift, manpage drift, openapi-changelog advisory), the
scale-benchmark skill and corpus.

- **Probe:** the docs-sync rule (`docs/agents/docs-site-sync.md`) —
  public facts must stay in sync across the trees; stub pairs are
  edited docs-site-side only.

## Cross-cutting: tenant isolation

Every serving query predicates org_id; the keys tables, detail
expansion, savings, heatmap, and advisory paths each carry the
predicate. The #840 P1 (detail expansion reading another tenant's
rows) was the family's one demonstrated escape — fixed with the
pre-commit review question answered ("how could these tests pass while
the bug lives?") and pinned. The v14 review's cluster-alias collision
analysis (UUID-foreign alias display, no tenant row leak) is recorded
in `docs/audit/adversarial-review-v14-2026-09-06.md`. Two open
questions qualify the family: #797 (cluster-alias resolution uses an
org-unscoped subquery — cross-tenant alias **existence inference**,
not row disclosure) and #789 (whether fleet/savings aggregates should
respect openshift.node grants — currently unfiltered on node).

## Cross-cutting: server/offline parity

`librobne` is shared by construction; the divergences found by review
(#847's window-wide attestation, its coverage-only-row P3) were closed
by bringing the offline path to the server's semantics, with the
parity tests keeping them in lockstep.

## Cross-cutting: the kruize→native transition

The container list always serves native; the namespace list falls
back to legacy JSONB only on zero native rows without tag filters;
detail endpoints have per-entity fallback conditions. The kruize plugin remains
as the flag-based rollback path. The residue adjudication
(`docs/audit/branch-residue-adjudication-2026-11-11.md`) closed the
"missing native implementation" question — runtime clean; the residue
is OpenAPI spec debt (#90/#605).
