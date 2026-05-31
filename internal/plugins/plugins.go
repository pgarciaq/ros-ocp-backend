// Package plugins ensures all built-in production plugins are registered via init().
// The example plugin is intentionally excluded here; it exists solely as an authoring
// template for developers and should only be imported explicitly in tests.
package plugins

import (
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/cluster-quota"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/container"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/gpu"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/kruize"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/namespace"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/node"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/pvc"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/quota"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/snapshot"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/vm"
)
