package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

// validateInternalTagsAuth enforces bearer token auth on internal tag endpoints when
// ROS_INTERNAL_TAGS_AUTH_REQUIRED is true (default). Set false for local dev without SA tokens.
func validateInternalTagsAuth(c echo.Context) *echo.HTTPError {
	if !config.InternalTagsAuthRequired() {
		return nil
	}
	bearerToken := tags.BearerTokenFromHeader(c.Request().Header.Get(echo.HeaderAuthorization))
	if err := tags.ValidateBearerToken(c.Request().Context(), bearerToken); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or missing service account token")
	}
	return nil
}
