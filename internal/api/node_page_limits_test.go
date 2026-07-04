package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestCapNodeListLimit(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_API_MAX_NODE_RESULTS", "1000")

	assert.Equal(t, 1000, capNodeListLimit(0))
	assert.Equal(t, 50, capNodeListLimit(50))
	assert.Equal(t, 1000, capNodeListLimit(5000))
}

func TestFleetHeatmapMaxNodes_Default(t *testing.T) {
	config.ResetForTest()
	cfg := config.GetConfig()
	assert.Equal(t, 1000, cfg.FleetHeatmapMaxNodes)
}

func TestFleetHeatmapMaxNodes_Custom(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_FLEET_HEATMAP_MAX_NODES", "500")
	cfg := config.GetConfig()
	assert.Equal(t, 500, cfg.FleetHeatmapMaxNodes)
}

func TestRateLimitConfig_Defaults(t *testing.T) {
	config.ResetForTest()
	cfg := config.GetConfig()
	assert.False(t, cfg.RateLimitEnabled)
	assert.Equal(t, 60, cfg.RateLimitRPM)
	assert.Equal(t, 30, cfg.RateLimitBurst)
}

func TestRateLimitConfig_CustomValues(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_API_RATE_LIMIT_ENABLED", "true")
	t.Setenv("ROS_API_RATE_LIMIT_RPM", "120")
	t.Setenv("ROS_API_RATE_LIMIT_BURST", "50")
	cfg := config.GetConfig()
	assert.True(t, cfg.RateLimitEnabled)
	assert.Equal(t, 120, cfg.RateLimitRPM)
	assert.Equal(t, 50, cfg.RateLimitBurst)
}

func TestHTTPTimeoutConfig_Defaults(t *testing.T) {
	config.ResetForTest()
	cfg := config.GetConfig()
	assert.Equal(t, 60, cfg.ReadTimeout)
	assert.Equal(t, 120, cfg.WriteTimeout)
	assert.Equal(t, 120, cfg.IdleTimeout)
}

func TestHTTPTimeoutConfig_CustomValues(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_API_READ_TIMEOUT", "30")
	t.Setenv("ROS_API_WRITE_TIMEOUT", "90")
	t.Setenv("ROS_API_IDLE_TIMEOUT", "180")
	cfg := config.GetConfig()
	assert.Equal(t, 30, cfg.ReadTimeout)
	assert.Equal(t, 90, cfg.WriteTimeout)
	assert.Equal(t, 180, cfg.IdleTimeout)
}
