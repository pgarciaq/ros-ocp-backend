package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// BackfillGPUTimeslicingResponse acknowledges a GPU time-slicing backfill job.
type BackfillGPUTimeslicingResponse struct {
	Status            string `json:"status"`
	OrgID             string `json:"org_id,omitempty"`
	ClusterUUID       string `json:"cluster_uuid,omitempty"`
	OrgsProcessed     int    `json:"orgs_processed"`
	ClustersProcessed int    `json:"clusters_processed"`
}

// PostBackfillGPUTimeslicing triggers recomputation and persistence of node GPU
// time-slicing recommendations. Query params: org_id (optional), cluster_uuid (optional).
// Requires a valid service account bearer token (same auth as tag sync).
func PostBackfillGPUTimeslicing(c echo.Context) error {
	if !plugin.EnabledFor("gpu") {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "gpu plugin is disabled",
		})
	}

	saName, authErr := authenticateInternalCaller(c)
	if authErr != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{
			"status":  "unauthorized",
			"message": "invalid or missing service account token",
		})
	}

	orgID := strings.TrimSpace(c.QueryParam("org_id"))
	clusterUUID := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if orgID != "" {
		if orgErr := validateInternalOrgTarget(orgID); orgErr != nil {
			return orgErr
		}
	}

	pool := database.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	auditInternalEndpoint(c, "POST /internal/backfill-gpu-timeslicing", orgID, saName, "backfill_gpu_timeslicing")

	ctx := c.Request().Context()
	orgsProcessed, clustersProcessed, err := engine.BackfillNodeGPUTimeslicingRecs(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"status":  "error",
			"message": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, BackfillGPUTimeslicingResponse{
		Status:            "completed",
		OrgID:             orgID,
		ClusterUUID:       clusterUUID,
		OrgsProcessed:     orgsProcessed,
		ClustersProcessed: clustersProcessed,
	})
}
