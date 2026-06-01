package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// respondSettingsLockedForbidden returns 403 when ROS_SETTINGS_LOCKED blocks tenant settings writes.
func respondSettingsLockedForbidden(c echo.Context) error {
	return c.JSON(http.StatusForbidden, echo.Map{
		"error":  "settings are locked by platform administrator",
		"locked": true,
	})
}
