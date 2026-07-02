package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/asyncjobs"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

const thresholdRecalcMaxConcurrent = 3

func thresholdRecalcConcurrency() int {
	n := config.GetConfig().ThresholdRecalcConcurrency
	if n <= 0 {
		return thresholdRecalcMaxConcurrent
	}
	return n
}

var (
	thresholdRecalculationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ros_threshold_recalculation_total",
			Help: "Threshold-triggered recommendation recalculations per recommendation type and outcome",
		},
		[]string{"recommendation_type", "status"},
	)

	// clusterRecalcFunc runs recommendation logic for one cluster; tests may replace it.
	clusterRecalcFunc = defaultRecalculateCluster

	// thresholdRecalcHookMu guards access to thresholdRecalcHook and thresholdRecalcRunHook.
	thresholdRecalcHookMu sync.Mutex

	// thresholdRecalcHook runs at the start of TriggerThresholdRecalculationAsync (tests only).
	thresholdRecalcHook func(orgID, recType string)

	// thresholdRecalcRunHook runs at the start of each RecalculateThresholdsForOrg invocation (tests only).
	thresholdRecalcRunHook func(orgID, recType string)
)

// SetClusterRecalcFuncForTest replaces the per-cluster recalculation function and returns a restore func.
func SetClusterRecalcFuncForTest(fn func(context.Context, *pgxpool.Pool, string, string, string) error) func() {
	prev := clusterRecalcFunc
	clusterRecalcFunc = fn
	return func() { clusterRecalcFunc = prev }
}

// SetThresholdRecalcHookForTest registers a hook invoked when async recalculation is triggered.
func SetThresholdRecalcHookForTest(hook func(orgID, recType string)) {
	thresholdRecalcHookMu.Lock()
	thresholdRecalcHook = hook
	thresholdRecalcHookMu.Unlock()
}

// ClearThresholdRecalcHookForTest removes the test hook.
func ClearThresholdRecalcHookForTest() {
	thresholdRecalcHookMu.Lock()
	thresholdRecalcHook = nil
	thresholdRecalcHookMu.Unlock()
}

// SetThresholdRecalcRunHookForTest registers a hook invoked when RecalculateThresholdsForOrg starts.
func SetThresholdRecalcRunHookForTest(hook func(orgID, recType string)) {
	thresholdRecalcHookMu.Lock()
	thresholdRecalcRunHook = hook
	thresholdRecalcHookMu.Unlock()
}

// ClearThresholdRecalcRunHookForTest removes the recalc run test hook.
func ClearThresholdRecalcRunHookForTest() {
	thresholdRecalcHookMu.Lock()
	thresholdRecalcRunHook = nil
	thresholdRecalcHookMu.Unlock()
}

// TriggerThresholdRecalculationAsync starts background recalculation after threshold settings change.
// The PUT handler returns immediately; work runs in a detached goroutine.
func TriggerThresholdRecalculationAsync(pool *pgxpool.Pool, orgID, recType string) {
	if pool == nil || orgID == "" || recType == "" {
		return
	}
	if !config.GetConfig().ThresholdRecalculationEnabled {
		return
	}
	thresholdRecalcHookMu.Lock()
	hook := thresholdRecalcHook
	thresholdRecalcHookMu.Unlock()
	if hook != nil {
		hook(orgID, recType)
	}
	fleetsummary.InvalidateOrg(orgID)
	fleetheatmap.InvalidateOrg(orgID)
	asyncjobs.Go(func(ctx context.Context) {
		triggerThresholdRecalcCoalesced(ctx, pool, orgID, recType)
	})
}

// RecalculateThresholdsForOrg re-runs the recommendation engine for all clusters in an org.
func RecalculateThresholdsForOrg(ctx context.Context, pool *pgxpool.Pool, orgID, recType string) {
	thresholdRecalcHookMu.Lock()
	runHook := thresholdRecalcRunHook
	thresholdRecalcHookMu.Unlock()
	if runHook != nil {
		runHook(orgID, recType)
	}
	log := logging.ForOrgOnly(orgID)
	started := time.Now()

	clusters, err := ListClustersForOrg(ctx, pool, orgID)
	if err != nil {
		log.WithFields(map[string]interface{}{
			"msg":                 "threshold recalculation failed",
			"recommendation_type": recType,
			"error":               err.Error(),
		}).Error("unable to list clusters")
		thresholdRecalculationTotal.WithLabelValues(recType, "error").Inc()
		return
	}

	log.WithFields(map[string]interface{}{
		"msg":                 "threshold recalculation started",
		"recommendation_type": recType,
		"clusters":            len(clusters),
	}).Info("threshold recalculation started")

	if len(clusters) == 0 {
		log.WithFields(map[string]interface{}{
			"msg":                 "threshold recalculation completed",
			"recommendation_type": recType,
			"clusters_processed":  0,
			"duration_ms":         time.Since(started).Milliseconds(),
		}).Info("threshold recalculation completed")
		thresholdRecalculationTotal.WithLabelValues(recType, "success").Inc()
		return
	}

	sem := thresholdRecalcConcurrency()
	jobs := make(chan string, len(clusters))
	var wg sync.WaitGroup
	var processed int
	var skipped int
	var mu sync.Mutex

	currentHash, hashErr := computeThresholdSettingsHash(ctx, pool, orgID, recType)
	if hashErr != nil {
		log.WithFields(map[string]interface{}{
			"msg":                 "threshold recalculation hash failed",
			"recommendation_type": recType,
			"error":               hashErr.Error(),
		}).Warn("unable to compute threshold hash; recalculating all clusters")
		currentHash = ""
	}

	for w := 0; w < sem; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for clusterID := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}
				if currentHash != "" {
					skip, err := shouldSkipClusterThresholdRecalc(ctx, pool, orgID, clusterID, recType, currentHash)
					if err != nil {
						logging.ForOrg(orgID, clusterID).Warnf("threshold recalc skip check failed: %v", err)
					} else if skip {
						mu.Lock()
						skipped++
						mu.Unlock()
						thresholdRecalculationTotal.WithLabelValues(recType, "skipped").Inc()
						continue
					}
				}
				if err := ctx.Err(); err != nil {
					return
				}

				if err := clusterRecalcFunc(ctx, pool, orgID, clusterID, recType); err != nil {
					logging.ForOrg(orgID, clusterID).WithFields(map[string]interface{}{
						"msg":                 "threshold recalculation cluster failed",
						"recommendation_type": recType,
						"error":               err.Error(),
					}).Warn("threshold recalculation cluster failed")
					thresholdRecalculationTotal.WithLabelValues(recType, "error").Inc()
					continue
				}
				mu.Lock()
				processed++
				mu.Unlock()
				if currentHash != "" {
					if err := setClusterRecalcHash(ctx, pool, orgID, clusterID, recType, currentHash); err != nil {
						logging.ForOrg(orgID, clusterID).Warnf("threshold recalc hash persist failed: %v", err)
					}
				}
				thresholdRecalculationTotal.WithLabelValues(recType, "success").Inc()
			}
		}()
	}

enqueue:
	for _, clusterUUID := range clusters {
		select {
		case <-ctx.Done():
			break enqueue
		case jobs <- clusterUUID:
		}
	}
	close(jobs)
	wg.Wait()

	log.WithFields(map[string]interface{}{
		"msg":                 "threshold recalculation completed",
		"recommendation_type": recType,
		"clusters_processed":  processed,
		"clusters_skipped":    skipped,
		"duration_ms":         time.Since(started).Milliseconds(),
	}).Info("threshold recalculation completed")
}

// ListClustersForOrg returns cluster UUIDs registered for the organization.
func ListClustersForOrg(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT c.cluster_uuid
		FROM clusters c
		JOIN rh_accounts a ON c.tenant_id = a.id
		WHERE a.org_id = $1`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list clusters for org: %w", err)
	}
	defer rows.Close()

	var uuids []string
	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			return nil, fmt.Errorf("scan cluster uuid: %w", err)
		}
		uuids = append(uuids, uuid)
	}
	return uuids, rows.Err()
}

func defaultRecalculateCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, recType string) error {
	switch recType {
	case "container":
		return recalculateContainerCluster(ctx, pool, orgID, clusterUUID)
	case "namespace":
		return recalculateNamespaceCluster(ctx, pool, orgID, clusterUUID)
	case "node":
		return recalculateNodeCluster(ctx, pool, orgID, clusterUUID)
	case "gpu":
		return recalculateGPUCluster(ctx, pool, orgID, clusterUUID)
	case "pvc":
		return recalculatePVCCluster(ctx, pool, orgID, clusterUUID)
	case "quota":
		return RunQuotaRecommendations(ctx, pool, orgID, clusterUUID)
	case "cluster-quota":
		return RunClusterQuotaRecommendations(ctx, pool, orgID, clusterUUID)
	case "snapshot":
		return RunSnapshotRecommendationsForCluster(ctx, pool, orgID, clusterUUID)
	default:
		return fmt.Errorf("unsupported recommendation_type %q", recType)
	}
}

func recalcDateRange() (time.Time, time.Time) {
	now := time.Now().UTC()
	cfg := config.GetConfig()
	start := now.AddDate(0, 0, -cfg.MaxLookbackDays)
	return start, now
}

func recalcCostDataProvider() costdata.CostDataProvider {
	cfg := config.GetConfig()
	if !cfg.SavingsEstimatesEnabled || cfg.KokuMasuURL == "" {
		return &costdata.NilCostDataProvider{}
	}
	timeout := time.Duration(cfg.GlobalHTTPClientTimeoutSecs) * time.Second
	return costdata.NewHTTPCostDataProvider(cfg.KokuMasuURL, timeout)
}

func fetchRecalcCostData(ctx context.Context, orgID, clusterUUID string, start, end time.Time) *costdata.ClusterCostData {
	provider := recalcCostDataProvider()
	cd, err := provider.GetEffectiveRates(ctx, orgID, clusterUUID, start, end)
	if err != nil {
		logging.ForOrg(orgID, clusterUUID).Warnf("threshold recalc: cost data fetch failed: %v", err)
		return nil
	}
	return cd
}

func recalculateContainerCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	log := logging.ForOrg(orgID, clusterUUID)
	start, now := recalcDateRange()
	appCfg := config.GetConfig()
	oomCfg := OOMConfig{
		BaseBump: appCfg.OOMBaseBump,
		MaxBump:  appCfg.OOMMaxBump,
	}

	var costData *costdata.ClusterCostData
	if appCfg.SavingsEstimatesEnabled {
		costData = fetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
	}

	oldRecs, err := ReadClusterOldRecommendationsByEngine(ctx, pool, orgID, clusterUUID)
	if err != nil {
		log.Warnf("threshold recalc: reading old recommendations failed: %v", err)
		oldRecs = nil
	}

	totalWritten := 0
	cycleStart := time.Now().UTC()
	tRec := cycleStart
	err = RecommendWorkloadsStreaming(ctx, pool, orgID, clusterUUID, start, now, oomCfg, func(batch []ContainerRec) error {
		ApplySavingsEstimates(batch, costData)
		if oldRecs != nil {
			adoptedKeys := FindAdoptedContainers(batch, oldRecs["cost"])
			if markErr := MarkAdopted(ctx, pool, orgID, clusterUUID, adoptedKeys); markErr != nil {
				log.Warnf("threshold recalc: adoption marking incomplete: %v", markErr)
			}
		}
		if writeErr := WriteRecommendations(ctx, pool, batch); writeErr != nil {
			return writeErr
		}
		totalWritten += len(batch)
		if histErr := WriteRecommendationHistory(ctx, pool, batch, ""); histErr != nil {
			log.Warnf("threshold recalc: writing recommendation history failed: %v", histErr)
		}
		if oldRecs != nil {
			oomCounts := OOMCountsByContainer(batch)
			if qualErr := WriteRecommendationQuality(ctx, pool, batch, oldRecs, oomCounts); qualErr != nil {
				log.Warnf("threshold recalc: writing quality metrics failed: %v", qualErr)
			}
		}
		return nil
	})
	metrics.ObserveRecommendation("container", tRec)
	if err != nil {
		return fmt.Errorf("recommend workloads: %w", err)
	}
	if totalWritten > 0 {
		if markedStale, staleErr := MarkUnreportedContainersStale(ctx, pool, orgID, clusterUUID, cycleStart); staleErr != nil {
			log.Warnf("threshold recalc: marking unreported containers stale failed: %v", staleErr)
		} else if markedStale > 0 {
			log.Infof("threshold recalc: marked %d unreported container recommendations stale", markedStale)
		}
		metrics.IncRecommendationsWritten("container", totalWritten)
		if err := RefreshOrgMetadata(ctx, pool, orgID); err != nil {
			return fmt.Errorf("refresh org metadata: %w", err)
		}
	}
	return nil
}

func recalculateNamespaceCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	log := logging.ForOrg(orgID, clusterUUID)
	start, now := recalcDateRange()

	tNs := time.Now()
	results, err := RecommendAllNamespaces(ctx, pool, orgID, clusterUUID, start, now)
	metrics.ObserveRecommendation("namespace", tNs)
	if err != nil {
		return fmt.Errorf("recommend namespaces: %w", err)
	}
	if len(results) == 0 {
		return nil
	}
	if err := WriteNamespaceRecommendations(ctx, pool, results); err != nil {
		return fmt.Errorf("write namespace recommendations: %w", err)
	}
	if bhResults, err := RecommendBusinessHoursNamespaces(ctx, pool, orgID, clusterUUID, start, now); err != nil {
		return fmt.Errorf("recommend business hours namespaces: %w", err)
	} else if len(bhResults) > 0 {
		if err := WriteNamespaceRecommendations(ctx, pool, bhResults); err != nil {
			return fmt.Errorf("write business hours namespace recommendations: %w", err)
		}
		if err := WriteNamespaceRecommendationHistory(ctx, pool, bhResults); err != nil {
			log.Warnf("threshold recalc: writing business hours namespace history failed: %v", err)
		}
	}
	if err := AggregateNamespaceIdleState(ctx, pool, orgID, clusterUUID); err != nil {
		return fmt.Errorf("aggregate namespace idle state: %w", err)
	}
	metrics.IncRecommendationsWritten("namespace", len(results))
	if err := WriteNamespaceRecommendationHistory(ctx, pool, results); err != nil {
		log.Warnf("threshold recalc: writing namespace history failed: %v", err)
	}
	return nil
}

func recalculateNodeCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	log := logging.ForOrg(orgID, clusterUUID)
	start, now := recalcDateRange()
	t0 := time.Now()
	defer func() { metrics.ObserveRecommendation("node", t0) }()

	digests, err := QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, now)
	if err != nil {
		return fmt.Errorf("query node digests: %w", err)
	}
	if len(digests) == 0 {
		return nil
	}

	terms, err := LoadTermConfigCached(ctx, pool, orgID, "node")
	if err != nil {
		log.Warnf("threshold recalc: load term config failed, using defaults: %v", err)
		terms = DefaultTermsForPlugin("node")
	}

	nodeSettings, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
	if err != nil {
		log.Warnf("threshold recalc: load threshold settings failed, using defaults: %v", err)
		nodeSettings = DefaultNodeThresholdSettings()
	}

	cfg := NodeRecConfigFromThresholds(nodeSettings)
	recs := RecommendNodes(digests, cfg, nodeSettings, terms)
	if len(recs) == 0 {
		return nil
	}

	var costData *costdata.ClusterCostData
	if config.GetConfig().SavingsEstimatesEnabled {
		costData = fetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
	}
	ApplyNodeSavings(recs, costData)

	validTerms := make([]string, len(terms))
	for i, tc := range terms {
		validTerms[i] = tc.Name
	}
	if err := PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, validTerms); err != nil {
		return fmt.Errorf("persist node recommendations: %w", err)
	}
	metrics.IncRecommendationsWritten("node", len(recs))
	return nil
}

func recalculateGPUCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	if !plugin.EnabledFor("gpu") {
		return nil
	}
	log := logging.ForOrg(orgID, clusterUUID)
	start, now := recalcDateRange()

	if err := MarkContainersWithGPU(ctx, pool, orgID, clusterUUID); err != nil {
		log.Warnf("threshold recalc: marking GPU containers failed: %v", err)
	}

	gpuTerms, err := LoadTermConfigCached(ctx, pool, orgID, "gpu")
	if err != nil {
		log.Warnf("threshold recalc: load GPU term config failed, using defaults: %v", err)
		gpuTerms = DefaultTermsForPlugin("gpu")
	}

	// Read old GPU MIG recommendations before overwriting (for quality metrics).
	oldGPURecs, oldErr := ReadClusterOldGPUMIGRecommendations(ctx, pool, orgID, clusterUUID)
	if oldErr != nil {
		log.Warnf("threshold recalc: reading old GPU MIG recommendations failed: %v", oldErr)
	}

	tGPU := time.Now()
	var gpuCostData *costdata.ClusterCostData
	if config.GetConfig().SavingsEstimatesEnabled {
		gpuCostData = fetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
	}
	if err := StoreGPUClassifications(ctx, pool, orgID, clusterUUID, gpuTerms, gpuCostData); err != nil {
		return fmt.Errorf("store GPU classifications: %w", err)
	}
	if err := ComputeAndPersistNodeGPUTimeSlicingRecs(ctx, pool, orgID, clusterUUID, gpuTerms, gpuCostData); err != nil {
		return fmt.Errorf("persist node GPU time-slicing recommendations: %w", err)
	}
	metrics.ObservePipelinePhase(metrics.PhasePostProcess, tGPU)

	// Write GPU MIG quality metrics.
	if oldGPURecs != nil {
		currentProfiles, _ := ReadCurrentGPUProfiles(ctx, pool, orgID, clusterUUID)
		gpuRecs, _, _, _ := QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, gpuTerms, nil)
		qualityRows := BuildGPUMIGQualityRows(ctx, pool, orgID, clusterUUID, gpuRecs, oldGPURecs, currentProfiles)
		if qualErr := WriteGPUMIGQuality(ctx, pool, qualityRows); qualErr != nil {
			log.Warnf("threshold recalc: writing GPU MIG quality metrics failed: %v", qualErr)
		}
	}

	return nil
}

func recalculatePVCCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	log := logging.ForOrg(orgID, clusterUUID)
	terms, err := LoadTermConfigCached(ctx, pool, orgID, "pvc")
	if err != nil {
		log.Warnf("threshold recalc: load PVC term config failed, using defaults: %v", err)
		terms = DefaultTermsForPlugin("pvc")
	}

	// Read old PVC recommendations before overwriting (for quality metrics).
	oldPVCRecs, oldErr := ReadClusterOldPVCRecommendations(ctx, pool, orgID, clusterUUID)
	if oldErr != nil {
		log.Warnf("threshold recalc: reading old PVC recommendations failed: %v", oldErr)
	}

	tPVC := time.Now()
	results, err := RecommendPVCs(ctx, pool, orgID, clusterUUID, terms)
	metrics.ObserveRecommendation("pvc", tPVC)
	if err != nil {
		return fmt.Errorf("recommend PVCs: %w", err)
	}
	if len(results) == 0 {
		return nil
	}

	start, now := recalcDateRange()
	var costData *costdata.ClusterCostData
	if config.GetConfig().SavingsEstimatesEnabled {
		costData = fetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
	}
	ApplyPVCSavings(results, costData)

	validTerms := make([]string, len(terms))
	for i, tc := range terms {
		validTerms[i] = tc.Name
	}
	if err := WritePVCRecommendations(ctx, pool, results, validTerms); err != nil {
		return fmt.Errorf("write PVC recommendations: %w", err)
	}
	metrics.IncRecommendationsWritten("pvc", len(results))

	// Write PVC quality metrics.
	if oldPVCRecs != nil {
		qualityRows := BuildPVCQualityRows(results, oldPVCRecs, nil)
		if qualErr := WritePVCQuality(ctx, pool, qualityRows); qualErr != nil {
			log.Warnf("threshold recalc: writing PVC quality metrics failed: %v", qualErr)
		}
	}

	return nil
}
