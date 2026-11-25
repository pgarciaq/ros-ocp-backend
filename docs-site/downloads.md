# Documentation PDF Books

> **Last verified:** 2026-11-25

Every top-level section of this site is also published as a PDF book, generated
from the same Markdown by `scripts/docs-pdf.sh` (Mermaid pre-rendered to PNG,
WeasyPrint print CSS). Downloads are GitHub Release assets: no login required,
and no PDF is committed to the repository.

| Book | PDF |
|------|-----|
| Getting Started | [getting-started.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/getting-started.pdf) |
| Features | [features.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/features.pdf) |
| Features (planned) | [planned-features.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/planned-features.pdf) |
| Architecture | [architecture.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/architecture.pdf) |
| Testing | [testing.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/testing.pdf) |
| Plugin Reference | [plugin-reference.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/plugin-reference.pdf) |
| API Specification | [api.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/api.pdf) |
| Operations | [operations.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/operations.pdf) |
| Security & Compliance | [security.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/security.pdf) |
| UI Integration | [ui-integration.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/ui-integration.pdf) |
| Historical | [historical.pdf](https://github.com/pgarciaq/ros-ocp-backend/releases/download/docs-pdf/historical.pdf) |

The `docs-pdf` release is rebuilt whenever `docs-site/` or `mkdocs.yml` changes;
its notes carry the commit and the build date. A link to a section that is not
part of the current book stays a dead link inside the PDF — the HTML site is the
cross-section reference. To build the books locally, see
[Contributing](contributing.md#generate-pdf-books).
