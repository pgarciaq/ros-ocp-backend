# Validating the Native Engine

> **Full guide:** [docs-site/testing/validating-native-engine.md](../../docs-site/testing/validating-native-engine.md)
> — browsable in-repo and on the published site. It is the **single source** for
> this topic: edit facts there.

**Why this page is a stub:** until 2026-11-01 this file duplicated the docs-site
guide (with internal link styling). The pair drifted repeatedly — stale suite
numbers, missing plugin/IQE facts on one side or the other (see #741) — while the
audiences overlapped almost completely, so the duplication was removed. The full
prior content is preserved in git history (through commit `ec0da9c4`).

**Rule for agents:** customer-relevant fact edits go to the docs-site page only;
do not restore duplicated guide content here. This file carries only the notes
below, which are internal-only. The pair still appears in
[docs-site-sync.md](../agents/docs-site-sync.md) so `make docs-sync-check`
keeps both files present.

## Internal-only notes

**E2E upload-scope gap (found in the #706 parity work):** the chart E2E flows
package only the CSV files their own assertions consume — a flow's upload
tarball omits file families the flow does not test (e.g. the snapshot flow's
package carries no storage or CRQ CSVs even though the payload directory
contains them). Any consumer that needs the server to have ingested a
payload's *full* file set — cross-entity parity comparisons, debugging against
a known upload — must inject those files manually with a fresh UUID cluster
id rather than relying on a flow's upload. The recipe:
`.cursor/skills/server-robne-parity/SKILL.md` (Generating Fresh Payloads →
manual injection). Fixing the flows to package everything they generate would
change what each flow's manifest asserts on; treat that as a deliberate chart
change, not a drive-by.

**Cluster rebuild / image tag / log rules:** [AGENTS.md](../../AGENTS.md) and
[sno-cluster-operations](../agents/sno-cluster-operations.md). Chart pytest
(`run-pytest.sh`) is not in this repository — it lives in cost-onprem-chart
until that harness is replaced.
