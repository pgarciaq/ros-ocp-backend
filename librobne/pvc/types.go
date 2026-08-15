package pvc

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const (
	PVCRecTypeOversized = "oversized"
	PVCRecTypeNearFull  = "near_full"
	PVCRecTypeOrphaned  = "orphaned"
	PVCRecTypeHealthy   = "healthy"
)

// PVCKey identifies a PVC in a grouped digest map.
type PVCKey struct {
	Namespace string
	PVC       string
}

// EngineConfig is the deposited config for pool-free PVC compute.
// Product wrappers load digests, terms, and thresholds from PostgreSQL,
// then call RecommendPVCs. ApplyPVCSavings is a separate call after this returns.
type EngineConfig struct {
	OrgID           string
	ClusterUUID     string
	Terms           []types.TermConfig
	Settings        ThresholdSettings
	NotifThresholds types.NotificationThresholds
}

// PVCDigestRow represents a row from daily_pvc_digests.
type PVCDigestRow struct {
	BucketDate    time.Time
	Namespace     string
	PVC           string
	LastSeenPod   string
	VMName        string
	PV            string
	StorageClass  string
	CapacityBytes int64
	RequestBytes  int64
	UsageBytesMin int64
	UsageBytesMax int64
	UsageBytesAvg int64
	SampleCount   int
}

// PVCRec is the output of the PVC recommendation engine.
type PVCRec struct {
	OrgID                        string
	ClusterUUID                  string
	Namespace                    string
	PVC                          string
	LastSeenPod                  string
	VMName                       string
	PV                           string
	StorageClass                 string
	CapacityBytes                int64
	RequestBytes                 int64
	UsageBytesMax                int64
	UsageRatio                   float64
	RecommendationType           string
	RecommendedBytes             *int64
	DaysToFull                   *int
	GrowthBytesPerDay            int64
	EstimatedMonthlySavingsCents int64
	NotificationCodes            []int16
	DataDays                     int
	ConfidenceLevel              float32
	Term                         string
	IdleSince                    *time.Time
	IdleDurationDays             int
	Expl                         types.PVCExplanationFactors
}

// ThresholdSettings holds PVC right-sizing classification parameters.
type ThresholdSettings struct {
	OversizedThreshold        float64 `json:"oversized_threshold"`
	NearFullThreshold         float64 `json:"near_full_threshold"`
	MinTrendDays              int     `json:"min_trend_days"`
	RecommendedSizeMultiplier int     `json:"recommended_size_multiplier"`
	MinRecommendedGiB         int     `json:"min_recommended_gib"`
	DaysToFullAlert           int     `json:"days_to_full_alert"`
}

// DefaultThresholdSettings returns compiled defaults for PVC recommendations.
func DefaultThresholdSettings() ThresholdSettings {
	return ThresholdSettings{
		OversizedThreshold:        0.20,
		NearFullThreshold:         0.85,
		MinTrendDays:              2,
		RecommendedSizeMultiplier: 2,
		MinRecommendedGiB:         1,
		DaysToFullAlert:           30,
	}
}
