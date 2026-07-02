package engine

import (
	"context"
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
// Utilization metrics are stored as basis points (0-10000 = 0%-100%).
// Frame buffer metrics are stored as MiB integers.
type GPUDigestRow struct {
	IntervalStart       time.Time
	NodeName            string
	GPUModelName        string
	GPUProfileName      string
	FBUsageMinMiB       int32
	FBUsageMaxMiB       int32
	FBUsageAvgMiB       int32
	TensorPipeActiveMin int32
	TensorPipeActiveMax int32
	TensorPipeActiveAvg int32
	DRAMActiveMin       int32
	DRAMActiveMax       int32
	DRAMActiveAvg       int32
	SMActiveMin         int32
	SMActiveMax         int32
	SMActiveAvg         int32
	GPUCount            int
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
	FBP98MiB                       int32
	EstimatedGPUSavingsCents       *int64 // nil if no cost data (idle/MIG savings)
	EstimatedTimeslicingSavingsUSD *float32 // nil if no cost data (per-candidate share of node time-slicing savings)
	NotificationCodes              []int16
	HasProfilingData               bool
	TimeSlicingNode                string // set by ComputeNodeTimeslicingRec for candidates
	TimeSlicingReplicas            int    // set by ComputeNodeTimeslicingRec for candidates
	Term                           string // short, medium, long
	GPUIdleState                   IdleState
	GPUIdleSince                   *time.Time
	GPUIdleDurationDays            int
	GPUEstimatedWasteCents         int64
	GPUCount                       int // number of distinct GPUs used by this container
}

// GPUThresholds holds the configurable thresholds for GPU workload classification
// and MIG profile selection. Construct via DefaultGPUThresholds or GPUThresholdsFromConfig.
// Float fields are serialized to JSON; basis-point fields are precomputed at construction.
// Methods on GPUThresholds are safe to call concurrently from parallel tests without
// global state mutation.
type GPUThresholds struct {
	IdleThreshold       float64 `json:"idle_threshold"`
	UnderutilizedSM     float64 `json:"underutilized_sm_threshold"`
	UnderutilizedTensor float64 `json:"underutilized_tensor_threshold"`
	MemBoundDRAM        float64 `json:"membound_dram_threshold"`
	MemBoundTensor      float64 `json:"membound_tensor_threshold"`
	FBHeadroomFactor    float64 `json:"fb_headroom_factor"`

	IdleThresholdBP       int32 `json:"-"`
	UnderutilizedSMBP       int32 `json:"-"`
	UnderutilizedTensorBP   int32 `json:"-"`
	MemBoundDRAMBP          int32 `json:"-"`
	MemBoundTensorBP        int32 `json:"-"`
}

// DefaultGPUThresholds returns the built-in defaults (matching viper defaults in config).
func DefaultGPUThresholds() GPUThresholds {
	th := GPUThresholds{
		IdleThreshold:       0.02,
		UnderutilizedSM:     0.25,
		UnderutilizedTensor: 0.15,
		MemBoundDRAM:        0.60,
		MemBoundTensor:      0.15,
		FBHeadroomFactor:    1.20,
	}
	normalizeGPUThresholds(&th)
	return th
}

// GPUThresholdsFromConfig constructs GPUThresholds from the application Config.
func GPUThresholdsFromConfig(cfg *config.Config) GPUThresholds {
	if cfg == nil {
		return DefaultGPUThresholds()
	}
	th := GPUThresholds{
		IdleThreshold:       cfg.GPUIdleThreshold,
		UnderutilizedSM:     cfg.GPUUnderutilizedSMThreshold,
		UnderutilizedTensor: cfg.GPUUnderutilizedTensorThreshold,
		MemBoundDRAM:        cfg.GPUMemBoundDRAMThreshold,
		MemBoundTensor:      cfg.GPUMemBoundTensorThreshold,
		FBHeadroomFactor:    cfg.GPUFBHeadroomFactor,
	}
	normalizeGPUThresholds(&th)
	return th
}

// normalizeGPUThresholds precomputes basis-point classification thresholds from float settings.
func normalizeGPUThresholds(th *GPUThresholds) {
	if th == nil {
		return
	}
	th.IdleThresholdBP = ThresholdToBasisPoints(th.IdleThreshold)
	th.UnderutilizedSMBP = ThresholdToBasisPoints(th.UnderutilizedSM)
	th.UnderutilizedTensorBP = ThresholdToBasisPoints(th.UnderutilizedTensor)
	th.MemBoundDRAMBP = ThresholdToBasisPoints(th.MemBoundDRAM)
	th.MemBoundTensorBP = ThresholdToBasisPoints(th.MemBoundTensor)
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
	InitVMRecDefaults(cfg)
	defaultThresholds = defaultGPUThresholdSettings.GPUThresholds
}

func avgGPUBasisPoints(digests []GPUDigestRow, pick func(GPUDigestRow) int32) int32 {
	if len(digests) == 0 {
		return 0
	}
	var sum int64
	for _, d := range digests {
		sum += int64(pick(d))
	}
	return int32(sum / int64(len(digests)))
}

func gpuHasProfilingData(digests []GPUDigestRow) bool {
	for _, d := range digests {
		if d.TensorPipeActiveAvg > 0 || d.DRAMActiveAvg > 0 || d.SMActiveAvg > 0 {
			return true
		}
	}
	return false
}

func classifyFromAverages(avgTensor, avgDRAM, avgSM int32, th GPUThresholds, computeBoundDRAMBP int32) GPUClassification {
	switch {
	case avgSM < th.IdleThresholdBP:
		return GPUClassIdle
	case avgDRAM > th.MemBoundDRAMBP && avgTensor < th.MemBoundTensorBP:
		return GPUClassMemoryBound
	case avgTensor < th.UnderutilizedTensorBP && avgSM < th.UnderutilizedSMBP:
		return GPUClassUnderutilized
	case avgTensor < th.UnderutilizedSMBP && avgDRAM < computeBoundDRAMBP:
		return GPUClassComputeBoundUnderutil
	default:
		return GPUClassWellUtilized
	}
}

// Classify determines the GPU utilization classification from daily digests.
// Returns empty classification and false HasProfilingData if all PROF_ metrics are zero/absent.
func (th *GPUThresholds) Classify(digests []GPUDigestRow) (GPUClassification, bool) {
	if !gpuHasProfilingData(digests) {
		return "", false
	}
	avgTensor := avgGPUBasisPoints(digests, func(d GPUDigestRow) int32 { return d.TensorPipeActiveAvg })
	avgDRAM := avgGPUBasisPoints(digests, func(d GPUDigestRow) int32 { return d.DRAMActiveAvg })
	avgSM := avgGPUBasisPoints(digests, func(d GPUDigestRow) int32 { return d.SMActiveAvg })
	return classifyFromAverages(avgTensor, avgDRAM, avgSM, *th, ThresholdToBasisPoints(0.30)), true
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
	if !gpuHasProfilingData(digests) {
		return "", false
	}
	avgTensor := avgGPUBasisPoints(digests, func(d GPUDigestRow) int32 { return d.TensorPipeActiveAvg })
	avgDRAM := avgGPUBasisPoints(digests, func(d GPUDigestRow) int32 { return d.DRAMActiveAvg })
	avgSM := avgGPUBasisPoints(digests, func(d GPUDigestRow) int32 { return d.SMActiveAvg })
	return classifyFromAverages(avgTensor, avgDRAM, avgSM, s.GPUThresholds, s.ComputeBoundDRAMThresholdBP), true
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
	vals := make([]int32, 0, len(digests))
	for _, d := range digests {
		vals = append(vals, d.FBUsageMaxMiB)
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
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
	return float64(vals[idx])
}

func percentile98FB(digests []GPUDigestRow) float64 {
	return percentileFB(digests, defaultGPUThresholdSettings.MIGFBPercentile)
}

// maxGPUCount returns the maximum GPUCount observed across digest rows.
// Falls back to 1 if all rows have GPUCount == 0 (backward compat with old data).
func maxGPUCount(digests []GPUDigestRow) int {
	var max int
	for _, d := range digests {
		if d.GPUCount > max {
			max = d.GPUCount
		}
	}
	if max < 1 {
		return 1
	}
	return max
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

	var maxSM int32
	var sumSMAvg int64
	for _, d := range digests {
		if d.SMActiveMax > maxSM {
			maxSM = d.SMActiveMax
		}
		sumSMAvg += int64(d.SMActiveAvg)
	}
	avgSM := BasisPointsToFloat(int32(sumSMAvg / int64(days)))
	maxSMFloat := BasisPointsToFloat(maxSM)
	if days > 0 && avgSM > 0 && maxSMFloat/avgSM > settings.SpikeRatioThreshold {
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
// Optional idleCfg overrides GPU idle/zombie thresholds; when omitted, env defaults apply.
func RecommendGPUWithSettings(digests []GPUDigestRow, settings GPUThresholdSettings, idleCfg ...GPUIdleConfig) *GPURec {
	if len(digests) == 0 {
		return nil
	}

	modelName := digests[0].GPUModelName
	profileName := digests[0].GPUProfileName

	gpuCount := maxGPUCount(digests)

	spec := MatchGPUModel(modelName)

	classification, hasProf := settings.ClassifyWithSettings(digests)

	currentProfile := profileName
	if currentProfile == "" {
		currentProfile = "full_gpu"
	}

	rec := &GPURec{
		GPUModelName:      modelName,
		CurrentGPUProfile: currentProfile,
		HasProfilingData:  hasProf,
		GPUCount:          gpuCount,
	}

	var sumTensor, sumDRAM, sumSM int64
	var maxFB int32
	for _, d := range digests {
		sumTensor += int64(d.TensorPipeActiveAvg)
		sumDRAM += int64(d.DRAMActiveAvg)
		sumSM += int64(d.SMActiveAvg)
		if d.FBUsageMaxMiB > maxFB {
			maxFB = d.FBUsageMaxMiB
		}
	}
	n := int64(len(digests))
	rec.TensorPipeActiveAvg = BasisPointsToFloat32(int32(sumTensor / n))
	rec.DRAMActiveAvg = BasisPointsToFloat32(int32(sumDRAM / n))
	rec.SMActiveAvg = BasisPointsToFloat32(int32(sumSM / n))
	rec.FBUsageMaxMiB = float32(maxFB)
	rec.FBP98MiB = int32(percentileFB(digests, settings.MIGFBPercentile))

	isMultiGPU := gpuCount > 1

	if !hasProf {
		rec.Classification = GPUClassNoProfiling
		rec.NotificationCodes = append(rec.NotificationCodes, NotifGPUNoProfilingData)
		if isMultiGPU {
			rec.NotificationCodes = append(rec.NotificationCodes, NotifGPUMultiDevice)
		} else if spec != nil && spec.MIGSupported {
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

	if isMultiGPU {
		rec.NotificationCodes = append(rec.NotificationCodes, NotifGPUMultiDevice)
	} else if spec != nil && spec.MIGSupported {
		switch classification {
		case GPUClassIdle, GPUClassUnderutilized, GPUClassComputeBoundUnderutil, GPUClassMemoryBound:
			rec.RecommendedGPUProfile = settings.SelectMIGProfileWithSettings(spec, digests)
		}
	}

	var gpuIdleCfg GPUIdleConfig
	if len(idleCfg) > 0 {
		gpuIdleCfg = idleCfg[0]
	} else {
		gpuIdleCfg = LoadGPUIdleConfig(context.Background(), nil, "")
	}
	gpuIdle := ClassifyGPUIdleFromDigests(digests, gpuIdleCfg)
	rec.GPUIdleState = gpuIdle.State
	rec.GPUIdleSince = gpuIdle.IdleSince
	rec.GPUIdleDurationDays = gpuIdle.DurationDays

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

	gpuRateMicroCents := RateMicroCentsPerDollarMonth(GPUMonthlyRate(costData))
	if gpuRateMicroCents == 0 {
		return
	}

	var savingsMicroCents int64

	switch rec.Classification {
	case GPUClassIdle:
		savingsMicroCents = gpuRateMicroCents
	case GPUClassUnderutilized, GPUClassComputeBoundUnderutil, GPUClassMemoryBound:
		if rec.RecommendedGPUProfile != "" && rec.RecommendedGPUProfile != "full_gpu" {
			spec := MatchGPUModel(rec.GPUModelName)
			if spec != nil {
				totalSlices := int64(migTotalSlices(spec))
				recSlices := int64(migProfileSlices(spec, rec.RecommendedGPUProfile))
				savingsMicroCents = MIGFractionSavingsMicroCents(gpuRateMicroCents, totalSlices, recSlices)
			}
		}
	}

	cents := MicroCentsToCents(savingsMicroCents)
	rec.EstimatedGPUSavingsCents = &cents
}

// ComputeGPUSavingsCents returns monthly GPU savings in cents, or nil when cost data is unavailable.
func ComputeGPUSavingsCents(rec *GPURec, costData *costdata.ClusterCostData) *int64 {
	if rec == nil {
		return nil
	}
	clone := *rec
	clone.EstimatedGPUSavingsCents = nil
	ApplyGPUSavings(&clone, costData)
	return clone.EstimatedGPUSavingsCents
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
