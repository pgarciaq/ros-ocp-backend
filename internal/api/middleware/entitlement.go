package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

type costManagementEntitlement struct {
	Entitlements struct {
		CostManagement struct {
			IsEntitled *bool `json:"is_entitled"`
		} `json:"cost_management"`
	} `json:"entitlements"`
}

// CostManagementEntitlement rejects requests without cost-management entitlement.
// ADR-0167: defense-in-depth cost_management entitlement check on v1 routes.
// Skipped when DEVELOPMENT=true (local/dev deployments without gateway enforcement).
func CostManagementEntitlement(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if config.IsDevelopment() {
			return next(c)
		}

		encodedIdentity := c.Request().Header.Get("X-Rh-Identity")
		decodedIdentity, err := base64.StdEncoding.DecodeString(encodedIdentity)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Unable to decode X-Rh-Identity")
		}

		var payload costManagementEntitlement
		if err := json.Unmarshal(decodedIdentity, &payload); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Unable to unmarshal X-Rh-Identity into struct")
		}

		entitled := payload.Entitlements.CostManagement.IsEntitled
		if entitled == nil || !*entitled {
			return echo.NewHTTPError(
				http.StatusForbidden,
				"Cost Management entitlement required (entitlements.cost_management.is_entitled must be true)",
			)
		}

		return next(c)
	}
}
