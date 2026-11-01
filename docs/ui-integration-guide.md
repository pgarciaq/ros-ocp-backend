# ROS UI Integration Guide

> **Full guide:** [docs-site/ui-integration-guide.md](../docs-site/ui-integration-guide.md)
> — browsable in-repo and on the published site. It is the **single source** for
> this topic: edit facts there. REST API reference for koku-ui (recommendations,
> settings, notifications, pagination, UX patterns) lives on that page.

**Why this page is a stub:** until 2026-11-01 this file duplicated the docs-site
guide — the largest duplicated pair in the repo (1,354 ↔ 1,441 lines, 96%
identical), with no internal-only content. The duplication was removed in the
docs dedup pass; the full prior content is preserved in git history (through
commit `440efc95`).

**Rule for agents:** UI-contract fact edits go to the docs-site page only; do
not restore duplicated content here. The pair still appears in
[docs-site-sync.md](../agents/docs-site-sync.md) so `make docs-sync-check`
keeps both files present.
