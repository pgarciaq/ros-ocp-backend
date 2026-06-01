package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

// GetIdleDetectionSettings handles GET /recommendations/openshift/settings/idle-detection.
func GetIdleDetectionSettings(c echo.Context) error {
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

	resp, err := engine.GetIdleDetectionSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		hlog.Errorf("get idle detection settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to read idle detection settings",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// PutIdleDetectionSettings handles PUT /recommendations/openshift/settings/idle-detection.
func PutIdleDetectionSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	if engine.IsSettingsLocked("idle_detection") {
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

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "invalid request body",
		})
	}

	if err := engine.UpdateIdleDetectionSettings(c.Request().Context(), pool, orgID, json.RawMessage(body)); err != nil {
		var valErr *engine.ThresholdValidationError
		if errors.As(err, &valErr) {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":            "error",
				"message":           valErr.Error(),
				"validation_errors": valErr.Errors,
			})
		}
		if errors.Is(err, engine.ErrFieldsLocked) {
			return c.JSON(http.StatusForbidden, echo.Map{
				"status":        "error",
				"message":       "one or more fields are locked by environment configuration and cannot be modified via the API",
				"locked_fields": engine.LockedFieldsFromError(err),
			})
		}
		hlog.Errorf("put idle detection settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to update idle detection settings",
		})
	}

	resp, err := engine.GetIdleDetectionSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings saved but unable to read back",
		})
	}

	engine.TriggerThresholdRecalculationAsync(pool, orgID, "container")

	return c.JSON(http.StatusOK, resp)
}

// DeleteIdleDetectionSettings handles DELETE /recommendations/openshift/settings/idle-detection.
func DeleteIdleDetectionSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	if engine.IsSettingsLocked("idle_detection") {
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

	if err := engine.DeleteIdleDetectionSettings(c.Request().Context(), pool, orgID); err != nil {
		hlog.Errorf("delete idle detection settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to delete idle detection settings",
		})
	}

	resp, err := engine.GetIdleDetectionSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings deleted but unable to read back",
		})
	}
	engine.TriggerThresholdRecalculationAsync(pool, orgID, "container")
	return c.JSON(http.StatusOK, resp)
}
