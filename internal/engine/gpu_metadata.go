package engine

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"gopkg.in/yaml.v3"
)

// GPUModelSpec describes the hardware capabilities of a GPU model.
type GPUModelSpec struct {
	Name               string       `yaml:"name"`
	TotalFBMiB         int          `yaml:"total_fb_mib"`
	SMCount            int          `yaml:"sm_count"`
	MIGSupported       bool         `yaml:"mig_supported"`
	ProfilingSupported bool         `yaml:"profiling_supported"`
	Profiles           []MIGProfile `yaml:"profiles"`
}

// MIGProfile describes a single MIG partition configuration.
type MIGProfile struct {
	Name        string  `yaml:"name"`        // e.g. "1g.5gb", "3g.40gb"
	Slices      int     `yaml:"slices"`      // number of GPU slices (1, 2, 3, 4, 7)
	FBSizeMiB   int     `yaml:"fb_size_mib"` // frame buffer for this partition in MiB
	ComputeFrac float64 `yaml:"-"`           // fraction of full GPU compute capacity (Slices/7), computed at load
}

//go:embed gpu_catalog.yaml
var gpuCatalogYAML []byte

type gpuCatalogFile struct {
	Models map[string]GPUModelSpec `yaml:"models"`
}

// gpuModels is the loaded GPU catalog. Populated by init().
var gpuModels map[string]GPUModelSpec

func init() {
	var catalog gpuCatalogFile
	if err := yaml.Unmarshal(gpuCatalogYAML, &catalog); err != nil {
		panic(fmt.Sprintf("gpu_catalog.yaml: parse error: %v", err))
	}
	gpuModels = make(map[string]GPUModelSpec, len(catalog.Models))
	for key, spec := range catalog.Models {
		// Compute MIG profile fractions from slice counts.
		if spec.MIGSupported {
			for i := range spec.Profiles {
				spec.Profiles[i].ComputeFrac = migFrac(spec.Profiles[i].Slices)
			}
		}
		gpuModels[key] = spec
	}
}

func migFrac(profileSMs int) float64 {
	return float64(profileSMs) / 7.0
}

var (
	gpuModelUnrecognized = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rosocp_gpu_model_unrecognized_total",
		Help: "Number of times a DCGM-reported GPU model string was not recognized by the catalog",
	}, []string{"model_name"})

	// Deduplicate log warnings per model string to avoid log spam.
	unrecognizedLogOnce sync.Map
)

// matchGPUModelKey resolves a lowercase model string to gpuModels lookup keys.
func matchGPUModelKey(lower string) string {
	if lower == "" {
		return ""
	}

	// NVIDIA / Tesla product lines — order is explicit (specific before general).
	switch {
	case strings.Contains(lower, "b200"):
		return "B200_180GB"
	case strings.Contains(lower, "h200"):
		return "H200_141GB"
	case strings.Contains(lower, "h100") && strings.Contains(lower, "nvl"):
		return "H100_94GB"
	case strings.Contains(lower, "h100") && strings.Contains(lower, "80gb"):
		return "H100_80GB"

	case strings.Contains(lower, "a100") && strings.Contains(lower, "80gb"):
		return "A100_80GB"
	case strings.Contains(lower, "a100") && strings.Contains(lower, "40gb"):
		return "A100_40GB"

	case strings.Contains(lower, "a30") && !strings.Contains(lower, "a300"):
		return "A30"
	case strings.Contains(lower, "a10g"):
		return "A10G"
	case strings.Contains(lower, "a10") && !strings.Contains(lower, "a100"):
		return "A10"

	case strings.Contains(lower, "l40s"):
		return "L40S"
	case strings.Contains(lower, "l40") && !strings.Contains(lower, "l40s"):
		return "L40"
	case strings.Contains(lower, "l4") && !strings.Contains(lower, "l40"):
		return "L4"

	case strings.Contains(lower, "v100") && strings.Contains(lower, "32gb"):
		return "V100_32GB"
	case strings.Contains(lower, "v100") && strings.Contains(lower, "16gb"):
		return "V100_16GB"

	case strings.Contains(lower, "p100"):
		return "P100"
	case strings.Contains(lower, "p40") && !strings.Contains(lower, "p400"):
		return "P40"

	case strings.Contains(lower, "t4"):
		return "T4"
	default:
		return ""
	}
}

// MatchGPUModel resolves a DCGM-reported model name string to a GPUModelSpec.
// Returns nil if the GPU model is not recognized. When nil is returned for a
// non-empty input, a Prometheus counter is incremented and a one-time warning
// is logged to help operators identify gaps in the catalog.
func MatchGPUModel(modelName string) *GPUModelSpec {
	s := strings.ToLower(strings.TrimSpace(modelName))
	key := matchGPUModelKey(s)
	if key == "" {
		if modelName != "" {
			// Truncate label value to prevent cardinality explosion from garbage input.
			label := modelName
			if len(label) > 64 {
				label = label[:64]
			}
			gpuModelUnrecognized.WithLabelValues(label).Inc()
			if _, loaded := unrecognizedLogOnce.LoadOrStore(s, struct{}{}); !loaded {
				logging.GetLogger().WithField("gpu_model", modelName).Warn("gpu_metadata: unrecognized GPU model — add to gpu_catalog.yaml and matchGPUModelKey")
			}
		}
		return nil
	}
	spec := gpuModels[key]
	specCopy := spec
	return &specCopy
}

// GPUModelCount returns the number of GPU models in the catalog.
// Useful for health checks and tests.
func GPUModelCount() int {
	return len(gpuModels)
}
