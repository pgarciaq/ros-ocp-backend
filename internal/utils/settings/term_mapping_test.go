package settings

import "testing"

func TestTermMapping_Term1ToShortTerm(t *testing.T) {
	result := MapTermNameToKruize("term1")
	if result != "short_term" {
		t.Errorf("expected short_term, got %s", result)
	}
}

func TestTermMapping_Term2ToMediumTerm(t *testing.T) {
	result := MapTermNameToKruize("term2")
	if result != "medium_term" {
		t.Errorf("expected medium_term, got %s", result)
	}
}

func TestTermMapping_Term3ToLongTerm(t *testing.T) {
	result := MapTermNameToKruize("term3")
	if result != "long_term" {
		t.Errorf("expected long_term, got %s", result)
	}
}

func TestTermMapping_UnknownTermPanicsOrErrors(t *testing.T) {
	result := MapTermNameToKruize("term4")
	if result != "" {
		t.Errorf("unknown term should return empty string, got %s", result)
	}
}

func TestTermMapping_ReverseShortTermToTerm1(t *testing.T) {
	result := MapKruizeTermToUser("short_term")
	if result != "term1" {
		t.Errorf("expected term1, got %s", result)
	}
}

func TestTermMapping_ReverseMediumTermToTerm2(t *testing.T) {
	result := MapKruizeTermToUser("medium_term")
	if result != "term2" {
		t.Errorf("expected term2, got %s", result)
	}
}

func TestTermMapping_ReverseLongTermToTerm3(t *testing.T) {
	result := MapKruizeTermToUser("long_term")
	if result != "term3" {
		t.Errorf("expected term3, got %s", result)
	}
}

func TestBuildTermSettings_FullConfig(t *testing.T) {
	config := &CustomTimeframesResponse{
		Terms: []TermConfig{
			{Name: "term1", DurationDays: 3},
			{Name: "term2", DurationDays: 20},
			{Name: "term3", DurationDays: 60},
		},
	}
	ts := BuildTermSettingsForKruize(config)
	if ts.ShortTerm.DurationInDays != 3 {
		t.Errorf("short_term duration expected 3, got %d", ts.ShortTerm.DurationInDays)
	}
	if ts.MediumTerm.DurationInDays != 20 {
		t.Errorf("medium_term duration expected 20, got %d", ts.MediumTerm.DurationInDays)
	}
	if ts.LongTerm.DurationInDays != 60 {
		t.Errorf("long_term duration expected 60, got %d", ts.LongTerm.DurationInDays)
	}
}

func TestBuildTermSettings_SingleTerm(t *testing.T) {
	config := &CustomTimeframesResponse{
		Terms: []TermConfig{
			{Name: "term1", DurationDays: 5},
		},
	}
	ts := BuildTermSettingsForKruize(config)
	if ts.ShortTerm.DurationInDays != 5 {
		t.Errorf("short_term duration expected 5, got %d", ts.ShortTerm.DurationInDays)
	}
	if ts.MediumTerm != nil {
		t.Error("medium_term should be nil for single-term config")
	}
	if ts.LongTerm != nil {
		t.Error("long_term should be nil for single-term config")
	}
}
