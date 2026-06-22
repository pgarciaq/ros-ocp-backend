package api

import (
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
)

// applyNativeEngineQueryFilter adds rs.engine IN ? or ns.engine IN ? when filter[engine] or ?engine= is set.
func applyNativeEngineQueryFilter(c echo.Context, queryParams map[string]interface{}, column string) error {
	engines, err := collectEngineFilterValues(c)
	if err != nil {
		return err
	}
	if len(engines) == 0 {
		return nil
	}
	queryParams[column+" IN ?"] = engines
	return nil
}

// resolveQualityEngineFilter returns the engine for quality queries (default cost).
func resolveQualityEngineFilter(c echo.Context) (string, error) {
	engines, err := collectEngineFilterValues(c)
	if err != nil {
		return "", err
	}
	if len(engines) == 0 {
		return "cost", nil
	}
	if len(engines) > 1 {
		return "", fmt.Errorf("only one engine filter value is allowed")
	}
	return engines[0], nil
}

func collectEngineFilterValues(c echo.Context) ([]string, error) {
	return queryparams.CollectEngineFilterValues(c)
}
