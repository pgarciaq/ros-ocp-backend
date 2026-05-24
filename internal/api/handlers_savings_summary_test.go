package api

import (
	"net/http"
	"testing"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

func TestGetFleetSavingsSummary_NoPool_Returns503(t *testing.T) {
	origPool := database.Pool
	database.Pool = nil
	t.Cleanup(func() { database.Pool = origPool })

	c, rec := newHandlerContext(t, http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary")

	err := GetFleetSavingsSummary(c)
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}
