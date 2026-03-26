package api

import "testing"

func TestDurationInHours_CustomShortTerm(t *testing.T) {
	kruizeRecommendation := map[string]interface{}{
		"short_term": map[string]interface{}{
			"duration_in_hours":     720.0,
			"monitoring_start_time": "2026-02-22T00:00:00Z",
			"recommendation_engines": map[string]interface{}{},
		},
	}
	result := FormatRecommendationTerms(kruizeRecommendation)
	shortTerm := result["short_term"]
	if shortTerm.DurationInHours != 720.0 {
		t.Errorf("expected duration_in_hours=720.0, got %f", shortTerm.DurationInHours)
	}
}

func TestDurationInHours_DefaultsPreserved(t *testing.T) {
	kruizeRecommendation := map[string]interface{}{
		"short_term": map[string]interface{}{
			"duration_in_hours": 24.0,
		},
		"medium_term": map[string]interface{}{
			"duration_in_hours": 168.0,
		},
		"long_term": map[string]interface{}{
			"duration_in_hours": 360.0,
		},
	}
	result := FormatRecommendationTerms(kruizeRecommendation)
	if result["short_term"].DurationInHours != 24.0 {
		t.Errorf("expected 24.0, got %f", result["short_term"].DurationInHours)
	}
	if result["medium_term"].DurationInHours != 168.0 {
		t.Errorf("expected 168.0, got %f", result["medium_term"].DurationInHours)
	}
}

func TestDurationInHours_MissingField(t *testing.T) {
	kruizeRecommendation := map[string]interface{}{
		"short_term": map[string]interface{}{
			"monitoring_start_time": "2026-02-22T00:00:00Z",
		},
	}
	result := FormatRecommendationTerms(kruizeRecommendation)
	if result["short_term"].DurationInHours < 0 {
		t.Error("duration_in_hours should not be negative")
	}
}
