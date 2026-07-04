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
		RateLimitEnabled: true,
		RateLimitRPM:     6,
		RateLimitBurst:   2,
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
		RateLimitEnabled: true,
		RateLimitRPM:     6,
		RateLimitBurst:   1,
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

	// Empty org_id requests share the "__unknown_org__" bucket.
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
