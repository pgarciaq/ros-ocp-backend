package services

import (
	"os"
	"testing"

	"go.uber.org/goleak"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("DEVELOPMENT", "true")
	_ = os.Setenv("ROS_CSV_DENY_PRIVATE_NETWORKS", "false")
	config.ResetForTest()
	_ = config.GetConfig()
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck"),
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"),
		goleak.IgnoreTopFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
		goleak.IgnoreTopFunction("github.com/hashicorp/golang-lru/v2/expirable.NewLRU[...].func1"),
	)
}
