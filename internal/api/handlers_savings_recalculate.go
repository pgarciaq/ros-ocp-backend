package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

// SavingsRecalcRequest is the body for POST /internal/recalculate-savings.
type SavingsRecalcRequest struct {
	OrgID               string   `json:"org_id"`
	ClusterUUID         string   `json:"cluster_uuid,omitempty"`
	RecommendationTypes []string `json:"recommendation_types,omitempty"`
}

// SavingsRecalcResponse acknowledges an async savings recalculation job.
type SavingsRecalcResponse struct {
	Status              string   `json:"status"`
	OrgID               string   `json:"org_id"`
	ClusterUUID         string   `json:"cluster_uuid,omitempty"`
	RecommendationTypes []string `json:"recommendation_types"`
}

// PostRecalculateSavings triggers savings-only recalculation after Koku cost model rate changes.
// Requires a valid service account bearer token (same auth as tag sync).
func PostRecalculateSavings(c echo.Context) error {
	if !config.GetConfig().SavingsEstimatesEnabled {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "savings estimates are disabled",
		})
	}
	if !config.GetConfig().SavingsRecalculationEnabled {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "savings recalculation is disabled",
		})
	}

	saName, authErr := authenticateInternalCaller(c)
	if authErr != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{
			"status":  "unauthorized",
			"message": "invalid or missing service account token",
		})
	}

	var req SavingsRecalcRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "invalid JSON request body",
		})
	}
	orgID := strings.TrimSpace(req.OrgID)
	if orgID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "org_id is required",
		})
	}
	if orgErr := validateInternalOrgTarget(orgID); orgErr != nil {
		return orgErr
	}

	recTypes := engine.NormalizeSavingsRecTypesForAPI(req.RecommendationTypes)
	if len(recTypes) == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "recommendation_types must include container, node, pvc, quota, and/or cluster-quota",
		})
	}

	clusterUUID := strings.TrimSpace(req.ClusterUUID)
	auditInternalEndpoint(c, "POST /internal/recalculate-savings", orgID, saName, "recalculate_savings")
	engine.TriggerSavingsRecalculationAsync(database.GetPool(), orgID, clusterUUID, recTypes)

	return c.JSON(http.StatusAccepted, SavingsRecalcResponse{
		Status:              "accepted",
		OrgID:               orgID,
		ClusterUUID:         clusterUUID,
		RecommendationTypes: recTypes,
	})
}
