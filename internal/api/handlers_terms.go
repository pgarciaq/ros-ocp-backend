package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

type termSettingsRequest struct {
	Terms []termSettingsItem `json:"terms"`
}

type termSettingsItem struct {
	Name               string   `json:"name"`
	WindowDays         int      `json:"window_days"`
	MinDataDays        *int     `json:"min_data_days,omitempty"`
	DecayHalfLifeHours *float64 `json:"decay_halflife_hours,omitempty"`
}

type termSettingsResponse struct {
	RecommendationType string                     `json:"recommendation_type"`
	Terms              []termSettingsResponseItem `json:"terms"`
	SettingsLocked     bool                       `json:"settings_locked,omitempty"`
}

func termsSettingsLocked(recommendationType string) bool {
	return engine.IsSettingsLocked(recommendationType) || engine.IsSettingsLocked("terms")
}

type termSettingsResponseItem struct {
	Name               string  `json:"name"`
	WindowDays         int     `json:"window_days"`
	MinDataDays        int     `json:"min_data_days"`
	DecayHalfLifeHours float64 `json:"decay_halflife_hours"`
	Locked             bool    `json:"locked"`
	IsDefault          bool    `json:"is_default"`
}

var termNameToOrd = map[string]int{"short": 1, "medium": 2, "long": 3}

func getRecommendationType(c echo.Context) (string, error) {
	rt := c.QueryParam("recommendation_type")
	if rt == "" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "recommendation_type query parameter is required")
	}
	if !isValidTermPlugin(rt) {
		return "", echo.NewHTTPError(http.StatusBadRequest, "recommendation_type must be a plugin that supports terms")
	}
	return rt, nil
}

func isValidTermPlugin(name string) bool {
	for _, tp := range plugin.ByTrait[plugin.TermProvider]() {
		if tp.Name() == name {
			return true
		}
	}
	return false
}

func GetTermSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	rt, err := getRecommendationType(c)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()
	terms, err := engine.LoadTermConfigCached(ctx, db.GetPool(), orgID, rt)
	if err != nil {
		hlog.Errorf("failed to load term config: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "failed to load term configuration",
		})
	}

	defaults := engine.DefaultTermsForPlugin(rt)
	items := make([]termSettingsResponseItem, len(terms))
	for i, t := range terms {
		isDefault := i < len(defaults) &&
			t.WindowDays == defaults[i].WindowDays &&
			t.MinDataDays == defaults[i].MinDataDays &&
			t.DecayHalfLifeHours == defaults[i].DecayHalfLifeHours
		items[i] = termSettingsResponseItem{
			Name:               t.Name,
			WindowDays:         t.WindowDays,
			MinDataDays:        t.MinDataDays,
			DecayHalfLifeHours: t.DecayHalfLifeHours,
			Locked:             engine.IsTermLocked(rt, t.Name),
			IsDefault:          isDefault,
		}
	}

	return c.JSON(http.StatusOK, termSettingsResponse{
		RecommendationType: rt,
		Terms:              items,
		SettingsLocked:     termsSettingsLocked(rt),
	})
}

func PutTermSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	rt, err := getRecommendationType(c)
	if err != nil {
		return err
	}
	if termsSettingsLocked(rt) {
		return respondSettingsLockedForbidden(c)
	}

	var req termSettingsRequest
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

	// Validate and check locks.
	maxWin := engine.PluginMaxWindowDays(rt)
	var lockedTerms []string
	for _, t := range req.Terms {
		if _, ok := termNameToOrd[t.Name]; !ok {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "term name must be one of: short, medium, long",
			})
		}
		if t.WindowDays < 1 || t.WindowDays > maxWin {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": fmt.Sprintf("window_days must be between 1 and %d for %s", maxWin, rt),
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
		if engine.IsTermLocked(rt, t.Name) {
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

	tx, err := pool.Begin(ctx)
	if err != nil {
		hlog.Errorf("failed to begin tx for term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		"DELETE FROM org_recommendation_terms WHERE org_id = $1 AND recommendation_type = $2",
		orgID, rt); err != nil {
		hlog.Errorf("failed to delete old term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	for _, t := range req.Terms {
		ord := termNameToOrd[t.Name]
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
			orgID, rt, ord, t.WindowDays, minData, decayHL,
		); err != nil {
			hlog.Errorf("failed to upsert term settings: %v", err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "database error",
			})
		}
	}

	if err := tx.Commit(ctx); err != nil {
		hlog.Errorf("failed to commit term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	engine.InvalidateTermCache(orgID, rt)
	return GetTermSettings(c)
}

func DeleteTermSettings(c echo.Context) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	rt, err := getRecommendationType(c)
	if err != nil {
		return err
	}
	if termsSettingsLocked(rt) {
		return respondSettingsLockedForbidden(c)
	}

	// Check if ALL terms are locked — if so, delete is a no-op but not an error.
	ctx := c.Request().Context()
	pool := db.GetPool()

	tx, err := pool.Begin(ctx)
	if err != nil {
		hlog.Errorf("failed to begin transaction for deleting term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		"DELETE FROM org_recommendation_terms WHERE org_id = $1 AND recommendation_type = $2",
		orgID, rt); err != nil {
		hlog.Errorf("failed to delete term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	if err := tx.Commit(ctx); err != nil {
		hlog.Errorf("failed to commit delete term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	engine.InvalidateTermCache(orgID, rt)
	return GetTermSettings(c)
}
