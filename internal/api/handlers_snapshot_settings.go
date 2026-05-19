package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

// GetSnapshotSettings handles GET /recommendations/openshift/settings/snapshot.
func GetSnapshotSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	resp, err := engine.GetSnapshotSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		log.Errorf("get snapshot settings failed for org=%s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to read snapshot settings",
		})
	}

	return c.JSON(http.StatusOK, resp)
}

// PutSnapshotSettings handles PUT /recommendations/openshift/settings/snapshot.
func PutSnapshotSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	var update engine.SnapshotSettingsUpdate
	if err := c.Bind(&update); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "invalid request body",
		})
	}

	if err := engine.UpdateSnapshotSettings(c.Request().Context(), pool, orgID, update); err != nil {
		if strings.Contains(err.Error(), "locked by environment variable") {
			return c.JSON(http.StatusForbidden, echo.Map{
				"status":  "error",
				"message": err.Error(),
			})
		}
		log.Errorf("put snapshot settings failed for org=%s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to update snapshot settings",
		})
	}

	// Return the updated settings
	resp, err := engine.GetSnapshotSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings saved but unable to read back",
		})
	}

	return c.JSON(http.StatusOK, resp)
}
