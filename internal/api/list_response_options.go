package api

import (
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// hasListProjectionParams reports whether the caller opted into slim list projection
// via explicit term and/or engine query parameters (flat or filter[...] syntax).
func hasListProjectionParams(c echo.Context) bool {
	if queryparams.FirstFilter(c, "term") != "" {
		return true
	}
	engines, _ := collectEngineFilterValues(c)
	return len(engines) > 0
}

func listResponseOptions(c echo.Context) model.ListResponseOptions {
	var opts model.ListResponseOptions

	engines, _ := collectEngineFilterValues(c)
	if len(engines) == 1 {
		opts.EngineFilter = engines[0]
	}

	terms := queryparams.IncludeValues(c, "term")
	if len(terms) == 0 {
		if flat := queryparams.FirstFilter(c, "term"); flat != "" {
			terms = []string{flat}
		}
	}
	if len(terms) == 1 {
		if dbTerm, err := queryparams.NormalizeRecommendationTermFilter(terms[0]); err == nil {
			opts.TermFilter = dbTerm + "_term"
		}
	}

	return opts
}
