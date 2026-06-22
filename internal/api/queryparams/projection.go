package queryparams

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
)

var variationOrderByAliases = map[string]struct{}{
	"cpu_variation":    {},
	"memory_variation": {},
}

func isAllowedOrderByAPIField(field string, allowed map[string]string) bool {
	if _, ok := allowed[field]; ok {
		return true
	}
	_, ok := variationOrderByAliases[field]
	return ok
}

// CollectEngineFilterValues reads filter[engine] / ?engine= values (cost or performance).
func CollectEngineFilterValues(c echo.Context) ([]string, error) {
	engines := IncludeValues(c, "engine")
	if len(engines) == 0 {
		if flat := FirstFilter(c, "engine"); flat != "" {
			engines = []string{flat}
		}
	}
	if len(engines) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(engines))
	for _, engine := range engines {
		switch engine {
		case "cost", "performance":
			normalized = append(normalized, engine)
		default:
			return nil, fmt.Errorf("invalid engine")
		}
	}
	return normalized, nil
}

// HasExplicitTermAndEngineFilters reports whether the caller set exactly one term
// and one engine filter (flat or bracket syntax).
func HasExplicitTermAndEngineFilters(c echo.Context) bool {
	term := strings.TrimSpace(FirstFilter(c, "term"))
	if term == "" {
		return false
	}
	engines, err := CollectEngineFilterValues(c)
	return err == nil && len(engines) == 1
}

// ResolveVariationOrderByKey expands cpu_variation / memory_variation using
// filter[term] and filter[engine] when present, otherwise short_term + cost.
func ResolveVariationOrderByKey(c echo.Context, apiField string) (string, bool) {
	if _, ok := variationOrderByAliases[apiField]; !ok {
		return apiField, false
	}

	termSuffix := "short"
	if raw := strings.TrimSpace(FirstFilter(c, "term")); raw != "" {
		if dbTerm, err := NormalizeRecommendationTermFilter(raw); err == nil && dbTerm != "" {
			termSuffix = dbTerm
		}
	}

	engine := "cost"
	if engines, err := CollectEngineFilterValues(c); err == nil && len(engines) == 1 {
		engine = engines[0]
	}

	return apiField + "_" + termSuffix + "_" + engine, true
}
