# Cost Integration (Koku ⇄ ROS)

> **Full guide:** [docs-site/architecture/cost-integration.md](../../docs-site/architecture/cost-integration.md)
> — browsable in-repo and on the published site. It is the **single source** for
> this topic: edit facts there. Ingestion contract, tags, `effective_rates`,
> `reship_ros`, savings recalculation, formulas, and the plugin savings matrix
> live on that page.

**Why this page is a stub:** until 2026-11-01 this file duplicated the docs-site
guide with different link mechanics — including sibling-relative koku links
(`../../../koku/…`) that only resolve in the multi-repo `~/dev/koku` checkout.
The public page links koku sources absolutely (pgarciaq/koku fork branch; the
referenced files are not on upstream `main` yet). The duplication was removed in
the docs dedup pass (2026-11-01); the full prior content is preserved in git
history (through commit `440efc95`).

**Rule for agents:** integration-contract fact edits go to the docs-site page
only; do not restore duplicated content here. The pair still appears in
[docs-site-sync.md](../agents/docs-site-sync.md) so `make docs-sync-check`
keeps both files present.
