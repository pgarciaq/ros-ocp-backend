package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetNotificationCodes_ReturnsAllCodesSorted(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/notification-codes", GetNotificationCodes)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/notification-codes", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp notifications.CatalogResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, len(notifications.Definitions), resp.Meta.Count)
	require.Len(t, resp.Data, len(notifications.Definitions))

	for i := 1; i < len(resp.Data); i++ {
		assert.Less(t, resp.Data[i-1].Code, resp.Data[i].Code, "codes should be sorted ascending")
	}
}

func TestGetNotificationCodes_EntryShape(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/notification-codes", GetNotificationCodes)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/notification-codes", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp notifications.CatalogResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)

	stale, ok := findCatalogEntry(resp.Data, 2)
	require.True(t, ok)
	assert.Equal(t, "STALE_DATA", stale.Name)
	assert.Equal(t, "WARNING", stale.Severity)
	assert.NotEmpty(t, stale.Description)
}

func TestGetNotificationCodes_PluginFilterNamespace(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/notification-codes", GetNotificationCodes)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/notification-codes?filter[plugin]=namespace",
		nil,
	)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp notifications.CatalogResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 7, resp.Meta.Count)

	codes := make([]int16, len(resp.Data))
	for i, entry := range resp.Data {
		codes[i] = entry.Code
	}
	assert.ElementsMatch(t, []int16{1, 2, 7, 9, 70, 71, 72}, codes)
}

func TestGetNotificationCodes_PluginFilterClusterQuota(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/notification-codes", GetNotificationCodes)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/notification-codes?filter[plugin]=cluster-quota",
		nil,
	)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp notifications.CatalogResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 4, resp.Meta.Count)

	codes := make([]int16, len(resp.Data))
	for i, entry := range resp.Data {
		codes[i] = entry.Code
	}
	assert.ElementsMatch(t, []int16{70, 71, 72, 73}, codes)
}

func TestGetNotificationCodes_PluginFilterUnknown_ReturnsEmpty(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/notification-codes", GetNotificationCodes)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/notification-codes?filter[plugin]=unknown",
		nil,
	)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp notifications.CatalogResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func findCatalogEntry(entries []notifications.CatalogEntry, code int16) (notifications.CatalogEntry, bool) {
	for _, e := range entries {
		if e.Code == code {
			return e, true
		}
	}
	return notifications.CatalogEntry{}, false
}
