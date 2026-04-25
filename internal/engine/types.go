package engine

import "time"

// DigestRow represents a single row from daily_container_digests,
// read from the database into Go for recommendation computation.
// All numeric fields are int64 matching the BIGINT schema.
type DigestRow struct {
	BucketDate       time.Time
	CPURequestP50MC  int64
	CPURequestP60MC  int64
	CPURequestP95MC  int64
	CPURequestP98MC  int64
	CPURequestP99MC  int64
	CPUUsageP50MC    int64
	CPUUsageP60MC    int64
	CPUUsageP95MC    int64
	CPUUsageP98MC    int64
	CPUUsageP99MC    int64
	CPUUsageMaxMC    int64
	CPUThrottleP95MC int64
	CPUThrottleMaxMC int64
	MemRequestP50KiB int64
	MemRequestP60KiB int64
	MemRequestP95KiB int64
	MemRequestP98KiB int64
	MemRequestP99KiB int64
	MemUsageP50KiB   int64
	MemUsageP60KiB   int64
	MemUsageP95KiB   int64
	MemUsageP98KiB   int64
	MemUsageP99KiB   int64
	MemUsageMaxKiB   int64
	MemRSSP95KiB     int64
	MemRSSMaxKiB     int64
	OOMCountSum      int64
	CPUUsageMeanMC   int64
	MemUsageMeanKiB  int64
	SampleCount      int64
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
	EstimatedSavingsUSD    float32
	NotificationCodes      []int16
	TrendSlope             float64
	IsIdle                 bool
	OOMCountSum            int64
	DataDays               int
	Stale                  bool
}

// TermConfig defines a recommendation term's parameters.
type TermConfig struct {
	Name               string
	WindowDays         int
	MinDataDays        int
	DecayHalfLifeHours float64
}

// CPUConfig holds parameters for CPU recommendation computation.
type CPUConfig struct {
	CostPercentile     float64
	PerfPercentile     float64
	MinMargin          float64
	MaxMargin          float64
	LimitMultiplier    float64
	FloorMC            int64
	DecayHalfLifeHours float64
	Now                time.Time
}

// MemoryConfig holds parameters for memory recommendation computation.
type MemoryConfig struct {
	CostPercentile     float64
	PerfPercentile     float64
	MinMargin          float64
	MaxMargin          float64
	LimitMultiplier    float64
	DecayHalfLifeHours float64
	Now                time.Time
	OOMCountSum        int64
	OOMBaseBump        float64
	OOMMaxBump         float64
}

// DefaultCPUConfig returns the default CPU recommendation parameters.
func DefaultCPUConfig(now time.Time, decayHalfLifeHours float64) CPUConfig {
	return CPUConfig{
		CostPercentile:     0.60,
		PerfPercentile:     0.98,
		MinMargin:          1.15,
		MaxMargin:          1.50,
		LimitMultiplier:    1.05,
		FloorMC:            25,
		DecayHalfLifeHours: decayHalfLifeHours,
		Now:                now,
	}
}

// DefaultMemoryConfig returns the default memory recommendation parameters.
func DefaultMemoryConfig(now time.Time, decayHalfLifeHours float64) MemoryConfig {
	return MemoryConfig{
		CostPercentile:     0.95,
		PerfPercentile:     1.0,
		MinMargin:          1.15,
		MaxMargin:          1.50,
		LimitMultiplier:    1.05,
		DecayHalfLifeHours: decayHalfLifeHours,
		Now:                now,
		OOMBaseBump:        0.15,
		OOMMaxBump:         1.60,
	}
}
