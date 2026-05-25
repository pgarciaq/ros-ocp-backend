package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

// GetTagsStatus returns per-org tag sync freshness metadata.
// Gated by ROS_TAGS_ENABLED; returns 404 when disabled.
func GetTagsStatus(c echo.Context) error {
	if !config.TagsFeatureEnabled() {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "tag sync is not enabled",
		})
	}

	bearerToken := tags.BearerTokenFromHeader(c.Request().Header.Get(echo.HeaderAuthorization))
	if err := tags.ValidateBearerToken(c.Request().Context(), bearerToken); err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{
			"status":  "unauthorized",
			"message": "invalid or missing service account token",
		})
	}

	orgID := strings.TrimSpace(c.QueryParam("org_id"))
	if orgID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "org_id query parameter is required",
		})
	}

	svc := tags.NewSyncService(database.GetPool())
	status, err := svc.GetSyncStatus(c.Request().Context(), orgID)
	if err != nil {
		hlog := requestLogger(c, orgID)
		hlog.Errorf("tag sync status failed: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"status":  "error",
			"message": "failed to read tag sync status",
		})
	}

	return c.JSON(http.StatusOK, status)
}
