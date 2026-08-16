package csv

// Package csv parses ROS container, namespace, storage, VM, cluster-quota, and
// snapshot-inventory CSVs into librobne daily digests, including in-memory node
// and GPU daily aggregation from container ROS rows, DailyNamespaceQuotaDigests /
// LatestNamespaceQuotaSnapshots / LatestNamespaceQuotaFromDaily from optional
// namespace quota columns, DailyClusterQuotaDigests /
// LatestClusterQuotaSnapshots / LatestClusterQuotaFromDaily from dedicated
// ClusterResourceQuota files, and LatestSnapshotInventory (hourly collapse)
// for VolumeSnapshot classification. The operator must not import this package
// (ADR-0305 / spec §0).
