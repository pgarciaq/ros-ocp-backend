# ADR-0335: Operator / webhook API tax recommendations — W5

## Status

Accepted (design) — **implementation coding deferred** until explicit W5 greenlight

## Phase

HCP / fleet FinOps (research R6 complete — metric contracts + thin digest; not lab thresholds)

## Context (plain English)

Sometimes the API feels slow because **something is hammering it**: a chatty operator, a service account doing endless list/watch, or a slow admission webhook — not because workers are small and not only because the control plane is undersized.

**R6 question:** Are the right signals available enough to build recommendations, and can we export them into ROS without exploding cardinality or leaking sensitive user identity?

**Locked before research:**

| Topic | Decision |
|-------|----------|
| Where | **Both** hosted and management clusters |
| Export shape | **Top-N** over a window — not full per-user history |
| vs W2 | If W5 finds a top talker / slow webhook, prefer “tune that” before capacity; coordinate with W2 CP-blame |
| Method | Docs / metric contracts first; lab only optional existence check |
| Scope | Useful on **any** OpenShift; HCP makes shared-CP pain worse |
| GO bar | **B** — define a **thin, low-cardinality digest** we would put in the metrics operator |

## Decision

### Go / no-go for W5

| Verdict | Meaning |
|---------|---------|
| **GO (with caveats)** | Build W5 on a **thin top-N digest** (+ webhook latency rollups), for **hosted and management** |
| **NO-GO** | Shipping full `apiserver_request_total{username=…}` (or similar) time series into ROS CSVs |
| **NO-GO** | Claiming standard kube `apiserver_request_total` already answers “which user” — **it does not** (no username label) |

### Metric availability matrix (docs)

| Signal | Source | Labels / shape | Available enough? | Notes |
|--------|--------|----------------|-------------------|--------|
| Request volume by resource/verb | Prom `apiserver_request_total` | verb, resource, group, version, code, … — **no user** | Yes on clusters with kube-apiserver metrics scraped | Good for “pods LIST is hot,” not “who” |
| Request latency | Prom `apiserver_request_duration_seconds` | same family; exclude WATCH for SLO (see ADR-0332) | Yes | Shared with W2 |
| Webhook latency | Prom `apiserver_admission_webhook_admission_duration_seconds` | name, type, operation, rejected | Yes (stable) | Bounded by webhook registration names |
| Webhook volume / codes | Prom `apiserver_admission_webhook_request_total` | name, type, operation, code, rejected | Yes (alpha stability) | Codes truncated for cardinality |
| **Top users / SAs** | OpenShift **`APIRequestCount`** CR (`apiserver.openshift.io/v1`) | Per resource: top N usernames + counts (best-effort) | **Yes on OpenShift** — preferred for “who” | Not a Prom label explosion; imprecise after apiserver restart; system users may appear |
| Per-user in Prom | — | — | **No (standard)** | Do not depend on username on `apiserver_request_total` |

**Monitoring stack:** Assumes in-cluster Prometheus can scrape kube-apiserver (normal OpenShift Monitoring). If metrics are disabled, W5 degrades to APIRequestCount-only (users) or no emission.

### Privacy / cardinality

| Risk | Mitigation |
|------|------------|
| Cardinality bomb from per-user Prom series | **Never** export raw per-user Prom; use top-N only |
| Human usernames in digests | Prefer recommending on **service accounts** (`system:serviceaccount:…`); optionally redact or bucket human users as `user:other` |
| Multi-tenant SaaS seeing other orgs’ SA names | Digests stay on the **source cluster’s** tenant path only (same as today) |
| Management plane | RH/self-managed only for deep SA naming; customer hosted path OK for their own cluster |

### Thin digest design (GO bar B) — operator export sketch

Operator (or a small collector job) periodically produces a **low-row** artifact, e.g. daily or per report window:

**A — Top API clients** (from `APIRequestCount` status top users, aggregated across resources or per heavy resource):

| Field | Example |
|-------|---------|
| `username` / `service_account` | `system:serviceaccount:my-op:controller` |
| `request_count` | 1.2e6 |
| `share_of_reported` | 0.37 |
| `top_verbs` / `top_resources` (optional short list) | LIST pods, WATCH secrets |
| `window` | 24h |

Keep **K ≤ 10–20** rows per cluster per window.

**B — Top slow / heavy webhooks** (from Prom histograms/counters, already low cardinality):

| Field | Example |
|-------|---------|
| `webhook_name` | `my.mutating.example` |
| `p99_seconds` or bucket share above threshold | 0.8 |
| `rejected_rate` | 0.02 |
| `window` | 1h / 24h |

**Not in digest:** full series, every username, every verb×resource×user.

### Recommendation algorithm (precision-first)

```text
on hosted and/or management source (both in scope):

if top SA share >= threshold OR webhook p99 high:
  emit advisory: tune QPS/informers / fix webhook; name the SA or webhook
  suppress or de-prioritize “add worker nodes” as first advice
  if W2 would also fire CP-blame: show both with clear ordering —
    (1) noisy client/webhook (2) CP capacity — unless CP evidence is overwhelming alone

if only resource/verb heat without attributable SA:
  weaker advisory: “API heat on LIST pods — investigate controllers” (no name)

if digests missing:
  no W5 emission
```

Advisory-first; never auto-disable operators.

### Cross-cutting: other recommendation types that should respect API tax

API tax is not only a W5 card. When W5 digests exist, these paths should **listen**:

| Recommendation area | Why |
|---------------------|-----|
| **W2 thin causality** | Prefer “noisy client/webhook” before / alongside CP blame |
| **Node / worker scale-up advice** | Don’t lead with “add nodes” if API tax explains pain |
| **W1 management CP rightsizing** | Don’t aggressively **downsize** CP while a known chatty client dominates load; annotate “load may be client-driven” |
| **W4 fleet headroom** | High API tax ⇒ effective capacity lower than request packing; prefer pressure warning |
| **Generic “cluster feels slow” narratives** | Same suppress/annotate pattern as hosted topology (W0) |

Out of scope for v1 hooks: JVM/runtime plugins, pure workload CPU rightsizing with no API symptoms.

### Audience

| Plane | Who sees deep SA/webhook names |
|-------|--------------------------------|
| Hosted (customer operator) | Customer — their cluster |
| Management (RH or self-managed) | RH-internal / self-managed fleet admin (ADR-0330) |

## Alternatives Considered

### Prom-only without APIRequestCount

Rejected for “who” — no username on standard `apiserver_request_total`.

### Full audit log shipping

Rejected — too heavy, privacy-sensitive, not ROS’s job.

### HCP-only scope

Rejected — user confirmed useful generally; epic still owns it because HCP amplifies shared-CP impact.

## Consequences

- Metrics-operator work: new thin CSV/digest for top clients + webhooks (coding later).
- Backend: W5 plugin + hooks into node/W2/W1/W4 paths when digests present.
- W5 backlog #393 may be promoted after coding greenlight; still no code now.
- OpenShift-specific `APIRequestCount` means degraded “who” on pure upstream k8s — document that.

## Related Decisions

- [ADR-0332](0332-thin-cross-plane-causality-w2.md) — W2 coordination
- [ADR-0330](0330-hcp-audience-visibility-rh-vs-customer.md) — management visibility
- [ADR-0334](0334-fleet-admission-headroom-w4.md) — headroom vs API load
- Research #390; wedge #393; epic #384

## References

- Kubernetes metrics: `apiserver_request_total`, `apiserver_admission_webhook_admission_duration_seconds`
- OKD: [APIRequestCount](https://docs.okd.io/latest/rest_api/metadata_apis/apirequestcount-apiserver-openshift-io-v1.html)
- OpenShift enhancement: API webhook supportability (webhook dashboards / metrics)
