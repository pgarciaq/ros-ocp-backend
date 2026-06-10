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
