#!/usr/bin/env bash
# check-docs-lint.sh — Light docs-site linter (#419).
#
# Tooling choice: custom relative-link checker (stdlib Python; no lychee install).
# Primary value: catch MkDocs docs_dir escapes — e.g. ../../docs/design/foo.md
# becomes /docs/design/foo.md on GitHub Pages and 404s (only docs-site/ is published).
#
# Also validates in-page fragments against the ids the published renderer
# produces (#802). mkdocs.yml pins GitHub's slug rule via
# pymdownx.slugs.slugify, so a heading has the same anchor on GitHub and on the
# site; this script re-implements that rule (and uses pymdownx directly when it
# is installed) plus Python-Markdown's duplicate suffixes.
#
# External http(s) and {{ git_branch }} GitHub URLs are skipped (checked elsewhere
# or resolve at MkDocs build). Class A Last-verified presence is checked.
#
# Soft mode (CI default): DOCS_LINT_SOFT=1 exits 0 after printing failures so the
# workflow can stay non-blocking until the backlog of escaped links is cleared.
# Hard mode (local / future gating): omit DOCS_LINT_SOFT → exit 1 on findings.
#
# Keep scripts/check-docs-drift.sh for Class A footguns; this does not replace it.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SOFT=0
if [[ "${1:-}" == "--soft" ]] || [[ "${DOCS_LINT_SOFT:-}" == "1" ]]; then
  SOFT=1
fi

python3 - "$SOFT" <<'PY'
from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlparse

soft = sys.argv[1] == "1"
docs_site = Path("docs-site")
docs_site_root = docs_site.resolve()

LINK_RE = re.compile(r"(?<!!)\[([^\]]*)\]\(([^)]+)\)")
FENCE_RE = re.compile(r"^```")
STAMP_RE = re.compile(r"^> \*\*Last verified:\*\* \d{4}-\d{2}-\d{2}", re.M)

CLASS_A_STAMP_REQUIRED = [
    "docs-site/configuration.md",
    "docs-site/quickstart.md",
    "docs-site/architecture/configurability.md",
    "docs-site/architecture/recommendation-engines.md",
    "docs-site/architecture/cost-integration.md",
    "docs-site/architecture/plugin-architecture.md",
    "docs-site/architecture/native-migration.md",
    "docs-site/ui-integration-guide.md",
    "docs-site/operations/upgrade-runbook.md",
]

# Files whose relative-link findings are ignored (empty = none).
# Changelog is generated from CHANGELOG.md with GitHub blob rewrites — not allowlisted.
SKIP_ESCAPE_FILES: set[str] = set()

# Anchors are validated with the renderer's own slugger (#802 Option B, where
# mkdocs.yml pins pymdownx.slugs.slugify so the site publishes GitHub ids).
try:
    from pymdownx.slugs import slugify as _pymdownx_slugify_factory

    _pymdownx_slugify = _pymdownx_slugify_factory(case="lower")
except ImportError:  # docs-lint's CI job installs only Python — fall back below.
    _pymdownx_slugify = None

INLINE_CODE_RE = re.compile(r"`([^`]*)`")
INLINE_LINK_RE = re.compile(r"!?\[([^\]]*)\]\([^)]*\)")
INLINE_HTML_RE = re.compile(r"</?[^>]+>")

# (heading markdown, expected id) pairs read from the built site. Guards the
# stdlib fallback against drifting from the renderer.
SLUG_CORPUS = {
    "Idle / zombie detection": "idle--zombie-detection",
    "VM — admin-only (no Settings API field)": "vm--admin-only-no-settings-api-field",
    "List API — Node recommendations": "list-api--node-recommendations",
    "6.1 Ingestion (`report_processor.go`)": "61-ingestion-report_processorgo",
    "1.0.0 Phase16 — 2026-07-02 — Phase 16: multi-GPU awareness and node GPU count":
        "100-phase16--2026-07-02--phase-16-multi-gpu-awareness-and-node-gpu-count",
}

HEADING_RE = re.compile(r"^(#{1,6})\s+(.*?)\s*$", re.M)
_ids_cache: dict[Path, set[str]] = {}


def rendered_heading(raw: str) -> str:
    """Text a renderer puts inside the heading: code spans and link labels only."""
    text = INLINE_CODE_RE.sub(r"\1", raw)
    text = INLINE_LINK_RE.sub(r"\1", text)
    return INLINE_HTML_RE.sub("", text)


def heading_slug(raw: str) -> str:
    text = rendered_heading(raw)
    if _pymdownx_slugify is not None:
        return _pymdownx_slugify(text, "-")
    out = re.sub(r"[^\w\- ]", "", text.strip()).lower()
    return out.replace(" ", "-")


def page_ids(md: Path) -> set[str]:
    """Ids the renderer publishes for a page, duplicate suffixes included."""
    cached = _ids_cache.get(md)
    if cached is not None:
        return cached
    ids: set[str] = set()
    seen: dict[str, int] = {}
    text = strip_fenced_code(md.read_text(encoding="utf-8", errors="replace"))
    for m in HEADING_RE.finditer(text):
        base = heading_slug(m.group(2))
        if base in seen:
            seen[base] += 1
            ids.add(f"{base}_{seen[base]}")
        else:
            seen[base] = 0
            ids.add(base)
    _ids_cache[md] = ids
    return ids


def fragment_page(src: Path, url: str) -> Path | None:
    """Markdown page a fragment belongs to, or None when out of scope."""
    if url.startswith("#"):
        return src
    path = unquote(urlparse(url).path)
    if not path or path.startswith("/"):
        return None
    base = src.resolve().parent if src.is_symlink() else src.parent
    target = (base / path).resolve()
    if target.is_dir():
        target = target / "index.md"
    if not target.suffix:
        target = Path(str(target) + ".md")
    try:
        target.relative_to(docs_site_root)
    except ValueError:
        return None  # symlinked out of the tree (generated changelog) or escaped
    return target if target.is_file() else None



def strip_fenced_code(text: str) -> str:
    out = []
    in_fence = False
    for line in text.splitlines(keepends=True):
        if FENCE_RE.match(line.strip() if False else line) or line.startswith("```"):
            in_fence = not in_fence
            out.append("\n" if line.endswith("\n") else "")
            continue
        if in_fence:
            out.append("\n" if line.endswith("\n") else "")
            continue
        out.append(line)
    return "".join(out)


def is_external(url: str) -> bool:
    u = url.strip()
    if not u or u.startswith("#"):
        return True
    if u.startswith(("http://", "https://", "mailto:", "tel:")):
        return True
    if "{{" in u:
        return True
    return False


def resolve_target(src: Path, url: str) -> Path | None:
    raw = url.strip().split()[0].strip("<>")
    if '"' in raw:
        raw = raw.split('"', 1)[0].strip()
    parsed = urlparse(raw)
    path = unquote(parsed.path)
    if not path:
        return None
    if path.startswith("/"):
        return None
    # Symlinks (e.g. docs-site/changelog.md → ../CHANGELOG.md): resolve
    # relative links from the real file's directory so repo paths like
    # docs/adr/... are escapes, not false "broken in-site" hits.
    base = src.resolve().parent if src.is_symlink() else src.parent
    return (base / path).resolve()


escaped: list[str] = []
escaped_skipped: list[str] = []
broken: list[str] = []

for md in sorted(docs_site.rglob("*.md")):
    rel = str(md)
    # Changelog is a symlink to root CHANGELOG.md — keep repo-relative docs/
    # links for GitHub; allowlist all relative-link findings on that file.
    skip_file = rel in SKIP_ESCAPE_FILES
    text = strip_fenced_code(md.read_text(encoding="utf-8", errors="replace"))
    for m in LINK_RE.finditer(text):
        url = m.group(2)
        if is_external(url):
            continue
        target = resolve_target(md, url)
        if target is None:
            continue
        try:
            target.relative_to(docs_site_root)
            in_site = True
        except ValueError:
            in_site = False
        if not in_site:
            line = f"{rel}: {url}"
            if skip_file:
                escaped_skipped.append(line)
            else:
                escaped.append(line)
            continue
        if target.is_dir():
            if (target / "index.md").is_file():
                continue
            if skip_file:
                escaped_skipped.append(f"{rel}: {url} (dir)")
            else:
                broken.append(f"{rel}: {url} (dir without index.md)")
            continue
        if target.is_file():
            continue
        if not target.suffix and Path(str(target) + ".md").is_file():
            continue
        try:
            missing = target.relative_to(Path.cwd())
        except ValueError:
            missing = target
        if skip_file:
            escaped_skipped.append(f"{rel}: {url} (missing {missing})")
        else:
            broken.append(f"{rel}: {url} (missing {missing})")

print("docs-lint: relative links in docs-site/ (fenced code skipped)")

# Fragment pass (#802): every in-page anchor must be an id the renderer
# publishes. Separate from the loop above because is_external() deliberately
# skips "#..." (same-page) links there.
anchors: list[str] = []
for md in sorted(docs_site.rglob("*.md")):
    rel = str(md)
    text = strip_fenced_code(md.read_text(encoding="utf-8", errors="replace"))
    for m in LINK_RE.finditer(text):
        url = m.group(2)
        if url.startswith("#"):
            page, frag = md.resolve(), url[1:]
        else:
            if is_external(url):
                continue
            page = fragment_page(md, url)
            if page is None:
                continue
            frag = urlparse(url).fragment
        if not frag:
            continue
        if unquote(frag) not in page_ids(page):
            anchors.append(
                f"{rel}: {url} (no heading #{unquote(frag)} in {page.relative_to(docs_site_root)})"
            )

slug_drift = [
    f"{markdown!r} -> {heading_slug(markdown)!r}, expected {expected!r}"
    for markdown, expected in SLUG_CORPUS.items()
    if heading_slug(markdown) != expected
]

# NISE command blocks (#825 follow-up). `-w` is `--write-monthly`, a store_true
# switch, so `-w /tmp/out` makes nise exit 2 with `unrecognized arguments`.
# Nothing executes these blocks — this static rule is the only thing that would
# have caught the 12 occurrences that shipped with a fabricated `--write-dir`
# flag row. Scans fenced blocks only; prose about the failure is not a command.
NISE_SWITCH_FLAGS = ("-w", "--write-monthly")
# Ends a command inside a fenced block: shell operators, redirects, comments.
NISE_COMMAND_END = {"#", ";", "&&", "||", "|", "&", ">", ">>", "<", "2>&1", "2>", "&>"}
# Verified against nise 5.4.2: this flag does not exist. The recipes page once
# documented it as the output-directory flag, which produced the broken commands.
NISE_NONEXISTENT_FLAGS = ("--write-dir", "--output-dir")


def scan_nise_block(md: Path, block_start: int, block: list[str]) -> list[str]:
    """Findings for one fenced block, or [] when it holds no nise command."""
    joined = " ".join(part.rstrip("\\") for part in block)
    tokens = joined.split()
    start = next(
        (
            index
            for index in range(len(tokens) - 1)
            if tokens[index] == "nise" and tokens[index + 1] == "report"
        ),
        None,
    )
    if start is None:
        return []
    # Only the command itself matters. A following comment, redirect or next
    # command (`--write-monthly &`, `--write-monthly` then `# 2. Package`) is
    # not a value for the flag — scanning past the command was the first
    # version's false-positive source.
    command: list[str] = []
    for token in tokens[start:]:
        if token in NISE_COMMAND_END or token.startswith("#"):
            break
        command.append(token)
    found: list[str] = []
    for index, token in enumerate(command):
        if token in NISE_NONEXISTENT_FLAGS:
            found.append(
                f"{md}: {token} does not exist in nise (block starting line {block_start})"
            )
        if token in NISE_SWITCH_FLAGS and index + 1 < len(command):
            value = command[index + 1]
            if not value.startswith("-"):
                found.append(
                    f"{md}: {token} is a switch and takes no value, got {value!r} "
                    f"(block starting line {block_start})"
                )
    return found


nise_flags: list[str] = []
for md in sorted(docs_site.rglob("*.md")):
    in_fence = False
    block_start = 0
    block: list[str] = []
    for number, line in enumerate(
        md.read_text(encoding="utf-8", errors="replace").splitlines(), 1
    ):
        if line.startswith("```"):
            if in_fence:
                nise_flags.extend(scan_nise_block(md, block_start, block))
                block = []
            else:
                block_start = number + 1
            in_fence = not in_fence
            continue
        if in_fence:
            block.append(line)

print(f"  nise switch-flag misuse:      {len(nise_flags)}")
print(f"  escaped (Pages 404 risk):     {len(escaped)}")
print(f"  escaped (allowlisted files):  {len(escaped_skipped)}  [{', '.join(sorted(SKIP_ESCAPE_FILES)) or 'none'}]")
print(f"  broken in-site targets:       {len(broken)}")
print(f"  broken in-page anchors:       {len(anchors)}")

def show(title: str, rows: list[str], limit: int = 40) -> None:
    if not rows:
        return
    print(f"\n{title} ({len(rows)}):")
    for line in rows[:limit]:
        print(f"  {line}")
    if len(rows) > limit:
        print(f"  ... and {len(rows) - limit} more")

show("ESCAPED — rewrite to GitHub blob/{{ git_branch }} or in-site path", escaped)
show("NISE flag misuse — see the recipes page flag table", nise_flags)
show("BROKEN in-site — fix path or remove", broken)
show("BROKEN anchors — point at an existing heading (ids follow GitHub's slug rule)", anchors)
show("SLUG DRIFT — check-docs-lint's fallback disagrees with the published rule", slug_drift)

missing_stamps = []
for rel in CLASS_A_STAMP_REQUIRED:
    p = Path(rel)
    if not p.is_file():
        missing_stamps.append(f"{rel} (file missing)")
        continue
    if not STAMP_RE.search(p.read_text(encoding="utf-8", errors="replace")):
        missing_stamps.append(rel)

if missing_stamps:
    print(f"\nClass A missing Last verified ({len(missing_stamps)}):")
    for line in missing_stamps:
        print(f"  {line}")
else:
    print("\ndocs-lint OK: Class A Last verified stamps present")

findings = len(escaped) + len(broken) + len(anchors) + len(slug_drift) + len(missing_stamps) + len(nise_flags)
if findings == 0:
    print("\ndocs-lint: all checks passed")
    sys.exit(0)

print(f"\ndocs-lint: {findings} finding(s) (plus {len(escaped_skipped)} allowlisted escapes)")
if soft:
    print("docs-lint: SOFT mode — exiting 0 (set DOCS_LINT_SOFT=0 for hard fail)")
    sys.exit(0)
print("docs-lint: HARD fail — fix findings or run with --soft / DOCS_LINT_SOFT=1", file=sys.stderr)
sys.exit(1)
PY
