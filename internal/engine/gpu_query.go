package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// GPUQueryFilters narrows gpu_container_digests rows (optional).
type GPUQueryFilters struct {
	NodeNameExact string
	GPUModelExact string
}

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
func QueryGPURecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time, terms []TermConfig, digestFilters *GPUQueryFilters) (map[string][]*GPURec, map[string]string, map[string]time.Time, error) {
	gpuSettings, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load gpu thresholds: %w", err)
	}
	query := `
		SELECT interval_start, namespace, workload, container_name,
			COALESCE(gpu_model_name, ''), COALESCE(gpu_profile_name, ''),
			COALESCE(node_name, ''),
			COALESCE(fb_usage_min_mib, 0), COALESCE(fb_usage_max_mib, 0), COALESCE(fb_usage_avg_mib, 0),
			COALESCE(tensor_pipe_active_min, 0), COALESCE(tensor_pipe_active_max, 0), COALESCE(tensor_pipe_active_avg, 0),
			COALESCE(dram_active_min, 0), COALESCE(dram_active_max, 0), COALESCE(dram_active_avg, 0),
			COALESCE(sm_active_min, 0), COALESCE(sm_active_max, 0), COALESCE(sm_active_avg, 0)
		FROM gpu_container_digests
		WHERE cluster_uuid = $1
		  AND interval_start >= $2 AND interval_start <= $3`
	args := []interface{}{clusterUUID, start.UTC().Format("2006-01-02"), end.UTC().Format("2006-01-02")}
	argPos := 4
	if digestFilters != nil {
		if strings.TrimSpace(digestFilters.NodeNameExact) != "" {
			query += fmt.Sprintf(" AND node_name = $%d", argPos)
			args = append(args, digestFilters.NodeNameExact)
			argPos++
		}
		if strings.TrimSpace(digestFilters.GPUModelExact) != "" {
			query += fmt.Sprintf(" AND gpu_model_name = $%d", argPos)
			args = append(args, digestFilters.GPUModelExact)
			argPos++
		}
	}
	query += `
		ORDER BY namespace, workload, container_name, interval_start`
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("query gpu_container_digests: %w", err)
	}
	defer rows.Close()

	grouped := make(map[string][]GPUDigestRow, 32)
	lastNode := make(map[string]string, 32)
	nodeLastSeen := make(map[string]time.Time, 8)
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
			rec := RecommendGPUWithSettings(windowDigests, gpuSettings)
			if rec != nil {
				rec.Term = tc.Name
				result[key] = append(result[key], rec)
			}
		}
	}

	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof(
		"QueryGPURecommendations: %d containers with GPU data, %d container-term recommendations",
		len(grouped), countGPURecs(result))
	return result, lastNode, nodeLastSeen, nil
}

// filterGPUByWindow returns GPU digest rows within the last windowDays
// from endDate (inclusive), anchored to the latest data point.
// filterGPUByWindow returns GPU digest rows within the last windowDays from endDate (inclusive).
// Rows are assumed sorted by interval_start (ascending) from the DB query.
func filterGPUByWindow(rows []GPUDigestRow, endDate time.Time, windowDays int) []GPUDigestRow {
	cutoffDay := endDate.AddDate(0, 0, -(windowDays - 1)).Truncate(24 * time.Hour)
	endDay := endDate.Truncate(24 * time.Hour)

	lo := 0
	hi := len(rows)
	for lo < hi {
		mid := (lo + hi) / 2
		if rows[mid].IntervalStart.Truncate(24 * time.Hour).Before(cutoffDay) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	result := make([]GPUDigestRow, 0, len(rows)-lo)
	for i := lo; i < len(rows); i++ {
		d := rows[i].IntervalStart.Truncate(24 * time.Hour)
		if d.After(endDay) {
			break
		}
		result = append(result, rows[i])
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

// MarkContainersWithGPU sets has_gpu = TRUE and gpu_model_name on recommendation_sets
// rows whose containers have data in gpu_container_digests. This enables SQL-level
// filtering on has_gpu and gpu_model_name for correct pagination.
func MarkContainersWithGPU(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE recommendation_sets rs
		SET has_gpu = TRUE,
		    gpu_model_name = COALESCE(g_latest.gpu_model_name, '')
		FROM (
			SELECT DISTINCT ON (namespace, workload, container_name)
				namespace, workload, container_name, gpu_model_name
			FROM gpu_container_digests
			WHERE cluster_uuid = $2
			ORDER BY namespace, workload, container_name, interval_start DESC
		) g_latest
		WHERE rs.org_id = $1
		  AND rs.cluster_uuid = $2
		  AND g_latest.namespace = rs.namespace
		  AND g_latest.workload = rs.workload
		  AND g_latest.container_name = rs.container_name
		  AND (rs.has_gpu = FALSE OR rs.gpu_model_name != COALESCE(g_latest.gpu_model_name, ''))`,
		orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("mark containers with GPU: %w", err)
	}

	_, err = pool.Exec(ctx, `
		UPDATE recommendation_sets rs
		SET has_gpu = FALSE, gpu_model_name = '', gpu_classification = ''
		WHERE rs.org_id = $1
		  AND rs.cluster_uuid = $2
		  AND rs.has_gpu = TRUE
		  AND NOT EXISTS (
			SELECT 1 FROM gpu_container_digests g
			WHERE g.cluster_uuid = rs.cluster_uuid
			  AND g.namespace = rs.namespace
			  AND g.workload = rs.workload
			  AND g.container_name = rs.container_name
		  )`, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("unmark containers without GPU: %w", err)
	}
	return nil
}

// StoreGPUClassifications computes GPU classifications for all GPU containers
// in a cluster and stores them in recommendation_sets.gpu_classification.
// This runs after MarkContainersWithGPU so has_gpu and gpu_model_name are set.
func StoreGPUClassifications(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []TermConfig) error {
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -MaxWindowDays(terms, 30))

	gpuRecs, _, _, err := QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, terms, nil)
	if err != nil {
		return fmt.Errorf("query GPU recommendations for classification: %w", err)
	}
	if len(gpuRecs) == 0 {
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for GPU classifications: %w", err)
	}
	defer tx.Rollback(ctx)

	for key, recs := range gpuRecs {
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 {
			continue
		}
		ns, wl, cn := parts[0], parts[1], parts[2]

		for _, rec := range recs {
			classification := string(rec.Classification)
			if classification == "" {
				classification = string(GPUClassNoProfiling)
			}
			_, err := tx.Exec(ctx, `
				UPDATE recommendation_sets
				SET gpu_classification = $6
				WHERE org_id = $1
				  AND cluster_uuid = $2
				  AND namespace = $3
				  AND workload = $4
				  AND container_name = $5
				  AND term = $7
				  AND gpu_classification != $6`,
				orgID, clusterUUID, ns, wl, cn, classification, rec.Term)
			if err != nil {
				return fmt.Errorf("store GPU classification for %s term=%s: %w", key, rec.Term, err)
			}
		}
	}

	return tx.Commit(ctx)
}
