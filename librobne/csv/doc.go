package csv

// Package csv parses ROS container, namespace, storage, VM, and cluster-quota
// CSVs into librobne daily digests, including in-memory node and GPU daily
// aggregation from container ROS rows, LatestNamespaceQuotaSnapshots from
// optional namespace quota columns, and LatestClusterQuotaSnapshots from
// dedicated ClusterResourceQuota files. The operator must not import this
// package (ADR-0305 / spec §0).
