package kruize

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/utils/settings"
)

func TestUpdateExperiment_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/updateExperiment" {
			t.Errorf("expected /updateExperiment, got %s", r.URL.Path)
		}
		var payload []map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)
		if payload[0]["experiment_name"] != "test-exp-1" {
			t.Errorf("expected experiment_name test-exp-1, got %v", payload[0]["experiment_name"])
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	config := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{
			{Name: "term1", DurationDays: 3},
			{Name: "term2", DurationDays: 20},
		},
		BusinessHours: settings.BusinessHoursConfig{Enabled: false},
	}
	err := UpdateExperiment(server.URL, "test-exp-1", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateExperiment_404_NoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{{Name: "term1", DurationDays: 3}},
	}
	err := UpdateExperiment(server.URL, "missing-exp", config)
	if err != nil {
		t.Fatalf("404 should be handled gracefully, got: %v", err)
	}
}

func TestUpdateExperiment_500_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{{Name: "term1", DurationDays: 3}},
	}
	err := UpdateExperiment(server.URL, "test-exp", config)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestUpdateExperiment_ConnectionError(t *testing.T) {
	config := &settings.CustomTimeframesResponse{
		Terms: []settings.TermConfig{{Name: "term1", DurationDays: 3}},
	}
	err := UpdateExperiment("http://localhost:1", "test-exp", config)
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
}
