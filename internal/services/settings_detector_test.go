package services

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/utils/settings"
)

func TestSettingsDetector_DetectsChange(t *testing.T) {
	stored := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{
			{Name: "term1", DurationDays: 1},
			{Name: "term2", DurationDays: 7},
			{Name: "term3", DurationDays: 15},
		},
	}
	incoming := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{
			{Name: "term1", DurationDays: 3},
			{Name: "term2", DurationDays: 20},
			{Name: "term3", DurationDays: 60},
		},
	}

	if !HasSettingsChanged(stored, incoming) {
		t.Error("expected settings change to be detected")
	}
}

func TestSettingsDetector_NoChangeWhenIdentical(t *testing.T) {
	config := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{
			{Name: "term1", DurationDays: 1},
		},
	}
	if HasSettingsChanged(config, config) {
		t.Error("identical settings should not report change")
	}
}

func TestSettingsDetector_DetectsBusinessHoursChange(t *testing.T) {
	stored := &settings.CustomTimeframesResponse{
		Terms:         []settings.TermConfig{{Name: "term1", DurationDays: 1}},
		BusinessHours: settings.BusinessHoursConfig{Enabled: false},
	}
	incoming := &settings.CustomTimeframesResponse{
		Terms:         []settings.TermConfig{{Name: "term1", DurationDays: 1}},
		BusinessHours: settings.BusinessHoursConfig{Enabled: true, StartTime: "09:00", EndTime: "17:00", Timezone: "UTC", Weekdays: []int{1, 2, 3, 4, 5}},
	}
	if !HasSettingsChanged(stored, incoming) {
		t.Error("business hours change should be detected")
	}
}

func TestSettingsDetector_NilStoredIsChange(t *testing.T) {
	incoming := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{{Name: "term1", DurationDays: 3}},
	}
	if !HasSettingsChanged(nil, incoming) {
		t.Error("nil stored settings should be treated as a change")
	}
}

func TestSettingsDetector_DetectsTermCountChange(t *testing.T) {
	stored := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{
			{Name: "term1", DurationDays: 1},
			{Name: "term2", DurationDays: 7},
			{Name: "term3", DurationDays: 15},
		},
	}
	incoming := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{
			{Name: "term1", DurationDays: 1},
			{Name: "term2", DurationDays: 7},
		},
	}
	if !HasSettingsChanged(stored, incoming) {
		t.Error("term count change should be detected")
	}
}
