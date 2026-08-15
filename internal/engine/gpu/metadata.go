package gpu

import (
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	libgpu "github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
)

var (
	gpuModelUnrecognized = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_gpu_model_unrecognized_total",
		Help: "Number of times a DCGM-reported GPU model string was not recognized by the catalog. Check logs for 'gpu_metadata: unrecognized GPU model' to identify specific model strings.",
	})

	// Deduplicate log warnings per model string to avoid log spam.
	unrecognizedLogOnce sync.Map
)

// MatchGPUModel resolves a DCGM-reported model name string to a GPUModelSpec.
// Returns nil if the GPU model is not recognized. When nil is returned for a
// non-empty input, a Prometheus counter is incremented and a one-time warning
// is logged to help operators identify gaps in the catalog.
func MatchGPUModel(modelName string) *GPUModelSpec {
	spec := libgpu.MatchGPUModel(modelName)
	if spec == nil && modelName != "" {
		gpuModelUnrecognized.Inc()
		s := strings.ToLower(strings.TrimSpace(modelName))
		if _, loaded := unrecognizedLogOnce.LoadOrStore(s, struct{}{}); !loaded {
			logging.GetLogger().WithField("gpu_model", modelName).Warn("gpu_metadata: unrecognized GPU model — add to gpu_catalog.yaml and matchGPUModelKey")
		}
	}
	return spec
}
