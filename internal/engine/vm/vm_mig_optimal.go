package vm

import (
	"math"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

// OptimalMIGProfile selects the smallest MIG profile with frame buffer at least
// observedMaxFBMiB * headroom. MIG profile definitions come from gpu_catalog.yaml via engine.MatchGPUModel.
// observedMaxFBMiB * headroomFactor (default 1.2). Returns current profile when already
// optimal, or a larger profile when the workload needs more memory.
func OptimalMIGProfile(modelName, currentProfile string, observedMaxFBMiB float64, avgUtilBP int32) string {
	spec := engine.MatchGPUModel(modelName)
	if spec == nil || !spec.MIGSupported || len(spec.Profiles) == 0 {
		return ""
	}
	if observedMaxFBMiB <= 0 && avgUtilBP <= 0 {
		return currentProfile
	}

	const headroom = 1.2
	requiredMiB := observedMaxFBMiB * headroom
	if requiredMiB <= 0 {
		requiredMiB = 1
	}

	best := ""
	for _, p := range spec.Profiles {
		if float64(p.FBSizeMiB) >= requiredMiB {
			best = p.Name
			break
		}
	}
	if best == "" {
		// Workload exceeds largest partition — recommend full GPU.
		return "full_gpu"
	}

	currentIdx := migProfileIndex(spec.Profiles, currentProfile)
	if currentIdx >= 0 {
		if float64(spec.Profiles[currentIdx].FBSizeMiB) < requiredMiB {
			// Current partition is too small — upsize to smallest profile that fits.
			return best
		}
		// Current fits workload; recommend smallest sufficient profile (may downsize).
	}
	return best
}

func migProfileIndex(profiles []engine.MIGProfile, name string) int {
	name = strings.TrimSpace(name)
	for i, p := range profiles {
		if p.Name == name {
			return i
		}
	}
	return -1
}

// recommendedMIGSliceCount returns ceil(avgUtil * maxSlices) for MIG-capable GPUs.
func recommendedMIGSliceCount(avgUtilBP, maxSlices int32) int32 {
	if maxSlices <= 0 {
		return 0
	}
	util := float64(avgUtilBP) / 10000.0
	if util <= 0 {
		return 1
	}
	slices := int32(math.Ceil(util * float64(maxSlices)))
	if slices < 1 {
		return 1
	}
	if slices > maxSlices {
		return maxSlices
	}
	return slices
}

// recommendedTimeSliceCount suggests sharing replicas for non-MIG GPUs (e.g. T4).
// Deprecated: use RecommendVMTimeSlicingForDevice for multi-signal production-quality guidance.
func recommendedTimeSliceCount(avgUtilBP int32) int32 {
	if avgUtilBP <= 0 {
		return 0
	}
	util := float64(avgUtilBP) / 10000.0
	if util <= 0 {
		return 0
	}
	count := int32(math.Ceil(1.0 / util))
	if count < 2 {
		return 0
	}
	if count > 16 {
		return 16
	}
	return count
}
