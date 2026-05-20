package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
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

// requestLogger returns a structured logger scoped to this HTTP request.
// It includes org_id (from identity) and request_id (from Echo RequestID middleware).
func requestLogger(c echo.Context, orgID string) *logrus.Entry {
	reqID := c.Response().Header().Get(echo.HeaderXRequestID)
	return logging.ForRequest(orgID, reqID)
}
