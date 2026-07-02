package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/asyncjobs"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const (
	savingsRecTypeContainer     = "container"
	savingsRecTypeNode          = "node"
	savingsRecTypePVC           = "pvc"
	savingsRecTypeQuota         = "quota"
	savingsRecTypeClusterQuota  = "cluster-quota"
)

var (
	savingsRecalculationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ros_savings_recalculation_total",
			Help: "Cost-model-triggered savings-only recalculations per recommendation type and outcome",
		},
		[]string{"recommendation_type", "status"},
	)

	// clusterSavingsRecalcFunc runs savings-only recalculation for one cluster (tests may replace).
	clusterSavingsRecalcFunc = defaultRecalculateClusterSavings

	// savingsRecalcHookMu guards access to savingsRecalcHook and savingsRecalcRunHook.
	savingsRecalcHookMu sync.Mutex

	// savingsRecalcHook runs at the start of TriggerSavingsRecalculationAsync (tests only).
	savingsRecalcHook func(orgID string, recTypes []string)

	// savingsRecalcRunHook runs at the start of each RecalculateSavingsForOrg invocation (tests only).
	savingsRecalcRunHook func(orgID string, recTypes []string)
)

// SetClusterSavingsRecalcFuncForTest replaces the per-cluster savings recalculation function.
func SetClusterSavingsRecalcFuncForTest(fn func(context.Context, *pgxpool.Pool, string, string, []string) error) func() {
	prev := clusterSavingsRecalcFunc
	clusterSavingsRecalcFunc = fn
	return func() { clusterSavingsRecalcFunc = prev }
}

// SetSavingsRecalcHookForTest registers a hook invoked when async savings recalculation is triggered.
func SetSavingsRecalcHookForTest(hook func(orgID string, recTypes []string)) {
	savingsRecalcHookMu.Lock()
	savingsRecalcHook = hook
	savingsRecalcHookMu.Unlock()
}

// ClearSavingsRecalcHookForTest removes the test hook.
func ClearSavingsRecalcHookForTest() {
	savingsRecalcHookMu.Lock()
	savingsRecalcHook = nil
	savingsRecalcHookMu.Unlock()
}

// SetSavingsRecalcRunHookForTest registers a hook invoked when RecalculateSavingsForOrg starts.
func SetSavingsRecalcRunHookForTest(hook func(orgID string, recTypes []string)) {
	savingsRecalcHookMu.Lock()
	savingsRecalcRunHook = hook
	savingsRecalcHookMu.Unlock()
}

// ClearSavingsRecalcRunHookForTest removes the recalc run test hook.
func ClearSavingsRecalcRunHookForTest() {
	savingsRecalcHookMu.Lock()
	savingsRecalcRunHook = nil
	savingsRecalcHookMu.Unlock()
}

// TriggerSavingsRecalculationAsync starts background savings-only recalculation after Koku cost model
// rate changes. The caller returns immediately; work runs in a detached goroutine.
func TriggerSavingsRecalculationAsync(pool *pgxpool.Pool, orgID, clusterUUID string, recTypes []string) {
	if pool == nil || orgID == "" {
		return
	}
	if !config.GetConfig().SavingsEstimatesEnabled || !config.GetConfig().SavingsRecalculationEnabled {
		return
	}
	types := normalizeSavingsRecTypes(recTypes)
	if len(types) == 0 {
		return
	}
	savingsRecalcHookMu.Lock()
	hook := savingsRecalcHook
	savingsRecalcHookMu.Unlock()
	if hook != nil {
		hook(orgID, types)
	}
	fleetsummary.InvalidateOrg(orgID)
	fleetheatmap.InvalidateOrg(orgID)
	asyncjobs.Go(func(ctx context.Context) {
		triggerSavingsRecalcCoalesced(ctx, pool, orgID, clusterUUID, types)
	})
}

// RecalculateSavingsForOrg recomputes estimated_savings_cents for persisted recommendations
// using current Koku effective rates. Classification and sizing are not re-run.
func RecalculateSavingsForOrg(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recTypes []string) {
	savingsRecalcHookMu.Lock()
	runHook := savingsRecalcRunHook
	savingsRecalcHookMu.Unlock()
	if runHook != nil {
		runHook(orgID, recTypes)
	}
	log := logging.ForOrgOnly(orgID)
	started := time.Now()
	types := normalizeSavingsRecTypes(recTypes)

	costdata.InvalidateCostDataCache(orgID, clusterUUID)

	clusters, err := listClustersForSavingsRecalc(ctx, pool, orgID, clusterUUID)
	if err != nil {
		log.WithFields(map[string]interface{}{
			"msg":   "savings recalculation failed",
			"error": err.Error(),
		}).Error("unable to list clusters")
		for _, rt := range types {
			savingsRecalculationTotal.WithLabelValues(rt, "error").Inc()
		}
		return
	}

	log.WithFields(map[string]interface{}{
		"msg":                  "savings recalculation started",
		"recommendation_types": types,
		"clusters":             len(clusters),
		"cluster_filter":       clusterUUID != "",
	}).Info("savings recalculation started")

	if len(clusters) == 0 {
		log.WithFields(map[string]interface{}{
			"msg":         "savings recalculation completed",
			"duration_ms": time.Since(started).Milliseconds(),
		}).Info("savings recalculation completed")
		for _, rt := range types {
			savingsRecalculationTotal.WithLabelValues(rt, "success").Inc()
		}
		return
	}

	workers := thresholdRecalcConcurrency()
	jobs := make(chan string, len(clusters))
	var wg sync.WaitGroup
	var processed int
	var mu sync.Mutex

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for clusterID := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}

				if err := clusterSavingsRecalcFunc(ctx, pool, orgID, clusterID, types); err != nil {
					logging.ForOrg(orgID, clusterID).WithFields(map[string]interface{}{
						"msg":   "savings recalculation cluster failed",
						"error": err.Error(),
					}).Warn("savings recalculation cluster failed")
					for _, rt := range types {
						savingsRecalculationTotal.WithLabelValues(rt, "error").Inc()
					}
					continue
				}
				if err := ctx.Err(); err != nil {
					return
				}
				mu.Lock()
				processed++
				mu.Unlock()
				for _, rt := range types {
					savingsRecalculationTotal.WithLabelValues(rt, "success").Inc()
				}
			}
		}()
	}

enqueue:
	for _, cu := range clusters {
		select {
		case <-ctx.Done():
			break enqueue
		case jobs <- cu:
		}
	}
	close(jobs)
	wg.Wait()

	if containsSavingsRecType(types, savingsRecTypeContainer) {
		if err := model.RefreshOrgRecommendationStats(ctx, pool, orgID); err != nil {
			log.Warnf("savings recalc: refresh org recommendation stats failed: %v", err)
		}
	}

	log.WithFields(map[string]interface{}{
		"msg":                 "savings recalculation completed",
		"clusters_processed":  processed,
		"recommendation_types": types,
		"duration_ms":         time.Since(started).Milliseconds(),
	}).Info("savings recalculation completed")
}

func listClustersForSavingsRecalc(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]string, error) {
	clusterUUID = strings.TrimSpace(clusterUUID)
	if clusterUUID != "" {
		return []string{clusterUUID}, nil
	}
	return ListClustersForOrg(ctx, pool, orgID)
}

func defaultRecalculateClusterSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recTypes []string) error {
	start, end := recalcDateRange()
	var costData *costdata.ClusterCostData
	if config.GetConfig().SavingsEstimatesEnabled {
		costData = fetchRecalcCostData(ctx, orgID, clusterUUID, start, end)
	}

	var errs []error
	if containsSavingsRecType(recTypes, savingsRecTypeContainer) {
		if err := recalculateContainerSavings(ctx, pool, orgID, clusterUUID, costData); err != nil {
			errs = append(errs, err)
		}
		if err := recalculateGPUSavings(ctx, pool, orgID, clusterUUID, costData); err != nil {
			errs = append(errs, err)
		}
	}
	if containsSavingsRecType(recTypes, savingsRecTypeNode) {
		if err := recalculateNodeSavings(ctx, pool, orgID, clusterUUID, costData); err != nil {
			errs = append(errs, err)
		}
	}
	if containsSavingsRecType(recTypes, savingsRecTypePVC) {
		if err := recalculatePVCSavings(ctx, pool, orgID, clusterUUID, costData); err != nil {
			errs = append(errs, err)
		}
	}
	if containsSavingsRecType(recTypes, savingsRecTypeQuota) {
		if err := recalculateQuotaSavings(ctx, pool, orgID, clusterUUID, costData); err != nil {
			errs = append(errs, err)
		}
	}
	if containsSavingsRecType(recTypes, savingsRecTypeClusterQuota) {
		if err := recalculateClusterQuotaSavings(ctx, pool, orgID, clusterUUID, costData); err != nil {
			errs = append(errs, err)
		}
	}
	return errorsJoin(errs)
}

func recalculateGPUSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, costData *costdata.ClusterCostData) error {
	terms, err := LoadTermConfigCached(ctx, pool, orgID, "gpu")
	if err != nil {
		terms = DefaultTermsForPlugin("gpu")
	}
	return StoreGPUClassifications(ctx, pool, orgID, clusterUUID, terms, costData)
}

func recalculateContainerSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, costData *costdata.ClusterCostData) error {
	recs, err := loadContainerRecsForSavingsRecalc(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("load container recommendations: %w", err)
	}
	if len(recs) == 0 {
		return nil
	}
	ApplySavingsEstimates(recs, costData)
	return updateContainerSavings(ctx, pool, recs)
}

func recalculateNodeSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, costData *costdata.ClusterCostData) error {
	nodeSettings, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
	if err != nil {
		logging.ForOrg(orgID, clusterUUID).Warnf("savings recalc: load node thresholds failed, using defaults: %v", err)
		nodeSettings = DefaultNodeThresholdSettings()
	}
	cfg := NodeRecConfigFromThresholds(nodeSettings)

	recs, err := loadNodeRecsForSavingsRecalc(ctx, pool, orgID, clusterUUID, cfg.AllocatableFactor)
	if err != nil {
		return fmt.Errorf("load node recommendations: %w", err)
	}
	if len(recs) == 0 {
		return nil
	}
	ApplyNodeSavings(recs, costData)
	return updateNodeSavings(ctx, pool, orgID, clusterUUID, recs)
}

func recalculatePVCSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, costData *costdata.ClusterCostData) error {
	recs, err := loadPVCRecsForSavingsRecalc(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("load PVC recommendations: %w", err)
	}
	if len(recs) == 0 {
		return nil
	}
	ApplyPVCSavings(recs, costData)
	return updatePVCSavings(ctx, pool, recs)
}

func recalculateQuotaSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, costData *costdata.ClusterCostData) error {
	recs, err := loadQuotaRecsForSavingsRecalc(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("load quota recommendations: %w", err)
	}
	if len(recs) == 0 {
		return nil
	}
	ApplyQuotaSavings(recs, costData)
	return updateQuotaSavings(ctx, pool, recs)
}

func recalculateClusterQuotaSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, costData *costdata.ClusterCostData) error {
	recs, err := loadClusterQuotaRecsForSavingsRecalc(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("load cluster-quota recommendations: %w", err)
	}
	if len(recs) == 0 {
		return nil
	}
	ApplyClusterQuotaSavings(recs, costData)
	return updateClusterQuotaSavings(ctx, pool, recs)
}

func loadContainerRecsForSavingsRecalc(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]ContainerRec, error) {
	rows, err := pool.Query(ctx, `
		SELECT org_id, cluster_uuid::text, namespace, workload, COALESCE(workload_type, ''), container_name,
			term, engine,
			COALESCE(rec_cpu_request_millicores, 0), COALESCE(rec_memory_request_kib, 0),
			COALESCE(current_cpu_request_millicores, 0), COALESCE(current_memory_request_kib, 0),
			COALESCE(pod_count_avg, 0), COALESCE(desired_replicas, 0),
			COALESCE(idle_state, 'active'), notification_codes
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND stale = false`,
		orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []ContainerRec
	for rows.Next() {
		var r ContainerRec
		var idleState string
		if err := rows.Scan(
			&r.OrgID, &r.ClusterUUID, &r.Namespace, &r.Workload, &r.WorkloadType, &r.ContainerName,
			&r.Term, &r.Engine,
			&r.RecCPURequestMC, &r.RecMemRequestKiB,
			&r.CurrentCPURequestMC, &r.CurrentMemRequestKiB,
			&r.PodCountAvg, &r.DesiredReplicas,
			&idleState, &r.NotificationCodes,
		); err != nil {
			return nil, err
		}
		r.IdleState = IdleState(idleState)
		r.IsIdle = r.IdleState == IdleStateIdle || r.IdleState == IdleStateZombie
		r.IsAbandoned = r.IdleState == IdleStateZombie
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

func loadNodeRecsForSavingsRecalc(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, allocatableFactor float64) ([]NodeRec, error) {
	rows, err := pool.Query(ctx, `
		SELECT nr.node, nr.term, nr.engine,
			COALESCE(nr.recommended_cpu_cores, 0), COALESCE(nr.recommended_memory_gib, 0),
			COALESCE(nr.node_count_reduction, 0), nr.notification_codes,
			d.max_cpu_allocatable_mc, d.max_mem_allocatable_kib,
			d.max_cpu_requests_mc, d.max_mem_requests_kib
		FROM node_recommendations nr
		LEFT JOIN LATERAL (
			SELECT max_cpu_allocatable_mc, max_mem_allocatable_kib,
				max_cpu_requests_mc, max_mem_requests_kib
			FROM daily_node_digests
			WHERE org_id = nr.org_id AND cluster_uuid = nr.cluster_uuid AND node = nr.node
			ORDER BY bucket_date DESC
			LIMIT 1
		) d ON true
		WHERE nr.org_id = $1 AND nr.cluster_uuid = $2::uuid`,
		orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	const kibPerGiB = 1024 * 1024
	var recs []NodeRec
	for rows.Next() {
		var r NodeRec
		var recCPUCores, recMemGiB float64
		var maxCPUAlloc, maxMemAlloc *int64
		var maxCPUReq, maxMemReq int64
		if err := rows.Scan(
			&r.Node, &r.Term, &r.Engine,
			&recCPUCores, &recMemGiB, &r.NodeCountReduction, &r.NotificationCodes,
			&maxCPUAlloc, &maxMemAlloc, &maxCPUReq, &maxMemReq,
		); err != nil {
			return nil, err
		}
		r.RecommendedCPUMC = int64(recCPUCores * 1000)
		r.RecommendedMemKiB = int64(recMemGiB * float64(kibPerGiB))
		r.CurrentCPUMC = resolveAllocatable(maxCPUAlloc, maxCPUReq, allocatableFactor)
		r.CurrentMemKiB = resolveAllocatableMem(maxMemAlloc, maxMemReq, allocatableFactor)
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

func loadPVCRecsForSavingsRecalc(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]PVCRec, error) {
	rows, err := pool.Query(ctx, `
		SELECT org_id, cluster_uuid::text, namespace, persistentvolumeclaim,
			COALESCE(capacity_bytes, 0),
			recommended_bytes, notification_codes, term
		FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
		orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []PVCRec
	for rows.Next() {
		var r PVCRec
		if err := rows.Scan(
			&r.OrgID, &r.ClusterUUID, &r.Namespace, &r.PVC,
			&r.CapacityBytes, &r.RecommendedBytes, &r.NotificationCodes, &r.Term,
		); err != nil {
			return nil, err
		}
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

func loadQuotaRecsForSavingsRecalc(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]QuotaRec, error) {
	rows, err := pool.Query(ctx, `
		SELECT org_id, cluster_uuid::text, namespace, COALESCE(quota_name, ''),
			recommendation_type,
			COALESCE(cpu_freed_millicores, 0), COALESCE(memory_freed_bytes, 0),
			COALESCE(storage_freed_bytes, 0), COALESCE(pods_freed, 0)
		FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND recommendation_type = $3`,
		orgID, clusterUUID, QuotaRecTypeTighten)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []QuotaRec
	for rows.Next() {
		var r QuotaRec
		if err := rows.Scan(
			&r.OrgID, &r.ClusterUUID, &r.Namespace, &r.QuotaName,
			&r.RecommendationType,
			&r.CapacityFreed.CPUMillicores, &r.CapacityFreed.MemoryBytes,
			&r.CapacityFreed.StorageBytes, &r.CapacityFreed.PodsFreed,
		); err != nil {
			return nil, err
		}
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

func loadClusterQuotaRecsForSavingsRecalc(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]ClusterQuotaRec, error) {
	rows, err := pool.Query(ctx, `
		SELECT org_id, cluster_uuid::text, cluster_quota_name,
			recommendation_type,
			COALESCE(savings_cpu_cores_freed, 0), COALESCE(savings_memory_bytes_freed, 0),
			COALESCE(savings_storage_bytes_freed, 0), COALESCE(savings_pods_freed, 0)
		FROM cluster_quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND recommendation_type = $3`,
		orgID, clusterUUID, QuotaRecTypeTighten)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []ClusterQuotaRec
	for rows.Next() {
		var r ClusterQuotaRec
		var cpuCoresFreed int64
		if err := rows.Scan(
			&r.OrgID, &r.ClusterUUID, &r.ClusterQuotaName,
			&r.RecommendationType,
			&cpuCoresFreed, &r.CapacityFreed.MemoryBytes,
			&r.CapacityFreed.StorageBytes, &r.CapacityFreed.PodsFreed,
		); err != nil {
			return nil, err
		}
		r.CapacityFreed.CPUMillicores = cpuCoresFreed * 1000
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

const containerSavingsUpdateSQL = `
	UPDATE recommendation_sets
	SET estimated_savings_cents = $1,
	    notification_codes = $2,
	    estimated_cpu_savings_cents = $3,
	    estimated_memory_savings_cents = $4,
	    updated_at = now()
	WHERE org_id = $5 AND cluster_uuid = $6::uuid
	  AND namespace = $7 AND workload = $8 AND workload_type = $9
	  AND container_name = $10 AND term = $11 AND engine = $12`

func updateContainerSavings(ctx context.Context, pool *pgxpool.Pool, recs []ContainerRec) error {
	if len(recs) == 0 {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(recs) {
			chunkEnd = len(recs)
		}
		chunk := recs[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, r := range chunk {
			batch.Queue(containerSavingsUpdateSQL,
				r.EstimatedSavingsCents, r.NotificationCodes,
				r.EstimatedCPUSavingsCents, r.EstimatedMemSavingsCents,
				r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName, r.Term, r.Engine,
			)
		}
		if err := flushRecommendationBatch(ctx, tx, batch, len(chunk)); err != nil {
			return fmt.Errorf("update container savings: %w", err)
		}
	}
	return tx.Commit(ctx)
}

const nodeSavingsUpdateSQL = `
	UPDATE node_recommendations
	SET estimated_savings_cents = $1,
	    notification_codes = $2,
	    updated_at = now()
	WHERE org_id = $3 AND cluster_uuid = $4::uuid
	  AND node = $5 AND term = $6 AND engine = $7`

func updateNodeSavings(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recs []NodeRec) error {
	if len(recs) == 0 {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", nodeRecsAdvisoryLock)); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(recs) {
			chunkEnd = len(recs)
		}
		chunk := recs[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, r := range chunk {
			batch.Queue(nodeSavingsUpdateSQL,
				r.EstimatedMonthlySavingsCents, r.NotificationCodes,
				orgID, clusterUUID, r.Node, r.Term, r.Engine,
			)
		}
		if err := flushRecommendationBatch(ctx, tx, batch, len(chunk)); err != nil {
			return fmt.Errorf("update node savings: %w", err)
		}
	}
	return tx.Commit(ctx)
}

const pvcSavingsUpdateSQL = `
	UPDATE pvc_recommendation_sets
	SET estimated_savings_cents = $1,
	    notification_codes = $2,
	    updated_at = now()
	WHERE org_id = $3 AND cluster_uuid = $4::uuid
	  AND namespace = $5 AND persistentvolumeclaim = $6 AND term = $7`

func updatePVCSavings(ctx context.Context, pool *pgxpool.Pool, recs []PVCRec) error {
	if len(recs) == 0 {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(recs) {
			chunkEnd = len(recs)
		}
		chunk := recs[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, r := range chunk {
			batch.Queue(pvcSavingsUpdateSQL,
				r.EstimatedMonthlySavingsCents, r.NotificationCodes,
				r.OrgID, r.ClusterUUID, r.Namespace, r.PVC, r.Term,
			)
		}
		if err := flushRecommendationBatch(ctx, tx, batch, len(chunk)); err != nil {
			return fmt.Errorf("update pvc savings: %w", err)
		}
	}
	return tx.Commit(ctx)
}

const quotaSavingsUpdateSQL = `
	UPDATE quota_recommendation_sets
	SET estimated_savings_cents = $1,
	    updated_at = now()
	WHERE org_id = $2 AND cluster_uuid = $3::uuid
	  AND namespace = $4 AND quota_name = $5`

func updateQuotaSavings(ctx context.Context, pool *pgxpool.Pool, recs []QuotaRec) error {
	if len(recs) == 0 {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(recs) {
			chunkEnd = len(recs)
		}
		chunk := recs[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, r := range chunk {
			batch.Queue(quotaSavingsUpdateSQL,
				r.EstimatedSavingsCents,
				r.OrgID, r.ClusterUUID, r.Namespace, r.QuotaName,
			)
		}
		if err := flushRecommendationBatch(ctx, tx, batch, len(chunk)); err != nil {
			return fmt.Errorf("update quota savings: %w", err)
		}
	}
	return tx.Commit(ctx)
}

const clusterQuotaSavingsUpdateSQL = `
	UPDATE cluster_quota_recommendation_sets
	SET estimated_savings_cents = $1,
	    updated_at = now()
	WHERE org_id = $2 AND cluster_uuid = $3::uuid
	  AND cluster_quota_name = $4`

func updateClusterQuotaSavings(ctx context.Context, pool *pgxpool.Pool, recs []ClusterQuotaRec) error {
	if len(recs) == 0 {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(recs) {
			chunkEnd = len(recs)
		}
		chunk := recs[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, r := range chunk {
			batch.Queue(clusterQuotaSavingsUpdateSQL,
				r.EstimatedSavingsCents,
				r.OrgID, r.ClusterUUID, r.ClusterQuotaName,
			)
		}
		if err := flushRecommendationBatch(ctx, tx, batch, len(chunk)); err != nil {
			return fmt.Errorf("update cluster-quota savings: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// NormalizeSavingsRecTypesForAPI validates recommendation type names for the internal API.
func NormalizeSavingsRecTypesForAPI(recTypes []string) []string {
	return normalizeSavingsRecTypes(recTypes)
}

func normalizeSavingsRecTypes(recTypes []string) []string {
	if len(recTypes) == 0 {
		return []string{
			savingsRecTypeContainer,
			savingsRecTypeNode,
			savingsRecTypePVC,
			savingsRecTypeQuota,
			savingsRecTypeClusterQuota,
		}
	}
	seen := make(map[string]struct{}, len(recTypes))
	var out []string
	for _, rt := range recTypes {
		rt = strings.TrimSpace(strings.ToLower(rt))
		switch rt {
		case savingsRecTypeContainer, savingsRecTypeNode, savingsRecTypePVC,
			savingsRecTypeQuota, savingsRecTypeClusterQuota:
			if _, ok := seen[rt]; ok {
				continue
			}
			seen[rt] = struct{}{}
			out = append(out, rt)
		}
	}
	return out
}

func containsSavingsRecType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

func containsNotificationCode(codes []int16, code int16) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

func errorsJoin(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("%s", strings.Join(msgs, "; "))
}
