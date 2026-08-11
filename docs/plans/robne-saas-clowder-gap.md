# Robne SaaS readiness — Clowder / app-interface gap

**Status:** Draft research (2026-08-10)  
**Audience:** ROS-OCP owners (Clowder + app-interface for cutover)  
**Related:** [robne-upstreaming-plan.md](robne-upstreaming-plan.md) (fork replacement + cross-repo PR plan), [koku-service-operator-triage-matrix.md](koku-service-operator-triage-matrix.md)

This document answers: what must change to run **robne-only** on console.redhat.com (Clowder), without Kruize, cutting over from today’s `ros-ocp-backend` + Kruize stack. On-prem Helm (`cost-onprem-chart`) is the proven config reference; SaaS today is **not** that chart.

---

## Executive summary

| Question | Answer |
|----------|--------|
| Have we made Clowder/app-interface changes sufficient for robne SaaS? | **No.** Fork `clowdapp.yaml` has probe/`/readyz` + namespace-flag hygiene only. **No** robne plugin/threshold/`GOMEMLIMIT`/cost-integration env. **No** app-interface MR yet. |
| Is robne a new Clowder service? | **No.** Same `ClowdApp` `ros-ocp-backend`, same DB `rosocp`, same Kafka ingress topic `hccm.ros.events`. |
| Cutover simplification | Remove resourceTemplate **`kruize`**, scale/remove **`recommendation-poller`**, drop Kruize env/topics/dashboards when safe. |
| Source of truth | **Two layers** (see below). Template in GitHub; stage/prod pins + replicas in GitLab app-interface. |

**Decision update (2026-08-11):** Dual-write is **superseded by cutover strategy** for this rollout. This document assumes hard cutover and validation-first gates.

---

## Source of truth (Q6)

Deployment is **not** a single file.

```text
┌─────────────────────────────────────────────────────────────┐
│ GitHub: RedHatInsights/ros-ocp-backend                      │
│   clowdapp.yaml          → ClowdApp template (deployments,  │
│                            env placeholders, kafkaTopics,   │
│                            database, probes)                │
│   kruize-clowdapp.yaml   → separate ClowdApp for Kruize     │
│   dashboards/            → Grafana (ros-ocp)                │
└────────────────────────────┬────────────────────────────────┘
                             │ path: /clowdapp.yaml  (etc.)
                             ▼
┌─────────────────────────────────────────────────────────────┐
│ GitLab: service/app-interface                               │
│   data/services/insights/ros/deploy-clowder.yml             │
│     resourceTemplates:                                      │
│       ros-frontend, ros-backend (RHEL),                     │
│       ros-ocp-backend, kruize, dashboards                   │
│     per-target: git ref, IMAGE, replica counts, memory…     │
└─────────────────────────────────────────────────────────────┘
                             │
                             ▼
                    App-SRE / Clowder applies
                    into namespaces ros-stage / ros-prod
```

| Layer | What it controls | How it updates |
|-------|------------------|----------------|
| **`clowdapp.yaml` in ros-ocp-backend** | Deployments (api, processor, recommendation-poller, housekeeper), jobs, default parameters, Kafka topic *names*, DB name/version, probe paths | Merge PR to `main` (stage tracks `main`; prod pins a commit in app-interface) |
| **`deploy-clowder.yml` in app-interface** | Which templates deploy where; **prod git `ref`**; replica/memory/log overrides; image org paths | App-interface MR → App-SRE |
| **Clowder / ACG_CONFIG** | Injected DB/Kafka/Unleash/ingress URLs when `CLOWDER_ENABLED=True` | Platform; not hand-edited in our repos |

HCCM (`data/services/insights/hccm/deploy-clowder.yml`) only **calls** ROS via `ROS_OCP_API: ros-ocp-backend-api.ros-*.svc…` — it does not deploy ROS.

Local research clone: `~/dev/app-interface` (remote `origin` = `gitlab.cee.redhat.com:service/app-interface.git`). Prefer `origin/master` for current `deploy-clowder.yml` (working tree may be an old personal branch). **No additional clone required** unless that directory is missing on another machine.

---

## What `insights/ros` actually deploys

The SaaS file you linked is an **umbrella app** named `ros` / saas-file `ros-clowder`. It is **not** Kruize-only and **not** ROS-OCP-only.

| resourceTemplate | Upstream | Role |
|------------------|----------|------|
| `ros-frontend` | `RedHatInsights/ros-frontend` | Shared Insights UI packaging (RHEL ROS + frontends) |
| `ros-backend` | `RedHatInsights/ros-backend` | **Legacy ROS for RHEL** — separate product/binary |
| `ros-ocp-backend` | `RedHatInsights/ros-ocp-backend` **`/clowdapp.yaml`** | **Our service** (today Kruize-coupled; tomorrow robne) |
| `kruize` | same repo **`/kruize-clowdapp.yaml`** | Autotune/Kruize Java service |
| `ros-ocp-dashboards` | `ros-ocp-backend` `/dashboards` | Grafana |
| `kruize-metrics-dashboards` | kruize-konflux `/dashboards` | Grafana |

**Cutover must not remove** `ros-backend` / `ros-frontend` unless RHEL ROS is separately decommissioned. Only **`kruize`** (+ eventually poller + Kruize-specific params) is in scope for robne replacement.

Namespaces (from app-interface): stage `ros-stage`, prod `ros-prod` (under Insights ROS).

### Current stage/prod shape (app-interface `origin/master`, 2026-08)

**ros-ocp-backend**

| Target | Git ref | Notable parameters |
|--------|---------|-------------------|
| stage | `main` | API/processor/poller/housekeeper replicas **3**; `UPDATE_KRUIZE_PERF_PROFILE: false` |
| prod | pinned commit | API **3**, processor **15**, poller **20**, housekeeper **3**; memory request 500Mi / limit 3Gi; daily partition cron |

**kruize**

| Target | Notable |
|--------|---------|
| stage | 3 replicas, 3–8Gi memory class |
| prod | 3 replicas, image tag pin (`KRUIZE_IMAGE_TAG`), 4–8Gi |

So SaaS today is still **heavily poller + Kruize scaled**. Robne-only cutover reclaims that capacity (especially **20 poller** pods in prod).

---

## Fork vs upstream Clowder template (already done)

On `pgarciaq-rosocp-superpowers-phase16` vs upstream `main`, `clowdapp.yaml` only:

- Liveness → `/status`, readiness → `/readyz`, **startupProbe**
- Recommendation-poller: drop `DISABLE_NAMESPACE_RECOMMENDATION` env (Unleash kill switch instead)
- Parameter description tweak for that flag

`kruize-clowdapp.yaml`: **unchanged**.  
**app-interface:** **no MRs** from this work.

That is **not** robne SaaS readiness.

---

## Gap matrix — Helm (proven) → Clowder (missing)

On-prem injects env via `cost-onprem/templates/ros/_feature-env.yaml`, `_db-pool-env.yaml`, `_go-memlimit-env.yaml`, `_healthz-env.yaml`, `_threshold-env.yaml`. SaaS `clowdapp.yaml` mostly has Kruize wiring + Clowder boilerplate.

### A. Must-have for robne-only cutover (minimum)

| Gap | Helm / robne today | SaaS ClowdApp today | Action |
|-----|--------------------|---------------------|--------|
| Disable Kruize plugin path | Native plugins on; `kruize` excluded by default | Processor/poller/housekeeper still set `KRUIZE_*`; poller runs | Set `ROS_DISABLED_PLUGINS=kruize` (or equivalent) **and** stop requiring Kruize service |
| Remove Kruize ClowdApp | Chart can omit Kruize | app-interface still deploys `kruize` | Delete/disable `kruize` resourceTemplate (stage first) |
| Recommendation poller | Only needed for Kruize HTTP poll | Prod **20** replicas | Scale to **0** or remove deployment from `clowdapp.yaml` after cutover |
| Kafka `rosocp.kruize.recommendations` | Poller topic | Declared in `kafkaTopics` | Stop consuming; keep topic until lag drained, then drop from template |
| Migrations | Processor/API run `db migrate up` | Same pattern | Keep; robne tables migrate in-process — **no new Clowder DB** |
| Unleash | `featureFlags: true` already on ClowdApp | Used for namespace kill switch etc. | Confirm SaaS Unleash project/flags for any cutover kill-switch (`rosocp.namespace_disabled`, etc.) |
| Probes | `/healthz` + `/readyz` | Fork has `/readyz` (merge upstream first) | Land probe PR before relying on them in prod |

### B. Should-have for production parity with on-prem robne

| Gap | Why | Suggested Clowder param / env |
|-----|-----|-------------------------------|
| `ROS_DISABLED_PLUGINS` / `ROS_ENABLED_PLUGINS` | Explicit engine surface | Template env + optional app-interface override |
| `GOMEMLIMIT` | Avoid OOM under Go GC (chart: ~90% of limit) | Per-deployment env from memory limit |
| `ROS_DB_MAX_CONNS` / pool sizing | Prod processor×15 + api×3 + housekeeper×3 without poller still needs budget | Align with ADR connection-budget work; fix `DB_POOL_SIZE` **parameter** (referenced in api env but **not** declared in `parameters:` today — latent template bug) |
| Idle / quota / cluster-quota thresholds | Chart `_feature-env.yaml` | Optional; compiled defaults OK if Settings API used |
| `ROS_CSV_ALLOWED_HOSTS` | SSRF allowlist for CSV URLs | SaaS ingress/S3 hosts — must set for security enforcement |
| Housekeeper `KRUIZE_*` env | Dead after cutover | Remove from housekeeper podSpec |
| Kafka `hccm.ros.events` partitions | Chart often **12**; ClowdApp template says **1** | Raise partitions before scaling processors (ops + Clowder topic config) |
| Resource requests/limits | Go engine ≠ Kruize poller profile | Re-size after load test; reclaim poller/Kruize quota |

### C. Deferred (you said “not yet on SaaS but it will”) — cost integration

Do **not** block cutover of recommendation engine, but track as follow-on Clowder/HCCM work:

| Item | Helm reference | SaaS need |
|------|----------------|-----------|
| Masu / Koku HTTP | `KOKU_MASU_URL`, NetworkPolicy ros-api→masu | Cross-namespace URL + allow |
| Tag sync | `ROS_TAGS_SOURCE=api`, internal SA allowlist | HCCM worker → ROS internal tags API |
| Savings recalculation | internal recalculate endpoints | Same |
| Direct DB tags (`tagsSource: db`) | `ros_user` grants on Koku schemas | Usually **avoid** on SaaS; prefer HTTP push ([ADR-0120](../adr/0120-saas-http-push-tag-sync.md)) |
| Business hours + reship | `ROS_BUSINESS_HOURS_*` | Env when feature enabled in SaaS |

### D. Explicitly out of scope for robne cutover

- Removing **`ros-backend`** / RHEL ROS  
- Replacing **ros-frontend** packaging (UI for OCP Optimizations may already live under cost-management frontends — confirm with UI; do not assume `ros-frontend` = koku-ui-ros)  
- HCP fleet wedges (docs-only today; no Clowder impact until coded)

---

## Recommended cutover sequence (Clowder-focused)

Assuming **hard cutover** (dual-write superseded):

1. **Fork replacement path:** deploy from the fork as the new ROS-OCP source of truth for this rollout; do not upstream by PRing `RedHatInsights/ros-ocp-backend` first.  
2. **PR to `clowdapp.yaml`:** robne env (`ROS_DISABLED_PLUGINS=kruize`), probes, `GOMEMLIMIT`, CSV allowlist, drop Kruize env from non-poller deployments, declare missing parameters (`DB_POOL_SIZE`, …).  
3. **app-interface MR (stage):** point `ros-ocp-backend` at the commit/image; set new params; set `POLLER_REPLICA_COUNT: 0` (or remove poller from template); **disable** `kruize` target (`disable: true` or delete template).  
4. **Validate stage:** ingest → digests → native recommendations; IQE ros-ocp; no calls to Kruize.  
5. **app-interface MR (prod):** pin ref; same param shape; scale processors for robne CPU (no 20 pollers).  
6. **Cleanup:** remove `kruize` resourceTemplate; remove `kruize-metrics-dashboards` if unused; drop `rosocp.kruize.recommendations` from `kafkaTopics` after idle; delete `kruize-clowdapp.yaml` from repo when nothing references it.  
7. **Later:** cost-integration env + HCCM wiring (section C).

## Checklist — files to touch

### In `ros-ocp-backend` (GitHub)

- [ ] `clowdapp.yaml` — robne env, remove/optionalize poller, fix parameter declarations, Kafka partitions as needed  
- [ ] `kruize-clowdapp.yaml` — stop deploying via app-interface; delete after soak  
- [ ] Docs: keep this gap doc + update upstreaming plan Clowder note  
- [ ] Unleash flag inventory for SaaS cutover (document names/defaults)

### In `app-interface` (GitLab)

- [ ] `data/services/insights/ros/deploy-clowder.yml` — stage then prod for `ros-ocp-backend` + disable `kruize`  
- [ ] Confirm image paths (`quay.io/redhat-services-prod/insights-management-tenant/insights-ocp-resource-optimization/ros-ocp-backend`)  
- [ ] Optional: remove `kruize-metrics-dashboards` target  
- [ ] Do **not** alter `ros-backend` / RHEL unless intentional

### In `hccm` app-interface (later / cost)

- [ ] NetworkPolicy / URL for Masu↔ROS when cost features enable  
- [ ] Keep `ROS_OCP_API` service DNS correct after any rename (none expected)

### Not required

- New ClowdApp / new database / new app-interface `app.yml` entry for “robne”

---

## Open decisions

1. **Stage image source** — fixed branch/tag policy for fork replacement (what is promoted to stage/prod and by whom).  
3. **Poller removal** — Delete deployment from template in the same PR as cutover, or scale to 0 for one release?  
4. **CSV allowlist hosts** — Exact SaaS hostname list for `ROS_CSV_ALLOWED_HOSTS` (ingress/S3).  
5. **Kafka partition count** for `hccm.ros.events` in stage/prod today vs template `partitions: 1`.  
6. **UI path** — Confirm Optimizations UI for OCP is cost-management frontend, not `ros-frontend` resourceTemplate (avoid wrong teardown).  
7. **Security enforcement** — SaaS `ROS_SECURITY_ENFORCE` / graduated defaults: match on-prem prod posture?

---

## Research notes / clone

| Repo | Path | Notes |
|------|------|-------|
| app-interface | `~/dev/app-interface` | Use `git fetch origin && git show origin/master:data/services/insights/ros/deploy-clowder.yml`. Working branch may be stale. |
| ros-ocp-backend | this repo | `clowdapp.yaml`, `kruize-clowdapp.yaml` |
| cost-onprem-chart | sibling | Env inventory under `cost-onprem/templates/ros/` |

No extra GitLab clone is required for further research if `~/dev/app-interface` stays updated. If VPN/`git fetch` fails, ask App-SRE or use the GitLab UI raw view for `deploy-clowder.yml`.

---

## Changelog

| Date | Note |
|------|------|
| 2026-08-10 | Initial gap doc from app-interface `origin/master` + Helm env inventory + fork ClowdApp diff |
