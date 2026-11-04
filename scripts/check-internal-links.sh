#!/usr/bin/env bash
# check-internal-links.sh — maintained internal-documentation link gate (#778).
#
# WHY A SEPARATE SCRIPT FROM check-docs-lint.sh
#   check-docs-lint.sh guards docs-site/, the published tree. It verifies that a
#   relative target EXISTS; it never checks anchors. This script guards the
#   maintained internal docs, which docs-lint cannot see, and it does check
#   anchors.
#
# WHY GITHUB'S SLUGGER AND NOT MKDOCS'
#   mkdocs.yml sets `docs_dir: docs-site`, and scripts/docs-pdf.sh copies only
#   docs-site/ into the PDF work tree. Nothing under docs/ is ever rendered by
#   MkDocs, so every link in this scope is resolved by GitHub's Markdown
#   renderer when a human browses the repository. The two sluggers disagree on
#   punctuation: "Idle / Zombie detection" becomes `idle--zombie-detection` on
#   GitHub and `idle-zombie-detection` under Python-Markdown's toc.slugify.
#   Validating internal anchors with the MkDocs slugger both hides real breaks
#   and invents false ones. Published-site anchors are a different defect with a
#   different oracle and are tracked separately.
#
# SCOPE
#   Sources: every *.md under docs/ except the record trees below, plus the root
#   guide files. Checks, for each relative link outside fenced blocks and inline
#   code: the target resolves from the source file's parent directory, it stays
#   inside the repository, and -- when the target is Markdown and the link has a
#   #fragment -- that fragment exists.
#
# RECORD TREES ARE EXCLUDED ON PURPOSE
#   docs/adr, docs/audits, docs/archive, docs/historical, docs/issues,
#   docs/plans, docs/superpowers are point-in-time records: their links describe
#   the tree as it stood when the record was written. Rewriting them would
#   falsify the record, and new records would reintroduce breaks forever.
#   CHANGELOG.md is generated history and is rewritten to GitHub blob URLs for
#   the published changelog.
#
# Soft mode (--soft or DOCS_LINKS_SOFT=1) prints findings and exits 0. It exists
# for local debugging of a large migration; CI always runs strict.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SOFT=0
if [[ "${1:-}" == "--soft" ]] || [[ "${DOCS_LINKS_SOFT:-}" == "1" ]]; then
  SOFT=1
fi

python3 - "$SOFT" <<'PY'
from __future__ import annotations

import collections
import re
import sys
from pathlib import Path
from urllib.parse import unquote

soft = sys.argv[1] == "1"
root = Path(".").resolve()

# --- scope -----------------------------------------------------------------
INCLUDE_DIRS = ("docs",)
EXCLUDE_SOURCES = (
    "docs/adr",
    "docs/audit",
    "docs/audits",
    "docs/archive",
    "docs/historical",
    "docs/issues",
    "docs/plans",
    "docs/superpowers",
)
ROOT_FILES = ("README.md", "CONTRIBUTING.md", "AGENTS.md")

# --- parsing ---------------------------------------------------------------
FENCE_RE = re.compile(r"^\s*(```|~~~)")
INLINE_LINK_RE = re.compile(r"(?<!!)\[[^\]]*\]\(\s*([^)\s]+?)(?:\s+\"[^\"]*\")?\s*\)")
REF_DEF_RE = re.compile(r"^\s*\[[^\]]+\]:\s*(\S+)")
CODE_SPAN_RE = re.compile(r"`[^`]*`")
SKIP_SCHEMES = ("#", "http://", "https://", "mailto:", "ftp:", "tel:", "data:")
HEADING_RE = re.compile(r"^(#{1,6})\s+(.*?)\s*#*\s*$")


def github_slug(text: str) -> str:
    """GitHub-flavored heading anchor.

    Lowercase, strip inline HTML, drop every character that is not a word
    character, hyphen or space, then turn spaces into hyphens.
    """
    out = re.sub(r"<[^>]+>", "", text.strip().lower())
    out = re.sub(r"[^\w\- ]", "", out)
    return out.replace(" ", "-")


def headings(path: Path) -> set[str]:
    """Anchor set as GitHub builds it, including duplicate-heading suffixes."""
    result: set[str] = set()
    if not (path.is_file() and path.suffix == ".md"):
        return result
    seen: collections.Counter[str] = collections.Counter()
    in_fence = False
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        if FENCE_RE.match(line):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        match = HEADING_RE.match(line)
        if not match:
            continue
        base = github_slug(match.group(2))
        count = seen[base]
        seen[base] += 1
        result.add(base if count == 0 else f"{base}-{count}")
    return result


def links_in(path: Path) -> list[tuple[int, str]]:
    """Yield (line number, href) outside fenced blocks and inline code."""
    found: list[tuple[int, str]] = []
    in_fence = False
    for number, line in enumerate(
        path.read_text(encoding="utf-8", errors="replace").splitlines(), 1
    ):
        if FENCE_RE.match(line):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        prose = CODE_SPAN_RE.sub("", line)
        for match in INLINE_LINK_RE.finditer(prose):
            found.append((number, match.group(1)))
        ref = REF_DEF_RE.match(prose)
        if ref:
            found.append((number, ref.group(1)))
    return found


def sources() -> list[Path]:
    chosen: list[Path] = []
    for directory in INCLUDE_DIRS:
        for path in sorted((root / directory).rglob("*.md")):
            rel = path.relative_to(root).as_posix()
            if any(rel == e or rel.startswith(e + "/") for e in EXCLUDE_SOURCES):
                continue
            chosen.append(path)
    for name in ROOT_FILES:
        path = root / name
        if path.is_file():
            chosen.append(path)
    return chosen


def skippable(href: str) -> bool:
    if href.startswith(SKIP_SCHEMES):
        return True
    if href.startswith("<") or "{{" in href or "{%" in href:
        return True
    return False


def main() -> int:
    failures: list[tuple[str, int, str, str]] = []
    affected: set[str] = set()
    checked = 0
    sibling_refs = 0

    for path in sources():
        source = path.relative_to(root).as_posix()
        own_anchors = None
        for number, href in links_in(path):
            if skippable(href):
                continue
            checked += 1
            target, _, fragment = href.partition("#")
            target = unquote(target).split("?")[0]

            if not target:
                # same-page anchor
                if fragment:
                    if own_anchors is None:
                        own_anchors = headings(path)
                    if fragment not in own_anchors:
                        failures.append(
                            (source, number, href, f"no heading #{fragment} on this page")
                        )
                        affected.add(source)
                continue

            resolved = (path.parent / target).resolve()
            try:
                rel = resolved.relative_to(root).as_posix()
                inside = True
            except ValueError:
                inside = False
                rel = str(resolved)

            if not inside:
                # A reference into a sibling checkout: the documented dev layout
                # is ~/dev/koku/{koku,koku-ui,koku-metrics-operator,cost-onprem-chart,
                # iqe-*}. Those are not checked out in CI, so they are out of
                # scope -- UNLESS the first path component is a tree of THIS
                # repository, which means one '../' too many and the author meant
                # to stay inside the repo (the #748 defect).
                try:
                    beyond = resolved.relative_to(root.parent).parts
                except ValueError:
                    beyond = ()
                if beyond and (root / beyond[0]).exists():
                    failures.append(
                        (
                            source,
                            number,
                            href,
                            f"{rel} escapes the repository; "
                            f"'{beyond[0]}/' is a tree of this repository",
                        )
                    )
                    affected.add(source)
                else:
                    sibling_refs += 1
                continue

            if not resolved.exists():
                failures.append((source, number, href, f"{rel} does not exist"))
                affected.add(source)
                continue

            if fragment and resolved.suffix == ".md":
                if fragment not in headings(resolved):
                    failures.append(
                        (source, number, href, f"no heading #{fragment} in {rel}")
                    )
                    affected.add(source)

    print(f"scanned {checked} relative links in {len(sources())} files")
    if sibling_refs:
        print(
            f"skipped {sibling_refs} links into sibling checkouts "
            "(not verifiable in CI)"
        )
    if failures:
        print(f"TOTAL: {len(failures)} broken link occurrences in {len(affected)} files\n")
        for source, number, href, why in failures:
            print(f"FAIL {source}:{number}\n     {href}\n     {why}")
        if soft:
            print("\nsoft mode: exiting 0 despite failures")
            return 0
        return 1

    print("OK: every relative link resolves and every anchor exists")
    return 0


sys.exit(main())
PY
