package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

// PostTagsSync receives resolved namespace tags from Koku (SaaS / ROS_TAGS_SOURCE=api only).
// Gated by ROS_TAGS_ENABLED; returns 404 when disabled or when source=db.
func PostTagsSync(c echo.Context) error {
	if !config.TagsFeatureEnabled() {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "tag sync is not enabled",
		})
	}
	if !config.TagsUsePushSync() {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "tag push sync is disabled; ROS reads Koku tag tables directly (ROS_TAGS_SOURCE=db)",
		})
	}

	bearerToken := tags.BearerTokenFromHeader(c.Request().Header.Get(echo.HeaderAuthorization))
	if err := tags.ValidateBearerToken(c.Request().Context(), bearerToken); err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{
			"status":  "unauthorized",
			"message": "invalid or missing service account token",
		})
	}

	var req tags.SyncRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "invalid JSON request body",
		})
	}
	if err := tags.ValidateSyncRequest(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": err.Error(),
		})
	}

	svc := tags.NewSyncService(database.GetPool())
	updated, err := svc.SyncOrgTags(c.Request().Context(), req)
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
