package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

// GetNodeRecommendations handles GET /recommendations/openshift/gpu/timeslicing.
// It computes GPU time-slicing recommendations by querying gpu_container_digests,
// grouping by node × GPU model, and running the time-slicing engine.
//
// TODO(future): Add `current_replicas` field once the operator reports nvidia.com/gpu.replicas from node labels.
//
// order_by sorting:
//   - DB-backed (SQL ORDER BY on gpu_container_digests triple pagination): node_name, cluster_uuid, gpu_model (gpu_model_name alias)
//   - Rejected with HTTP 400: recommended_replicas, confidence, total_node_savings, total_node_savings_usd —
//     these are computed after per-triple aggregation and cannot be pushed down to SQL at scale.
func GetNodeRecommendations(c echo.Context) error {
	tGPU := time.Now()
	defer func() { metrics.ObserveRecommendation("gpu", tGPU) }()

	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgIDStr := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgIDStr)

	opts, err := listoptions.ListAPIOptions(c, listoptions.DefaultNodeRecsOrderBy, listoptions.NodeRecsAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	opts.Limit = capNodeListLimit(opts.Limit)

	pool := database.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	now := time.Now().UTC()

	ctx := c.Request().Context()
	ctx = WithEnrichmentCache(ctx, orgIDStr)

	terms, err := engine.LoadTermConfigCached(ctx, pool, orgIDStr, "gpu")
	if err != nil {
		hlog.Warnf("GetNodeRecommendations: load term config failed: %v", err)
		terms = engine.DefaultTermsForPlugin("gpu")
	}
	start := now.AddDate(0, 0, -engine.MaxWindowDays(terms, 30))

	termFilterRaw := queryparams.FirstFilter(c, "term")
	termFilter, termNormErr := queryparams.NormalizeRecommendationTermFilter(termFilterRaw)
	if termNormErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termNormErr.Error()})
	}
	gpuMinDataDays := engine.MinDataDaysForTerm(terms, termFilter)

	clusterUUIDs, err := getClustersForOrg(ctx, orgIDStr)
	if err != nil {
		hlog.Errorf("GetNodeRecommendations: failed to get clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}

	clusterUUIDs = filterClustersByRBAC(clusterUUIDs, userPerms)

	clusterFilter := queryparams.FirstFilter(c, "cluster")
	clusterUUIDs, clusterFilterMiss := restrictClustersToQueryFilter(clusterUUIDs, clusterFilter)
	if clusterFilterMiss {
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, model.NodeRecommendationListResponse{
			Meta: model.NodeRecommendationMeta{
				Count:       0,
				Limit:       opts.Limit,
				Offset:      opts.Offset,
				Currency:    resolveListCurrencyFromRequest(c, orgIDStr),
				MinDataDays: gpuMinDataDays,
			},
			Data:  []model.NodeGPURecommendation{},
			Links: buildNodeLinks(c.Request(), 0, opts.Limit, opts.Offset),
		})
	}

	nodeNameFilter := queryparams.FirstFilter(c, "node")
	gpuModelFilter := queryparams.FirstFilter(c, "gpu_model")

	if len(clusterUUIDs) == 0 {
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, model.NodeRecommendationListResponse{
			Meta: model.NodeRecommendationMeta{
				Count:       0,
				Limit:       opts.Limit,
				Offset:      opts.Offset,
				Currency:    resolveListCurrencyFromRequest(c, orgIDStr),
				MinDataDays: gpuMinDataDays,
			},
			Data:  []model.NodeGPURecommendation{},
			Links: buildNodeLinks(c.Request(), 0, opts.Limit, opts.Offset),
		})
	}

	if queryparams.GroupByField(c, "cluster") {
		return getGPUTSRecsGrouped(
			c, ctx, pool, hlog, orgIDStr, userPerms, opts,
			clusterUUIDs, clusterFilter, nodeNameFilter, gpuModelFilter, termFilter,
		)
	}

	hasPersisted, persistErr := orgHasPersistedNodeGPUTimeslicingRecs(ctx, pool, orgIDStr)
	if persistErr != nil {
		hlog.Warnf("GetNodeRecommendations: persisted table check failed: %v", persistErr)
	}
	if hasPersisted {
		return respondNodeGPURecommendationsFromTable(
			c, ctx, pool, orgIDStr, userPerms, opts, clusterUUIDs,
			clusterFilter, nodeNameFilter, gpuModelFilter, termFilter,
			RequestIncludesExplanation(c.QueryParam("include")),
		)
	}

	if !engine.GPUOrderColumnSupportsTriplePagination(opts.OrderBy) {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": fmt.Sprintf("order_by %q cannot be paginated at scale; use node_name, cluster_uuid, or gpu_model", opts.OrderBy),
		})
	}

	return respondNodeGPURecommendationsTripleSQL(
		c, ctx, pool, orgIDStr, userPerms, opts, terms, clusterUUIDs, start, now,
		clusterFilter, nodeNameFilter, gpuModelFilter, termFilter,
	)
}

// restrictClustersToQueryFilter narrows clusterUUIDs when filter[cluster] or the
// cluster_uuid flat alias is set. The second return value is true when the
// requested cluster is not in the allowed (RBAC-filtered) set.
func restrictClustersToQueryFilter(clusterUUIDs []string, clusterFilter string) ([]string, bool) {
	if clusterFilter == "" {
		return clusterUUIDs, false
	}
	for _, u := range clusterUUIDs {
		if u == clusterFilter {
			return []string{clusterFilter}, false
		}
	}
	return nil, true
}

func respondNodeGPURecommendationsTripleSQL(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	orgIDStr string,
	userPerms map[string][]string,
	opts listoptions.ListOptions,
	terms []engine.TermConfig,
	clusterUUIDs []string,
	start, now time.Time,
	clusterFilter, nodeNameFilter, gpuModelFilter, termFilter string,
) error {
	hlog := requestLogger(c, orgIDStr)

	cursor, hasCursor, cursorErr := applyNodeGPUCursor(c, opts.OrderBy)
	if cursorErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": cursorErr.Error()})
	}
	if hasCursor {
		opts.Offset = 0
	}

	pageLimit := opts.Limit
	if opts.Format == listoptions.ResponseFormatCSV {
		pageLimit = config.GetConfig().RecordLimitCSV
		if pageLimit <= 0 {
			pageLimit = listoptions.MaxLimit
		}
	}
	if pageLimit <= 0 {
		pageLimit = listoptions.DefaultLimit
	}

	const tripleBatchSize = 50
	var allRecs []model.NodeGPURecommendation
	var gpuClusterErrors []error
	var firstTripleCluster string
	tripleOffset := 0
	var seek *engine.NodeGPUTripleSeek
	if hasCursor {
		seek = nodeGPUCursorToTripleSeek(cursor)
	}

	for {
		triples, err := engine.ListNodeGPUTriplesPage(
			ctx, pool, orgIDStr, clusterUUIDs, start, now, now,
			nodeNameFilter, gpuModelFilter, opts.OrderBy, opts.OrderHow == listoptions.OrderDesc,
			tripleBatchSize, tripleOffset, seek,
		)
		if err != nil {
			hlog.Errorf("GetNodeRecommendations: triple page failed: %v", err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load node GPU recommendations"})
		}
		if len(triples) == 0 {
			break
		}
		if firstTripleCluster == "" {
			firstTripleCluster = triples[0].ClusterUUID
		}
		batchRecs, batchErrs := expandNodeGPURecsFromTriples(ctx, pool, orgIDStr, terms, triples, start, now)
		allRecs = append(allRecs, batchRecs...)
		gpuClusterErrors = append(gpuClusterErrors, batchErrs...)
		tripleOffset += len(triples)
		seek = nil
		if len(triples) < tripleBatchSize {
			break
		}
	}

	var warnings []string
	if len(gpuClusterErrors) > 0 {
		hlog.Warnf("GetNodeRecommendations: incomplete GPU queries: %v", errors.Join(gpuClusterErrors...))
		switch len(gpuClusterErrors) {
		case 1:
			warnings = append(warnings, fmt.Sprintf("GPU enrichment failed: %s", briefGPUEnrichmentErr(gpuClusterErrors[0])))
		default:
			warnings = append(warnings, fmt.Sprintf("GPU data unavailable for %d clusters", len(gpuClusterErrors)))
		}
	}

	allRecs = filterNodeRecsByRBAC(allRecs, userPerms)

	var tagFilterErr error
	allRecs, tagFilterErr = applyNodeGPURecTagFilters(ctx, c, pool, orgIDStr, allRecs)
	if tagFilterErr != nil {
		return tagFilterErr
	}

	if allRecs == nil {
		allRecs = []model.NodeGPURecommendation{}
	}

	if termFilter != "" {
		allRecs = filterNodeRecs(allRecs, "", "", termFilter)
	}

	totalCount := len(allRecs)
	paged, hasNext, nextCursor, err := paginateNodeGPURecs(allRecs, listoptions.ListOptions{
		Limit:    pageLimit,
		Offset:   opts.Offset,
		OrderBy:  opts.OrderBy,
		OrderHow: opts.OrderHow,
	}, cursor, hasCursor)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	if opts.Format == listoptions.ResponseFormatCSV && pageLimit > 0 && len(paged) > pageLimit {
		paged = paged[:pageLimit]
		hasNext = false
		nextCursor = ""
	}

	userCurrency := resolveUserCurrency(ctx, orgIDStr)
	convertNodeGPURecsToUserCurrency(ctx, orgIDStr, paged, userCurrency)
	refCluster := clusterFilter
	if refCluster == "" {
		refCluster = firstTripleCluster
	}
	displayCurrency := userCurrency
	if refCluster != "" {
		sc := fetchClusterCurrency(ctx, orgIDStr, refCluster)
		r := fetchExchangeRate(ctx, orgIDStr, sc, userCurrency)
		if r == 1.0 && sc != userCurrency {
			displayCurrency = sc
		}
	}
	totalSavings := sumNodeGPUSavings(paged, displayCurrency)
	return respondNodeGPURecommendations(c, listoptions.ListOptions{
		Limit:    pageLimit,
		Offset:   opts.Offset,
		OrderBy:  opts.OrderBy,
		OrderHow: opts.OrderHow,
		Format:   opts.Format,
	}, totalCount, paged, totalSavings, warnings, displayCurrency, hasNext, nextCursor,
		engine.MinDataDaysForTerm(terms, termFilter))
}

func nodeGPUCursorToTripleSeek(cursor NodeGPUCursor) *engine.NodeGPUTripleSeek {
	if cursor.ClusterUUID == "" {
		return nil
	}
	seek := &engine.NodeGPUTripleSeek{
		ClusterUUID: cursor.ClusterUUID,
		NodeName:    cursor.NodeName,
		GPUModel:    cursor.GPUModel,
	}
	if len(cursor.SortValue) > 0 {
		if sortVal, err := decodeCursorSortValue(cursor.SortValue); err == nil {
			seek.SortValue = sortVal
		}
	}
	return seek
}

func expandNodeGPURecsFromTriples(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgIDStr string,
	terms []engine.TermConfig,
	triples []engine.NodeGPUTriple,
	start, now time.Time,
) ([]model.NodeGPURecommendation, []error) {
	costProvider := getGPUCostProvider()
	clusterRates := make(map[string]*float32)
	for _, t := range triples {
		if _, ok := clusterRates[t.ClusterUUID]; ok {
			continue
		}
		var gpuRate *float32
		if costProvider != nil && orgIDStr != "" {
			if cd := GetCachedCostRates(ctx, orgIDStr, t.ClusterUUID, start, now); cd != nil {
				if rate := engine.GPUMonthlyRate(cd); rate > 0 {
					r := float32(rate)
					gpuRate = &r
				}
			}
		}
		clusterRates[t.ClusterUUID] = gpuRate
	}

	var allRecs []model.NodeGPURecommendation
	var gpuClusterErrors []error
	for _, tr := range triples {
		f := &engine.GPUQueryFilters{NodeNameExact: tr.NodeName, GPUModelExact: tr.GPUModel}
		gpuRecs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, orgIDStr, tr.ClusterUUID, start, now, terms, f)
		if err != nil {
			gpuClusterErrors = append(gpuClusterErrors, fmt.Errorf("cluster %s: %w", tr.ClusterUUID, err))
			continue
		}
		if gpuRecs == nil {
			continue
		}
		groups := engine.GroupGPURecsByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, tr.ClusterUUID)
		gpuRate := clusterRates[tr.ClusterUUID]
		for _, group := range groups {
			if group.NodeName != tr.NodeName || group.GPUModel != tr.GPUModel {
				continue
			}
			tsRec := engine.ComputeNodeTimeslicingRecForOrg(ctx, pool, orgIDStr, group, gpuRate, now)
			if tsRec == nil {
				continue
			}
			currency := fetchClusterCurrency(ctx, orgIDStr, tr.ClusterUUID)
			allRecs = append(allRecs, toNodeGPURecommendation(tsRec, currency))
		}
	}
	return allRecs, gpuClusterErrors
}

func respondNodeGPURecommendations(
	c echo.Context,
	opts listoptions.ListOptions,
	totalCount int,
	data []model.NodeGPURecommendation,
	totalSavings *money.MoneyAmount,
	warnings []string,
	nodeCurrency string,
	hasNext bool,
	nextCursor string,
	minDataDays int,
) error {
	setRecommendationNoStore(c)
	if opts.Format == listoptions.ResponseFormatCSV {
		if data == nil {
			data = []model.NodeGPURecommendation{}
		}
		return streamCSV(c, csvFilename("gpu-timeslicing"), func(ctx context.Context, w io.Writer) error {
			return generateNodeGPURecCSV(ctx, w, data)
		})
	}
	links := buildNodeLinks(c.Request(), totalCount, opts.Limit, opts.Offset)
	resp := model.NodeRecommendationListResponse{
		Meta: model.NodeRecommendationMeta{
			Count:                   totalCount,
			Limit:                   opts.Limit,
			Offset:                  opts.Offset,
			HasNext:                 hasNext,
			NextCursor:              nextCursor,
			Currency:                nodeCurrency,
			MinDataDays:             minDataDays,
			EstimatedMonthlySavings: totalSavings,
		},
		Data:     data,
		Links:    links,
		Warnings: warnings,
	}
	if hasNext && nextCursor != "" {
		q := c.Request().URL.Query()
		q.Set("limit", strconv.Itoa(opts.Limit))
		q.Del("offset")
		q.Set("after", nextCursor)
		params, _ := url.PathUnescape(q.Encode())
		resp.Links.Next = fmt.Sprintf("%v?%v", c.Request().URL.Path, params)
	}
	return c.JSON(http.StatusOK, resp)
}

func nodeGPUSortValue(r model.NodeGPURecommendation, orderBy string) interface{} {
	switch orderBy {
	case "cluster_uuid":
		return r.ClusterUUID
	case "gpu_model", "gpu_model_name":
		return r.GPUModel
	case "recommended_replicas":
		return r.RecommendedReplicas
	case "confidence":
		return r.Confidence
	case "total_node_savings", "total_node_savings_usd", "estimated_monthly_savings":
		return savingsObjectUSDValue(r.EstimatedMonthlySavings)
	default:
		return r.NodeName
	}
}

const maxGPUWarningErrLen = 160

func briefGPUEnrichmentErr(err error) string {
	if err == nil {
		return "unknown error"
	}
	s := err.Error()
	if len(s) <= maxGPUWarningErrLen {
		return s
	}
	return s[:maxGPUWarningErrLen] + "..."
}

func collectNodeGPURecsForCluster(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgIDStr, clusterUUID string,
	start, now time.Time,
	terms []engine.TermConfig,
	costProvider costdata.CostDataProvider,
) ([]model.NodeGPURecommendation, error) {
	gpuRecs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, orgIDStr, clusterUUID, start, now, terms, nil)
	if err != nil {
		return nil, err
	}
	if gpuRecs == nil {
		return nil, nil
	}

	var gpuRate *float32
	if costProvider != nil && orgIDStr != "" {
		if cd := GetCachedCostRates(ctx, orgIDStr, clusterUUID, start, now); cd != nil {
			if rate := engine.GPUMonthlyRate(cd); rate > 0 {
				r := float32(rate)
				gpuRate = &r
			}
		}
	}

	groups := engine.GroupGPURecsByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID)
	var recs []model.NodeGPURecommendation
	for _, group := range groups {
		tsRec := engine.ComputeNodeTimeslicingRecForOrg(ctx, pool, orgIDStr, group, gpuRate, now)
		if tsRec == nil {
			continue
		}
		currency := fetchClusterCurrency(ctx, orgIDStr, clusterUUID)
		recs = append(recs, toNodeGPURecommendation(tsRec, currency))
	}
	return recs, nil
}

func getClustersForOrg(ctx context.Context, orgID string) ([]string, error) {
	return clustercache.GetClustersForOrg(ctx, orgID)
}

func toNodeGPURecommendation(tsRec *engine.TimeslicingRec, currency string) model.NodeGPURecommendation {
	rec := model.NodeGPURecommendation{
		NodeName:                tsRec.NodeName,
		ClusterUUID:             tsRec.ClusterUUID,
		Term:                    tsRec.Term,
		RecommendationType:      "gpu_time_slicing",
		GPUModel:                tsRec.GPUModel,
		RecommendedReplicas:     tsRec.RecommendedReplicas,
		SavingsPerGPU:           money.FormatUSDPtrToAmountPtr(tsRec.SavingsPerGPU, currency),
		EstimatedMonthlySavings: money.FormatUSDPtrToAmountPtr(tsRec.TotalNodeSavings, currency),
		Confidence:              tsRec.Confidence,
		ConfidenceLevel:         tsRec.Confidence,
		NotificationCodes:       tsRec.NotificationCodes,
	}
	for _, c := range tsRec.CandidateContainers {
		rec.CandidateContainers = append(rec.CandidateContainers, model.NodeContainerRef{
			Namespace:      c.Namespace,
			Workload:       c.Workload,
			Container:      c.Container,
			SMActiveAvg:    c.SMActiveAvg,
			Classification: string(c.Classification),
		})
	}
	for _, c := range tsRec.ImpactedContainers {
		rec.ImpactedContainers = append(rec.ImpactedContainers, model.NodeContainerRef{
			Namespace:      c.Namespace,
			Workload:       c.Workload,
			Container:      c.Container,
			SMActiveAvg:    c.SMActiveAvg,
			Classification: string(c.Classification),
		})
	}
	if rec.CandidateContainers == nil {
		rec.CandidateContainers = []model.NodeContainerRef{}
	}
	if rec.ImpactedContainers == nil {
		rec.ImpactedContainers = []model.NodeContainerRef{}
	}
	rec.Classification = dominantClassification(rec.CandidateContainers)
	return rec
}

func filterNodeRecs(recs []model.NodeGPURecommendation, nodeName, gpuModel, term string) []model.NodeGPURecommendation {
	if nodeName == "" && gpuModel == "" && term == "" {
		return recs
	}
	filtered := make([]model.NodeGPURecommendation, 0, len(recs))
	for _, r := range recs {
		if nodeName != "" && !strings.EqualFold(r.NodeName, nodeName) {
			continue
		}
		if gpuModel != "" && !strings.Contains(strings.ToLower(r.GPUModel), strings.ToLower(gpuModel)) {
			continue
		}
		if term != "" && !strings.EqualFold(r.Term, term) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// sortNodeRecs sorts recommendations in-place by the given column and direction.
func sortNodeRecs(recs []model.NodeGPURecommendation, orderBy, orderHow string) {
	if len(recs) <= 1 {
		return
	}
	desc := orderHow == listoptions.OrderDesc
	slices.SortFunc(recs, func(a, b model.NodeGPURecommendation) int {
		if desc {
			a, b = b, a
		}
		return compareNodeRecs(a, b, orderBy)
	})
}

func compareNodeRecs(a, b model.NodeGPURecommendation, orderBy string) int {
	var primary int
	switch orderBy {
	case "cluster_uuid":
		primary = strings.Compare(a.ClusterUUID, b.ClusterUUID)
	case "gpu_model", "gpu_model_name":
		primary = strings.Compare(a.GPUModel, b.GPUModel)
	case "recommended_replicas":
		primary = cmpInt(a.RecommendedReplicas, b.RecommendedReplicas)
	case "confidence":
		primary = cmpFloat32(a.Confidence, b.Confidence)
	case "total_node_savings", "total_node_savings_usd", "estimated_monthly_savings":
		primary = cmpFloat64(savingsObjectUSDValue(a.EstimatedMonthlySavings), savingsObjectUSDValue(b.EstimatedMonthlySavings))
	default: // node_name
		primary = strings.Compare(a.NodeName, b.NodeName)
	}
	if primary != 0 {
		return primary
	}
	if c := strings.Compare(a.ClusterUUID, b.ClusterUUID); c != 0 {
		return c
	}
	if c := strings.Compare(a.NodeName, b.NodeName); c != 0 {
		return c
	}
	if c := strings.Compare(a.GPUModel, b.GPUModel); c != 0 {
		return c
	}
	return strings.Compare(a.Term, b.Term)
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmpFloat32(a, b float32) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func derefFloat32(p *float32) float32 {
	if p == nil {
		return 0
	}
	return *p
}

func cmpFloat64(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func savingsObjectUSDValue(s *money.MoneyAmount) float64 {
	if s == nil || s.Value == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s.Value, 64)
	if err != nil {
		return 0
	}
	return v
}

func sumNodeGPUSavings(recs []model.NodeGPURecommendation, currency string) *money.MoneyAmount {
	var sum float64
	hasSavings := false
	for _, r := range recs {
		v := savingsObjectUSDValue(r.EstimatedMonthlySavings)
		if v > 0 || r.EstimatedMonthlySavings != nil {
			sum += v
			hasSavings = true
		}
	}
	if !hasSavings {
		return nil
	}
	cents := money.USDToCents(sum)
	return money.FormatCentsToAmountPtr(&cents, currency)
}

// dominantClassification picks the most common classification from candidate containers.
// On ties, the most severe wins: idle > underutilized > compute_bound_underutil > well_utilized.
func dominantClassification(containers []model.NodeContainerRef) string {
	if len(containers) == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, c := range containers {
		if c.Classification != "" {
			counts[c.Classification]++
		}
	}
	if len(counts) == 0 {
		return ""
	}
	severityOrder := map[string]int{
		"idle":                    4,
		"underutilized":           3,
		"compute_bound_underutil": 2,
		"well_utilized":           1,
	}
	var best string
	var bestCount int
	for cls, cnt := range counts {
		if cnt > bestCount || (cnt == bestCount && severityOrder[cls] > severityOrder[best]) {
			best = cls
			bestCount = cnt
		}
	}
	return best
}

// applyNodeGPURecTagFilters keeps node GPU recs with at least one candidate container matching tag filters.
func applyNodeGPURecTagFilters(
	ctx context.Context,
	c echo.Context,
	pool *pgxpool.Pool,
	orgID string,
	recs []model.NodeGPURecommendation,
) ([]model.NodeGPURecommendation, error) {
	if !config.TagsFeatureEnabled() {
		return recs, nil
	}
	tagFilters, err := parseTagFiltersFromRequest(c)
	if err != nil {
		return recs, c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	if len(tagFilters) == 0 {
		return recs, nil
	}
	allowedKeys, keysErr := model.MatchingContainerKeys(ctx, pool, orgID, tagFilters)
	if keysErr != nil {
		return recs, c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to apply tag filters"})
	}
	filtered := recs[:0]
	for _, r := range recs {
		for _, cand := range r.CandidateContainers {
			if allowedKeys.Contains(r.ClusterUUID, cand.Namespace, cand.Workload, cand.Container) {
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered, nil
}

// applyNodePagination returns the slice corresponding to the given offset and limit.
func applyNodePagination(recs []model.NodeGPURecommendation, offset, limit int) []model.NodeGPURecommendation {
	if offset >= len(recs) {
		return []model.NodeGPURecommendation{}
	}
	recs = recs[offset:]
	if limit > 0 && limit < len(recs) {
		recs = recs[:limit]
	}
	return recs
}

func paginateNodeGPURecs(recs []model.NodeGPURecommendation, opts listoptions.ListOptions, cursor NodeGPUCursor, hasCursor bool) ([]model.NodeGPURecommendation, bool, string, error) {
	if len(recs) == 0 {
		return []model.NodeGPURecommendation{}, false, "", nil
	}
	start := 0
	if hasCursor {
		found := false
		for i, r := range recs {
			if nodeGPURecAfterCursor(r, cursor, opts.OrderBy, opts.OrderHow) {
				start = i
				found = true
				break
			}
		}
		if !found {
			return []model.NodeGPURecommendation{}, false, "", nil
		}
	} else if opts.Offset > 0 {
		if opts.Offset >= len(recs) {
			return []model.NodeGPURecommendation{}, false, "", nil
		}
		start = opts.Offset
	}
	end := len(recs)
	if opts.Limit > 0 {
		end = start + opts.Limit + 1
		if end > len(recs) {
			end = len(recs)
		}
	}
	slice := recs[start:end]
	hasNext := opts.Limit > 0 && len(slice) > opts.Limit
	var nextCursor string
	if hasNext {
		last := slice[opts.Limit-1]
		nextCursor = nodeGPUNextCursor(last, nodeGPUSortValue(last, opts.OrderBy), opts.OrderBy)
		slice = slice[:opts.Limit]
	}
	return slice, hasNext, nextCursor, nil
}

func nodeGPURecAfterCursor(r model.NodeGPURecommendation, cursor NodeGPUCursor, orderBy, orderHow string) bool {
	if len(cursor.SortValue) > 0 {
		sortVal, err := decodeCursorSortValue(cursor.SortValue)
		if err != nil {
			return false
		}
		cur := nodeGPUSortValue(r, orderBy)
		if orderHow == listoptions.OrderDesc {
			return compareSortValues(cur, sortVal) < 0 || (compareSortValues(cur, sortVal) == 0 && nodeGPURecTieAfter(r, cursor))
		}
		return compareSortValues(cur, sortVal) > 0 || (compareSortValues(cur, sortVal) == 0 && nodeGPURecTieAfter(r, cursor))
	}
	return nodeGPURecTieAfter(r, cursor)
}

func nodeGPURecTieAfter(r model.NodeGPURecommendation, cursor NodeGPUCursor) bool {
	tie := r.ClusterUUID + "\x00" + r.NodeName + "\x00" + r.GPUModel + "\x00" + r.Term
	cur := cursor.ClusterUUID + "\x00" + cursor.NodeName + "\x00" + cursor.GPUModel + "\x00" + cursor.Term
	return tie > cur
}

// buildNodeLinks produces pagination links consistent with the standard Collection shape.
func buildNodeLinks(req *http.Request, count, limit, offset int) model.NodeRecommendationLinks {
	q := req.URL.Query()
	makeLink := func(o int) string {
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(o))
		params, _ := url.PathUnescape(q.Encode())
		return fmt.Sprintf("%v?%v", req.URL.Path, params)
	}

	links := model.NodeRecommendationLinks{
		First: makeLink(0),
	}
	if limit <= 0 {
		return links
	}
	lastOffset := 0
	if count > 0 {
		lastOffset = ((count - 1) / limit) * limit
	}
	links.Last = makeLink(lastOffset)
	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		links.Previous = makeLink(prev)
	}
	if offset+limit < count {
		links.Next = makeLink(offset + limit)
	}
	return links
}

// filterClustersByRBAC restricts the cluster UUIDs to those the user has
// openshift.cluster read permission for. Returns the full list when RBAC is
// disabled, the user has a global wildcard, or cluster permissions are "*".
func filterClustersByRBAC(clusterUUIDs []string, userPerms map[string][]string) []string {
	if !config.GetConfig().RBACEnabled {
		return clusterUUIDs
	}
	if _, ok := userPerms["*"]; ok {
		return clusterUUIDs
	}
	clusterPerms, hasCluster := userPerms["openshift.cluster"]
	if !hasCluster {
		return clusterUUIDs
	}
	if utils.StringInSlice("*", clusterPerms) {
		return clusterUUIDs
	}
	allowed := make(map[string]bool, len(clusterPerms))
	for _, p := range clusterPerms {
		allowed[p] = true
	}
	filtered := make([]string, 0, len(clusterUUIDs))
	for _, uuid := range clusterUUIDs {
		if allowed[uuid] {
			filtered = append(filtered, uuid)
		}
	}
	return filtered
}

// openshiftNodeRBACScope reports whether node-level RBAC restricts visible node names.
// When restrict is false, all nodes in allowed clusters are visible.
func openshiftNodeRBACScope(userPerms map[string][]string) (restrict bool, allowed []string) {
	if !config.GetConfig().RBACEnabled {
		return false, nil
	}
	if _, ok := userPerms["*"]; ok {
		return false, nil
	}
	nodePerms, hasNode := userPerms["openshift.node"]
	if !hasNode {
		return false, nil
	}
	if utils.StringInSlice("*", nodePerms) {
		return false, nil
	}
	return true, nodePerms
}


// filterNodeRecsByRBAC restricts node recommendations to those whose NodeName
// the user has openshift.node read permission for. Returns the full list when
// RBAC is disabled, the user has a global wildcard, or node permissions are "*".
func filterNodeRecsByRBAC(recs []model.NodeGPURecommendation, userPerms map[string][]string) []model.NodeGPURecommendation {
	restrict, allowed := openshiftNodeRBACScope(userPerms)
	if !restrict {
		return recs
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, n := range allowed {
		allowedSet[n] = true
	}
	filtered := make([]model.NodeGPURecommendation, 0, len(recs))
	for _, r := range recs {
		if allowedSet[r.NodeName] {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
