package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFetchCustomTimeframes_Success(t *testing.T) {
	responseBody := CustomTimeframesResponse{
		Terms: []TermConfig{
			{Name: "term1", DurationDays: 3},
			{Name: "term2", DurationDays: 20},
		},
		BusinessHours: BusinessHoursConfig{Enabled: false},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-rh-identity") != "test-identity" {
			t.Error("expected x-rh-identity header")
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"data": []interface{}{responseBody},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	result, err := FetchCustomTimeframes(server.URL, "test-identity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff(result.Terms[0].DurationDays, 3); diff != "" {
		t.Error(diff)
	}
	if diff := cmp.Diff(result.Terms[1].DurationDays, 20); diff != "" {
		t.Error(diff)
	}
}

func TestFetchCustomTimeframes_404_ReturnsDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result, err := FetchCustomTimeframes(server.URL, "test-identity")
	if err != nil {
		t.Fatalf("404 should not return error, got: %v", err)
	}
	if len(result.Terms) != 3 {
		t.Errorf("expected 3 default terms, got %d", len(result.Terms))
	}
	if result.Terms[0].DurationDays != 1 {
		t.Errorf("expected default term1=1, got %d", result.Terms[0].DurationDays)
	}
}

func TestFetchCustomTimeframes_ConnectionError_ReturnsDefaults(t *testing.T) {
	result, err := FetchCustomTimeframes("http://localhost:1", "test-identity")
	if err != nil {
		t.Fatalf("connection error should not propagate, got: %v", err)
	}
	if len(result.Terms) != 3 {
		t.Errorf("expected 3 default terms, got %d", len(result.Terms))
	}
}

func TestFetchCustomTimeframes_500_ReturnsDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	result, err := FetchCustomTimeframes(server.URL, "test-identity")
	if err != nil {
		t.Fatalf("500 should not return error, got: %v", err)
	}
	if result.Terms[0].DurationDays != 1 {
		t.Errorf("expected default term1=1, got %d", result.Terms[0].DurationDays)
	}
}

func TestFetchCustomTimeframes_OnPrem_NoAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-rh-identity") != "" {
			t.Error("on-prem should not send x-rh-identity header")
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"data": []interface{}{DefaultCustomTimeframes()},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	result, err := FetchCustomTimeframes(server.URL, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Terms) != 3 {
		t.Errorf("expected 3 terms, got %d", len(result.Terms))
	}
}

func TestFetchCustomTimeframes_EmptyBaseURL_ReturnsDefaults(t *testing.T) {
	result, err := FetchCustomTimeframes("", "test-identity")
	if err != nil {
		t.Fatalf("empty base URL should not return error, got: %v", err)
	}
	if len(result.Terms) != 3 {
		t.Errorf("expected 3 default terms, got %d", len(result.Terms))
	}
	if result.Terms[0].DurationDays != 1 {
		t.Errorf("expected default term1=1, got %d", result.Terms[0].DurationDays)
	}
}
