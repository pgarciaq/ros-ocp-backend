#!/usr/bin/env python3
"""Derive per-section PDF book navs from mkdocs.yml (#383).

The PDF pipeline (scripts/docs-pdf.sh) used to carry a hand-copied nav per
book. Those copies drifted: pages added to mkdocs.yml after the 2026-07-30
books reached the site but no book, and mkdocs only *warns* about a nav entry
that no longer resolves, so a moved page silently vanished from its book.

This script makes mkdocs.yml the single source:

    list --json | list --slugs | title/pages/nav --section SLUG | verify

A book is a top-level nav SECTION (an entry with children); a top-level page
(Home) is not a book. A book nav is FLAT: mkdocs-to-pdf iterates only
top-level nav items, so a nested group would render as an empty book. Slugs
are a pinned contract — they are release asset names and download URLs.

`verify` is the CI gate: it reports every nav page that does not exist and
every section that has no slug, and exits 1 (mkdocs itself would exit 0).
"""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass
from pathlib import Path

import yaml

# Section title in mkdocs.yml -> book slug (release asset name, pinned).
SLUGS: dict[str, str] = {
    "Getting Started": "getting-started",
    "Features": "features",
    "Features (planned)": "planned-features",
    "Architecture": "architecture",
    "Testing": "testing",
    "Plugin Reference": "plugin-reference",
    "API Specification": "api",
    "Operations": "operations",
    "Security & Compliance": "security",
    "UI Integration": "ui-integration",
    "Historical": "historical",
}


@dataclass(frozen=True)
class Leaf:
    """One nav page. `label` is None when mkdocs.yml names the path alone."""

    label: str | None
    path: str


@dataclass(frozen=True)
class Book:
    slug: str
    title: str
    leaves: tuple[Leaf, ...]


class _Loader(yaml.SafeLoader):
    """SafeLoader that tolerates MkDocs' python/name tags.

    mkdocs.yml uses unresolved Python tags for two options —
    `!!python/name:pymdownx.superfences.fence_code_format` (superfences
    `format`) and `!!python/object/apply:pymdownx.slugs.slugify` (toc
    `slugify`) — which SafeLoader rejects. The nav never depends on either
    value, so the whole python tag namespace maps to None.
    """


_Loader.add_multi_constructor(  # type: ignore[no-untyped-call]
    "tag:yaml.org,2002:python/",
    lambda loader, suffix, node: None,
)
_Loader.add_multi_constructor(  # type: ignore[no-untyped-call]
    "!",
    lambda loader, suffix, node: None,
)


def load_config(path: Path) -> dict:
    with path.open(encoding="utf-8") as handle:
        config = yaml.load(handle, Loader=_Loader)  # noqa: S506 (custom SafeLoader)
    if not isinstance(config, dict):
        raise SystemExit(f"{path}: not a mapping")
    return config


def _page_path(value: object) -> str | None:
    """The .md path a nav value names, or None when it is not a page.

    A fragment is stripped: `- Foo: features/bar.md#section` names a page that
    must stay in the book, and a value that matches no page is reported by
    `verify` instead of being dropped silently.
    """
    if not isinstance(value, str):
        return None
    path = value.strip().split("#", 1)[0]
    return path if path.endswith(".md") else None


def _flatten(items: list, out: list[Leaf]) -> None:
    for item in items:
        if isinstance(item, str):
            path = _page_path(item)
            if path:
                out.append(Leaf(None, path))
            continue
        if not isinstance(item, dict) or len(item) != 1:
            continue
        label, value = next(iter(item.items()))
        if isinstance(value, list):
            # A group inside a section: keep its pages, drop the group label.
            _flatten(value, out)
        else:
            path = _page_path(value)
            if path:
                out.append(Leaf(str(label), path))
            # A bare URL (external link) is not a page; verify reports any
            # other non-page value so it cannot vanish unnoticed.


def top_level_pages(config: dict) -> list[Leaf]:
    """Nav entries that are single pages (Home), not sections."""
    pages: list[Leaf] = []
    for item in config.get("nav") or []:
        if isinstance(item, str):
            if _is_page(item):
                pages.append(Leaf(None, item.strip()))
            continue
        if not isinstance(item, dict) or len(item) != 1:
            continue
        label, value = next(iter(item.items()))
        path = _page_path(value)
        if path:
            pages.append(Leaf(str(label), path))
    return pages


def sections(config: dict) -> list[tuple[str, tuple[Leaf, ...]]]:
    """(title, leaves) per top-level nav section, in nav order."""
    found: list[tuple[str, tuple[Leaf, ...]]] = []
    for item in config.get("nav") or []:
        if not isinstance(item, dict) or len(item) != 1:
            continue
        title, value = next(iter(item.items()))
        if not isinstance(value, list):
            continue
        leaves: list[Leaf] = []
        _flatten(value, leaves)
        found.append((str(title), tuple(leaves)))
    return found


def books(config: dict) -> list[Book]:
    """Mapped books in nav order; a section without a slug is left out."""
    result: list[Book] = []
    for title, leaves in sections(config):
        slug = SLUGS.get(title)
        if slug is None:
            continue
        result.append(Book(slug=slug, title=title, leaves=leaves))
    return result


def _book(config: dict, slug: str) -> Book:
    for book in books(config):
        if book.slug == slug:
            return book
    known = ", ".join(book.slug for book in books(config))
    raise SystemExit(f"unknown section '{slug}'. Known: {known}")


def cmd_list(config: dict, args: argparse.Namespace) -> int:
    found = books(config)
    if args.json:
        print(json.dumps([
            {"slug": book.slug, "title": book.title, "pages": len(book.leaves)}
            for book in found
        ]))
    elif args.slugs:
        for book in found:
            print(book.slug)
    else:
        for book in found:
            print(f"{book.slug:<18} {len(book.leaves):>3} pages  {book.title}")
    return 0


def cmd_title(config: dict, args: argparse.Namespace) -> int:
    print(_book(config, args.section).title)
    return 0


def cmd_pages(config: dict, args: argparse.Namespace) -> int:
    for leaf in _book(config, args.section).leaves:
        print(leaf.path)
    return 0


def cmd_nav(config: dict, args: argparse.Namespace) -> int:
    for leaf in _book(config, args.section).leaves:
        if leaf.label:
            print(f"  - {leaf.label}: {leaf.path}")
        else:
            # Unlabelled in mkdocs.yml: let the page's own H1 name the chapter
            # instead of inventing a title from the filename.
            print(f"  - {leaf.path}")
    return 0


def _non_page_values(config: dict) -> list[tuple[str, str]]:
    """Every nav value that is not a page (external links included)."""
    found: list[tuple[str, str]] = []

    def walk(items: list, owner: str) -> None:
        for item in items:
            if isinstance(item, str):
                if not _page_path(item):
                    found.append((item, owner))
                continue
            if not isinstance(item, dict) or len(item) != 1:
                continue
            label, value = next(iter(item.items()))
            if isinstance(value, list):
                walk(value, str(label))
            elif not _page_path(value):
                found.append((str(value), str(label)))

    for item in config.get("nav") or []:
        if isinstance(item, dict) and len(item) == 1:
            label, value = next(iter(item.items()))
            if isinstance(value, list):
                walk(value, str(label))
            elif not _page_path(value):
                found.append((str(value), str(label)))
        elif isinstance(item, str) and not _page_path(item):
            found.append((item, "top level"))
    return found


def cmd_verify(config: dict, args: argparse.Namespace) -> int:
    site_root = Path(args.site_root) if args.site_root else args.mkdocs.parent
    docs_dir = Path(config.get("docs_dir") or "docs-site")
    if not docs_dir.is_absolute():
        docs_dir = site_root / docs_dir

    problems: list[str] = []
    seen: dict[str, str] = {}

    for title, leaves in sections(config):
        slug = SLUGS.get(title)
        if slug is None:
            problems.append(f"unmapped nav section: {title}")
        elif slug in seen:
            problems.append(f"duplicate slug: {slug} ({seen[slug]} and {title})")
        else:
            seen[slug] = title
        for leaf in leaves:
            if not (docs_dir / leaf.path).is_file():
                problems.append(f"missing page: {leaf.path} (nav: {title})")

    for value, label in _non_page_values(config):
        if not (value or "").startswith(("http://", "https://")):
            problems.append(f"unsupported nav value: {value} (nav: {label})")

    for leaf in top_level_pages(config):
        if not (docs_dir / leaf.path).is_file():
            problems.append(f"missing page: {leaf.path} (nav: {leaf.label or 'top level'})")

    if problems:
        for problem in problems:
            print(problem, file=sys.stderr)
        print(
            f"\nPDF nav coverage FAILED ({len(problems)} problem(s)). mkdocs would "
            "drop these nav entries with a warning; each one is a chapter missing "
            "from a book, or a book published under a name nothing links to.",
            file=sys.stderr,
        )
        return 1

    print(f"PDF nav coverage OK: {len(seen)} books, every nav page present")
    return 0


def build_parser() -> argparse.ArgumentParser:
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--mkdocs", type=Path, default=Path("mkdocs.yml"),
                        help="mkdocs config to read (default: ./mkdocs.yml)")

    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)

    p_list = sub.add_parser("list", parents=[common], help="list books")
    group = p_list.add_mutually_exclusive_group()
    group.add_argument("--json", action="store_true", help="JSON array of {slug,title,pages}")
    group.add_argument("--slugs", action="store_true", help="one slug per line")
    p_list.set_defaults(func=cmd_list)

    for name, func, help_text in (
        ("title", cmd_title, "print a book's mkdocs section title"),
        ("pages", cmd_pages, "print a book's page paths (relative to docs_dir)"),
        ("nav", cmd_nav, "print a book's flat YAML nav"),
    ):
        sub_parser = sub.add_parser(name, parents=[common], help=help_text)
        sub_parser.add_argument("--section", required=True, help="book slug")
        sub_parser.set_defaults(func=func)

    p_verify = sub.add_parser("verify", parents=[common],
                              help="fail when a nav page is missing or a section has no slug")
    p_verify.add_argument("--site-root", type=Path, default=None,
                          help="directory holding docs_dir (default: the mkdocs file's directory)")
    p_verify.set_defaults(func=cmd_verify)
    return parser


def main(argv: list[str]) -> int:
    args = build_parser().parse_args(argv)
    config = load_config(args.mkdocs)
    return args.func(config, args)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
