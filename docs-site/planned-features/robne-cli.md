# robne CLI — Standalone Offline/Batch Recommendations

!!! warning "Status: Planned / Future Work"
    This feature is **not yet implemented**. The description below is the intended
    product direction for a future release. All current recommendation features
    remain available today via the existing ros-ocp-backend pipeline and the
    planned robne-operator (see [Local Mode](local-mode.md)).

!!! info "Quick Facts (planned)"
    **Tool:** `robne` — standalone CLI binary  
    **Library:** librobne (issue #94) — same algorithms as ros-ocp-backend and robne-operator  
    **Input:** NISE CSVs, Prometheus JSON exports, PostgreSQL (`COPY TO`)  
    **Output:** JSON, CSV, table (terminal), PostgreSQL (`COPY FROM`)  
    **Infrastructure:** None — no Kafka, no API server, no ingestion pipeline

---

## What it does

The `robne` CLI reads metric data from local files or a database, computes
recommendations using librobne for all 9 entity types, and outputs results
in multiple formats. It is a zero-infrastructure tool for development,
testing, and offline analysis.

---

## Use cases

- **Development and testing:** compute recommendations locally without the full stack
- **CI/CD validation:** run against golden datasets to detect recommendation regressions
- **Offline analysis:** process exported Prometheus data from air-gapped clusters
- **Data exploration:** experiment with engine configurations (percentiles, margins, terms)
- **Bulk seeding:** load recommendations into PostgreSQL via `COPY FROM` for test/demo environments
- **Migration validation:** compare Kruize and native engine recommendations side-by-side

---

## Planned subcommands

| Subcommand | Purpose |
|------------|---------|
| `robne recommend` | Compute recommendations from input data |
| `robne diff` | Compare two recommendation sets (regression testing) |
| `robne explain` | Show detailed explanation factors for a specific container/workload |
| `robne validate` | Validate input data format without computing |

---

## Example usage

```bash
# Read NISE CSVs, output as JSON
robne recommend --input /path/to/nise-output/ --format json

# Custom configuration
robne recommend --input /path/to/csvs/ \
  --cpu-percentile 0.95 --memory-percentile 0.99 \
  --terms short,medium,long --engines cost,performance \
  --format table

# Write to PostgreSQL (bulk COPY FROM)
robne recommend --input /path/to/csvs/ \
  --output postgres://localhost:5432/ros?sslmode=disable

# Diff mode
robne diff --baseline recs-v1.json --candidate recs-v2.json --threshold 5%
```

---

## Input formats

| Format | Source | Bulk read |
|--------|--------|-----------|
| NISE CSV directory | koku-metrics-operator output format | File I/O |
| Prometheus JSON | `curl` against Prometheus API | File I/O |
| PostgreSQL | Existing digest tables | `COPY TO` |

## Output formats

| Format | Target | Bulk write |
|--------|--------|------------|
| JSON | Structured, machine-readable | File I/O |
| CSV | Spreadsheet analysis | File I/O |
| Table | Human-readable terminal output | stdout |
| PostgreSQL | `recommendation_sets` table | `COPY FROM` |

The CLI uses PostgreSQL `COPY FROM` for bulk writes (loading recommendations)
and `COPY TO` for bulk reads (extracting digest data or exporting
recommendations). This avoids per-row INSERT/SELECT overhead for large datasets.

---

## Architecture

```
Input Sources              CLI                          Output Targets
┌──────────────┐     ┌─────────────┐              ┌──────────────┐
│ NISE CSVs    │────▶│             │──────────────▶│ JSON / CSV   │
│ Prom export  │────▶│  robne CLI  │──────────────▶│ Table (tty)  │
│ PostgreSQL   │────▶│  (librobne) │──────────────▶│ PostgreSQL   │
└──────────────┘     └─────────────┘              └──────────────┘
```

---

## Entity type coverage

All 9 types supported by librobne:

- Container
- Node
- VM
- GPU (MIG + time-slicing)
- PVC
- Namespace quota
- Cluster quota
- Snapshot

Each entity type is selectively enabled/disabled via CLI flags.

---

## Phasing

| Phase | Scope |
|-------|-------|
| **Phase 1** | Container recommendations from NISE CSVs → JSON/CSV/table |
| **Phase 2** | All entity types, PostgreSQL input/output, Prometheus export input |
| **Phase 3** | Diff mode, explain mode, CI integration helpers |

---

## Relationship to other planned features

- **Depends on** librobne (ADR-0303, issue #94)
- **Standalone binary**, not a subcommand of ros-ocp-backend (ADR-0305)
- **Complements [Local Mode](local-mode.md)** — CLI = offline/batch; operator = real-time on-cluster
- **Complements ros-ocp-backend** — CLI = no infrastructure; backend = full pipeline

---

## Prerequisites

- librobne library extraction (issue #94)
- NISE CSV parsing logic (adaptable from ros-ocp-backend's `internal/ingestion/`)

---

## Limitations (planned)

- No real-time collection — processes static data snapshots
- No API server — use ros-ocp-backend or robne-operator for API access
- No cost/savings estimation without cost rates (accepts rates as CLI flags or skips savings)
- No tag enrichment or cloud tag correlation (central-only features)
