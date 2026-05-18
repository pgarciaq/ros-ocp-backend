package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/kafka"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
	"github.com/redhatinsights/ros-ocp-backend/internal/services"
	"github.com/redhatinsights/ros-ocp-backend/internal/services/housekeeper"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

var startCmdLog = logging.GetLogger()

var startCmd = &cobra.Command{Use: "start", Short: "Use to start ros-ocp-backend services"}

var processorCmd = &cobra.Command{
	Use:   "processor",
	Short: "starts ros-ocp processor",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("starting ros-ocp processor")
		cfg := config.GetConfig()
		engine.InitGPUEngine(cfg)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go utils.Start_prometheus_server()
		utils.SetupKruizePerformanceProfile()
		if !cfg.UseNativeEngine {
		go func() {
			if err := utils.Start_prometheus_server(); err != nil {
				startCmdLog.Errorf("prometheus metrics server: %v", err)
			}
		}()
		if !plugin.EnabledFor(plugin.KruizePluginName) {
			pool := db.GetPool()
			if pool != nil {
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
		cfg := config.GetConfig()
		engine.InitGPUEngine(cfg)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
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
		cfg := config.GetConfig()
		engine.InitGPUEngine(cfg)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
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
