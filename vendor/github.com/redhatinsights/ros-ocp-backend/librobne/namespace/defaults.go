package namespace

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/engine"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// DefaultNamespaceSizingThresholds is the compiled namespace sizing set
// (container defaults with a higher memory-trend threshold).
func DefaultNamespaceSizingThresholds() types.SizingThresholdSettings {
	th := engine.DefaultContainerSizingThresholds()
	th.MemTrendSlopeThreshold = MemTrendSlopeThreshold
	return th
}

// DefaultNamespaceEngineConfig builds a pool-free config from compiled defaults.
// Product wrappers overlay tenant terms and thresholds.
func DefaultNamespaceEngineConfig(orgID, clusterUUID string, now, end time.Time) NamespaceEngineConfig {
	return NamespaceEngineConfig{
		OrgID:              orgID,
		ClusterUUID:        clusterUUID,
		End:                end,
		ScheduleType:       ScheduleAllHours,
		Terms:              engine.DefaultTerms(),
		Sizing:             DefaultNamespaceSizingThresholds(),
		Now:                now,
		StalenessThreshold: engine.DefaultStalenessThreshold,
	}
}
