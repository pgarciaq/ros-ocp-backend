package middleware

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestNewRateLimiter_DisabledIsPassthrough(t *testing.T) {
	cfg := &config.Config{RateLimitEnabled: false}
	mw := NewRateLimiter(cfg)

	e := echo.New()
	e.Use(Identity)
	e.Use(mw)
	e.GET("/", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	req := newIdentityRequest(t, true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewRateLimiter_NilConfigIsPassthrough(t *testing.T) {
	mw := NewRateLimiter(nil)

	e := echo.New()
	e.Use(Identity)
	e.Use(mw)
	e.GET("/", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	req := newIdentityRequest(t, true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewRateLimiter_DeniesAfterBurstExhausted(t *testing.T) {
	cfg := &config.Config{
		RateLimitEnabled:        true,
		RateLimitRPM:            6,
		RateLimitBurst:          2,
		RateLimitExpiresMinutes: 5,
	}
	mw := NewRateLimiter(cfg)

	e := echo.New()
	e.Use(Identity)
	e.Use(mw)
	e.GET("/", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	// First 2 requests should succeed (burst=2).
	for i := 0; i < 2; i++ {
		req := newIdentityRequest(t, true)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "request %d should pass", i+1)
	}

	// Third request should be rate-limited (burst exhausted, rate is 0.1/s).
	req := newIdentityRequest(t, true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestNewRateLimiter_SeparateOrgsHaveIndependentLimits(t *testing.T) {
	cfg := &config.Config{
		RateLimitEnabled:        true,
		RateLimitRPM:            6,
		RateLimitBurst:          1,
		RateLimitExpiresMinutes: 5,
	}
	mw := NewRateLimiter(cfg)

	e := echo.New()
	e.Use(Identity)
	e.Use(mw)
	e.GET("/", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	// Org "1234567" — first request passes.
	req := newIdentityRequest(t, true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// Org "1234567" — second request denied.
	req = newIdentityRequest(t, true)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// Org "9999999" — first request should still pass (independent bucket).
	req = newIdentityRequestWithOrg(t, "9999999")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func newIdentityRequestWithOrg(t *testing.T, orgID string) *http.Request {
	t.Helper()
	payload := map[string]interface{}{
		"identity": map[string]interface{}{
			"org_id": orgID,
			"type":   "User",
		},
		"entitlements": map[string]interface{}{
			"cost_management": map[string]interface{}{
				"is_entitled": true,
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

func TestNewRateLimiter_EmptyOrgUsesSharedBucket(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_API_RATE_LIMIT_ENABLED", "true")
	t.Setenv("ROS_API_RATE_LIMIT_RPM", "60")
	t.Setenv("ROS_API_RATE_LIMIT_BURST", "2")
	cfg := config.GetConfig()

	e := echo.New()
	e.Use(Identity)
	e.Use(NewRateLimiter(cfg))
	e.GET("/", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	// Empty org_id requests share the UnknownOrgSentinel bucket.
	for i := 0; i < 2; i++ {
		req := newIdentityRequestWithOrg(t, "")
		req.RemoteAddr = fmt.Sprintf("10.0.0.%d:12345", i+1)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "request %d from distinct IP should share bucket", i)
	}

	// Third empty-org request should be denied (shared bucket exhausted).
	req := newIdentityRequestWithOrg(t, "")
	req.RemoteAddr = "10.0.0.99:12345"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "shared bucket should be exhausted regardless of IP")

	// A request with a real org should still pass (independent bucket).
	req = newIdentityRequestWithOrg(t, "7654321")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewRateLimiter_ExpiresMinutesConfigurable(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_API_RATE_LIMIT_EXPIRES_MINUTES", "10")
	cfg := config.GetConfig()
	assert.Equal(t, 10, cfg.RateLimitExpiresMinutes)
}

func TestNewRateLimiter_ExpiresMinutesDefault(t *testing.T) {
	config.ResetForTest()
	cfg := config.GetConfig()
	assert.Equal(t, 5, cfg.RateLimitExpiresMinutes)
}

func TestUnknownOrgSentinelConstant(t *testing.T) {
	assert.Equal(t, "__unknown_org__", UnknownOrgSentinel)
}

func TestNewRateLimiter_HealthEndpointsNotRateLimited(t *testing.T) {
	cfg := &config.Config{
		RateLimitEnabled:        true,
		RateLimitRPM:            6,
		RateLimitBurst:          1,
		RateLimitExpiresMinutes: 5,
	}

	e := echo.New()

	// Health endpoints registered directly on app (no rate limiter)
	e.GET("/healthz", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/readyz", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/status", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	// API endpoints behind identity + rate limiter (simulating v1 group)
	v1 := e.Group("/api/v1")
	v1.Use(Identity)
	v1.Use(NewRateLimiter(cfg))
	v1.GET("/data", func(c echo.Context) error { return c.String(http.StatusOK, "data") })

	// Exhaust the rate limit for a given org via API endpoint
	req := newIdentityRequest(t, true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	// Reassign to /api/v1/data path
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/data", nil)
	req.Header.Set("X-Rh-Identity", newIdentityRequest(t, true).Header.Get("X-Rh-Identity"))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "first API request should pass")

	req, _ = http.NewRequest(http.MethodGet, "/api/v1/data", nil)
	req.Header.Set("X-Rh-Identity", newIdentityRequest(t, true).Header.Get("X-Rh-Identity"))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "second API request should be rate limited")

	// Health endpoints should still respond 200 regardless of rate limit state
	for _, path := range []string{"/healthz", "/readyz", "/status"} {
		req, _ = http.NewRequest(http.MethodGet, path, nil)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "%s should NOT be rate limited", path)
	}
}
