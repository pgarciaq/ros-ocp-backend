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

// GetClusterQuotaSettings handles GET /recommendations/openshift/settings/cluster-quota.
func GetClusterQuotaSettings(c echo.Context) error {
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

	resp, err := engine.GetClusterQuotaSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		hlog.Errorf("get cluster-quota settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to read cluster-quota settings",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// PutClusterQuotaSettings handles PUT /recommendations/openshift/settings/cluster-quota.
func PutClusterQuotaSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	if engine.IsSettingsLocked("cluster-quota") {
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

	if err := engine.UpdateClusterQuotaSettings(c.Request().Context(), pool, orgID, json.RawMessage(body)); err != nil {
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
		hlog.Errorf("put cluster-quota settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to update cluster-quota settings",
		})
	}

	resp, err := engine.GetClusterQuotaSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings saved but unable to read back",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// DeleteClusterQuotaSettings handles DELETE /recommendations/openshift/settings/cluster-quota.
func DeleteClusterQuotaSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	if engine.IsSettingsLocked("cluster-quota") {
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

	if err := engine.DeleteClusterQuotaSettings(c.Request().Context(), pool, orgID); err != nil {
		hlog.Errorf("delete cluster-quota settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to delete cluster-quota settings",
		})
	}

	resp, err := engine.GetClusterQuotaSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings deleted but unable to read back",
		})
	}
	return c.JSON(http.StatusOK, resp)
}
