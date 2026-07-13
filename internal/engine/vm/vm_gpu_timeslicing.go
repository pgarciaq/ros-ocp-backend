package vm

import (
	"fmt"
	"math"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// VMTimeSliceRecommendation holds production-quality vGPU time-slicing guidance for a VM GPU.
type VMTimeSliceRecommendation struct {
	EnableTimeSlicing      bool
	RecommendedSliceCount  int32
	Confidence             string // high, moderate, low
	Rationale              string
	SMUtilAvg              float64
	DRAMUtilAvg            float64
	FBUsedFraction         float64
	MinSlices              int32
	MaxSlices              int32
	RecommendedVGPUProfile string
	FBUnsafe               bool
	PreferMIG              bool
}

// RecommendVMTimeSlicing aggregates per-device signals across all GPUs on a VM.
func RecommendVMTimeSlicing(devices []model.GPUDeviceDigest, observationDays int, cfg VMRecConfig) VMTimeSliceRecommendation {
	if len(devices) == 0 {
		return VMTimeSliceRecommendation{Rationale: "no GPU devices observed"}
	}
	if observationDays < 1 {
		observationDays = 1
	}

	var (
		sumSM, sumDRAM, sumFB float64
		worst                 VMTimeSliceRecommendation
		anyEnable             bool
		minConfRank           = 3
	)
	confRank := map[string]int{"high": 0, "moderate": 1, "low": 2}

	for _, dev := range devices {
		rec := RecommendVMTimeSlicingForDevice(dev, observationDays, cfg)
		sumSM += rec.SMUtilAvg
		sumDRAM += rec.DRAMUtilAvg
		sumFB += rec.FBUsedFraction
		if rec.EnableTimeSlicing {
			anyEnable = true
		}
		if rank, ok := confRank[rec.Confidence]; ok && rank < minConfRank {
			minConfRank = rank
			worst.Confidence = rec.Confidence
		}
		if rec.EnableTimeSlicing && (worst.RecommendedSliceCount == 0 || rec.RecommendedSliceCount < worst.RecommendedSliceCount) {
			worst = rec
		} else if !anyEnable && rec.FBUnsafe && !worst.FBUnsafe {
			worst = rec
		} else if worst.RecommendedSliceCount == 0 && rec.RecommendedVGPUProfile != "" {
			if worst.RecommendedVGPUProfile == "" {
				worst.RecommendedVGPUProfile = rec.RecommendedVGPUProfile
			}
		}
	}

	n := float64(len(devices))
	out := VMTimeSliceRecommendation{
		SMUtilAvg:              sumSM / n,
		DRAMUtilAvg:            sumDRAM / n,
		FBUsedFraction:         sumFB / n,
		MinSlices:              cfg.GPUTimeSliceMinReplicas,
		MaxSlices:              vmEffectiveMaxSlices(cfg, sumDRAM/n),
		RecommendedVGPUProfile: worst.RecommendedVGPUProfile,
	}
	if worst.Confidence != "" {
		out.Confidence = worst.Confidence
	} else {
		out.Confidence = vmTimeSliceConfidenceLevel(observationDays, nil)
	}

	if anyEnable {
		out.EnableTimeSlicing = true
		out.RecommendedSliceCount = worst.RecommendedSliceCount
		out.Rationale = worst.Rationale
		if out.RecommendedSliceCount == 0 {
			agg := vmDeviceMetrics(devices[0])
			for i := 1; i < len(devices); i++ {
				m := vmDeviceMetrics(devices[i])
				if m.sm > agg.sm {
					agg = m
				}
				if m.dram > agg.dram {
					agg.dram = m.dram
				}
				if m.fb > agg.fb {
					agg.fb = m.fb
				}
			}
			slices, ok := vmComputeSliceCount(agg.sm, agg.dram, agg.fb, cfg)
			if ok {
				out.RecommendedSliceCount = slices
				out.Rationale = vmTimeSliceRationale(agg.sm, agg.dram, agg.fb, slices, observationDays, out.Confidence, false, false)
			}
		}
		return out
	}

	out.EnableTimeSlicing = false
	if worst.FBUnsafe || out.FBUsedFraction >= engine.VMBasisPointsToFraction(cfg.GPUTimeSliceFBSafetyThresholdBP) {
		out.FBUnsafe = true
		out.Rationale = fmt.Sprintf(
			"time-slicing not recommended: GPU frame-buffer usage %.0f%% exceeds safety threshold %.0f%%",
			out.FBUsedFraction*100, engine.VMBasisPointsToFraction(cfg.GPUTimeSliceFBSafetyThresholdBP)*100,
		)
	} else if worst.PreferMIG {
		out.PreferMIG = true
		out.Rationale = "MIG-capable GPU — prefer MIG partitioning over time-slicing"
	} else {
		out.Rationale = worst.Rationale
		if out.Rationale == "" {
			out.Rationale = "GPU utilization does not justify time-slicing replicas"
		}
	}
	if out.RecommendedVGPUProfile == "" && len(devices) == 1 {
		out.RecommendedVGPUProfile = engine.RecommendVGPUProfile(devices[0].Model, devices[0].FBUsedMaxMiB)
	}
	return out
}

// RecommendVMTimeSlicingForDevice evaluates time-slicing for a single aggregated GPU device.
func RecommendVMTimeSlicingForDevice(dev model.GPUDeviceDigest, observationDays int, cfg VMRecConfig) VMTimeSliceRecommendation {
	dev.Model = vmCanonicalGPUModel(dev.Model)
	minSlices := cfg.GPUTimeSliceMinReplicas
	maxSlices := cfg.GPUTimeSliceMaxReplicas
	if minSlices < 1 {
		minSlices = 2
	}
	if maxSlices < minSlices {
		maxSlices = minSlices
	}

	metrics := vmDeviceMetrics(dev)
	out := VMTimeSliceRecommendation{
		SMUtilAvg:      metrics.sm,
		DRAMUtilAvg:    metrics.dram,
		FBUsedFraction: metrics.fb,
		MinSlices:      minSlices,
		MaxSlices:      vmEffectiveMaxSlices(cfg, metrics.dram),
		Confidence:     vmTimeSliceConfidenceLevel(observationDays, nil),
	}

	spec := engine.MatchGPUModel(dev.Model)
	migCapable := strings.TrimSpace(dev.MIGProfile) != "" || dev.MaxSlices > 0 || (spec != nil && spec.MIGSupported)
	if migCapable && strings.TrimSpace(dev.MIGProfile) == "" {
		out.PreferMIG = true
		out.Rationale = "MIG-capable GPU — prefer MIG partitioning over time-slicing"
		return out
	}

	fbThreshold := engine.VMBasisPointsToFraction(cfg.GPUTimeSliceFBSafetyThresholdBP)
	if fbThreshold <= 0 {
		fbThreshold = 0.80
	}
	if metrics.fb >= fbThreshold {
		out.FBUnsafe = true
		out.Rationale = fmt.Sprintf(
			"time-slicing unsafe: frame-buffer usage %.0f%% exceeds %.0f%% threshold",
			metrics.fb*100, fbThreshold*100,
		)
		out.RecommendedVGPUProfile = engine.RecommendVGPUProfile(dev.Model, dev.FBUsedMaxMiB)
		return out
	}

	slices, ok := vmComputeSliceCount(metrics.sm, metrics.dram, metrics.fb, cfg)
	if !ok || slices < minSlices {
		out.Rationale = "peak utilization too high for safe time-slicing replica count"
		return out
	}

	out.EnableTimeSlicing = true
	out.RecommendedSliceCount = slices
	out.Rationale = vmTimeSliceRationale(metrics.sm, metrics.dram, metrics.fb, slices, observationDays, out.Confidence, false, false)
	out.RecommendedVGPUProfile = engine.RecommendVGPUProfile(dev.Model, dev.FBUsedMaxMiB)
	return out
}

type vmDeviceUtilMetrics struct {
	sm   float64
	dram float64
	fb   float64
}

func vmDeviceMetrics(dev model.GPUDeviceDigest) vmDeviceUtilMetrics {
	sm := engine.VMBasisPointsToFraction(dev.SMActiveAvgBP)
	if sm <= 0 {
		sm = engine.VMBasisPointsToFraction(dev.UtilAvgBP)
	}
	dram := engine.VMBasisPointsToFraction(dev.DRAMAvgBP)
	return vmDeviceUtilMetrics{
		sm:   sm,
		dram: dram,
		fb:   engine.VMFBUsedFraction(dev),
	}
}

func vmEffectiveMaxSlices(cfg VMRecConfig, dramUtil float64) int32 {
	maxSlices := cfg.GPUTimeSliceMaxReplicas
	if maxSlices < 1 {
		maxSlices = 16
	}
	dramThreshold := engine.VMBasisPointsToFraction(cfg.GPUTimeSliceDRAMPenaltyThresholdBP)
	if dramThreshold <= 0 {
		dramThreshold = 0.50
	}
	if dramUtil >= dramThreshold {
		reduced := int32(math.Max(float64(cfg.GPUTimeSliceMinReplicas), float64(maxSlices)/2))
		if reduced < maxSlices {
			return reduced
		}
	}
	return maxSlices
}

// vmComputeSliceCount mirrors container computeReplicas using SM, DRAM, and FB fractions.
func vmComputeSliceCount(avgSM, avgDRAM, avgFBFrac float64, cfg VMRecConfig) (int32, bool) {
	peak := avgSM
	if avgDRAM > peak {
		peak = avgDRAM
	}
	if avgFBFrac > peak {
		peak = avgFBFrac
	}
	maxReplicas := int(vmEffectiveMaxSlices(cfg, avgDRAM))
	minReplicas := int(cfg.GPUTimeSliceMinReplicas)
	if minReplicas < 1 {
		minReplicas = 2
	}
	if peak <= 0 {
		return int32(maxReplicas), true
	}
	r := int(math.Ceil(1.0 / peak))
	if r < minReplicas {
		return 0, false
	}
	if r > maxReplicas {
		r = maxReplicas
	}
	return int32(r), true
}

func vmTimeSliceConfidenceLevel(observationDays int, utilSamples []int32) string {
	cv := engine.VMUtilCoefficientOfVariation(utilSamples)
	if observationDays >= 7 && cv < 0.25 {
		return "high"
	}
	if observationDays >= 3 || (observationDays >= 1 && cv < 0.40) {
		return "moderate"
	}
	return "low"
}

func vmTimeSliceRationale(sm, dram, fb float64, slices int32, obsDays int, confidence string, fbUnsafe, preferMIG bool) string {
	if preferMIG {
		return "MIG-capable GPU — prefer MIG partitioning over time-slicing"
	}
	if fbUnsafe {
		return fmt.Sprintf("time-slicing not recommended: frame-buffer usage %.0f%% too high", fb*100)
	}
	return fmt.Sprintf(
		"recommend %d time-slice replicas (SM %.1f%%, DRAM %.1f%%, FB %.1f%% over %d day(s); confidence %s)",
		slices, sm*100, dram*100, fb*100, obsDays, confidence,
	)
}

func vmGPUObservationDays(digests []model.DailyVMDigest) int {
	n := 0
	for _, d := range digests {
		if d.HasGPU || len(d.Devices) > 0 {
			n++
		}
	}
	return n
}
