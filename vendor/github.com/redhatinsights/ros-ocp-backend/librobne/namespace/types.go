package namespace

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// ScheduleAllHours is the digest_schedule_type for the all-hours stream.
const ScheduleAllHours = "all_hours"

// ScheduleBusinessHours is the digest_schedule_type for the business-hours stream.
const ScheduleBusinessHours = "business_hours"

// MemTrendSlopeThreshold is higher than the container threshold (100 KiB/day)
// because namespace-level memory aggregates multiple pods.
const MemTrendSlopeThreshold = 500.0

// NamespaceKey identifies a namespace in a grouped digest map.
type NamespaceKey struct {
	Namespace string
}

// NamespaceEngineConfig is the deposited config for pool-free namespace compute.
// Product wrappers load terms, thresholds, and last-reported from PostgreSQL,
// then call RecommendNamespaces. ScheduleType is copied onto every rec
// (all_hours and business_hours are two product entry points, one runner).
type NamespaceEngineConfig struct {
	OrgID               string
	ClusterUUID         string
	End                 time.Time
	ScheduleType        string
	Terms               []types.TermConfig
	Sizing              types.SizingThresholdSettings
	Now                 time.Time
	StalenessThreshold  time.Duration
	ClusterLastReported time.Time
	NamespaceAllow      func(string) bool
}

// NamespaceRec combines CPU and memory recommendations for a single namespace
// within a single term and engine.
type NamespaceRec struct {
	OrgID        string
	ClusterUUID  string
	Namespace    string
	Term         string
	Engine       string
	ScheduleType string

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
	NotificationCodes      []int16
	MemTrendSlope          float64
	DataDays               int
	Stale                  bool

	MonitoringStartTime time.Time
	MonitoringEndTime   time.Time

	EstimatedSavingsCents    *int64
	EstimatedCPUSavingsCents *int64
	EstimatedMemSavingsCents *int64

	Category       string
	CategoryCPU    string
	CategoryMemory string

	Expl types.ContainerExplanationFactors
}
