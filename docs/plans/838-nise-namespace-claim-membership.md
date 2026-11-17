# #838 Nise claim-membership namespace qualification — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step. Implementation in `~/dev/koku/nise` (fork `pgarciaq`, branch `pgarciaq-rosocp-superpowers-phase17`, baseline `4f33858`); centrally tracked in ros-ocp-backend #838. nise-only; no ROS change.

**Goal:** a static VM that references a PVC name synthesized in its own namespace gets its volume and storage row, even when another namespace's pod maps to a claim with the same name.

**Mechanism (verified by the #830 review, reproduced there; re-read at `4f33858`):** `_gen_virtual_machines` gates volume synthesis on

```python
vcm = vm_copy.get("vm_persistentvolumeclaim_name")
if vcm and vcm not in self.pod_pvc_map.values():
```

`pod_pvc_map` is keyed `pod_key(namespace, pod)` → claim **name** (writes at 1797, 1843). `.values()` is therefore a flat, cross-namespace set of names, and the gate answers "is this claim name pod-backed *anywhere*" instead of "in this VM's namespace". Namespace A's pod claim named `data` suppresses namespace B's VM volume, and B's storage row disappears.

**Fix (one predicate, no reshape):** a namespace-qualified membership helper — `any(key.split("/", 1)[0] == namespace and claim == vcm for key, claim in self.pod_pvc_map.items())`. `pod_key` is `f"{namespace}/{pod}"` and Kubernetes DNS labels cannot contain `/`, so the split is unambiguous (pinned in the `pod_key` docstring from #775). Helper-vs-inline is a hypothesis; if the helper is used at more than one site it belongs on the class.

**Consumer audit (done):** `pod_pvc_map.values()` is read at exactly one site — this gate (2017). The other four references are the init (1206), the two writes (1797, 1843), and #830's key lookup (1864). The fix cannot reach #830's resolution path or the VM disk consumer except through the synthesized volume itself.

**Test matrix (append to `tests/test_ocp_generator.py`, following the existing generator-fixture style):**
- RED: ns-a pod claims `data`; ns-b static VM also references `data`. Assert the ns-b VM's volume/storage row exists. Baseline: absent — the failure message must name the suppressing namespace and the missing VM storage row, not a row count.
- Control (order reversal): declare ns-b before ns-a → same assertion both ways.
- Control (distinct capacities): ns-a claim 1 GiB, ns-b 4 GiB → the synthesized ns-b volume carries 4 GiB, never ns-a's capacity.
- Green-must-stay: #830's cross-namespace claim-selection regressions, #775's namespace-qualified pod identity, #821's VM identity fixture (currently red for its own reason — do not fold it in).

**Verification commands:** `uv run --frozen --group dev python -m unittest tests.test_ocp_generator -v` (143 tests, ~9s at baseline). Mutation check: revert only the predicate (stash the fix) and confirm the new tests fail naming the mechanism.

**Constraints:** nise-only; no generator rewrite; not #837 (the map *shape*) and not #821 (VM identity). If #837's reshape lands later it may subsume this predicate — record the overlap in a comment rather than pre-empting it. Commits wait for owner go-ahead.
