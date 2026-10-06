# ADR-0339: W3 idle leg — workload+CPU primary, API informational

## Status

Accepted (design) — implements the #391 redefinition amendment (owner
decision 2026-10-06), superseding the API-primary leg greenlit 2026-10-05.

## Phase

HCP / fleet FinOps (W3 child-1, #664; follows #658 which proved the mechanism).

## Context (plain English)

The greenlit W3 rule defined "idle" as hosted API requests/day below T
(T=100). Live measurement (#662) killed that definition: quiet demo
clusters serve 0.9–3.6M requests/day (controller leases, list/watch
loops, monitoring) across two labs and two months — ~10,000× over T.
Verb-scoping cannot rescue it (mutating-only still reads ~364k/day on
a quiet box). No absolute request threshold separates zombie from
alive, and node-count scaling inverts any absolute ranking (a 10-worker
zombie out-chatters a 2-worker alive cluster).

The same evidence that kills API-count crowns the replacement: a Sept
specimen (1 workload at exactly 0.0000 CPU over 84 hourly rows, two
grains) served 888k API requests the same day. Workload CPU has a
natural zero; API counts do not.

## Decision

Idle = zero active non-system workloads every day for 14 straight days,
where a workload is active iff its daily CPU sums at or above the
ceremonial floor (default 10m, tenant-tunable, discouraged). Missing
digest days read as unknown (silence), preserving the new-cluster grace
by construction. API request evidence rides advisory signals as labeled
no-gate context (feeds the #663 falsification experiment); it never gates
firing. The `z_idle_req_per_day` setting is deprecated (accepted, stored,
unread — no breakage, removal later if ever).

Platform exclusion is compiled and narrow-side (`kube-`/`openshift-`
prefixes + evidence-grounded addon list): an incomplete list fails
silent, an over-broad list would fire wrongly. Tenant-curated
replacement via koku cost-groups sync is a deferred slice (#665).

## Alternatives Considered

### Retune T sky-high
Rejected — any number is arbitrary without idle distributions, and
mutating-only background (364k/day) sits orders above any defensible
line. A threshold needs separating distributions; we have one class.

### Verb-scoped API idle
Rejected — measured insufficient by 3+ orders (see Context).

### Workload-count without CPU
Rejected — the Sept specimen carries 1 workload; count alone reads it
alive. CPU is the discriminating half.

### Keep API as a veto (must ALSO be quiet)
Rejected — veto power recreates the never-fires rule through the back
door. Informational-only is the honest placement until #663 measures a
separating profile.

## Consequences

- #658 stays closed as mechanism history; #664 implements this ADR.
- #663 (falsification tracker) measures zombie API profiles against
  this decision — a separating profile reopens it publicly.
- #662 closes on the investigation that produced this ADR.
- Floor and predicate constants pin behavior; tests fail for the
  mechanism, not incidental values.

## Related Decisions

- [ADR-0333](0333-unused-hostedcluster-lifecycle-w3.md) — W3 design (this ADR amends its idle definition, nothing else)
- [ADR-0330](0330-hcp-audience-visibility-rh-vs-customer.md) — confidence/copy branching
- [ADR-0332](0332-thin-cross-plane-causality-w2.md) — join key, evalC grain precedent

## References

- #662 investigation (4 measurements, verb splits, Sept-specimen A/B)
- 09-20 evidence pack (`hcp-capture-20260920/`, hosted payloads + prom)
- Sept specimen: `demo-steady/web`, 168 container rows + 84 namespace rows, usage exactly 0.0000
