package api

import (
	"errors"
	"net/http"

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
	hlog := requestLogger(c, orgID)

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	resp, err := engine.GetSnapshotSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		hlog.Errorf("get snapshot settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to read snapshot settings",
		})
	}

	return c.JSON(http.StatusOK, resp)
}

// DeleteSnapshotSettings handles DELETE /recommendations/openshift/settings/snapshot.
func DeleteSnapshotSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	if engine.IsSettingsLocked("snapshot") {
		return respondSettingsLockedForbidden(c)
	}
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	if err := engine.DeleteSnapshotSettings(c.Request().Context(), pool, orgID); err != nil {
		hlog.Errorf("delete snapshot settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to delete snapshot settings",
		})
	}
	return c.NoContent(http.StatusNoContent)
}

// PutSnapshotSettings handles PUT /recommendations/openshift/settings/snapshot.
func PutSnapshotSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	if engine.IsSettingsLocked("snapshot") {
		return respondSettingsLockedForbidden(c)
	}
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

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
		if errors.Is(err, engine.ErrFieldsLocked) {
			lockedFields := engine.LockedFieldsFromError(err)
			return c.JSON(http.StatusForbidden, echo.Map{
				"status":        "error",
				"message":       "one or more fields are locked by environment configuration and cannot be modified via the API",
				"locked_fields": lockedFields,
			})
		}
		hlog.Errorf("put snapshot settings failed: %v", err)
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
