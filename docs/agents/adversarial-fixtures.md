# Adversarial Fixtures: Tests That Fail for the Right Reason

## The Problem

A regression test that goes red pre-fix can still prove nothing. #525 shipped
with a colliding-UUID test that failed pre-fix — via lexicographic luck
(`MAX(c.cluster_alias)` picked `alias-tenant-b` over `alias-tenant-a`) — while
its `COUNT(DISTINCT nr.node)` assertion was structurally blind to the join
fan-out it was meant to catch. A same-alias collision would have sailed through
both assertions. The fix was same-value fixtures plus fan-out-sensitive `SUM`
assertions (`ExcessNodes`, savings cents).

Red is necessary. Red **for the right reason** is the requirement.

## Required Practice

Every regression test must do all three:

1. **One sentence per assertion.** In the test comment, state which pre-fix
   behavior each assertion rules out. An assertion you cannot write that
   sentence for is redundant or vacuous — delete it or replace it:

```go
// SUM aggregates double on join fan-out (pre-fix: 2 and "2400.00").
assert.Equal(t, 1, resp.Data[0].ExcessNodes)
```

2. **Mutation-check, then read the failure.** Stash the fix, run the test,
   and read the failure message — it must name the bug mechanism, not an
   incidental property. `expected "alias-tenant-a", actual "alias-tenant-b"`
   names lexicographic order, not tenant isolation: that is the signal to
   strengthen the fixture, not a passing grade. Restore the fix after.

3. **Weaken the fixture, not just the code.** Hold the fix and rerun against
   deliberately weakened fixtures: same values where distinctness could mask,
   single shared alias, reversed insertion order. Confirm the test still
   passes — and that each weakened variant still isolates the bug.

## Masking Catalog (this repo's usual suspects)

| Mask | What it hides | Fixture that defeats it |
|------|---------------|-------------------------|
| `MAX` / `MIN` over joined columns | Multiplicity (fan-out returns same max) | Assert `SUM` / row counts sensitive to fan-out |
| `COUNT(DISTINCT …)` | Join fan-out | Assert non-distinct aggregates (`SUM`, result length) |
| `COALESCE(col, '')` | NULLs | Include NULL rows in the seed |
| `ORDER BY … LIMIT 1` | Nondeterministic winners | Assert full result sets, or deterministic tiebreaks |
| Lexicographic order (`MAX` on text) | Wrong-tenant / wrong-row wins | Same-value fixtures across tenants; reversed insertion order |
| `ON CONFLICT DO NOTHING` in seeds | Schema/constraint changes (0 rows inserted, test goes false-green) | Plain `INSERT` + `require.NoError` — `SetupTestDB` truncates per test, so there is nothing legitimate to conflict with |
| Single-row seeds | Join fan-out (nothing to duplicate) | Multi-row seeds sharing the join key |
| Midnight-only timestamps | Day-boundary off-by-ones | Mid-day `interval_start` alongside midnight |

## Property Tests Complement, Not Replace

Hand-built adversarial fixtures are for SQL paths and integration tests.
For pure engine math (`librobne/`, classifiers, percentile paths), generative
tests automate fixture imagination: `pgregory.net/rapid` is the candidate
(it is currently transitive-only via the testcontainers chain — adopt it as a
direct dependency with `go get` if property tests are wanted; keep SQL paths
hand-built either way).

## Review Norm

Risky patches (tenant isolation, auth/RBAC, migrations) get a pre-commit
adversarial question from a reviewer — human or agent — before merge:

> How could these tests pass while the bug lives?

Post-merge commentary (how #525's weakness was actually caught) is the
fallback, not the process.
