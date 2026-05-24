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

func validateThresholdRecommendationType(c echo.Context) (string, error) {
	rt := c.QueryParam("recommendation_type")
	if rt == "" {
		return "", echo.NewHTTPError(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "recommendation_type query parameter is required",
		})
	}
	switch rt {
	case "container", "namespace", "node", "gpu", "pvc":
		return rt, nil
	default:
		return "", echo.NewHTTPError(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "recommendation_type must be one of: container, namespace, node, gpu, pvc",
		})
	}
}

// GetThresholdSettings handles GET /recommendations/openshift/settings/thresholds.
func GetThresholdSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	rt, err := validateThresholdRecommendationType(c)
	if err != nil {
		return err
	}

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	resp, err := engine.GetThresholdSettingsForAPI(c.Request().Context(), pool, orgID, rt)
	if err != nil {
		hlog.Errorf("get threshold settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to read threshold settings",
		})
	}

	return c.JSON(http.StatusOK, resp)
}

// PutThresholdSettings handles PUT /recommendations/openshift/settings/thresholds.
func PutThresholdSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	rt, err := validateThresholdRecommendationType(c)
	if err != nil {
		return err
	}

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

	if err := engine.UpdateThresholdSettings(c.Request().Context(), pool, orgID, rt, json.RawMessage(body)); err != nil {
		if errors.Is(err, engine.ErrFieldsLocked) {
			lockedFields := engine.LockedFieldsFromError(err)
			return c.JSON(http.StatusForbidden, echo.Map{
				"status":        "error",
				"message":       "one or more fields are locked by environment configuration and cannot be modified via the API",
				"locked_fields": lockedFields,
			})
		}
		hlog.Errorf("put threshold settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to update threshold settings",
		})
	}

	resp, err := engine.GetThresholdSettingsForAPI(c.Request().Context(), pool, orgID, rt)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings saved but unable to read back",
		})
	}

	return c.JSON(http.StatusOK, resp)
}

// DeleteThresholdSettings handles DELETE /recommendations/openshift/settings/thresholds.
func DeleteThresholdSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	rt, err := validateThresholdRecommendationType(c)
	if err != nil {
		return err
	}

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	if err := engine.DeleteThresholdSettings(c.Request().Context(), pool, orgID, rt); err != nil {
		hlog.Errorf("delete threshold settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to reset threshold settings",
		})
	}

	return c.NoContent(http.StatusNoContent)
}
