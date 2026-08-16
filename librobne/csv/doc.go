package csv

// Package csv parses ROS container, namespace, storage, and VM CSVs into librobne
// daily digests, including in-memory node and GPU daily aggregation from
// container ROS rows and LatestNamespaceQuotaSnapshots from optional namespace
// quota columns. The operator must not import this package (ADR-0305 / spec §0).
