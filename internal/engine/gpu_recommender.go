package engine

import (
	"math"
	"sort"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
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
	GPUClassNoProfiling           GPUClassification = "no_profiling"
)

// GPUDigestRow holds one daily GPU digest row for a single container.
type GPUDigestRow struct {
	IntervalStart       time.Time
	NodeName            string
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

// GPURec holds the GPU recommendation for a single container within a single term.
type GPURec struct {
	GPUModelName                   string
	CurrentGPUProfile              string            // current MIG profile or "" for full GPU
	Classification                 GPUClassification // empty if no profiling data (Tier 2)
	RecommendedGPUProfile          string            // recommended MIG profile, "full_gpu", or ""
	MemoryBoundDetected            bool
	Confidence                     float32
	TensorPipeActiveAvg            float32
	DRAMActiveAvg                  float32
	SMActiveAvg                    float32
	FBUsageMaxMiB                  float32
	EstimatedGPUSavingsUSD         *float32 // nil if no cost data (idle/MIG savings)
	EstimatedTimeslicingSavingsUSD *float32 // nil if no cost data (per-candidate share of node time-slicing savings)
	NotificationCodes              []int16
	HasProfilingData               bool
	TimeSlicingNode                string // set by ComputeNodeTimeslicingRec for candidates
	TimeSlicingReplicas            int    // set by ComputeNodeTimeslicingRec for candidates
	Term                           string // short, medium, long
}

// GPUThresholds holds the configurable thresholds for GPU workload classification
// and MIG profile selection. Construct via NewGPUThresholds or GPUThresholdsFromConfig.
// Methods on GPUThresholds are safe to call concurrently from parallel tests without
// global state mutation.
type GPUThresholds struct {
	IdleThreshold       float64 `json:"idle_threshold"`
	UnderutilizedSM     float64 `json:"underutilized_sm_threshold"`
	UnderutilizedTensor float64 `json:"underutilized_tensor_threshold"`
	MemBoundDRAM        float64 `json:"membound_dram_threshold"`
	MemBoundTensor      float64 `json:"membound_tensor_threshold"`
	FBHeadroomFactor    float64 `json:"fb_headroom_factor"`
}

// DefaultGPUThresholds returns the built-in defaults (matching viper defaults in config).
func DefaultGPUThresholds() GPUThresholds {
	return GPUThresholds{
		IdleThreshold:       0.02,
		UnderutilizedSM:     0.25,
		UnderutilizedTensor: 0.15,
		MemBoundDRAM:        0.60,
		MemBoundTensor:      0.15,
		FBHeadroomFactor:    1.20,
	}
}

// GPUThresholdsFromConfig constructs GPUThresholds from the application Config.
func GPUThresholdsFromConfig(cfg *config.Config) GPUThresholds {
	if cfg == nil {
		return DefaultGPUThresholds()
	}
	return GPUThresholds{
		IdleThreshold:       cfg.GPUIdleThreshold,
		UnderutilizedSM:     cfg.GPUUnderutilizedSMThreshold,
		UnderutilizedTensor: cfg.GPUUnderutilizedTensorThreshold,
		MemBoundDRAM:        cfg.GPUMemBoundDRAMThreshold,
		MemBoundTensor:      cfg.GPUMemBoundTensorThreshold,
		FBHeadroomFactor:    cfg.GPUFBHeadroomFactor,
	}
}

// defaultThresholds is the process-wide instance used by top-level convenience
// functions. Updated by InitGPUEngine at startup.
var defaultThresholds = DefaultGPUThresholds()

// InitGPUEngine copies GPU recommendation thresholds from the central config.
// Call once after config load (e.g. from cmd/start.go or StartAPIServer).
func InitGPUEngine(cfg *config.Config) {
	if cfg == nil {
		return
	}
	InitThresholdDefaults(cfg)
	defaultThresholds = defaultGPUThresholdSettings.GPUThresholds
}

// Classify determines the GPU utilization classification from daily digests.
// Returns empty classification and false HasProfilingData if all PROF_ metrics are zero/absent.
func (th *GPUThresholds) Classify(digests []GPUDigestRow) (GPUClassification, bool) {
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
	case avgSM < th.IdleThreshold:
		return GPUClassIdle, true
	case avgDRAM > th.MemBoundDRAM && avgTensor < th.MemBoundTensor:
		return GPUClassMemoryBound, true
	case avgTensor < th.UnderutilizedTensor && avgSM < th.UnderutilizedSM:
		return GPUClassUnderutilized, true
	case avgTensor < th.UnderutilizedSM && avgDRAM < 0.30:
		return GPUClassComputeBoundUnderutil, true
	default:
		return GPUClassWellUtilized, true
	}
}

// SelectMIGProfile recommends the smallest MIG profile that fits the workload's
// frame buffer and compute requirements. Returns "" if no MIG profile fits or
// GPU is not MIG-capable.
func (th *GPUThresholds) SelectMIGProfile(spec *GPUModelSpec, digests []GPUDigestRow) string {
	if spec == nil || !spec.MIGSupported || len(spec.Profiles) == 0 || len(digests) == 0 {
		return ""
	}

	fbMax := percentile98FB(digests)
	requiredFB := fbMax * th.FBHeadroomFactor

	for _, p := range spec.Profiles {
		if float64(p.FBSizeMiB) >= requiredFB {
			return p.Name
		}
	}
	return "full_gpu"
}

// ClassifyWithSettings classifies GPU workloads using extended threshold settings.
func (s GPUThresholdSettings) ClassifyWithSettings(digests []GPUDigestRow) (GPUClassification, bool) {
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
	th := s.GPUThresholds

	switch {
	case avgSM < th.IdleThreshold:
		return GPUClassIdle, true
	case avgDRAM > th.MemBoundDRAM && avgTensor < th.MemBoundTensor:
		return GPUClassMemoryBound, true
	case avgTensor < th.UnderutilizedTensor && avgSM < th.UnderutilizedSM:
		return GPUClassUnderutilized, true
	case avgTensor < th.UnderutilizedSM && avgDRAM < s.ComputeBoundDRAMThreshold:
		return GPUClassComputeBoundUnderutil, true
	default:
		return GPUClassWellUtilized, true
	}
}

// SelectMIGProfileWithSettings recommends a MIG profile using extended settings.
func (s GPUThresholdSettings) SelectMIGProfileWithSettings(spec *GPUModelSpec, digests []GPUDigestRow) string {
	if spec == nil || !spec.MIGSupported || len(spec.Profiles) == 0 || len(digests) == 0 {
		return ""
	}
	fbMax := percentileFB(digests, s.MIGFBPercentile)
	requiredFB := fbMax * s.FBHeadroomFactor
	for _, p := range spec.Profiles {
		if float64(p.FBSizeMiB) >= requiredFB {
			return p.Name
		}
	}
	return "full_gpu"
}
func ClassifyGPUWorkload(digests []GPUDigestRow) (GPUClassification, bool) {
	return defaultGPUThresholdSettings.ClassifyWithSettings(digests)
}

// SelectMIGProfile is a convenience function using the process-wide default thresholds.
func SelectMIGProfile(spec *GPUModelSpec, digests []GPUDigestRow) string {
	return defaultGPUThresholdSettings.SelectMIGProfileWithSettings(spec, digests)
}

func percentileFB(digests []GPUDigestRow, pct float64) float64 {
	vals := make([]float64, 0, len(digests))
	for _, d := range digests {
		vals = append(vals, float64(d.FBUsageMaxMiB))
	}
	sort.Float64s(vals)
	if len(vals) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(vals))*pct)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	return vals[idx]
}

func percentile98FB(digests []GPUDigestRow) float64 {
	return percentileFB(digests, defaultGPUThresholdSettings.MIGFBPercentile)
}

// GPUConfidence computes a 0.0-1.0 confidence score for a GPU recommendation.
func GPUConfidence(digests []GPUDigestRow) float32 {
	return GPUConfidenceWithSettings(digests, defaultGPUThresholdSettings)
}

// GPUConfidenceWithSettings computes confidence using explicit GPU threshold settings.
func GPUConfidenceWithSettings(digests []GPUDigestRow, settings GPUThresholdSettings) float32 {
	days := len(digests)
	var base float32
	switch {
	case days < settings.ConfidenceDaysTier1:
		base = 0.3
	case days < settings.ConfidenceDaysTier2:
		base = 0.6
	case days < settings.ConfidenceDaysTier3:
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
	if days > 0 && avgSM > 0 && maxSM/avgSM > settings.SpikeRatioThreshold {
		base *= float32(settings.SpikeConfidencePenalty)
	}

	return base
}

// RecommendGPU produces a GPU recommendation for a container given its daily GPU digests.
// Returns nil if no GPU data is present.
func RecommendGPU(digests []GPUDigestRow) *GPURec {
	return RecommendGPUWithSettings(digests, defaultGPUThresholdSettings)
}

// RecommendGPUWithSettings produces a GPU recommendation using explicit threshold settings.
func RecommendGPUWithSettings(digests []GPUDigestRow, settings GPUThresholdSettings) *GPURec {
	if len(digests) == 0 {
		return nil
	}

	modelName := digests[0].GPUModelName
	profileName := digests[0].GPUProfileName

	spec := MatchGPUModel(modelName)

	classification, hasProf := settings.ClassifyWithSettings(digests)

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
		rec.Classification = GPUClassNoProfiling
		rec.NotificationCodes = append(rec.NotificationCodes, NotifGPUNoProfilingData)
		if spec != nil && spec.MIGSupported {
			rec.RecommendedGPUProfile = settings.SelectMIGProfileWithSettings(spec, digests)
		}
		rec.Confidence = GPUConfidenceWithSettings(digests, settings) * float32(settings.NoProfilingConfidenceFactor)
		return rec
	}

	rec.Classification = classification
	rec.Confidence = GPUConfidenceWithSettings(digests, settings)

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
			rec.RecommendedGPUProfile = settings.SelectMIGProfileWithSettings(spec, digests)
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

	gpuRate := GPUMonthlyRate(costData)
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

	s := float32(math.Round(savings*100) / 100)
	rec.EstimatedGPUSavingsUSD = &s
}

// GPUMonthlyRate extracts the GPU monthly cost rate (infrastructure +
// supplementary) from Koku cost data. Returns 0 if unavailable.
func GPUMonthlyRate(costData *costdata.ClusterCostData) float64 {
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
