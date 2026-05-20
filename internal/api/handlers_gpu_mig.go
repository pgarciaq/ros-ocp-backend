package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
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

	opts, err := listoptions.ListAPIOptions(c, listoptions.DefaultGpuMigOrderBy, listoptions.GpuMigAllowedOrderBy)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	if opts.Format == listoptions.ResponseFormatCSV {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "csv format is not supported for this endpoint"})
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

	terms, err := engine.LoadTermConfigCached(ctx, pool, orgIDStr)
	if err != nil {
		log.Warnf("GetGPUMIGRecommendations: load term config failed: %v", err)
		terms = engine.DefaultTerms()
	}
	start := now.AddDate(0, 0, -engine.MaxWindowDays(terms, 30))

	clusterUUIDs, err := getClustersForOrg(ctx, orgIDStr)
	if err != nil {
		log.Errorf("GetGPUMIGRecommendations: failed to get clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	clusterUUIDs = filterClustersByRBAC(clusterUUIDs, userPerms)

	var warnings []string
	var gpuClusterErrors []error
	var entries []model.GPUMIGRecommendationEntry

	for _, clusterUUID := range clusterUUIDs {
		gpuRecs, nodeMap, _, err := engine.QueryGPURecommendations(ctx, pool, clusterUUID, start, now, terms, nil)
		if err != nil {
			log.Warnf("GetGPUMIGRecommendations: failed for cluster %s: %v", clusterUUID, err)
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
				})
			}
		}
	}

	if len(gpuClusterErrors) > 0 {
		log.Warnf("GetGPUMIGRecommendations: incomplete GPU queries: %v", errors.Join(gpuClusterErrors...))
		switch len(gpuClusterErrors) {
		case 1:
			warnings = append(warnings, fmt.Sprintf("GPU enrichment failed: %s", briefGPUEnrichmentErr(gpuClusterErrors[0])))
		default:
			warnings = append(warnings, fmt.Sprintf("GPU data unavailable for %d clusters", len(gpuClusterErrors)))
		}
	}

	entries = filterGPUMIGEntriesByRBAC(entries, userPerms)
	if entries == nil {
		entries = []model.GPUMIGRecommendationEntry{}
	}

	sortGPUMIGEntries(entries, opts.OrderBy, opts.OrderHow)

	totalCount := len(entries)
	paged := applyGPUMIGPagination(entries, opts.Offset, opts.Limit)

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, model.GPUMIGListResponse{
		Meta: model.GPUMIGListMeta{
			Count:  totalCount,
			Limit:  opts.Limit,
			Offset: opts.Offset,
		},
		Data:     paged,
		Warnings: warnings,
	})
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
