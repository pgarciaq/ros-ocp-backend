package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// requireXRHID returns the request identity or writes a 401 JSON response and
// returns a non-nil error so callers stop processing.
func requireXRHID(c echo.Context) (identity.XRHID, error) {
	v := c.Get("Identity")
	xrhid, ok := v.(identity.XRHID)
	if !ok || strings.TrimSpace(xrhid.Identity.OrgID) == "" {
		_ = c.JSON(http.StatusUnauthorized, echo.Map{
			"status":  "error",
			"message": "missing or invalid identity",
		})
		return identity.XRHID{}, echo.ErrUnauthorized
	}
	return xrhid, nil
}

// requestLogger returns a structured logger scoped to this HTTP request.
// It includes org_id (from identity) and request_id (from Echo RequestID middleware).
func requestLogger(c echo.Context, orgID string) *logrus.Entry {
	reqID := c.Response().Header().Get(echo.HeaderXRequestID)
	return logging.ForRequest(orgID, reqID)
}

// orgIDFromContext returns the caller's org from the request identity, or
// "" when the identity is absent (bare unit-test contexts). #797: the
// alias-resolution subquery treats "" as fail-closed — an empty org
// matches no rows, never org-wide.
func orgIDFromContext(c echo.Context) string {
	v := c.Get("Identity")
	xrhid, ok := v.(identity.XRHID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(xrhid.Identity.OrgID)
}
