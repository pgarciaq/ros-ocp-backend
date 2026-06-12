package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// CostManagementEntitlement rejects requests without cost-management entitlement.
// ADR-0167: defense-in-depth cost_management entitlement check on v1 routes.
// Skipped when DEVELOPMENT=true (local/dev deployments without gateway enforcement).
func CostManagementEntitlement(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if config.IsDevelopment() {
			return next(c)
		}

		entitled, ok := c.Get(CostManagementEntitledKey).(bool)
		if !ok || !entitled {
			return echo.NewHTTPError(
				http.StatusForbidden,
				"Cost Management entitlement required (entitlements.cost_management.is_entitled must be true)",
			)
		}

		return next(c)
	}
}
