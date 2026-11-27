# API contract recon

How to find the defects that reading a handler cannot show: one parameter, one
response shape, or one error class implemented several ways across surfaces.

The rule this document exists for: **count mechanisms before estimating, and
report cost in files and code lines, not in surfaces.**

## When

Run a recon before locking the design of any change that touches:

- a parameter accepted by more than one route
- a response shape or a status code another service matches on
- a vocabulary (term, engine, category, node name, table/CSV column keys)
- persistence or output shape (schema, goldens, contract fixtures)

And at each phase boundary, before any sign-off, and whenever a fix's premise is
"the other surfaces already do this".

## What the miss cost (#950 → #963–#968, 2026-11)

The v18 review raised 1 Low + 4 Informational. Recon turned that into ~40 files
across four batches. The estimate that was made before batch A was a **surface
count** ("16 unified, 9 not") — that number was true and useless: `filter[cluster]`
turned out to have **six implementations** (raw uuid bind, exact membership in
the RBAC set, `cluster_alias IN (...)`, an unscoped subquery, the union helper,
and two handlers that dropped the value silently). Each mechanism failed
differently:

| Mechanism | Alias input | UUID input | Result |
|---|---|---|---|
| raw uuid bind | — | — | `503` (Postgres cast) |
| exact alias membership | empty page | empty page | `200` + nothing |
| `cluster_alias IN (...)` | only byte-exact | nothing | `200` + nothing |
| union, but multi-match on a single-object payload | both clusters merged | fine | silently wrong data |
| shared helper | fine | fine | — |

Three lessons:

1. **A surface count is not a cost estimate.** The honest pre-work statement was
   "~40 files, ~560 production code lines, six routes change error codes". Say
   that, in those units, before locking the design.
2. **The worst findings only appear in a fixture.** `/nodes/{node}` merging two
   clusters' variants under one `cluster_uuid` was invisible to reading; it
   appeared when the fixture had two clusters sharing a node name. Build the
   adversarial fixture as part of the recon, not after.
3. **Docs encode old bugs as intent.** After the fix, the API cheat sheet still
   documented the exact-alias comparison as the contract, and the validation
   page named two different branches for the same repository. Recon has to sweep
   docs and consumers, not just code.

## Axes

Enumerate mechanically, then read bodies. `grep` for the mechanism, never for
your memory of it.

| Axis | Enumerate with | A finding looks like |
|---|---|---|
| Parameter interpretation | every handler mentioning the parameter, minus tests; group by the expression it builds | two routes accept the same value and answer differently |
| Error semantics | one bad value across every surface; list statuses | the same input is `404` on one route, `200`-empty on another, `503` on a third |
| Vocabulary | each enum's accepted spellings per route | `short` accepted here, `400` there |
| Shape vs cardinality | does the payload carry the dimension the filter can widen? | a merged object, or one row picked from several |
| Docs ↔ code | each documented parameter claim | a page stating the behaviour the bug produces |
| Consumers | koku-ui request builders, Bruno/cheat sheet, IQE suites | a UI that only ever sends UUIDs, hiding an alias break |
| Performance | the query path behind each surface | filter not index-supported, N+1, full scan per row |

## Method

1. **Enumerate by mechanism.** One grep per shared parameter; bucket call sites
   by the expression they build. A site that bypasses the "shared" helper is a
   bucket of its own — check for it explicitly.
2. **Read the body and the layer beneath it.** Handler, then mapper/allowlist/
   SQL builder/serializer. The allowlist silently dropping a key is a real
   failure mode here.
3. **Probe five input shapes per surface:** valid UUID, alias, alias matching
   several, value matching nothing, lookup failure. Record status + row count.
4. **Build the matrix** — surface × input shape × today × documented contract.
   Every disagreement is a finding, including docs that describe the bug.
5. **Make it visible with a fixture** that varies values: two clusters sharing a
   name, distinct aliases sharing a substring, fan-out-sensitive sums. See
   [adversarial-fixtures.md](adversarial-fixtures.md).
6. **Estimate in the right units.** Files touched, production code lines,
   which error codes change, which pages need rewriting. If that number is much
   larger than expected, surface it before implementing — the owner may want to
   cut scope.
7. **File one issue per finding**; keep the matrix in a single inventory issue
   that the per-finding issues reference.
8. **Name what was not examined** and why (no live lab, no UI checkout, etc.).

## Prompt

> Run an API contract recon over `<scope>`. Do not change code yet.
>
> 1. Mechanically enumerate every surface in scope that consumes each shared
>    parameter, response shape, or error class. Grep — do not work from memory
>    or from the helper's name — and group the hits by the **mechanism** each
>    one uses, not by route.
> 2. For each mechanism bucket, read the handler body **and** the layer it
>    feeds (mapper, allowlist, SQL builder, serializer). Record what it accepts
>    and what it returns for: a valid UUID, an alias, an alias matching several
>    rows, a value matching nothing, and a lookup failure.
> 3. Build the matrix: surface × input shape × today's answer × what the
>    documented contract says. Every disagreement is a finding — including a
>    doc page that states the buggy behaviour as intended.
> 4. Cross-check consumers and docs: `docs-site/`, `CHANGELOG.md`,
>    `costmgmt-api-cheatsheet` (prose + Bruno), koku-ui request builders, IQE
>    suites. Report drift in each.
> 5. For the surfaces in scope, note the query path and whether the filter is
>    index-supported; flag N+1, full scans, and per-row queries that will not
>    hold at cluster scale.
> 6. Report: mechanism buckets, the matrix, findings ranked by demonstrated user
>    impact with severity evidence, and a **cost estimate in files and
>    production code lines plus the error codes that would change**. File one
>    issue per finding; keep the matrix in one inventory issue.
> 7. End with what you did **not** examine, and what that leaves unverified.

## Common mistakes

- Counting surfaces and calling it an estimate.
- Trusting "this goes through the shared helper" — walk the call sites.
- Reading the handler and stopping; the mapper or allowlist is where the value
  is dropped.
- Fixtures where every row has the same value, so a merge or a fan-out bug
  cannot show.
- Sweeping code and skipping docs: a page that documents the bug reads as
  approval of the bug.
- Reporting "all tests pass" as evidence of correctness when the tests were
  written by the same session that wrote the code. Say that plainly; live-bench
  replay and an independent pass are the actual gates.
