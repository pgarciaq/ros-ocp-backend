# Separating Ingestion and Recommendation Processing

!!! warning "Status: Under consideration"
    This is an idea being evaluated, not a shipped feature or a committed delivery.
    There is no release date, and the design may change or not proceed.

## Current behavior

Koku sends ROS references to individual CSV files in object storage. In the
default native mode, the ROS processor downloads those files, parses them and
persists digests, then runs native recommendation generation as part of handling
the upload event. Recommendation engines wait until the expected files for a
manifest have been ingested.

See [Validating the Native Engine](../testing/validating-native-engine.md) for
the current end-to-end data flow.

## What is being considered

The proposal is to keep CSV download, parsing, and digest persistence together,
but separate that work from recommendation generation:

```text
Current:
  download CSVs → parse and persist digests → generate recommendations

Under consideration:
  download CSVs → parse and persist digests → durable handoff
                                          → separate worker generates recommendations
```

The goal is to reduce the time recommendation computation occupies the upload
processor and to allow ingestion and recommendation capacity to be managed
separately. This would not change the CSV format, recommendation algorithms, or
API response shape.

If adopted, recommendations for newly ingested data could become available
asynchronously: digests may be persisted before the corresponding
recommendations are refreshed. Moving computation to a separate worker would
not, by itself, eliminate PostgreSQL contention or guarantee a throughput gain;
those effects would need to be measured.

## What this does not mean

- ROS is **not** changing its current processing behavior today.
- The proposal is not to store raw CSV files as PostgreSQL blobs.
- CSV download and digest calculation are not being proposed as separate stages
  from each other.
- No customer configuration or action is required while this remains under
  consideration.

Follow [issue #708](https://github.com/pgarciaq/ros-ocp-backend/issues/708) for
discussion. Issue status does not imply a delivery commitment.
