package settings

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload"
)

// Kruize term keys used in payloads and API responses.
const (
	kruizeShortTerm  = "short_term"
	kruizeMediumTerm = "medium_term"
	kruizeLongTerm   = "long_term"
)

var userToKruizeTerm = map[string]string{
	"term1": kruizeShortTerm,
	"term2": kruizeMediumTerm,
	"term3": kruizeLongTerm,
}

var kruizeToUserTerm = map[string]string{
	kruizeShortTerm:  "term1",
	kruizeMediumTerm: "term2",
	kruizeLongTerm:   "term3",
}

// MapTermNameToKruize maps UI/API term names (term1..term3) to Kruize keys.
// Unknown names return an empty string.
func MapTermNameToKruize(name string) string {
	return userToKruizeTerm[name]
}

// MapKruizeTermToUser maps Kruize term keys back to user term names (term1..term3).
func MapKruizeTermToUser(kruizeTerm string) string {
	return kruizeToUserTerm[kruizeTerm]
}

// BuildTermSettingsForKruize converts account settings into Kruize term_settings.
// Omitted Kruize terms are left nil when the corresponding user term is not configured.
func BuildTermSettingsForKruize(config *CustomTimeframesResponse) *kruizePayload.TermSettings {
	if config == nil {
		return nil
	}
	ts := &kruizePayload.TermSettings{}
	for _, term := range config.Terms {
		k := MapTermNameToKruize(term.Name)
		if k == "" {
			continue
		}
		td := &kruizePayload.TermDuration{
			DurationInDays: term.DurationDays,
		}
		switch k {
		case kruizeShortTerm:
			td.ThresholdInPercent = 0.53
			ts.ShortTerm = td
		case kruizeMediumTerm:
			td.ThresholdInPercent = 0.53
			ts.MediumTerm = td
		case kruizeLongTerm:
			td.ThresholdInPercent = 0.70
			ts.LongTerm = td
		}
	}
	if ts.ShortTerm == nil && ts.MediumTerm == nil && ts.LongTerm == nil {
		return nil
	}
	return ts
}
