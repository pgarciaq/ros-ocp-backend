package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
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
			Help: "Threshold-triggered recommendation recalculations per org, type, and outcome",
		},
		[]string{"org_id", "recommendation_type", "status"},
	)

	// clusterRecalcFunc runs recommendation logic for one cluster; tests may replace it.
	clusterRecalcFunc = defaultRecalculateCluster

	// thresholdRecalcHook runs at the start of TriggerThresholdRecalculationAsync (tests only).
	thresholdRecalcHook func(orgID, recType string)
)

// SetClusterRecalcFuncForTest replaces the per-cluster recalculation function and returns a restore func.
func SetClusterRecalcFuncForTest(fn func(context.Context, *pgxpool.Pool, string, string, string) error) func() {
	prev := clusterRecalcFunc
	clusterRecalcFunc = fn
	return func() { clusterRecalcFunc = prev }
}

// SetThresholdRecalcHookForTest registers a hook invoked when async recalculation is triggered.
func SetThresholdRecalcHookForTest(hook func(orgID, recType string)) {
	thresholdRecalcHook = hook
}

// ClearThresholdRecalcHookForTest removes the test hook.
func ClearThresholdRecalcHookForTest() {
	thresholdRecalcHook = nil
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
	if thresholdRecalcHook != nil {
		thresholdRecalcHook(orgID, recType)
	}
	go func() {
		ctx := context.Background()
		RecalculateThresholdsForOrg(ctx, pool, orgID, recType)
	}()
}

// RecalculateThresholdsForOrg re-runs the recommendation engine for all clusters in an org.
func RecalculateThresholdsForOrg(ctx context.Context, pool *pgxpool.Pool, orgID, recType string) {
	log := logging.ForOrgOnly(orgID)
	started := time.Now()

	clusters, err := ListClustersForOrg(ctx, pool, orgID)
	if err != nil {
		log.WithFields(map[string]interface{}{
			"msg":                 "threshold recalculation failed",
			"recommendation_type": recType,
			"error":               err.Error(),
		}).Error("unable to list clusters")
		thresholdRecalculationTotal.WithLabelValues(orgID, recType, "error").Inc()
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
		thresholdRecalculationTotal.WithLabelValues(orgID, recType, "success").Inc()
		return
	}

	sem := make(chan struct{}, thresholdRecalcConcurrency())
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

	for _, clusterUUID := range clusters {
		wg.Add(1)
		go func(clusterID string) {
			defer wg.Done()
			if currentHash != "" {
				skip, err := shouldSkipClusterThresholdRecalc(ctx, pool, orgID, clusterID, recType, currentHash)
				if err != nil {
					logging.ForOrg(orgID, clusterID).Warnf("threshold recalc skip check failed: %v", err)
				} else if skip {
					mu.Lock()
					skipped++
					mu.Unlock()
					thresholdRecalculationTotal.WithLabelValues(orgID, recType, "skipped").Inc()
					return
				}
			}

			sem <- struct{}{}
			defer func() { <-sem }()

			if err := clusterRecalcFunc(ctx, pool, orgID, clusterID, recType); err != nil {
				logging.ForOrg(orgID, clusterID).WithFields(map[string]interface{}{
					"msg":                 "threshold recalculation cluster failed",
					"recommendation_type": recType,
					"error":               err.Error(),
				}).Warn("threshold recalculation cluster failed")
				thresholdRecalculationTotal.WithLabelValues(orgID, recType, "error").Inc()
				return
			}
			mu.Lock()
			processed++
			mu.Unlock()
			if currentHash != "" {
				if err := setClusterRecalcHash(ctx, pool, orgID, clusterID, recType, currentHash); err != nil {
					logging.ForOrg(orgID, clusterID).Warnf("threshold recalc hash persist failed: %v", err)
				}
			}
			thresholdRecalculationTotal.WithLabelValues(orgID, recType, "success").Inc()
		}(clusterUUID)
	}
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

	oldRecs, err := ReadClusterOldRecommendations(ctx, pool, orgID, clusterUUID)
	if err != nil {
		log.Warnf("threshold recalc: reading old recommendations failed: %v", err)
		oldRecs = nil
	}

	totalWritten := 0
	tRec := time.Now()
	err = RecommendWorkloadsStreaming(ctx, pool, orgID, clusterUUID, start, now, oomCfg, func(batch []ContainerRec) error {
		ApplySavingsEstimates(batch, costData)
		if oldRecs != nil {
			adoptedKeys := FindAdoptedContainers(batch, oldRecs)
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
		metrics.IncRecommendationsWritten("container", totalWritten)
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

	tGPU := time.Now()
	if err := StoreGPUClassifications(ctx, pool, orgID, clusterUUID, gpuTerms); err != nil {
		return fmt.Errorf("store GPU classifications: %w", err)
	}
	metrics.ObservePipelinePhase("gpu_enrichment", tGPU)

	// Time-slicing is evaluated at API read time; re-run classification path so persisted
	// gpu_classification reflects new thresholds. Exercise timeslicing heuristics for observability.
	gpuRecs, nodeMap, nodeLastSeen, err := QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, gpuTerms, nil)
	if err != nil {
		return fmt.Errorf("query GPU recommendations: %w", err)
	}
	if len(gpuRecs) == 0 {
		return nil
	}

	var gpuRate *float32
	if config.GetConfig().SavingsEstimatesEnabled {
		if costData := fetchRecalcCostData(ctx, orgID, clusterUUID, start, now); costData != nil {
			for _, recs := range gpuRecs {
				for _, rec := range recs {
					ApplyGPUSavings(rec, costData)
				}
			}
			if rate := GPUMonthlyRate(costData); rate > 0 {
				r := float32(rate)
				gpuRate = &r
			}
		}
	}

	for _, group := range groupGPURecsByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID) {
		ComputeNodeTimeslicingRecForOrg(ctx, pool, orgID, group, gpuRate, now)
	}
	return nil
}

func recalculatePVCCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	terms, err := LoadTermConfigCached(ctx, pool, orgID, "pvc")
	if err != nil {
		logging.ForOrg(orgID, clusterUUID).Warnf("threshold recalc: load PVC term config failed, using defaults: %v", err)
		terms = DefaultTermsForPlugin("pvc")
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
	return nil
}

func groupGPURecsByNodeAndModel(gpuRecs map[string][]*GPURec, nodeMap map[string]string, nodeLastSeen map[string]time.Time, clusterUUID string) []NodeGPUGroup {
	type groupKey struct {
		node  string
		model string
		term  string
	}
	grouped := map[groupKey]*NodeGPUGroup{}

	for key, recs := range gpuRecs {
		nodeName := nodeMap[key]
		if nodeName == "" {
			continue
		}
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 {
			continue
		}
		for _, rec := range recs {
			gk := groupKey{node: nodeName, model: rec.GPUModelName, term: rec.Term}
			g, ok := grouped[gk]
			if !ok {
				g = &NodeGPUGroup{
					NodeName:    nodeName,
					ClusterUUID: clusterUUID,
					GPUModel:    rec.GPUModelName,
					Term:        rec.Term,
					LastSeen:    nodeLastSeen[nodeName],
				}
				grouped[gk] = g
			}
			g.Containers = append(g.Containers, NodeGPUContainer{
				Namespace: parts[0],
				Workload:  parts[1],
				Container: parts[2],
				Rec:       rec,
			})
		}
	}

	result := make([]NodeGPUGroup, 0, len(grouped))
	for _, g := range grouped {
		result = append(result, *g)
	}
	return result
}
