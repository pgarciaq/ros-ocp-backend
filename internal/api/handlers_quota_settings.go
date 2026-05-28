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

// GetQuotaSettings handles GET /recommendations/openshift/settings/quota.
func GetQuotaSettings(c echo.Context) error {
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

	resp, err := engine.GetQuotaSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		hlog.Errorf("get quota settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to read quota settings",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// PutQuotaSettings handles PUT /recommendations/openshift/settings/quota.
func PutQuotaSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
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

	if err := engine.UpdateQuotaSettings(c.Request().Context(), pool, orgID, json.RawMessage(body)); err != nil {
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
		hlog.Errorf("put quota settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to update quota settings",
		})
	}

	resp, err := engine.GetQuotaSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings saved but unable to read back",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// DeleteQuotaSettings handles DELETE /recommendations/openshift/settings/quota.
func DeleteQuotaSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
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

	if err := engine.DeleteQuotaSettings(c.Request().Context(), pool, orgID); err != nil {
		hlog.Errorf("delete quota settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to delete quota settings",
		})
	}

	resp, err := engine.GetQuotaSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings deleted but unable to read back",
		})
	}
	return c.JSON(http.StatusOK, resp)
}
