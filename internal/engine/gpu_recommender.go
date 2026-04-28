package engine

import (
	"math"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// GPUClassification represents the utilization classification of a GPU workload.
type GPUClassification string

const (
	GPUClassIdle                  GPUClassification = "idle"
	GPUClassUnderutilized         GPUClassification = "underutilized"
	GPUClassMemoryBound           GPUClassification = "memory_bound"
	GPUClassComputeBoundUnderutil GPUClassification = "compute_bound_underutil"
	GPUClassWellUtilized          GPUClassification = "well_utilized"
)

// GPUDigestRow holds one daily GPU digest row for a single container.
type GPUDigestRow struct {
	IntervalStart       time.Time
	GPUModelName        string
	GPUProfileName      string
	FBUsageMinMiB       float32
	FBUsageMaxMiB       float32
	FBUsageAvgMiB       float32
	TensorPipeActiveMin float32
	TensorPipeActiveMax float32
	TensorPipeActiveAvg float32
	DRAMActiveMin       float32
	DRAMActiveMax       float32
	DRAMActiveAvg       float32
	SMActiveMin         float32
	SMActiveMax         float32
	SMActiveAvg         float32
}

// GPURec holds the GPU recommendation for a single container.
type GPURec struct {
	GPUModelName           string
	CurrentGPUProfile      string            // current MIG profile or "" for full GPU
	Classification         GPUClassification // empty if no profiling data (Tier 2)
	RecommendedGPUProfile  string            // recommended MIG profile, "full_gpu", or ""
	MemoryBoundDetected    bool
	Confidence             float32
	TensorPipeActiveAvg    float32
	DRAMActiveAvg          float32
	SMActiveAvg            float32
	FBUsageMaxMiB          float32
	EstimatedGPUSavingsUSD *float32 // nil if no cost data
	NotificationCodes      []int16
	HasProfilingData       bool
}

func gpuThreshold(envKey string, defaultVal float64) float64 {
	if v := os.Getenv(envKey); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

var (
	gpuIdleThreshold       = gpuThreshold("ROS_GPU_IDLE_THRESHOLD", 0.02)
	gpuUnderutilizedSM     = gpuThreshold("ROS_GPU_UNDERUTILIZED_SM_THRESHOLD", 0.25)
	gpuUnderutilizedTensor = gpuThreshold("ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD", 0.15)
	gpuMemBoundDRAM        = gpuThreshold("ROS_GPU_MEMBOUND_DRAM_THRESHOLD", 0.60)
	gpuMemBoundTensor      = gpuThreshold("ROS_GPU_MEMBOUND_TENSOR_THRESHOLD", 0.15)
	gpuFBHeadroomFactor    = gpuThreshold("ROS_GPU_FB_HEADROOM_FACTOR", 1.20)
)

// ClassifyGPUWorkload determines the GPU utilization classification from daily digests.
// Returns empty classification and false HasProfilingData if all PROF_ metrics are zero/absent.
func ClassifyGPUWorkload(digests []GPUDigestRow) (GPUClassification, bool) {
	hasProf := false
	for _, d := range digests {
		if d.TensorPipeActiveAvg > 0 || d.DRAMActiveAvg > 0 || d.SMActiveAvg > 0 {
			hasProf = true
			break
		}
	}
	if !hasProf {
		return "", false
	}

	var sumTensor, sumDRAM, sumSM float64
	for _, d := range digests {
		sumTensor += float64(d.TensorPipeActiveAvg)
		sumDRAM += float64(d.DRAMActiveAvg)
		sumSM += float64(d.SMActiveAvg)
	}
	n := float64(len(digests))
	avgTensor := sumTensor / n
	avgDRAM := sumDRAM / n
	avgSM := sumSM / n

	switch {
	case avgSM < gpuIdleThreshold:
		return GPUClassIdle, true
	case avgDRAM > gpuMemBoundDRAM && avgTensor < gpuMemBoundTensor:
		return GPUClassMemoryBound, true
	case avgTensor < gpuUnderutilizedTensor && avgSM < gpuUnderutilizedSM:
		return GPUClassUnderutilized, true
	case avgTensor < gpuUnderutilizedSM && avgDRAM < 0.30:
		return GPUClassComputeBoundUnderutil, true
	default:
		return GPUClassWellUtilized, true
	}
}

// SelectMIGProfile recommends the smallest MIG profile that fits the workload's
// frame buffer and compute requirements. Returns "" if no MIG profile fits or
// GPU is not MIG-capable.
func SelectMIGProfile(spec *GPUModelSpec, digests []GPUDigestRow) string {
	if spec == nil || !spec.MIGSupported || len(spec.Profiles) == 0 || len(digests) == 0 {
		return ""
	}

	fbMax := percentile98FB(digests)
	requiredFB := fbMax * gpuFBHeadroomFactor

	for _, p := range spec.Profiles {
		if float64(p.FBSizeMiB) >= requiredFB {
			return p.Name
		}
	}
	return "full_gpu"
}

func percentile98FB(digests []GPUDigestRow) float64 {
	vals := make([]float64, 0, len(digests))
	for _, d := range digests {
		vals = append(vals, float64(d.FBUsageMaxMiB))
	}
	sort.Float64s(vals)
	if len(vals) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(vals))*0.98)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	return vals[idx]
}

// GPUConfidence computes a 0.0-1.0 confidence score for a GPU recommendation.
func GPUConfidence(digests []GPUDigestRow) float32 {
	days := len(digests)
	var base float32
	switch {
	case days < 3:
		base = 0.3
	case days < 7:
		base = 0.6
	case days < 14:
		base = 0.8
	default:
		base = 1.0
	}

	var maxSM, sumSMAvg float64
	for _, d := range digests {
		if float64(d.SMActiveMax) > maxSM {
			maxSM = float64(d.SMActiveMax)
		}
		sumSMAvg += float64(d.SMActiveAvg)
	}
	avgSM := sumSMAvg / float64(days)
	if days > 0 && avgSM > 0 && maxSM/avgSM > 5.0 {
		base *= 0.7
	}

	return base
}

// RecommendGPU produces a GPU recommendation for a container given its daily GPU digests.
// Returns nil if no GPU data is present.
func RecommendGPU(digests []GPUDigestRow) *GPURec {
	if len(digests) == 0 {
		return nil
	}

	modelName := digests[0].GPUModelName
	profileName := digests[0].GPUProfileName

	spec := MatchGPUModel(modelName)

	classification, hasProf := ClassifyGPUWorkload(digests)

	rec := &GPURec{
		GPUModelName:      modelName,
		CurrentGPUProfile: profileName,
		HasProfilingData:  hasProf,
	}

	var sumTensor, sumDRAM, sumSM, maxFB float64
	for _, d := range digests {
		sumTensor += float64(d.TensorPipeActiveAvg)
		sumDRAM += float64(d.DRAMActiveAvg)
		sumSM += float64(d.SMActiveAvg)
		if float64(d.FBUsageMaxMiB) > maxFB {
			maxFB = float64(d.FBUsageMaxMiB)
		}
	}
	n := float64(len(digests))
	rec.TensorPipeActiveAvg = float32(sumTensor / n)
	rec.DRAMActiveAvg = float32(sumDRAM / n)
	rec.SMActiveAvg = float32(sumSM / n)
	rec.FBUsageMaxMiB = float32(maxFB)

	if !hasProf {
		rec.NotificationCodes = append(rec.NotificationCodes, NotifGPUNoProfilingData)
		if spec != nil && spec.MIGSupported {
			rec.RecommendedGPUProfile = SelectMIGProfile(spec, digests)
		}
		rec.Confidence = GPUConfidence(digests) * 0.5
		return rec
	}

	rec.Classification = classification
	rec.Confidence = GPUConfidence(digests)

	switch classification {
	case GPUClassIdle:
		rec.NotificationCodes = append(rec.NotificationCodes, NotifGPUIdle)
	case GPUClassUnderutilized, GPUClassComputeBoundUnderutil:
		rec.NotificationCodes = append(rec.NotificationCodes, NotifGPUUnderutilized)
	case GPUClassMemoryBound:
		rec.MemoryBoundDetected = true
		rec.NotificationCodes = append(rec.NotificationCodes, NotifGPUMemBound)
	}

	if spec != nil && spec.MIGSupported {
		switch classification {
		case GPUClassIdle, GPUClassUnderutilized, GPUClassComputeBoundUnderutil, GPUClassMemoryBound:
			rec.RecommendedGPUProfile = SelectMIGProfile(spec, digests)
		}
	}

	return rec
}

// ApplyGPUSavings computes the GPU savings estimate using the gpu_cost_per_month
// rate from the cost model. Modifies rec in-place.
//
// Savings logic:
//   - idle: full GPU rate (could remove the GPU entirely)
//   - MIG right-sized: (1 - recommended_slices/total_slices) * rate
//   - well_utilized / no recommendation: $0
//   - no cost data: nil (no estimate available)
func ApplyGPUSavings(rec *GPURec, costData *costdata.ClusterCostData) {
	if rec == nil {
		return
	}
	if costData == nil {
		return
	}

	gpuRate := gpuMonthlyRate(costData)
	if gpuRate == 0 {
		return
	}

	var savings float64

	switch rec.Classification {
	case GPUClassIdle:
		savings = gpuRate
	case GPUClassUnderutilized, GPUClassComputeBoundUnderutil, GPUClassMemoryBound:
		if rec.RecommendedGPUProfile != "" && rec.RecommendedGPUProfile != "full_gpu" {
			spec := MatchGPUModel(rec.GPUModelName)
			if spec != nil {
				totalSlices := migTotalSlices(spec)
				recSlices := migProfileSlices(spec, rec.RecommendedGPUProfile)
				if totalSlices > 0 && recSlices > 0 {
					savings = (1.0 - float64(recSlices)/float64(totalSlices)) * gpuRate
				}
			}
		}
	}

	if savings > 0 {
		s := float32(math.Round(savings*100) / 100)
		rec.EstimatedGPUSavingsUSD = &s
	}
}

func gpuMonthlyRate(costData *costdata.ClusterCostData) float64 {
	if costData == nil || costData.ConfiguredRates == nil {
		return 0
	}
	rp, ok := costData.ConfiguredRates["gpu_cost_per_month"]
	if !ok {
		return 0
	}
	return rp.Infrastructure + rp.Supplementary
}

func migTotalSlices(spec *GPUModelSpec) int {
	if spec == nil || len(spec.Profiles) == 0 {
		return 0
	}
	last := spec.Profiles[len(spec.Profiles)-1]
	return last.Slices
}

func migProfileSlices(spec *GPUModelSpec, profileName string) int {
	for _, p := range spec.Profiles {
		if p.Name == profileName {
			return p.Slices
		}
	}
	return 0
}
