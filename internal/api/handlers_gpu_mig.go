package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
// It lists containers with MIG profile recommendations (recommended_gpu_profile set and not full_gpu).
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
	now := time.Now().UTC()

	terms, err := engine.LoadTermConfigCached(ctx, pool, orgIDStr, "gpu")
	if err != nil {
		hlog.Warnf("GetGPUMIGRecommendations: load term config failed: %v", err)
		terms = engine.DefaultTermsForPlugin("gpu")
	}
	start := now.AddDate(0, 0, -engine.MaxWindowDays(terms, 30))

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
		setRecommendationNoStore(c)
		gpuResp := model.GPUMIGListResponse{
			Meta: model.GPUMIGListMeta{
				Count:    0,
				Limit:    opts.Limit,
				Offset:   opts.Offset,
				Currency: resolveListCurrencyFromRequest(c, orgIDStr),
			},
			Data: []model.GPUMIGRecommendationEntry{},
		}
		attachTagWarningsToGPUMIG(&gpuResp, c, orgIDStr, 0)
		gpuResp.Warnings = gpuResp.Meta.Warnings
		if opts.Format == listoptions.ResponseFormatCSV {
			return streamCSV(c, csvFilename("gpu-mig-recommendations"), func(ctx context.Context, w io.Writer) error {
				return generateGPUMIGCSV(ctx, w, gpuResp.Data)
			})
		}
		return c.JSON(http.StatusOK, gpuResp)
	}

	if !engine.GPUMIGOrderColumnSupportsPagination(opts.OrderBy) {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": fmt.Sprintf("order_by %q cannot be paginated at scale; use cluster_uuid, namespace, workload, container, or gpu_model", opts.OrderBy),
		})
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

	totalCount, err := engine.CountGPUMIGKeys(ctx, pool, clusterUUIDs, start, now)
	if err != nil {
		hlog.Errorf("GetGPUMIGRecommendations: count keys failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load GPU MIG recommendations"})
	}

	queryLimit := pageLimit + 1
	var seek *engine.GPUMIGKeySeek
	if hasCursor {
		seek = gpuMIGCursorToSeek(cursor)
	}
	keys, err := engine.ListGPUMIGKeysPage(
		ctx, pool, clusterUUIDs, start, now,
		opts.OrderBy, opts.OrderHow == listoptions.OrderDesc,
		queryLimit, opts.Offset, seek,
	)
	if err != nil {
		hlog.Errorf("GetGPUMIGRecommendations: list keys failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load GPU MIG recommendations"})
	}

	var warnings []string
	entries, gpuClusterErrors := buildGPUMIGEntriesFromKeys(ctx, pool, orgIDStr, clusterUUIDs, keys, start, now, terms)
	if len(gpuClusterErrors) > 0 {
		hlog.Warnf("GetGPUMIGRecommendations: incomplete GPU queries: %v", errors.Join(gpuClusterErrors...))
		switch len(gpuClusterErrors) {
		case 1:
			warnings = append(warnings, fmt.Sprintf("GPU enrichment failed: %s", briefGPUEnrichmentErr(gpuClusterErrors[0])))
		default:
			warnings = append(warnings, fmt.Sprintf("GPU data unavailable for %d clusters", len(gpuClusterErrors)))
		}
	}

	entries = filterGPUMIGEntriesByRBAC(entries, userPerms)

	if projects := queryparams.IncludeValues(c, "project"); len(projects) > 0 {
		entries = filterGPUMIGEntriesByNamespaces(entries, projects)
	}

	termFilter := queryparams.FirstFilter(c, "term")
	if termFilter != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if strings.EqualFold(e.Term, termFilter) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if gpuIdleVals := queryparams.IncludeValues(c, "gpu_idle_state"); len(gpuIdleVals) > 0 {
		states, idleErr := model.IdleStateFilterValues(strings.Join(gpuIdleVals, ","))
		if idleErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": idleErr.Error()})
		}
		if len(states) > 0 {
			allowed := make(map[string]struct{}, len(states))
			for _, s := range states {
				allowed[s] = struct{}{}
			}
			filtered := entries[:0]
			for _, e := range entries {
				state := e.GPUIdleState
				if state == "" {
					state = "active"
				}
				if _, ok := allowed[state]; ok {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}
	}

	if config.TagsFeatureEnabled() {
		tagFilters, tagErr := parseTagFiltersFromRequest(c)
		if tagErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagErr.Error()})
		}
		if len(tagFilters) > 0 {
			allowedKeys, keysErr := model.MatchingContainerKeys(ctx, pool, orgIDStr, tagFilters)
			if keysErr != nil {
				hlog.Errorf("GetGPUMIGRecommendations: tag filter keys failed: %v", keysErr)
				return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to apply tag filters"})
			}
			filtered := entries[:0]
			for _, e := range entries {
				if allowedKeys.Contains(e.ClusterUUID, e.Namespace, e.Workload, e.Container) {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}
	}

	if entries == nil {
		entries = []model.GPUMIGRecommendationEntry{}
	}

	totalCount = len(entries)

	hasNext := opts.Format != listoptions.ResponseFormatCSV && pageLimit > 0 && len(entries) > pageLimit
	var nextCursor string
	paged := entries
	if hasNext {
		last := entries[pageLimit-1]
		nextCursor = gpuMIGNextCursor(last, gpuMIGSortValue(last, opts.OrderBy), opts.OrderBy)
		paged = entries[:pageLimit]
	} else if opts.Format == listoptions.ResponseFormatCSV && pageLimit > 0 && len(entries) > pageLimit {
		paged = entries[:pageLimit]
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
			Warnings:   warnings,
		},
		Data: paged,
	}
	attachTagWarningsToGPUMIG(&gpuResp, c, orgIDStr, len(paged))
	gpuResp.Warnings = gpuResp.Meta.Warnings
	if opts.Format == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("gpu-mig-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateGPUMIGCSV(ctx, w, paged)
		})
	}
	return c.JSON(http.StatusOK, gpuResp)
}

func gpuMIGCursorToSeek(cursor GPUMIGCursor) *engine.GPUMIGKeySeek {
	if cursor.ClusterUUID == "" {
		return nil
	}
	seek := &engine.GPUMIGKeySeek{
		ClusterUUID: cursor.ClusterUUID,
		Namespace:   cursor.Namespace,
		Container:   cursor.Container,
		GPUModel:    cursor.GPUModel,
	}
	if len(cursor.SortValue) > 0 {
		if sortVal, err := decodeCursorSortValue(cursor.SortValue); err == nil {
			seek.SortValue = sortVal
		}
	}
	return seek
}

func buildGPUMIGEntriesFromKeys(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	clusterUUIDs []string,
	keys []engine.GPUMIGKey,
	start, now time.Time,
	terms []engine.TermConfig,
) ([]model.GPUMIGRecommendationEntry, []error) {
	if len(keys) == 0 {
		return nil, nil
	}
	allowedClusters := make(map[string]struct{}, len(clusterUUIDs))
	for _, id := range clusterUUIDs {
		allowedClusters[id] = struct{}{}
	}
	keyIndex := make(map[string]map[string]engine.GPUMIGKey, len(keys))
	clustersNeeded := make(map[string]struct{})
	for _, k := range keys {
		if _, ok := allowedClusters[k.ClusterUUID]; !ok {
			continue
		}
		clustersNeeded[k.ClusterUUID] = struct{}{}
		rowKey := k.Namespace + "\x00" + k.Workload + "\x00" + k.Container + "\x00" + k.GPUModel
		if keyIndex[k.ClusterUUID] == nil {
			keyIndex[k.ClusterUUID] = make(map[string]engine.GPUMIGKey)
		}
		keyIndex[k.ClusterUUID][rowKey] = k
	}

	var entries []model.GPUMIGRecommendationEntry
	var gpuClusterErrors []error
	for clusterUUID := range clustersNeeded {
		gpuRecs, nodeMap, _, err := engine.QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, terms, nil)
		if err != nil {
			gpuClusterErrors = append(gpuClusterErrors, fmt.Errorf("cluster %s: %w", clusterUUID, err))
			continue
		}
		if gpuRecs == nil {
			continue
		}
		index := keyIndex[clusterUUID]
		for key, recs := range gpuRecs {
			parts := strings.SplitN(key, "/", 3)
			if len(parts) != 3 {
				continue
			}
			ns, wl, cn := parts[0], parts[1], parts[2]
			nodeName := nodeMap[key]
			for _, rec := range recs {
				if rec == nil || !rec.HasMIGRecommendation() {
					continue
				}
				rowKey := ns + "\x00" + wl + "\x00" + cn + "\x00" + rec.GPUModelName
				if _, wanted := index[rowKey]; !wanted {
					continue
				}
				gpuIdle := string(rec.GPUIdleState)
				if gpuIdle == "" {
					gpuIdle = "active"
				}
				entries = append(entries, model.GPUMIGRecommendationEntry{
					ClusterUUID:           clusterUUID,
					Namespace:             ns,
					Workload:              wl,
					Container:             cn,
					Term:                  rec.Term,
					GPUModel:              rec.GPUModelName,
					NodeName:              nodeName,
					RecommendedGPUProfile: rec.RecommendedGPUProfile,
					CurrentGPUProfile:     rec.CurrentGPUProfile,
					Classification:        string(rec.Classification),
					Confidence:            rec.Confidence,
					ConfidenceLevel:       rec.Confidence,
					GPUIdleState:          gpuIdle,
				})
			}
		}
	}
	return entries, gpuClusterErrors
}

func paginateGPUMIGEntries(entries []model.GPUMIGRecommendationEntry, opts listoptions.ListOptions, cursor GPUMIGCursor, hasCursor bool) ([]model.GPUMIGRecommendationEntry, bool, string, error) {
	if len(entries) == 0 {
		return []model.GPUMIGRecommendationEntry{}, false, "", nil
	}
	start := 0
	if hasCursor {
		found := false
		for i, e := range entries {
			if migEntryAfterCursor(e, cursor, opts.OrderBy, opts.OrderHow) {
				start = i
				found = true
				break
			}
		}
		if !found {
			return []model.GPUMIGRecommendationEntry{}, false, "", nil
		}
	} else if opts.Offset > 0 {
		if opts.Offset >= len(entries) {
			return []model.GPUMIGRecommendationEntry{}, false, "", nil
		}
		start = opts.Offset
	}
	end := len(entries)
	if opts.Limit > 0 {
		end = start + opts.Limit + 1
		if end > len(entries) {
			end = len(entries)
		}
	}
	slice := entries[start:end]
	hasNext := opts.Limit > 0 && len(slice) > opts.Limit
	var nextCursor string
	if hasNext {
		last := slice[opts.Limit-1]
		nextCursor = gpuMIGNextCursor(last, gpuMIGSortValue(last, opts.OrderBy), opts.OrderBy)
		slice = slice[:opts.Limit]
	}
	return slice, hasNext, nextCursor, nil
}

func migEntryAfterCursor(e model.GPUMIGRecommendationEntry, cursor GPUMIGCursor, orderBy, orderHow string) bool {
	if len(cursor.SortValue) > 0 {
		sortVal, err := decodeCursorSortValue(cursor.SortValue)
		if err != nil {
			return false
		}
		cur := gpuMIGSortValue(e, orderBy)
		if orderHow == listoptions.OrderDesc {
			return compareMIGSort(cur, sortVal) < 0 || (compareMIGSort(cur, sortVal) == 0 && migEntryTieAfter(e, cursor))
		}
		return compareMIGSort(cur, sortVal) > 0 || (compareMIGSort(cur, sortVal) == 0 && migEntryTieAfter(e, cursor))
	}
	return migEntryTieAfter(e, cursor)
}

func migEntryTieAfter(e model.GPUMIGRecommendationEntry, cursor GPUMIGCursor) bool {
	tie := e.ClusterUUID + "\x00" + e.Namespace + "\x00" + e.Container + "\x00" + e.GPUModel + "\x00" + e.Term
	cur := cursor.ClusterUUID + "\x00" + cursor.Namespace + "\x00" + cursor.Container + "\x00" + cursor.GPUModel + "\x00" + cursor.Term
	return tie > cur
}

func compareMIGSort(a, b interface{}) int {
	switch av := a.(type) {
	case string:
		bv, _ := b.(string)
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
		return 0
	case float32:
		var bf float32
		switch x := b.(type) {
		case float32:
			bf = x
		case float64:
			bf = float32(x)
		}
		if av < bf {
			return -1
		}
		if av > bf {
			return 1
		}
		return 0
	default:
		return 0
	}
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
	default:
		return e.ClusterUUID
	}
}

// gpuMIGEntryRBACVisible reports whether a row scoped to nodeName is visible under openshift.node permissions.
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

func filterGPUMIGEntriesByNamespaces(entries []model.GPUMIGRecommendationEntry, namespaces []string) []model.GPUMIGRecommendationEntry {
	allowed := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		allowed[ns] = struct{}{}
	}
	filtered := entries[:0]
	for _, e := range entries {
		if _, ok := allowed[e.Namespace]; ok {
			filtered = append(filtered, e)
		}
	}
	return filtered
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

func sortGPUMIGEntries(recs []model.GPUMIGRecommendationEntry, orderBy, orderHow string) {
	if len(recs) <= 1 {
		return
	}
	desc := orderHow == listoptions.OrderDesc
	sort.SliceStable(recs, func(i, j int) bool {
		if desc {
			i, j = j, i
		}
		switch orderBy {
		case "namespace":
			return recs[i].Namespace < recs[j].Namespace
		case "workload":
			return recs[i].Workload < recs[j].Workload
		case "container":
			return recs[i].Container < recs[j].Container
		case "term":
			return recs[i].Term < recs[j].Term
		case "gpu_model":
			return recs[i].GPUModel < recs[j].GPUModel
		case "confidence":
			return recs[i].Confidence < recs[j].Confidence
		default: // cluster_uuid
			return recs[i].ClusterUUID < recs[j].ClusterUUID
		}
	})
}

func applyGPUMIGPagination(recs []model.GPUMIGRecommendationEntry, offset, limit int) []model.GPUMIGRecommendationEntry {
	if offset >= len(recs) {
		return []model.GPUMIGRecommendationEntry{}
	}
	recs = recs[offset:]
	if limit > 0 && limit < len(recs) {
		recs = recs[:limit]
	}
	return recs
}
