# ADR-0325: Use stdlib encoding/csv streaming over DataFrame library (go-gota)

## Status

Accepted

## Phase

Foundational (Ingestion)

## Context

The legacy Kruize ingestion path uses [go-gota](https://github.com/dreamsxin/gota)
(a Go DataFrame library inspired by pandas) to parse CSV files. The pattern is:

1. `csv.Reader.ReadAll()` — load entire file as `[][]string`
2. `dataframe.LoadRecords(data, WithTypes(...))` — materialize as typed Series columns
3. `df.GroupBy(...).Aggregation(...)` — whole-dataset group-by and aggregate
4. `df.Maps()` → `[]map[string]interface{}` — convert to generic maps for Kruize API

This approach requires the **entire dataset in memory** at least 3× over (raw strings,
DataFrame columns, filter/aggregate copies). For a 30-day CSV with 1000 containers at
96 samples/day, peak memory reaches 50–120 MB per file.

When designing the native engine's ingestion pipeline, the question arose: should we
use go-gota (or its `ScanCSV` streaming API added in v1.5+) or build a custom
streaming parser on stdlib `encoding/csv`?

### go-gota's ScanCSV (chunked materialization)

go-gota v1.5+ offers `dataframe.ScanCSV(reader, batchSize, callback)` which reads
N rows at a time into a DataFrame and passes each batch to a callback. This reduces
peak memory compared to `ReadCSV` (full file) but still materializes each chunk as
a full DataFrame with typed Series columns:

```go
dataframe.ScanCSV(f, 1000, func(batch dataframe.DataFrame) error {
    // batch is a full DataFrame with Series columns, NaN tracking, etc.
    return nil
}, dataframe.DetectDelimiter(true))
```

Each 1000-row batch allocates:
- N typed `Series` objects (float64/string backing slices with NaN sentinel handling)
- Element access via `interface{}` boxing
- Column lookup by string name (map access per cell)

### What the native engine actually needs

The native engine's ingestion computes statistical digests (percentiles, CVs, means)
incrementally — it groups rows by `DigestKey` and appends slim `metricSample` structs
(~96 bytes each, all int64) as they stream in. There is no operation that benefits
from having multiple rows accessible simultaneously as a columnar structure. The
group-by **is** the `map[DigestKey][]metricSample`, filled one row at a time.

## Decision

Use Go's stdlib `encoding/csv` with `ReuseRecord=true` for row-at-a-time streaming.
No DataFrame library is used in the native engine's ingestion path.

The implementation (`librobne/csv.ForEachRow` / `ForEachNamespace` / `ForEachPVC` / `ForEachVM` / `ForEachVMPVC` / `ForEachVMGPU` / `ForEachSnapshot` / `ForEachClusterQuota`, wrapped by
`internal/ingestion/csvparser.go:forEachCSVRow`,
`internal/ingestion/namespace.go:forEachNamespaceCSVRow`,
`internal/ingestion/pvc.go:forEachPVCRow`,
`internal/ingestion/vm_csv.go:forEachVMCSVRow`,
`internal/ingestion/vm_pvc_csv.go:forEachVMPVCCSVRow`,
`internal/ingestion/vm_gpu_device_csv.go:forEachVMGPUDeviceCSVRow`,
`internal/ingestion/snapshot.go:forEachSnapshotCSVRow`, and
`internal/ingestion/cluster_quota.go:forEachClusterQuotaCSVRow`) provides:

1. **Zero per-row allocation** — `ReuseRecord=true` reuses the `[]string` buffer
2. **Integer-position column access** — header parsed once into a `csvColumnIndex`
   struct; subsequent rows decoded by array index, not string name
3. **Immediate int64 conversion** — values converted to millicores/KiB at parse time
   (ADR-0098); no float64 intermediate survives past the parse boundary
4. **Direct callback** — each `MetricRow` is passed to the caller and discarded;
   only the slim `metricSample` is retained in the digest map
5. **Mid-file flush** — `ROS_INGEST_FLUSH_BATCH_SIZE` allows bounded memory even
   for extremely large CSVs (ADR-0091)

## Alternatives Considered

### go-gota ReadCSV (full file materialization)

The legacy approach. Loads entire file → DataFrame → GroupBy → Aggregation. Requires
3× file size in memory. Incompatible with bounded-memory streaming and integer-first
architecture (gota stores numerics as `float64`).

### go-gota ScanCSV (chunked materialization)

Reads N-row batches into DataFrame objects. Reduces peak memory versus `ReadCSV` but
still materializes each chunk as typed Series with:
- `interface{}` element boxing (allocation per access)
- `float64` numeric representation (violates ADR-0295 integer-first)
- Column access by string name (map lookup vs array index)
- Per-batch Series/DataFrame allocation overhead

After materializing each batch, the native engine would immediately destructure it
row-by-row to extract values into `metricSample` structs — making the DataFrame an
unnecessary intermediate step that adds allocation overhead without providing any
analytical capability the engine uses.

### go-gota ScanCSV with batch size 1

Even at batch=1, gota constructs a full DataFrame (Series objects, type detection,
NaN handling) for a single row. The overhead of Series construction exceeds the cost
of the native engine's direct `strconv.ParseInt` + struct assignment.

### Alternative CSV libraries (gocsv, csvutil)

Struct-tag-based CSV libraries add reflection overhead and enforce a fixed struct
shape. The native engine's `csvColumnIndex` approach handles optional/missing columns
gracefully (the operator adds new columns across versions per ADR-0265) without
struct tag changes or reflection.

## Consequences

- **Zero external dependencies for CSV parsing.** The native engine's ingestion adds
  no library weight. Removing the legacy Kruize path (BUILD-GOTA, issue #339) will
  eliminate go-gota and its transitive gonum dependency (~20 MB binary contribution).
- **Bounded memory.** Peak ingest memory is ~9 MB for 1000 container-days (vs 50–120
  MB with the DataFrame approach). Bounded by `ROS_INGEST_FLUSH_BATCH_SIZE`.
- **Integer-first from the parse boundary.** No float64 intermediate means no
  representation noise propagation (ADR-0295).
- **Column evolution tolerance.** Integer-position indexing with optional-column
  handling supports operator CSV schema evolution without code changes (ADR-0265).
- **No DataFrame analytics.** The engine cannot perform ad-hoc exploratory analysis
  on in-flight data. This is acceptable — the design is a streaming ETL pipeline,
  not an analytical workbench.
- **Custom code to maintain.** `forEachCSVRow` + `parseRecord` + `buildColumnIndex`
  is ~120 lines of purpose-built parsing code. This is small and stable — the CSV
  format changes only when the operator adds new columns.

## Related Decisions

| ADR | Relationship |
|-----|-------------|
| [ADR-0001](0001-native-engine-over-kruize.md) | High-level: native engine replaces Kruize |
| [ADR-0045](0045-daily-digest-tables-not-raw-metrics.md) | Digest output — why we aggregate at ingest |
| [ADR-0091](0091-incremental-digest-flush-streaming.md) | Mid-file flush for memory bounding |
| [ADR-0095](0095-csv-type-longest-prefix-first.md) | CSV type detection for routing to correct parser |
| [ADR-0098](0098-csv-float-to-int64-parse-time.md) | Float→int64 conversion at parse boundary |
| [ADR-0265](0265-operator-csv-column-contract-optional-columns-partial-upgrade.md) | Optional columns and partial-upgrade tolerance |
| [ADR-0295](0295-integer-first-architecture.md) | Integer-first representation (incompatible with gota's float64 Series) |

## References

- [`librobne/csv/parse.go`](../../librobne/csv/parse.go) — `ForEachRow` / `ParseRows` (one parse loop)
- [`librobne/csv/parse_namespace.go`](../../librobne/csv/parse_namespace.go) — `ForEachNamespace` / `ParseNamespaceRows` (one parse loop)
- [`librobne/csv/parse_pvc.go`](../../librobne/csv/parse_pvc.go) — `ForEachPVC` / `ParsePVCRows` (one parse loop)
- [`librobne/csv/parse_vm.go`](../../librobne/csv/parse_vm.go) — `ForEachVM` / `ParseVMRows` (one parse loop)
- [`librobne/csv/parse_vm_pvc.go`](../../librobne/csv/parse_vm_pvc.go) — `ForEachVMPVC` / `ParseVMPVCRows` (one parse loop)
- [`librobne/csv/parse_vm_gpu.go`](../../librobne/csv/parse_vm_gpu.go) — `ForEachVMGPU` / `ParseVMGPURows` (one parse loop)
- [`librobne/csv/parse_snapshot.go`](../../librobne/csv/parse_snapshot.go) — `ForEachSnapshot` / `ParseSnapshotRows` (one parse loop)
- [`librobne/csv/parse_cluster_quota.go`](../../librobne/csv/parse_cluster_quota.go) — `ForEachClusterQuota` / `ParseClusterQuotaRows` (one parse loop)
- [`internal/ingestion/csvparser.go`](../../internal/ingestion/csvparser.go) — container ingest wrapper (`forEachCSVRow`)
- [`internal/ingestion/namespace.go`](../../internal/ingestion/namespace.go) — namespace ingest wrapper (`forEachNamespaceCSVRow`)
- [`internal/ingestion/pvc.go`](../../internal/ingestion/pvc.go) — storage ingest wrapper (`forEachPVCRow`)
- [`internal/ingestion/vm_csv.go`](../../internal/ingestion/vm_csv.go) — VM usage ingest wrapper (`forEachVMCSVRow`)
- [`internal/ingestion/vm_pvc_csv.go`](../../internal/ingestion/vm_pvc_csv.go) — VM-PVC ingest wrapper (`forEachVMPVCCSVRow`)
- [`internal/ingestion/vm_gpu_device_csv.go`](../../internal/ingestion/vm_gpu_device_csv.go) — VM-GPU device ingest wrapper (`forEachVMGPUDeviceCSVRow`)
- [`internal/ingestion/snapshot.go`](../../internal/ingestion/snapshot.go) — snapshot inventory ingest wrapper (`forEachSnapshotCSVRow`)
- [`internal/ingestion/cluster_quota.go`](../../internal/ingestion/cluster_quota.go) — cluster-quota ingest wrapper (`forEachClusterQuotaCSVRow`)
- [`internal/utils/aggregator.go`](../../internal/utils/aggregator.go) — Legacy gota path (to be removed)
- [BUILD-GOTA issue #339](https://github.com/pgarciaq/ros-ocp-backend/issues/339) — Tech debt: remove go-gota when Kruize deprecated
- [go-gota ScanCSV](https://github.com/dreamsxin/gota) — Chunked DataFrame streaming (v1.5+)
- [Performance audit (July 2026)](../performance/ros-ocp-backend-audit-2026-07.md) — BUILD-GOTA finding context
