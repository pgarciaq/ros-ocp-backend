package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

// GetTagsStatus returns per-org tag catalog metadata.
// Gated by ROS_TAGS_ENABLED; returns 404 when disabled.
// DB source reads Koku tables directly; API source reads org_tag_sync_metadata.
func GetTagsStatus(c echo.Context) error {
	if !config.TagsFeatureEnabled() {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "tag sync is not enabled",
		})
	}

	saName, authErr := validateInternalTagsAuth(c)
	if authErr != nil {
		return authErr
	}

	orgID := strings.TrimSpace(c.QueryParam("org_id"))
	if orgID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "org_id query parameter is required",
		})
	}
	if orgErr := validateInternalOrgTarget(orgID); orgErr != nil {
		return orgErr
	}
	auditInternalEndpoint(c, "GET /internal/tags/status", orgID, saName, "read_tag_status")

	provider := tags.GetProvider()
	catalog, err := tags.BuildTagCatalog(c.Request().Context(), provider, orgID)
	if err != nil {
		hlog := requestLogger(c, orgID)
		hlog.Errorf("tag status failed: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"status":  "error",
			"message": "failed to read tag status",
		})
	}

	status := tags.SyncStatus{
		OrgID:   orgID,
		TagKeys: catalog,
	}
	if config.TagsUsePushSync() {
		status.Source = "api"
		svc := tags.NewSyncService(database.GetPool())
		syncStatus, err := svc.GetSyncStatus(c.Request().Context(), orgID)
		if err != nil {
			hlog := requestLogger(c, orgID)
			hlog.Errorf("tag sync status failed: %v", err)
			return c.JSON(http.StatusInternalServerError, echo.Map{
				"status":  "error",
				"message": "failed to read tag sync status",
			})
		}
		status.SyncedAt = syncStatus.SyncedAt
	} else {
		status.Source = "db"
		status.Note = "Tags are read live from Koku PostgreSQL at query time; there is no push sync delay."
	}

	return c.JSON(http.StatusOK, status)
}
