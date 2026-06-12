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
)

func TestIdentity_ParsesEntitlementFlag(t *testing.T) {
	e := echo.New()
	var entitled bool
	var gotEntitled bool
	e.Use(Identity)
	e.GET("/", func(c echo.Context) error {
		entitled, gotEntitled = c.Get(CostManagementEntitledKey).(bool)
		return c.NoContent(http.StatusOK)
	})

	req := newIdentityRequest(t, true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, gotEntitled)
	assert.True(t, entitled)
}

func TestIdentity_MissingEntitlementFlagIsFalse(t *testing.T) {
	e := echo.New()
	var entitled bool
	e.Use(Identity)
	e.GET("/", func(c echo.Context) error {
		entitled, _ = c.Get(CostManagementEntitledKey).(bool)
		return c.NoContent(http.StatusOK)
	})

	payload := map[string]interface{}{
		"identity": map[string]interface{}{
			"org_id": "1234567",
			"type":   "User",
		},
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)
	req.Header.Set("X-Rh-Identity", base64.StdEncoding.EncodeToString(b))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, entitled)
}

func newIdentityRequest(t *testing.T, entitled bool) *http.Request {
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
