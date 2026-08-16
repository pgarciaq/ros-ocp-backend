# ADR-0305: robne CLI as Standalone Binary Separate from ros-ocp-backend

## Status

Accepted (amended 2026-08-16)

## Phase

Current: in-tree `cmd/robne` (Phase 1+2a+container pgdigest INSERT/SELECT+namespace/node/gpu/pvc/vm/quota/cluster_quota stdout+2c other-entity rec upsert+other-entity digest INSERT+other-entity Path A SELECT shipped). Later: optional split to a `robne-cli` repo.

## Context

The librobne library extraction (ADR-0303) creates a nested Go module with the
pure recommendation engine. A robne CLI ([#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99))
computes recommendations from local files (use cases **a/b**) and can persist
into a database **this CLI owns** (use case **c**).

ros-ocp-backend already uses a multi-mode Cobra binary (ADR-0129): `api`,
`processor`, `housekeeper`, `poller`. Two options exist: add `robne` as another
subcommand of that binary, or keep a **separate** `robne` binary.

## Decision

`robne` is a **standalone binary**, not a `rosocp` / ros-ocp-backend subcommand
and not a robne-operator subcommand.

**Current delivery (2026-08-16):** `cmd/robne` in this repository (`make robne` →
`bin/robne`). Phase 1 stdout, Phase 2a `--output postgres://`, digest INSERT,
digest SELECT (`--input postgres://` recompute; files+`--output` SELECT after INSERT),
and **2b namespace + node/GPU + PVC + VM + quota + cluster_quota files → stdout** (`--plugins namespace|node|gpu|pvc|vm|quota|cluster_quota`, JSON v2–v8 sibling arrays; remaining 2b under [#472](https://github.com/pgarciaq/ros-ocp-backend/issues/472)
is none) shipped. **2c other-entity rec upsert** ([#473](https://github.com/pgarciaq/ros-ocp-backend/issues/473)) shipped. **Other-entity digest INSERT** ([#481](https://github.com/pgarciaq/ros-ocp-backend/issues/481)) shipped. **Other-entity Path A SELECT** ([#482](https://github.com/pgarciaq/ros-ocp-backend/issues/482)) shipped. **Next:** snapshot ([#478](https://github.com/pgarciaq/ros-ocp-backend/issues/478)). Phase 3 is [#480](https://github.com/pgarciaq/ros-ocp-backend/issues/480). It imports librobne **plus** optional I/O
packages (`librobne/csv`,
`librobne/pgrec`, `librobne/pgdigest`). `pgx` is used when the user passes a
`postgres://` URL on `--input` or `--output`. It must
**not** import Kafka, Echo, Unleash, `internal/engine` (the product god-package),
or the plugin `init()` registry.

**Later:** splitting `cmd/robne` into its own module/repo (`go install …/robne@latest`,
independent versioning) remains the end state. Plan the `go:embed` of `migrations/`
for that split; do not block **(c)** on the split.

**Never:** add `robne` as a subcommand of the multi-mode server binary.

### Rationale (unchanged)

1. **Import isolation.** The server binary transitively depends on Kafka, Echo,
   Prometheus, Unleash, and the ingestion/plugin pipeline. A subcommand would
   pull those into every CLI user’s download.
2. **Configuration isolation.** CLI users must not need `KAFKA_BOOTSTRAP_SERVERS`
   or `ROS_DB_HOST` for **(a)(b)**. **(c)** uses `PG*` / `--pg-url-file` only
   when they ask for Postgres.
3. **Distribution.** A single static binary (`CGO_ENABLED=0`, `pgx` is pure Go).
4. **Independent versioning** (after a repo split).
5. **Portability.** Laptops, CI, air-gap, pedestrian cron — not a Kubernetes
   Kafka consumer.

### What changed in the amendment

The first draft said “import only librobne, ~10 MB, separate repo on day one,
zero pgx.” That is the **(a)(b)** shape (files → stdout). Use case **(c)**
(pedestrian ROS) needs pgx, embedded product `migrations/`, and persist helpers.
Those stay **optional I/O packages**, not librobne core, so robne-operator
([#138](https://github.com/pgarciaq/ros-ocp-backend/issues/138)) never links them.

## Consequences

### Positive

- CLI is not contaminated by server startup or Kafka.
- **(a)(b)** remain a no-database path.
- **(c)** can own a dedicated Postgres without Helm or `rosocp db migrate`.

### Negative

- In-tree `cmd/robne` plus a later repo split is two delivery stages.
- `bin/robne` with **(c)** linked is larger than a stdout-only binary (one
  artifact, runtime mode, not two binaries).

### Neutral

- ADR-0129 still applies to server modes only.

## Alternatives Considered

### Add as subcommand to ros-ocp-backend

Rejected — import and configuration contamination.

### Add as subcommand to robne-operator

Rejected — controller-runtime contamination.

### Ship as a Go plugin (.so)

Rejected — same-toolchain / Linux-only.

### Two CLI binaries (`robne` vs `robne-pg`)

Rejected — one `robne`; Postgres is `--output postgres://` at runtime.

## References

- [ADR-0303](0303-library-extraction-librobne.md) — librobne
- [ADR-0129](0129-multi-mode-cobra-binary.md) — multi-mode Cobra binary
- [CLI spec](../plans/robne-cli-spec.md)
- [#99](https://github.com/pgarciaq/ros-ocp-backend/issues/99)
