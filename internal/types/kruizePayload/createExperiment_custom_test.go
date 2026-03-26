package kruizePayload

import (
	"encoding/json"
	"testing"
)

func TestCreateExperimentCustom_IncludesTermSettings(t *testing.T) {
	termSettings := &TermSettings{
		ShortTerm:  &TermDuration{DurationInDays: 3, ThresholdInPercent: 0.53},
		MediumTerm: &TermDuration{DurationInDays: 20, ThresholdInPercent: 0.53},
		LongTerm:   &TermDuration{DurationInDays: 60, ThresholdInPercent: 0.70},
	}
	containers := []map[string]string{
		{"container_name": "app", "container_image_name": "image:latest"},
	}
	data := map[string]string{
		"k8s_object_type": "deployment",
		"k8s_object_name": "my-app",
		"namespace":       "default",
	}

	payload, err := GetCreateExperimentPayloadWithSettings("test-exp", "cluster-1", containers, data, termSettings, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed []map[string]interface{}
	json.Unmarshal(payload, &parsed)

	ts, ok := parsed[0]["term_settings"]
	if !ok {
		t.Fatal("expected term_settings in payload")
	}
	tsMap := ts.(map[string]interface{})
	shortTerm := tsMap["short_term"].(map[string]interface{})
	if shortTerm["duration_in_days"].(float64) != 3 {
		t.Errorf("expected short_term duration 3, got %v", shortTerm["duration_in_days"])
	}
}

func TestCreateExperimentCustom_OmitsTermSettingsWhenNil(t *testing.T) {
	containers := []map[string]string{
		{"container_name": "app", "container_image_name": "image:latest"},
	}
	data := map[string]string{
		"k8s_object_type": "deployment",
		"k8s_object_name": "my-app",
		"namespace":       "default",
	}

	payload, err := GetCreateExperimentPayloadWithSettings("test-exp", "cluster-1", containers, data, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed []map[string]interface{}
	json.Unmarshal(payload, &parsed)

	if _, ok := parsed[0]["term_settings"]; ok {
		t.Error("term_settings should be omitted when nil")
	}
}

func TestCreateExperimentCustom_IncludesBusinessHours(t *testing.T) {
	bh := &BusinessHoursSettings{
		Enabled:   true,
		StartTime: "09:00",
		EndTime:   "17:00",
		Weekdays:  []int{1, 2, 3, 4, 5},
		Timezone:  "America/New_York",
	}
	containers := []map[string]string{
		{"container_name": "app", "container_image_name": "image:latest"},
	}
	data := map[string]string{
		"k8s_object_type": "deployment",
		"k8s_object_name": "my-app",
		"namespace":       "default",
	}

	payload, err := GetCreateExperimentPayloadWithSettings("test-exp", "cluster-1", containers, data, nil, bh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed []map[string]interface{}
	json.Unmarshal(payload, &parsed)

	bhParsed, ok := parsed[0]["business_hours"]
	if !ok {
		t.Fatal("expected business_hours in payload")
	}
	bhMap := bhParsed.(map[string]interface{})
	if bhMap["timezone"] != "America/New_York" {
		t.Errorf("expected timezone America/New_York, got %v", bhMap["timezone"])
	}
}
