package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

const vmRecommendationType = "vm"

var vmTermAPINames = []string{"short_term", "medium_term", "long_term"}
var vmTermEngineOrd = []string{"short", "medium", "long"}

type vmTermSettingsRequest struct {
	Terms []vmTermSettingsItem `json:"terms"`
}

type vmTermSettingsItem struct {
	Name               string   `json:"name"`
	WindowDays         int      `json:"window_days"`
	MinDataDays        *int     `json:"min_data_days,omitempty"`
	DecayHalfLifeHours *float64 `json:"decay_halflife_hours,omitempty"`
}

type vmTermSettingsResponse struct {
	Terms []vmTermSettingsResponseItem `json:"terms"`
}

type vmTermSettingsResponseItem struct {
	Name               string  `json:"name"`
	WindowDays         int     `json:"window_days"`
	MinDataDays        int     `json:"min_data_days"`
	DecayHalfLifeHours float64 `json:"decay_halflife_hours"`
	Locked             bool    `json:"locked"`
	IsDefault          bool    `json:"is_default"`
}

// GetVMSettings handles GET /recommendations/openshift/settings/vm.
func GetVMSettings(c echo.Context) error {
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

	resp, err := engine.GetVMSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		hlog.Errorf("get VM settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to read VM settings",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// PutVMSettings handles PUT /recommendations/openshift/settings/vm.
func PutVMSettings(c echo.Context) error {
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

	if err := engine.UpdateVMSettings(c.Request().Context(), pool, orgID, json.RawMessage(body)); err != nil {
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
		hlog.Errorf("put VM settings failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to update VM settings",
		})
	}

	resp, err := engine.GetVMSettingsForAPI(c.Request().Context(), pool, orgID)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "settings saved but unable to read back",
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// GetVMTermSettings handles GET /recommendations/openshift/settings/vm/terms.
func GetVMTermSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	ctx := c.Request().Context()
	terms, err := engine.LoadTermConfigCached(ctx, db.GetPool(), orgID, vmRecommendationType)
	if err != nil {
		hlog.Errorf("failed to load VM term config: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "failed to load term configuration",
		})
	}

	defaults := engine.DefaultTermsForPlugin(vmRecommendationType)
	items := make([]vmTermSettingsResponseItem, len(terms))
	for i, t := range terms {
		apiName := vmTermAPINames[i]
		if i >= len(vmTermAPINames) {
			apiName = t.Name
		}
		isDefault := i < len(defaults) &&
			t.WindowDays == defaults[i].WindowDays &&
			t.MinDataDays == defaults[i].MinDataDays &&
			t.DecayHalfLifeHours == defaults[i].DecayHalfLifeHours
		items[i] = vmTermSettingsResponseItem{
			Name:               apiName,
			WindowDays:         t.WindowDays,
			MinDataDays:        t.MinDataDays,
			DecayHalfLifeHours: t.DecayHalfLifeHours,
			Locked:             engine.IsTermLocked(vmRecommendationType, vmTermEngineOrd[i]),
			IsDefault:          isDefault,
		}
	}

	return c.JSON(http.StatusOK, vmTermSettingsResponse{Terms: items})
}

// PutVMTermSettings handles PUT /recommendations/openshift/settings/vm/terms.
func PutVMTermSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	var req vmTermSettingsRequest
	body := http.MaxBytesReader(c.Response(), c.Request().Body, 1<<20)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "invalid JSON body",
		})
	}

	if len(req.Terms) == 0 || len(req.Terms) > 3 {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "terms must contain 1-3 items",
		})
	}

	maxWin := engine.PluginMaxWindowDays(vmRecommendationType)
	var lockedTerms []string
	for _, t := range req.Terms {
		engineName, mapErr := vmTermNameToEngine(t.Name)
		if mapErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": mapErr.Error(),
			})
		}
		if t.WindowDays < 1 || t.WindowDays > maxWin {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": fmt.Sprintf("window_days must be between 1 and %d for vm", maxWin),
			})
		}
		if t.MinDataDays != nil && (*t.MinDataDays < 1 || *t.MinDataDays > t.WindowDays) {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "min_data_days must be between 1 and window_days",
			})
		}
		if t.DecayHalfLifeHours != nil && *t.DecayHalfLifeHours < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "decay_halflife_hours must be non-negative",
			})
		}
		if t.DecayHalfLifeHours != nil && *t.DecayHalfLifeHours > 8760 {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "decay_halflife_hours must not exceed 8760 (1 year)",
			})
		}
		if engine.IsTermLocked(vmRecommendationType, engineName) {
			lockedTerms = append(lockedTerms, t.Name)
		}
	}

	if len(lockedTerms) > 0 {
		return c.JSON(http.StatusUnprocessableEntity, echo.Map{
			"status":       "error",
			"message":      "one or more terms are locked by administrator and cannot be modified",
			"locked_terms": lockedTerms,
		})
	}

	ctx := c.Request().Context()
	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		hlog.Errorf("failed to begin tx for VM term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		"DELETE FROM org_recommendation_terms WHERE org_id = $1 AND recommendation_type = $2",
		orgID, vmRecommendationType); err != nil {
		hlog.Errorf("failed to delete old VM term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	for _, t := range req.Terms {
		engineName, _ := vmTermNameToEngine(t.Name)
		ord := termNameToOrd[engineName]
		var minData int
		if t.MinDataDays != nil {
			minData = *t.MinDataDays
		} else {
			minData = engine.ComputeMinDataDays(t.WindowDays)
		}
		var decayHL *float64
		if t.DecayHalfLifeHours != nil {
			decayHL = t.DecayHalfLifeHours
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO org_recommendation_terms (org_id, recommendation_type, term_ord, window_days, min_data_days, decay_halflife_hours)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (org_id, recommendation_type, term_ord) DO UPDATE SET
			   window_days = $4, min_data_days = $5, decay_halflife_hours = $6`,
			orgID, vmRecommendationType, ord, t.WindowDays, minData, decayHL,
		); err != nil {
			hlog.Errorf("failed to upsert VM term settings: %v", err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "database error",
			})
		}
	}

	if err := tx.Commit(ctx); err != nil {
		hlog.Errorf("failed to commit VM term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	engine.InvalidateTermCache(orgID, vmRecommendationType)
	return GetVMTermSettings(c)
}

func vmTermNameToEngine(name string) (string, error) {
	switch name {
	case "short_term", "short":
		return "short", nil
	case "medium_term", "medium":
		return "medium", nil
	case "long_term", "long":
		return "long", nil
	default:
		return "", fmt.Errorf("term name must be one of: short_term, medium_term, long_term")
	}
}
