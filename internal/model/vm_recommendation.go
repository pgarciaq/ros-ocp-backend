package model

import "github.com/redhatinsights/ros-ocp-backend/internal/model/types"

// Re-export VM recommendation types from the lightweight types sub-package.
type VMRecommendation = types.VMRecommendation
type VMRecommendationStatus = types.VMRecommendationStatus

const (
	VMStatusActive    = types.VMStatusActive
	VMStatusIdle      = types.VMStatusIdle
	VMStatusOversized = types.VMStatusOversized
)
