package engine

import "github.com/redhatinsights/ros-ocp-backend/internal/engine/core"

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
	DefaultIdleConfig                 = core.DefaultIdleConfig
	CPUConfigFromSizing               = core.CPUConfigFromSizing
	ErrFieldsLocked                   = core.ErrFieldsLocked
	ErrPartitionMissing               = core.ErrPartitionMissing
	LockedFieldsFromError             = core.LockedFieldsFromError

	// Savings functions
	RateMicroCentsPerMCHour           = core.RateMicroCentsPerMCHour
	RateMicroCentsPerGiBHour          = core.RateMicroCentsPerGiBHour
	RateMicroCentsPerGiBMonth         = core.RateMicroCentsPerGiBMonth
	RateMicroCentsPerDollarMonth      = core.RateMicroCentsPerDollarMonth
	EffectiveRateMicroCentsPerMCHour  = core.EffectiveRateMicroCentsPerMCHour
	EffectiveRateMicroCentsPerGiBHour = core.EffectiveRateMicroCentsPerGiBHour
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
	CPUCoreHourlyRate           = core.CPUCoreHourlyRate
	MemoryGBHourlyRate          = core.MemoryGBHourlyRate
	NodeCostPerMonth            = core.NodeCostPerMonth
	VMCostPerMonth              = core.VMCostPerMonth
	EffectiveCPUCoreHourlyRate  = core.EffectiveCPUCoreHourlyRate
	EffectiveMemoryGBHourlyRate = core.EffectiveMemoryGBHourlyRate
	StorageRequestPerMonth      = core.StorageRequestPerMonth

	// Explanation persistence
	NullIntExpl    = core.NullIntExpl
	NullInt64Expl  = core.NullInt64Expl
	NullInt32Expl  = core.NullInt32Expl
	NullFloatExpl  = core.NullFloatExpl
	NullStringExpl = core.NullStringExpl
	AppendSnapshotExplArgs         = core.AppendSnapshotExplArgs
	AppendVMExplArgs               = core.AppendVMExplArgs
)

// Exported SQL constants from explanation_persist
const SnapshotExplSQLColumns = core.SnapshotExplSQLColumns
const SnapshotExplUpdateSet = core.SnapshotExplUpdateSet
const VMExplSQLColumns = core.VMExplSQLColumns
const VMExplUpdateSet = core.VMExplUpdateSet

// --- Unexported aliases (for root engine internal callers) ---

var (
	nullIfEmpty      = core.NullIfEmpty
	nullIfZeroInt64  = core.NullIfZeroInt64
	decayTableLookup = core.DecayTableLookup
	appendUnique           = core.AppendUnique
	mergeNotificationCodes = core.MergeNotificationCodes
	sortedNotificationCodes = core.SortedNotificationCodes
	splitCSVList            = core.SplitCSVList
	maxDailyCPUUsageP95     = core.MaxDailyCPUUsageP95
	maxDailyMemUsageP95     = core.MaxDailyMemUsageP95
	maxCPU                  = core.MaxCPU
	maxMemBytes             = core.MaxMemBytes
	findIdleSince           = core.FindIdleSince
	isExcludedWorkloadType  = core.IsExcludedWorkloadType
	isExcludedNamespace     = core.IsExcludedNamespace
	computeIdleDuration     = core.ComputeIdleDuration
	idleStateForWrite       = core.IdleStateForWrite
	allZeroUsage            = core.AllZeroUsage
	idleClassificationAuthoritative = core.IdleClassificationAuthoritative
	clampNonNegativeUSD     = core.ClampNonNegativeUSD
	replicaCountInt         = core.ReplicaCountInt
	replicaCountForSavingsApply = core.ReplicaCountForSavingsApply
	combinedConfiguredRate          = core.CombinedConfiguredRate
	combinedConfiguredRateWithFallbacks = core.CombinedConfiguredRateWithFallbacks

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

// notificationCodeBitmap compatibility - expose the exported core type under the old unexported name.
type notificationCodeBitmap = core.NotificationCodeBitmap

var notificationCodesFromSlice = core.NotificationCodesFromSlice
