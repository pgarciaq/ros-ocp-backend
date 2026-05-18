// Package plugins ensures all built-in plugins are registered via init().
package plugins

import (
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/container"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/gpu"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/node"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/example"
)
