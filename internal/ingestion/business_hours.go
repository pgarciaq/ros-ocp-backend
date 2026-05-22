package ingestion

import "github.com/redhatinsights/ros-ocp-backend/internal/config"

// BusinessHoursAggregationEnabled reports whether ingestion should produce business_hours
// digest streams in addition to all_hours. When false, only all_hours digests are written.
func BusinessHoursAggregationEnabled() bool {
	return config.BusinessHoursFeatureEnabled()
}
