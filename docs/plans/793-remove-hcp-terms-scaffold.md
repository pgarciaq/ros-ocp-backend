# #793 — Remove the HCP Terms Scaffold Artifact — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step.
> Owner decisions 2026-11-14: the parity break's polarity was REVERSED by
> recon — robne is right, the server's stray TermProvider is wrong. Remove
> the HCP terms settings surface (option a). HCP recommendations were never
> publicly released — no compatibility concern.

**Repo:** ros-ocp-backend, branch `pgarciaq-rosocp-superpowers-phase17`,
post `66453f2f`. Tracker: #857 (serving P3 blitz, item 1).

## The verified mechanism (the readiness recon)

- **#630's design line** (the HCP scaffold, 2026-09-29): "TermProvider
  supports_terms:false v1 **reusing container windows**" — hcp is not
  term-configurable by design.
- **ADR-0346** (2026-10-27): "quota and cluster-quota (threshold-based),
  snapshot (age-threshold), and the HCP fleet-capacity advisory
  (snapshot-series model) have no term windows in either implementation."
- **But the scaffold implemented a full TermProvider** —
  `DefaultTerms()` echoing the container table (1/7/15), `MaxWindowDays()`
  90 — which made the terms handler's trait walk accept
  `recommendation_type=hcp` on GET/PUT/DELETE `/settings/terms`.
- **No computation consumer exists** (verified): no
  `LoadTermConfigCached(…, "hcp")` caller; the capacity ingestor has zero
  term references; the HCP detail rides the native container-family
  machinery (the container terms); the advisories use correlator policy
  windows. The knob stores values nothing reads.
- robne's exclusion is correct per ADR-0346 — no robne change.

## Mechanism

Remove `DefaultTerms()`/`MaxWindowDays()` from the HCP plugin (dropping
it from the `TermProvider` trait walk), remove the trait assertion from
the plugin test, update the plugin's doc comment (term-less per
ADR-0346, container windows reused via the native detail path), and
update the OpenAPI terms-endpoint surface if it enumerates hcp.

**Surface change:** `GET/PUT/DELETE /settings/terms?recommendation_type=hcp`
→ 400 "unsupported recommendation type" (today: 200, storing inert rows)
— the same answer quota, cluster-quota, and snapshot already give.
HCP recommendation endpoints unchanged.

## Tests

- **RED:** the terms endpoint with `recommendation_type=hcp` must 400
  (baseline: it succeeds — the trait walk accepts it).
- **Controls:** container 200; quota/snapshot 400 (the existing
  term-less behavior); the HCP detail still honors `filter[term]` (the
  container path — an existing test or a targeted assertion).
- **Mutation:** restore the trait methods → the RED fails.

## Non-goals

Robne (already correct); #812/#851/#792 (the rest of the blitz); the
inert stored hcp rows in any pre-existing tenant DB (the fork is
unreleased; test DBs are fresh).
