# Extract native engine into nested librobne module

**Canonical tracker:** [GitHub #94](https://github.com/pgarciaq/ros-ocp-backend/issues/94)  
**Canonical plan:** [librobne-extraction-blueprint.md](../plans/librobne-extraction-blueprint.md)

This file is a **pointer**. Do not treat the old Cut 1 checklists (export
privates, copy files, dual types + converters, `RecommendCPUAndMemory` as the
product API) as work items. They are **void**.

librobne is a **statically linked Go engine**: runner + canonical digest types +
savings on a deposited RateCard. Nested module at **P4** after in-tree cleanup
(P1–P3). Optional `csv` / `pgdigest` are **P5**, not this issue’s DoD.

**Stop line:** no extract code until the blueprint is approved, then P0.5
baseline before P1.

## Definition of done

Same as GitHub #94 / blueprint §13: **container-first** nested module, zero
convert loops, no user-facing API change, §8 gates vs `841639f3` at 10k.
Other entities are P4+. Operator (#138) and CLI (#99) are P6.

## Related

- [ADR-0303](../adr/0303-library-extraction-librobne.md) — Accepted = this blueprint (amended P0.5)
- [Rejected Cut-1 blueprint](../archive/librobne-extraction-blueprint-cut1-2026-08.md)
- [#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138) Local Mode
- [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99) robne CLI
