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
