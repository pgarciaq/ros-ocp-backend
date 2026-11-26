# Cluster-Filter Resolver Batch A (#951, #953, #954a, #956) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `filter[cluster]` resolve identically and safely everywhere it is resolved: a UUID filter is canonicalized before any string comparison, an alias value is matched literally (no ILIKE metacharacters), a row-scope lookup failure answers 503 instead of an empty list, and the display-currency first match is deterministic.

**Architecture:** Four defects in one function family (`clusterFilterPredicate`, `resolveClusterFilterUUIDs`, `resolveClusterFilterUUID`, `filterClustersByRBAC`) plus their two row-scope callers. One shared escape constructor replaces five ad-hoc `"%" + value + "%"` sites; one canonicalizer replaces four `uuid.Parse` call sites; the plural resolver gains an error return that the two union surfaces render as 503; the singular resolver gains `ORDER BY` and keeps its best-effort contract.

**Tech Stack:** Go, pgx/pgxpool, testcontainers via `internal/testutil`, testify.

**Spec:** [#951](https://github.com/pgarciaq/ros-ocp-backend/issues/951), [#953](https://github.com/pgarciaq/ros-ocp-backend/issues/953), [#954](https://github.com/pgarciaq/ros-ocp-backend/issues/954), [#956](https://github.com/pgarciaq/ros-ocp-backend/issues/956) — each with its 2026-11-27 decision/addendum comment — and `docs/audit/adversarial-review-v18-2026-11-26.md` §3 (V18-01/03/04/06), §6.1, §6.3, §6.6.

## Global Constraints

- **#790 does not move.** Unknown alias + successful query = **200 + count 0**. 503 means "the lookup failed"; 200/0 means "looked, nothing there".
- **Only the row-scope resolver gains an error.** `resolveClusterFilterUUIDs` (2 callers) returns `([]string, error)`; `resolveClusterFilterUUID` (10 callers) keeps `""` on failure and is documented best-effort. Folding one into the other is forbidden.
- **One escaping implementation.** Reuse `escapeILIKE` (`internal/api/utils.go:33`). Do not add a second replacer and do not change the pinned SQL text (Postgres `ILIKE` defaults to backslash escape; the existing `cluster_filter_predicate_test.go` string assertions stay valid).
- **No behaviour change for the UUID form** other than returning the canonical lowercase form.
- `make test-short` must pass. DB tests use `internal/testutil.SetupTestDB` and stay in the two existing container files.
- No new source file for helpers; no OpenAPI change (503 is already documented on `/recommendations/openshift/gpu/timeslicing`, `/recommendations/openshift/gpu/mig`, `/recommendations/openshift/nodes`).

## Review Focus

1. `filter[cluster]=team_a` must not match `teamXa`, and `filter[cluster]=%` must not match every cluster.
2. A literal backslash in the filter round-trips exactly once — double-escaping makes it match a `\\` sequence that is not there.
3. An uppercase UUID filter must return rows, not 200 + empty, and an uppercase RBAC grant must still match a lowercase DB UUID.
4. Unknown alias with a healthy database stays 200 + count 0 — never 503. A 503 for "no match" breaks #790 and the IQE leg that pins it.
5. A multi-match alias must price in one deterministic cluster's currency across calls — not heap order.

---

### Task 1: #956 — one ILIKE contains-pattern constructor

**Files:**
- Modify: `internal/api/utils.go:32-38` (add `containsPattern`, switch its four sites at `:343`, `:399`, `:624`, `:663`)
- Modify: `internal/api/cluster_filter_predicate.go:49`, `:64`, `:119`
- Test: `internal/api/cluster_filter_predicate_test.go`

**Interfaces:**
- Produces: `func containsPattern(s string) string` — `"%" + escapeILIKE(s) + "%"`.
- Later tasks use `containsPattern` for every new or moved pattern; none constructs `"%"+…+"%"` itself.

- [ ] **Step 1: Write the failing tests** in `TestClusterFilterPredicate`:

```go
t.Run("alias metacharacters are matched literally", func(t *testing.T) {
	sql, args, _, ok := clusterFilterPredicate(orgID, "cluster_uuid", "team_a", 2)
	require.True(t, ok)
	assert.Contains(t, sql, "cluster_alias ILIKE $3")
	assert.Equal(t, []any{orgID, `%team\_a%`}, args,
		"an underscore is a literal, not ILIKE's single-char wildcard (#956)")
})

t.Run("a percent filter does not become the whole-fleet wildcard", func(t *testing.T) {
	_, args, _, _ := clusterFilterPredicate(orgID, "cluster_uuid", "%", 2)
	assert.Equal(t, []any{orgID, `%\%%`}, args,
		"`filter[cluster]=%` must match a literal percent, not every alias (#956)")
})

t.Run("a literal backslash escapes exactly once", func(t *testing.T) {
	_, args, _, _ := clusterFilterPredicate(orgID, "cluster_uuid", `a\b`, 2)
	assert.Equal(t, []any{orgID, `%a\\b%`}, args,
		"the backslash is escaped once; double-escaping matches a sequence that is not there (#956)")
})
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/api/ -run TestClusterFilterPredicate -v`
Expected: FAIL — args are `%team_a%` and `%%%`.

- [ ] **Step 3: Implement `containsPattern` and route every site through it**

`internal/api/utils.go`, directly below `escapeILIKE`:

```go
// containsPattern is the single construction point for an ILIKE
// contains-pattern: the value is escaped so user input matches literally
// (#956). Postgres ILIKE defaults to backslash as the escape character, so
// callers bind the result without an ESCAPE clause.
func containsPattern(s string) string { return "%" + escapeILIKE(s) + "%" }
```

Replace `"%"+…+"%"` at `utils.go:343/399/624/663` (behaviour-preserving), and at `cluster_filter_predicate.go:49`, `:64`, `:119` with `containsPattern(…)`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/api/ -run 'TestClusterFilterPredicate|TestParseClusterParams_OuterColumn' -v`
Expected: PASS. The refactor of `utils.go`'s four sites is behaviour-preserving, so `TestParseClusterParams_OuterColumn` must pass unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/api/utils.go internal/api/cluster_filter_predicate.go internal/api/cluster_filter_predicate_test.go
git commit -m "fix(#956): match filter[cluster] aliases literally — one ILIKE contains-pattern constructor"
```

---

### Task 2: #951 — canonicalize UUIDs before every string comparison

**Files:**
- Modify: `internal/api/cluster_filter_predicate.go` (new helper; `:44-45`, `:59-60`, `:107-108`)
- Modify: `internal/api/handlers_node_recs.go:878-881` (`filterClustersByRBAC` grant keys)
- Test: `internal/api/cluster_filter_predicate_test.go` (short), `internal/api/cluster_filter_resolver_test.go` (container)

**Interfaces:**
- Consumes: `github.com/google/uuid` (already imported).
- Produces: `func canonicalClusterUUID(value string) (string, bool)` — the canonical lowercase dashed form and whether the input parses as a UUID.

- [ ] **Step 1: Write the failing short test** in `TestClusterFilterPredicate`:

```go
t.Run("an uppercase UUID binds in canonical form", func(t *testing.T) {
	_, args, _, ok := clusterFilterPredicate(orgID, "cluster_uuid",
		"4C5D2494-F20D-4B4A-9ED7-848954A2B103", 5)
	require.True(t, ok)
	assert.Equal(t, []any{testUUID}, args,
		"UUID-string comparisons are case-sensitive in Go; bind the canonical form (#951)")
})
```

- [ ] **Step 2: Write the failing container tests** — a new `TestResolveClusterFilterUUIDs` function in `cluster_filter_resolver_test.go`, mirroring that file's existing seeding block (`orgID = "org-resolver-unit"`, `testutil.TestClusterUUID` with alias `alpha-a`):

```go
got, err := resolveClusterFilterUUIDs(ctx, pool, orgID, strings.ToUpper(testutil.TestClusterUUID))
require.NoError(t, err)
assert.Equal(t, []string{testutil.TestClusterUUID}, got,
	"an uppercase UUID filter must return the cluster, not no rows (#951)")
```

and in the RBAC case, seed the grant as `strings.ToUpper(testutil.TestClusterUUID)` and assert `filterClustersByRBAC` keeps it — the third comparison site (§6.1 of the audit).

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/api/ -run 'TestClusterFilterPredicate' -v` → FAIL (args hold the uppercase input).
Run: `go test ./internal/api/ -run 'TestResolveClusterFilter' -v` → FAIL (uppercase compared exactly against lowercase DB text).

- [ ] **Step 4: Implement**

`cluster_filter_predicate.go`:

```go
// canonicalClusterUUID returns the canonical lowercase dashed form of a UUID
// input and whether it parses as one (#951). Every comparison that compares
// cluster UUIDs as strings must go through it: Postgres compares uuid values
// canonically, Go compares strings byte-wise.
func canonicalClusterUUID(value string) (string, bool) {
	u, err := uuid.Parse(value)
	if err != nil {
		return "", false
	}
	return u.String(), true
}
```

Then: `clusterFilterPredicate` binds the canonical form on the UUID path; `resolveClusterFilterUUIDs` returns `[]string{canonical}`; `resolveClusterFilterUUID` returns `canonical`.

`filterClustersByRBAC` — inside the existing permission loop only:

```go
for _, p := range clusterPerms {
	if canon, ok := canonicalClusterUUID(p); ok {
		p = canon
	}
	allowed[p] = true
}
```

Non-UUID permission strings keep their current meaning (the `"*"` case is handled earlier).

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/api/ -run 'TestClusterFilterPredicate' -v` → PASS.
Run: `make test-short` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/cluster_filter_predicate.go internal/api/handlers_node_recs.go internal/api/cluster_filter_predicate_test.go internal/api/cluster_filter_resolver_test.go
git commit -m "fix(#951): canonicalize cluster UUIDs before string comparison in the filter resolvers"
```

---

### Task 3: #953 — the row-scope resolver reports failure; the union surfaces answer 503

**Files:**
- Modify: `internal/api/cluster_filter_predicate.go:55-77`
- Modify: `internal/api/handlers_node_recs.go:110-112`
- Modify: `internal/api/handlers_gpu_mig.go:69-72`
- Test: `internal/api/cluster_filter_resolver_test.go` (short, dead pool), `internal/api/substring_alias_union_test.go` (container, 503 wiring)

**Interfaces:**
- Produces: `func resolveClusterFilterUUIDs(ctx context.Context, pool *pgxpool.Pool, orgID, value string) ([]string, error)`
- Consumes: nothing from Tasks 1–2 beyond `containsPattern` and `canonicalClusterUUID`.

- [ ] **Step 1: Write the failing short test** — a lazily-connected pool against a dead DSN fails at `Query`:

```go
func TestResolveClusterFilterUUIDsReportsLookupFailure(t *testing.T) {
	dead, err := pgxpool.New(context.Background(),
		"postgres://u:p@127.0.0.1:1/db?connect_timeout=2")
	require.NoError(t, err)
	defer dead.Close()
	got, qErr := resolveClusterFilterUUIDs(context.Background(), dead, "org-1", "team-a")
	require.Error(t, qErr, "a failed lookup must not be reported as 'nothing matched' (#953)")
	assert.Nil(t, got)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run TestResolveClusterFilterUUIDsReportsLookupFailure -v`
Expected: FAIL. Change the signature first if you want a compiling RED — before the change this does not compile, which is the same signal.

- [ ] **Step 3: Change the signature**

```go
func resolveClusterFilterUUIDs(ctx context.Context, pool *pgxpool.Pool, orgID, value string) ([]string, error) {
```

Return `nil, err` on `pool.Query` failure; after the scan loop `if err := rows.Err(); err != nil { return nil, err }`. Empty filter and UUID form keep returning `(nil, nil)` / `([]string{canonical}, nil)`.

- [ ] **Step 4: Write the failing container test** proving the wiring, in `TestClusterFilter_SubstringAliasIsUnionOnAllSurfaces` — as a new `t.Run` inside it, reusing the `app`, `id` and `orgID` it already builds. Warm the cluster cache with the live pool, then swap `database.Pool` to a dead pool so `getClustersForOrg` still succeeds from cache while the resolver fails. Add two imports to the file: `clustercache "github.com/redhatinsights/ros-ocp-backend/internal/clustercache"` and `database "github.com/redhatinsights/ros-ocp-backend/internal/db"`.

```go
t.Run("a failed alias lookup is 503, not an empty 200", func(t *testing.T) {
	_, err := clustercache.GetClustersForOrg(ctx, orgID) // warm: same org the handler uses
	require.NoError(t, err)
	live := database.Pool
	t.Cleanup(func() { database.Pool = live })
	dead, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?connect_timeout=2")
	require.NoError(t, err)
	t.Cleanup(dead.Close)
	database.Pool = dead

	for _, path := range []string{"/recommendations/openshift/gpu/timeslicing", "/recommendations/openshift/gpu"} {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1"+path+"?filter[cluster]=team-a", nil)
		req.Header.Set("X-Rh-Identity", id)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
			"%s: a failed cluster-filter lookup must not render as an empty list (#953); body: %s", path, rec.Body.String())
	}
})
```

The paths are the ones `app` already registers (`:71` time slicing, `:72` MIG). The cache-warm step is what makes the failure land on the *filter* resolver instead of the earlier "unable to resolve clusters for organization" 503.

- [ ] **Step 5: Wire the 503 in both callers**

`handlers_node_recs.go`:

```go
matched, matchErr := resolveClusterFilterUUIDs(ctx, pool, orgIDStr, clusterFilter)
if matchErr != nil {
	hlog.Errorf("GetNodeRecommendations: failed to resolve cluster filter: %v", matchErr)
	return c.JSON(http.StatusServiceUnavailable, echo.Map{
		"status":  "error",
		"message": "unable to resolve cluster filter",
	})
}
```

`handlers_gpu_mig.go`: the same shape with `hlog.Errorf("GetGPUMIGRecommendations: failed to resolve cluster filter: %v", matchErr)`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/api/ -run TestResolveClusterFilterUUIDsReportsLookupFailure -v` → PASS.
Run: `go test ./internal/api/ -run TestClusterFilter_SubstringAliasIsUnionOnAllSurfaces -v` → PASS (including the existing "unknown alias stays 200 + count 0" case, which pins #790).

- [ ] **Step 7: Commit**

```bash
git add internal/api/cluster_filter_predicate.go internal/api/handlers_node_recs.go internal/api/handlers_gpu_mig.go internal/api/cluster_filter_resolver_test.go internal/api/substring_alias_union_test.go
git commit -m "fix(#953): row-scope cluster-filter lookup failure answers 503, not an empty list"
```

---

### Task 4: #954a — deterministic first match for the currency resolver

**Files:**
- Modify: `internal/api/cluster_filter_predicate.go:116-123`
- Test: `internal/api/cluster_filter_resolver_test.go`

- [ ] **Step 1: Write the failing test** — a new `TestResolveClusterFilterUUIDFirstMatchIsDeterministic` in `cluster_filter_resolver_test.go`, following that file's `SetupTestDB` + `rh_accounts`/`clusters` seeding block:

```go
const small = "11111111-1111-4111-8111-111111111111"
const large = "ffffffff-ffff-4fff-8fff-ffffffffffff"
// Insert the LARGER uuid first and give it the alphabetically earlier alias:
// with no ORDER BY the heap returns that row, so this fixture fails pre-fix
// on purpose instead of by luck.
for _, cl := range []struct{ uuid, alias string }{
	{large, "cur-first"}, {small, "cur-second"},
} { /* same INSERT INTO clusters ... ON CONFLICT DO NOTHING as above */ }

assert.Equal(t, small, resolveClusterFilterUUID(ctx, pool, orgID, "cur-"),
	"first match is the lowest canonical UUID, not heap order (#954a)")
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run TestResolveClusterFilterUUID -v`
Expected: FAIL with the larger UUID returned.

- [ ] **Step 3: Implement**

Append `ORDER BY cluster_uuid` to the singular resolver's query, and extend the existing comment: the first match is the **lowest canonical cluster UUID**, deterministic across calls; exact-alias preference is out of scope (#954a).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/api/ -run 'TestResolveClusterFilterUUID' -v` → PASS (both resolver tests; the pre-existing pass-through and unknown-alias assertions must still hold).

- [ ] **Step 5: Commit**

```bash
git add internal/api/cluster_filter_predicate.go internal/api/cluster_filter_resolver_test.go
git commit -m "fix(#954): deterministic first-match for the cluster-filter currency resolver"
```

---

### Task 5: Close-out — CHANGELOG, decision records, gates, completion report

**Files:**
- Modify: `CHANGELOG.md` (`## [Unreleased]` → `### Fixed`)

- [ ] **Step 1: CHANGELOG** — add two bullets and amend one sentence:

New: one bullet for #956 + #951 (aliases match literally; UUID filters are case-insensitive), one for #953 (503 instead of an empty list when the cluster-filter lookup fails). Amended: the #947 bullet's "The display-currency resolution for multi-match filters remains first-match" gains "in a deterministic order (the lowest cluster UUID)".

- [ ] **Step 2: Run the documentation gates**

Run: `make docs-lint docs-drift docs-check-generated`
Expected: `all checks passed` for each.

- [ ] **Step 3: Post the implementation comment** on each of #951, #953, #954, #956: design decisions taken, files touched, the non-goals below, and the behaviour changes named for users.

- [ ] **Step 4: Record the IQE coverage honestly**

Comment on [#955](https://github.com/pgarciaq/ros-ocp-backend/issues/955) proposing two extra legs — a case-insensitive UUID filter and a literal `%`/`_` alias filter, both observable through the gateway — and note on #953 that the 503 leg cannot be fault-injected through the gateway, so it is covered in-repo only. Do not claim IQE coverage that was not run.

- [ ] **Step 5: Run the full local gates**

Run: `make test-short`, then `make test`, then `make lint`
Expected: all pass. Record the exact counts in the completion report; if a container test needs a retry on first startup ("matched 1 times, expected 2"), say so rather than silently retrying.

- [ ] **Step 6: Completion report** — post the properly-implement-feature table on each issue, distinguishing verified (command output) from asserted, and present it in chat.

- [ ] **Step 7: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(#951,#953,#954,#956): changelog for the cluster-filter resolver batch A repairs"
```

---

## Non-goals (explicit)

- **#950's five surfaces** — GPU TS detail, fleet heatmap, VM list, machine sets, savings-summary exact matches. Batch C, after the unify-vs-document decision.
- **#954(b)** — the page total. Batch D, decided as warn-and-exclude.
- **#955's authorized-empty IQE leg.** Batch B, parallel.
- **`parseClusterParams` behaviour.** Task 1 switches its pattern construction to `containsPattern` with no behaviour change; its surfaces already escape.
- **GPU-model and guest-OS ILIKE sites** (`handlers_node_recs_persist.go:260`, `engine/vm/vm_api_db.go:420`) — a different parameter family, not `filter[cluster]`.
- **Exact-alias preference** in the currency resolver's `ORDER BY`.
- **OpenAPI.** No shape or status change; 503 is already documented on all three affected paths.
- **ADR.** Four defects in one helper family, no architectural choice.
