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

	"golang.org/x/sync/errgroup"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

// GetNodeRecommendations handles GET /recommendations/openshift/gpu/timeslicing.
// It computes GPU time-slicing recommendations by querying gpu_container_digests,
// grouping by node × GPU model, and running the time-slicing engine.
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
				Count:  0,
				Limit:  opts.Limit,
				Offset: opts.Offset,
			},
			Data:     []model.NodeGPURecommendation{},
			Links:    buildNodeLinks(c.Request(), 0, opts.Limit, opts.Offset),
			Currency: costdata.DefaultCurrency,
		})
	}

	nodeNameFilter := queryparams.FirstFilter(c, "node")
	gpuModelFilter := queryparams.FirstFilter(c, "gpu_model")
	termFilterRaw := queryparams.FirstFilter(c, "term")
	termFilter, termErr := queryparams.NormalizeRecommendationTermFilter(termFilterRaw)
	if termErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termErr.Error()})
	}

	useTripleSQL := termFilter == "" &&
		opts.Format != listoptions.ResponseFormatCSV &&
		engine.GPUOrderColumnSupportsTriplePagination(opts.OrderBy) &&
		len(clusterUUIDs) > 0

	if useTripleSQL {
		return respondNodeGPURecommendationsTripleSQL(
			c, ctx, pool, orgIDStr, userPerms, opts, terms, clusterUUIDs, start, now,
			clusterFilter, nodeNameFilter, gpuModelFilter,
		)
	}

	costProvider := getGPUCostProvider()

	type clusterResult struct {
		recs []model.NodeGPURecommendation
		err  error
	}
	resultsByIdx := make([]clusterResult, len(clusterUUIDs))

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(5)
	for i, clusterUUID := range clusterUUIDs {
		i, clusterUUID := i, clusterUUID
		eg.Go(func() error {
			recs, err := collectNodeGPURecsForCluster(egCtx, pool, orgIDStr, clusterUUID, start, now, terms, costProvider)
			resultsByIdx[i] = clusterResult{recs: recs, err: err}
			return nil
		})
	}
	_ = eg.Wait()

	var allRecs []model.NodeGPURecommendation
	var gpuClusterErrors []error
	for i, clusterUUID := range clusterUUIDs {
		cr := resultsByIdx[i]
		if cr.err != nil {
			hlog.Warnf("GetNodeRecommendations: failed for cluster %s: %v", clusterUUID, cr.err)
			gpuClusterErrors = append(gpuClusterErrors, fmt.Errorf("cluster %s: %w", clusterUUID, cr.err))
			continue
		}
		allRecs = append(allRecs, cr.recs...)
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

	allRecs = filterNodeRecs(allRecs, nodeNameFilter, gpuModelFilter, termFilter)

	var tagFilterErr error
	allRecs, tagFilterErr = applyNodeGPURecTagFilters(ctx, c, pool, orgIDStr, allRecs)
	if tagFilterErr != nil {
		return tagFilterErr
	}

	if allRecs == nil {
		allRecs = []model.NodeGPURecommendation{}
	}

	var totalSavings *float32
	var sum float32
	hasSavings := false
	for _, r := range allRecs {
		if r.TotalNodeSavingsUSD != nil {
			sum += *r.TotalNodeSavingsUSD
			hasSavings = true
		}
	}
	if hasSavings {
		totalSavings = &sum
	}

	totalCount := len(allRecs)
	sortNodeRecs(allRecs, opts.OrderBy, opts.OrderHow)
	paged := applyNodePagination(allRecs, opts.Offset, opts.Limit)

	nodeCurrency := costdata.DefaultCurrency
	if clusterFilter != "" {
		nodeCurrency = fetchClusterCurrency(ctx, orgIDStr, clusterFilter)
	} else if len(clusterUUIDs) > 0 {
		nodeCurrency = fetchClusterCurrency(ctx, orgIDStr, clusterUUIDs[0])
	}
	hasNext := opts.Limit > 0 && len(paged) > 0 && opts.Offset+opts.Limit < totalCount
	return respondNodeGPURecommendations(c, opts, totalCount, paged, totalSavings, warnings, nodeCurrency, hasNext, "")
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
	clusterFilter, nodeNameFilter, gpuModelFilter string,
) error {
	hlog := requestLogger(c, orgIDStr)

	cursor, hasCursor, cursorErr := applyNodeGPUCursor(c)
	if cursorErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": cursorErr.Error()})
	}
	if hasCursor {
		opts.Offset = 0
	}

	triples, err := engine.ListNodeGPUTriplesPage(ctx, pool, orgIDStr, clusterUUIDs, start, now, now, nodeNameFilter, gpuModelFilter, opts.OrderBy, opts.OrderHow == listoptions.OrderDesc, 0, 0, nil)
	if err != nil {
		hlog.Errorf("GetNodeRecommendations: triple page failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load node GPU recommendations"})
	}

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
			hlog.Warnf("GetNodeRecommendations: failed for cluster %s: %v", tr.ClusterUUID, err)
			gpuClusterErrors = append(gpuClusterErrors, fmt.Errorf("cluster %s: %w", tr.ClusterUUID, err))
			continue
		}
		if gpuRecs == nil {
			continue
		}
		groups := groupByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, tr.ClusterUUID)
		gpuRate := clusterRates[tr.ClusterUUID]
		for _, group := range groups {
			if group.NodeName != tr.NodeName || group.GPUModel != tr.GPUModel {
				continue
			}
			tsRec := engine.ComputeNodeTimeslicingRecForOrg(ctx, pool, orgIDStr, group, gpuRate, now)
			if tsRec == nil {
				continue
			}
			allRecs = append(allRecs, toNodeGPURecommendation(tsRec))
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

	var totalSavings *float32
	var sum float32
	hasSavings := false
	for _, r := range allRecs {
		if r.TotalNodeSavingsUSD != nil {
			sum += *r.TotalNodeSavingsUSD
			hasSavings = true
		}
	}
	if hasSavings {
		totalSavings = &sum
	}

	sortNodeRecs(allRecs, opts.OrderBy, opts.OrderHow)
	totalCount := len(allRecs)
	paged, hasNext, nextCursor, pageErr := paginateNodeGPURecs(allRecs, opts, cursor, hasCursor)
	if pageErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": pageErr.Error()})
	}

	nodeCurrency := costdata.DefaultCurrency
	if clusterFilter != "" {
		nodeCurrency = fetchClusterCurrency(ctx, orgIDStr, clusterFilter)
	} else if len(triples) > 0 {
		nodeCurrency = fetchClusterCurrency(ctx, orgIDStr, triples[0].ClusterUUID)
	}
	return respondNodeGPURecommendations(c, opts, totalCount, paged, totalSavings, warnings, nodeCurrency, hasNext, nextCursor)
}

func respondNodeGPURecommendations(
	c echo.Context,
	opts listoptions.ListOptions,
	totalCount int,
	data []model.NodeGPURecommendation,
	totalSavings *float32,
	warnings []string,
	nodeCurrency string,
	hasNext bool,
	nextCursor string,
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
			Count:           totalCount,
			Limit:           opts.Limit,
			Offset:          opts.Offset,
			HasNext:         hasNext,
			NextCursor:      nextCursor,
			TotalSavingsUSD: totalSavings,
		},
		Data:     data,
		Links:    links,
		Warnings: warnings,
		Currency: nodeCurrency,
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
	case "total_node_savings_usd":
		if r.TotalNodeSavingsUSD != nil {
			return *r.TotalNodeSavingsUSD
		}
		return nil
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

	groups := groupByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID)
	var recs []model.NodeGPURecommendation
	for _, group := range groups {
		tsRec := engine.ComputeNodeTimeslicingRecForOrg(ctx, pool, orgIDStr, group, gpuRate, now)
		if tsRec == nil {
			continue
		}
		recs = append(recs, toNodeGPURecommendation(tsRec))
	}
	return recs, nil
}

func getClustersForOrg(ctx context.Context, orgID string) ([]string, error) {
	dbPool := database.GetPool()
	if dbPool == nil {
		return nil, fmt.Errorf("no database pool")
	}
	rows, err := dbPool.Query(ctx,
		`SELECT DISTINCT c.cluster_uuid::text
		 FROM clusters c
		 JOIN rh_accounts a ON c.tenant_id = a.id
		 WHERE a.org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var uuids []string
	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			return nil, err
		}
		uuids = append(uuids, uuid)
	}
	return uuids, rows.Err()
}

func groupByNodeAndModel(gpuRecs map[string][]*engine.GPURec, nodeMap map[string]string, nodeLastSeen map[string]time.Time, clusterUUID string) []engine.NodeGPUGroup {
	type groupKey struct {
		node  string
		model string
		term  string
	}
	grouped := map[groupKey]*engine.NodeGPUGroup{}

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
				g = &engine.NodeGPUGroup{
					NodeName:    nodeName,
					ClusterUUID: clusterUUID,
					GPUModel:    rec.GPUModelName,
					Term:        rec.Term,
					LastSeen:    nodeLastSeen[nodeName],
				}
				grouped[gk] = g
			}
			g.Containers = append(g.Containers, engine.NodeGPUContainer{
				Namespace: parts[0],
				Workload:  parts[1],
				Container: parts[2],
				Rec:       rec,
			})
		}
	}

	result := make([]engine.NodeGPUGroup, 0, len(grouped))
	for _, g := range grouped {
		result = append(result, *g)
	}
	return result
}

func toNodeGPURecommendation(tsRec *engine.TimeslicingRec) model.NodeGPURecommendation {
	rec := model.NodeGPURecommendation{
		NodeName:            tsRec.NodeName,
		ClusterUUID:         tsRec.ClusterUUID,
		Term:                tsRec.Term,
		RecommendationType:  "gpu_time_slicing",
		GPUModel:            tsRec.GPUModel,
		RecommendedReplicas: tsRec.RecommendedReplicas,
		SavingsPerGPUUSD:    tsRec.SavingsPerGPU,
		TotalNodeSavingsUSD: tsRec.TotalNodeSavings,
		Confidence:          tsRec.Confidence,
		NotificationCodes:   tsRec.NotificationCodes,
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
	case "total_node_savings_usd":
		primary = cmpFloat32(derefFloat32(a.TotalNodeSavingsUSD), derefFloat32(b.TotalNodeSavingsUSD))
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

// applyNodeGPURecTagFilters keeps node GPU recs with at least one candidate container matching tag filters.
func applyNodeGPURecTagFilters(
	ctx context.Context,
	c echo.Context,
	pool *pgxpool.Pool,
	orgID string,
	recs []model.NodeGPURecommendation,
) ([]model.NodeGPURecommendation, error) {
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
		nextCursor = nodeGPUNextCursor(last, nodeGPUSortValue(last, opts.OrderBy))
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
			return compareMIGSort(cur, sortVal) < 0 || (compareMIGSort(cur, sortVal) == 0 && nodeGPURecTieAfter(r, cursor))
		}
		return compareMIGSort(cur, sortVal) > 0 || (compareMIGSort(cur, sortVal) == 0 && nodeGPURecTieAfter(r, cursor))
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
