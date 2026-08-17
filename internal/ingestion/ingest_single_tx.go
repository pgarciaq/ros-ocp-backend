package ingestion

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

func commitIngestInSingleTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	grouped map[DigestKey][]metricSample,
	gpuAccum *gpuStreamAccumulator,
	gpuBHAccum *gpuStreamAccumulator,
	nodeAccum map[NodeDayKey]*NodeDayAccumulator,
	nodeBHAccum map[NodeDayKey]*NodeDayAccumulator,
	scheduleCache *bhschedule.Cache,
	orgID, clusterUUID string,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ingest tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := db.SetLocalIngestStatementTimeout(ctx, tx); err != nil {
		return fmt.Errorf("set ingest statement timeout: %w", err)
	}

	if len(grouped) > 0 {
		if err := upsertContainerDigestsOnSender(ctx, tx, grouped, scheduleCache); err != nil {
			return err
		}
	}
	if gpuAccum != nil && len(gpuAccum.groups) > 0 {
		if err := flushGPUStreamGroupsOnSender(ctx, tx, gpuAccum.groups, clusterUUID, ScheduleTypeAllHours); err != nil {
			return fmt.Errorf("GPU digest upsert: %w", err)
		}
	}
	if gpuBHAccum != nil && len(gpuBHAccum.groups) > 0 {
		if err := flushGPUStreamGroupsOnSender(ctx, tx, gpuBHAccum.groups, clusterUUID, ScheduleTypeBusinessHours); err != nil {
			return fmt.Errorf("GPU business_hours digest upsert: %w", err)
		}
	}
	if nodeAccum != nil && len(nodeAccum) > 0 {
		entries := make([]nodeDigestEntry, 0, len(nodeAccum))
		for k, acc := range nodeAccum {
			entries = append(entries, nodeDigestEntry{key: k, acc: acc})
		}
		cfg := config.GetConfig()
		if err := flushNodeDigestsOnSender(ctx, tx, entries, orgID, clusterUUID, cfg.NodeAllocatableFactor, ScheduleTypeAllHours); err != nil {
			return fmt.Errorf("node digest upsert: %w", err)
		}
	}
	if nodeBHAccum != nil && len(nodeBHAccum) > 0 {
		entries := make([]nodeDigestEntry, 0, len(nodeBHAccum))
		for k, acc := range nodeBHAccum {
			entries = append(entries, nodeDigestEntry{key: k, acc: acc})
		}
		cfg := config.GetConfig()
		if err := flushNodeDigestsOnSender(ctx, tx, entries, orgID, clusterUUID, cfg.NodeAllocatableFactor, ScheduleTypeBusinessHours); err != nil {
			return fmt.Errorf("node business_hours digest upsert: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ingest tx: %w", err)
	}
	logging.ForOrg(orgID, clusterUUID).Infof("ProcessCSVToDigests: committed digests+gpu+node in single tx")
	return nil
}
