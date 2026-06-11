package api

import (
	"github.com/labstack/echo/v4"
)

// validateInternalTagsAuth enforces bearer token auth on internal tag endpoints when
// ROS_INTERNAL_TAGS_AUTH_REQUIRED is true (default). Set false for local dev without SA tokens.
func validateInternalTagsAuth(c echo.Context) (saName string, httpErr *echo.HTTPError) {
	return authenticateInternalCaller(c)
}
