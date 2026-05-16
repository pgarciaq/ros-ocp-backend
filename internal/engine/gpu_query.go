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
// time range, then calls RecommendGPU for each container × term.
// Returns:
//   - recs: map keyed by "namespace/workload/container" → []*GPURec (one per term)
//   - nodeMap: map keyed by "namespace/workload/container" → last-seen node name
//   - nodeLastSeen: map keyed by node name → most recent interval_start for that node
func QueryGPURecommendations(ctx context.Context, pool *pgxpool.Pool, clusterUUID string, start, end time.Time, terms []TermConfig) (map[string][]*GPURec, map[string]string, map[string]time.Time, error) {
	rows, err := pool.Query(ctx, `
		SELECT interval_start, namespace, workload, container_name,
			COALESCE(gpu_model_name, ''), COALESCE(gpu_profile_name, ''),
			COALESCE(node_name, ''),
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
		return nil, nil, nil, fmt.Errorf("query gpu_container_digests: %w", err)
	}
	defer rows.Close()

	grouped := map[string][]GPUDigestRow{}
	lastNode := map[string]string{}
	nodeLastSeen := map[string]time.Time{}
	for rows.Next() {
		var d GPUDigestRow
		var ns, wl, cn string
		err := rows.Scan(
			&d.IntervalStart, &ns, &wl, &cn,
			&d.GPUModelName, &d.GPUProfileName,
			&d.NodeName,
			&d.FBUsageMinMiB, &d.FBUsageMaxMiB, &d.FBUsageAvgMiB,
			&d.TensorPipeActiveMin, &d.TensorPipeActiveMax, &d.TensorPipeActiveAvg,
			&d.DRAMActiveMin, &d.DRAMActiveMax, &d.DRAMActiveAvg,
			&d.SMActiveMin, &d.SMActiveMax, &d.SMActiveAvg,
		)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("scan GPU digest row: %w", err)
		}
		key := fmt.Sprintf("%s/%s/%s", ns, wl, cn)
		grouped[key] = append(grouped[key], d)
		if d.NodeName != "" {
			lastNode[key] = d.NodeName
			if prev, ok := nodeLastSeen[d.NodeName]; !ok || d.IntervalStart.After(prev) {
				nodeLastSeen[d.NodeName] = d.IntervalStart
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("iterate GPU digest rows: %w", err)
	}

	if len(grouped) == 0 {
		return nil, nil, nil, nil
	}

	result := make(map[string][]*GPURec, len(grouped))
	for key, allDigests := range grouped {
		latest := latestGPUDigest(allDigests)
		for _, tc := range terms {
			windowDigests := filterGPUByWindow(allDigests, latest.IntervalStart, tc.WindowDays)
			if len(windowDigests) < tc.MinDataDays {
				continue
			}
			rec := RecommendGPU(windowDigests)
			if rec != nil {
				rec.Term = tc.Name
				result[key] = append(result[key], rec)
			}
		}
	}

	log.Infof("QueryGPURecommendations: cluster=%s, %d containers with GPU data, %d container-term recommendations",
		clusterUUID, len(grouped), countGPURecs(result))
	return result, lastNode, nodeLastSeen, nil
}

// filterGPUByWindow returns GPU digest rows within the last windowDays
// from endDate (inclusive), anchored to the latest data point.
func filterGPUByWindow(rows []GPUDigestRow, endDate time.Time, windowDays int) []GPUDigestRow {
	cutoff := endDate.AddDate(0, 0, -(windowDays - 1))
	var result []GPUDigestRow
	for _, r := range rows {
		d := r.IntervalStart.Truncate(24 * time.Hour)
		if !d.Before(cutoff.Truncate(24*time.Hour)) && !d.After(endDate.Truncate(24*time.Hour)) {
			result = append(result, r)
		}
	}
	return result
}

// latestGPUDigest returns the GPUDigestRow with the most recent IntervalStart.
func latestGPUDigest(rows []GPUDigestRow) GPUDigestRow {
	if len(rows) == 0 {
		return GPUDigestRow{}
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.IntervalStart.After(best.IntervalStart) {
			best = r
		}
	}
	return best
}

func countGPURecs(m map[string][]*GPURec) int {
	n := 0
	for _, recs := range m {
		n += len(recs)
	}
	return n
}
