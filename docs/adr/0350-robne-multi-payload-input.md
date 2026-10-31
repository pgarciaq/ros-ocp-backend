# ADR-0350: robne Multi-Payload Input — First-Is-Primary, Identity-Based Joins

## Status

Accepted (design) — implementation in #721

## Phase

robne CLI — advisory parity (#721–#724); the input-model foundation for the correlator-advisory family

## Context

The W8 non-prod sleep advisory (#716) is the first robne parity target whose inputs span **two KMO payloads**: the guest cluster's digests (the business-hours signature) and the management cluster's facts (the `hcTier` topology emission from #715, the NodePool rows for the reclaim estimate). robne's file mode processes one cluster's payload per run and **deliberately fails fast on mixed payloads** (the W1-review guard: a manifest from another cluster beside this payload's CSVs must not silently scope guardrails).

Four designs were considered (owner decision 2026-10-31):

| Option | Shape | Verdict |
|---|---|---|
| (a) Pure file | A `--hcp-mgmt-dir` flag reading only the topology + NodePool from a second directory | Scaffolding that #722 (zombie parity, which needs the mgmt payload's *digests* for the still-on leg) would immediately outgrow |
| (b) Hybrid | Guest payload on the command line + `--dsn` reading the management facts from the database | The first file-mode output requiring a DB; weaker offline self-containedness |
| (c) Both-paths | `--dsn` or `--hcp-mgmt-dir`, whichever is given | Two code paths for the same facts, kept semantically identical by test |
| **(d) Repeatable `--input`** | **The first payload is the primary (the recommendation cluster); additional payloads are advisory context** | **Chosen** |

## Decision

### 1. Repeatable `--input`, first-is-primary

`--input` accepts multiple values. The **first** is the primary — the cluster whose recommendations get computed, and for advisories the guest whose cluster ID *is* the HC's identity (the N-leg convention). Additional inputs are **context payloads**, consumed only by the advisory family's explicit joins. No `--cluster` override in v1: input order *is* the selection, and every payload's manifest carries its cluster UUID, so attribution is never ambiguous — only the choice of primary is conventional.

### 2. Per-payload consistency, unchanged

Each payload loads via its own `csv.Load` call. The W1-review mixed-payload guard applies **per load** exactly as before: each context payload's manifest must match its own CSVs. A mismatched context payload fails fast rather than mislabeling facts. Context payloads **never scope the primary's guardrails** — only the advisory's explicit identity joins consume them.

### 3. Identity-based joins, not role-based

The advisory joins on the primary's cluster identity, not on payload roles: the tier is the latest `hcTier` among the context topologies' entries whose `hostedClusterID` equals the primary's cluster ID; the worker nodes sum the latest `spec_replicas` per pool for that HC. No "which payload is the management one?" flag, no ordering convention beyond first-is-primary. This eliminates role ambiguity by construction.

### 4. The advisory is independent of the HCP capacity plugin

The standard guest payload carries container CSVs, not HCP capacity CSVs. Gating the advisory on `pluginEnabled("hcp")` would silence it for the standard multi-payload case. The advisory is gated on its own inputs: the BH schedule, the context facts, and the dual digests — silence on any missing input (the server's never-fail-on-evidence posture).

### 5. The envelope version bump is content-conditional

The `hcp_advisories` sibling bumps the JSON envelope version to v13 **only when the sibling is actually present** — unlike the v12 capacity bump, which is capability-conditional (whenever the hcp plugin is enabled). The distinction matters: the existing version tests pin v12 for capacity-only runs, and an unconditional v13 bump broke them. Content-conditional is the honest reading: the version reflects what the consumer sees, not what the CLI could compute.

## Consequences

- The advisory family (#722–#724) inherits the input capability: #722 later consumes the context payload's *digests* (the mgmt still-on leg) through the same repeatable `--input` — no new flag, no redesign.
- The deferred input-model generalizations (`--cluster` override, multi-payload directories, mixed sources) are tracked in #725 with a demonstrated-need trigger.
- robne's single-cluster recommendation contract is unchanged: the primary payload drives all recommendation types; context payloads are advisory-only.

## Related Decisions

- [ADR-0336](0336-robne-json-entity-sibling-arrays.md) — the per-entity sibling-array envelope this extends
- [ADR-0345](0345-robne-offline-input-identity-and-capacity-series.md) — the input-identity contract (manifest cluster attribution) this builds on
- [ADR-0348](0348-w8-nonprod-sleep-advisory.md) — the advisory semantics this computes
- [ADR-0349](0349-hcp-advisories-serving-api-contract.md) — the serving contract the output shape matches
- #721 (implementation), #725 (deferred generalizations)
