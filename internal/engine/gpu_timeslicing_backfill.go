package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// BackfillNodeGPUTimeslicingRecs recomputes and persists node GPU time-slicing
// recommendations for matching org/cluster pairs. When orgID is empty, all orgs
// in rh_accounts are processed. Returns counts of orgs and clusters processed.
func BackfillNodeGPUTimeslicingRecs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (orgsProcessed, clustersProcessed int, err error) {
	if pool == nil {
		return 0, 0, fmt.Errorf("database pool is nil")
	}
	if !plugin.EnabledFor("gpu") {
		return 0, 0, fmt.Errorf("gpu plugin is disabled")
	}

	orgIDs, err := listOrgIDsForGPUTimeslicingBackfill(ctx, pool, orgID)
	if err != nil {
		return 0, 0, err
	}
	if len(orgIDs) == 0 {
		return 0, 0, nil
	}

	for _, oid := range orgIDs {
		clusters, listErr := listClustersForGPUTimeslicingBackfill(ctx, pool, oid, clusterUUID)
		if listErr != nil {
			return orgsProcessed, clustersProcessed, listErr
		}
		if len(clusters) == 0 {
			continue
		}
		orgsProcessed++

		for _, cu := range clusters {
			if err := backfillGPUTimeslicingCluster(ctx, pool, oid, cu); err != nil {
				logging.ForOrg(oid, cu).Warnf("GPU time-slicing backfill failed: %v", err)
				continue
			}
			clustersProcessed++
		}
	}
	return orgsProcessed, clustersProcessed, nil
}

func backfillGPUTimeslicingCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	log := logging.ForOrg(orgID, clusterUUID)
	start, now := recalcDateRange()

	gpuTerms, err := LoadTermConfigCached(ctx, pool, orgID, "gpu")
	if err != nil {
		log.Warnf("GPU time-slicing backfill: load term config failed, using defaults: %v", err)
		gpuTerms = DefaultTermsForPlugin("gpu")
	}

	var costData *costdata.ClusterCostData
	if config.GetConfig().SavingsEstimatesEnabled {
		costData = fetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
	}

	if err := MarkContainersWithGPU(ctx, pool, orgID, clusterUUID); err != nil {
		log.Warnf("GPU time-slicing backfill: mark GPU containers failed: %v", err)
	}
	if err := StoreGPUClassifications(ctx, pool, orgID, clusterUUID, gpuTerms, costData); err != nil {
		return fmt.Errorf("store GPU classifications: %w", err)
	}
	if err := ComputeAndPersistNodeGPUTimeSlicingRecs(ctx, pool, orgID, clusterUUID, gpuTerms, costData); err != nil {
		return fmt.Errorf("persist node GPU time-slicing: %w", err)
	}
	log.Infof("GPU time-slicing backfill completed for cluster")
	return nil
}

func listOrgIDsForGPUTimeslicingBackfill(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]string, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID != "" {
		return []string{orgID}, nil
	}
	rows, err := pool.Query(ctx, `SELECT org_id FROM rh_accounts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list orgs for GPU time-slicing backfill: %w", err)
	}
	defer rows.Close()

	var orgs []string
	for rows.Next() {
		var oid string
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		orgs = append(orgs, oid)
	}
	return orgs, rows.Err()
}

func listClustersForGPUTimeslicingBackfill(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]string, error) {
	clusterUUID = strings.TrimSpace(clusterUUID)
	if clusterUUID != "" {
		return []string{clusterUUID}, nil
	}
	return ListClustersForOrg(ctx, pool, orgID)
}
