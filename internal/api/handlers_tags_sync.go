package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

const internalTagsTokenHeader = "X-ROS-Internal-Token"

// PostTagsSync receives resolved container tags from Koku (or an operator script).
// Gated by ROS_TAGS_ENABLED; returns 404 when disabled.
func PostTagsSync(c echo.Context) error {
	if !config.TagsFeatureEnabled() {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "tag sync is not enabled",
		})
	}

	expectedToken := strings.TrimSpace(config.GetConfig().TagsInternalToken)
	if expectedToken == "" {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "unavailable",
			"message": "tag sync token is not configured",
		})
	}
	gotToken := strings.TrimSpace(c.Request().Header.Get(internalTagsTokenHeader))
	if gotToken == "" || gotToken != expectedToken {
		return c.JSON(http.StatusUnauthorized, echo.Map{
			"status":  "unauthorized",
			"message": "invalid or missing internal token",
		})
	}

	var req tags.SyncRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "invalid request body",
		})
	}
	if strings.TrimSpace(req.OrgID) == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "org_id is required",
		})
	}

	svc := tags.NewSyncService(database.GetPool())
	updated, err := svc.SyncOrgTags(c.Request().Context(), req.OrgID, req.ContainerTags)
	if err != nil {
		hlog := requestLogger(c, req.OrgID)
		hlog.Errorf("tag sync failed: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"status":  "error",
			"message": "failed to sync tags",
		})
	}

	return c.JSON(http.StatusOK, tags.SyncResponse{Updated: updated})
}
