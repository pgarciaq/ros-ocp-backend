package core_test

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	libtypes "github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func TestColumnConstantsAliasLibrobneTypes(t *testing.T) {
	aliases := []struct {
		name string
		got  core.Column
		want libtypes.Column
	}{
		{"none", core.ColNone, libtypes.ColNone},
		{"cpu_p50", core.ColCPUUsageP50MC, libtypes.ColCPUUsageP50MC},
		{"cpu_p60", core.ColCPUUsageP60MC, libtypes.ColCPUUsageP60MC},
		{"cpu_p95", core.ColCPUUsageP95MC, libtypes.ColCPUUsageP95MC},
		{"cpu_p98", core.ColCPUUsageP98MC, libtypes.ColCPUUsageP98MC},
		{"cpu_p99", core.ColCPUUsageP99MC, libtypes.ColCPUUsageP99MC},
		{"cpu_max", core.ColCPUUsageMaxMC, libtypes.ColCPUUsageMaxMC},
		{"cpu_mean", core.ColCPUUsageMeanMC, libtypes.ColCPUUsageMeanMC},
		{"memory_p50", core.ColMemUsageP50KiB, libtypes.ColMemUsageP50KiB},
		{"memory_p60", core.ColMemUsageP60KiB, libtypes.ColMemUsageP60KiB},
		{"memory_p95", core.ColMemUsageP95KiB, libtypes.ColMemUsageP95KiB},
		{"memory_p98", core.ColMemUsageP98KiB, libtypes.ColMemUsageP98KiB},
		{"memory_p99", core.ColMemUsageP99KiB, libtypes.ColMemUsageP99KiB},
		{"memory_max", core.ColMemUsageMaxKiB, libtypes.ColMemUsageMaxKiB},
		{"memory_mean", core.ColMemUsageMeanKiB, libtypes.ColMemUsageMeanKiB},
	}

	for _, alias := range aliases {
		t.Run(alias.name, func(t *testing.T) {
			// The core facade must expose each descriptor with the same value as librobne/types.
			if alias.got != alias.want {
				t.Fatalf("core column alias = %d, want librobne/types value %d", alias.got, alias.want)
			}
		})
	}
}
