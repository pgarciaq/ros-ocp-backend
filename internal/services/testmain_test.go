package services

import (
	"os"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("DEVELOPMENT", "true")
	_ = os.Setenv("ROS_CSV_DENY_PRIVATE_NETWORKS", "false")
	config.ResetForTest()
	_ = config.GetConfig()
	os.Exit(m.Run())
}
