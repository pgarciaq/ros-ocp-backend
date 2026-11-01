# Configurability Reference

> **Full guide:** [docs-site/architecture/configurability.md](../../docs-site/architecture/configurability.md)
> — browsable in-repo and on the published site. It is the **single source** for
> this topic: edit facts there. Every `ROS_*` variable, the Settings API routes,
> the precedence model, and the tuning guidance live on that page.

**Why this page is a stub:** until 2026-11-01 this file duplicated the docs-site
guide with internal link styling. The pair was 95% identical, so the
duplication was removed in the docs dedup pass. The full prior content is
preserved in git history (through commit `440efc95`).

**Rule for agents:** configuration fact edits go to the docs-site page only; do
not restore duplicated content here. The pair still appears in
[docs-site-sync.md](../agents/docs-site-sync.md) so `make docs-sync-check`
keeps both files present.

## Internal-only notes

- Deep design rationale for configurable areas lives in the internal
  [design docs](../design/) (e.g. [VM recommendations design](../design/vm-recommendations.md)) —
  those never sync to the public site, so the docs-side configurability page
  deliberately does not link them.
