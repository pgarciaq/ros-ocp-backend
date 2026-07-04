package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"golang.org/x/time/rate"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

var rateLimitedRequests = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_rate_limited_requests_total",
	Help: "Number of API requests rejected by the per-org rate limiter",
})

// NewRateLimiter returns an Echo middleware that applies per-org token bucket
// rate limiting. It is a no-op (passthrough) when ROS_API_RATE_LIMIT_ENABLED
// is false. The limiter runs after Identity middleware so the org_id context
// key is available.
func NewRateLimiter(cfg *config.Config) echo.MiddlewareFunc {
	if cfg == nil || !cfg.RateLimitEnabled {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return next
		}
	}

	ratePerSec := rate.Limit(float64(cfg.RateLimitRPM) / 60.0)

	store := middleware.NewRateLimiterMemoryStoreWithConfig(
		middleware.RateLimiterMemoryStoreConfig{
			Rate:      ratePerSec,
			Burst:     cfg.RateLimitBurst,
			ExpiresIn: 5 * time.Minute,
		},
	)

	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: store,
		IdentifierExtractor: func(c echo.Context) (string, error) {
			v := c.Get("Identity")
			xrhid, ok := v.(identity.XRHID)
			if !ok || strings.TrimSpace(xrhid.Identity.OrgID) == "" {
				return c.RealIP(), nil
			}
			return xrhid.Identity.OrgID, nil
		},
		ErrorHandler: func(c echo.Context, err error) error {
			return c.JSON(http.StatusInternalServerError, echo.Map{
				"status":  "error",
				"message": "rate limiter error",
			})
		},
		DenyHandler: func(c echo.Context, identifier string, err error) error {
			rateLimitedRequests.Inc()
			return c.JSON(http.StatusTooManyRequests, echo.Map{
				"status":  "error",
				"message": "rate limit exceeded; retry after a short delay",
			})
		},
	})
}
