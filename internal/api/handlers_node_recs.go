package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

// GetNodeRecommendations handles GET /recommendations/openshift/nodes.
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

	ctx := context.Background()
	now := time.Now().UTC()

	terms, err := engine.LoadTermConfig(ctx, pool, orgIDStr)
	if err != nil {
		log.Warnf("GetNodeRecommendations: load term config failed: %v", err)
		terms = engine.DefaultTerms()
	}
	start := now.AddDate(0, 0, -engine.MaxWindowDays(terms, 30))

	clusterUUIDs, err := getClustersForOrg(ctx, orgIDStr)
	if err != nil {
		log.Errorf("GetNodeRecommendations: failed to get clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}

	clusterUUIDs = filterClustersByRBAC(clusterUUIDs, userPerms)

	nodeNameFilter := strings.TrimSpace(c.QueryParam("node_name"))
	gpuModelFilter := strings.TrimSpace(c.QueryParam("gpu_model"))
	termFilter := strings.TrimSpace(c.QueryParam("term"))

	useTripleSQL := termFilter == "" &&
		opts.Format != listoptions.ResponseFormatCSV &&
		engine.GPUOrderColumnSupportsTriplePagination(opts.OrderBy) &&
		len(clusterUUIDs) > 0

	if useTripleSQL {
		return respondNodeGPURecommendationsTripleSQL(c, ctx, pool, orgIDStr, userPerms, opts, terms, clusterUUIDs, start, now, nodeNameFilter, gpuModelFilter)
	}

	costProvider := getGPUCostProvider()

	var allRecs []model.NodeGPURecommendation
	var gpuClusterErrors []error
	for _, clusterUUID := range clusterUUIDs {
		gpuRecs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, clusterUUID, start, now, terms, nil)
		if err != nil {
			log.Warnf("GetNodeRecommendations: failed for cluster %s: %v", clusterUUID, err)
			gpuClusterErrors = append(gpuClusterErrors, fmt.Errorf("cluster %s: %w", clusterUUID, err))
			continue
		}
		if gpuRecs == nil {
			continue
		}

		var costData *costdata.ClusterCostData
		if costProvider != nil && orgIDStr != "" {
			kokuOrgID := strings.TrimPrefix(orgIDStr, "org")
			cd, err := costProvider.GetEffectiveRates(ctx, kokuOrgID, clusterUUID, start, now)
			if err != nil {
				log.Warnf("GetNodeRecommendations: cost data failed for cluster %s: %v", clusterUUID, err)
			} else {
				costData = cd
			}
		}

		groups := groupByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID)

		var gpuRate *float32
		if costData != nil {
			if rate := engine.GPUMonthlyRate(costData); rate > 0 {
				r := float32(rate)
				gpuRate = &r
			}
		}

		for _, group := range groups {
			tsRec := engine.ComputeNodeTimeslicingRec(group, gpuRate)
			if tsRec == nil {
				continue
			}
			allRecs = append(allRecs, toNodeGPURecommendation(tsRec))
		}
	}

	var warnings []string
	if len(gpuClusterErrors) > 0 {
		log.Warnf("GetNodeRecommendations: incomplete GPU queries: %v", errors.Join(gpuClusterErrors...))
		switch len(gpuClusterErrors) {
		case 1:
			warnings = append(warnings, fmt.Sprintf("GPU enrichment failed: %s", briefGPUEnrichmentErr(gpuClusterErrors[0])))
		default:
			warnings = append(warnings, fmt.Sprintf("GPU data unavailable for %d clusters", len(gpuClusterErrors)))
		}
	}

	allRecs = filterNodeRecsByRBAC(allRecs, userPerms)

	allRecs = filterNodeRecs(allRecs, nodeNameFilter, gpuModelFilter, termFilter)

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

	return c.JSON(http.StatusOK, model.NodeRecommendationListResponse{
		Meta: model.NodeRecommendationMeta{
			Count:           totalCount,
			Limit:           opts.Limit,
			Offset:          opts.Offset,
			TotalSavingsUSD: totalSavings,
		},
		Data:     paged,
		Links:    buildNodeLinks(c.Request(), totalCount, opts.Limit, opts.Offset),
		Warnings: warnings,
	})
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
	nodeNameFilter, gpuModelFilter string,
) error {
	totalCount, err := engine.CountNodeGPUTriples(ctx, pool, orgIDStr, clusterUUIDs, start, now, nodeNameFilter, gpuModelFilter)
	if err != nil {
		log.Errorf("GetNodeRecommendations: triple count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load node GPU recommendations"})
	}
	triples, err := engine.ListNodeGPUTriplesPage(ctx, pool, orgIDStr, clusterUUIDs, start, now, nodeNameFilter, gpuModelFilter, opts.OrderBy, opts.OrderHow == listoptions.OrderDesc, opts.Limit, opts.Offset)
	if err != nil {
		log.Errorf("GetNodeRecommendations: triple page failed: %v", err)
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
			kokuOrgID := strings.TrimPrefix(orgIDStr, "org")
			cd, err := costProvider.GetEffectiveRates(ctx, kokuOrgID, t.ClusterUUID, start, now)
			if err != nil {
				log.Warnf("GetNodeRecommendations: cost data failed for cluster %s: %v", t.ClusterUUID, err)
			} else if cd != nil {
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
		gpuRecs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, tr.ClusterUUID, start, now, terms, f)
		if err != nil {
			log.Warnf("GetNodeRecommendations: failed for cluster %s: %v", tr.ClusterUUID, err)
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
			tsRec := engine.ComputeNodeTimeslicingRec(group, gpuRate)
			if tsRec == nil {
				continue
			}
			allRecs = append(allRecs, toNodeGPURecommendation(tsRec))
		}
	}

	var warnings []string
	if len(gpuClusterErrors) > 0 {
		log.Warnf("GetNodeRecommendations: incomplete GPU queries: %v", errors.Join(gpuClusterErrors...))
		switch len(gpuClusterErrors) {
		case 1:
			warnings = append(warnings, fmt.Sprintf("GPU enrichment failed: %s", briefGPUEnrichmentErr(gpuClusterErrors[0])))
		default:
			warnings = append(warnings, fmt.Sprintf("GPU data unavailable for %d clusters", len(gpuClusterErrors)))
		}
	}

	allRecs = filterNodeRecsByRBAC(allRecs, userPerms)
	if allRecs == nil {
		allRecs = []model.NodeGPURecommendation{}
	}

	sortNodeRecs(allRecs, opts.OrderBy, opts.OrderHow)

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

	return c.JSON(http.StatusOK, model.NodeRecommendationListResponse{
		Meta: model.NodeRecommendationMeta{
			Count:           totalCount,
			Limit:           opts.Limit,
			Offset:          opts.Offset,
			TotalSavingsUSD: totalSavings,
		},
		Data:     allRecs,
		Links:    buildNodeLinks(c.Request(), totalCount, opts.Limit, opts.Offset),
		Warnings: warnings,
	})
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

func getClustersForOrg(ctx context.Context, orgID string) ([]string, error) {
	dbPool := database.GetPool()
	if dbPool == nil {
		return nil, fmt.Errorf("no database pool")
	}
	rows, err := dbPool.Query(ctx,
		`SELECT DISTINCT c.cluster_uuid
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
	sort.SliceStable(recs, func(i, j int) bool {
		if desc {
			i, j = j, i
		}
		switch orderBy {
		case "cluster_uuid":
			return recs[i].ClusterUUID < recs[j].ClusterUUID
		case "gpu_model", "gpu_model_name":
			return recs[i].GPUModel < recs[j].GPUModel
		case "recommended_replicas":
			return recs[i].RecommendedReplicas < recs[j].RecommendedReplicas
		case "confidence":
			return recs[i].Confidence < recs[j].Confidence
		case "total_node_savings_usd":
			return derefFloat32(recs[i].TotalNodeSavingsUSD) < derefFloat32(recs[j].TotalNodeSavingsUSD)
		default: // node_name
			return recs[i].NodeName < recs[j].NodeName
		}
	})
}

func derefFloat32(p *float32) float32 {
	if p == nil {
		return 0
	}
	return *p
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
	if offset >= limit {
		links.Previous = makeLink(offset - limit)
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

// filterNodeRecsByRBAC restricts node recommendations to those whose NodeName
// the user has openshift.node read permission for. Returns the full list when
// RBAC is disabled, the user has a global wildcard, or node permissions are "*".
func filterNodeRecsByRBAC(recs []model.NodeGPURecommendation, userPerms map[string][]string) []model.NodeGPURecommendation {
	if !config.GetConfig().RBACEnabled {
		return recs
	}
	if _, ok := userPerms["*"]; ok {
		return recs
	}
	nodePerms, hasNode := userPerms["openshift.node"]
	if !hasNode {
		return recs
	}
	if utils.StringInSlice("*", nodePerms) {
		return recs
	}
	allowed := make(map[string]bool, len(nodePerms))
	for _, n := range nodePerms {
		allowed[n] = true
	}
	filtered := make([]model.NodeGPURecommendation, 0, len(recs))
	for _, r := range recs {
		if allowed[r.NodeName] {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
