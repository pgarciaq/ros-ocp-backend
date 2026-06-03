#!/usr/bin/env bash
set -euo pipefail

# generate-docs.sh — Generates plugin API reference markdown from Go source
# code using gomarkdoc. Run locally or in CI before mkdocs build.
#
# Prerequisites:
#   go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest
#
# Usage:
#   ./scripts/generate-docs.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DOCS_DIR="$ROOT_DIR/docs-site"
PLUGIN_REF_DIR="$DOCS_DIR/plugin-reference"

export PATH="${PATH}:$(go env GOPATH)/bin"

if ! command -v gomarkdoc &>/dev/null; then
    echo "Installing gomarkdoc..."
    go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest
fi

echo "Generating plugin API reference..."

mkdir -p "$PLUGIN_REF_DIR"

# Generate docs for the plugin interfaces package
gomarkdoc --output "$PLUGIN_REF_DIR/plugin.md" \
    --template-file file="$ROOT_DIR/scripts/docs-templates/package.gotxt" \
    ./internal/plugin/ 2>/dev/null || \
gomarkdoc --output "$PLUGIN_REF_DIR/plugin.md" ./internal/plugin/

# Generate docs for each plugin package. Hand-written stubs (API/settings/filters):
# container, node, pvc, quota, cluster-quota, gpu, namespace, snapshot, vm — see docs-site/plugin-reference/
for pkg in kruize example; do
    echo "  → internal/plugins/$pkg"
    gomarkdoc --output "$PLUGIN_REF_DIR/$pkg.md" "./internal/plugins/$pkg/" 2>/dev/null || \
    gomarkdoc --output "$PLUGIN_REF_DIR/$pkg.md" "./internal/plugins/$pkg/"
done

# Copy static docs into the site directory structure
echo "Assembling site content..."

# Architecture docs
mkdir -p "$DOCS_DIR/architecture"
for f in "$ROOT_DIR/docs/architecture/"*.md; do
    # Operator-facing notification catalog is maintained in docs-site/ (not copied from docs/).
    [ "$(basename "$f")" = "notification-codes.md" ] && continue
    [ -f "$f" ] && cp "$f" "$DOCS_DIR/architecture/"
done

# Operations docs
mkdir -p "$DOCS_DIR/operations"
[ -f "$ROOT_DIR/docs/upgrade-runbook.md" ] && cp "$ROOT_DIR/docs/upgrade-runbook.md" "$DOCS_DIR/operations/upgrade-runbook.md"

# Feature docs — container-recommendations, pvc-rightsizing, business-hours, gpu-time-slicing,
# snapshot-staleness, and cluster-resource-quota are maintained in docs-site/features/
# (not copied from docs/)
mkdir -p "$DOCS_DIR/features"

# Top-level docs
[ -f "$ROOT_DIR/docs/known-issues.md" ] && cp "$ROOT_DIR/docs/known-issues.md" "$DOCS_DIR/known-issues.md"
if [ -f "$ROOT_DIR/CONTRIBUTING.md" ]; then
    sed -e 's|(docs/architecture/|(architecture/|g' \
        -e 's|(openapi\.json)|(openapi.md)|g' \
        -e 's|(LICENSE)|(https://github.com/pgarciaq/ros-ocp-backend/blob/main/LICENSE)|g' \
        "$ROOT_DIR/CONTRIBUTING.md" > "$DOCS_DIR/contributing.md"
fi

# Development guide (extract from CONTRIBUTING or create stub)
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

echo "Done. Run 'mkdocs serve' from the repo root to preview."
