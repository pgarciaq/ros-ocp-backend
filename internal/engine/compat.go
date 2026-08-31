// compat.go provides backward-compatible type aliases, constant aliases, and
// function-variable aliases that map root engine exports to sub-package
// implementations (core, gpu, node, pvc, quota, container, etc.).
//
// Why this file exists: the engine package was decomposed from a monolithic
// "god package" (~245 files) into 8 focused sub-packages during Phases 1–4.
// External callers (API handlers, ingestion pipeline, tests) originally imported
// symbols from the root engine package. These aliases preserve that import path
// so callers can migrate to direct sub-package imports incrementally.
//
// Maintenance: every new exported symbol in a sub-package that was previously
// exported from the root engine needs a corresponding alias here. Aliases are
// purely mechanical — they carry no logic and are tested through the original
// call sites. As callers migrate to direct sub-package imports, stale aliases
// can be removed. The long-term goal is to shrink this file to zero.
package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/gpu"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/node"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/pvc"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/quota"
	"github.com/redhatinsights/ros-ocp-backend/internal/fixedpoint"
)

// --- Exported type aliases (backward compat for external consumers) ---

type DigestRow = core.DigestRow
type CPURec = core.CPURec
type MemoryRec = core.MemoryRec
type ContainerRec = core.ContainerRec
type TermConfig = core.TermConfig
type CPUConfig = core.CPUConfig
type MemoryConfig = core.MemoryConfig
type SizingThresholdSettings = core.SizingThresholdSettings
type IdleState = core.IdleState
type IdleConfig = core.IdleConfig
type IdleResult = core.IdleResult
type ContainerExplanationFactors = core.ContainerExplanationFactors
type GPUExplanationFactors = core.GPUExplanationFactors
type NodeExplanationFactors = core.NodeExplanationFactors
type PVCExplanationFactors = core.PVCExplanationFactors
type QuotaExplanationFactors = core.QuotaExplanationFactors
type ClusterQuotaExplanationFactors = core.ClusterQuotaExplanationFactors
type VMExplanationFactors = core.VMExplanationFactors
type SnapshotExplanationFactors = core.SnapshotExplanationFactors
type NodeGPUTimeslicingExplanationFactors = core.NodeGPUTimeslicingExplanationFactors
type WindowExtraOpts = core.WindowExtraOpts
type WindowExtras = core.WindowExtras
type RateCard = core.RateCard
type NamespaceSpend = core.NamespaceSpend
type KeyedDigest = core.KeyedDigest
type EngineConfig = core.EngineConfig
type EmitContainer = core.EmitContainer
type ContainerKey = core.ContainerKey

// --- Exported constants ---

const (
	CategoryUndersized   = core.CategoryUndersized
	CategoryOversized    = core.CategoryOversized
	CategoryOptimized    = core.CategoryOptimized
	CategoryThresholdPct = core.CategoryThresholdPct

	DefaultIdleThresholdMC     = core.DefaultIdleThresholdMC
	DefaultIdleThresholdMemKiB = core.DefaultIdleThresholdMemKiB

	MarginScale = core.MarginScale

	MicroCentsPerDollar = core.MicroCentsPerDollar
	MillicoresPerCore   = core.MillicoresPerCore
	KiBPerGiB           = core.KiBPerGiB
	HoursPerMonthInt    = core.HoursPerMonthInt
)

const (
	IdleStateActive = core.IdleStateActive
	IdleStateIdle   = core.IdleStateIdle
	IdleStateZombie = core.IdleStateZombie
)

// --- Exported function aliases ---

var (
	DecayWeight                       = core.DecayWeight
	DeriveDecayHalfLifeHours          = core.DeriveDecayHalfLifeHours
	WeightedPercentile                = core.WeightedPercentile
	MultiWeightedPercentile           = core.MultiWeightedPercentile
	MultiWeightedPercentileWithExtras = core.MultiWeightedPercentileWithExtras
	SelectCPUUsagePercentile          = core.SelectCPUUsagePercentile
	SelectMemUsagePercentile          = core.SelectMemUsagePercentile
	ComputeTrendSlope                 = core.ComputeTrendSlope
	ComputeAdaptiveMargin             = core.ComputeAdaptiveMargin
	ComputeAdaptiveMarginScaledDirect = core.ComputeAdaptiveMarginScaledDirect
	ComputeAdaptiveMarginScaled       = core.ComputeAdaptiveMarginScaled
	ScaleMargin                       = core.ScaleMargin
	ApplyScaledMargin                 = core.ApplyScaledMargin
	ScaleLimitMultiplier              = core.ScaleLimitMultiplier
	ApplyOOMBumpScaled                = core.ApplyOOMBumpScaled
	ClassifyResource                  = core.ClassifyResource
	ClassifyOverall                   = core.ClassifyOverall
	ClassifyIdleState                 = core.ClassifyIdleState
	CategoryFromIdleState             = core.CategoryFromIdleState
	DefaultIdleConfig                 = core.DefaultIdleConfig
	CPUConfigFromSizing               = core.CPUConfigFromSizing
	ErrFieldsLocked                   = core.ErrFieldsLocked
	ErrPartitionMissing               = core.ErrPartitionMissing
	LockedFieldsFromError             = core.LockedFieldsFromError

	// Calendar-accurate month helper
	HoursInMonth = core.HoursInMonth

	// Savings functions
	RateMicroCentsPerMCHour           = core.RateMicroCentsPerMCHour
	RateMicroCentsPerGiBHour          = core.RateMicroCentsPerGiBHour
	RateMicroCentsPerGiBMonth         = core.RateMicroCentsPerGiBMonth
	RateMicroCentsPerDollarMonth      = core.RateMicroCentsPerDollarMonth
	EffectiveRateMicroCentsPerMCHour  = core.EffectiveRateMicroCentsPerMCHour
	EffectiveRateMicroCentsPerGiBHour = core.EffectiveRateMicroCentsPerGiBHour
	EffectiveRateFromCPUTotals        = core.EffectiveRateFromCPUTotals
	EffectiveRateFromMemTotals        = core.EffectiveRateFromMemTotals
	CPUSavingsMicroCents              = core.CPUSavingsMicroCents
	MemSavingsMicroCentsFromKiB       = core.MemSavingsMicroCentsFromKiB
	GiBSavingsMicroCents              = core.GiBSavingsMicroCents
	VCPUSavingsMicroCents             = core.VCPUSavingsMicroCents
	MemorySavingsMicroCentsFromBytes  = core.MemorySavingsMicroCentsFromBytes
	StorageSavingsMicroCentsFromBytes = core.StorageSavingsMicroCentsFromBytes
	MonthlyFlatSavingsMicroCents      = core.MonthlyFlatSavingsMicroCents
	MIGFractionSavingsMicroCents      = core.MIGFractionSavingsMicroCents
	ScaleMicroCentsByBasisPoints      = core.ScaleMicroCentsByBasisPoints
	MicroCentsToCents                 = core.MicroCentsToCents
	MicroCentsToDollars               = core.MicroCentsToDollars
	QuotaTightenSavingsMicroCents     = core.QuotaTightenSavingsMicroCents

	// Cost rate functions
	CPUCoreHourlyRate           = costdata.CPUCoreHourlyRate
	MemoryGBHourlyRate          = costdata.MemoryGBHourlyRate
	NodeCostPerMonth            = costdata.NodeCostPerMonth
	VMCostPerMonth              = costdata.VMCostPerMonth
	EffectiveCPUCoreHourlyRate  = costdata.EffectiveCPUCoreHourlyRate
	EffectiveMemoryGBHourlyRate = costdata.EffectiveMemoryGBHourlyRate
	StorageRequestPerMonth      = costdata.StorageRequestPerMonth

	// Explanation persistence
	NullIntExpl            = core.NullIntExpl
	NullInt64Expl          = core.NullInt64Expl
	NullInt32Expl          = core.NullInt32Expl
	NullFloatExpl          = core.NullFloatExpl
	NullStringExpl         = core.NullStringExpl
	AppendSnapshotExplArgs = core.AppendSnapshotExplArgs
	AppendVMExplArgs       = core.AppendVMExplArgs
)

// Exported SQL constants from explanation_persist
const SnapshotExplSQLColumns = core.SnapshotExplSQLColumns
const SnapshotExplUpdateSet = core.SnapshotExplUpdateSet
const VMExplSQLColumns = core.VMExplSQLColumns
const VMExplUpdateSet = core.VMExplUpdateSet

// --- Unexported aliases (for root engine internal callers) ---

var (
	nullIfEmpty                         = core.NullIfEmpty
	nullIfZeroInt64                     = core.NullIfZeroInt64
	decayTableLookup                    = core.DecayTableLookup
	appendUnique                        = core.AppendUnique
	mergeNotificationCodes              = core.MergeNotificationCodes
	sortedNotificationCodes             = core.SortedNotificationCodes
	splitCSVList                        = core.SplitCSVList
	maxDailyCPUUsageP95                 = core.MaxDailyCPUUsageP95
	maxDailyMemUsageP95                 = core.MaxDailyMemUsageP95
	maxCPU                              = core.MaxCPU
	maxMemBytes                         = core.MaxMemBytes
	findIdleSince                       = core.FindIdleSince
	isExcludedWorkloadType              = core.IsExcludedWorkloadType
	isExcludedNamespace                 = core.IsExcludedNamespace
	computeIdleDuration                 = core.ComputeIdleDuration
	idleStateForWrite                   = core.IdleStateForWrite
	allZeroUsage                        = core.AllZeroUsage
	idleClassificationAuthoritative     = core.IdleClassificationAuthoritative
	clampNonNegativeUSD                 = core.ClampNonNegativeUSD
	replicaCountInt                     = core.ReplicaCountInt
	replicaCountForSavingsApply         = core.ReplicaCountForSavingsApply
	combinedConfiguredRate              = costdata.CombinedConfiguredRate
	combinedConfiguredRateWithFallbacks = costdata.CombinedConfiguredRateWithFallbacks

	// Explanation persist unexported
	containerExplValuePlaceholders   = core.ContainerExplValuePlaceholders
	appendContainerExplArgs          = core.AppendContainerExplArgs
	appendGPUExplArgs                = core.AppendGPUExplArgs
	appendQuotaExplArgs              = core.AppendQuotaExplArgs
	appendClusterQuotaExplArgs       = core.AppendClusterQuotaExplArgs
	appendNodeGPUTimeslicingExplArgs = core.AppendNodeGPUTimeslicingExplArgs
)

// Unexported const aliases
const containerExplSQLColumns = core.ContainerExplSQLColumns
const containerExplUpdateSet = core.ContainerExplUpdateSet
const gpuExplUpdateSet = core.GPUExplUpdateSet
const quotaExplSQLColumns = core.QuotaExplSQLColumns
const quotaExplUpdateSet = core.QuotaExplUpdateSet
const clusterQuotaExplSQLColumns = core.ClusterQuotaExplSQLColumns
const clusterQuotaExplUpdateSet = core.ClusterQuotaExplUpdateSet
const nodeGPUTimeslicingExplSQLColumns = core.NodeGPUTimeslicingExplSQLColumns
const nodeGPUTimeslicingExplUpdateSet = core.NodeGPUTimeslicingExplUpdateSet

// Unexported savings consts
const microCentsPerCent int64 = core.MicroCentsPerCent
const bytesPerGiB int64 = core.BytesPerGiB

// --- Quality utility aliases (canonical in core) ---

var (
	MaxWindowDaysFromCore                 = core.MaxWindowDays
	WithinToleranceFromCore               = core.WithinTolerance
	ComputeRecommendationAgeHoursFromCore = core.ComputeRecommendationAgeHours
	IsPartitionMissingFromCore            = core.IsPartitionMissing
)

// PgxBatchSenderAlias provides backward compat for external consumers.
type PgxBatchSenderAlias = db.PgxBatchSender

// --- PVC domain aliases ---

type PVCDigestRow = pvc.PVCDigestRow
type PVCRec = pvc.PVCRec
type OldPVCRecommendation = pvc.OldPVCRecommendation
type PVCQualityRow = pvc.PVCQualityRow
type PVCQualityKey = pvc.QualityKey

var (
	PVCConfidenceLevel               = pvc.PVCConfidenceLevel
	EvaluatePVCNotifications         = pvc.EvaluatePVCNotifications
	WritePVCRecommendations          = pvc.WritePVCRecommendations
	ApplyPVCSavings                  = pvc.ApplyPVCSavings
	ComputePVCStability              = pvc.ComputePVCStability
	DetectPVCAdoption                = pvc.DetectPVCAdoption
	CountPVCDaysAboveThreshold       = pvc.CountPVCDaysAboveThreshold
	WritePVCQuality                  = pvc.WritePVCQuality
	BuildPVCQualityRows              = pvc.BuildPVCQualityRows
	ReadClusterOldPVCRecommendations = pvc.ReadClusterOldPVCRecommendations
)

// PVC recommendation type constants.
const (
	PVCRecTypeOversized = "oversized"
	PVCRecTypeNearFull  = "near_full"
	PVCRecTypeOrphaned  = "orphaned"
	PVCRecTypeHealthy   = "healthy"
)

// --- Node domain aliases ---

type NodeDigestRow = node.DigestRow
type NodeRec = node.Rec
type NodeEngineConfig = node.EngineConfig
type NodeRecConfig = node.RecConfig

var (
	RecommendNodes                    = node.RecommendNodes
	QueryNodeDigests                  = node.QueryNodeDigests
	QueryNodeDigestsBySchedule        = node.QueryNodeDigestsBySchedule
	QueryNodeDigestsForNodeBySchedule = node.QueryNodeDigestsForNodeBySchedule
	PersistNodeRecommendations        = node.PersistRecommendations
	ApplyNodeSavings                  = node.ApplyNodeSavings
	LinearRegressionSlope             = node.LinearRegressionSlope
)

// --- Quota domain aliases ---

type QuotaRecConfig = quota.QuotaRecConfig
type NamespaceQuotaSnapshot = quota.NamespaceQuotaSnapshot
type ContainerQuotaAggregate = quota.ContainerQuotaAggregate
type QuotaRec = quota.QuotaRec
type QuotaResourceBundle = quota.QuotaResourceBundle
type QuotaUtilizationBP = quota.QuotaUtilizationBP
type QuotaCapacityFreed = quota.QuotaCapacityFreed
type ClusterQuotaSnapshot = quota.ClusterQuotaSnapshot
type NamespaceQuotaClusterAggregate = quota.NamespaceQuotaClusterAggregate
type ClusterQuotaRec = quota.ClusterQuotaRec
type QuotaRecommendationHistoryRow = quota.QuotaRecommendationHistoryRow
type ClusterQuotaRecommendationHistoryRow = quota.ClusterQuotaRecommendationHistoryRow

const (
	QuotaRecTypeTighten  = quota.QuotaRecTypeTighten
	QuotaRecTypeRaise    = quota.QuotaRecTypeRaise
	QuotaRecTypeOptimal  = quota.QuotaRecTypeOptimal
	QuotaRecTypeNone     = quota.QuotaRecTypeNone
	QuotaRiskHigh        = quota.QuotaRiskHigh
	QuotaRiskMedium      = quota.QuotaRiskMedium
	QuotaRiskLow         = quota.QuotaRiskLow
	QuotaRiskNone        = quota.QuotaRiskNone
	QuotaContainerTerm   = quota.QuotaContainerTerm
	QuotaContainerEngine = quota.QuotaContainerEngine
)

var (
	RecommendQuotas                           = quota.RecommendQuotas
	RecommendClusterQuotas                    = quota.RecommendClusterQuotas
	ApplyQuotaSavings                         = quota.ApplyQuotaSavings
	ApplyClusterQuotaSavings                  = quota.ApplyClusterQuotaSavings
	WriteQuotaRecommendations                 = quota.WriteQuotaRecommendations
	WriteClusterQuotaRecommendations          = quota.WriteClusterQuotaRecommendations
	AppendQuotaRecommendationHistory          = quota.AppendQuotaRecommendationHistory
	AppendClusterQuotaRecommendationHistory   = quota.AppendClusterQuotaRecommendationHistory
	PruneQuotaRecommendationHistory           = quota.PruneQuotaRecommendationHistory
	PruneClusterQuotaRecommendationHistory    = quota.PruneClusterQuotaRecommendationHistory
	ListQuotaRecommendationHistory            = quota.ListQuotaRecommendationHistory
	ListClusterQuotaRecommendationHistory     = quota.ListClusterQuotaRecommendationHistory
	QueryContainerQuotaAggregates             = quota.QueryContainerQuotaAggregates
	QueryLatestNamespaceQuotaSnapshots        = quota.QueryLatestNamespaceQuotaSnapshots
	QueryLatestClusterQuotaSnapshots          = quota.QueryLatestClusterQuotaSnapshots
	QueryNamespaceQuotaAggregateForNamespaces = quota.QueryNamespaceQuotaAggregateForNamespaces
	QuotaNotificationCodes                    = quota.QuotaNotificationCodes
	ClusterQuotaNotificationCodes             = quota.ClusterQuotaNotificationCodes
	QuotaRecConfigFromApp                     = quota.QuotaRecConfigFromApp
)

// --- GPU domain aliases ---

type GPUClassification = gpu.GPUClassification
type GPUDigestRow = gpu.GPUDigestRow
type GPURec = gpu.GPURec
type GPUThresholds = gpu.GPUThresholds
type GPUIdleConfig = gpu.GPUIdleConfig
type GPUQueryFilters = gpu.GPUQueryFilters
type GPUContainerKey = gpu.GPUContainerKey
type PageGPUKey = gpu.PageGPUKey
type GPUTimeslicingCrossRef = gpu.GPUTimeslicingCrossRef
type GPUMIGListFilters = gpu.GPUMIGListFilters
type GPUMIGListSeek = gpu.GPUMIGListSeek
type OldGPUMIGRecommendation = gpu.OldGPUMIGRecommendation
type GPUMIGQualityRow = gpu.GPUMIGQualityRow
type GPUModelSpec = gpu.GPUModelSpec
type MIGProfile = gpu.MIGProfile
type TimeslicingRec = gpu.TimeslicingRec
type GPUContainerRef = gpu.GPUContainerRef
type NodeGPUGroup = gpu.NodeGPUGroup
type NodeGPUContainer = gpu.NodeGPUContainer
type NodeGPUTriple = gpu.NodeGPUTriple
type NodeGPUTripleSeek = gpu.NodeGPUTripleSeek
type VGPUProfile = gpu.VGPUProfile
type VGPUModelSpec = gpu.VGPUModelSpec

const gpuZombieThresholdBP = gpu.GPUZombieThresholdBP
const BasisPointsScale = fixedpoint.BasisPointsScale
const NotifGPUTimeSharingCandidate = gpu.NotifGPUTimeSharingCandidate
const GPUClassIdle = gpu.GPUClassIdle
const GPUClassUnderutilized = gpu.GPUClassUnderutilized
const GPUClassMemoryBound = gpu.GPUClassMemoryBound
const GPUClassWellUtilized = gpu.GPUClassWellUtilized
const GPUClassComputeBoundUnderutil = gpu.GPUClassComputeBoundUnderutil

var NodeGPUTimeslicingHistoryOrderBy = gpu.NodeGPUTimeslicingHistoryOrderBy

var (
	MarkContainersWithGPU                        = gpu.MarkContainersWithGPU
	StoreGPUClassifications                      = gpu.StoreGPUClassifications
	ComputeAndPersistNodeGPUTimeSlicingRecs      = gpu.ComputeAndPersistNodeGPUTimeSlicingRecs
	QueryGPURecommendations                      = gpu.QueryGPURecommendations
	ApplyGPUSavings                              = gpu.ApplyGPUSavings
	ComputeGPUSavingsCents                       = gpu.ComputeGPUSavingsCents
	GPUMonthlyRate                               = gpu.GPUMonthlyRate
	ClassifyGPUWorkload                          = gpu.ClassifyGPUWorkload
	DefaultGPUThresholds                         = gpu.DefaultGPUThresholds
	MatchGPUModel                                = gpu.MatchGPUModel
	VMBasisPointsToFraction                      = gpu.VMBasisPointsToFraction
	RecommendVGPUProfile                         = gpu.RecommendVGPUProfile
	VMFBUsedFraction                             = gpu.VMFBUsedFraction
	VMUtilCoefficientOfVariation                 = gpu.VMUtilCoefficientOfVariation
	MigTotalSlices                               = gpu.MigTotalSlices
	MigProfileSlices                             = gpu.MigProfileSlices
	ThresholdToBasisPoints                       = gpu.ThresholdToBasisPoints
	RatioToBasisPoints                           = fixedpoint.RatioToBasisPoints
	PersistGPUMIGRecommendationSets              = gpu.PersistGPUMIGRecommendationSets
	QueryGPURecommendationsForContainers         = gpu.QueryGPURecommendationsForContainers
	LoadPersistedGPUSavings                      = gpu.LoadPersistedGPUSavings
	LoadPersistedGPUTimeslicingCrossRefs         = gpu.LoadPersistedGPUTimeslicingCrossRefs
	GPUSavingsLookupKey                          = gpu.GPUSavingsLookupKey
	CountGPUMIGRecommendationSets                = gpu.CountGPUMIGRecommendationSets
	ListGPUMIGRecommendationSets                 = gpu.ListGPUMIGRecommendationSets
	CountGPUMIGGrouped                           = gpu.CountGPUMIGGrouped
	ListGPUMIGGrouped                            = gpu.ListGPUMIGGrouped
	CountNodeGPUTriples                          = gpu.CountNodeGPUTriples
	ListNodeGPUTriplesPage                       = gpu.ListNodeGPUTriplesPage
	CountOrgGPUClusterStats                      = gpu.CountOrgGPUClusterStats
	GPUOrderColumnSupportsTriplePagination       = gpu.GPUOrderColumnSupportsTriplePagination
	GroupGPURecsByNodeAndModel                   = gpu.GroupGPURecsByNodeAndModel
	ComputeNodeTimeslicingRecForOrg              = gpu.ComputeNodeTimeslicingRecForOrg
	ComputeNodeTimeslicingRecWithSettings        = gpu.ComputeNodeTimeslicingRecWithSettings
	ListNodeGPUTimeslicingRecommendationHistory  = gpu.ListNodeGPUTimeslicingRecommendationHistory
	PruneNodeGPUTimeslicingRecommendationHistory = gpu.PruneNodeGPUTimeslicingRecommendationHistory
)
