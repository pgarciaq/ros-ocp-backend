package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// GetNodeGPUTimeslicingDetail handles GET /recommendations/openshift/gpu/timeslicing/{node}.
// Returns all GPU-model × term rows for one node (same shape as the list). Nested
// business_hours is attached here only; the list stays all-hours.
func GetNodeGPUTimeslicingDetail(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	nodeName := strings.TrimSpace(c.Param("node"))
	if nodeName == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "node name is required"})
	}

	if restrictNodes, allowedNodes := openshiftNodeRBACScope(userPerms); restrictNodes {
		if len(allowedNodes) == 0 {
			return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
		}
		allowed := false
		for _, n := range allowedNodes {
			if n == nodeName {
				allowed = true
				break
			}
		}
		if !allowed {
			return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
		}
	}

	pool := database.GetPool()
	if pool == nil {
		hlog.Warnf("GetNodeGPUTimeslicingDetail: database pool unavailable")
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()

	allClusters, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		hlog.Warnf("GetNodeGPUTimeslicingDetail: failed to resolve clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if len(allowedClusters) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
	}

	clusterFilter := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if clusterFilter == "" {
		clusterFilter = strings.TrimSpace(c.QueryParam("cluster"))
	}
	if clusterFilter == "" {
		clusterFilter = queryparams.FirstFilter(c, "cluster")
	}
	if clusterFilter != "" {
		found := false
		for _, id := range allowedClusters {
			if id == clusterFilter {
				found = true
				break
			}
		}
		if !found {
			return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
		}
		allowedClusters = []string{clusterFilter}
	}

	termFilterRaw := queryparams.FirstFilter(c, "term")
	termFilter, termErr := queryparams.NormalizeRecommendationTermFilter(termFilterRaw)
	if termErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termErr.Error()})
	}
	gpuModelFilter := queryparams.FirstFilter(c, "gpu_model")
	includeExplanation := RequestIncludesExplanation(c.QueryParam("include"))

	filterSQL, args, _, _, filterErr := buildNodeGPUTimeslicingFilterSQL(
		c, orgID, allowedClusters, userPerms,
		clusterFilter, nodeName, gpuModelFilter, termFilter,
	)
	if filterErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": filterErr.Error()})
	}

	recs, listErr := queryPersistedNodeGPURecs(
		ctx, pool, filterSQL, args,
		nodeGPUTimeslicingOrderBy[listoptions.DefaultNodeRecsOrderBy], listoptions.OrderAsc,
		0, 0, false, includeExplanation,
	)
	if listErr != nil {
		hlog.Warnf("GetNodeGPUTimeslicingDetail: persisted query failed: %v", listErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load GPU time-slicing recommendations",
		})
	}

	if len(recs) == 0 {
		live, liveErr := loadLiveNodeGPUTimeslicingForNode(
			ctx, pool, orgID, userPerms, allowedClusters, nodeName, gpuModelFilter, termFilter,
		)
		if liveErr != nil {
			hlog.Warnf("GetNodeGPUTimeslicingDetail: live fallback failed: %v", liveErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to load GPU time-slicing recommendations",
			})
		}
		recs = live
	}
	if len(recs) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
	}

	userCurrency := resolveUserCurrency(ctx, orgID)
	convertNodeGPURecsToUserCurrency(ctx, orgID, recs, userCurrency)

	if enrichErr := engine.EnrichNodeGPUTimeslicingDetailWithBusinessHours(ctx, pool, orgID, nodeName, recs); enrichErr != nil {
		hlog.Warnf("GetNodeGPUTimeslicingDetail: business hours enrich failed: %v", enrichErr)
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, model.NodeGPUTimeslicingDetailResponse{Data: recs})
}

func loadLiveNodeGPUTimeslicingForNode(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	userPerms map[string][]string,
	clusterUUIDs []string,
	nodeName, gpuModelFilter, termFilter string,
) ([]model.NodeGPURecommendation, error) {
	terms, err := engine.LoadTermConfigCached(ctx, pool, orgID, "gpu")
	if err != nil {
		terms = engine.DefaultTermsForPlugin("gpu")
	}
	windowDays := engine.MaxWindowDays(terms, 30)
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -windowDays)

	var all []model.NodeGPURecommendation
	for _, clusterUUID := range clusterUUIDs {
		recs, recErr := collectNodeGPURecsForCluster(ctx, pool, orgID, clusterUUID, start, now, terms, getGPUCostProvider())
		if recErr != nil {
			return nil, recErr
		}
		all = append(all, recs...)
	}
	all = filterNodeRecsByRBAC(all, userPerms)
	all = filterNodeRecs(all, nodeName, gpuModelFilter, termFilter)
	if all == nil {
		all = []model.NodeGPURecommendation{}
	}
	return all, nil
}
