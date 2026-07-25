package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

// ContainerRecBatchState tracks analytics degradation across streaming recommendation batches.
type ContainerRecBatchState struct {
	Degraded bool
}

// WriteContainerRecBatch persists a recommendation batch with configurable analytics ordering.
// When strictAnalytics is true, history and quality are written before recommendations; any
// analytics failure aborts the batch without writing recommendations.
// When strictAnalytics is false (default), recommendations are written first; analytics failures
// mark the batch state degraded and increment rosocp_analytics_incomplete_total.
func WriteContainerRecBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	batch []ContainerRec,
	oldRecs map[string]map[containerKey]OldRecommendation,
	costData *costdata.ClusterCostData,
	orgID, clusterUUID string,
	strictAnalytics bool,
	state *ContainerRecBatchState,
	onWriteError func(),
	hoursPerMonth int64,
) (int, error) {
	ApplySavingsEstimates(batch, costData, hoursPerMonth)

	if oldRecs != nil {
		adoptedKeys := FindAdoptedContainers(batch, oldRecs["cost"])
		if markErr := MarkAdopted(ctx, pool, orgID, clusterUUID, adoptedKeys); markErr != nil {
			log.Warnf("native engine: adoption marking incomplete: %v", markErr)
		}
	}

	writeAnalytics := func() error {
		if histErr := WriteContainerHistory(ctx, pool, batch, ""); histErr != nil {
			if strictAnalytics {
				return fmt.Errorf("writing recommendation history: %w", histErr)
			}
			log.WithFields(logrus.Fields{
				"org_id": orgID, "cluster_uuid": clusterUUID, "error_type": "history",
			}).Errorf("native engine: writing recommendation history failed: %v", histErr)
			state.Degraded = true
			metrics.IncAnalyticsIncomplete(orgID, clusterUUID, "history")
			return nil
		}
		if oldRecs != nil {
			oomCounts := OOMCountsByContainer(batch)
			if qualErr := WriteContainerQuality(ctx, pool, batch, oldRecs, oomCounts); qualErr != nil {
				if strictAnalytics {
					return fmt.Errorf("writing quality metrics: %w", qualErr)
				}
				log.WithFields(logrus.Fields{
					"org_id": orgID, "cluster_uuid": clusterUUID, "error_type": "quality",
				}).Errorf("native engine: writing quality metrics failed: %v", qualErr)
				state.Degraded = true
				metrics.IncAnalyticsIncomplete(orgID, clusterUUID, "quality")
			}
		}
		return nil
	}

	if strictAnalytics {
		if err := writeAnalytics(); err != nil {
			return 0, err
		}
		if writeErr := WriteRecommendations(ctx, pool, batch); writeErr != nil {
			if onWriteError != nil {
				onWriteError()
			}
			return 0, writeErr
		}
		return len(batch), nil
	}

	if writeErr := WriteRecommendations(ctx, pool, batch); writeErr != nil {
		if onWriteError != nil {
			onWriteError()
		}
		return 0, writeErr
	}
	if err := writeAnalytics(); err != nil {
		return 0, err
	}
	return len(batch), nil
}
