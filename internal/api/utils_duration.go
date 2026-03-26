package api

// FormattedTerm carries display-oriented fields for a recommendation term in API responses.
type FormattedTerm struct {
	DurationInHours float64 `json:"duration_in_hours"`
}

// FormatRecommendationTerms extracts duration_in_hours from a Kruize recommendation map
// for short_term, medium_term, and long_term. Missing values are returned as 0.
func FormatRecommendationTerms(kruizeRecommendation map[string]interface{}) map[string]FormattedTerm {
	keys := []string{"short_term", "medium_term", "long_term"}
	out := make(map[string]FormattedTerm, len(keys))
	for _, k := range keys {
		ft := FormattedTerm{}
		raw, ok := kruizeRecommendation[k]
		if !ok {
			out[k] = ft
			continue
		}
		termMap, ok := raw.(map[string]interface{})
		if !ok {
			out[k] = ft
			continue
		}
		if dh, ok := termMap["duration_in_hours"]; ok {
			ft.DurationInHours = floatFromInterface(dh)
		}
		out[k] = ft
	}
	return out
}

func floatFromInterface(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0
	}
}
