#!/usr/bin/env python3
"""Rewrite docs-site relative escapes/broken links for GitHub Pages (#419 backlog).

Prefer in-site equivalents when published; otherwise GitHub blob/{{ git_branch }}.
"""
from __future__ import annotations

import os
import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlparse

ROOT = Path(__file__).resolve().parents[1]
DOCS_SITE = ROOT / "docs-site"
GH = "https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}"
NISE_GH = "https://github.com/project-koku/nise/blob/main"
LINK_RE = re.compile(r"(?<!!)\[([^\]]*)\]\(([^)]+)\)")

BROKEN_MAP = {
    "../kruize-vs-native-comparison.md": f"{GH}/docs/kruize-vs-native-comparison.md",
    "../features-f27-pvc-rightsizing.md": "../features/pvc-rightsizing.md",
    "../features-f-snapshot-staleness.md": "../features/snapshot-staleness.md",
    "../business-hours-admin-guide.md": "../features/business-hours.md",
    "../operations/monitoring.md": "../monitoring.md",
    "../upgrade-runbook.md": "../operations/upgrade-runbook.md",
    "../operations/stale-detection.md": f"{GH}/docs/operations/stale-detection.md",
    "../operations/query-performance.md": "../query-performance.md",
    "../operations/gpu-catalog.md": f"{GH}/docs/operations/gpu-catalog.md",
    "architecture/adrs.md": "adrs.md",
    "../native-engine-performance.md": f"{GH}/docs/native-engine-performance.md",
    "490-issues.md": f"{GH}/docs/archive/490-issues.md",
    "../archive/performance-analysis.md": f"{GH}/docs/archive/performance-analysis.md",
    "../features-business-hours.md": "../features/business-hours.md",
    "../audits/adversarial-review.md": "../operations/adversarial-reviews.md",
    "namespace-boxplots-performance-analysis.md": f"{GH}/docs/archive/namespace-boxplots-performance-analysis.md",
    "../database/db-schema": f"{GH}/docs/database/db-schema",
    "operations/query-performance.md": "query-performance.md",
    "docs/adr/0324-vm-pvc-companion-csv-for-shared-storage-detection.md": f"{GH}/docs/adr/0324-vm-pvc-companion-csv-for-shared-storage-detection.md",
    "docs/adr/0182-monthly-savings-730-hours.md": f"{GH}/docs/adr/0182-monthly-savings-730-hours.md",
    "docs/adr/0326-calendar-accurate-monthly-hours.md": f"{GH}/docs/adr/0326-calendar-accurate-monthly-hours.md",
    "docs/operations/security-enforcement.md": f"{GH}/docs/operations/security-enforcement.md",
    "docs-site/plugin-reference/index.md": "plugin-reference/index.md",
    "scripts/check-docs-drift.sh": f"{GH}/scripts/check-docs-drift.sh",
    "scripts/check-docs-lint.sh": f"{GH}/scripts/check-docs-lint.sh",
    "docs/operations/gpu-catalog.md": f"{GH}/docs/operations/gpu-catalog.md",
}


def site_equiv(repo_rel: Path) -> str | None:
    s = repo_rel.as_posix()
    if s.startswith("docs/architecture/") and s.endswith(".md"):
        name = s[len("docs/architecture/") :]
        if (DOCS_SITE / "architecture" / name).is_file():
            return f"architecture/{name}"
    if s.startswith("docs/features/") and s.endswith(".md"):
        name = s[len("docs/features/") :]
        if (DOCS_SITE / "features" / name).is_file():
            return f"features/{name}"
    if s == "docs/features-business-hours.md":
        return "features/business-hours.md"
    if s.startswith("docs/features-f-snapshot"):
        return "features/snapshot-staleness.md"
    if s.startswith("docs/operations/") and s.endswith(".md"):
        name = s[len("docs/operations/") :]
        if (DOCS_SITE / name).is_file():
            return name
        if (DOCS_SITE / "operations" / name).is_file():
            return f"operations/{name}"
    if s == "openapi.json":
        return "openapi.md"
    return None


def strip_fenced_parts(text: str) -> list[tuple[bool, str]]:
    parts: list[tuple[bool, str]] = []
    in_fence = False
    buf: list[str] = []
    for line in text.splitlines(keepends=True):
        if line.startswith("```"):
            if buf:
                parts.append((in_fence, "".join(buf)))
                buf = []
            parts.append((True, line))
            in_fence = not in_fence
            continue
        buf.append(line)
    if buf:
        parts.append((in_fence, "".join(buf)))
    return parts


def rewrite_url(src: Path, url: str) -> str | None:
    raw = url.strip()
    title = ""
    if '"' in raw:
        idx = raw.find('"')
        pathpart, title = raw[:idx].strip(), raw[idx:]
        raw = pathpart
    if not raw or raw.startswith("#"):
        return None
    if raw.startswith(("http://", "https://", "mailto:", "tel:")):
        return None
    if "{{" in raw:
        return None

    parsed = urlparse(raw)
    path = unquote(parsed.path)
    frag = parsed.fragment

    for key in (raw, path):
        if key in BROKEN_MAP:
            repl = BROKEN_MAP[key]
            if frag and "#" not in repl:
                repl = f"{repl}#{frag}"
            return repl + ((" " + title) if title else "")

    if not path or path.startswith("/"):
        return None

    target = (src.parent / path).resolve()
    try:
        target.relative_to(DOCS_SITE.resolve())
        in_site = True
    except ValueError:
        in_site = False

    if in_site:
        if target.is_file() or (target.is_dir() and (target / "index.md").is_file()):
            return None
        if not target.suffix and Path(str(target) + ".md").is_file():
            return None
        return None

    nise_root = (ROOT.parent / "nise").resolve()
    try:
        nise_rel = target.relative_to(nise_root)
        new = f"{NISE_GH}/{nise_rel.as_posix()}"
        if frag:
            new += f"#{frag}"
        return new + ((" " + title) if title else "")
    except ValueError:
        pass

    try:
        repo_rel = target.relative_to(ROOT)
    except ValueError:
        return None

    equiv = site_equiv(repo_rel)
    if equiv:
        new = os.path.relpath(DOCS_SITE / equiv, src.parent).replace("\\", "/")
        if frag:
            new += f"#{frag}"
        return new + ((" " + title) if title else "")

    new = f"{GH}/{repo_rel.as_posix()}"
    if frag:
        new += f"#{frag}"
    return new + ((" " + title) if title else "")


def main() -> int:
    changed_files = 0
    replacements = 0
    for md in sorted(DOCS_SITE.rglob("*.md")):
        if md.is_symlink():
            print(f"skip symlink: {md.relative_to(ROOT)}")
            continue
        text = md.read_text(encoding="utf-8")
        parts = strip_fenced_parts(text)
        new_parts: list[str] = []
        file_reps = 0

        for is_code, chunk in parts:
            if is_code or chunk.startswith("```"):
                new_parts.append(chunk)
                continue

            def repl(m: re.Match[str], _src: Path = md) -> str:
                nonlocal file_reps
                label, url = m.group(1), m.group(2)
                new_url = rewrite_url(_src, url)
                if new_url is None or new_url == url:
                    return m.group(0)
                file_reps += 1
                return f"[{label}]({new_url})"

            new_parts.append(LINK_RE.sub(repl, chunk))

        if file_reps:
            md.write_text("".join(new_parts), encoding="utf-8")
            changed_files += 1
            replacements += file_reps
            print(f"{md.relative_to(ROOT)}: {file_reps} replacements")

    print(f"\nDone: {replacements} replacements in {changed_files} files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
