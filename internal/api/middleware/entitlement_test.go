package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestCostManagementEntitlement_DevelopmentSkipsCheck(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")

	e := echo.New()
	e.Use(Identity)
	e.Use(CostManagementEntitlement)
	e.GET("/", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	req := newEntitlementRequest(t, false)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCostManagementEntitlement_RejectsMissingEntitlement(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "false")

	e := echo.New()
	e.Use(Identity)
	e.Use(CostManagementEntitlement)
	e.GET("/", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	req := newEntitlementRequest(t, false)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCostManagementEntitlement_AcceptsEntitled(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "false")

	e := echo.New()
	e.Use(Identity)
	e.Use(CostManagementEntitlement)
	e.GET("/", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	req := newEntitlementRequest(t, true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func newEntitlementRequest(t *testing.T, entitled bool) *http.Request {
	t.Helper()
	payload := map[string]interface{}{
		"identity": map[string]interface{}{
			"org_id": "1234567",
			"type":   "User",
		},
		"entitlements": map[string]interface{}{
			"cost_management": map[string]interface{}{
				"is_entitled": entitled,
			},
		},
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)
	req.Header.Set("X-Rh-Identity", base64.StdEncoding.EncodeToString(b))
	return req
}
