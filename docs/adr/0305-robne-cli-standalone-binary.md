# ADR-0305: robne CLI as Standalone Binary Separate from ros-ocp-backend

## Status

Proposed

## Phase

Future (cross-repo: librobne + robne-cli + ros-ocp-backend)

## Context

The librobne library extraction (ADR-0303) creates a standalone Go module with
the pure recommendation engine. A robne CLI tool is planned (issue #99) for
offline/batch recommendation computation.

ros-ocp-backend already uses a multi-mode Cobra binary pattern (ADR-0129) with
subcommands: `api`, `processor`, `housekeeper`, `poller`. Two options exist: add
`robne-cli` as another subcommand in ros-ocp-backend, or create a separate
standalone binary.

## Decision

Create the robne CLI as a **separate standalone binary** in its own
repository/module, importing only librobne. Do not add it as a subcommand to
ros-ocp-backend's existing multi-mode binary.

### Rationale

1. **Import isolation.** ros-ocp-backend's binary transitively depends on Kafka
   (confluent-kafka-go), pgxpool, Echo (HTTP framework), Prometheus client,
   Unleash (feature flags), and the entire ingestion/plugin pipeline. Adding the
   CLI as a subcommand would pull all of these into the CLI binary — even though
   the CLI needs none of them. A standalone binary depending only on librobne
   (zero external dependencies beyond Go stdlib) produces a ~10 MB binary vs
   ~50+ MB.

2. **Configuration isolation.** ros-ocp-backend's startup reads 40+ environment
   variables (Kafka brokers, S3 endpoints, database credentials, feature flag
   URLs). A CLI user computing recommendations from local CSV files should not
   need to set `KAFKA_BOOTSTRAP_SERVERS` or `ROS_DB_HOST`. A standalone binary
   has its own minimal config (input path, output format, engine parameters).

3. **Distribution simplicity.** The CLI should be installable via
   `go install github.com/<org>/robne-cli@latest` — a single static binary with
   no runtime dependencies. Embedding it in ros-ocp-backend would require users
   to install the full service to get CLI access.

4. **Independent versioning.** The CLI can pin to a specific librobne version
   independently from ros-ocp-backend. This allows the CLI to ship stable
   releases on a different cadence from the service.

5. **Portability.** The CLI targets developer laptops, CI pipelines, and
   air-gapped environments. These contexts are fundamentally different from
   ros-ocp-backend's deployment model (Kubernetes pod with Kafka consumer,
   PostgreSQL, and S3).

## Consequences

### Positive

- CLI binary is small, self-contained, and easy to distribute.
- No configuration leakage from ros-ocp-backend.
- Independent release lifecycle.

### Negative

- A third repository/module to maintain (librobne, robne-cli, ros-ocp-backend).
- Shared input parsing logic (NISE CSV parsing) may need to be extracted into
  librobne or a shared utility module to avoid duplication between
  ros-ocp-backend's ingestion and the CLI's reader.

### Neutral

- The multi-mode Cobra pattern (ADR-0129) remains valid for ros-ocp-backend's
  internal modes — this decision does not change the existing binary's
  architecture.

## Alternatives Considered

### Add as subcommand to ros-ocp-backend

Rejected — import contamination, configuration contamination, binary size,
distribution complexity. The existing multi-mode binary is for server-side modes
that share infrastructure dependencies; the CLI shares only the engine.

### Add as subcommand to robne-operator

Rejected — the operator is a Kubernetes controller with controller-runtime
dependencies. Same import isolation problem.

### Ship as a Go plugin (.so)

Rejected — Go plugins have severe limitations (same Go version, same dependency
versions, Linux-only). A separate binary is simpler and more portable.

## References

- [ADR-0303](0303-library-extraction-librobne.md) — Library Extraction of the Native Engine (librobne)
- [ADR-0129](0129-multi-mode-cobra-binary.md) — Multi-mode Cobra binary
- [ADR-0277](0277-local-hybrid-on-cluster-engine-deferred-central-only-v1.md) — Local/hybrid on-cluster engine deferred
