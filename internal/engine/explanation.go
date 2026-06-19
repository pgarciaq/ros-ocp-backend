package engine

// ContainerExplanationFactors captures intermediate CPU/memory values computed during
// container and namespace recommendation. Persisted as expl_* columns on recommendation_sets
// and namespace_recommendation_sets.
type ContainerExplanationFactors struct {
	DataDays            int
	DecayHalfLifeHours  float64
	CPUCostPctMC        int64
	CPUPerfPctMC        int64
	CPUUsageP95MC       int64
	CPUUsageP50MC       int64
	CPUUsageMeanMC      int64
	CPUAdaptiveMarginBP int32
	CPUTrendSlope       float64
	MemCostPctKiB       int64
	MemPerfPctKiB       int64
	MemUsageP95KiB      int64
	MemUsageP50KiB      int64
	MemUsageMeanKiB     int64
	MemAdaptiveMarginBP int32
	MemTrendSlope       float64
	OOMCountSum         int64
	OOMBumpApplied      bool
	CPUFloorApplied     bool
	MemFloorApplied     bool
	IsIdle              bool
}

// GPUExplanationFactors captures GPU classification driving factors for expl_* columns.
type GPUExplanationFactors struct {
	SMActiveAvgBP       int32
	TensorActiveAvgBP   int32
	DRAMActiveAvgBP     int32
	FBUsageMaxMiB       int32
	FBP98MiB            int32
	RecommendedProfile  string
	CurrentProfile      string
	HasProfilingData    bool
	MemoryBound         bool
}

// NodeExplanationFactors captures node sizing driving factors for expl_* columns.
type NodeExplanationFactors struct {
	DataDays                 int
	TargetUtilizationBP      int32
	CurrentCPUMC             int64
	CurrentMemKiB            int64
	MaxCPUUsageP95MC         int64
	MaxMemUsageP95KiB        int64
	PodSchedulingHeadroomBP  int32
	EMAImbalanceBP           int32
	ConsolidationApplied     bool
	SizingFormula            string
}

// PVCExplanationFactors captures PVC classification driving factors for expl_* columns.
type PVCExplanationFactors struct {
	DataDays                  int
	OversizedThresholdBP      int32
	NearFullThresholdBP       int32
	RecommendedSizeMultiplier int32
	MinRecommendedGiB         int32
	ClassificationReason      string
}

// QuotaExplanationFactors captures namespace quota driving factors for expl_* columns.
type QuotaExplanationFactors struct {
	HeadroomBP            int32
	ContainerCPUSumMC     int64
	ContainerMemSumBytes  int64
	SignalCCPUUsedMC      int64
	MaxUtilizationBP      int32
	RiskLevel             string
	RecommendationReason  string
}

// ClusterQuotaExplanationFactors captures cluster quota driving factors for expl_* columns.
type ClusterQuotaExplanationFactors struct {
	HeadroomBP           int32
	NSQuotaCPUSumMC      int64
	NSQuotaMemSumBytes   int64
	BaseCPUMC            int64
	MaxUtilizationBP     int32
	RecommendationReason string
}

// VMExplanationFactors captures VM sizing driving factors for expl_* columns.
type VMExplanationFactors struct {
	DataDays               int
	MaxCPUUsageMC          int64
	MaxMemUsageKiB         int64
	CPUMarginBP            int32
	MemMarginBP            int32
	RawRecommendedVCPU     int32
	RawRecommendedMemGiB   int32
	DownsizeHysteresisHeld bool
	GuestAgentUsed         bool
	IdleDetected           bool
	AbandonedDetected      bool
	PowerOffCandidate      bool
	SizingBranch           string
	GPUAction              string
	GPURationale           string
}

// SnapshotExplanationFactors captures snapshot classification driving factors for expl_* columns.
type SnapshotExplanationFactors struct {
	ThresholdUsed       int
	ThresholdName       string
	ClassificationRule  string
}

// NodeGPUTimeslicingExplanationFactors captures node GPU time-slicing driving factors.
type NodeGPUTimeslicingExplanationFactors struct {
	DataDays           int
	CandidateCount     int
	ImpactedCount      int
	ClassificationRule string
}
