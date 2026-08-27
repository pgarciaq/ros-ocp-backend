package api

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

// GetGPUMIGRecommendations handles GET /recommendations/openshift/gpu/mig.
// It reads persisted MIG recommendations from gpu_mig_recommendation_sets with
// full SQL-backed pagination, sorting, and filtering.
func GetGPUMIGRecommendations(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgIDStr := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgIDStr)

	opts, err := listoptions.ListAPIOptions(c, listoptions.DefaultGpuMigOrderBy, listoptions.GpuMigAllowedOrderBy)
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

	ctx := c.Request().Context()

	clusterUUIDs, err := getClustersForOrg(ctx, orgIDStr)
	if err != nil {
		hlog.Errorf("GetGPUMIGRecommendations: failed to get clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	clusterUUIDs = filterClustersByRBAC(clusterUUIDs, userPerms)

	clusterFilter := queryparams.FirstFilter(c, "cluster")
	clusterUUIDs, clusterFilterMiss := restrictClustersToQueryFilter(clusterUUIDs, clusterFilter)
	if clusterFilterMiss {
		return emptyGPUMIGResponse(c, orgIDStr, opts)
	}

	filters := engine.GPUMIGListFilters{
		ClusterUUIDs: clusterUUIDs,
	}

	if projects := queryparams.IncludeValues(c, "project"); len(projects) > 0 {
		filters.Namespaces = projects
	}
	if workloads := queryparams.IncludeValues(c, "workload"); len(workloads) > 0 {
		filters.Workloads = workloads
	}

	termFilterRaw := queryparams.FirstFilter(c, "term")
	termFilter, termErr := queryparams.NormalizeRecommendationTermFilter(termFilterRaw)
	if termErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termErr.Error()})
	}
	if termFilter != "" {
		filters.Term = termFilter
	}

	if gpuIdleVals := queryparams.IncludeValues(c, "gpu_idle_state"); len(gpuIdleVals) > 0 {
		states, idleErr := model.IdleStateFilterValues(strings.Join(gpuIdleVals, ","))
		if idleErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": idleErr.Error()})
		}
		if len(states) > 0 {
			filters.GPUIdleStates = states
		}
	}

	if config.TagsFeatureEnabled() {
		tagFilters, tagErr := parseTagFiltersFromRequest(c)
		if tagErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagErr.Error()})
		}
		if len(tagFilters) > 0 {
			filters.TagFilterFunc = func(argIdx int) (string, []any, int) {
				clause, tagArgs, nextIdx := model.TagFilterExistsClause(
					orgIDStr, "m.cluster_uuid", "m.namespace", tagFilters, argIdx)
				return clause, tagArgs, nextIdx
			}
		}
	}

	groupByCluster, groupByProject, groupByErr := parseGPUMIGListGroupBy(c)
	if groupByErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": groupByErr.Error()})
	}
	if groupByCluster || groupByProject {
		return getGPUMIGRecsGroupedSQL(c, ctx, pool, hlog, orgIDStr, filters, groupByCluster, opts.Limit, opts.Offset)
	}

	totalCount, countErr := engine.CountGPUMIGRecommendationSets(ctx, pool, orgIDStr, filters)
	if countErr != nil {
		hlog.Errorf("GetGPUMIGRecommendations: count failed: %v", countErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load GPU MIG recommendations"})
	}

	cursor, hasCursor, cursorErr := applyGPUMIGCursor(c, opts.OrderBy)
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

	desc := opts.OrderHow == listoptions.OrderDesc
	var seek *engine.GPUMIGListSeek
	if hasCursor {
		seek = gpuMIGCursorToListSeek(cursor)
	}

	rows, listErr := engine.ListGPUMIGRecommendationSets(
		ctx, pool, orgIDStr, filters,
		opts.OrderBy, desc,
		pageLimit+1, opts.Offset, seek,
	)
	if listErr != nil {
		hlog.Errorf("GetGPUMIGRecommendations: list failed: %v", listErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load GPU MIG recommendations"})
	}

	hasNext := len(rows) > pageLimit
	if hasNext {
		rows = rows[:pageLimit]
	}

	entries := gpuMIGRowsToEntries(rows)

	entries = filterGPUMIGEntriesByRBAC(entries, userPerms)

	var nextCursor string
	if hasNext && len(entries) > 0 {
		last := entries[len(entries)-1]
		nextCursor = gpuMIGNextCursor(last, gpuMIGSortValue(last, opts.OrderBy), opts.OrderBy)
	}

	if entries == nil {
		entries = []model.GPUMIGRecommendationEntry{}
	}

	setRecommendationNoStore(c)
	gpuResp := model.GPUMIGListResponse{
		Meta: model.GPUMIGListMeta{
			Count:      totalCount,
			Limit:      opts.Limit,
			Offset:     opts.Offset,
			HasNext:    hasNext,
			NextCursor: nextCursor,
			Currency:   resolveListCurrencyFromRequest(c, orgIDStr),
		},
		Data: entries,
	}
	attachTagWarningsToGPUMIG(&gpuResp, c, orgIDStr, len(entries))
	gpuResp.Warnings = gpuResp.Meta.Warnings
	if opts.Format == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("gpu-mig-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateGPUMIGCSV(ctx, w, entries)
		})
	}
	return c.JSON(http.StatusOK, gpuResp)
}

func emptyGPUMIGResponse(c echo.Context, orgID string, opts listoptions.ListOptions) error {
	setRecommendationNoStore(c)
	gpuResp := model.GPUMIGListResponse{
		Meta: model.GPUMIGListMeta{
			Count:    0,
			Limit:    opts.Limit,
			Offset:   opts.Offset,
			Currency: resolveListCurrencyFromRequest(c, orgID),
		},
		Data: []model.GPUMIGRecommendationEntry{},
	}
	attachTagWarningsToGPUMIG(&gpuResp, c, orgID, 0)
	gpuResp.Warnings = gpuResp.Meta.Warnings
	if opts.Format == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("gpu-mig-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateGPUMIGCSV(ctx, w, gpuResp.Data)
		})
	}
	return c.JSON(http.StatusOK, gpuResp)
}

func gpuMIGRowsToEntries(rows []model.GPUMIGRecommendationSetRow) []model.GPUMIGRecommendationEntry {
	entries := make([]model.GPUMIGRecommendationEntry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, model.GPUMIGRecommendationEntry{
			ID:                    model.NativeContainerID(r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.Container),
			ClusterUUID:           r.ClusterUUID,
			Namespace:             r.Namespace,
			Workload:              r.Workload,
			WorkloadType:          r.WorkloadType,
			Container:             r.Container,
			Term:                  r.Term,
			GPUModel:              r.GPUModel,
			NodeName:              r.NodeName,
			RecommendedGPUProfile: r.RecommendedGPUProfile,
			CurrentGPUProfile:     r.CurrentGPUProfile,
			Classification:        r.Classification,
			Confidence:            r.Confidence,
			ConfidenceLevel:       r.ConfidenceLevel,
			FBUsageMaxMiB:         r.FBUsageMaxMiB,
			TotalFBMiB:            r.TotalFBMiB,
			GPUIdleState:          r.GPUIdleState,
		})
	}
	return entries
}

func gpuMIGCursorToListSeek(cursor GPUMIGCursor) *engine.GPUMIGListSeek {
	if cursor.ClusterUUID == "" {
		return nil
	}
	seek := &engine.GPUMIGListSeek{
		ClusterUUID: cursor.ClusterUUID,
		Namespace:   cursor.Namespace,
		Container:   cursor.Container,
		GPUModel:    cursor.GPUModel,
		Term:        cursor.Term,
	}
	if len(cursor.SortValue) > 0 {
		if sortVal, err := decodeCursorSortValue(cursor.SortValue); err == nil {
			seek.SortValue = sortVal
		}
	}
	return seek
}

func gpuMIGNextCursorFromRow(last model.GPUMIGRecommendationSetRow, orderBy string) string {
	var sortVal interface{}
	switch orderBy {
	case "namespace":
		sortVal = last.Namespace
	case "workload":
		sortVal = last.Workload
	case "container":
		sortVal = last.Container
	case "gpu_model":
		sortVal = last.GPUModel
	case "term":
		sortVal = last.Term
	case "confidence":
		sortVal = last.Confidence
	case "gpu_idle_state":
		sortVal = last.GPUIdleState
	default:
		sortVal = last.ClusterUUID
	}
	return EncodeGPUMIGCursor(GPUMIGCursor{
		ClusterUUID: last.ClusterUUID,
		Namespace:   last.Namespace,
		Container:   last.Container,
		GPUModel:    last.GPUModel,
		Term:        last.Term,
		SortValue:   model.PaginationSortValueJSON(sortVal),
		OrderBy:     orderBy,
	})
}

func gpuMIGSortValue(e model.GPUMIGRecommendationEntry, orderBy string) interface{} {
	switch orderBy {
	case "namespace":
		return e.Namespace
	case "workload":
		return e.Workload
	case "container":
		return e.Container
	case "term":
		return e.Term
	case "gpu_model":
		return e.GPUModel
	case "confidence":
		return e.Confidence
	case "gpu_idle_state":
		return e.GPUIdleState
	default:
		return e.ClusterUUID
	}
}

func gpuMIGEntryRBACVisible(nodeName string, userPerms map[string][]string) bool {
	if !config.GetConfig().RBACEnabled {
		return true
	}
	if _, ok := userPerms["*"]; ok {
		return true
	}
	nodePerms, hasNode := userPerms["openshift.node"]
	if !hasNode {
		return true
	}
	if utils.StringInSlice("*", nodePerms) {
		return true
	}
	for _, n := range nodePerms {
		if n == nodeName {
			return true
		}
	}
	return false
}

func filterGPUMIGEntriesByRBAC(entries []model.GPUMIGRecommendationEntry, userPerms map[string][]string) []model.GPUMIGRecommendationEntry {
	filtered := make([]model.GPUMIGRecommendationEntry, 0, len(entries))
	for _, e := range entries {
		if gpuMIGEntryRBACVisible(e.NodeName, userPerms) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
