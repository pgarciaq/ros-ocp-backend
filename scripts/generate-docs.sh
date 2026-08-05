#!/usr/bin/env bash
set -euo pipefail

# generate-docs.sh — Assembles a few site pages that are sourced outside docs-site/
# before mkdocs build (CI and local).
#
# Source of truth:
#   - Most pages under docs-site/ are hand-maintained and committed (including
#     plugin-reference/, architecture/, features/, operations/).
#   - This script must NOT overwrite those curated trees.
#   - It only refreshes:
#       docs/known-issues.md  → docs-site/known-issues.md  (with link rewrites)
#       CONTRIBUTING.md       → docs-site/contributing.md  (with path rewrites)
#       docs-site/development.md stub when missing
#
# Optional (maintainers only — overwrites curated plugin-ref pages):
#   DOC_GENERATE_GOMARKDOC=1 ./scripts/generate-docs.sh
#   Writes gomarkdoc output for plugin.md / kruize.md / example.md. Prefer editing
#   the curated pages instead; see docs-site/plugin-reference/index.md.
#
# Usage:
#   ./scripts/generate-docs.sh
#   make docs-generate

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DOCS_DIR="$ROOT_DIR/docs-site"
PLUGIN_REF_DIR="$DOCS_DIR/plugin-reference"

export PATH="${PATH}:$(go env GOPATH)/bin"

# Rewrite hardcoded GitHub branch links to use the current branch at mkdocs build time.
rewrite_github_links() {
    local file="$1"
    sed -i \
        -e 's|https://github.com/RedHatInsights/ros-ocp-backend/blob/main/|https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/|g' \
        -e 's|https://github.com/RedHatInsights/ros-ocp-backend/tree/main/|https://github.com/pgarciaq/ros-ocp-backend/tree/{{ git_branch }}/|g' \
        -e 's|https://github.com/pgarciaq/ros-ocp-backend/blob/main/|https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/|g' \
        -e 's|https://github.com/pgarciaq/ros-ocp-backend/tree/main/|https://github.com/pgarciaq/ros-ocp-backend/tree/{{ git_branch }}/|g' \
        "$file"
}

if [ "${DOC_GENERATE_GOMARKDOC:-}" = "1" ]; then
    if ! command -v gomarkdoc &>/dev/null; then
        echo "Installing gomarkdoc..."
        go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest
    fi

    echo "WARNING: DOC_GENERATE_GOMARKDOC=1 — overwriting curated plugin-reference pages with gomarkdoc"
    mkdir -p "$PLUGIN_REF_DIR"

    gomarkdoc --output "$PLUGIN_REF_DIR/plugin.md" \
        --template-file file="$ROOT_DIR/scripts/docs-templates/package.gotxt" \
        ./internal/plugin/ 2>/dev/null || \
    gomarkdoc --output "$PLUGIN_REF_DIR/plugin.md" ./internal/plugin/

    for pkg in kruize example; do
        echo "  → internal/plugins/$pkg"
        gomarkdoc --output "$PLUGIN_REF_DIR/$pkg.md" "./internal/plugins/$pkg/" 2>/dev/null || \
        gomarkdoc --output "$PLUGIN_REF_DIR/$pkg.md" "./internal/plugins/$pkg/"
    done

    for generated in "$PLUGIN_REF_DIR/plugin.md" "$PLUGIN_REF_DIR/kruize.md" "$PLUGIN_REF_DIR/example.md"; do
        [ -f "$generated" ] && rewrite_github_links "$generated"
    done
else
    echo "Skipping gomarkdoc (plugin-reference is hand-maintained)."
    echo "  To regenerate dumps intentionally: DOC_GENERATE_GOMARKDOC=1 $0"
fi

echo "Assembling site content (known-issues, contributing)..."

# Do not copy docs/architecture or docs/operations over docs-site/ — those trees
# are curated and committed under docs-site/ (see #415–#417).

# Top-level docs — known-issues is authored under docs/ with links that point at
# docs-site/ via ../docs-site/... Rewrite those (and a few internal-only paths)
# so the published site resolves correctly. See #415.
if [ -f "$ROOT_DIR/docs/known-issues.md" ]; then
    sed -e 's|(../docs-site/|(|g' \
        -e 's|(design/seasonality-plugin.md)|(https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/design/seasonality-plugin.md)|g' \
        -e 's|(operations/runbooks.md#[^)]*)|(monitoring.md)|g' \
        -e 's|(operations/query-performance\.md)|(query-performance.md)|g' \
        -e 's|\./features-f27-pvc-rightsizing\.md|features/pvc-rightsizing.md|g' \
        -e 's|\./features-f-snapshot-staleness\.md|features/snapshot-staleness.md|g' \
        -e 's|\./features-f26-f33-f54-f55\.md|features/idle-detection.md|g' \
        -e 's|](\.\./internal/|](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/|g' \
        "$ROOT_DIR/docs/known-issues.md" > "$DOCS_DIR/known-issues.md"
fi
if [ -f "$ROOT_DIR/CONTRIBUTING.md" ]; then
    sed -e 's|(docs/architecture/|(architecture/|g' \
        -e 's|(docs/operations/|(https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/operations/|g' \
        -e 's|(docs-site/|(|g' \
        -e 's|(scripts/|(https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/scripts/|g' \
        -e 's|(openapi\.json)|(openapi.md)|g' \
        -e 's|(LICENSE)|(https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/LICENSE)|g' \
        "$ROOT_DIR/CONTRIBUTING.md" > "$DOCS_DIR/contributing.md"
fi

# Development guide stub when missing (hand-maintained file takes precedence)
if [ ! -f "$DOCS_DIR/development.md" ]; then
    cat > "$DOCS_DIR/development.md" << 'EOF'
# Local Development

See the [Contributing Guide](contributing.md) for full setup instructions.

## Quick Start

```bash
# Prerequisites
go install (see go.mod for version)
podman run -d --name ros-test-db -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:16

# Run
cp .env.example .env  # edit as needed
go run . start

# Test
go test ./...                    # unit tests (short)
go test -count=1 ./...           # all tests including integration
```

## Environment Variables

ROS-OCP uses [Viper](https://github.com/spf13/viper) for configuration.
Create a `.env` file at the repository root (see `.env.example`).

Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `ROS_DB_HOST` | `localhost` | PostgreSQL host |
| `ROS_DB_PORT` | `5432` | PostgreSQL port |
| `ROS_DB_NAME` | `ros_ocp` | Database name |
| `ROS_DB_USER` | `postgres` | Database user |
| `ROS_DB_PASSWORD` | `postgres` | Database password |
| `ROS_KAFKA_BROKERS` | `localhost:9092` | Kafka bootstrap servers |
| `ROS_ENABLED_PLUGINS` | (all) | Comma-separated plugin allowlist |
| `ROS_DISABLED_PLUGINS` | (none) | Comma-separated plugin denylist |

## Plugin Development

See the [Plugin Architecture](architecture/plugin-architecture.md) and
[example plugin](plugin-reference/example.md) for how to add new recommendation domains.
EOF
fi

echo "Done. Preview with: make docs-serve   (or: mkdocs serve --config-file mkdocs.yml)"
