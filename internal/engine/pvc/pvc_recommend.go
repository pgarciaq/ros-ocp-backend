package pvc

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	libpvc "github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
)

// RecommendPVCs reads PVC digest data and produces per-term recommendations.
// notifThresholds controls notification code evaluation.
func RecommendPVCs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []core.TermConfig, settings ThresholdSettings, notifThresholds core.NotificationThresholds) ([]PVCRec, error) {
	rows, err := queryPVCDigests(ctx, pool, orgID, clusterUUID, terms)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	groups := make(map[PVCKey][]PVCDigestRow)
	for _, r := range rows {
		key := PVCKey{Namespace: r.Namespace, PVC: r.PVC}
		groups[key] = append(groups[key], r)
	}

	return libpvc.RecommendPVCs(ctx, groups, EngineConfig{
		OrgID:           orgID,
		ClusterUUID:     clusterUUID,
		Terms:           terms,
		Settings:        settings,
		NotifThresholds: notifThresholds,
	})
}

func queryPVCDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []core.TermConfig) ([]PVCDigestRow, error) {
	lookbackDays := core.MaxWindowDays(terms, 90)
	query := fmt.Sprintf(`
		SELECT bucket_date, namespace, persistentvolumeclaim, last_seen_pod, vm_name, persistentvolume,
			storageclass, capacity_bytes, request_bytes,
			usage_bytes_min, usage_bytes_max, usage_bytes_avg, sample_count
		FROM daily_pvc_digests
		WHERE org_id = $1 AND cluster_uuid = $2
			AND bucket_date >= (CURRENT_DATE - INTERVAL '%d days')
		ORDER BY namespace, persistentvolumeclaim, bucket_date`, lookbackDays)

	pgRows, err := pool.Query(ctx, query, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("querying PVC digests: %w", err)
	}
	defer pgRows.Close()

	var results []PVCDigestRow
	for pgRows.Next() {
		var r PVCDigestRow
		if err := pgRows.Scan(
			&r.BucketDate, &r.Namespace, &r.PVC, &r.LastSeenPod, &r.VMName, &r.PV,
			&r.StorageClass, &r.CapacityBytes, &r.RequestBytes,
			&r.UsageBytesMin, &r.UsageBytesMax, &r.UsageBytesAvg, &r.SampleCount,
		); err != nil {
			return nil, fmt.Errorf("scanning PVC digest row: %w", err)
		}
		results = append(results, r)
	}
	return results, pgRows.Err()
}

// WritePVCRecommendations upserts PVC recommendations to the database and
// removes rows for terms no longer in the active configuration.
func WritePVCRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []PVCRec, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("write_pvc_recommendations", t0) }()

	err := pgrec.WritePVCRecommendations(ctx, pool, recs, validTerms)
	if err != nil {
		logging.GetLogger().Warnf("WritePVCRecommendations: %v", err)
	}
	return err
}
