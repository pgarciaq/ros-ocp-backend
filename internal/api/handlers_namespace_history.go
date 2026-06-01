package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// NamespaceRecommendationHistoryResponse wraps namespace recommendation history rows.
type NamespaceRecommendationHistoryResponse struct {
	Meta  Metadata                                  `json:"meta"`
	Links Links                                     `json:"links"`
	Data  []engine.NamespaceRecommendationHistoryRow `json:"data"`
}

func respondNamespaceRecommendationHistory(c echo.Context, orgID, clusterUUID, namespace string, limit int) error {
	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	rows, listErr := engine.ListNamespaceRecommendationHistory(
		c.Request().Context(),
		pool,
		orgID,
		clusterUUID,
		namespace,
		queryparams.IncludeValues(c, "term"),
		queryparams.IncludeValues(c, "engine"),
		limit,
	)
	if listErr != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": listErr.Error()})
	}

	resp := NamespaceRecommendationHistoryResponse{
		Meta: Metadata{Count: len(rows)},
		Data: rows,
	}
	if resp.Data == nil {
		resp.Data = []engine.NamespaceRecommendationHistoryRow{}
	}
	c.Response().Header().Set("Cache-Control", "private, max-age=300")
	return c.JSON(http.StatusOK, resp)
}

// GetNamespaceRecommendationHistory handles
// GET /recommendations/openshift/namespaces/{recommendation-id}/history.
func GetNamespaceRecommendationHistory(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)

	idStr := strings.TrimSpace(c.Param("recommendation-id"))
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation-id"})
	}

	limit, err := engine.ParseNamespaceHistoryLimit(c.QueryParam("limit"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	result, lookupErr := model.GetNativeNamespaceRecommendationByID(orgID, idStr, userPerms)
	if lookupErr != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch recommendation",
		})
	}
	if result == nil {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
	}

	return respondNamespaceRecommendationHistory(c, orgID, result.ClusterUUID, result.Project, limit)
}

// GetNamespaceRecommendationHistoryWithFallback resolves recommendation-id via native
// namespace rows first, then legacy namespace_recommendation_sets id.
func GetNamespaceRecommendationHistoryWithFallback(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)

	idStr := strings.TrimSpace(c.Param("recommendation-id"))
	recUUID, parseErr := uuid.Parse(idStr)
	if parseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "bad recommendation-id"})
	}

	limit, limErr := engine.ParseNamespaceHistoryLimit(c.QueryParam("limit"))
	if limErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": limErr.Error()})
	}

	result, lookupErr := model.GetNativeNamespaceRecommendationByID(orgID, idStr, userPerms)
	if lookupErr != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch recommendation",
		})
	}
	if result != nil {
		return respondNamespaceRecommendationHistory(c, orgID, result.ClusterUUID, result.Project, limit)
	}

	nsSet := model.NamespaceRecommendationSet{}
	legacy, legacyErr := nsSet.GetNamespaceRecommendationSetByID(orgID, recUUID.String(), userPerms)
	if legacyErr != nil {
		if errors.Is(legacyErr, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
		}
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "unable to fetch project recommendation"})
	}
	if len(legacy.Recommendations) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "not_found", "message": "recommendation not found"})
	}

	return respondNamespaceRecommendationHistory(c, orgID, legacy.ClusterUUID, legacy.Project, limit)
}
