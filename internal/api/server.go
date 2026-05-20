package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"

	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

var log *logrus.Entry = logging.GetLogger()
var cfg *config.Config = config.GetConfig()

// pluginRecommendationRoutesActive reports whether gpu/node/pvc/snapshot HTTP routes are registered:
// those plugins omit routes entirely when Kruize owns recommendations or when the plugin is disabled.
func pluginRecommendationRoutesActive(pluginName string) bool {
	if plugin.EnabledFor(plugin.KruizePluginName) {
		return false
	}
	return plugin.EnabledFor(pluginName)
}

func disabledPluginRoute404(pluginName string) echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": fmt.Sprintf("plugin '%s' is not enabled", pluginName),
		})
	}
}

// registerDisabledPluginRouteGuards installs 404 handlers for plugin URL prefixes when the plugin
// does not register real routes, so requests do not fall through to /recommendations/openshift/:recommendation-id
// (which treats e.g. "gpu" as a UUID and returns 400).
func registerDisabledPluginRouteGuards(v1 *echo.Group) {
	if !pluginRecommendationRoutesActive("gpu") {
		v1.GET("/recommendations/openshift/gpu", disabledPluginRoute404("gpu"))
		v1.GET("/recommendations/openshift/gpu/*", disabledPluginRoute404("gpu"))
	}
	if !pluginRecommendationRoutesActive("node") {
		v1.GET("/recommendations/openshift/nodes", disabledPluginRoute404("node"))
		v1.GET("/recommendations/openshift/nodes/*", disabledPluginRoute404("node"))
	}
	if !pluginRecommendationRoutesActive("pvc") {
		v1.GET("/recommendations/openshift/pvcs", disabledPluginRoute404("pvc"))
	}
	if !pluginRecommendationRoutesActive("snapshot") {
		v1.GET("/recommendations/openshift/snapshots", disabledPluginRoute404("snapshot"))
		v1.GET("/recommendations/openshift/settings/snapshot", disabledPluginRoute404("snapshot"))
		v1.PUT("/recommendations/openshift/settings/snapshot", disabledPluginRoute404("snapshot"))
	}
}

// StartAPIServer runs the REST API and Prometheus metrics listener until ctx is cancelled,
// then shuts both down gracefully.
func StartAPIServer(ctx context.Context) {
	app := echo.New()
	app.Use(echoprometheus.NewMiddlewareWithConfig(echoprometheus.MiddlewareConfig{
		Subsystem: "rosocp",
		LabelFuncs: map[string]echoprometheus.LabelValueFunc{
			"url": func(c echo.Context, err error) string {
				return c.Path()
			},
		},
	}))

	metricsEcho := echo.New()
	metricsEcho.GET("/metrics", echoprometheus.NewHandler())
	metricsErrCh := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%s", cfg.PrometheusPort)
		if err := metricsEcho.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			metricsErrCh <- err
		}
	}()

	app.Pre(middleware.RemoveTrailingSlash())
	app.Use(middleware.RequestID())
	app.Use(middleware.RequestLogger())
	app.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodDelete},
	}))

	app.GET("/status", GetAppStatus)
	app.GET("/readyz", GetReadyz)
	app.GET("/api/cost-management/v1/recommendations/openshift/openapi.json", ServeFilteredOpenAPI)

	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	if cfg.RBACEnabled {
		v1.Use(ros_middleware.Rbac)
	}

	nativeRecommendationRoutes := !plugin.EnabledFor(plugin.KruizePluginName)

	// Container recommendations (list + detail) stay in this package rather than a plugin APIProvider:
	// they pair native handlers with Kruize-specific fallbacks, and Echo requires registering this
	// parameterized `/.../:recommendation-id` route after static paths and plugin routes (see below).

	// Container recommendations — native engine with Kruize fallback, or legacy-only.
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift", GetRecommendationSetListWithFallback)
	} else {
		v1.GET("/recommendations/openshift", GetRecommendationSetList)
	}

	// Namespace routes are registered by namespace.APIProvider (see plugin.APIProviders).

	// Custom recommendation term settings (native engine only).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/settings/terms", GetTermSettings)
		v1.PUT("/recommendations/openshift/settings/terms", PutTermSettings)
		v1.DELETE("/recommendations/openshift/settings/terms", DeleteTermSettings)
	}

	// Historical tracking and quality metrics (native engine only).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/history", GetRecommendationHistory)
		v1.GET("/recommendations/openshift/quality", GetRecommendationQuality)
	}

	// GPU / node utilization routes are registered by gpu/node APIProvider plugins.

	// Fleet-level summary (native engine only).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/fleet-summary", GetFleetSummary)
	}

	// Plugin-provided routes ([plugin.APIProviders] returns individually enabled plugins,
	// without Kruize mutual exclusivity, so namespace endpoints stay available in Kruize mode).
	for _, ap := range plugin.APIProviders() {
		ap.RegisterRoutes(v1)
	}

	registerDisabledPluginRouteGuards(v1)

	// Parameterized container recommendation detail — after static paths and plugin routes (ordering; see note above).
	if nativeRecommendationRoutes {
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSetWithFallback)
	} else {
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSet)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.API_PORT,
		Handler:           app,
		ReadHeaderTimeout: time.Duration(cfg.ReadHeaderTimeout) * time.Second,
	}

	apiErrCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			apiErrCh <- err
		}
	}()

	select {
	case err := <-apiErrCh:
		log.Fatalf("api server failed to start: %v", err)
	case err := <-metricsErrCh:
		log.Fatalf("metrics server failed to start: %v", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := metricsEcho.Shutdown(shutdownCtx); err != nil {
		log.Warnf("metrics server shutdown: %v", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warnf("api server shutdown: %v", err)
	}
}
