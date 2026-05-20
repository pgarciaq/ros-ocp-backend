package model

import "strings"

// IsPartitionMissing checks whether a GORM/DB error indicates a missing partition.
// PostgreSQL raises "no partition of relation ... found for row" when INSERT targets
// a range-partitioned table without a matching partition. GORM surfaces this as a
// wrapped error whose message contains "no partition". This helper centralizes that
// detection so callers don't do ad-hoc string matching.
func IsPartitionMissing(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no partition")
}
