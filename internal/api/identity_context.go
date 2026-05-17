package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
)

// requireXRHID returns the request identity or a 401 JSON error.
func requireXRHID(c echo.Context) (identity.XRHID, error) {
	v := c.Get("Identity")
	xrhid, ok := v.(identity.XRHID)
	if !ok {
		return identity.XRHID{}, c.JSON(http.StatusUnauthorized, echo.Map{
			"status":  "error",
			"message": "missing or invalid identity",
		})
	}
	return xrhid, nil
}
