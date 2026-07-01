package engine

import "time"

// DigestRow represents a single row from daily_container_digests,
// read from the database into Go for recommendation computation.
// All numeric fields are int64 matching the BIGINT schema.
type DigestRow struct {
	BucketDate        time.Time
	CPURequestP50MC   int64
	CPURequestP60MC   int64
	CPURequestP95MC   int64
	CPURequestP98MC   int64
	CPURequestP99MC   int64
	CPUUsageP50MC     int64
	CPUUsageP60MC     int64
	CPUUsageP95MC     int64
	CPUUsageP98MC     int64
	CPUUsageP99MC     int64
	CPUUsageMaxMC     int64
	CPUThrottleP95MC  int64
	CPUThrottleMaxMC  int64
	MemRequestP50KiB  int64
	MemRequestP60KiB  int64
	MemRequestP95KiB  int64
	MemRequestP98KiB  int64
	MemRequestP99KiB  int64
	MemUsageP50KiB    int64
	MemUsageP60KiB    int64
	MemUsageP95KiB    int64
	MemUsageP98KiB    int64
	MemUsageP99KiB    int64
	MemUsageMaxKiB    int64
	MemRSSP95KiB      int64
	MemRSSMaxKiB      int64
	OOMCountSum       int64
	CPUUsageMeanMC    int64
	MemUsageMeanKiB   int64
	SampleCount       int64
	PodCountMin       int64
	PodCountMax       int64
	PodCountAvg       int64
	DesiredReplicas   int64
	AvailableReplicas int64
	CPUUsageCVBP      *int64 // coefficient of variation in basis points, nil if unavailable
}

// CPURec holds both cost and performance CPU recommendations for a container.
type CPURec struct {
	CostRequestMC int64
	CostLimitMC   int64
	PerfRequestMC int64
	PerfLimitMC   int64
	TrendSlope    float64
	IsIdle        bool
}

// MemoryRec holds both cost and performance memory recommendations for a container.
type MemoryRec struct {
	CostRequestKiB int64
	CostLimitKiB   int64
	PerfRequestKiB int64
	PerfLimitKiB   int64
	TrendSlope     float64
}

// ContainerRec combines CPU and memory recommendations for a single container
// within a single term and engine.
type ContainerRec struct {
	OrgID         string
	ClusterUUID   string
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
	Term          string
	Engine        string

	RecCPURequestMC  int64
	RecCPULimitMC    int64
	RecMemRequestKiB int64
	RecMemLimitKiB   int64

	CurrentCPURequestMC  int64
	CurrentCPULimitMC    int64
	CurrentMemRequestKiB int64
	CurrentMemLimitKiB   int64

	VariationCPURequestPct int32
	VariationCPULimitPct   int32
	VariationMemRequestPct int32
	VariationMemLimitPct   int32
	ConfidenceLevel        float32
	EstimatedSavingsCents  *int64
	EstimatedCPUSavingsCents *int64
	EstimatedMemSavingsCents *int64
	NotificationCodes      []int16
	CPUTrendSlope          float64
	MemTrendSlope          float64
	IsIdle                 bool
	IsAbandoned            bool
	IdleState              IdleState
	IdleSince              *time.Time
	IdleDurationDays       int
	PeakCPUMC              int64
	PeakMemoryBytes        int64
	EstimatedWasteCents    *int64
	OOMCountSum            int64
	DataDays               int
	Stale                  bool
	PodCountMin            int64
	PodCountMax            int64
	PodCountAvg            int64
	DesiredReplicas        int64
	AvailableReplicas      int64
	RecommendedReplicas    int64
	ReplicaConfidence      string
	ReplicaExplanation     string

	MonitoringStartTime time.Time
	MonitoringEndTime   time.Time

	Category       string
	CategoryCPU    string
	CategoryMemory string

	Expl ContainerExplanationFactors
}

// TermConfig defines a recommendation term's parameters.
type TermConfig struct {
	Name                       string
	WindowDays                 int
	MinDataDays                int
	DecayHalfLifeHours         float64
	ReplicaTargetUtilizationPct int
}

// CPUConfig holds parameters for CPU recommendation computation.
type CPUConfig struct {
	CostPercentile      float64
	PerfPercentile      float64
	MinMargin           float64
	MaxMargin           float64
	LimitMultiplier     float64
	FloorMC             int64
	IdleThresholdMC     int64
	IdleThresholdMemKiB int64
	DecayHalfLifeHours  float64
	Now                 time.Time
}

// MemoryConfig holds parameters for memory recommendation computation.
type MemoryConfig struct {
	CostPercentile     float64
	PerfPercentile     float64
	MinMargin          float64
	MaxMargin          float64
	LimitMultiplier    float64
	FloorKiB           int64
	DecayHalfLifeHours float64
	Now                time.Time
	OOMCountSum        int64
	OOMBaseBump        float64
	OOMMaxBump         float64
}

// DefaultCPUConfig returns the default CPU recommendation parameters.
func DefaultCPUConfig(now time.Time, decayHalfLifeHours float64) CPUConfig {
	return CPUConfigFromSizing(defaultContainerSizingThresholds, now, decayHalfLifeHours, "")
}

// CPUConfigFromSizing builds CPUConfig from resolved sizing thresholds.
func CPUConfigFromSizing(th SizingThresholdSettings, now time.Time, decayHalfLifeHours float64, profile string) CPUConfig {
	cfg := CPUConfig{
		CostPercentile:      th.CPUCostPercentile,
		PerfPercentile:      th.CPUPerfPercentile,
		MinMargin:           th.MinMargin,
		MaxMargin:           th.MaxMargin,
		LimitMultiplier:     th.LimitMultiplier,
		FloorMC:             th.CPUFloorMC,
		IdleThresholdMC:     th.IdleCPUThresholdMC,
		IdleThresholdMemKiB: th.IdleMemThresholdKiB,
		DecayHalfLifeHours:  decayHalfLifeHours,
		Now:                 now,
	}
	if profile == "performance" {
		cfg.CostPercentile = th.CPUPerfPercentile
		cfg.PerfPercentile = th.CPUPerfPercentile
	}
	return cfg
}

// DefaultMemoryConfig returns the default memory recommendation parameters.
func DefaultMemoryConfig(now time.Time, decayHalfLifeHours float64) MemoryConfig {
	return MemoryConfigFromSizing(defaultContainerSizingThresholds, now, decayHalfLifeHours, OOMConfig{}, "")
}

// MemoryConfigFromSizing builds MemoryConfig from resolved sizing thresholds.
func MemoryConfigFromSizing(th SizingThresholdSettings, now time.Time, decayHalfLifeHours float64, oom OOMConfig, profile string) MemoryConfig {
	cfg := MemoryConfig{
		CostPercentile:     th.MemCostPercentile,
		PerfPercentile:     th.MemPerfPercentile,
		MinMargin:          th.MinMargin,
		MaxMargin:          th.MaxMargin,
		LimitMultiplier:    th.LimitMultiplier,
		FloorKiB:           th.MemFloorKiB,
		DecayHalfLifeHours: decayHalfLifeHours,
		Now:                now,
		OOMBaseBump:        0.15,
		OOMMaxBump:         1.60,
	}
	if profile == "performance" {
		cfg.CostPercentile = th.MemPerfPercentile
		cfg.PerfPercentile = th.MemPerfPercentile
	}
	if oom.BaseBump > 0 {
		cfg.OOMBaseBump = oom.BaseBump
	}
	if oom.MaxBump > 0 {
		cfg.OOMMaxBump = oom.MaxBump
	}
	return cfg
}
