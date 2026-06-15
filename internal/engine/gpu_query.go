package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
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

// PageGPUKey identifies a container on a page for scoped GPU digest lookups.
type PageGPUKey struct {
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
	return queryGPURecommendations(ctx, pool, orgID, clusterUUID, start, end, terms, digestFilters, nil)
}

// QueryGPURecommendationsForContainers loads GPU digest rows for specific containers on a page.
func QueryGPURecommendationsForContainers(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	pageKeys []PageGPUKey,
	start, end time.Time,
	terms []TermConfig,
	digestFilters *GPUQueryFilters,
) (map[string][]*GPURec, map[string]string, map[string]time.Time, error) {
	if len(pageKeys) == 0 {
		return nil, nil, nil, nil
	}
	namespaces := make([]string, 0, len(pageKeys))
	workloads := make([]string, 0, len(pageKeys))
	containers := make([]string, 0, len(pageKeys))
	for _, key := range pageKeys {
		if key.ClusterUUID != "" && key.ClusterUUID != clusterUUID {
			continue
		}
		if key.Namespace == "" || key.Workload == "" || key.ContainerName == "" {
			continue
		}
		namespaces = append(namespaces, key.Namespace)
		workloads = append(workloads, key.Workload)
		containers = append(containers, key.ContainerName)
	}
	if len(namespaces) == 0 {
		return nil, nil, nil, nil
	}
	return queryGPURecommendations(ctx, pool, orgID, clusterUUID, start, end, terms, digestFilters, &gpuContainerFilter{
		namespaces: namespaces,
		workloads:  workloads,
		containers: containers,
	})
}

type gpuContainerFilter struct {
	namespaces []string
	workloads  []string
	containers []string
}

func queryGPURecommendations(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	start, end time.Time,
	terms []TermConfig,
	digestFilters *GPUQueryFilters,
	containerFilter *gpuContainerFilter,
) (map[string][]*GPURec, map[string]string, map[string]time.Time, error) {
	gpuSettings, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load gpu thresholds: %w", err)
	}
	gpuIdleCfg := LoadGPUIdleConfig(ctx, pool, orgID)
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
	if containerFilter != nil && len(containerFilter.namespaces) > 0 {
		query += fmt.Sprintf(`
		  AND (namespace, workload, container_name) IN (
			SELECT u.n, u.w, u.cn
			FROM unnest($%d::text[], $%d::text[], $%d::text[]) AS u(n, w, cn)
		  )`, argPos, argPos+1, argPos+2)
		args = append(args, containerFilter.namespaces, containerFilter.workloads, containerFilter.containers)
		argPos += 3
	}
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

	grouped := make(map[containerID][]GPUDigestRow, 32)
	lastNode := make(map[containerID]string, 32)
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
		key := containerID{Namespace: ns, Workload: wl, ContainerName: cn}
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
	nodeMapStr := make(map[string]string, len(lastNode))
	for key, allDigests := range grouped {
		strKey := gpuContainerKeyString(key)
		nodeMapStr[strKey] = lastNode[key]
		latest := latestGPUDigest(allDigests)
		for _, tc := range terms {
			windowDigests := filterGPUByWindow(allDigests, latest.IntervalStart, tc.WindowDays)
			if len(windowDigests) < tc.MinDataDays {
				continue
			}
			rec := RecommendGPUWithSettings(windowDigests, gpuSettings, gpuIdleCfg)
			if rec != nil {
				rec.Term = tc.Name
				result[strKey] = append(result[strKey], rec)
			}
		}
	}

	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof(
		"QueryGPURecommendations: %d containers with GPU data, %d container-term recommendations",
		len(grouped), countGPURecs(result))
	return result, nodeMapStr, nodeLastSeen, nil
}

func gpuContainerKeyString(key containerID) string {
	return key.Namespace + "/" + key.Workload + "/" + key.ContainerName
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
		SET has_gpu = FALSE, gpu_model_name = '', gpu_classification = '',
		    gpu_idle_state = 'active', gpu_idle_since = NULL,
		    gpu_idle_duration_days = NULL, gpu_estimated_waste_cents = 0
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

const gpuClassificationUpdateSQL = `
	UPDATE recommendation_sets
	SET gpu_classification = $6,
	    gpu_idle_state = $8,
	    gpu_idle_since = $9,
	    gpu_idle_duration_days = $10,
	    gpu_estimated_waste_cents = $11,
	    estimated_gpu_savings_cents = $12
	WHERE org_id = $1
	  AND cluster_uuid = $2
	  AND namespace = $3
	  AND workload = $4
	  AND container_name = $5
	  AND term = $7`

type gpuClassificationWrite struct {
	orgID, clusterUUID, namespace, workload, containerName string
	term, classification, idleState                        string
	idleSince                                              *time.Time
	idleDurationDays                                       int
	wasteCents                   int64
	gpuSavingsCents              *int64
}

func queueGPUClassificationUpdate(batch *pgx.Batch, w gpuClassificationWrite) {
	batch.Queue(gpuClassificationUpdateSQL,
		w.orgID, w.clusterUUID, w.namespace, w.workload, w.containerName,
		w.classification, w.term,
		w.idleState, w.idleSince, w.idleDurationDays, w.wasteCents, w.gpuSavingsCents,
	)
}

func flushGPUClassificationBatch(ctx context.Context, sender pgxBatchSender, batch *pgx.Batch, chunk []gpuClassificationWrite) error {
	if len(chunk) == 0 {
		return nil
	}
	br := sender.SendBatch(ctx, batch)
	defer br.Close()

	for i := range chunk {
		w := chunk[i]
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("store GPU classification for %s/%s/%s term=%s: %w",
				w.namespace, w.workload, w.containerName, w.term, err)
		}
	}
	return nil
}

// StoreGPUClassifications computes GPU classifications and idle/zombie state for all
// GPU containers in a cluster and stores them on recommendation_sets. This runs after
// MarkContainersWithGPU so has_gpu and gpu_model_name are set.
func StoreGPUClassifications(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []TermConfig, costData *costdata.ClusterCostData) error {
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -MaxWindowDays(terms, 30))

	gpuRecs, _, _, err := QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, terms, nil)
	if err != nil {
		return fmt.Errorf("query GPU recommendations for classification: %w", err)
	}
	if len(gpuRecs) == 0 {
		return nil
	}

	gpuMonthlyRate := GPUMonthlyRate(costData)
	writes := make([]gpuClassificationWrite, 0, len(gpuRecs)*len(terms))
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
			wasteCents := int64(0)
			if rec.GPUIdleState != IdleStateActive && gpuMonthlyRate > 0 {
				wasteCents = money.USDToCents(gpuMonthlyRate)
			}
			writes = append(writes, gpuClassificationWrite{
				orgID:            orgID,
				clusterUUID:      clusterUUID,
				namespace:        ns,
				workload:         wl,
				containerName:    cn,
				term:             rec.Term,
				classification:   classification,
				idleState:        idleStateForWrite(rec.GPUIdleState),
				idleSince:        rec.GPUIdleSince,
				idleDurationDays: rec.GPUIdleDurationDays,
				wasteCents:       wasteCents,
				gpuSavingsCents:  ComputeGPUSavingsCents(rec, costData),
			})
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for GPU classifications: %w", err)
	}
	defer tx.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(writes); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(writes) {
			chunkEnd = len(writes)
		}
		chunk := writes[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, w := range chunk {
			queueGPUClassificationUpdate(batch, w)
		}
		if err := flushGPUClassificationBatch(ctx, tx, batch, chunk); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GPUSavingsLookupKey builds the map key for persisted GPU savings (namespace/workload/container/term).
func GPUSavingsLookupKey(namespace, workload, container, term string) string {
	return namespace + "/" + workload + "/" + container + "/" + term
}

// LoadPersistedGPUSavings reads estimated_gpu_savings_cents from recommendation_sets for GPU containers.
func LoadPersistedGPUSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (map[string]*int64, error) {
	rows, err := pool.Query(ctx, `
		SELECT namespace, workload, container_name, term, estimated_gpu_savings_cents
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND has_gpu = true`,
		orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]*int64)
	for rows.Next() {
		var ns, wl, cn, term string
		var cents *int64
		if err := rows.Scan(&ns, &wl, &cn, &term, &cents); err != nil {
			return nil, err
		}
		out[GPUSavingsLookupKey(ns, wl, cn, term)] = cents
	}
	return out, rows.Err()
}
