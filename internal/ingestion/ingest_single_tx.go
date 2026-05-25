package ingestion

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

func commitIngestInSingleTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	samples []MetricRow,
	grouped map[DigestKey][]MetricRow,
	gpuAccum *gpuStreamAccumulator,
	nodeAccum map[NodeDayKey]*NodeDayAccumulator,
	scheduleCache *bhschedule.Cache,
	orgID, clusterUUID string,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ingest tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if len(samples) > 0 {
		if err := upsertUsageSamplesOnSender(ctx, tx, samples, orgID, clusterUUID); err != nil {
			return err
		}
	}
	if len(grouped) > 0 {
		if err := upsertContainerDigestsOnSender(ctx, tx, grouped, scheduleCache); err != nil {
			return err
		}
	}
	if gpuAccum != nil && len(gpuAccum.groups) > 0 {
		if err := flushGPUStreamGroupsOnSender(ctx, tx, gpuAccum.groups, clusterUUID); err != nil {
			return fmt.Errorf("GPU digest upsert: %w", err)
		}
	}
	if nodeAccum != nil && len(nodeAccum) > 0 {
		entries := make([]nodeDigestEntry, 0, len(nodeAccum))
		for k, acc := range nodeAccum {
			entries = append(entries, nodeDigestEntry{key: k, acc: acc})
		}
		cfg := config.GetConfig()
		if err := flushNodeDigestsOnSender(ctx, tx, entries, orgID, clusterUUID, cfg.NodeAllocatableFactor); err != nil {
			return fmt.Errorf("node digest upsert: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ingest tx: %w", err)
	}
	logging.ForOrg(orgID, clusterUUID).Infof("ProcessCSVToDigests: committed samples+digests+gpu+node in single tx (%d rows)", len(samples))
	return nil
}
