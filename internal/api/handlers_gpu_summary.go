package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const gpuSummaryAPIBase = "/api/cost-management/v1/recommendations/openshift/gpu"

// GetGPUSummary handles GET /recommendations/openshift/gpu.
// Returns lightweight counts for MIG vs time-slicing strategies and GPU digest coverage stats.
func GetGPUSummary(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgIDStr := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgIDStr)

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
		hlog.Warnf("GetGPUSummary: load term config failed: %v", err)
		terms = engine.DefaultTerms()
	}
	start := now.AddDate(0, 0, -engine.MaxWindowDays(terms, 30))

	clusterUUIDs, err := getClustersForOrg(ctx, orgIDStr)
	if err != nil {
		hlog.Errorf("GetGPUSummary: failed to get clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	clusterUUIDs = filterClustersByRBAC(clusterUUIDs, userPerms)

	tsCount, err := engine.CountNodeGPUTriples(ctx, pool, orgIDStr, clusterUUIDs, start, now, now, "", "")
	if err != nil {
		hlog.Errorf("GetGPUSummary: timeslicing triple count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load GPU summary",
		})
	}

	clustersWithGPU, totalTriples, err := engine.CountOrgGPUClusterStats(ctx, pool, orgIDStr, clusterUUIDs)
	if err != nil {
		hlog.Errorf("GetGPUSummary: cluster GPU stats failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load GPU summary",
		})
	}

	migCount := countMIGRecommendationsForSummary(ctx, pool, clusterUUIDs, userPerms, terms, start, now)

	resp := model.GPUSummaryResponse{
		MIG: model.GPUStrategySummary{
			Count: migCount,
			Link:  gpuSummaryAPIBase + "/mig",
		},
		Timeslicing: model.GPUStrategySummary{
			Count: tsCount,
			Link:  gpuSummaryAPIBase + "/timeslicing",
		},
		TotalGPUsAnalyzed:   totalTriples,
		ClustersWithGPUData: clustersWithGPU,
	}
	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, resp)
}

func countMIGRecommendationsForSummary(
	ctx context.Context,
	pool *pgxpool.Pool,
	clusterUUIDs []string,
	userPerms map[string][]string,
	terms []engine.TermConfig,
	start, now time.Time,
) int {
	n := 0
	for _, clusterUUID := range clusterUUIDs {
		gpuRecs, nodeMap, _, err := engine.QueryGPURecommendations(ctx, pool, clusterUUID, start, now, terms, nil)
		if err != nil || gpuRecs == nil {
			continue
		}
		for key, recs := range gpuRecs {
			parts := strings.SplitN(key, "/", 3)
			if len(parts) != 3 {
				continue
			}
			nodeName := nodeMap[key]
			for _, rec := range recs {
				if rec == nil || !rec.HasMIGRecommendation() {
					continue
				}
				if !gpuMIGEntryRBACVisible(nodeName, userPerms) {
					continue
				}
				n++
			}
		}
	}
	return n
}
