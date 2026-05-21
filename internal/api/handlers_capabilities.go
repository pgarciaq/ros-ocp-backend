package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

type capabilityItem struct {
	Name          string `json:"name"`
	SupportsTerms bool   `json:"supports_terms"`
	Enabled       bool   `json:"enabled"`
}

type capabilitiesResponse struct {
	RecommendationTypes []capabilityItem `json:"recommendation_types"`
}

// GetCapabilities handles GET /recommendations/openshift/settings/capabilities.
// It lists all registered production plugins, their enabled state, and whether they
// support configurable recommendation terms. Disabled plugins are included so that
// API consumers can discover what domains exist even if temporarily turned off.
// The Kruize legacy plugin is excluded when disabled since it represents a mutually
// exclusive engine mode rather than a feature domain.
func GetCapabilities(c echo.Context) error {
	allPlugins := plugin.All()
	var items []capabilityItem
	for _, p := range allPlugins {
		if p.Name() == plugin.KruizePluginName && !p.Enabled() {
			continue
		}
		_, supportTerms := p.(plugin.TermProvider)
		items = append(items, capabilityItem{
			Name:          p.Name(),
			SupportsTerms: supportTerms,
			Enabled:       p.Enabled(),
		})
	}

	return c.JSON(http.StatusOK, capabilitiesResponse{RecommendationTypes: items})
}
