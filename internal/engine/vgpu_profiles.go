package engine

import (
	_ "embed"
	"fmt"
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"gopkg.in/yaml.v3"
)

// VGPUProfile describes a licensed NVIDIA vGPU (GRID) profile.
type VGPUProfile struct {
	Name         string `yaml:"name"`
	FBMiB        int    `yaml:"fb_mib"`
	MaxInstances int    `yaml:"max_instances"`
}

// VGPUModelSpec holds vGPU profiles for a GPU model family.
type VGPUModelSpec struct {
	SupportedModels []string      `yaml:"supported_models"`
	Profiles        []VGPUProfile `yaml:"profiles"`
}

//go:embed vgpu_profiles.yaml
var vgpuProfilesYAML []byte

type vgpuProfilesFile struct {
	Models map[string]VGPUModelSpec `yaml:"models"`
}

var vgpuModels map[string]VGPUModelSpec

func loadVGPUProfiles() error {
	var catalog vgpuProfilesFile
	if err := yaml.Unmarshal(vgpuProfilesYAML, &catalog); err != nil {
		return fmt.Errorf("vgpu_profiles.yaml: parse error: %w", err)
	}
	models := make(map[string]VGPUModelSpec, len(catalog.Models))
	for key, spec := range catalog.Models {
		models[key] = spec
	}
	vgpuModels = models
	return nil
}

func init() {
	if err := loadVGPUProfiles(); err != nil {
		logging.GetLogger().Fatal(err)
	}
}

// MatchVGPUModel returns vGPU catalog entry for a GPU model name, or nil.
func MatchVGPUModel(modelName string) *VGPUModelSpec {
	if spec := MatchGPUModel(modelName); spec != nil {
		if entry, ok := vgpuModels[spec.Name]; ok {
			return &entry
		}
	}
	for _, entry := range vgpuModels {
		for _, name := range entry.SupportedModels {
			if name == modelName {
				return &entry
			}
		}
	}
	return nil
}

// RecommendVGPUProfile returns the smallest vGPU profile with frame buffer at least
// observedMaxFBMiB * headroom. Empty string when no catalog entry or workload exceeds largest profile.
func RecommendVGPUProfile(modelName string, observedMaxFBMiB float64) string {
	entry := MatchVGPUModel(modelName)
	if entry == nil || len(entry.Profiles) == 0 {
		return ""
	}
	const headroom = 1.2
	requiredMiB := observedMaxFBMiB * headroom
	if requiredMiB <= 0 {
		requiredMiB = 1
	}
	for _, p := range entry.Profiles {
		if float64(p.FBMiB) >= requiredMiB {
			return p.Name
		}
	}
	// Workload exceeds largest vGPU partition — caller may keep full GPU.
	if len(entry.Profiles) > 0 {
		return entry.Profiles[len(entry.Profiles)-1].Name
	}
	return ""
}

// VGPUProfileFBMiB returns frame buffer MiB for a profile name, or 0 if unknown.
func VGPUProfileFBMiB(modelName, profileName string) int {
	entry := MatchVGPUModel(modelName)
	if entry == nil {
		return 0
	}
	for _, p := range entry.Profiles {
		if p.Name == profileName {
			return p.FBMiB
		}
	}
	return 0
}

func VMBasisPointsToFraction(bp int32) float64 {
	if bp <= 0 {
		return 0
	}
	return float64(bp) / 10000.0
}

func VMFBUsedFraction(dev model.GPUDeviceDigest) float64 {
	spec := MatchGPUModel(dev.Model)
	if spec == nil || spec.TotalFBMiB <= 0 {
		return 0
	}
	fb := dev.FBUsedMaxMiB
	if fb <= 0 && dev.FBUsedAvgMiB > 0 {
		fb = dev.FBUsedAvgMiB
	}
	if fb <= 0 {
		return 0
	}
	frac := fb / float64(spec.TotalFBMiB)
	if frac > 1 {
		return 1
	}
	return frac
}

func VMUtilCoefficientOfVariation(samples []int32) float64 {
	if len(samples) < 2 {
		return 0
	}
	var sum float64
	for _, v := range samples {
		sum += float64(v)
	}
	mean := sum / float64(len(samples))
	if mean <= 0 {
		return 0
	}
	var sq float64
	for _, v := range samples {
		d := float64(v) - mean
		sq += d * d
	}
	return math.Sqrt(sq/float64(len(samples))) / mean
}
