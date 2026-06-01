package api

import (
	"fmt"

	"github.com/labstack/echo/v4"
)

const thresholdSettingsAPIPrefix = "/api/cost-management/v1/recommendations/openshift/settings"

// thresholdRecommendationTypes are sizing plugins served by dedicated /settings/{type} paths.
var thresholdRecommendationTypes = []string{"container", "namespace", "node", "gpu", "pvc"}

// RegisterThresholdSettingsRoutes mounts canonical per-type threshold settings routes and the
// deprecated /settings/thresholds?recommendation_type= alias.
func RegisterThresholdSettingsRoutes(v1 *echo.Group) {
	v1.GET("/recommendations/openshift/settings/thresholds", GetThresholdSettings)
	v1.PUT("/recommendations/openshift/settings/thresholds", PutThresholdSettings)
	v1.DELETE("/recommendations/openshift/settings/thresholds", DeleteThresholdSettings)

	for _, recType := range thresholdRecommendationTypes {
		path := "/recommendations/openshift/settings/" + recType
		rt := recType
		v1.GET(path, makeGetThresholdSettings(rt))
		v1.PUT(path, makePutThresholdSettings(rt))
		v1.DELETE(path, makeDeleteThresholdSettings(rt))
	}
}

func makeGetThresholdSettings(recType string) echo.HandlerFunc {
	return func(c echo.Context) error {
		return respondGetThresholdSettings(c, recType, false)
	}
}

func makePutThresholdSettings(recType string) echo.HandlerFunc {
	return func(c echo.Context) error {
		return respondPutThresholdSettings(c, recType, false)
	}
}

func makeDeleteThresholdSettings(recType string) echo.HandlerFunc {
	return func(c echo.Context) error {
		return respondDeleteThresholdSettings(c, recType, false)
	}
}

func thresholdSuccessorLink(recType string) string {
	return fmt.Sprintf("<%s/%s>; rel=\"successor-version\"", thresholdSettingsAPIPrefix, recType)
}

func setThresholdDeprecationHeaders(c echo.Context, recType string) {
	c.Response().Header().Set("Deprecation", "true")
	c.Response().Header().Set("Link", thresholdSuccessorLink(recType))
}
