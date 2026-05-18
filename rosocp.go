package main

import (
	"github.com/redhatinsights/ros-ocp-backend/cmd"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func main() {
	cfg := config.GetConfig()
	plugin.ApplyLegacyUseNativeEngineEnv(cfg.UseNativeEngine)
	cmd.Execute()
}
