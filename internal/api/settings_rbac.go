package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// hasSettingsWritePermission reports whether the caller may mutate ROS settings.
// When RBAC is disabled, any authenticated user may write. When RBAC is enabled,
// cost-management:settings:write (wildcard) is required — matching Koku SettingsAccessPermission.
func hasSettingsWritePermission(c echo.Context) bool {
	if !config.GetConfig().RBACEnabled {
		return true
	}
	perms := get_user_permissions(c)
	if _, ok := perms["*"]; ok {
		return true
	}
	writes, ok := perms["settings.write"]
	if !ok {
		return false
	}
	for _, v := range writes {
		if v == "*" {
			return true
		}
	}
	return false
}

// requireSettingsWrite returns nil when the caller may PUT/DELETE settings endpoints.
func requireSettingsWrite(c echo.Context) error {
	if hasSettingsWritePermission(c) {
		return nil
	}
	return c.JSON(http.StatusForbidden, echo.Map{
		"status":  "error",
		"message": "User does not have permission to modify settings",
	})
}
