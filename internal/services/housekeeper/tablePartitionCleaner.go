package housekeeper

import (
	"context"
	"fmt"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

func DeletePartitions(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	log := logging.GetLogger()
	cfg := config.GetConfig()

	select {
	case <-ctx.Done():
		log.Info("shutting down housekeeper gracefully before partition cleanup")
		return
	default:
	}

	db := database.GetDB()
	currentTime := time.Now().UTC()

	// subtracting $cfg.DataRetentionPeriod from the currentTime
	retentionThresholdDate := currentTime.AddDate(0, 0, -cfg.DataRetentionPeriod)

	// If the day of the month in $retentionThresholdDate is less than 15,
	// set $partitionTableDate to the 1st of the month.
	// Otherwise, set $partitionTableDate to the 16th of the previous month.
	var partitionTableDate string
	if retentionThresholdDate.Day() < 15 {
		partitionTableDate = retentionThresholdDate.AddDate(0, 0, -retentionThresholdDate.Day()+1).Format("2006-01-02")
	} else {
		partitionTableDate = time.Date(currentTime.Year(), currentTime.Month()-1, 16, 0, 0, 0, 0, currentTime.Location()).Format("2006-01-02")
	}

	tx := db.Exec("SELECT drop_ros_partition(?)", partitionTableDate)
	// drop_ros_partition only considers partitions of ROS-owned parent tables (see migration 000011).
	if tx.Error != nil {
		fmt.Println(tx.Error.Error())
	}

	if ctx.Err() != nil {
		log.Info("shutting down housekeeper gracefully")
		waitHousekeeperShutdownGrace(cfg.HousekeeperShutdownGraceSecs)
	}
}
