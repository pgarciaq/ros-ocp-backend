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

// GetHCPCorrelationSettings handles GET /recommendations/openshift/settings/hcp-correlation.
func GetHCPCorrelationSettings(c echo.Context) error {
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

	resp, err := engine.GetHCPCorrelationSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		hlog.Errorf("get hcp-correlation settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to read hcp-correlation settings",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// PutHCPCorrelationSettings handles PUT /recommendations/openshift/settings/hcp-correlation.
// No recalculation is triggered: the hourly correlator picks new values up
// within one cycle (documented delay, #645). Tenant overrides, admin locks,
// and validation mirror the quota settings path.
func PutHCPCorrelationSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	if engine.IsSettingsLocked("hcp-correlation") {
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

	if err := engine.UpdateHCPCorrelationSettings(c.Request().Context(), pool, orgID, json.RawMessage(body)); err != nil {
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
		hlog.Errorf("put hcp-correlation settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to update hcp-correlation settings",
		})
	}

	resp, err := engine.GetHCPCorrelationSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings saved but unable to read back",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// DeleteHCPCorrelationSettings handles DELETE /recommendations/openshift/settings/hcp-correlation.
func DeleteHCPCorrelationSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	if engine.IsSettingsLocked("hcp-correlation") {
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

	if err := engine.DeleteHCPCorrelationSettings(c.Request().Context(), pool, orgID); err != nil {
		hlog.Errorf("delete hcp-correlation settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to delete hcp-correlation settings",
		})
	}

	resp, err := engine.GetHCPCorrelationSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings deleted but unable to read back",
		})
	}
	return c.JSON(http.StatusOK, resp)
}
