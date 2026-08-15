package engine

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/container"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const (
	defaultRecommendBatchSize = 500
	// DefaultStalenessThreshold is used when EngineConfig.StalenessThreshold is 0.
	DefaultStalenessThreshold = 48 * time.Hour
)

// DefaultEngineConfig builds a pool-free config from compiled defaults.
// Product wrappers overlay tenant terms, thresholds, and idle settings.
func DefaultEngineConfig(orgID, clusterUUID string, now time.Time) EngineConfig {
	return EngineConfig{
		OrgID:              orgID,
		ClusterUUID:        clusterUUID,
		Terms:              DefaultTerms(),
		Sizing:             DefaultContainerSizingThresholds(),
		Idle:               types.DefaultIdleConfig(),
		Now:                now,
		StalenessThreshold: DefaultStalenessThreshold,
		BatchSize:          defaultRecommendBatchSize,
	}
}

// DefaultTerms is the compiled 1/7/15-day term set with replica target 70%.
func DefaultTerms() []TermConfig {
	replicaPct := container.DefaultReplicaTargetUtilizationPct
	return []TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0, ReplicaTargetUtilizationPct: replicaPct},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168, ReplicaTargetUtilizationPct: replicaPct},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360, ReplicaTargetUtilizationPct: replicaPct},
	}
}

// DefaultContainerSizingThresholds matches the product compiled defaults.
func DefaultContainerSizingThresholds() SizingThresholdSettings {
	return SizingThresholdSettings{
		CPUCostPercentile:      0.60,
		CPUPerfPercentile:      0.98,
		MemCostPercentile:      0.95,
		MemPerfPercentile:      1.0,
		MinMargin:              1.15,
		MaxMargin:              1.50,
		LimitMultiplier:        1.05,
		CPUFloorMC:             25,
		MemFloorKiB:            4096,
		IdleCPUThresholdMC:     types.DefaultIdleThresholdMC,
		IdleMemThresholdKiB:    types.DefaultIdleThresholdMemKiB,
		MemTrendSlopeThreshold: 100.0,
		LowConfidenceThreshold: 0.5,
		SparseDataThreshold:    2,
	}
}
