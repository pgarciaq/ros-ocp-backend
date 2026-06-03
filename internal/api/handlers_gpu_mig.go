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
				Count:  0,
				Limit:  opts.Limit,
				Offset: opts.Offset,
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

	var warnings []string
	var gpuClusterErrors []error
	var entries []model.GPUMIGRecommendationEntry

	for _, clusterUUID := range clusterUUIDs {
		gpuRecs, nodeMap, _, err := engine.QueryGPURecommendations(ctx, pool, orgIDStr, clusterUUID, start, now, terms, nil)
		if err != nil {
			hlog.Warnf("GetGPUMIGRecommendations: failed for cluster %s: %v", clusterUUID, err)
			gpuClusterErrors = append(gpuClusterErrors, fmt.Errorf("cluster %s: %w", clusterUUID, err))
			continue
		}
		if gpuRecs == nil {
			continue
		}
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
					GPUIdleState:          gpuIdle,
				})
			}
		}
	}

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

	if entries == nil {
		entries = []model.GPUMIGRecommendationEntry{}
	}

	sortGPUMIGEntries(entries, opts.OrderBy, opts.OrderHow)

	totalCount := len(entries)
	paged := applyGPUMIGPagination(entries, opts.Offset, opts.Limit)

	setRecommendationNoStore(c)
	gpuResp := model.GPUMIGListResponse{
		Meta: model.GPUMIGListMeta{
			Count:    totalCount,
			Limit:    opts.Limit,
			Offset:   opts.Offset,
			Warnings: warnings,
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
