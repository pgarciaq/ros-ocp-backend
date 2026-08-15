package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	libcontainer "github.com/redhatinsights/ros-ocp-backend/librobne/container"
)

// Backward-compat aliases for replica optimization (canonical in librobne/container).

const (
	DefaultReplicaTargetUtilizationPct = libcontainer.DefaultReplicaTargetUtilizationPct
	MinReplicaTargetUtilizationPct     = libcontainer.MinReplicaTargetUtilizationPct
	MaxReplicaTargetUtilizationPct     = libcontainer.MaxReplicaTargetUtilizationPct
)

var (
	ComputeRecommendedReplicas        = libcontainer.ComputeRecommendedReplicas
	ReplicaReductionSavingsMicroCents = libcontainer.ReplicaReductionSavingsMicroCents
)

// DefaultReplicaTargetUtilizationPctFromConfig returns the effective target
// utilization percentage from the global config, falling back to the hardcoded
// default if the config is unavailable.
func DefaultReplicaTargetUtilizationPctFromConfig() int {
	cfg := config.GetConfig()
	if cfg != nil && cfg.ReplicaTargetUtilizationPct >= MinReplicaTargetUtilizationPct &&
		cfg.ReplicaTargetUtilizationPct <= MaxReplicaTargetUtilizationPct {
		return cfg.ReplicaTargetUtilizationPct
	}
	return DefaultReplicaTargetUtilizationPct
}
