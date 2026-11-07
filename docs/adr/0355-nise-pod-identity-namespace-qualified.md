# ADR-0355: Nise Pod Identity Is Namespace-Qualified

## Status

Accepted — implemented in #775 (nise `nise/generators/ocp/ocp_generator.py`)

## Phase

Fixture data — nise OCP generator's per-pod state and ROS/GPU/PVC emission

## Context

The OCP generator (`nise/generators/ocp/ocp_generator.py`) resolves a pod once,
at intake, and then re-reads that pod from several dicts while emitting the CSV
families: the billable pod state (`pods`), the ROS container rows (`ros_data`),
the GPU devices (`gpus`), the pod→PVC map (`pod_pvc_map`), and the VM→pod map
(`vm_pod_map`). Every one of those was keyed by the **bare** pod name.

Pod names are namespace-scoped. `namespaceA/web` and `namespaceB/web` are two
different pods that may legitimately appear in one cluster, one day, and one
static YAML — and ROS recommendations are keyed by
`(cluster, namespace, container)`, so a fixture that cannot hold two `web` pods
cannot exercise the ROS namespace-scoped container identity at all. With
name-only keys the second `web` overwrote the first, silently:

- `pods` / `ros_data` kept the last `web`; the first pod's billable and ROS
  rows disappeared (a one-day two-namespace fixture emitted 96 quarter-hour
  rows instead of 192).
- `get_specific_pvc_from_pod(pod_name)` and `get_vm_disk(pod_name=...)` looked
  up by name, so a VM or pod could inherit the **other** namespace's PVC.
- The GPU and MIG seeds were `nise.ocp.gpu.{node}.{pod}.{idx}` and
  `nise.ocp.mig.{node}.{pod}.{gpu_idx}.{mig_idx}`, so the surviving `web` and
  a same-named pod in another namespace shared `gpu_uuid` /
  `mig_instance_id` values that are supposed to be device-unique.

The failure mode was the worst kind for a fixture generator: a valid, plausible
YAML produced fewer rows than it declared, with no warning.

## Decision

1. **One composite identity: `"namespace/pod_name"`.** A `pod_key(namespace,
   pod_name)` helper produces it, and `pods`, `ros_data`, `gpus`,
   `pod_pvc_map` and the `vm_pod_map` values all use it. A string key was
   chosen over a `(namespace, pod_name)` tuple after measuring the hot path
   (29.6 ns vs 35.9 ns per lookup, an estimated 0.25 % of ROS emission cost);
   Kubernetes DNS labels cannot contain `/`, so the separator is unambiguous.
2. **Duplicate identities fail fast.** Declaring the same `(namespace,
   pod_name)` twice inside one generator instance raises a `ValueError` naming
   the identity and both node names. Last-writes-wins was how the overwrite
   stayed invisible; a fixture that expresses an impossible cluster is now
   rejected instead of silently shrunk.
3. **GPU and MIG seeds include the namespace.** `nise.ocp.gpu.{node}.
   {namespace}.{pod}.{idx}` and `nise.ocp.mig.{node}.{namespace}.{pod}.
   {gpu_idx}.{mig_idx}`. Same-named pods in different namespaces now get
   distinct `gpu_uuid` / `mig_instance_id` values. This changes those values
   for existing fixtures once (see Consequences); the identities were never
   meant to be shared between pods.
4. **The PVC and VM helpers take the namespace explicitly.**
   `get_specific_pvc_from_pod(*, namespace, pod_name)` and
   `get_vm_disk(..., namespace=...)` are keyword-only. No name-only fallback:
   a fallback would resolve to another namespace's PVC whenever it happened to
   exist, which is the bug being fixed.
5. **The CSV contract does not move.** The `pod` column keeps the bare name and
   the row's `namespace` column disambiguates, exactly as the real OCP report
   and the ROS ingestion path expect. Only the generator's internal keys are
   qualified.
6. **An invariant guards the maps.** After construction,
   `_assert_pod_identity_maps_consistent()` requires `set(ros_data) ==
   set(pods)` and `set(gpus) <= set(pods)`, so a future code path that keys one
   map by bare name again fails loudly at generation time instead of
   reintroducing the collapse.

**Out of scope:** VM identity. `vm_pod_map`, `namespace2vm` and the VM report
families still key on `vm_name` alone, so two same-named VMs in different
namespaces still collide. That is a separate defect with a different blast
radius (VM CSV families and the VM ROS reports), tracked in #821.

## Alternatives Considered

- **Tuple keys `(namespace, pod_name)`.** Strictly more explicit, but the dicts
  are iterated per hour per pod, and the measured cost was 6.3 ns per lookup
  against a key whose only consumer is internal. Rejected on cost with no
  correctness gain — the separator is unambiguous for Kubernetes names.
- **Rewrite colliding pod names in the emitted CSV** (e.g. `web-2`). Rejected:
  the generator's job is to reproduce what OCP writes. Inventing names would
  break ROS cursors keyed on the real container name and hide the collision
  from the tests instead of representing it.
- **Warn on overwrite and keep last-wins.** Rejected: the reproducible symptom
  (fewer rows than declared, one namespace missing from a report) survives, and
  warnings in a data generator are routinely unread — the failure surfaces
  later as an inexplicable ROS result.
- **Qualify only when a collision is detected** (namespace-keyed maps for
  colliding names, name-only otherwise). Rejected: two keying schemes in one
  object, decided by a scan of the input, means every reader has to know which
  one is active; the invariant in decision 6 becomes unstateable.

## Consequences

- **Same-named pods are now first-class.** A one-day static fixture declaring
  `namespaceA/web` and `namespaceB/web` emits 192 quarter-hour ROS rows (one
  per pod per interval) instead of 96, with distinct GPU devices when the pods
  declare GPUs.
- **Existing fixtures keep their `pod` values.** Compared as order-insensitive
  row multisets against the baseline (`e2e1b9b`), `examples/ocp_on_aws/
  ocp_static_data.yml` (6 432 rows) is identical except `gpu_uuid` and
  `csi_volume_handle`; the latter differs baseline-against-baseline (it is a
  `uuid4`), so it is not attributable to this change.
- **GPU and MIG identifiers move once, by design.** `ocp_report_gpu_combined.yml`
  differs in 360 `gpu_uuid` values, `ocp_report_gpu_mig.yml` in 264
  `gpu_uuid` values and 72 `mig_instance_id` values, on otherwise identical
  rows (576 each). Anything that pinned the old literal values had to be
  updated; nothing outside the generator's own tests did.
- **Impossible fixtures now error instead of shrinking.** A static YAML that
  declares the same pod twice in one namespace (for example the same namespace
  block on two nodes) fails with `duplicate pod identity 'ns/pod': … declared
  on nodes … and …`. No shipped example fixture does this — verified by
  generating every example OCP YAML before and after the change (identical
  exit codes; the 13 pre-existing failures are Jinja templates, `today_*`
  fixtures needing today's date, and a pre-existing `usage_gig` attribute
  error).
- **Pod-keyed code must keep using the helper.** Iterating the maps yields
  qualified keys; code that needs the CSV name reads `pod_data["pod"]` (as the
  GPU usage emitter now does) rather than the dict key. Decision 6's invariant
  catches a map that reverts to bare names, not a caller that misuses the key
  as a name.

## Verification

- `tests/test_ocp_generator.py` gains five regression tests: distinct
  namespaces keep both pods in `pods`/`ros_data`; a one-day two-namespace
  fixture emits 192 rows; the reverse declaration order still yields distinct
  CPU/GPU/PVC facts per pod; GPU/MIG identities differ across namespaces;
  duplicate `(namespace, pod)` raises. All five fail on the reviewed baseline
  `e2e1b9b2771d` with failure messages naming the collapse
  (`{'web'}` vs `{'ns-a/web', 'ns-b/web'}`, `KeyError: 'ns-a/web'`,
  `ValueError not raised`), and pass after the change.
- Full nise unit suite: 552 tests, OK (baseline 547).
- Every example OCP static YAML swept through the CLI with the same flags on
  the baseline worktree and on the patched tree: identical exit code and error
  text per file.
- `ruff check` and `ruff format --check` clean on the changed files.
- No consumer pins the old identifiers: `nise.ocp.gpu` / `nise.ocp.mig` seed
  strings appear outside the generator only in its own tests, and no IQE
  plugin, koku-ui ROS test, or chart fixture pins a generated `gpu_uuid`
  (hand-authored `GPU-…` literals in the IQE cost plugin are unrelated
  static fixtures).

## Related Decisions

- **#773** (namespace usage aggregation for randomly generated pods) is
  independent of pod identity and is unblocked, not changed, by this ADR.
- **#821** tracks VM identity, which the out-of-scope note under Decision
  deliberately leaves alone.
- ADR-0324 established the other nise→ROS CSV contract that ROS consumes
  (the VM PVC companion file); this ADR fixes the identity vocabulary the pod
  side needed to represent a real cluster.
