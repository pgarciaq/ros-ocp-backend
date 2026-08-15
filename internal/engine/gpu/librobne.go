package gpu

import libgpu "github.com/redhatinsights/ros-ocp-backend/librobne/gpu"

type GPUClassification = libgpu.GPUClassification
type GPUDigestRow = libgpu.GPUDigestRow
type GPURec = libgpu.GPURec
type GPUThresholds = libgpu.GPUThresholds
type GPUThresholdSettings = libgpu.GPUThresholdSettings
type GPUIdleConfig = libgpu.GPUIdleConfig
type GPUModelSpec = libgpu.GPUModelSpec
type MIGProfile = libgpu.MIGProfile
type TimeslicingRec = libgpu.TimeslicingRec
type GPUContainerRef = libgpu.GPUContainerRef
type NodeGPUGroup = libgpu.NodeGPUGroup
type NodeGPUContainer = libgpu.NodeGPUContainer
type VGPUProfile = libgpu.VGPUProfile
type VGPUModelSpec = libgpu.VGPUModelSpec
type GPUContainerKey = libgpu.GPUContainerKey

const (
	GPUClassIdle                  = libgpu.GPUClassIdle
	GPUClassUnderutilized         = libgpu.GPUClassUnderutilized
	GPUClassMemoryBound           = libgpu.GPUClassMemoryBound
	GPUClassComputeBoundUnderutil = libgpu.GPUClassComputeBoundUnderutil
	GPUClassWellUtilized          = libgpu.GPUClassWellUtilized
	GPUClassNoProfiling           = libgpu.GPUClassNoProfiling
	GPUZombieThresholdBP          = libgpu.GPUZombieThresholdBP
	NotifGPUTimeSharingCandidate  = libgpu.NotifGPUTimeSharingCandidate
	NodeGPUFreshnessDays          = libgpu.NodeGPUFreshnessDays
	BasisPointsScale              = libgpu.BasisPointsScale
)

var (
	DefaultGPUThresholds           = libgpu.DefaultGPUThresholds
	DefaultGPUThresholdSettings    = libgpu.DefaultGPUThresholdSettings
	NormalizeGPUThresholdSettings  = libgpu.NormalizeGPUThresholdSettings
	NormalizeGPUThresholds         = libgpu.NormalizeGPUThresholds
	SetDefaultGPUThresholdSettings = libgpu.SetDefaultGPUThresholdSettings
	CurrentGPUThresholdSettings    = libgpu.CurrentGPUThresholdSettings
	DefaultGPUIdleConfig           = libgpu.DefaultGPUIdleConfig
	ClassifyGPUIdleState           = libgpu.ClassifyGPUIdleState
	ClassifyGPUIdleFromDigests     = libgpu.ClassifyGPUIdleFromDigests
	ClassifyGPUWorkload            = libgpu.ClassifyGPUWorkload
	SelectMIGProfile               = libgpu.SelectMIGProfile
	GPUConfidence                  = libgpu.GPUConfidence
	GPUConfidenceWithSettings      = libgpu.GPUConfidenceWithSettings
	ComputeNodeTimeslicingRec      = libgpu.ComputeNodeTimeslicingRec
	GroupGPURecsByNodeAndModel     = libgpu.GroupGPURecsByNodeAndModel
	RecommendVGPUProfile           = libgpu.RecommendVGPUProfile
	MatchVGPUModel                 = libgpu.MatchVGPUModel
	VGPUProfileFBMiB               = libgpu.VGPUProfileFBMiB
	VMBasisPointsToFraction        = libgpu.VMBasisPointsToFraction
	VMUtilCoefficientOfVariation   = libgpu.VMUtilCoefficientOfVariation
	MigTotalSlices                 = libgpu.MigTotalSlices
	MigProfileSlices               = libgpu.MigProfileSlices
	GPUModelCount                  = libgpu.GPUModelCount
	ThresholdToBasisPoints         = libgpu.ThresholdToBasisPoints
	FloatToBasisPoints             = libgpu.FloatToBasisPoints
	BasisPointsToFloat             = libgpu.BasisPointsToFloat
	BasisPointsToFloat32           = libgpu.BasisPointsToFloat32
	RatioToBasisPoints             = libgpu.RatioToBasisPoints
	UtilizationBasisPoints         = libgpu.UtilizationBasisPoints
	FilterGPUByWindow              = libgpu.FilterGPUByWindow
	LatestGPUDigest                = libgpu.LatestGPUDigest
	filterGPUByWindow              = libgpu.FilterGPUByWindow
	latestGPUDigest                = libgpu.LatestGPUDigest
)
