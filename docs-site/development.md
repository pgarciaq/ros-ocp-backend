# Local Development

> **Last verified:** 2026-08-05

This guide covers day-to-day development for **ros-ocp-backend** (ROBNE — the ROS-OCP
Backend Native Engine). For contribution workflow, PR expectations, and architecture
background, see the [Contributing Guide](contributing.md). For the full test inventory
and NISE filename conventions, see [Testing & Quality](testing.md).

## Documentation site and PDF books

```bash
make docs-install            # HTML site deps
make docs-serve              # http://localhost:8000
make docs-pdf-install        # WeasyPrint + mkdocs-to-pdf
make docs-pdf-all            # all nav sections → dist/pdf/*.pdf
make docs-pdf-features       # single section (also architecture, operations, historical, …)
```

PDFs are local-only (gitignored). See
[Contributing — Generate PDF books](contributing.md#generate-pdf-books-local-only)
for sections, Mermaid/`mmdc`, Chrome, print-CSS rules, and known limitations
(#381 / #382).

## Prerequisites

| Tool | Version / notes |
|------|-----------------|
| **Go** | See `go.mod` (currently Go 1.25+) |
| **PostgreSQL** | 16 — local install, Docker, or `scripts/docker-compose.yml` (`db-ros` on port **15432**) |
| **Docker or Podman** | Required for integration tests ([Testcontainers](https://golang.testcontainers.org/)) |
| **Kafka** | Optional for full pipeline; provided by `scripts/docker-compose.yml` |
| **golang-migrate** | Via `go run rosocp.go db migrate` (no separate install required) |

## Quick start

```bash
# Clone and configure
git clone https://github.com/pgarciaq/ros-ocp-backend.git
cd ros-ocp-backend
```

Published docs: https://pgarciaq.github.io/ros-ocp-backend/ .
librobne import path stays `github.com/redhatinsights/ros-ocp-backend/librobne` —
[Integrating librobne](architecture/librobne.md).

```bash
cp .env.example .env   # edit DB/Kafka ports if needed

# Infrastructure (Kafka + PostgreSQL + topics)
docker compose -f scripts/docker-compose.yml up -d kafka db-ros kafka-create-topics

# Database migrations
go run rosocp.go db migrate up

# Terminal 1 — API
PROMETHEUS_PORT=5007 go run rosocp.go start api

# Terminal 2 — processor (consumes Kafka, ingests CSVs)
PROMETHEUS_PORT=5005 go run rosocp.go start processor

# Terminal 3 — recommendation poller
PROMETHEUS_PORT=5006 go run rosocp.go start recommendation-poller
```

Makefile shortcuts: `make db-migrate`, `make run-api-server`, `make run-processor`,
`make run-recommendation-poller`, `make build`, `make robne`, `make build-all`, `make test`.

For a guided first run with NISE data and API queries, see the
[Quick Start Tutorial](quickstart.md).

## Configuration

ROS-OCP uses [Viper](https://github.com/spf13/viper) with [godotenv](https://github.com/joho/godotenv).
Copy `.env.example` to `.env` and uncomment only what you need.

**Precedence:** shell env vars → `.env` → defaults in `internal/config/config.go`.

| Variable | Default (local) | Description |
|----------|-------------------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `15432` | PostgreSQL port (`db-ros` in compose) |
| `DB_NAME` | `postgres` | Database name |
| `DB_USER` / `DB_PASSWORD` | `postgres` | Credentials |
| `DB_SSL` | `disable` | TLS mode |
| `KAFKA_BOOTSTRAP_SERVERS` | `localhost:29092` | Kafka brokers |
| `KAFKA_CONSUMER_GROUP_ID` | `ros-ocp` | Consumer group |
| `UPLOAD_TOPIC` | `hccm.ros.events` | Upload events (processor) |
| `API_PORT` | `8000` | REST API port |
| `LOG_LEVEL` | `INFO` | Log level |
| `RBAC_ENABLE` | `false` (local) | API authorization |
| `ROS_ENABLED_PLUGINS` | (all native) | Comma-separated allowlist |
| `ROS_DISABLED_PLUGINS` | (none) | Comma-separated denylist |

See [Configuration Reference](configuration.md) for performance tuning, plugin env vars,
GPU/PVC/snapshot thresholds, and tag-sync settings.

## Running tests

```bash
# Unit + integration (Docker required for testcontainers)
go test ./internal/... -v

# Full suite (serial packages, race detector — matches CI)
make test

# Benchmarks
go test -bench=. -run='^$' ./internal/engine/

# Specific package
go test ./internal/utils/ -run TestDetermineCSVType -v
```

Details: [Testing & Quality](testing.md) — markers, E2E in cost-onprem-chart, IQE profiles.

## Test data generation

Use [NISE](https://github.com/project-koku/nise) to produce operator-compatible CSVs.
Filename patterns must match `DetermineCSVType()` (prefix rules + `Contains` fallback
for `--write-monthly` names). See [Test Data Generation](testing.md#test-data-generation-nise).

**Critical:** Upload tarballs must include `manifest.json` with non-empty **`start`**
and **`end`** fields so Koku can populate summary tables. Hand-crafted manifests that
omit these fields cause silent empty cost summaries. ROBNE validates `start`/`end`
when a Kafka message includes optional `manifest` metadata.

## Common workflows

### Adding a plugin

Use the scaffolder for steps 1–5, then finish migrations / OpenAPI / enablement by hand:

```bash
make new-plugin NAME=myplugin
# optional: PHASE=enrich PRIORITY=40 TRAITS=csv,terms DRY_RUN=1
# equivalent: go run ./cmd/newplugin -name myplugin
```

Defaults create a compiling stub under `internal/plugins/<name>/` with live
**Plugin** + **APIProvider** + **RetentionProvider**, other traits **commented**
for discovery, a blank import in [`plugins.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/plugins.go)
(sorted), and a checklist. Hyphenated names are allowed (`cluster-quota`); the
Go package name strips hyphens.

Manual checklist (also printed by the tool):

1. Copy `internal/plugins/example/` only if you prefer hand-editing the template
   (see [`example/README.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/example/README.md)).
2. Set `Name()` / use `plugin.EnabledFor` (scaffolder already does this).
3. Set `Phase()` / `Priority()` as needed (defaults via `plugin.BasePlugin`:
   Phase Produce, priority 50). Execution order is **phase → priority (lower
   first) → name**. See [Plugin Execution Phases](architecture/plugin-phases.md).
4. Implement / uncomment **trait interfaces** you need. Start with the
   [Plugin Traits](architecture/plugin-traits.md) (role / when to use / who
   implements). Go signatures: [Plugin Architecture §4](architecture/plugin-architecture.md#4-plugin-interfaces-trait-based).
   Checklist matrix: [plugin-reference overview](plugin-reference/index.md#trait-matrix).
5. Blank import belongs in [`internal/plugins/plugins.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/plugins.go)
   — do **not** edit `internal/plugin/registry.go` for the plugin list.
6. Add migrations under `migrations/` with a `-- plugin: <name>` header when you
   own new tables.
7. Register HTTP routes via `RegisterRoutes` and document them in `openapi.json`
   (`x-plugin-required` so disabled plugins are filtered from `/openapi.json`).
8. Enable locally when using an allowlist:
   `ROS_ENABLED_PLUGINS=container,gpu,...,<name>`.

> **Tooling:** `make new-plugin` / `go run ./cmd/newplugin` — see [#410](https://github.com/pgarciaq/ros-ocp-backend/issues/410).

### Adding an API endpoint

1. Handler in `internal/api/` (use `requireXRHID`, tenant-scoped DB via `db.GetPool()`).
2. Register route in plugin `RegisterRoutes` or `internal/api/server.go`.
3. Update `openapi.json` and contract tests.
4. Document in `docs-site/plugin-reference/` or feature pages.

### Adding a migration

```bash
go run rosocp.go db migrate create -ext sql -dir migrations -seq add_my_table
go run rosocp.go db migrate up
```

Partitioned tables follow patterns in existing migrations; see
[Contributing — Database](contributing.md).

## Debugging tips

- **Processor not ingesting:** Check Kafka topic, `UPLOAD_TOPIC`, and processor logs.
  Use `make upload-msg-to-rosocp` with compose `nginx` serving sample CSVs on port 8888.
- **All CSVs treated as container:** Filename missing `ocp_ros_namespace`, `ocp_storage_usage`, etc.
  Run `go test ./internal/utils/ -run NiseMonthly`.
- **Recommendations empty:** Confirm digests exist for the cluster; check poller logs and
  `ROS_ENABLED_PLUGINS`.
- **API 403:** Set `RBAC_ENABLE=false` locally or pass a valid `x-rh-identity` header.
- **Integration tests hang:** Ensure Docker is running; use `make test` (`-p=1`) to avoid
  testcontainer starvation.
- **DB connection refused:** `DB_PORT=15432` for compose `db-ros`, not 5432.

Structured logs use logrus; search by `org_id`, `cluster_uuid`, and `request_id` from Kafka messages.

## Plugin development

See [Plugin Architecture](architecture/plugin-architecture.md) and the
[example plugin](plugin-reference/example.md) template.

## Related documentation

| Document | Purpose |
|----------|---------|
| [Contributing](contributing.md) | PR workflow, architecture, code style |
| [Testing](testing.md) | Test layers, NISE, manifest structure |
| [Quick Start Tutorial](quickstart.md) | End-to-end first run |
| [Configuration](configuration.md) | All environment variables |
