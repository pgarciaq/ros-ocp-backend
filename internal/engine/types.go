package engine

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// DefaultCPUConfig returns the default CPU recommendation parameters.
func DefaultCPUConfig(now time.Time, decayHalfLifeHours float64) core.CPUConfig {
	return core.CPUConfigFromSizing(defaultContainerSizingThresholds, now, decayHalfLifeHours, "")
}

// DefaultMemoryConfig returns the default memory recommendation parameters.
func DefaultMemoryConfig(now time.Time, decayHalfLifeHours float64) core.MemoryConfig {
	return MemoryConfigFromSizing(defaultContainerSizingThresholds, now, decayHalfLifeHours, OOMConfig{}, "")
}

// MemoryConfigFromSizing builds MemoryConfig from resolved sizing thresholds.
// This wrapper adapts the OOMConfig type used in root engine to the core function signature.
func MemoryConfigFromSizing(th core.SizingThresholdSettings, now time.Time, decayHalfLifeHours float64, oom OOMConfig, profile string) core.MemoryConfig {
	return core.MemoryConfigFromSizing(th, now, decayHalfLifeHours, oom.BaseBump, oom.MaxBump, profile)
}
