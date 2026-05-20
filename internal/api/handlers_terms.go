package api

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

type termSettingsRequest struct {
	Terms []termSettingsItem `json:"terms"`
}

type termSettingsItem struct {
	Name               string   `json:"name"`
	WindowDays         int      `json:"window_days"`
	DecayHalfLifeHours *float64 `json:"decay_halflife_hours,omitempty"`
}

type termSettingsResponse struct {
	Terms []termSettingsResponseItem `json:"terms"`
}

type termSettingsResponseItem struct {
	Name               string  `json:"name"`
	WindowDays         int     `json:"window_days"`
	MinDataDays        int     `json:"min_data_days"`
	DecayHalfLifeHours float64 `json:"decay_halflife_hours"`
	IsDefault          bool    `json:"is_default"`
}

var termNameToOrd = map[string]int{"short": 1, "medium": 2, "long": 3}

func GetTermSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID

	ctx := c.Request().Context()
	terms, err := engine.LoadTermConfigCached(ctx, db.GetPool(), orgID)
	if err != nil {
		log.Errorf("failed to load term config for org %s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "failed to load term configuration",
		})
	}

	defaults := engine.DefaultTerms()
	isDefault := len(terms) == len(defaults)
	if isDefault {
		for i := range terms {
			if terms[i].WindowDays != defaults[i].WindowDays || terms[i].DecayHalfLifeHours != defaults[i].DecayHalfLifeHours {
				isDefault = false
				break
			}
		}
	}

	items := make([]termSettingsResponseItem, len(terms))
	for i, t := range terms {
		items[i] = termSettingsResponseItem{
			Name:               t.Name,
			WindowDays:         t.WindowDays,
			MinDataDays:        t.MinDataDays,
			DecayHalfLifeHours: t.DecayHalfLifeHours,
			IsDefault:          isDefault,
		}
	}

	return c.JSON(http.StatusOK, termSettingsResponse{Terms: items})
}

func PutTermSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID

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

	for _, t := range req.Terms {
		if _, ok := termNameToOrd[t.Name]; !ok {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "term name must be one of: short, medium, long",
			})
		}
		if t.WindowDays < 1 || t.WindowDays > 90 {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "window_days must be between 1 and 90",
			})
		}
		if t.DecayHalfLifeHours != nil && *t.DecayHalfLifeHours < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "decay_halflife_hours must be non-negative",
			})
		}
	}

	ctx := c.Request().Context()
	pool := db.GetPool()

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Errorf("failed to begin tx for term settings: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, "DELETE FROM org_recommendation_terms WHERE org_id = $1", orgID); err != nil {
		log.Errorf("failed to delete old term settings for org %s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	for _, t := range req.Terms {
		ord := termNameToOrd[t.Name]
		var decayHL *float64
		if t.DecayHalfLifeHours != nil {
			decayHL = t.DecayHalfLifeHours
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO org_recommendation_terms (org_id, term_ord, window_days, decay_halflife_hours)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (org_id, term_ord) DO UPDATE SET window_days = $3, decay_halflife_hours = $4`,
			orgID, ord, t.WindowDays, decayHL,
		); err != nil {
			log.Errorf("failed to upsert term settings for org %s: %v", orgID, err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "database error",
			})
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Errorf("failed to commit term settings for org %s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	return GetTermSettings(c)
}

func DeleteTermSettings(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID

	ctx := c.Request().Context()
	pool := db.GetPool()

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Errorf("failed to begin transaction for deleting term settings org %s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, "DELETE FROM org_recommendation_terms WHERE org_id = $1", orgID); err != nil {
		log.Errorf("failed to delete term settings for org %s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	if err := tx.Commit(ctx); err != nil {
		log.Errorf("failed to commit delete term settings for org %s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database error",
		})
	}

	return GetTermSettings(c)
}
