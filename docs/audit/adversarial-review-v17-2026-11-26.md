# Adversarial review v17 — v16 remediation verification + phase-17 closer review

**Date:** 2026-11-26  
**Reviewer:** AI-assisted (session agent); locally executed probes, not a security certification.  
**Verdict:** **PASS with one Low defense-in-depth defect and three informational notes.** No P1/P2 demonstrated in this pass.  
**Findings:** 1 Low, 3 Informational; all with one-line-to-S fixes.  
**Scope:** verification of the seven V16 findings' remediations, plus adversarial review of the phase-17 closer commits (the work since the V16 HEAD).

## 1. Scope and exact revisions

| Repository | V16 reviewed HEAD | V17 reviewed HEAD | Delta |
|---|---|---|---|
| ros-ocp-backend | `da5b76c8faee` | `1fdc648a` | 218 commits; the #913–#946 era and the eight phase-17 closers |
| koku-metrics-operator | `fad195d91105` | `27b82673` | the #829/#836 recovery fixes |
| nise | `0a12a20118e2` | `5beda91` | the #830/#859 and #928/#931 fixes |

Review method: targeted probes on the new code paths (the currency/FX rewrite, the tag-sync caps, the GPU rate, the reship poller/guard/terminal changes, the PDF workflow, the RBAC twin), plus verification that each V16 finding's fix is present at the reviewed HEAD with its regression test. Risk-based, not exhaustive line coverage.

## 2. V16 remediation verification — all seven resolved

| V16 finding | Issue | Fix verified at HEAD | Evidence |
|---|---|---|---|
| V16-01 node-util tag-key oracle | #828 | ✅ `handlers_node_utilization.go:558-564` passes the server-owned `allowedClusters` (the fix comment cites #828); the IQE runbook documents the scoped fixture pattern (#913-era) | Code read + fix commit present |
| V16-02 off-hours medians | #826 | ✅ Closed with the evidence-model rework | Issue closed; the #835 design (hour-grain activity) is the tracked continuation |
| V16-03 two-hours-as-sleep | #827 | ✅ Closed | Same family as #826; the hour-grain signal is the accepted direction |
| V16-04 advice under changed schedule | #831 | ✅ Closed as a risk-reassessment; the reship machinery it questioned was since rebuilt (#863 drain, #936 bounded exhaustion, #937 terminal) | The #936/#937 live verifications are on the issue |
| V16-05 KMO split-part deletion | #829 | ✅ `packaging_recovery_test.go:602` — "every real split part must be recovered" test; the #836 transactional finalize followed | Fix commits `27b82673` lineage |
| V16-06 nise cross-namespace PVC | #830 | ✅ `ocp_generator.py:1973` — the namespace-qualified lookup with the #830 comment; the #838 suppression gate and #859 vm_name fix landed on top | Code read |
| V16-07 nise TLS warning | #832 | ✅ Closed | Fix landed with the warning correction |

**No regressions of the fixed mechanisms were found** in the new code that touches the same paths (the tag-warning scope change in #868's db-mode catalog work preserves the `allowedClusters` threading; the reship rewrite did not reintroduce latest-slot semantics — #863's drain is the documented SET semantics).

## 3. New findings (this pass)

### V17-01 — `ConvertCentsPPB` recurses infinitely on `math.MinInt64` cents

**Severity:** Low (defense-in-depth; no demonstrated reachable path).  
**Dimension:** Correctness / operational robustness.  
**Location:** `internal/costdata/conversion.go` — `ConvertCentsPPB`'s negative branch.  
**Description:** the negative-cents handling negates before recursing (`return -ConvertCentsPPB(-cents, ppb)`). Negating `math.MinInt64` overflows back to `MinInt64` (still negative), so the function recurses forever. **Executed proof:** a probe test calling `ConvertCentsPPB(-9223372036854775808, 860000000)` died with `fatal error: stack overflow` — a process crash, not a panic with a message.  
**Risk:** a savings row whose parsed cents equal MinInt64 would crash the API goroutine's process. Reachability is theoretical: `ParseCentsFromAmount` parses `m.Value` via `ParseFloat` when `Cents == 0`, and Go's out-of-range float→int conversion yields MinInt64 on amd64 — a MoneyAmount with `Value: "-9.2233720368547758e18"` and unset Cents would produce it. No such value is produced by the API's own formatting today.  
**Recommendation:** guard the negation: `if cents == math.MinInt64 { return cents }` (or clamp), plus a test.  
**Effort:** S.

### V17-02 — `money.ConvertAmount` is the dead float path #462 replaced

**Severity:** Informational.  
**Dimension:** Maintainability.  
**Location:** `internal/money/format.go:119-127`.  
**Description:** `ConvertAmount` (float rate, `math.Floor(cents*rate+0.5)`) has no production callers — #462 replaced the pattern with `ConvertCentsPPB`. It is the exact float-conversion idiom the issue removed, left callable.  
**Risk:** a future contributor re-uses it and reintroduces the float round-trip silently.  
**Recommendation:** delete it, or mark it deprecated with a pointer to `ConvertCentsPPB`.  
**Effort:** S.

### V17-03 — Substring aliases: union semantics on the main lists, first-match on GPU TS/MIG

**Severity:** Informational (documented behavior difference).  
**Dimension:** Design quality / correctness.  
**Location:** `resolveClusterFilterUUID` (`cluster_filter_predicate.go`) and the #798 entry normalizations vs. `clusterFilterPredicate`'s ILIKE.  
**Description:** `filter[cluster]=demo` (a substring matching several clusters) returns **all** matching clusters' rows on the main list surfaces (the ILIKE predicate) but **one** cluster's rows on the GPU time-slicing/MIG lists (normalization resolves to the first match, then exact-restricts). `resolveClusterFilterUUID`'s comment documents the single-cluster choice for the currency layer; the row-scoping difference between surfaces is the same choice surfacing differently.  
**Risk:** a user filtering by a shared-prefix alias sees inconsistent row sets between tabs. Same-org information only; no correctness impact per row.  
**Recommendation:** either document the difference in the alias contract (cheapest), or make the GPU TS/MIG entries use the ILIKE predicate for row scoping and normalization only for currency (matching the main lists). Decide with #799's parity contract in view.  
**Effort:** S either way.

### V17-04 — `ConvertCentsPPB`'s overflow doc comment overstates the bound

**Severity:** Informational.  
**Dimension:** Maintainability (documentation accuracy).  
**Location:** `internal/costdata/conversion.go` — the doc comment's "a×ppb overflows only past ~1e28 cents-by-rate".  
**Description:** `a×ppb ≈ cents×rate`, so the intermediate overflows exactly when the **true result** no longer fits int64 (~9.2e18 cents-by-rate, not 1e28). The decomposition is still correct — it never overflows where the true result fits — but the stated bound is wrong by nine orders of magnitude.  
**Recommendation:** correct the comment to "overflows only when the true converted amount exceeds int64 — never where the result fits".  
**Effort:** S.

## 4. Checked and clean (this pass)

- **The 23 `rate == 1.0` sweep (#462)**: grep for remaining float comparisons on rate variables — only `money.ConvertAmount`'s dead code (V17-02). The sweep was complete.
- **`ParseRateStringToPPB`**: rejects negatives and scientific notation via `isAllDigits` (garbage errors, not silent rates); a 15-digit combined value cannot overflow int64; digits beyond the 9th decimal truncate per the documented ratio-precision rationale.
- **The #941 budget vs. the 10 MiB body limit**: the body limit is the binding payload constraint; the 100k budget bounds catalog cardinality — the two compose correctly.
- **The #937 reason taxonomy**: 404 (no provider) → terminal; 5xx/network → retry. A provider-existing-but-404 would require the provider scan to positively miss, which is a DB query — failure there is a 5xx. The classes are genuinely disjoint.
- **The PDF workflow**: `permissions: contents: read` at the top, `contents: write` scoped to the release job only; no `pull_request_target`; triggers are push/dispatch on docs paths. Release assets never committed. Sound.
- **The RBAC twin (#921)**: the distinct component label + NetworkPolicy patches + the immutable-selector delete-before-apply are all present in `docs/testing/rbacstub/deploy.sh`; the #921 live verification (main Service endpoints clean) stands.
- **The internal tags-sync endpoint**: SA-token review auth unchanged by #941; validation is pre-write (the live verification proved the ordering — validation errors write nothing).

## 5. Scorecard

| Dimension | Rating | Key note |
|-----------|--------|----------|
| Security | ★★★★☆ | No new exposure; the twin and internal endpoints hold; the alias substring is same-org only |
| Correctness | ★★★★☆ | V17-01 is the one defect, theoretical reachability, one-line fix |
| Auditability | ★★★★★ | The session's issues carry decision records, live-verification evidence, and mutation checks |
| Operational robustness | ★★★★☆ | The reship rewrite (bounded exhaustion, terminal providerless, drain semantics) is verified live |
| Performance | ★★★★☆ | The ppb conversion is integer math; the Tier-B GPU rate adds one indexed SUM per cycle |
| Design quality | ★★★★☆ | V17-03 is the notable inconsistency (substralias semantics across surfaces) |
| Maintainability | ★★★★☆ | V17-02's dead float path is the #462 lesson's residue |
| Governance | ★★★★★ | ADR discipline, changelog, CI gates, and the audit cadence itself all maintained |

## 6. Priority remediation order

1. **V17-01** — the MinInt64 guard (S) — the only code change recommended before the phase-17 sign-off.
2. **V17-02** — delete/deprecate `money.ConvertAmount` (S).
3. **V17-04** — the doc-comment bound correction (S) — rides V17-01's commit.
4. **V17-03** — the alias-semantics decision (S; a docs decision or a small normalization change; can ride phase 18 if the difference is documented now).

## 7. Current state

4 findings: 1 Low, 3 Informational. All verified against the reviewed HEADs with executed probes where applicable (V17-01's stack overflow reproduced; the sweep completeness grep-run; the V16 remediations code-read). The phase-17 closer work — eight issues, each with RED-first tests, live verification, and mutation checks — holds up under adversarial review. The branch is **ready for sign-off** once V17-01 lands.
