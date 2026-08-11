# koku-service-operator triage matrix (vs cost-onprem-chart)

**Date:** 2026-08-11  
**Decision context:** `koku-service-operator` is now the on-prem deployment target of record. `cost-onprem-chart` is reference-only and being replaced.  
**Source inputs:** `koku-service-operator/docs/helm-vs-operator-comparison.md`, `koku-service-operator/docs/gap_analysis/BETA-PRIORITY.md`, current `cost-onprem-chart` ROS/Koku templates.

This matrix identifies what still needs to move from chart behavior into operator behavior.

## Summary

| Area | Status |
|------|--------|
| Core stack parity (Koku/Masu/Listener/RBAC/UI/Ingress/Gateway) | Mostly implemented |
| Critical correctness/ops deltas | Still open (timeouts, ports, readiness, defaults) |
| ROS/Kruize behavior | Present but being strategically reduced for cutover |
| Recommended execution | Grouped issues by capability, not by single env vars |

## Capability matrix

| Capability | Chart behavior | Operator behavior today | Status | Required action |
|------------|----------------|-------------------------|--------|-----------------|
| Ingress upload timeout | Envoy + route timeout tuned for large uploads (`180s/60s`) | Envoy ingress route still short timeout in audit | Gap | Add CR-configurable ingress timeout defaults matching chart safety envelope |
| Masu service ports | Service exposes app + metrics expectations consistently | Audit notes port/name mismatch and scrape risk | Gap | Normalize service ports/names and ServiceMonitor target names |
| Celery beat resources | Explicit requests/limits | Audit notes missing bounded resources | Gap | Add defaults in operator resource builder + CR override support |
| RBAC Keycloak sync CronJob | Chart has sync CronJob template | CRD fields exist; job wiring incomplete in audit | Gap | Implement reconciled CronJob from `KeycloakSyncSpec` |
| ROS processor/poller scrape endpoints | Chart provides services/NPs for metrics | Audit found missing services/NP coverage | Gap | Add optional services and metrics policies when ROS enabled |
| NetworkPolicy parity | Chart has broad component-specific policies | Operator has partial set, not full parity | Gap | Add policy coverage for required paths only, stage by risk |
| Gateway config rollout safety | Chart rollouts pick up config changes via Helm apply | Audit flags ConfigMap change rollout blind spot | Gap | Add checksum annotation or equivalent rollout trigger for Envoy config |
| App readiness truthfulness | Chart less strict but operationally known | Audit flags condition collisions / status optimism in some flows | Gap | Tighten condition transitions and readiness gating for core dependencies |
| Critical env defaults | Chart ships sane defaults for key env vars | Operator relies heavily on user `.spec.*.env` overrides | Gap | Promote critical defaults into operator-generated env (document override contract) |
| DB/cache/Kafka dependency validation | Chart mostly install-time assumptions | Operator has discovery/validation but some holes in key checks | Partial | Close secret-key and probe-quality gaps for core path |
| Profile-based sizing (`standard`/`ha`) | Chart has practical sizing knobs | CR profile exists; wiring partial | Gap | Implement shared profile map and apply to core deployments |
| OLM/bundle/install path | Chart install scripts available | Operator bundle/CI/install docs still maturing | Gap | Complete OLM/bundle pipeline and operator install docs |
| ROS/Kruize optionality | Chart deploys both by default | Operator added `spec.ros.enabled`; still follow-up edges | Partial | Keep ROS/Kruize non-blocking for Cost core, finish conditional validation and cleanup |

## Grouped issue plan (recommended)

Create 5–10 grouped issues in `project-koku/koku-service-operator`:

1. **Gateway/Ingress reliability parity**  
   Upload timeouts, route timeout defaults, Envoy config rollout trigger.

2. **Service + monitoring parity for core components**  
   Masu service ports/labels, ServiceMonitor target correctness, core scrape integrity.

3. **Readiness/condition correctness hardening**  
   Avoid status false-positives/collisions; tighten core dependency gates.

4. **Critical env defaults and configuration contract**  
   Move must-have chart defaults into operator generated env; keep explicit override model.

5. **RBAC Keycloak sync implementation**  
   Reconcile CronJob from CR spec and validate schedule/auth behavior.

6. **Resource and sizing baseline**  
   Celery beat resource defaults + profile map wiring (`standard` first, `ha` staged).

7. **NetworkPolicy minimum safe parity**  
   Add required ingress/egress and metrics paths for core, then optional ROS/Kruize expansions.

8. **ROS/Kruize optionality completion**  
   Finish conditional validation/secret requirements when `ros.enabled=false`.

9. **OLM/bundle + install docs completion**  
   Bundle generation, validation CI, and user install/config guide parity.

## Risks if skipped

- Upload/API reliability regressions under real ingestion load
- Monitoring blind spots and false operational confidence
- Core readiness showing healthy while key dependencies are degraded
- Security/safety drift from missing defaults or open traffic policies
- Migration friction from undocumented or inconsistent operator behavior

## Notes

- This matrix intentionally groups by capability area for issue quality.
- Dual-write migration strategy is being treated as **superseded by cutover strategy** in ros-ocp-backend planning.
