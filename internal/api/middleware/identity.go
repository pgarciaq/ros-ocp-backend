package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
)

// CostManagementEntitledKey is the echo context key for the parsed entitlement flag.
const CostManagementEntitledKey = "CostManagementEntitled"

type parsedIdentityHeader struct {
	Identity     identity.Identity `json:"identity"`
	Entitlements struct {
		CostManagement struct {
			IsEntitled *bool `json:"is_entitled"`
		} `json:"cost_management"`
	} `json:"entitlements"`
}

func Identity(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		encodedIdentity := c.Request().Header.Get("X-Rh-Identity")
		decodedIdentity, err := base64.StdEncoding.DecodeString(encodedIdentity)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Unable to decode X-Rh-Identity")
		}

		var payload parsedIdentityHeader
		if err := json.Unmarshal(decodedIdentity, &payload); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Unable to unmarshal X-Rh-Identity into struct")
		}

		c.Set("Identity", identity.XRHID{Identity: payload.Identity})

		entitled := payload.Entitlements.CostManagement.IsEntitled != nil && *payload.Entitlements.CostManagement.IsEntitled
		c.Set(CostManagementEntitledKey, entitled)

		return next(c)
	}
}
