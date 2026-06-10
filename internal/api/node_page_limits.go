package api

import "github.com/redhatinsights/ros-ocp-backend/internal/config"

// capNodeListLimit enforces ROS_API_MAX_NODE_RESULTS on node list endpoints.
func capNodeListLimit(limit int) int {
	max := config.GetConfig().APIMaxNodeResults
	if max <= 0 {
		max = 1000
	}
	if limit <= 0 {
		return max
	}
	if limit > max {
		return max
	}
	return limit
}
