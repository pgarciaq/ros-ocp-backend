package engine

import "github.com/redhatinsights/ros-ocp-backend/internal/engine/container"

// Backward-compat aliases for replica optimization (canonical in container/).

const (
	DefaultReplicaTargetUtilizationPct = container.DefaultReplicaTargetUtilizationPct
	MinReplicaTargetUtilizationPct     = container.MinReplicaTargetUtilizationPct
	MaxReplicaTargetUtilizationPct     = container.MaxReplicaTargetUtilizationPct
)

var (
	DefaultReplicaTargetUtilizationPctFromConfig = container.DefaultReplicaTargetUtilizationPctFromConfig
	ComputeRecommendedReplicas                   = container.ComputeRecommendedReplicas
	ReplicaReductionSavingsMicroCents            = container.ReplicaReductionSavingsMicroCents
)
