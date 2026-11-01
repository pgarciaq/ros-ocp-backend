# Legacy-to-Native Engine Migration Guide

> **Full guide:** [docs-site/architecture/native-migration.md](../../docs-site/architecture/native-migration.md)
> — browsable in-repo and on the published site. It is the **single source** for
> this topic: edit facts there. The consumer-migration status table (koku-ui-ros
> #599 phases, RHDH plugin #605/#606, IQE rewrite lanes), the units/term-engine/
> box-plot traps, and the decommission target live on that page.

**Why this page is a stub:** this file was an abridged copy that lagged the
public page (it lacked the consumer-migration section and the precise
box-plots wording). The duplication was removed in the docs dedup pass
(2026-11-01). The full prior content is preserved in git history (through
commit `440efc95`).

**Rule for agents:** migration fact edits go to the docs-site page only; do not
restore duplicated content here. The pair still appears in
[docs-site-sync.md](../agents/docs-site-sync.md) so `make docs-sync-check`
keeps both files present.
