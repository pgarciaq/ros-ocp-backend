package housekeeper

import (
	"context"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/correlate"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// RunCorrelator executes one thin-correlator cycle (#646): evaluate the last
// complete hour for every evidenced HC and write high-confidence advisories.
// Warn-and-continue posture: evidence problems are silence by design; only
// infrastructure errors propagate (the scheduler retries next hour).
func RunCorrelator(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	log := logging.GetLogger()
	select {
	case <-ctx.Done():
		log.Info("shutting down housekeeper gracefully before correlation")
		return nil
	default:
	}

	pool := db.GetPool()
	if pool == nil {
		log.Warn("hcp correlator: no database pool, skipping cycle")
		return nil
	}
	res, err := correlate.RunCycle(ctx, pool)
	if err != nil {
		return err
	}
	log.Infof("hcp correlator cycle: %d fired, %d silent", res.Fired, res.Silent)
	return nil
}
