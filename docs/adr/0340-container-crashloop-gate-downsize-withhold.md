# ADR-0340: Crash-loop gate — withhold cost downsize on churn over muting all recs

## Status

Accepted (design) — implements [Issue #78](https://github.com/pgarciaq/ros-ocp-backend/issues/78)
(Tier-A scope, owner greenlight 2026-10-10), motivated by the reliability blind-spot
write-up in [#677](https://github.com/pgarciaq/ros-ocp-backend/issues/677).

## Phase

Engine / Algorithm (container plugin; cross-repo: operator query + CSV column).

## Context (plain English)

A container in CrashLoopBackOff runs for seconds before dying, so its usage
baseline is untrustworthy — yet the cost engine happily turns that baseline into
a "downsize and save $X" recommendation. Shrinking an already-crashing container
because its average usage looks tiny is actively harmful, not just noisy. The
OOM path already solved the sibling problem (memory bump on `oom_count`); generic
restarts were discarded at collection (the operator joined restarts to the
OOMKilled reason only). The VM plugin likewise already notifies on crash loops
(`NotifVMCrashLoop`, threshold 3). The container lane had neither detection nor
protection.

## Decision

Count **total** restarts per term window (`restart_count_container_sum`, same
namespace scope as OOM — manual label plus HCP auto-match) and, at **≥ 3
restarts**, withhold the **cost downsize only** for that term: keep current
requests, zero the savings, attach notification code **86**
(`UNSTABLE_CONTAINER`, WARNING). Cost upsizes, all performance recs, and
non-downsize outcomes flow unchanged. Resume is implicit — restarts age out of
the window and the gate lifts on its own, per term. No explanation-column
persistence (the code carries the signal), no new env knob (zero-value threshold
input falls back to 3, mirroring the VM default).

Code 86 because 48 — the plan's first pick — is `NotifVMCrashLoop` in the
domain-local VM registry (`librobne/vm/vm_notifications.go`), which the recon
missed; the implementer caught it and 86 was verified free everywhere
(constants, seeds, mappings, definitions).

## Alternatives Considered

### Mute all sizing recs while churning (#78 body text)
Rejected — throws away valid headroom signal with the garbage baseline. The
failure mode being prevented is specifically harmful shrinkage; an upsize rec
on a churning workload is at worst unhelpful, never destructive. If perf recs
built on crash baselines prove misleading in practice, revisit with evidence.

### Threshold 5 per 24h (#78 body text)
Rejected — no measurement behind either number; 3 mirrors the shipped VM
`CrashLoopRestartThreshold` default, keeping one mental model across plugins.
Revisit when real churn distributions exist.

### Explicit stabilization timer before resume (#78 body text)
Rejected (YAGNI) — per-term windows already forget: short-term recs resume
within a day of calm, long-term within two weeks. A dedicated calm-down clock
adds state for UX smoothness we have no complaint about. Known side effect:
twice-a-week crashers flicker in and out of short-term suppression.

### New `withheld` rec category instead of `optimized`
Rejected — a new contract value ripples to UI, API, and history consumers for
cosmetic precision. Zeroed variations mechanically classify `optimized`
(`ClassifyResource`); the attached code discloses the real story, exactly as
`NotifVMDownsizeHeld` already does for VMs.

### Env-configurable container threshold
Rejected for now — the VM lane has `ROS_VM_CRASH_LOOP_RESTART_THRESHOLD`, but
adding a second knob before any operator tunes the first is speculative
surface. The struct field exists (`CrashLoopRestartThreshold`, always 3 in
prod); wiring it to settings is a one-line follow-up when demanded.

## Consequences

- Mixed CPU-up/memory-down recs are held in full (conservative; documented in
  the Tier-A plan, not revisited here).
- Held recs read `optimized` with the code attached — consumers must read the
  code, not just the category.
- Downstream owners pull their threads: IQE catalog constant, cheatsheet row,
  docs-site counts (done alongside), chart E2E gated on a deployed backend.
- Tier B (control-plane reliability: restarts of mgmt-plane operators, lease
  loss, API latency) is untouched — #677 stays open for that decision.
