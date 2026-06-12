package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// GetNotificationCodes handles GET /recommendations/openshift/notification-codes.
// Returns the full notification code catalog from in-memory definitions (no DB round-trip).
// Optional filter[plugin] limits codes to those used by a recommendation plugin.
func GetNotificationCodes(c echo.Context) error {
	pluginFilter := c.QueryParam("filter[plugin]")
	resp := notifications.BuildCatalog(pluginFilter)
	c.Response().Header().Set("Cache-Control", "public, max-age=86400")
	return c.JSON(http.StatusOK, resp)
}
