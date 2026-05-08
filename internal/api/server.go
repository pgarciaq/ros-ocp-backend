package api

import (
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
)

var log *logrus.Entry = logging.GetLogger()
var cfg *config.Config = config.GetConfig()

func StartAPIServer() {
	app := echo.New()
	app.Use(echoprometheus.NewMiddlewareWithConfig(echoprometheus.MiddlewareConfig{
		Subsystem: "rosocp",
		LabelFuncs: map[string]echoprometheus.LabelValueFunc{
			"url": func(c echo.Context, err error) string {
				return c.Path()
			},
		},
	}))

	go func() {
		metrics := echo.New()
		metrics.GET("/metrics", echoprometheus.NewHandler())
		if err := metrics.Start(fmt.Sprintf(":%s", cfg.PrometheusPort)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	app.Use(middleware.RequestLogger())
	app.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodDelete},
	}))

	app.GET("/status", GetAppStatus)
	app.File("/api/cost-management/v1/recommendations/openshift/openapi.json", "openapi.json")

	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	if cfg.RBACEnabled {
		v1.Use(ros_middleware.Rbac)
	}

	// Container recommendations — native engine with Kruize fallback, or legacy-only.
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift", GetRecommendationSetListWithFallback)
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSetWithFallback)
	} else {
		v1.GET("/recommendations/openshift", GetRecommendationSetList)
		v1.GET("/recommendations/openshift/:recommendation-id", GetRecommendationSet)
	}

	// Project/Namespace — native engine with Kruize fallback, or legacy-only.
	if cfg.UseNativeEngine {
		v1.GET("/openshift/namespace/recommendations", GetNamespaceRecommendationSetListWithFallback)
		v1.GET("/recommendations/openshift/namespace/:recommendation-id", GetNamespaceRecommendationSetWithFallback)
	} else {
		v1.GET("/openshift/namespace/recommendations", GetNamespaceRecommendationSetList)
		v1.GET("/recommendations/openshift/namespace/:recommendation-id", GetNamespaceRecommendationSet)
	}

	// Custom recommendation term settings (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/settings/terms", GetTermSettings)
		v1.PUT("/recommendations/openshift/settings/terms", PutTermSettings)
		v1.DELETE("/recommendations/openshift/settings/terms", DeleteTermSettings)
	}

	// Historical tracking and quality metrics (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/history", GetRecommendationHistory)
		v1.GET("/recommendations/openshift/quality", GetRecommendationQuality)
	}

	// Node-level GPU recommendations (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/nodes", GetNodeRecommendations)
	}

	// Fleet-level summary (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/fleet-summary", GetFleetSummary)
	}

	// PVC right-sizing recommendations (native engine only).
	if cfg.UseNativeEngine {
		v1.GET("/recommendations/openshift/pvcs", GetPVCRecommendations)
	}

	s := http.Server{
		Addr:              ":" + cfg.API_PORT, // local dev server
		Handler:           app,
		ReadHeaderTimeout: time.Duration(cfg.ReadHeaderTimeout) * time.Second,
	}
	if err := s.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
