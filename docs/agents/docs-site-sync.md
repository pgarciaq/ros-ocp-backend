# docs-site Sync Rule

**Policy A:** Keep both trees. `docs/` = internal/developer depth; `docs-site/` = public
MkDocs site. They need not be identical prose, but **customer-relevant facts must stay in
sync** when you edit either side. Prefer editing `docs-site/` for public contracts
(defaults, API paths, deploy behavior), then mirror into `docs/` when a parallel internal
page exists.

Do **not** rely on CI to copy `docs/architecture/` or `docs/operations/` over `docs-site/` —
that overwrite was removed in #417 because it wiped curated public pages (Last verified,
Class A fixes) on every Pages build. Sync is a human/agent checklist, not an automatic
clobber.

Expected differences (do **not** “fix” by wholesale copy):
- Link style: `docs/` uses repo-relative paths; `docs-site/` uses GitHub/`{{ git_branch }}` links
- `> **Last verified:**` stamps live on Class A **docs-site** pages
- Intentional depth: e.g. idle-detection / business-hours internals vs public plugin-ref

`docs/` contains internal developer documentation.
`docs-site/` contains the public-facing documentation website.
`docs/agents/` is agent guidance only — no public pair; do not invent a docs-site copy.

## Parallel pairs (check both sides)

| Internal docs path | Public docs-site equivalent |
|---|---|
| `docs/operations/configuration.md` | `docs-site/configuration.md` (also `docs-site/operations/configuration.md`) |
| `docs/testing/validating-native-engine.md` | `docs-site/testing/validating-native-engine.md` |
| `docs/testing/iqe-requirements-registration.md` | `docs-site/testing/iqe-requirements-registration.md` |
| `docs/ui-integration-guide.md` | `docs-site/ui-integration-guide.md` |
| `docs/upgrade-runbook.md` | `docs-site/operations/upgrade-runbook.md` |
| `docs/operations/monitoring.md` | `docs-site/monitoring.md` |
| `docs/operations/query-performance.md` | `docs-site/query-performance.md` |
| `docs/known-issues.md` | `docs-site/known-issues.md` |
| `docs/features/idle-detection.md` | `docs-site/plugin-reference/idle-detection.md` |
| `docs/features-business-hours.md` | `docs-site/plugin-reference/business-hours.md` |
| `docs/features/quota-recommendations.md` | `docs-site/plugin-reference/quota.md` |
| `docs/features/cluster-resource-quota.md` | `docs-site/plugin-reference/cluster-quota.md` |
| `docs/operations/api-query-parameters.md` | `docs-site/plugin-reference/query-parameters.md` |
| `docs/architecture/recommendation-engines.md` | `docs-site/architecture/recommendation-engines.md` |
| `docs/architecture/configurability.md` | `docs-site/architecture/configurability.md` |
| `docs/architecture/cost-integration.md` | `docs-site/architecture/cost-integration.md` |
| `docs/architecture/plugin-architecture.md` | `docs-site/architecture/plugin-architecture.md` |
| `docs/architecture/native-migration.md` | `docs-site/architecture/native-migration.md` |
| `docs/architecture/notification-codes.md` | `docs-site/api-reference/notification-codes.md` |

Inventory / checklist: `make docs-sync-check` (`scripts/check-docs-sync.sh`).

## When editing any file under `docs/` **or** `docs-site/`

1. Find the matching pair from the table (or `make docs-sync-check`)
2. If the change is **customer-relevant** (defaults, API paths, deploy contracts, feature
   status), apply the same fact on the other side (adapt links; do not clobber depth)
3. Prefer `docs-site/` as SoT for public contracts; mirror Class A into `docs/`
4. If no public equivalent exists but content is customer-facing, suggest creating one
5. For **Class A** docs-site pages, bump `> **Last verified:** YYYY-MM-DD` — see
   CONTRIBUTING.md “Last-verified convention”

Skip pair lookup for `docs/agents/` (internal only).

## Last-verified (docs-site)

- Format: `> **Last verified:** YYYY-MM-DD` (visible blockquote; not an HTML comment)
- Means Class A facts were checked against code/OpenAPI/chart on that date
- Required on Class A; encouraged on Features/plugin-ref after edits; skip Historical/planned
- Lightweight CI: `scripts/check-docs-drift.sh` (`make docs-drift`)

## What stays internal-only (never sync to docs-site)

- Agent guidance (`docs/agents/`)
- ADRs and deep design docs (`docs/adr/`, most of `docs/design/`)
- Implementation plans (`docs/plans/`)
- Phase notes and archives (`docs/archive/`, `docs/phase*`)
- Test plans and audits under archive
- Internal tooling docs (`docs/bruno/`)
- Spec snapshots that live under Historical on the public site only
- Historical performance audits that record old defaults (do not “update” past tense)

## Phase branch bumps (critical)

When moving to a new `pgarciaq-rosocp-superpowers-phaseN` branch, **never** run a
repo-wide search-and-replace of the old phase branch name.

- **Update:** live clone/checkout pointers (`mkdocs.yml` `repo_url`, quickstart,
  validating-native-engine, forward-looking cherry-pick source in upstreaming plan).
- **Keep unchanged:** audit reports (`docs/performance/*`), changelog release
  `**Branch:**` lines, feature-status archive rows, whats-new “Recently completed”
  sections, point-in-time analysis notes, image tag comments, benchmark runbook pins.

Rule: *work done on branch X* → keep X; *which branch to use today* → new phase branch.

Full checklist: `CONTRIBUTING.md` → “Phase branch bump checklist”.
