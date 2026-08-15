package vm

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/gpu"
	libvm "github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

// RecommendVM computes a VM recommendation from aggregated daily digests.
func RecommendVM(
	digests []Digest,
	cfg VMRecConfig,
	term TermWindow,
	engine string,
	clusterTypes []InstanceType,
	prefCtx *VMPreferenceContext,
	clusterCtx *ClusterContext,
	nodeMemGiBByNode map[string]float64,
) (*Recommendation, error) {
	for i := range digests {
		if digests[i].GPUModel != "" {
			_ = gpu.MatchGPUModel(digests[i].GPUModel)
		}
		for _, dev := range digests[i].Devices {
			if dev.Model != "" {
				_ = gpu.MatchGPUModel(dev.Model)
			}
		}
	}
	return libvm.RecommendVM(digests, cfg, term, engine, clusterTypes, prefCtx, clusterCtx, nodeMemGiBByNode)
}

func vmExplFromRecommendation(r Recommendation) core.VMExplanationFactors {
	return libvm.VMExplFromRecommendation(r)
}
