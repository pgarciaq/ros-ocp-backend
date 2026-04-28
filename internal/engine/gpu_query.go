package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

type containerID struct {
	ClusterUUID   string
	Namespace     string
	Workload      string
	ContainerName string
}

// QueryGPURecommendations reads gpu_container_digests for the given cluster and
// time range, then calls RecommendGPU for each container that has GPU data.
// Returns a map keyed by "namespace/workload/container" for fast lookup.
func QueryGPURecommendations(ctx context.Context, pool *pgxpool.Pool, clusterUUID string, start, end time.Time) (map[string]*GPURec, error) {
	rows, err := pool.Query(ctx, `
		SELECT interval_start, namespace, workload, container_name,
			COALESCE(gpu_model_name, ''), COALESCE(gpu_profile_name, ''),
			COALESCE(fb_usage_min_mib, 0), COALESCE(fb_usage_max_mib, 0), COALESCE(fb_usage_avg_mib, 0),
			COALESCE(tensor_pipe_active_min, 0), COALESCE(tensor_pipe_active_max, 0), COALESCE(tensor_pipe_active_avg, 0),
			COALESCE(dram_active_min, 0), COALESCE(dram_active_max, 0), COALESCE(dram_active_avg, 0),
			COALESCE(sm_active_min, 0), COALESCE(sm_active_max, 0), COALESCE(sm_active_avg, 0)
		FROM gpu_container_digests
		WHERE cluster_uuid = $1
		  AND interval_start >= $2 AND interval_start <= $3
		ORDER BY namespace, workload, container_name, interval_start`,
		clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query gpu_container_digests: %w", err)
	}
	defer rows.Close()

	grouped := map[string][]GPUDigestRow{}
	for rows.Next() {
		var d GPUDigestRow
		var ns, wl, cn string
		err := rows.Scan(
			&d.IntervalStart, &ns, &wl, &cn,
			&d.GPUModelName, &d.GPUProfileName,
			&d.FBUsageMinMiB, &d.FBUsageMaxMiB, &d.FBUsageAvgMiB,
			&d.TensorPipeActiveMin, &d.TensorPipeActiveMax, &d.TensorPipeActiveAvg,
			&d.DRAMActiveMin, &d.DRAMActiveMax, &d.DRAMActiveAvg,
			&d.SMActiveMin, &d.SMActiveMax, &d.SMActiveAvg,
		)
		if err != nil {
			return nil, fmt.Errorf("scan GPU digest row: %w", err)
		}
		key := fmt.Sprintf("%s/%s/%s", ns, wl, cn)
		grouped[key] = append(grouped[key], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate GPU digest rows: %w", err)
	}

	if len(grouped) == 0 {
		return nil, nil
	}

	result := make(map[string]*GPURec, len(grouped))
	for key, digests := range grouped {
		rec := RecommendGPU(digests)
		if rec != nil {
			result[key] = rec
		}
	}

	log.Infof("QueryGPURecommendations: cluster=%s, %d containers with GPU data, %d recommendations",
		clusterUUID, len(grouped), len(result))
	return result, nil
}
