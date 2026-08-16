package quota

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const (
	QuotaRecTypeTighten  = "tighten"
	QuotaRecTypeRaise    = "raise"
	QuotaRecTypeOptimal  = "optimal"
	QuotaRecTypeNone     = "none"
	QuotaRiskHigh        = "high"
	QuotaRiskMedium      = "medium"
	QuotaRiskLow         = "low"
	QuotaRiskNone        = "none"
	QuotaContainerTerm   = "medium"
	QuotaContainerEngine = "cost"
)

func DefaultQuotaRecConfig() QuotaRecConfig {
	return QuotaRecConfig{
		HeadroomBasisPoints:   11000,
		HighRiskThresholdBP:   9000,
		MediumRiskThresholdBP: 7000,
	}
}

// QuotaRecConfig holds quota recommendation thresholds (basis points).
// Currency is deposited by the product; an empty value is left empty
// (librobne does not invent a default currency).
type QuotaRecConfig struct {
	HeadroomBasisPoints   int
	HighRiskThresholdBP   int
	MediumRiskThresholdBP int
	Currency              string
}

// NamespaceQuotaSnapshot is the latest hard/used quota per namespace from digests.
type NamespaceQuotaSnapshot struct {
	Namespace               string
	QuotaName               string
	CPURequestHardMC        int64
	CPULimitHardMC          int64
	MemoryRequestHardBytes  int64
	MemoryLimitHardBytes    int64
	CPURequestUsedMC        int64
	CPULimitUsedMC          int64
	MemoryRequestUsedBytes  int64
	MemoryLimitUsedBytes    int64
	StorageRequestHardBytes int64
	StorageRequestUsedBytes int64
	PodsHard                int64
	PodsUsed                int64
	ObjectCountHard         int64
	ObjectCountUsed         int64
	LastObservedAt          time.Time
}

// ContainerQuotaAggregate sums container recommendations per namespace.
type ContainerQuotaAggregate struct {
	CPURequestSumMC       int64
	CPULimitSumMC         int64
	MemoryRequestSumBytes int64
	MemoryLimitSumBytes   int64
}

// QuotaRec is the output of the quota recommendation engine.
type QuotaRec struct {
	OrgID                 string
	ClusterUUID           string
	Namespace             string
	QuotaName             string
	Snapshot              NamespaceQuotaSnapshot
	Recommended           QuotaResourceBundle
	HeadroomBP            int
	Utilization           QuotaUtilizationBP
	CapacityFreed         QuotaCapacityFreed
	EstimatedSavingsCents int64
	Currency              string
	RecommendationType    string
	RiskLevel             string
	NotificationCodes     []int16
	Expl                  types.QuotaExplanationFactors
}

// QuotaResourceBundle holds quota hard, used, or recommended values.
type QuotaResourceBundle struct {
	CPURequestMillicores int64
	CPULimitMillicores   int64
	MemoryRequestBytes   int64
	MemoryLimitBytes     int64
	StorageRequestBytes  int64
	Pods                 int64
}

// QuotaUtilizationBP holds utilization ratios in basis points (used or recommended vs hard).
type QuotaUtilizationBP struct {
	CPURequestBP     *int
	CPULimitBP       *int
	MemoryRequestBP  *int
	MemoryLimitBP    *int
	StorageRequestBP *int
	PodsBP           *int
	ObjectCountBP    *int
}

// QuotaCapacityFreed holds capacity that could be reclaimed by tightening quota.
type QuotaCapacityFreed struct {
	CPUMillicores int64
	MemoryBytes   int64
	StorageBytes  int64
	PodsFreed     int64
}

// ClusterQuotaSnapshot is the latest hard/used per ClusterResourceQuota from digests.
type ClusterQuotaSnapshot struct {
	ClusterQuotaName        string
	Namespaces              string
	CPURequestHardMC        int64
	CPULimitHardMC          int64
	MemoryRequestHardBytes  int64
	MemoryLimitHardBytes    int64
	CPURequestUsedMC        int64
	CPULimitUsedMC          int64
	MemoryRequestUsedBytes  int64
	MemoryLimitUsedBytes    int64
	StorageRequestHardBytes int64
	StorageRequestUsedBytes int64
	PodsHard                int64
	PodsUsed                int64
	ObjectCountHard         int64
	ObjectCountUsed         int64
	LastObservedAt          time.Time
}

// NamespaceQuotaClusterAggregate sums namespace quota recommendations for selected namespaces.
type NamespaceQuotaClusterAggregate struct {
	CPURequestRecommendedMC       int64
	CPULimitRecommendedMC         int64
	MemoryRequestRecommendedBytes int64
	MemoryLimitRecommendedBytes   int64
}

// ClusterQuotaRec is the output of the cluster-quota recommendation engine.
type ClusterQuotaRec struct {
	OrgID                            string
	ClusterUUID                      string
	ClusterQuotaName                 string
	Namespaces                       string
	Snapshot                         ClusterQuotaSnapshot
	Recommended                      QuotaResourceBundle
	StorageRecommendedBytes          int64
	PodsRecommended                  int64
	UtilizationCPURequestPercent     *int
	UtilizationMemoryRequestPercent  *int
	UtilizationStorageRequestPercent *int
	UtilizationPodsPercent           *int
	CapacityFreed                    QuotaCapacityFreed
	EstimatedSavingsCents            int64
	RecommendationType               string
	RiskLevel                        string
	NotificationCodes                []int16
	Expl                             types.ClusterQuotaExplanationFactors
}

// HasHardLimits reports whether the snapshot has any hard quota limit set.
func (s NamespaceQuotaSnapshot) HasHardLimits() bool {
	return s.CPURequestHardMC > 0 || s.CPULimitHardMC > 0 ||
		s.MemoryRequestHardBytes > 0 || s.MemoryLimitHardBytes > 0 ||
		s.StorageRequestHardBytes > 0 || s.PodsHard > 0 || s.ObjectCountHard > 0
}

func (s NamespaceQuotaSnapshot) hasHardLimits() bool {
	return s.HasHardLimits()
}

// HasHardLimits reports whether the snapshot has any hard quota limit set.
func (s ClusterQuotaSnapshot) HasHardLimits() bool {
	return s.CPURequestHardMC > 0 || s.CPULimitHardMC > 0 ||
		s.MemoryRequestHardBytes > 0 || s.MemoryLimitHardBytes > 0 ||
		s.StorageRequestHardBytes > 0 || s.PodsHard > 0 || s.ObjectCountHard > 0
}

func (s ClusterQuotaSnapshot) hasHardLimits() bool {
	return s.HasHardLimits()
}
