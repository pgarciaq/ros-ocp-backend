package api

import (
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// hasListProjectionParams reports whether the caller opted into slim list projection
// via explicit term and/or engine query parameters (flat or filter[...] syntax).
func hasListProjectionParams(c echo.Context) bool {
	if len(queryparams.IncludeTermFilterValues(c)) > 0 {
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

	// Include form only: this selects which term's percentile bands a detail
	// response carries, so an exclude filter must not become a positive one.
	terms, _ := queryparams.NormalizeTermFilterValuesFrom(queryparams.IncludeTermFilterValues(c))
	if len(terms) == 1 {
		opts.TermFilter = terms[0] + "_term"
	}

	return opts
}
