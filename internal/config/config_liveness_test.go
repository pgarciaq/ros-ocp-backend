package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// #874: the liveness window is its own knob and must never inherit the engine
// window (ROS_MAX_LOOKBACK_DAYS). A zero or negative value must fall back to 14
// days rather than produce a cutoff in the future, which would mark every
// cluster stale in one sweep.
func TestClusterLivenessHoursDefaultAndFallback(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		ResetForTest()
		assert.Equal(t, 336, GetConfig().ClusterLivenessHours)
	})

	t.Run("configured", func(t *testing.T) {
		ResetForTest()
		t.Setenv("ROS_CLUSTER_LIVENESS_HOURS", "48")
		assert.Equal(t, 48, GetConfig().ClusterLivenessHours)
	})

	t.Run("zero falls back", func(t *testing.T) {
		ResetForTest()
		t.Setenv("ROS_CLUSTER_LIVENESS_HOURS", "0")
		assert.Equal(t, 336, GetConfig().ClusterLivenessHours)
	})

	t.Run("negative falls back", func(t *testing.T) {
		ResetForTest()
		t.Setenv("ROS_CLUSTER_LIVENESS_HOURS", "-5")
		assert.Equal(t, 336, GetConfig().ClusterLivenessHours)
	})

	t.Run("independent of max lookback", func(t *testing.T) {
		ResetForTest()
		t.Setenv("ROS_MAX_LOOKBACK_DAYS", "10")
		cfg := GetConfig()
		assert.Equal(t, 10, cfg.MaxLookbackDays)
		assert.Equal(t, 336, cfg.ClusterLivenessHours,
			"the engine window must not drive the liveness sweep")
	})
}
