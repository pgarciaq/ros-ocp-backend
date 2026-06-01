package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	_ "go.uber.org/automaxprocs"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/kafka"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
	"github.com/redhatinsights/ros-ocp-backend/internal/services"
	"github.com/redhatinsights/ros-ocp-backend/internal/services/housekeeper"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

var startCmdLog = logging.GetLogger()

// runServiceStartup runs plugin registry init, tag DB health check, and startup logging.
func runServiceStartup(ctx context.Context) {
	plugin.Init()
	_ = db.GetPool()
	tags.RunStartupHealthCheck(ctx)
	logEnabledPlugins()
	engine.LogSettingsLockedStartup(startCmdLog.Warnf)
}

// logEnabledPlugins emits the enabled-plugin allowlist after registry filtering.
// Tested indirectly via E2E; startup log verified on SNO cluster.
func logEnabledPlugins() {
	enabled := plugin.Enabled()
	names := make([]string, 0, len(enabled))
	for _, p := range enabled {
		names = append(names, p.Name())
	}
	startCmdLog.Infof("Plugin registry: enabled plugins: [%s]", strings.Join(names, ", "))
}

var startCmd = &cobra.Command{Use: "start", Short: "Use to start ros-ocp-backend services"}

func ensureStartupPartitions(ctx context.Context) {
	pool := db.GetPool()
	if pool == nil {
		return
	}
	ingestion.EnsureIngestPartitionsAtStartup(ctx, pool)
	engine.EnsureRecommendationPartitionsAtStartup(ctx, pool)
}

var processorCmd = &cobra.Command{
	Use:   "processor",
	Short: "starts ros-ocp processor",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("starting ros-ocp processor")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go utils.Start_prometheus_server()
		utils.SetupKruizePerformanceProfile()
		if !cfg.UseNativeEngine {
		runServiceStartup(ctx)
		cfg := config.GetConfig()
		engine.InitGPUEngine(cfg)
		go func() {
			if err := utils.Start_prometheus_server(); err != nil {
				startCmdLog.Errorf("prometheus metrics server: %v", err)
			}
		}()
		if !plugin.EnabledFor(plugin.KruizePluginName) {
			pool := db.GetPool()
			if pool != nil {
				ensureStartupPartitions(ctx)
				go engine.StartRetentionTicker(ctx, pool, cfg.RetentionMonths)
			}
		} else {
			utils.Setup_kruize_performance_profile()
		}
		kafka.StartConsumer(ctx, cfg.UploadTopic, services.ProcessReport)
	},
}

var recommendationPollerCmd = &cobra.Command{
	Use:   "recommendation-poller",
	Short: "starts ros-ocp recommendation-poller",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("starting ros-ocp recommendation-poller")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		runServiceStartup(ctx)
		cfg := config.GetConfig()
		engine.InitGPUEngine(cfg)
		go func() {
			if err := utils.Start_prometheus_server(); err != nil {
				startCmdLog.Errorf("prometheus metrics server: %v", err)
			}
		}()
		kafka.StartConsumer(ctx, cfg.RecommendationTopic, services.PollForRecommendations, false)
	},
}

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "starts ros-ocp api server",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting ros-ocp API server")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		runServiceStartup(ctx)
		cfg := config.GetConfig()
		engine.InitGPUEngine(cfg)
		if pool := db.GetPool(); pool != nil {
			ensureStartupPartitions(ctx)
		}
		api.StartAPIServer(ctx)
	},
}

var houseKeeperCmd = &cobra.Command{
	Use:   "housekeeper",
	Short: "starts ros-ocp housekeeper service",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("starting ros-ocp housekeeper service")
		sourcesFlag, _ := cmd.Flags().GetBool("sources")
		partitionFlag, _ := cmd.Flags().GetBool("partitions")
		if sourcesFlag {
			housekeeper.StartSourcesListenerService()
		}
		if partitionFlag {
			housekeeper.DeletePartitions()
		}
	},
}

var sources, partitions bool

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.AddCommand(processorCmd)
	startCmd.AddCommand(recommendationPollerCmd)
	startCmd.AddCommand(apiCmd)
	startCmd.AddCommand(houseKeeperCmd)

	houseKeeperCmd.Flags().BoolVar(&sources, "sources", false, "starts sources listener service")
	houseKeeperCmd.Flags().BoolVar(&partitions, "partitions", false, "deletes older partitions")
	houseKeeperCmd.MarkFlagsOneRequired("sources", "partitions")
	houseKeeperCmd.MarkFlagsMutuallyExclusive("sources", "partitions")
}
